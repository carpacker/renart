package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
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
		SnapshotVersionID: "snapshot-id", FullRefresh: true, SensorMode: "wait",
		WinStart: &started, WinEnd: &finished, ExecutionContextResolved: true,
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

	summary, err := service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, callbackCount)
	assert.Equal(t, 1, summary.ReconciledRuns)
	assert.Zero(t, summary.RiverJobsCancelled)
	assert.Equal(t, 1, summary.ReplayedRuns)
	assert.Zero(t, summary.ReplayFailures)
	assert.Equal(t, RunStatusFailed, recoveredRun.Status)
	assert.Equal(t, orphanedRunError, recoveredRun.Error)
	assert.Equal(t, "snapshot-id", recoveredRun.SnapshotVersionID)
	assert.True(t, recoveredRun.FullRefresh)
	assert.False(t, recoveredRun.Backfill)
	assert.Equal(t, "wait", recoveredRun.SensorMode)
	assert.True(t, recoveredRun.ExecutionContextResolved)
	require.Len(t, recoveredSteps, 2)
	assert.Equal(t, RunStatusSuccess, recoveredSteps[0].Status)
	assert.Equal(t, RunStatusFailed, recoveredSteps[1].Status)
	assert.Equal(t, orphanedRunError, recoveredSteps[1].Error)

	summary, err = service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, callbackCount, "already reconciled runs must not replay twice")
	assert.Zero(t, summary.ReconciledRuns)
	assert.Zero(t, summary.ReplayedRuns)
}

func TestRecoverOrphanedRunsSkipsUnresolvedLegacyContext(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	runID, err := store.Create(ctx, PipelineRun{
		ID: "legacy-unresolved", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
		WinStart: &started, WinEnd: &finished, FullRefresh: true, SensorMode: "skip",
	})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, runID, started))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: runID, Asset: "analytics.finished", Status: RunStatusSuccess,
		StartedAt: &started, FinishedAt: &finished,
	}))

	callbackCount := 0
	service := New(Options{
		Store: store,
		RecoverRun: func(context.Context, PipelineRun, []PipelineRunStep) error {
			callbackCount++
			return nil
		},
	})

	summary, err := service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Zero(t, callbackCount, "request-only legacy context must never produce materialization facts")
	assert.Equal(t, 1, summary.ReconciledRuns)
	assert.Zero(t, summary.ReplayedRuns)
	assert.Equal(t, 1, summary.SkippedRunReplays)
	assert.Zero(t, summary.ReplayFailures)
	pending, err := store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending, "a deliberately skipped replay must be acknowledged")

	summary, err = service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Zero(t, callbackCount)
	assert.Zero(t, summary.SkippedRunReplays, "an acknowledged skip must not repeat")
}

func TestRecoverOrphanedRunsRetriesUnacknowledgedReplay(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	finished := started.Add(time.Minute)
	runID, err := store.Create(ctx, PipelineRun{
		ID: "retry-recovery", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerSchedule, Status: RunStatusQueued,
		WinStart: &started, WinEnd: &finished, SensorMode: "once",
		ExecutionContextResolved: true,
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

	summary, err := service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
	assert.Equal(t, 1, summary.ReconciledRuns)
	assert.Zero(t, summary.ReplayedRuns)
	assert.Equal(t, 1, summary.ReplayFailures)
	pending, err := store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{runID}, pending)

	summary, err = service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
	assert.Zero(t, summary.ReconciledRuns)
	assert.Equal(t, 1, summary.ReplayedRuns)
	assert.Zero(t, summary.ReplayFailures)
	pending, err = store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)

	summary, err = service.recoverOrphanedRuns(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, attempts, "an acknowledged replay is not emitted again")
	assert.Zero(t, summary.ReplayedRuns)
}

