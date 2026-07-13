package scheduler

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreCreatesRunsLogsAndWatermarks(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), ".renart", "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	id, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Environment: "dev", Trigger: RunTriggerManual, Status: RunStatusQueued, WinStart: &start, WinEnd: &end})
	require.NoError(t, err)
	require.NotEmpty(t, id)
	require.NoError(t, store.MarkRunning(ctx, id, start))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: id, Asset: "orders_cleaned", Status: RunStatusRunning, StartedAt: &start}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: id, Asset: "orders_cleaned", Status: RunStatusSuccess, FinishedAt: &end}))
	require.NoError(t, store.AppendLog(ctx, id, LogLine{At: start, Line: "hello"}))
	require.NoError(t, store.Finish(ctx, id, RunStatusSuccess, nil))
	require.NoError(t, store.SetInterval(ctx, "pipeline-id", end))

	result, err := store.List(ctx, RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, err)
	runs := result.Runs
	require.Len(t, runs, 1)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, RunStatusSuccess, runs[0].Status)

	run, logs, steps, err := store.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "analytics", run.Pipeline)
	require.Len(t, logs, 1)
	assert.Equal(t, "hello", logs[0].Line)
	require.Len(t, steps, 1)
	assert.Equal(t, "orders_cleaned", steps[0].Asset)
	assert.Equal(t, RunStatusSuccess, steps[0].Status)
	require.NotNil(t, steps[0].StartedAt)
	require.NotNil(t, steps[0].FinishedAt)

	watermark, ok, err := store.LastInterval(ctx, "pipeline-id")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, end, watermark)
}

