package matlog_test

import (
	"context"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		AssetID: "p:a", Environment: "dev", Fingerprint: "fp1", Status: "failed", RanAt: ts(1),
	}))
	runs, err := store.LastRuns(ctx, []string{"p:a"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, "failed", runs["p:a"].Status)
	assert.Equal(t, "fp1", runs["p:a"].Fingerprint)

	// A later run of either outcome overwrites the previous row (one per key), so
	// a success clears the prior failure.
	require.NoError(t, store.RecordRun(ctx, matlog.AssetRunRecord{
		AssetID: "p:a", Environment: "dev", Fingerprint: "fp2", Status: "succeeded", RanAt: ts(2),
	}))
	runs, err = store.LastRuns(ctx, []string{"p:a"}, "dev")
	require.NoError(t, err)
	assert.Equal(t, "succeeded", runs["p:a"].Status)
	assert.Equal(t, "fp2", runs["p:a"].Fingerprint)

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
		RunID:          "run",
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
			RunID:          "run",
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
			IntervalStart: start, IntervalEnd: end, RunID: "run", MaterializedAt: ts(i),
		}))
	}
	rows := coverageRows(t, store)
	assert.Len(t, rows, 2)
}

func TestHasAnyCoverageIgnoresFingerprintAndVars(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	require.NoError(t, store.Record(ctx, matlog.Materialization{
		AssetID: "p:a", Environment: "prod", Fingerprint: "v1:old", VarsHash: "other",
		RunID: "run", MaterializedAt: ts(0),
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