func TestServiceStartFailsBeforeWorkersWhenRecoveryStateCannotBeRead(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()
	_, err = store.db.Exec(`PRAGMA foreign_keys = OFF`)
	require.NoError(t, err)
	_, err = store.db.Exec(`DROP TABLE pipeline_runs`)
	require.NoError(t, err)

	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
	})
	err = service.Start(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "scheduler startup recovery failed")
	assert.ErrorContains(t, err, "pipeline_runs")
	assert.False(t, service.schedulerOn)
	assert.Nil(t, service.riverClient)
	assert.Equal(t, SchedulerOwnershipUnavailable, service.Ownership().State)
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

	var followerDeployCalls atomic.Int32
	follower := New(Options{
		Store:    followerStore,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
		DeployPipeline: func(context.Context, string) (string, error) {
			followerDeployCalls.Add(1)
			return "should-not-exist", nil
		},
		ValidateSnapshot: func(context.Context, string, string) error {
			return nil
		},
	})
	require.NoError(t, follower.Start(ctx))
	defer follower.Stop()

	assert.True(t, owner.schedulerOn)
	require.NotNil(t, owner.riverClient)
	assert.Equal(t, SchedulerOwnership{State: SchedulerOwnershipOwner}, owner.Ownership())
	assert.False(t, follower.schedulerOn)
	assert.Nil(t, follower.riverClient)
	assert.Equal(t, SchedulerOwnershipFollower, follower.Ownership().State)
	assert.Contains(t, follower.Ownership().Message, "read-only")

	_, err = follower.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSchedulerNotOwner)

	_, err = follower.UpsertEnvSchedule(ctx, "pipeline-uuid", UpsertEnvScheduleRequest{
		Environment: "prod",
		Cron:        "@daily",
		DeployNow:   true,
	})
	require.ErrorIs(t, err, ErrSchedulerNotOwner)
	assert.Zero(t, followerDeployCalls.Load(), "a follower must reject before deploying")

	require.NoError(t, ownerStore.UpsertEnvSchedule(ctx, EnvSchedule{
		PipelineUUID:      "pipeline-uuid",
		Environment:       "prod",
		SnapshotVersionID: "snapshot-id",
		Cron:              "@daily",
		Timezone:          "UTC",
		CatchupPolicy:     CatchupSkip,
		Status:            ScheduleStatusPaused,
	}))
	require.ErrorIs(t, follower.SetEnvScheduleLifecycle(ctx, "pipeline-uuid", "prod", ScheduleStatusActive), ErrSchedulerNotOwner)
	require.ErrorIs(t, follower.ArchiveEnvSchedule(ctx, "pipeline-uuid", "prod"), ErrSchedulerNotOwner)
	unchanged, found, err := ownerStore.GetEnvSchedule(ctx, "pipeline-uuid", "prod")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleStatusPaused, unchanged.Status)
	assert.Empty(t, unchanged.ArchivedReason)
}

func TestServiceTriggerPersistsRunAndLogs(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	var capturedRequest RunRequest
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
			capturedRequest = req
			onLog("running " + req.PipelineID)
			close(done)
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics"}, TriggerRequest{
		Source:            RunSourceSnapshot,
		SnapshotVersionID: " snapshot-7 ",
		LegacyTrigger:     string(RunTriggerSchedule),
	})
	require.NoError(t, err)
	require.NotEmpty(t, run.ID)
	require.NotNil(t, run.RiverJobID)
	assert.Equal(t, RunTriggerManual, run.Trigger)
	assert.Equal(t, "snapshot-7", run.SnapshotVersionID)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not execute")
	}
	assert.False(t, capturedRequest.Scheduled)
	assert.Equal(t, "pipeline-uuid", capturedRequest.PipelineUUID)
	assert.Equal(t, "snapshot-7", capturedRequest.SnapshotVersionID)

	require.Eventually(t, func() bool {
		stored, logs, _, err := service.GetRun(context.Background(), run.ID)
		return err == nil && stored.Status == RunStatusSuccess && len(logs) == 1
	}, 2*time.Second, 20*time.Millisecond)

	stored, logs, _, err := service.GetRun(context.Background(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusSuccess, stored.Status)
	assert.Equal(t, "snapshot-7", stored.SnapshotVersionID)
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

	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{})
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
	active, err := service.Trigger(ctx, pipeline, TriggerRequest{})
	require.NoError(t, err)
	_, err = service.Trigger(ctx, pipeline, TriggerRequest{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPipelineRunActive)
	var conflict *PipelineRunActiveError
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, active.ID, conflict.ActiveRunID)
}

