package matlog_test

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/bus"
	"renart/internal/web/matlog"
	"renart/internal/web/scheduler"
)

func openTestStore(t *testing.T) *matlog.Store {
	t.Helper()
	schedStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedStore.Close() })
	return matlog.NewStore(schedStore.DB())
}

func ts(hour int) time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(hour) * time.Hour)
}

func TestRecordRunUpsertsLatestAttempt(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.RecordRun(ctx, matlog.AssetRunRecord{
		AssetID: "p:a", Environment: "dev", Fingerprint: "fp1", Status: "succeeded", RunID: "run-1", RanAt: ts(1),
		QualityStatus: bus.QualityStatusFailed,
		FailedChecks: []bus.QualityCheckFailure{{
			Kind: bus.QualityCheckKindColumn, Name: "not_null", Column: "customer_id", Blocking: true,
		}},
	}))
	runs, err := store.LastRuns(ctx, []string{"p:a"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", runs["p:a"].Status)
	assert.Equal(t, "fp1", runs["p:a"].Fingerprint)
	assert.Equal(t, bus.QualityStatusFailed, runs["p:a"].QualityStatus)
	assert.Equal(t, []bus.QualityCheckFailure{{
		Kind: bus.QualityCheckKindColumn, Name: "not_null", Column: "customer_id", Blocking: true,
	}}, runs["p:a"].FailedChecks)

	// A later run of either outcome overwrites the previous row (one per key), so
	// a success clears the prior failure.
	require.NoError(t, store.RecordRun(ctx, matlog.AssetRunRecord{
		AssetID: "p:a", Environment: "dev", Fingerprint: "fp2", Status: "succeeded", RunID: "run-2", RanAt: ts(2),
	}))
	runs, err = store.LastRuns(ctx, []string{"p:a"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", runs["p:a"].Status)
	assert.Equal(t, "fp2", runs["p:a"].Fingerprint)
	assert.Equal(t, "run-2", runs["p:a"].RunID)
	assert.Empty(t, runs["p:a"].QualityStatus, "a newer unobserved quality result clears the old failure")
	assert.Empty(t, runs["p:a"].FailedChecks)

	// Startup recovery can replay an older persisted run after a newer attempt
	// was already recorded. It must not move the latest-attempt row backwards.
	require.NoError(t, store.RecordRun(ctx, matlog.AssetRunRecord{
		AssetID: "p:a", Environment: "dev", Fingerprint: "fp-old", Status: "failed", RunID: "run-old", RanAt: ts(1),
	}))
	runs, err = store.LastRuns(ctx, []string{"p:a"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", runs["p:a"].Status)
	assert.Equal(t, "fp2", runs["p:a"].Fingerprint)
	assert.Equal(t, "run-2", runs["p:a"].RunID)

	// Attempts are environment-scoped.
	other, err := store.LastRuns(ctx, []string{"p:a"}, "prod")
	require.NoError(t, err)
	assert.Empty(t, other)
}

func interval(startHour, endHour int) (start, end *time.Time) {
	s, e := ts(startHour), ts(endHour)
	return &s, &e
}

func record(t *testing.T, store *matlog.Store, startHour, endHour int) {
	t.Helper()
	start, end := interval(startHour, endHour)
	require.NoError(t, store.Record(context.Background(), matlog.Materialization{
		AssetID:        "p:a",
		Environment:    "prod",
		Fingerprint:    "v1:abc",
		VarsHash:       "vh",
		IntervalStart:  start,
		IntervalEnd:    end,
		RunID:          "",
		MaterializedAt: ts(endHour),
	}))
}

func coverageRows(t *testing.T, store *matlog.Store) []matlog.CoverageRow {
	t.Helper()
	byAsset, err := store.Coverage(context.Background(), []string{"p:a"}, "prod", "vh")
	require.NoError(t, err)
	return byAsset["p:a"]
}

func TestFullRefreshUpsertsSingleMarker(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.Record(ctx, matlog.Materialization{
			AssetID:        "p:a",
			Environment:    "prod",
			Fingerprint:    "v1:abc",
			VarsHash:       "vh",
			RunID:          "",
			MaterializedAt: ts(i),
		}))
	}

	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.True(t, rows[0].FullRefresh())
	assert.Equal(t, ts(2), rows[0].MaterializedAt)

	facts, err := store.CountFacts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, facts)
}

func TestReplacementIntervalResetsCoverage(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	record(t, store, 0, 2)
	record(t, store, 2, 4)
	require.Len(t, coverageRows(t, store), 1)

	start, end := interval(6, 7)
	require.NoError(t, store.Record(context.Background(), matlog.Materialization{
		AssetID:         "p:a",
		Environment:     "prod",
		Fingerprint:     "v1:abc",
		VarsHash:        "vh",
		IntervalStart:   start,
		IntervalEnd:     end,
		ReplaceCoverage: true,
		MaterializedAt:  ts(7),
	}))

	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, ts(6), *rows[0].IntervalStart)
	assert.Equal(t, ts(7), *rows[0].IntervalEnd)
}

func TestScheduledRunFactReplayIsIdempotent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()

	original := matlog.Materialization{
		AssetID: "p:a", Environment: "prod", Fingerprint: "v1:first", VarsHash: "vh",
		RunID: "scheduled-run", MaterializedAt: ts(1),
	}
	require.NoError(t, store.Record(ctx, original))
	// A crash after the first transaction committed can cause startup recovery
	// to emit the same completion again. An exact fact is a no-op, while reused
	// run coordinates with different evidence fail closed.
	require.NoError(t, store.Record(ctx, original))
	require.ErrorIs(t, store.Record(ctx, matlog.Materialization{
		AssetID: "p:a", Environment: "prod", Fingerprint: "v1:replayed", VarsHash: "vh",
		RunID: "scheduled-run", MaterializedAt: ts(2),
	}), matlog.ErrMaterializationReplayConflict)

	facts, err := store.CountFacts(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, facts)
	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, "v1:first", rows[0].Fingerprint)
	assert.Equal(t, ts(1), rows[0].MaterializedAt)
}

