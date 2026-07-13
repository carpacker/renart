package scheduler

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecoverOrphanedRunsReplaysPersistedTerminalStepsOnce(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	runID, err := store.Create(ctx, PipelineRun{
		ID: "orphaned-run", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerSchedule, Status: RunStatusQueued,
		SnapshotVersionID: "snapshot-id",
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, runID, started))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.finished", Status: RunStatusSuccess,
		StartedAt: &started, FinishedAt: &finished,
	}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.interrupted", Status: RunStatusRunning,
		StartedAt: &finished,
	}))

	callbackCount := 0
	var recoveredRun PipelineRun
	var recoveredSteps []PipelineRunStep
	service := New(Options{
		Store: store,
		RecoverRun: func(_ context.Context, run PipelineRun, steps []PipelineRunStep) error {
			callbackCount++
			recoveredRun = run
			recoveredSteps = append([]PipelineRunStep(nil), steps...)
			return nil
		},
	})

	summary := service.recoverOrphanedRuns(ctx)
	require.Equal(t, 1, callbackCount)
	assert.Equal(t, 1, summary.ReconciledRuns)
	assert.Zero(t, summary.RiverJobsCancelled)
	assert.Equal(t, 1, summary.ReplayedRuns)
	assert.Zero(t, summary.ReplayFailures)
	assert.Equal(t, RunStatusFailed, recoveredRun.Status)
	assert.Equal(t, orphanedRunError, recoveredRun.Error)
	assert.Equal(t, "snapshot-id", recoveredRun.SnapshotVersionID)
	require.Len(t, recoveredSteps, 2)
	assert.Equal(t, RunStatusSuccess, recoveredSteps[0].Status)
	assert.Equal(t, RunStatusFailed, recoveredSteps[1].Status)
	assert.Equal(t, orphanedRunError, recoveredSteps[1].Error)

	summary = service.recoverOrphanedRuns(ctx)
	assert.Equal(t, 1, callbackCount, "already reconciled runs must not replay twice")
	assert.Zero(t, summary.ReconciledRuns)
	assert.Zero(t, summary.ReplayedRuns)
}

func TestRecoverOrphanedRunsRetriesUnacknowledgedReplay(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	runID, err := store.Create(ctx, PipelineRun{
		ID: "retry-recovery", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerSchedule, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, runID, started))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.interrupted", Status: RunStatusRunning, StartedAt: &started,
	}))

	attempts := 0
	service := New(Options{
		Store: store,
		RecoverRun: func(context.Context, PipelineRun, []PipelineRunStep) error {
			attempts++
			if attempts == 1 {
				return errors.New("temporary replay failure")
			}
			return nil
		},
	})

	summary := service.recoverOrphanedRuns(ctx)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, summary.ReconciledRuns)
	assert.Zero(t, summary.ReplayedRuns)
	assert.Equal(t, 1, summary.ReplayFailures)
	pending, err := store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{runID}, pending)

	summary = service.recoverOrphanedRuns(ctx)
	assert.Equal(t, 2, attempts)
	assert.Zero(t, summary.ReconciledRuns)
	assert.Equal(t, 1, summary.ReplayedRuns)
	assert.Zero(t, summary.ReplayFailures)
	pending, err = store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)

	summary = service.recoverOrphanedRuns(ctx)
	assert.Equal(t, 2, attempts, "an acknowledged replay is not emitted again")
	assert.Zero(t, summary.ReplayedRuns)
}

func TestServiceStartsRiverWorkersOnlyForSchedulerLockOwner(t *testing.T) {
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "state.db")
	ownerStore, err := OpenStore(statePath)
	require.NoError(t, err)
	defer ownerStore.Close()
	followerStore, err := OpenStore(statePath)
	require.NoError(t, err)
	defer followerStore.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := New(Options{
		Store:    ownerStore,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, owner.Start(ctx))
	defer owner.Stop()

	follower := New(Options{
		Store:    followerStore,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, follower.Start(ctx))
	defer follower.Stop()

	assert.True(t, owner.schedulerOn)
	require.NotNil(t, owner.riverClient)
	assert.False(t, follower.schedulerOn)
	assert.Nil(t, follower.riverClient)
	assert.Contains(t, follower.ownerMessage, "will not run jobs")

	_, err = follower.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler is not running")
}

func TestServiceTriggerPersistsRunAndLogs(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	var eventsMu sync.Mutex
	events := []any{}
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Publish: func(event any) {
			eventsMu.Lock()
			defer eventsMu.Unlock()
			events = append(events, event)
		},
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			onLog("running " + req.PipelineID)
			close(done)
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{Trigger: string(RunTriggerManual)})
	require.NoError(t, err)
	require.NotEmpty(t, run.ID)
	require.NotNil(t, run.RiverJobID)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not execute")
	}

	require.Eventually(t, func() bool {
		stored, logs, _, err := service.GetRun(context.Background(), run.ID)
		return err == nil && stored.Status == RunStatusSuccess && len(logs) == 1
	}, 2*time.Second, 20*time.Millisecond)

	stored, logs, _, err := service.GetRun(context.Background(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusSuccess, stored.Status)
	require.NotNil(t, stored.RiverJobID)
	assert.Equal(t, *run.RiverJobID, *stored.RiverJobID)
	assert.Equal(t, "running pipeline-id", logs[0].Line)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	assert.Contains(t, events, map[string]any{
		"type": "run.log",
		"run": map[string]any{
			"run_id": run.ID,
			"log":    logs[0],
		},
	})
}