func TestTriggerAdmissionPersistsSpecAndRunIDOnlyRiverJob(t *testing.T) {
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
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			<-release
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	defer close(release)

	start := "2026-07-16T08:00:00.123456789Z"
	end := "2026-07-16T09:00:00.123456789Z"
	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{
		Environment:          "prod",
		Start:                start,
		End:                  end,
		Source:               RunSourceSnapshot,
		SnapshotVersionID:    "snapshot-id",
		FullRefresh:          true,
		ConfirmedEnvironment: "prod",
		SensorMode:           "skip",
	})
	require.NoError(t, err)
	require.NotNil(t, run.RiverJobID)

	spec, found, err := store.GetRunSpec(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, runSpecVersionV1, spec.Version)
	assert.Equal(t, RunSourceSnapshot, spec.Source.Kind)
	assert.Equal(t, "snapshot-id", spec.Source.SnapshotVersionID)
	assert.Equal(t, "prod", spec.Requested.Environment)
	assert.True(t, spec.Requested.FullRefresh)
	assert.Equal(t, "skip", spec.Requested.SensorMode)
	assert.Equal(t, "prod", spec.Authorization.ConfirmedEnvironment)
	assert.Nil(t, spec.Schedule)

	var argsJSON string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT json(args) FROM river_job WHERE id = ?`, *run.RiverJobID).Scan(&argsJSON))
	assert.JSONEq(t, fmt.Sprintf(`{"run_id":%q}`, run.ID), argsJSON)
}

func TestConcurrentTriggerAdmissionCreatesOneRunSpecSlotAndRiverJob(t *testing.T) {
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
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			<-release
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	defer close(release)

	pipeline := PipelineSchedule{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
	}
	start := make(chan struct{})
	type triggerResult struct {
		run PipelineRun
		err error
	}
	results := make(chan triggerResult, 2)
	for range 2 {
		go func() {
			<-start
			run, triggerErr := service.Trigger(ctx, pipeline, TriggerRequest{})
			results <- triggerResult{run: run, err: triggerErr}
		}()
	}
	close(start)
	first, second := <-results, <-results
	accepted, rejected := first, second
	if accepted.err != nil {
		accepted, rejected = second, first
	}
	require.NoError(t, accepted.err)
	require.ErrorIs(t, rejected.err, ErrPipelineRunActive)
	var conflict *PipelineRunActiveError
	require.ErrorAs(t, rejected.err, &conflict)
	assert.Equal(t, accepted.run.ID, conflict.ActiveRunID)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE pipeline_id = 'pipeline-id'`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_specs WHERE run_id = ?`, accepted.run.ID))
	assert.Equal(t, 2, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, accepted.run.ID))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM river_job WHERE kind = ?`, pipelineRunJobKind))
}

func TestTriggerAdmissionRollsBackRunSpecAndJobWhenRiverInsertFails(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var events []any
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Publish: func(event any) {
			events = append(events, event)
		},
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	_, err = store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_pipeline_job_admission
		BEFORE INSERT ON river_job
		WHEN NEW.kind = 'renart-pipeline-run'
		BEGIN
			SELECT RAISE(ABORT, 'injected River admission failure');
		END`)
	require.NoError(t, err)

	_, err = service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{Source: RunSourceWorkingTree})
	require.ErrorContains(t, err, "injected River admission failure")
	result, err := service.ListRuns(ctx, RunFilter{})
	require.NoError(t, err)
	assert.Zero(t, result.Total)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_specs`))
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM river_job WHERE kind = ?`, pipelineRunJobKind))
	for _, event := range events {
		payload, ok := event.(map[string]any)
		if ok {
			assert.NotEqual(t, "run.queued", payload["type"])
		}
	}
}

func TestPrepareRunUsesStoredSpecAndIgnoresConflictingLegacyArguments(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	start := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	run := PipelineRun{
		ID: "stored-spec", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
		WinStart: &start, WinEnd: &end, SnapshotVersionID: "snapshot-id",
		FullRefresh: true, SensorMode: "skip",
	}
	spec := manualRunSpec(run, RunSourceSnapshot, "prod")
	_, err = store.CreateWithSpec(context.Background(), run, spec)
	require.NoError(t, err)

	service := New(Options{Store: store})
	prepared, preparedSpec, ok, err := service.prepareRun(context.Background(), 91, pipelineRunJobArgs{
		RunID:                run.ID,
		SnapshotVersionID:    "wrong-snapshot",
		FullRefresh:          false,
		Backfill:             true,
		ConfirmedEnvironment: "wrong-environment",
		SensorMode:           "wait",
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, spec, preparedSpec)
	assert.Equal(t, "snapshot-id", prepared.SnapshotVersionID)
	assert.True(t, prepared.FullRefresh)
	assert.False(t, prepared.Backfill)
	assert.Equal(t, "skip", prepared.SensorMode)
}

func TestPrepareRunRejectsStoredSpecWhoseStableUUIDWasRewrittenConsistently(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	start := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	run := PipelineRun{
		ID: "stable-identity", PipelineID: "old-path", PipelineUUID: "stable-uuid", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerSchedule, Status: RunStatusQueued,
		WinStart: &start, WinEnd: &end, SnapshotVersionID: "snapshot-id",
	}
	spec := scheduledRunSpec(run, pipelineRunJobArgs{
		PipelineUUID: "stable-uuid", Environment: "prod", Schedule: "@hourly", Timezone: "UTC",
	})
	_, err = store.CreateWithSpec(ctx, run, spec)
	require.NoError(t, err)

	// Rewriting both JSON UUID copies still satisfies the RunSpec's internal
	// equality check. The independently admitted UUID slot must reject it.
	_, err = store.db.ExecContext(ctx, `
		UPDATE pipeline_run_specs
		SET body = json_set(body,
			'$.pipeline.uuid', 'rewritten-uuid',
			'$.schedule.pipeline_uuid', 'rewritten-uuid')
		WHERE run_id = ?`, run.ID)
	require.NoError(t, err)

	service := New(Options{Store: store})
	_, _, ok, err := service.prepareRun(ctx, 93, pipelineRunJobArgs{RunID: run.ID})
	require.ErrorIs(t, err, ErrInvalidStoredSpec)
	assert.ErrorContains(t, err, "stable pipeline UUID does not match active run slot")
	assert.False(t, ok)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE slot_key = 'uuid:stable-uuid' AND run_id = ?`, run.ID))
}