func TestContiguousAppendsStayOneRow(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	for hour := 0; hour < 24; hour++ {
		record(t, store, hour, hour+1)
	}
	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, ts(0), *rows[0].IntervalStart)
	assert.Equal(t, ts(24), *rows[0].IntervalEnd)
}

func TestOutOfOrderBackfillMerges(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	hours := rand.New(rand.NewSource(42)).Perm(24)
	for _, hour := range hours {
		record(t, store, hour, hour+1)
	}
	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, ts(0), *rows[0].IntervalStart)
	assert.Equal(t, ts(24), *rows[0].IntervalEnd)
}

func TestBridgeCollapsesThreeRowsToOne(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	record(t, store, 0, 1)
	record(t, store, 2, 3)
	require.Len(t, coverageRows(t, store), 2)

	record(t, store, 1, 2)
	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, ts(0), *rows[0].IntervalStart)
	assert.Equal(t, ts(3), *rows[0].IntervalEnd)
}

func TestGapStaysTwoRows(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	record(t, store, 0, 1)
	record(t, store, 5, 6)
	assert.Len(t, coverageRows(t, store), 2)
}

func TestFullContainmentAbsorbed(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	record(t, store, 0, 10)
	record(t, store, 2, 3)
	rows := coverageRows(t, store)
	require.Len(t, rows, 1)
	assert.Equal(t, ts(0), *rows[0].IntervalStart)
	assert.Equal(t, ts(10), *rows[0].IntervalEnd)
}

func TestDifferentFingerprintsDoNotMerge(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	for i, fp := range []string{"v1:one", "v1:two"} {
		start, end := interval(i, i+1)
		require.NoError(t, store.Record(ctx, matlog.Materialization{
			AssetID: "p:a", Environment: "prod", Fingerprint: fp, VarsHash: "vh",
			IntervalStart: start, IntervalEnd: end, RunID: "", MaterializedAt: ts(i),
		}))
	}
	rows := coverageRows(t, store)
	assert.Len(t, rows, 2)
}

func TestDifferentVariableVariantsDoNotMergeCoverage(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	for i, varsHash := range []string{"region-eu", "region-us"} {
		start, end := interval(i, i+1)
		require.NoError(t, store.Record(ctx, matlog.Materialization{
			AssetID: "p:a", Environment: "prod", Fingerprint: "v1:same", VarsHash: varsHash,
			IntervalStart: start, IntervalEnd: end, RunID: "", MaterializedAt: ts(i),
		}))
	}
	eu, err := store.Coverage(ctx, []string{"p:a"}, "prod", "region-eu")
	require.NoError(t, err)
	require.Len(t, eu["p:a"], 1)
	assert.Equal(t, ts(0), *eu["p:a"][0].IntervalStart)
	assert.Equal(t, ts(1), *eu["p:a"][0].IntervalEnd)

	us, err := store.Coverage(ctx, []string{"p:a"}, "prod", "region-us")
	require.NoError(t, err)
	require.Len(t, us["p:a"], 1)
	assert.Equal(t, ts(1), *us["p:a"][0].IntervalStart)
	assert.Equal(t, ts(2), *us["p:a"][0].IntervalEnd)
}

func TestHasAnyCoverageIgnoresFingerprintAndVars(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Record(ctx, matlog.Materialization{
		AssetID: "p:a", Environment: "prod", Fingerprint: "v1:old", VarsHash: "other",
		RunID: "", MaterializedAt: ts(0),
	}))

	built, err := store.HasAnyCoverage(ctx, []string{"p:a", "p:b"}, "prod")
	require.NoError(t, err)
	assert.True(t, built["p:a"])
	assert.False(t, built["p:b"])

	builtStaging, err := store.HasAnyCoverage(ctx, []string{"p:a"}, "staging")
	require.NoError(t, err)
	assert.False(t, builtStaging["p:a"])
}

func TestPruneRemovesOldFactsKeepsCoverage(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	record(t, store, 0, 1)
	record(t, store, 1, 2)

	pruned, err := store.Prune(ctx, ts(2).Add(time.Minute))
	require.NoError(t, err)
	assert.EqualValues(t, 2, pruned)

	facts, err := store.CountFacts(ctx)
	require.NoError(t, err)
	assert.Zero(t, facts)
	assert.Len(t, coverageRows(t, store), 1)
}

// TestYearOfHourlyRunsSimulation is the Phase 2 exit criterion: a gapless
// year of hourly appends compacts to one coverage row and lookups stay flat.
func TestYearOfHourlyRunsSimulation(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	for hour := 0; hour < 24*365; hour++ {
		record(t, store, hour, hour+1)
	}

	started := time.Now()
	rows := coverageRows(t, store)
	elapsed := time.Since(started)

	require.Len(t, rows, 1)
	assert.Equal(t, ts(0), *rows[0].IntervalStart)
	assert.Equal(t, ts(24*365), *rows[0].IntervalEnd)
	assert.Less(t, elapsed, time.Second, "coverage lookup should be O(gaps), not O(runs)")
}
