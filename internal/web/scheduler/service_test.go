package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

func TestRecoverOrphanedRunsPreservesResolvedContextWhenRunSpecHasRequestedContext(t *testing.T) {
	requestedStart := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	requestedEnd := requestedStart.Add(time.Hour)

	tests := []struct {
		name             string
		scheduled        bool
		mutateRequested  func(*PipelineRun)
		mutateEffective  func(*RunExecutionContext)
		assertDifference func(*testing.T, PipelineRun, RunExecutionContext)
	}{
		{
			name: "default environment",
			mutateRequested: func(run *PipelineRun) {
				run.Environment = ""
			},
			assertDifference: func(t *testing.T, requested PipelineRun, effective RunExecutionContext) {
				assert.Empty(t, requested.Environment)
				assert.Equal(t, "prod", effective.Environment)
			},
		},
		{
			name: "default window",
			mutateRequested: func(run *PipelineRun) {
				run.WinStart = nil
				run.WinEnd = nil
			},
			assertDifference: func(t *testing.T, requested PipelineRun, effective RunExecutionContext) {
				assert.Nil(t, requested.WinStart)
				assert.Nil(t, requested.WinEnd)
				assert.False(t, effective.WinStart.IsZero())
				assert.False(t, effective.WinEnd.IsZero())
			},
		},
		{
			name:      "scheduled restricted full refresh",
			scheduled: true,
			mutateRequested: func(run *PipelineRun) {
				run.FullRefresh = true
			},
			mutateEffective: func(context *RunExecutionContext) {
				context.FullRefresh = false
			},
			assertDifference: func(t *testing.T, requested PipelineRun, effective RunExecutionContext) {
				assert.True(t, requested.FullRefresh)
				assert.False(t, effective.FullRefresh)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
			require.NoError(t, err)
			defer store.Close()

			ctx := context.Background()
			start := requestedStart
			end := requestedEnd
			requested := PipelineRun{
				ID:           "recovered-context",
				PipelineID:   "pipeline-id",
				PipelineUUID: "pipeline-uuid",
				Pipeline:     "analytics",
				Environment:  "prod",
				Trigger:      RunTriggerManual,
				Status:       RunStatusQueued,
				WinStart:     &start,
				WinEnd:       &end,
				SensorMode:   "once",
			}
			if test.scheduled {
				requested.Trigger = RunTriggerSchedule
				requested.SnapshotVersionID = "snapshot-id"
			}
			if test.mutateRequested != nil {
				test.mutateRequested(&requested)
			}

			var spec runSpecV1
			if test.scheduled {
				spec = scheduledRunSpec(requested, pipelineRunJobArgs{
					PipelineUUID:      requested.PipelineUUID,
					Environment:       requested.Environment,
					SnapshotVersionID: requested.SnapshotVersionID,
					FullRefresh:       requested.FullRefresh,
					SensorMode:        requested.SensorMode,
				})
			} else {
				spec = manualRunSpec(requested, RunSourceWorkingTree, "")
			}
			_, err = store.CreateWithSpec(ctx, requested, spec)
			require.NoError(t, err)
			jobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: requested.ID})
			require.NoError(t, store.SetRunRiverJob(ctx, requested.ID, jobID))
			require.NoError(t, store.MarkRunning(ctx, requested.ID, requestedStart))

			effective := RunExecutionContext{
				Environment: "prod",
				WinStart:    requestedStart,
				WinEnd:      requestedEnd,
				FullRefresh: requested.FullRefresh,
				Backfill:    requested.Backfill,
				SensorMode:  requested.SensorMode,
			}
			if test.mutateEffective != nil {
				test.mutateEffective(&effective)
			}
			require.NoError(t, store.SetRunExecutionContext(ctx, requested.ID, effective))
			require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
				RunID: requested.ID, Asset: "analytics.finished", Status: RunStatusSuccess,
				StartedAt: &requestedStart, FinishedAt: &requestedEnd,
			}))
			markTestRiverJobRunning(t, store, jobID)

			var recovered PipelineRun
			service := New(Options{
				Store: store,
				RecoverRun: func(_ context.Context, run PipelineRun, _ []PipelineRunStep) error {
					recovered = run
					return nil
				},
			})
			summary, err := service.recoverOrphanedRuns(ctx)
			require.NoError(t, err)
			assert.Equal(t, 1, summary.ReconciledRuns)
			assert.Equal(t, 1, summary.ReplayedRuns)
			require.True(t, recovered.ExecutionContextResolved)
			assert.Equal(t, "pipeline-uuid", recovered.PipelineUUID, "recovery restores only the private stable identity")
			assert.Equal(t, effective.Environment, recovered.Environment)
			require.NotNil(t, recovered.WinStart)
			require.NotNil(t, recovered.WinEnd)
			assert.True(t, effective.WinStart.Equal(*recovered.WinStart))
			assert.True(t, effective.WinEnd.Equal(*recovered.WinEnd))
			assert.Equal(t, effective.FullRefresh, recovered.FullRefresh)
			assert.Equal(t, effective.Backfill, recovered.Backfill)
			assert.Equal(t, effective.SensorMode, recovered.SensorMode)
			test.assertDifference(t, requested, effective)
		})
	}
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
			require.NotNil(t, req.OnTargetsResolved)
			require.NoError(t, req.OnTargetsResolved(testExecutionTargetSnapshot()))
			started := time.Now().UTC()
			require.NoError(t, req.OnStep(RunStepEvent{
				Asset: "orders_cleaned", Status: RunStatusRunning, StartedAt: &started,
				UpstreamWriters: testUpstreamWriterSnapshot(), HasUpstreamWriterSnapshot: true,
			}))
			finished := started.Add(150 * time.Millisecond)
			ordinal := int64(0)
			require.NoError(t, req.OnStep(RunStepEvent{
				Asset: "orders_cleaned", Status: RunStatusSuccess,
				StartedAt: &started, FinishedAt: &finished, CompletionOrdinal: &ordinal,
			}))
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		stored, _, steps, err := service.GetRun(context.Background(), run.ID)
		return err == nil && stored.ExecutionTargetSnapshot != nil &&
			len(steps) == 1 && steps[0].Status == RunStatusSuccess &&
			steps[0].CompletionOrdinal != nil && *steps[0].CompletionOrdinal == 0 &&
			steps[0].HasUpstreamWriterSnapshot &&
			len(steps[0].UpstreamWriters) == 1
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
	executionTime := "2026-07-16T08:30:00.123456789Z"
	confirmedPlan := validPipelineRunPlan(t)
	confirmedPlan.ExecutionTime = executionTime
	confirmedPlan.SourceMerkle = strings.Repeat("a", 64)
	confirmedPlan.ConfigurationDigest = strings.Repeat("b", 64)
	confirmedPlan.Artifact = pipelineRunPlanArtifact(t, confirmedPlan)
	run, err := service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{
		Environment:                 "prod",
		Start:                       start,
		End:                         end,
		Source:                      RunSourceSnapshot,
		SnapshotVersionID:           "snapshot-id",
		FullRefresh:                 true,
		ConfirmedEnvironment:        "prod",
		SensorMode:                  "skip",
		ExecutionTime:               executionTime,
		ExpectedSourceMerkle:        strings.Repeat("a", 64),
		ExpectedConfigurationDigest: strings.Repeat("b", 64),
		ConfirmedPlan:               &confirmedPlan,
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
	require.NotNil(t, spec.Requested.ExecutionTime)
	assert.Equal(t, executionTime, spec.Requested.ExecutionTime.Format(time.RFC3339Nano))
	require.NotNil(t, spec.Expected)
	assert.Equal(t, strings.Repeat("a", 64), spec.Expected.SourceMerkle)
	assert.Equal(t, strings.Repeat("b", 64), spec.Expected.ConfigurationDigest)
	assert.Nil(t, spec.Schedule)
	persistedPlan, found, err := store.GetRunPlan(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, confirmedPlan, persistedPlan)
	assert.Equal(t, len(confirmedPlan.ExecutionUnits), countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_units WHERE run_id = ?`, run.ID))

	var argsJSON string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT json(args) FROM river_job WHERE id = ?`, *run.RiverJobID).Scan(&argsJSON))
	assert.JSONEq(t, fmt.Sprintf(`{"run_id":%q}`, run.ID), argsJSON)
}