func TestPrepareRunStrictlyUpgradesLegacyJobOrRejectsUnknownSpecVersion(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()

	legacy := PipelineRun{
		ID: "legacy-run", PipelineID: "legacy-pipeline", Pipeline: "legacy",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
	}
	_, err = store.Create(ctx, legacy)
	require.NoError(t, err)
	service := New(Options{Store: store})
	_, upgraded, ok, err := service.prepareRun(ctx, 92, pipelineRunJobArgs{
		RunID:                legacy.ID,
		FullRefresh:          true,
		ConfirmedEnvironment: "prod",
		SensorMode:           "skip",
	})
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, upgraded.Requested.FullRefresh)
	assert.Equal(t, "prod", upgraded.Authorization.ConfirmedEnvironment)
	assert.Equal(t, "skip", upgraded.Requested.SensorMode)
	_, found, err := store.GetRunSpec(ctx, legacy.ID)
	require.NoError(t, err)
	assert.True(t, found)

	_, err = store.db.ExecContext(ctx, `UPDATE pipeline_run_specs SET version = 99, body = json_set(body, '$.version', 99) WHERE run_id = ?`, legacy.ID)
	require.NoError(t, err)
	_, _, ok, err = service.prepareRun(ctx, 92, pipelineRunJobArgs{RunID: legacy.ID, FullRefresh: false})
	require.ErrorContains(t, err, "unsupported run spec version 99")
	assert.False(t, ok, "unknown stored versions must never fall back to River arguments")

	worker := &pipelineRunWorker{service: service}
	err = worker.Work(ctx, &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: 92},
		Args:   pipelineRunJobArgs{RunID: legacy.ID, FullRefresh: false},
	})
	require.Error(t, err)
	failed, _, _, getErr := store.Get(ctx, legacy.ID)
	require.NoError(t, getErr)
	assert.Equal(t, RunStatusFailed, failed.Status)
	assert.Contains(t, failed.Error, "unsupported run spec version 99")
}