func TestServicePersistsStructuredRunSteps(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			started := time.Now().UTC()
			req.OnStep(RunStepEvent{Asset: "orders_cleaned", Status: RunStatusRunning, StartedAt: &started})
			finished := started.Add(150 * time.Millisecond)
			req.OnStep(RunStepEvent{Asset: "orders_cleaned", Status: RunStatusSuccess, StartedAt: &started, FinishedAt: &finished})
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{Trigger: string(RunTriggerManual)})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, _, steps, err := service.GetRun(context.Background(), run.ID)
		return err == nil && len(steps) == 1 && steps[0].Status == RunStatusSuccess
	}, 2*time.Second, 20*time.Millisecond)
}

func TestServiceTriggerRejectsActiveRun(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	release := make(chan struct{})
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			<-release
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	defer close(release)

	pipeline := PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}
	_, err = service.Trigger(ctx, pipeline, TriggerRequest{Trigger: string(RunTriggerManual)})
	require.NoError(t, err)
	_, err = service.Trigger(ctx, pipeline, TriggerRequest{Trigger: string(RunTriggerManual)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already has a queued or running run")
}

func TestServiceListSchedulesAppliesLocalEnabledState(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	service := New(Options{
		Store: store,
		Pipelines: func(context.Context) ([]PipelineSchedule, error) {
			return []PipelineSchedule{{PipelineID: "pipeline-id", PipelineName: "analytics", Schedule: "@hourly", Timezone: "UTC", Enabled: true}}, nil
		},
	})

	items, err := service.ListSchedules(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.True(t, items[0].Enabled)
	require.NotNil(t, items[0].NextRunAt)

	require.NoError(t, store.SetScheduleEnabled(ctx, "pipeline-id", false))
	items, err = service.ListSchedules(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.False(t, items[0].Enabled)
	assert.Nil(t, items[0].NextRunAt)
}

func TestScheduledWorkerCreatesRunAndWatermark(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			start, err := time.Parse(time.RFC3339, req.Start)
			require.NoError(t, err)
			end, err := time.Parse(time.RFC3339, req.End)
			require.NoError(t, err)
			require.True(t, start.Before(end), "scheduled run start must be before end")
			onLog("scheduled " + req.PipelineID)
			return RunResult{Status: "ok"}
		},
	})
	worker := &pipelineRunWorker{service: service}
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{Args: pipelineRunJobArgs{PipelineID: "pipeline-id", PipelineName: "analytics", Trigger: RunTriggerSchedule, Schedule: "@hourly", Timezone: "UTC"}}))

	result, err := service.ListRuns(context.Background(), RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, err)
	runs := result.Runs
	require.Len(t, runs, 1)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, RunStatusSuccess, runs[0].Status)
	assert.Equal(t, RunTriggerSchedule, runs[0].Trigger)
	require.NotNil(t, runs[0].WinStart)
	require.NotNil(t, runs[0].WinEnd)
	assert.True(t, runs[0].WinStart.Before(*runs[0].WinEnd))

	_, logs, _, err := service.GetRun(context.Background(), runs[0].ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "scheduled pipeline-id", logs[0].Line)

	watermark, ok, err := store.LastInterval(context.Background(), "pipeline-id")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, *runs[0].WinEnd, watermark)
}

func TestPreviousScheduleIntervalForMinuteCron(t *testing.T) {
	schedule, err := parseSchedule("0,5,10,20,30,40,50 * * * *", "UTC")
	require.NoError(t, err)

	now := time.Date(2026, 1, 1, 10, 5, 0, 123456789, time.UTC)
	start, end, ok := previousScheduleInterval(schedule, now)
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC), end)
	assert.True(t, start.Before(end))
}

func TestEnsureScheduledRunWindowRepairsExistingEqualWindow(t *testing.T) {
	end := time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)
	start := end
	run := ensureScheduledRunWindow(PipelineRun{Trigger: RunTriggerSchedule, WinStart: &start, WinEnd: &end})
	require.NotNil(t, run.WinStart)
	assert.True(t, run.WinStart.Before(*run.WinEnd))
}
