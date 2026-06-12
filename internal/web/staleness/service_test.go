package staleness

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/bus"
	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
	"renart/internal/web/scheduler"
)

type fixture struct {
	store    *matlog.Store
	engine   *fingerprint.Engine
	service  *Service
	pipeline *pipeline.Pipeline
	events   *bus.Bus
	pushed   chan any
}

func sqlAsset(name, content string, upstreams ...string) *pipeline.Asset {
	asset := &pipeline.Asset{
		Name:            name,
		Type:            "duckdb.sql",
		ExecutableFile:  pipeline.ExecutableFile{Path: "/w/p/assets/" + name + ".sql", Content: content},
		Materialization: pipeline.Materialization{Type: pipeline.MaterializationTypeTable},
	}
	for _, upstream := range upstreams {
		asset.Upstreams = append(asset.Upstreams, pipeline.Upstream{Type: "asset", Value: upstream})
	}
	return asset
}

func newFixture(t *testing.T, assets ...*pipeline.Asset) *fixture {
	t.Helper()
	schedStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedStore.Close() })

	f := &fixture{
		store:  matlog.NewStore(schedStore.DB()),
		engine: fingerprint.NewEngine(),
		events: bus.New(),
		pushed: make(chan any, 16),
		pipeline: &pipeline.Pipeline{
			LegacyID:       "p",
			Name:           "test",
			DefinitionFile: pipeline.DefinitionFile{Path: "/w/p/pipeline.yml"},
			Assets:         assets,
		},
	}
	f.service = New(Dependencies{
		Store:   f.store,
		Engine:  f.engine,
		Resolve: func(ctx context.Context, uuid string) (*pipeline.Pipeline, error) { return f.pipeline, nil },
		Publish: func(event any) { f.pushed <- event },
	})
	f.service.AttachBus(f.events)
	return f
}

// recordRun simulates a completed run: it fingerprints the current pipeline
// and writes coverage rows for the named assets.
func (f *fixture) recordRun(t *testing.T, environment string, window *Interval, assetNames ...string) {
	t.Helper()
	vars := fingerprint.EffectiveVars(f.pipeline, nil)
	results, err := f.engine.DAG(f.pipeline, vars)
	require.NoError(t, err)
	varsHash := fingerprint.AllVarsHash(vars)

	for _, name := range assetNames {
		assetID := identity.AssetID("p", name)
		result := results[assetID]
		m := matlog.Materialization{
			AssetID:        assetID,
			Environment:    environment,
			Fingerprint:    string(result.FP),
			OwnContent:     string(result.OwnContent),
			VarsHash:       varsHash,
			RunID:          "run",
			MaterializedAt: time.Now().UTC(),
		}
		if window != nil {
			m.IntervalStart = &window.Start
			m.IntervalEnd = &window.End
		}
		require.NoError(t, f.store.Record(context.Background(), m))
	}
}

func (f *fixture) statuses(t *testing.T, environment string, start, end *time.Time) map[string]AssetStatus {
	t.Helper()
	statuses, err := f.service.Statuses(context.Background(), Selection{
		PipelineUUID: "p",
		Environment:  environment,
		Start:        start,
		End:          end,
	})
	require.NoError(t, err)
	byName := make(map[string]AssetStatus, len(statuses))
	for _, status := range statuses {
		byName[status.AssetName] = status
	}
	return byName
}

func TestNeverBuiltThenFresh(t *testing.T) {
	t.Parallel()
	f := newFixture(t, sqlAsset("a", "select 1"))

	assert.Equal(t, StatusNeverBuilt, f.statuses(t, "dev", nil, nil)["a"].Status)

	f.recordRun(t, "dev", nil, "a")
	assert.Equal(t, StatusFresh, f.statuses(t, "dev", nil, nil)["a"].Status)
}