func TestScheduledWorkerSnoozesOriginalJobWhilePipelineSlotIsOccupied(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	blockingID, err := store.Create(ctx, PipelineRun{
		ID: "blocking-run", PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusRunning,
	})
	require.NoError(t, err)

	var runnerCalls atomic.Int32
	service := New(Options{
		Store: store,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			runnerCalls.Add(1)
			assert.Equal(t, "2026-07-16T08:00:00Z", req.Start)
			assert.Equal(t, "2026-07-16T09:00:00Z", req.End)
			return RunResult{Status: "ok"}
		},
	})
	worker := &pipelineRunWorker{service: service}
	job := &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: 321},
		Args: pipelineRunJobArgs{
			PipelineUUID:      "pipeline-uuid",
			Environment:       "prod",
			Start:             "2026-07-16T08:00:00Z",
			End:               "2026-07-16T09:00:00Z",
			SnapshotVersionID: "snapshot-id",
		}}
	err = worker.Work(ctx, job)
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, err, &snooze)
	assert.Equal(t, runSpecRetrySnoozeTime, snooze.Duration)
	assert.Zero(t, runnerCalls.Load())
	require.NoError(t, store.Finish(ctx, blockingID, RunStatusSuccess, nil))

	require.NoError(t, worker.Work(ctx, job))
	require.NoError(t, worker.Work(ctx, job), "a duplicate delivery of the same linked job must be a no-op")
	assert.EqualValues(t, 1, runnerCalls.Load())
	runs, listErr := store.List(ctx, RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, listErr)
	require.Equal(t, 2, runs.Total)
	var scheduled PipelineRun
	for _, run := range runs.Runs {
		if run.Trigger == RunTriggerSchedule {
			scheduled = run
		}
	}
	require.NotEmpty(t, scheduled.ID)
	require.NotNil(t, scheduled.RiverJobID)
	assert.EqualValues(t, 321, *scheduled.RiverJobID)
	assert.Equal(t, RunStatusSuccess, scheduled.Status)
	assert.Equal(t, "2026-07-16T08:00:00Z", scheduled.WinStart.Format(time.RFC3339))
	assert.Equal(t, "2026-07-16T09:00:00Z", scheduled.WinEnd.Format(time.RFC3339))
	spec, found, specErr := store.GetRunSpec(ctx, scheduled.ID)
	require.NoError(t, specErr)
	require.True(t, found)
	assert.Equal(t, "2026-07-16T08:00:00Z", spec.Requested.Start.Format(time.RFC3339))
	assert.Equal(t, "2026-07-16T09:00:00Z", spec.Requested.End.Format(time.RFC3339))
	watermark, found, watermarkErr := store.LastInterval(ctx, "pipeline-uuid|prod")
	require.NoError(t, watermarkErr)
	require.True(t, found)
	assert.Equal(t, "2026-07-16T09:00:00Z", watermark.Format(time.RFC3339))
}

