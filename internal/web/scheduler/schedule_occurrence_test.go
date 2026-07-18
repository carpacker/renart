package scheduler

import (
	"context"
	"errors"
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

func TestScheduleOccurrenceKeyIsStableForNormalizedInterval(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 18, 10, 0, 0, 123456789, time.FixedZone("CEST", 2*60*60))
	end := start.Add(time.Hour)
	first, err := newScheduleOccurrence(" pipeline-uuid ", " prod ", start, end)
	require.NoError(t, err)
	second, err := newScheduleOccurrence("pipeline-uuid", "prod", start.UTC(), end.UTC())
	require.NoError(t, err)
	otherEnvironment, err := newScheduleOccurrence("pipeline-uuid", "dev", start.UTC(), end.UTC())
	require.NoError(t, err)

	assert.Equal(t, first.Key, second.Key)
	assert.Len(t, first.Key, 64)
	assert.NotEqual(t, first.Key, otherEnvironment.Key)
	assert.Equal(t, start.UTC(), first.IntervalStart)
	require.ErrorContains(t, func() error {
		_, invalidErr := newScheduleOccurrence("pipeline-uuid", "prod", end, start)
		return invalidErr
	}(), "increasing interval")
}

func TestScheduleOccurrenceDeduplicatesActiveAndSuccessfulSignals(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	occurrence, run, spec, plan := scheduledOccurrenceFixture(t)

	persisted, changed, err := store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, ScheduleOccurrencePending, persisted.Status)
	_, changed, err = store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)
	assert.False(t, changed)

	runID, err := store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
	require.NoError(t, err)
	require.NotEmpty(t, runID)
	active, found, err := store.GetScheduleOccurrence(ctx, occurrence.Key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleOccurrenceActive, active.Status)
	assert.Equal(t, runID, active.CurrentRunID)
	assert.Equal(t, 1, active.AttemptCount)

	_, err = store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
	require.ErrorIs(t, err, ErrScheduleOccurrenceAlreadyAdmitted)
	var duplicate *ScheduleOccurrenceAlreadyAdmittedError
	require.ErrorAs(t, err, &duplicate)
	assert.Equal(t, runID, duplicate.RunID)
	assert.Equal(t, ScheduleOccurrenceActive, duplicate.Status)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs`))

	started := time.Now().UTC()
	finished := started.Add(time.Second)
	require.NoError(t, store.UpdateRunUnit(ctx, runID, PipelineRunUnitEvent{
		Position: 0, Status: PipelineRunUnitRunning, StartedAt: &started,
	}))
	require.NoError(t, store.UpdateRunUnit(ctx, runID, PipelineRunUnitEvent{
		Position: 0, Status: PipelineRunUnitSuccess, StartedAt: &started, FinishedAt: &finished,
	}))
	require.NoError(t, store.Finish(ctx, runID, RunStatusSuccess, nil))
	succeeded, found, err := store.GetScheduleOccurrence(ctx, occurrence.Key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleOccurrenceSuccess, succeeded.Status)

	_, changed, err = store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)
	assert.False(t, changed, "a successful occurrence is immutable when duplicate signals arrive")
	_, err = store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
	require.ErrorIs(t, err, ErrScheduleOccurrenceAlreadyAdmitted)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs`))
}

func TestScheduleOccurrenceRetriesFailedRunAsNumberedAttempt(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	occurrence, run, spec, plan := scheduledOccurrenceFixture(t)
	_, _, err = store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)

	firstRunID, err := store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
	require.NoError(t, err)
	require.NoError(t, store.Finish(ctx, firstRunID, RunStatusFailed, errors.New("temporary warehouse failure")))
	failed, found, err := store.GetScheduleOccurrence(ctx, occurrence.Key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleOccurrenceFailed, failed.Status)
	assert.Equal(t, 1, failed.AttemptCount)

	pending, changed, err := store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, ScheduleOccurrencePending, pending.Status)
	deferred, found, err := store.DeferredScheduleOccurrence(ctx, occurrence.PipelineUUID, occurrence.Environment)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, deferred.AttemptCount)

	secondRunID, err := store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
	require.NoError(t, err)
	assert.NotEqual(t, firstRunID, secondRunID)
	retried, found, err := store.GetScheduleOccurrence(ctx, occurrence.Key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleOccurrenceActive, retried.Status)
	assert.Equal(t, secondRunID, retried.CurrentRunID)
	assert.Equal(t, 2, retried.AttemptCount)
	assert.Equal(t, 2, countRows(t, store, `SELECT COUNT(*) FROM schedule_occurrence_attempts WHERE occurrence_key = ?`, occurrence.Key))
}

