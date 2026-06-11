package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

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

func TestStoreMigratesRiverQueueTables(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

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