func TestWorkerSnoozesWhenQueuedRunCannotPersistStart(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	riverJobID := int64(654)
	run := PipelineRun{
		ID: "retry-start", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued, RiverJobID: &riverJobID,
	}
	_, err = store.CreateWithSpec(ctx, run, manualRunSpec(run, RunSourceWorkingTree, ""))
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
		CREATE TRIGGER reject_run_start
		BEFORE UPDATE OF status ON pipeline_runs
		WHEN NEW.id = 'retry-start' AND NEW.status = 'running'
		BEGIN
			SELECT RAISE(ABORT, 'injected start persistence failure');
		END`)
	require.NoError(t, err)
	var runnerCalls atomic.Int32
	worker := &pipelineRunWorker{service: New(Options{
		Store: store,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			runnerCalls.Add(1)
			return RunResult{Status: "ok"}
		},
	})}
	job := &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: riverJobID},
		Args:   pipelineRunJobArgs{RunID: run.ID},
	}
	err = worker.Work(ctx, job)
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, err, &snooze)
	assert.Equal(t, runSpecRetrySnoozeTime, snooze.Duration)
	assert.Zero(t, runnerCalls.Load())
	queued, _, _, getErr := store.Get(ctx, run.ID)
	require.NoError(t, getErr)
	assert.Equal(t, RunStatusQueued, queued.Status)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, run.ID))

	_, err = store.db.ExecContext(ctx, `DROP TRIGGER reject_run_start`)
	require.NoError(t, err)
	require.NoError(t, worker.Work(ctx, job))
	assert.EqualValues(t, 1, runnerCalls.Load())
	finished, _, _, getErr := store.Get(ctx, run.ID)
	require.NoError(t, getErr)
	assert.Equal(t, RunStatusSuccess, finished.Status)
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, run.ID))
}

func TestParseRequestWindowStrictlyValidatesBothBounds(t *testing.T) {
	t.Parallel()

	start, end, err := parseRequestWindow("2026-07-16T08:00:00.123456789Z", "2026-07-16T09:00:00Z")
	require.NoError(t, err)
	require.NotNil(t, start)
	require.NotNil(t, end)
	assert.True(t, start.Before(*end))

	start, end, err = parseRequestWindow("", "")
	require.NoError(t, err)
	assert.Nil(t, start)
	assert.Nil(t, end)

	tests := []struct {
		name  string
		start string
		end   string
	}{
		{name: "invalid start", start: "yesterday", end: "2026-07-16T09:00:00Z"},
		{name: "invalid end", start: "2026-07-16T08:00:00Z", end: "later"},
		{name: "missing end", start: "2026-07-16T08:00:00Z"},
		{name: "missing start", end: "2026-07-16T09:00:00Z"},
		{name: "equal bounds", start: "2026-07-16T08:00:00Z", end: "2026-07-16T08:00:00Z"},
		{name: "reversed bounds", start: "2026-07-16T09:00:00Z", end: "2026-07-16T08:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := parseRequestWindow(tt.start, tt.end)
			require.Error(t, err)
		})
	}
}

func TestServiceTriggerRejectsInvalidWindowWithoutCreatingRun(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runnerCalls atomic.Int32
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			runnerCalls.Add(1)
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	pipeline := PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}
	invalid := []TriggerRequest{
		{Start: "not-a-time", End: "2026-07-16T09:00:00Z"},
		{Start: "2026-07-16T08:00:00Z"},
		{Start: "2026-07-16T09:00:00Z", End: "2026-07-16T08:00:00Z"},
	}
	for _, req := range invalid {
		_, err := service.Trigger(ctx, pipeline, req)
		require.Error(t, err)
	}

	runs, err := service.ListRuns(ctx, RunFilter{})
	require.NoError(t, err)
	assert.Empty(t, runs.Runs)
	assert.Zero(t, runnerCalls.Load())
}

func TestServiceTriggerRejectsInvalidSourceWithoutCreatingRun(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runnerCalls atomic.Int32
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			runnerCalls.Add(1)
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	pipeline := PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}
	tests := []struct {
		name string
		req  TriggerRequest
		want string
	}{
		{
			name: "unknown source",
			req:  TriggerRequest{Source: RunSource("deployment")},
			want: "invalid run source",
		},
		{
			name: "source is not whitespace-normalized",
			req:  TriggerRequest{Source: RunSource(" snapshot "), SnapshotVersionID: "snapshot-7"},
			want: "invalid run source",
		},
		{
			name: "snapshot source requires exact pin",
			req:  TriggerRequest{Source: RunSourceSnapshot},
			want: "snapshot_version_id is required",
		},
		{
			name: "working tree rejects pin",
			req:  TriggerRequest{Source: RunSourceWorkingTree, SnapshotVersionID: "snapshot-7"},
			want: "snapshot_version_id must be empty",
		},
		{
			name: "omitted source remains working tree",
			req:  TriggerRequest{SnapshotVersionID: "snapshot-7"},
			want: "snapshot_version_id must be empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Trigger(ctx, pipeline, tt.req)
			require.ErrorContains(t, err, tt.want)
		})
	}

	runs, err := service.ListRuns(ctx, RunFilter{})
	require.NoError(t, err)
	assert.Empty(t, runs.Runs)
	assert.Zero(t, runnerCalls.Load())
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
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		Runner: func(ctx context.Context, req RunRequest, onLog func(string)) RunResult {
			require.True(t, req.Scheduled)
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
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: 777},
		Args: pipelineRunJobArgs{
			PipelineID:        "pipeline-id",
			PipelineUUID:      "pipeline-uuid",
			PipelineName:      "analytics",
			Environment:       "prod",
			Trigger:           RunTriggerSchedule,
			Schedule:          "@hourly",
			Timezone:          "UTC",
			SnapshotVersionID: "snapshot-id",
		}}))

	result, err := service.ListRuns(context.Background(), RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, err)
	runs := result.Runs
	require.Len(t, runs, 1)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, RunStatusSuccess, runs[0].Status)
	assert.Equal(t, RunTriggerSchedule, runs[0].Trigger)
	require.NotNil(t, runs[0].RiverJobID)
	assert.EqualValues(t, 777, *runs[0].RiverJobID)
	require.NotNil(t, runs[0].WinStart)
	require.NotNil(t, runs[0].WinEnd)
	assert.True(t, runs[0].WinStart.Before(*runs[0].WinEnd))

	_, logs, _, err := service.GetRun(context.Background(), runs[0].ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "scheduled pipeline-id", logs[0].Line)

	watermark, ok, err := store.LastInterval(context.Background(), "pipeline-uuid|prod")
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