func TestFailOrphanedRunsReconcilesRunningRuns(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), ".renart", "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

	// A run that was executing when the process died: still "running", with an
	// open step.
	orphan, err := store.Create(ctx, PipelineRun{PipelineID: "p1", Pipeline: "analytics", Environment: "dev", Trigger: RunTriggerManual, Status: RunStatusQueued})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, orphan, started))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: orphan, Asset: "orders", Status: RunStatusRunning, StartedAt: &started}))

	// A run that finished normally must be left untouched.
	done, err := store.Create(ctx, PipelineRun{PipelineID: "p1", Pipeline: "analytics", Environment: "dev", Trigger: RunTriggerManual, Status: RunStatusQueued})
	require.NoError(t, err)
	require.NoError(t, store.MarkRunning(ctx, done, started))
	require.NoError(t, store.Finish(ctx, done, RunStatusSuccess, nil))

	recovery, err := store.ReconcileInterruptedState(ctx, orphanedRunError)
	require.NoError(t, err)
	require.Equal(t, []string{orphan}, recovery.RunIDs)
	assert.Zero(t, recovery.RiverJobsCancelled)
	pending, err := store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{orphan}, pending)

	orphanRun, _, steps, err := store.Get(ctx, orphan)
	require.NoError(t, err)
	assert.Equal(t, RunStatusFailed, orphanRun.Status)
	assert.Equal(t, orphanedRunError, orphanRun.Error)
	require.NotNil(t, orphanRun.FinishedAt)
	require.Len(t, steps, 1)
	assert.Equal(t, RunStatusFailed, steps[0].Status)
	require.NotNil(t, steps[0].FinishedAt)

	doneRun, _, _, err := store.Get(ctx, done)
	require.NoError(t, err)
	assert.Equal(t, RunStatusSuccess, doneRun.Status)

	// Idempotent: a second pass finds nothing to reconcile.
	again, err := store.ReconcileInterruptedState(ctx, orphanedRunError)
	require.NoError(t, err)
	assert.Empty(t, again.RunIDs)
	assert.Zero(t, again.RiverJobsCancelled)
	pending, err = store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{orphan}, pending, "replay remains pending until acknowledged")
	require.NoError(t, store.MarkRunRecoveryReplayed(ctx, orphan))
	pending, err = store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestReconcileInterruptedStateCancelsClaimedRiverJobsAndPreservesQueuedJobs(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), ".renart", "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	claimedRunID, err := store.Create(ctx, PipelineRun{
		ID: "claimed-run", PipelineID: "pipeline-id", Pipeline: "analytics",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	claimedJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: claimedRunID})
	markTestRiverJobRunning(t, store, claimedJobID)

	queuedRunID, err := store.Create(ctx, PipelineRun{
		ID: "queued-run", PipelineID: "other-pipeline", Pipeline: "marketing",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusQueued,
	})
	require.NoError(t, err)
	queuedJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: queuedRunID})
	require.NoError(t, store.SetRunRiverJob(ctx, queuedRunID, queuedJobID))

	finishedJobID := insertTestRiverJob(t, store, pipelineRunJobArgs{RunID: "finished-run"})
	finishedRunID, err := store.Create(ctx, PipelineRun{
		ID: "finished-run", PipelineID: "finished-pipeline", Pipeline: "finished",
		Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusSuccess,
		RiverJobID: &finishedJobID,
	})
	require.NoError(t, err)
	markTestRiverJobRunning(t, store, finishedJobID)

	housekeepingJobID := insertTestRiverJob(t, store, housekeepingJobArgs{})
	markTestRiverJobRunning(t, store, housekeepingJobID)

	recovery, err := store.ReconcileInterruptedState(ctx, orphanedRunError)
	require.NoError(t, err)
	assert.Equal(t, []string{claimedRunID}, recovery.RunIDs)
	assert.EqualValues(t, 3, recovery.RiverJobsCancelled)

	claimedRun, _, _, err := store.Get(ctx, claimedRunID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusFailed, claimedRun.Status)
	assert.Equal(t, orphanedRunError, claimedRun.Error)
	require.NotNil(t, claimedRun.RiverJobID)
	assert.Equal(t, claimedJobID, *claimedRun.RiverJobID)

	queuedRun, _, _, err := store.Get(ctx, queuedRunID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusQueued, queuedRun.Status)
	assertRiverJobState(t, store, queuedJobID, rivertype.JobStateAvailable)

	finishedRun, _, _, err := store.Get(ctx, finishedRunID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusSuccess, finishedRun.Status)
	assertRiverJobState(t, store, claimedJobID, rivertype.JobStateCancelled)
	assertRiverJobState(t, store, finishedJobID, rivertype.JobStateCancelled)
	assertRiverJobState(t, store, housekeepingJobID, rivertype.JobStateCancelled)

	var riverErrors string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT json(errors) FROM river_job WHERE id = ?`, claimedJobID).Scan(&riverErrors))
	assert.Contains(t, riverErrors, orphanedRunError)
}

func TestRunRecoveryMigrationBackfillsInterruptedRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".renart", "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	migrations, err := fs.Sub(schedulerMigrations, "storedb/migrations")
	require.NoError(t, err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, store.db, migrations)
	require.NoError(t, err)
	_, err = provider.DownTo(ctx, 7)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO pipeline_runs (id, pipeline_id, pipeline, environment, trigger, status, error)
		VALUES
			('previously-reconciled', 'p1', 'analytics', 'prod', 'schedule', 'failed', ?),
			('ordinary-failure', 'p1', 'analytics', 'prod', 'schedule', 'failed', 'asset failed')`,
		orphanedRunError,
	)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()
	pending, err := store.PendingRunRecoveries(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"previously-reconciled"}, pending)
}

func insertTestRiverJob(t *testing.T, store *Store, args river.JobArgs) int64 {
	t.Helper()
	client, err := river.NewClient(riversqlite.New(store.db), &river.Config{})
	require.NoError(t, err)
	inserted, err := client.Insert(context.Background(), args, &river.InsertOpts{
		MaxAttempts: 1,
		Queue:       pipelineRunQueue,
	})
	require.NoError(t, err)
	return inserted.Job.ID
}

func markTestRiverJobRunning(t *testing.T, store *Store, jobID int64) {
	t.Helper()
	_, err := store.db.ExecContext(context.Background(), `
		UPDATE river_job
		SET state = ?, attempt = 1, attempted_at = ?
		WHERE id = ?`,
		string(rivertype.JobStateRunning), formatTime(time.Now().UTC()), jobID,
	)
	require.NoError(t, err)
}