func TestEditFlipsAssetAndCone(t *testing.T) {
	t.Parallel()
	f := newFixture(t,
		sqlAsset("a", "select 1"),
		sqlAsset("b", "select * from a", "a"),
		sqlAsset("c", "select 2"),
	)
	f.recordRun(t, "dev", nil, "a", "b", "c")
	require.Equal(t, StatusFresh, f.statuses(t, "dev", nil, nil)["a"].Status)

	// Edit a: own content changes, b inherits, c untouched.
	f.pipeline.Assets[0].ExecutableFile.Content = "select 1, 2"

	statuses := f.statuses(t, "dev", nil, nil)
	assert.Equal(t, StatusStaleEdited, statuses["a"].Status)
	assert.Equal(t, StatusStaleUpstream, statuses["b"].Status)
	assert.Equal(t, StatusFresh, statuses["c"].Status)
}

func TestCommentEditStaysFresh(t *testing.T) {
	t.Parallel()
	f := newFixture(t, sqlAsset("a", "select 1"))
	f.recordRun(t, "dev", nil, "a")

	f.pipeline.Assets[0].ExecutableFile.Content = "-- explain the query\nselect   1"
	assert.Equal(t, StatusFresh, f.statuses(t, "dev", nil, nil)["a"].Status)
}

func TestEnvironmentSwitchKeepsIndependentStatus(t *testing.T) {
	t.Parallel()
	f := newFixture(t, sqlAsset("a", "select 1"))
	f.recordRun(t, "prod", nil, "a")

	assert.Equal(t, StatusFresh, f.statuses(t, "prod", nil, nil)["a"].Status)
	assert.Equal(t, StatusNeverBuilt, f.statuses(t, "staging", nil, nil)["a"].Status)
	// Toggling back to a previously built env shows fresh again — the bug
	// the old reset-flags idea would have had.
	assert.Equal(t, StatusFresh, f.statuses(t, "prod", nil, nil)["a"].Status)
}

func intervalAwareAsset(name string) *pipeline.Asset {
	asset := sqlAsset(name, "select * from events")
	asset.Materialization.Strategy = pipeline.MaterializationStrategyTimeInterval
	asset.Materialization.IncrementalKey = "ts"
	return asset
}

func TestPartialCoverageReportsGaps(t *testing.T) {
	t.Parallel()
	f := newFixture(t, intervalAwareAsset("a"))
	day := func(d int) time.Time { return time.Date(2026, 6, 1+d, 0, 0, 0, 0, time.UTC) }

	// Cover days 0-20 of a 30-day selection.
	f.recordRun(t, "dev", &Interval{Start: day(0), End: day(20)}, "a")

	start, end := day(0), day(30)
	status := f.statuses(t, "dev", &start, &end)["a"]
	assert.Equal(t, StatusPartial, status.Status)
	assert.InDelta(t, (20 * 24 * time.Hour).Seconds(), status.CoveredSeconds, 1)
	assert.InDelta(t, (30 * 24 * time.Hour).Seconds(), status.TotalSeconds, 1)
	require.Len(t, status.Gaps, 1)
	assert.Equal(t, day(20), status.Gaps[0].Start)
	assert.Equal(t, day(30), status.Gaps[0].End)

	// Filling the gap turns it fresh.
	f.recordRun(t, "dev", &Interval{Start: day(20), End: day(30)}, "a")
	status = f.statuses(t, "dev", &start, &end)["a"]
	assert.Equal(t, StatusFresh, status.Status)
	assert.Empty(t, status.Gaps)
}

func TestUnbuiltSelectedRangeReadsAsZeroPartialNotStale(t *testing.T) {
	t.Parallel()
	f := newFixture(t, intervalAwareAsset("a"))
	day := func(d int) time.Time { return time.Date(2026, 6, 1+d, 0, 0, 0, 0, time.UTC) }

	// Build days 10-20, then look at the disjoint range 0-10: the
	// definition is unchanged, so this must read as "0/10 days built" with
	// the whole range as the gap — not as a stale_* fingerprint mismatch.
	f.recordRun(t, "dev", &Interval{Start: day(10), End: day(20)}, "a")

	start, end := day(0), day(10)
	status := f.statuses(t, "dev", &start, &end)["a"]
	assert.Equal(t, StatusPartial, status.Status)
	assert.Zero(t, status.CoveredSeconds)
	require.Len(t, status.Gaps, 1)
	assert.Equal(t, day(0), status.Gaps[0].Start)
	assert.Equal(t, day(10), status.Gaps[0].End)

	// Switching the selector back to the built range reads fresh again.
	builtStart, builtEnd := day(10), day(20)
	assert.Equal(t, StatusFresh, f.statuses(t, "dev", &builtStart, &builtEnd)["a"].Status)

	// An actual edit still reports stale_edited, not partial.
	f.pipeline.Assets[0].ExecutableFile.Content = "select * from events, more_events"
	assert.Equal(t, StatusStaleEdited, f.statuses(t, "dev", &builtStart, &builtEnd)["a"].Status)
}