func TestConfirmedPlanRunPersistsUnitProgressBeforeSuccess(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	confirmedPlan := validPipelineRunPlan(t)
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			require.NotNil(t, req.ConfirmedPlan)
			require.Equal(t, confirmedPlan, *req.ConfirmedPlan)
			require.NotNil(t, req.OnUnit)
			started := time.Now().UTC()
			require.NoError(t, req.OnUnit(PipelineRunUnitEvent{
				Position: 0, Status: PipelineRunUnitRunning, StartedAt: &started,
			}))
			finished := started.Add(time.Second)
			require.NoError(t, req.OnUnit(PipelineRunUnitEvent{
				Position: 0, Status: PipelineRunUnitSuccess, StartedAt: &started, FinishedAt: &finished,
			}))
			return RunResult{Status: "ok"}
		},
		PlanScheduledRun: testScheduledRunPlan,
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
	}, TriggerRequest{
		Source:                      RunSourceWorkingTree,
		ExecutionTime:               confirmedPlan.ExecutionTime,
		ExpectedSourceMerkle:        confirmedPlan.SourceMerkle,
		ExpectedConfigurationDigest: confirmedPlan.ConfigurationDigest,
		ConfirmedPlan:               &confirmedPlan,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		stored, _, _, getErr := service.GetRun(context.Background(), run.ID)
		if getErr != nil || stored.Status != RunStatusSuccess {
			return false
		}
		units, unitsErr := service.ListRunUnits(context.Background(), run.ID)
		return unitsErr == nil && len(units) == 1 && units[0].Status == PipelineRunUnitSuccess
	}, 2*time.Second, 20*time.Millisecond)
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

	confirmedPlan := validPipelineRunPlan(t)
	_, err = service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id", PipelineName: "analytics"}, TriggerRequest{
		Source:                      RunSourceWorkingTree,
		ExecutionTime:               confirmedPlan.ExecutionTime,
		ExpectedSourceMerkle:        confirmedPlan.SourceMerkle,
		ExpectedConfigurationDigest: confirmedPlan.ConfigurationDigest,
		ConfirmedPlan:               &confirmedPlan,
	})
	require.ErrorContains(t, err, "injected River admission failure")
	result, err := service.ListRuns(ctx, RunFilter{})
	require.NoError(t, err)
	assert.Zero(t, result.Total)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_specs`))
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_plans`))
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_units`))
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
			if err := completeTestScheduledRunUnits(req); err != nil {
				return RunResult{Status: "error", Error: err.Error()}
			}
			return RunResult{Status: "ok"}
		},
		PlanScheduledRun: testScheduledRunPlan,
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

func TestWorkerFinalizesWithContextDetachedFromRunnerCancellation(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	riverJobID := int64(655)
	run := PipelineRun{
		ID: "cancelled-after-execution", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued, RiverJobID: &riverJobID,
	}
	_, err = store.CreateWithSpec(ctx, run, manualRunSpec(run, RunSourceWorkingTree, ""))
	require.NoError(t, err)

	worker := &pipelineRunWorker{service: New(Options{
		Store: store,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			cancel()
			return RunResult{Status: "ok"}
		},
	})}
	require.NoError(t, worker.Work(ctx, &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: riverJobID},
		Args:   pipelineRunJobArgs{RunID: run.ID},
	}))
	require.ErrorIs(t, ctx.Err(), context.Canceled)

	finished, _, _, getErr := store.Get(context.Background(), run.ID)
	require.NoError(t, getErr)
	assert.Equal(t, RunStatusSuccess, finished.Status)
	assert.NotNil(t, finished.FinishedAt)
	assert.Zero(t, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_slots WHERE run_id = ?`, run.ID))
}