func assertRiverJobState(t *testing.T, store *Store, jobID int64, expected rivertype.JobState) {
	t.Helper()
	var state string
	require.NoError(t, store.db.QueryRowContext(context.Background(), `SELECT state FROM river_job WHERE id = ?`, jobID).Scan(&state))
	assert.Equal(t, string(expected), state)
}

func TestStoreDefaultsRunStatusAndGeneratedID(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	id, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	run, _, _, err := store.Get(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, run.ID)
	assert.Equal(t, RunStatusQueued, run.Status)
}

func TestStoreListOrdersFiltersAndLimitsRuns(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	started := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	older := started.Add(-time.Hour)
	newer := started.Add(time.Hour)

	for _, run := range []PipelineRun{
		{ID: "older", PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual, Status: RunStatusSuccess, StartedAt: &older},
		{ID: "first", PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual, Status: RunStatusSuccess, StartedAt: &started},
		{ID: "second", PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual, Status: RunStatusFailed, StartedAt: &started},
		{ID: "other", PipelineID: "other-pipeline", Pipeline: "other", Trigger: RunTriggerManual, Status: RunStatusSuccess, StartedAt: &newer},
	} {
		_, err := store.Create(ctx, run)
		require.NoError(t, err)
	}

	result, err := store.List(ctx, RunFilter{PipelineID: "pipeline-id", Limit: 2})
	require.NoError(t, err)
	runs := result.Runs
	require.Len(t, runs, 2)
	assert.Equal(t, 3, result.Total)
	assert.Equal(t, []string{"second", "first"}, []string{runs[0].ID, runs[1].ID})

	result, err = store.List(ctx, RunFilter{Limit: 2})
	require.NoError(t, err)
	runs = result.Runs
	require.Len(t, runs, 2)
	assert.Equal(t, 4, result.Total)
	assert.Equal(t, []string{"other", "second"}, []string{runs[0].ID, runs[1].ID})
}

func TestStoreUpsertStepPreservesFirstStartedAtAndIgnoresBlankAsset(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual})
	require.NoError(t, err)
	started := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	laterStart := started.Add(time.Minute)
	finished := started.Add(5 * time.Minute)

	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: "orders", Status: RunStatusRunning, StartedAt: &started}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: "orders", Status: RunStatusSuccess, StartedAt: &laterStart, FinishedAt: &finished}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: " ", Status: RunStatusSuccess}))

	steps, err := store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	assert.Equal(t, "orders", steps[0].Asset)
	require.NotNil(t, steps[0].StartedAt)
	require.NotNil(t, steps[0].FinishedAt)
	assert.Equal(t, started, *steps[0].StartedAt)
	assert.Equal(t, finished, *steps[0].FinishedAt)
	assert.Equal(t, RunStatusSuccess, steps[0].Status)
}

func TestStoreFinishOpenStepsUpdatesOnlyUnfinishedSteps(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual})
	require.NoError(t, err)
	started := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	previousFinish := started.Add(time.Minute)
	finish := started.Add(2 * time.Minute)

	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: "finished", Status: RunStatusSuccess, StartedAt: &started, FinishedAt: &previousFinish}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: "open", Status: RunStatusRunning, StartedAt: &started}))
	require.NoError(t, store.FinishOpenSteps(ctx, runID, RunStatusFailed, finish, assert.AnError))

	steps, err := store.ListSteps(ctx, runID)
	require.NoError(t, err)
	require.Len(t, steps, 2)
	assert.Equal(t, "finished", steps[0].Asset)
	assert.Equal(t, RunStatusSuccess, steps[0].Status)
	assert.Equal(t, previousFinish, *steps[0].FinishedAt)
	assert.Equal(t, "open", steps[1].Asset)
	assert.Equal(t, RunStatusFailed, steps[1].Status)
	assert.Equal(t, finish, *steps[1].FinishedAt)
	assert.Equal(t, assert.AnError.Error(), steps[1].Error)
}

func TestStoreDeletesRunLogsAndStepsWithRun(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	runID, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual})
	require.NoError(t, err)
	require.NoError(t, store.AppendLog(ctx, runID, LogLine{Line: "hello"}))
	require.NoError(t, store.UpsertStep(ctx, PipelineRunStep{RunID: runID, Asset: "orders", Status: RunStatusRunning}))

	_, err = store.db.ExecContext(ctx, `DELETE FROM pipeline_runs WHERE id = ?`, runID)
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_logs WHERE run_id = ?`, runID))
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_run_steps WHERE run_id = ?`, runID))
}