func TestRunCompletedEventRecomputesAndPublishes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, sqlAsset("a", "select 1"))
	require.Equal(t, StatusNeverBuilt, f.statuses(t, "dev", nil, nil)["a"].Status)

	f.recordRun(t, "dev", nil, "a")
	f.events.EmitRunCompleted(bus.RunCompleted{PipelineUUID: "p", Environment: "dev", CompletedAt: time.Now()})

	select {
	case event := <-f.pushed:
		payload, ok := event.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "staleness.updated", payload["type"])
		statuses, ok := payload["assets"].([]AssetStatus)
		require.True(t, ok)
		require.Len(t, statuses, 1)
		assert.Equal(t, StatusFresh, statuses[0].Status)
	case <-time.After(5 * time.Second):
		t.Fatal("no staleness.updated event published")
	}
}

func TestVerificationDowngradesToMissing(t *testing.T) {
	t.Parallel()
	schedStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedStore.Close() })

	store := matlog.NewStore(schedStore.DB())
	engine := fingerprint.NewEngine()
	p := &pipeline.Pipeline{
		LegacyID:       "p",
		Name:           "test",
		DefinitionFile: pipeline.DefinitionFile{Path: "/w/p/pipeline.yml"},
		Assets:         []*pipeline.Asset{sqlAsset("a", "select 1")},
	}

	pushed := make(chan any, 16)
	verifyCalls := make(chan []string, 2)
	service := New(Dependencies{
		Store:   store,
		Engine:  engine,
		Resolve: func(ctx context.Context, uuid string) (*pipeline.Pipeline, error) { return p, nil },
		Publish: func(event any) { pushed <- event },
		Verify: func(ctx context.Context, selection Selection, assetNames []string) (map[string]bool, error) {
			verifyCalls <- assetNames
			return map[string]bool{"a": false}, nil
		},
	})

	vars := fingerprint.EffectiveVars(p, nil)
	results, err := engine.DAG(p, vars)
	require.NoError(t, err)
	result := results["p:a"]
	require.NoError(t, store.Record(context.Background(), matlog.Materialization{
		AssetID: "p:a", Environment: "dev",
		Fingerprint: string(result.FP), OwnContent: string(result.OwnContent),
		VarsHash: fingerprint.AllVarsHash(vars), RunID: "run", MaterializedAt: time.Now().UTC(),
	}))

	selection := Selection{PipelineUUID: "p", Environment: "dev"}
	statuses, err := service.Statuses(context.Background(), selection)
	require.NoError(t, err)
	assert.Equal(t, StatusFresh, statuses[0].Status)

	select {
	case names := <-verifyCalls:
		assert.Equal(t, []string{"a"}, names)
	case <-time.After(5 * time.Second):
		t.Fatal("verifier never called")
	}

	// The verification republish downgrades the asset to missing.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-pushed:
			payload := event.(map[string]any)
			statuses := payload["assets"].([]AssetStatus)
			if statuses[0].Status == StatusMissing {
				return
			}
		case <-deadline:
			t.Fatal("asset never downgraded to missing")
		}
	}
}

func TestVerificationThrottledPerSession(t *testing.T) {
	t.Parallel()
	f := newFixture(t, sqlAsset("a", "select 1"))
	calls := 0
	f.service.deps.Verify = func(ctx context.Context, selection Selection, assetNames []string) (map[string]bool, error) {
		calls++
		return map[string]bool{"a": true}, nil
	}
	f.recordRun(t, "dev", nil, "a")

	f.statuses(t, "dev", nil, nil)
	f.statuses(t, "dev", nil, nil)
	time.Sleep(100 * time.Millisecond)
	assert.LessOrEqual(t, calls, 1)
}