func TestScheduleOccurrenceSlotConflictRollsBackAttemptAndStaysDeferred(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx := context.Background()
	occurrence, scheduledRun, scheduledSpec, plan := scheduledOccurrenceFixture(t)
	_, _, err = store.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)

	manualRun := PipelineRun{
		ID: "manual-owner", PipelineID: scheduledRun.PipelineID, PipelineUUID: scheduledRun.PipelineUUID,
		Pipeline: scheduledRun.Pipeline, Environment: "dev", Trigger: RunTriggerManual, Status: RunStatusQueued,
	}
	_, err = store.CreateWithSpec(ctx, manualRun, manualRunSpec(manualRun, RunSourceWorkingTree, ""))
	require.NoError(t, err)

	_, err = store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, scheduledRun, scheduledSpec, plan)
	require.ErrorIs(t, err, ErrPipelineRunActive)
	persisted, found, err := store.GetScheduleOccurrence(ctx, occurrence.Key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ScheduleOccurrencePending, persisted.Status)
	assert.Zero(t, persisted.AttemptCount)
	assert.Empty(t, persisted.CurrentRunID)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM schedule_occurrence_attempts`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs`))
}

func TestConcurrentScheduleOccurrenceAdmissionCreatesOneRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	firstStore, err := OpenStore(path)
	require.NoError(t, err)
	defer firstStore.Close()
	secondStore, err := OpenStore(path)
	require.NoError(t, err)
	defer secondStore.Close()
	ctx := context.Background()
	occurrence, run, spec, plan := scheduledOccurrenceFixture(t)
	_, _, err = firstStore.EnsureScheduleOccurrence(ctx, occurrence)
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, store := range []*Store{firstStore, secondStore} {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			<-start
			_, admissionErr := store.CreateScheduleOccurrenceAttemptWithSpecAndPlan(ctx, occurrence, run, spec, plan)
			results <- admissionErr
		}(store)
	}
	close(start)
	wg.Wait()
	close(results)
	var admitted, duplicate int
	for result := range results {
		switch {
		case result == nil:
			admitted++
		case errors.Is(result, ErrScheduleOccurrenceAlreadyAdmitted):
			duplicate++
		default:
			require.NoError(t, result)
		}
	}
	assert.Equal(t, 1, admitted)
	assert.Equal(t, 1, duplicate)
	assert.Equal(t, 1, countRows(t, firstStore, `SELECT COUNT(*) FROM pipeline_runs`))
}

func TestScheduledWorkerDoesNotReexecuteCompletedOccurrence(t *testing.T) {
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
		PlanScheduledRun: testScheduledRunPlan,
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			runnerCalls.Add(1)
			if err := completeTestScheduledRunUnits(req); err != nil {
				return RunResult{Status: "error", Error: err.Error()}
			}
			return RunResult{Status: "ok"}
		},
	})
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	args := pipelineRunJobArgs{
		PipelineUUID: "pipeline-uuid", Environment: "prod", PipelineName: "analytics",
		Start: start.Format(time.RFC3339Nano), End: start.Add(time.Hour).Format(time.RFC3339Nano),
		SnapshotVersionID: "snapshot-id",
	}
	worker := &pipelineRunWorker{service: service}
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: 101}, Args: args,
	}))
	require.NoError(t, worker.Work(context.Background(), &river.Job[pipelineRunJobArgs]{
		JobRow: &rivertype.JobRow{ID: 202}, Args: args,
	}))

	assert.EqualValues(t, 1, runnerCalls.Load())
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs`))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM schedule_occurrences`))
}

func scheduledOccurrenceFixture(t testing.TB) (
	ScheduleOccurrence,
	PipelineRun,
	runSpecV1,
	PipelineRunPlan,
) {
	t.Helper()
	start := time.Date(2026, 7, 18, 8, 0, 0, 123456789, time.UTC)
	end := start.Add(time.Hour)
	executionTime := start.Add(30 * time.Minute)
	occurrence, err := newScheduleOccurrence("pipeline-uuid", "prod", start, end)
	require.NoError(t, err)
	planned, err := testScheduledRunPlan(context.Background(), ScheduledRunPlanRequest{
		PipelineID: "pipeline-id", PipelineUUID: occurrence.PipelineUUID,
		Environment: occurrence.Environment, SnapshotVersionID: "snapshot-id",
		Start: start, End: end, ExecutionTime: executionTime,
	})
	require.NoError(t, err)
	run := PipelineRun{
		PipelineID: "pipeline-id", PipelineUUID: occurrence.PipelineUUID, Pipeline: "analytics",
		Environment: occurrence.Environment, Trigger: RunTriggerSchedule, Status: RunStatusQueued,
		WinStart: &start, WinEnd: &end, ExecutionTime: &executionTime,
		SnapshotVersionID:           "snapshot-id",
		ExpectedSourceMerkle:        planned.Plan.SourceMerkle,
		ExpectedConfigurationDigest: planned.Plan.ConfigurationDigest,
	}
	spec := scheduledRunSpec(run, pipelineRunJobArgs{
		PipelineUUID: occurrence.PipelineUUID, Environment: occurrence.Environment,
		Schedule: "@hourly", Timezone: "UTC", OccurrenceKey: occurrence.Key,
	})
	spec.Expected = &runExpectedIdentity{
		SourceMerkle: planned.Plan.SourceMerkle, ConfigurationDigest: planned.Plan.ConfigurationDigest,
	}
	require.NoError(t, spec.validate())
	return occurrence, run, spec, planned.Plan
}