func TestWorkerRetryFinalizesRunningRunWithoutRepeatingPhysicalExecution(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	riverJobID := int64(656)
	run := PipelineRun{
		ID: "indeterminate-running-retry", PipelineID: "pipeline-id", Pipeline: "analytics",
		Trigger: RunTriggerManual, Status: RunStatusQueued, RiverJobID: &riverJobID,
	}
	_, err = store.CreateWithSpec(ctx, run, manualRunSpec(run, RunSourceWorkingTree, ""))
	require.NoError(t, err)
	started := time.Date(2026, 7, 17, 15, 0, 0, 0, time.UTC)
	succeededAt := started.Add(time.Second)
	require.NoError(t, store.MarkRunning(ctx, run.ID, started))
	require.NoError(t, store.AppendLog(ctx, run.ID, LogLine{At: started, Line: "physical execution started"}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: run.ID, Asset: "analytics.finished", Status: RunStatusSuccess,
		StartedAt: &started, FinishedAt: &succeededAt,
	}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{
		RunID: run.ID, Asset: "analytics.open", Status: RunStatusRunning, StartedAt: &started,
	}))

	var runnerCalls atomic.Int32
	worker := &pipelineRunWorker{service: New(Options{
		Store: store,
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			runnerCalls.Add(1)
			return RunResult{Status: "ok"}
		},
	})}
	require.NoError(t, worker.Work(ctx, &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: riverJobID},
		Args:   pipelineRunJobArgs{RunID: run.ID},
	}))
	assert.Zero(t, runnerCalls.Load(), "indeterminate physical work must never be repeated")

	finished, logs, steps, getErr := store.Get(ctx, run.ID)
	require.NoError(t, getErr)
	assert.Equal(t, RunStatusFailed, finished.Status)
	assert.Contains(t, finished.Error, "physical outcome is indeterminate")
	assert.NotNil(t, finished.FinishedAt)
	require.Len(t, logs, 2)
	assert.Equal(t, "physical execution started", logs[0].Line)
	assert.Contains(t, logs[1].Line, "scheduler recovery")
	require.Len(t, steps, 2)
	stepByAsset := make(map[string]PipelineRunStep, len(steps))
	for _, step := range steps {
		stepByAsset[step.Asset] = step
	}
	assert.Equal(t, RunStatusSuccess, stepByAsset["analytics.finished"].Status,
		"terminal evidence from the original attempt must be preserved")
	assert.Equal(t, succeededAt, *stepByAsset["analytics.finished"].FinishedAt)
	assert.Equal(t, RunStatusFailed, stepByAsset["analytics.open"].Status)
	assert.Contains(t, stepByAsset["analytics.open"].Error, "physical outcome is indeterminate")
	assert.NotNil(t, stepByAsset["analytics.open"].FinishedAt)
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
			if err := completeTestScheduledRunUnits(req); err != nil {
				return RunResult{Status: "error", Error: err.Error()}
			}
			return RunResult{Status: "ok"}
		},
		PlanScheduledRun: testScheduledRunPlan,
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