func TestStorePersistsRunsWatermarksAndScheduleSettingsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenStore(path)
	require.NoError(t, err)

	ctx := context.Background()
	upTo := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	runID, err := store.Create(ctx, PipelineRun{ID: "persisted-run", PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual, Status: RunStatusSuccess})
	require.NoError(t, err)
	require.Equal(t, "persisted-run", runID)
	require.NoError(t, store.SetInterval(ctx, "pipeline-id", upTo))
	require.NoError(t, store.SetScheduleEnabled(ctx, "pipeline-id", false))
	require.NoError(t, store.Close())

	store, err = OpenStore(path)
	require.NoError(t, err)
	defer store.Close()

	run, _, _, err := store.Get(ctx, runID)
	require.NoError(t, err)
	assert.Equal(t, "analytics", run.Pipeline)
	watermark, ok, err := store.LastInterval(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, upTo, watermark)
	enabled, ok, err := store.ScheduleEnabled(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, enabled)
}

func TestStoreMigratesRiverQueueTables(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	var applied bool
	err = store.db.QueryRowContext(context.Background(), `SELECT is_applied FROM goose_db_version WHERE version_id = 1`).Scan(&applied)
	require.NoError(t, err)
	assert.True(t, applied)

	var tableName string
	err = store.db.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'river_job'`).Scan(&tableName)
	require.NoError(t, err)
	assert.Equal(t, "river_job", tableName)
}

func TestStoreDetectsActiveRuns(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	active, err := store.HasActiveRun(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.False(t, active)

	id, err := store.Create(ctx, PipelineRun{PipelineID: "pipeline-id", Pipeline: "analytics", Trigger: RunTriggerManual, Status: RunStatusQueued})
	require.NoError(t, err)
	active, err = store.HasActiveRun(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.True(t, active)

	require.NoError(t, store.Finish(ctx, id, RunStatusFailed, assert.AnError))
	active, err = store.HasActiveRun(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.False(t, active)
}

func TestStoreListFiltersAndPaginatesRuns(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	for index, run := range []PipelineRun{
		{PipelineID: "pipeline-a", Pipeline: "analytics", Environment: "default", Trigger: RunTriggerManual, Status: RunStatusSuccess},
		{PipelineID: "pipeline-b", Pipeline: "marketing", Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusFailed},
		{PipelineID: "pipeline-c", Pipeline: "analytics_daily", Environment: "prod", Trigger: RunTriggerManual, Status: RunStatusSuccess},
	} {
		startedAt := base.Add(time.Duration(index) * time.Minute)
		run.StartedAt = &startedAt
		_, err := store.Create(ctx, run)
		require.NoError(t, err)
	}

	result, err := store.List(ctx, RunFilter{Query: "analytics", Status: RunStatusSuccess, Limit: 1})
	require.NoError(t, err)
	require.Len(t, result.Runs, 1)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, "analytics_daily", result.Runs[0].Pipeline)

	result, err = store.List(ctx, RunFilter{Query: "analytics", Status: RunStatusSuccess, Limit: 1, Offset: 1})
	require.NoError(t, err)
	require.Len(t, result.Runs, 1)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, "analytics", result.Runs[0].Pipeline)

	result, err = store.List(ctx, RunFilter{Environment: "prod", Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)
}

func TestStorePersistsScheduleEnabledState(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()
	enabled, ok, err := store.ScheduleEnabled(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, enabled)

	require.NoError(t, store.SetScheduleEnabled(ctx, "pipeline-id", false))
	enabled, ok, err = store.ScheduleEnabled(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, enabled)

	require.NoError(t, store.SetScheduleEnabled(ctx, "pipeline-id", true))
	enabled, ok, err = store.ScheduleEnabled(ctx, "pipeline-id")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, enabled)
}

func countRows(t *testing.T, store *Store, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, store.db.QueryRowContext(context.Background(), query, args...).Scan(&count))
	return count
}
