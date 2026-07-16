package scheduler

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineRunJSONExposesResolutionWithoutLeakingAmbiguousModes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		resolved bool
	}{
		{name: "request only", resolved: false},
		{name: "effective", resolved: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(PipelineRun{
				ID: "run-id", PipelineID: "pipeline-id", Pipeline: "analytics",
				Trigger: RunTriggerManual, Status: RunStatusQueued,
				FullRefresh: true, ExecutionContextResolved: test.resolved,
			})
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(raw, &payload))
			assert.Equal(t, test.resolved, payload["execution_context_resolved"])
			assert.NotContains(t, payload, "full_refresh")
			assert.NotContains(t, payload, "backfill")
			assert.NotContains(t, payload, "sensor_mode")
		})
	}
}

func TestRunExecutionContextMigrationKeepsLegacyRowsReadable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)
	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 10)
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, pipeline_id, pipeline, environment, trigger, status)
		VALUES ('legacy-run', 'pipeline-id', 'analytics', 'prod', 'manual', 'failed')`)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()
	run, _, _, err := store.Get(ctx, "legacy-run")
	require.NoError(t, err)
	assert.False(t, run.FullRefresh)
	assert.False(t, run.Backfill)
	assert.Empty(t, run.SensorMode)
	assert.False(t, run.ExecutionContextResolved)
}

func TestStorePersistsRequestedAndResolvedRunExecutionContext(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	requestedStart := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	requestedEnd := requestedStart.Add(time.Hour)
	runID, err := store.Create(ctx, PipelineRun{
		PipelineID:  "pipeline-id",
		Pipeline:    "analytics",
		Environment: "prod",
		Trigger:     RunTriggerManual,
		Status:      RunStatusQueued,
		WinStart:    &requestedStart,
		WinEnd:      &requestedEnd,
		FullRefresh: true,
		SensorMode:  "skip",
	})
	require.NoError(t, err)

	requested, _, _, err := store.Get(ctx, runID)
	require.NoError(t, err)
	assert.True(t, requested.FullRefresh)
	assert.False(t, requested.Backfill)
	assert.Equal(t, "skip", requested.SensorMode)
	assert.False(t, requested.ExecutionContextResolved)

	resolvedStart := requestedStart.Add(24 * time.Hour)
	resolvedEnd := resolvedStart.Add(30 * time.Minute)
	require.NoError(t, store.SetRunExecutionContext(ctx, runID, RunExecutionContext{
		Environment: " restricted-prod ",
		WinStart:    resolvedStart,
		WinEnd:      resolvedEnd,
		FullRefresh: false,
		Backfill:    true,
		SensorMode:  " wait ",
	}))

	resolved, _, _, err := store.Get(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, "restricted-prod", resolved.Environment)
	assert.Equal(t, resolvedStart, *resolved.WinStart)
	assert.Equal(t, resolvedEnd, *resolved.WinEnd)
	assert.False(t, resolved.FullRefresh, "effective restriction replaces requested full refresh")
	assert.True(t, resolved.Backfill)
	assert.Equal(t, "wait", resolved.SensorMode)
	assert.True(t, resolved.ExecutionContextResolved)

	listed, err := store.List(ctx, RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, err)
	require.Len(t, listed.Runs, 1)
	assert.Equal(t, resolved.FullRefresh, listed.Runs[0].FullRefresh)
	assert.Equal(t, resolved.Backfill, listed.Runs[0].Backfill)
	assert.Equal(t, resolved.SensorMode, listed.Runs[0].SensorMode)
	assert.True(t, listed.Runs[0].ExecutionContextResolved)

	err = store.SetRunExecutionContext(ctx, "missing-run", RunExecutionContext{
		Environment: "prod", WinStart: resolvedStart, WinEnd: resolvedEnd, SensorMode: "once",
	})
	require.ErrorContains(t, err, "was not found")
}

func TestManualAndScheduledAdmissionPersistRequestedExecutionModes(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	manualService := newRunningTestService(t, store, func(context.Context, RunRequest, func(string)) RunResult {
		return RunResult{Status: "ok"}
	})
	manual, err := manualService.Trigger(ctx, PipelineSchedule{PipelineID: "manual-pipeline", PipelineName: "manual"}, TriggerRequest{
		Environment: "prod",
		FullRefresh: true,
		SensorMode:  "skip",
	})
	require.NoError(t, err)
	persistedManual, _, _, err := store.Get(ctx, manual.ID)
	require.NoError(t, err)
	assert.True(t, persistedManual.FullRefresh)
	assert.False(t, persistedManual.Backfill)
	assert.Equal(t, "skip", persistedManual.SensorMode)

	scheduledService := New(Options{
		Store: store,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "scheduled-pipeline", Name: "scheduled"}, true
		},
	})
	scheduled, _, ok, err := scheduledService.prepareRun(ctx, 42, pipelineRunJobArgs{
		PipelineUUID:      "scheduled-uuid",
		PipelineName:      "scheduled",
		Environment:       "prod",
		Trigger:           RunTriggerSchedule,
		Start:             "2026-07-16T08:00:00Z",
		End:               "2026-07-16T09:00:00Z",
		SnapshotVersionID: "snapshot-id",
		Backfill:          true,
		SensorMode:        "wait",
	})
	require.NoError(t, err)
	require.True(t, ok)
	persistedScheduled, _, _, err := store.Get(ctx, scheduled.ID)
	require.NoError(t, err)
	assert.False(t, persistedScheduled.FullRefresh)
	assert.True(t, persistedScheduled.Backfill)
	assert.Equal(t, "wait", persistedScheduled.SensorMode)
}

func TestInterruptedStateKeepsResolvedContextAndBackfillsLegacyRiverIntent(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	start := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	resolvedID, err := store.Create(ctx, PipelineRun{
		ID: "resolved", PipelineID: "resolved-pipeline", Pipeline: "resolved",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
		FullRefresh: true,
	})
	require.NoError(t, err)
	resolvedJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: resolvedID, FullRefresh: true, SensorMode: "skip"})
	require.NoError(t, store.SetRunRiverJob(ctx, resolvedID, resolvedJobID))
	require.NoError(t, store.SetRunExecutionContext(ctx, resolvedID, RunExecutionContext{
		Environment: "prod", WinStart: start, WinEnd: end,
		FullRefresh: false, SensorMode: "once",
	}))
	markTestRiverJobRunning(t, store, resolvedJobID)

	legacyID, err := store.Create(ctx, PipelineRun{
		ID: "legacy", PipelineID: "legacy-pipeline", Pipeline: "legacy",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	legacyJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: legacyID, FullRefresh: true, SensorMode: "skip"})
	markTestRiverJobRunning(t, store, legacyJobID)

	specBackedRun := PipelineRun{
		ID: "spec-backed", PipelineID: "spec-backed-pipeline", Pipeline: "spec-backed",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
		FullRefresh: true, SensorMode: "skip",
	}
	_, err = store.CreateWithSpec(ctx, specBackedRun, manualRunSpec(specBackedRun, RunSourceWorkingTree, "prod"))
	require.NoError(t, err)
	specBackedJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: specBackedRun.ID})
	require.NoError(t, store.SetRunRiverJob(ctx, specBackedRun.ID, specBackedJobID))
	markTestRiverJobRunning(t, store, specBackedJobID)

	_, err = store.ReconcileInterruptedState(ctx, orphanedRunError)
	require.NoError(t, err)

	resolved, _, _, err := store.Get(ctx, resolvedID)
	require.NoError(t, err)
	assert.False(t, resolved.FullRefresh, "River request must not overwrite the persisted effective mode")
	assert.Equal(t, "once", resolved.SensorMode)
	assert.True(t, resolved.ExecutionContextResolved)
	assert.Equal(t, start, *resolved.WinStart)
	assert.Equal(t, end, *resolved.WinEnd)

	legacy, _, _, err := store.Get(ctx, legacyID)
	require.NoError(t, err)
	assert.True(t, legacy.FullRefresh, "legacy claimed rows retain best-known River request metadata")
	assert.Equal(t, "skip", legacy.SensorMode)
	assert.False(t, legacy.ExecutionContextResolved)

	specBacked, _, _, err := store.Get(ctx, specBackedRun.ID)
	require.NoError(t, err)
	assert.True(t, specBacked.FullRefresh, "run-ID-only River arguments must not erase requested spec diagnostics")
	assert.Equal(t, "skip", specBacked.SensorMode)
	assert.False(t, specBacked.ExecutionContextResolved)
}

func TestExecutePublishesCanonicalResolvedContext(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	requestedStart := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	requestedEnd := requestedStart.Add(time.Hour)
	run := PipelineRun{
		ID: "canonical-events", PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid",
		Pipeline: "analytics", Environment: "requested", Trigger: RunTriggerSchedule,
		Status: RunStatusQueued, WinStart: &requestedStart, WinEnd: &requestedEnd,
		SnapshotVersionID: "snapshot-id", FullRefresh: true, SensorMode: "skip",
	}
	spec := scheduledRunSpec(run, pipelineRunJobArgs{
		PipelineUUID:      "pipeline-uuid",
		Environment:       "requested",
		SnapshotVersionID: "snapshot-id",
		FullRefresh:       true,
		SensorMode:        "skip",
	})
	_, err = store.CreateWithSpec(ctx, run, spec)
	require.NoError(t, err)

	resolvedStart := requestedStart.Add(24 * time.Hour)
	resolvedEnd := resolvedStart.Add(30 * time.Minute)
	events := make([]map[string]any, 0)
	service := New(Options{
		Store: store,
		Publish: func(event any) {
			payload, ok := event.(map[string]any)
			require.True(t, ok)
			events = append(events, payload)
		},
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			require.NotNil(t, req.OnContextResolved)
			require.NoError(t, req.OnContextResolved(RunExecutionContext{
				Environment: "effective", WinStart: resolvedStart, WinEnd: resolvedEnd,
				FullRefresh: false, Backfill: true, SensorMode: "once",
			}))
			persisted, _, _, getErr := store.Get(ctx, run.ID)
			require.NoError(t, getErr)
			assert.True(t, persisted.ExecutionContextResolved)
			assert.Equal(t, "effective", persisted.Environment)
			return RunResult{Status: "ok"}
		},
	})

	require.NoError(t, service.execute(ctx, run, spec))

	var startedEvents []PipelineRun
	var finishedEvents []PipelineRun
	for _, event := range events {
		publishedRun, ok := event["run"].(PipelineRun)
		if !ok {
			continue
		}
		switch event["type"] {
		case "run.started":
			startedEvents = append(startedEvents, publishedRun)
		case "run.finished":
			finishedEvents = append(finishedEvents, publishedRun)
		}
	}
	require.Len(t, startedEvents, 1)
	require.Len(t, finishedEvents, 1)
	for _, published := range []PipelineRun{startedEvents[0], finishedEvents[0]} {
		assert.True(t, published.ExecutionContextResolved)
		assert.Equal(t, "effective", published.Environment)
		assert.Equal(t, resolvedStart, *published.WinStart)
		assert.Equal(t, resolvedEnd, *published.WinEnd)
		assert.False(t, published.FullRefresh)
		assert.True(t, published.Backfill)
		assert.Equal(t, "once", published.SensorMode)
	}
	watermark, found, err := store.LastInterval(ctx, "pipeline-uuid|requested")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, requestedEnd, watermark, "schedule progress follows immutable occurrence provenance")
	_, found, err = store.LastInterval(ctx, "pipeline-uuid|effective")
	require.NoError(t, err)
	assert.False(t, found, "effective execution context must not redirect schedule progress")
}

func TestScheduledRunSpecRejectsMismatchedProgressProvenance(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	run := PipelineRun{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerSchedule, WinStart: &start, WinEnd: &end,
		SnapshotVersionID: "snapshot-id",
	}
	newSpec := func() runSpecV1 {
		return scheduledRunSpec(run, pipelineRunJobArgs{
			PipelineUUID: "pipeline-uuid", Environment: "prod", SnapshotVersionID: "snapshot-id",
		})
	}

	tests := map[string]func(*runSpecV1){
		"pipeline UUID": func(spec *runSpecV1) { spec.Schedule.PipelineUUID = "other-uuid" },
		"environment":   func(spec *runSpecV1) { spec.Schedule.Environment = "staging" },
		"interval":      func(spec *runSpecV1) { spec.Requested.Start, spec.Requested.End = nil, nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			spec := newSpec()
			mutate(&spec)
			require.Error(t, spec.validate())
		})
	}
}

func newRunningTestService(t *testing.T, store *Store, runner func(context.Context, RunRequest, func(string)) RunResult) *Service {
	t.Helper()
	service := New(Options{Store: store, StateDir: t.TempDir(), Runner: runner})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, service.Start(ctx))
	t.Cleanup(func() {
		cancel()
		service.Stop()
	})
	return service
}