func TestScheduledWorkerRetainsBlockedPlanWithoutExecuting(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	var runnerCalls atomic.Int32
	service := New(Options{
		Store: store,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		PlanScheduledRun: func(ctx context.Context, req ScheduledRunPlanRequest) (ScheduledRunPlanResult, error) {
			planned, err := testScheduledRunPlan(ctx, req)
			if err != nil {
				return ScheduledRunPlanResult{}, err
			}
			var artifact map[string]any
			if err := json.Unmarshal(planned.Plan.Artifact, &artifact); err != nil {
				return ScheduledRunPlanResult{}, err
			}
			artifact["status"] = "blocked"
			artifact["execution_units"] = []any{}
			artifact["readiness"] = map[string]any{
				"blockers": []map[string]string{{"message": "analytics.report cannot be rendered"}},
			}
			planned.Plan.Artifact, err = json.Marshal(artifact)
			if err != nil {
				return ScheduledRunPlanResult{}, err
			}
			planned.Plan.Blocked = true
			planned.Plan.ExecutionUnits = nil
			planned.Plan.Blockers = []string{"analytics.report cannot be rendered"}
			return planned, nil
		},
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			runnerCalls.Add(1)
			return RunResult{Status: "ok"}
		},
	})
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	worker := &pipelineRunWorker{service: service}
	args := pipelineRunJobArgs{
		PipelineUUID: "pipeline-uuid", Environment: "prod",
		Start: start.Format(time.RFC3339), End: start.Add(time.Hour).Format(time.RFC3339),
		SnapshotVersionID: "snapshot-id",
	}
	_, _, ok, err := service.prepareRun(context.Background(), 77, args)
	require.Error(t, err)
	assert.False(t, ok)
	var blocked *scheduledPlanBlockedError
	require.ErrorAs(t, err, &blocked)

	// A crash after atomic admission but before the worker records failure must
	// still observe the retained blocked bit and never execute on retry.
	_, _, ok, retryErr := service.prepareRun(context.Background(), 77, args)
	require.Error(t, retryErr)
	assert.False(t, ok)
	require.ErrorAs(t, retryErr, &blocked)

	err = worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{JobRow: &rivertype.JobRow{ID: 77}, Args: args})
	require.Error(t, err)
	assert.Zero(t, runnerCalls.Load())

	runs, listErr := store.List(context.Background(), RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, listErr)
	require.Len(t, runs.Runs, 1)
	assert.Equal(t, RunStatusFailed, runs.Runs[0].Status)
	assert.Contains(t, runs.Runs[0].Error, "analytics.report cannot be rendered")
	_, found, planErr := store.GetRunPlan(context.Background(), runs.Runs[0].ID)
	require.NoError(t, planErr)
	assert.True(t, found)
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
