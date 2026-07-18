package scheduler

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManualQueuedRunPreservesDestructiveContextThroughRiver(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captured := make(chan RunRequest, 1)
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			captured <- req
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	run, err := service.Trigger(ctx, PipelineSchedule{
		PipelineID: "pipeline-id", PipelineName: "analytics",
	}, TriggerRequest{
		Environment:          "prod",
		Start:                "2026-07-16T08:00:00Z",
		End:                  "2026-07-16T09:00:00Z",
		Backfill:             true,
		FullRefresh:          false,
		ConfirmedEnvironment: "  prod  ",
		SensorMode:           "skip",
	})
	require.NoError(t, err)

	select {
	case req := <-captured:
		assert.Equal(t, run.ID, req.RunID)
		assert.False(t, req.Scheduled)
		assert.True(t, req.Backfill)
		assert.False(t, req.FullRefresh)
		assert.Equal(t, "prod", req.ConfirmedEnvironment)
		assert.Equal(t, "skip", req.SensorMode)
		assert.Equal(t, "2026-07-16T08:00:00Z", req.Start)
		assert.Equal(t, "2026-07-16T09:00:00Z", req.End)
	case <-time.After(2 * time.Second):
		t.Fatal("queued runner did not receive the manual run context")
	}
}

func TestManualQueuedRunPreservesFullRefreshAndSensorContextThroughRiver(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captured := make(chan RunRequest, 1)
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			captured <- req
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()

	_, err = service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id"}, TriggerRequest{
		SensorMode: "sometimes",
	})
	require.ErrorContains(t, err, "invalid sensor_mode")
	_, err = service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id"}, TriggerRequest{
		FullRefresh: true,
		Backfill:    true,
	})
	require.ErrorContains(t, err, "mutually exclusive")
	_, err = service.Trigger(ctx, PipelineSchedule{PipelineID: "pipeline-id"}, TriggerRequest{
		Backfill: true,
	})
	require.ErrorContains(t, err, "explicit start and end")

	_, err = service.Trigger(ctx, PipelineSchedule{
		PipelineID: "pipeline-id", PipelineName: "analytics",
	}, TriggerRequest{
		Environment:          "prod",
		FullRefresh:          true,
		ConfirmedEnvironment: "prod",
		SensorMode:           "skip",
	})
	require.NoError(t, err)

	select {
	case req := <-captured:
		assert.False(t, req.Scheduled)
		assert.True(t, req.FullRefresh)
		assert.False(t, req.Backfill)
		assert.Equal(t, "prod", req.ConfirmedEnvironment)
		assert.Equal(t, "skip", req.SensorMode)
	case <-time.After(2 * time.Second):
		t.Fatal("queued runner did not receive full-refresh and sensor context")
	}
}

func TestScheduledSignalQueuesAndExecutesRunIDOnlyJobThroughRiver(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captured := make(chan RunRequest, 1)
	service := New(Options{
		Store:    store,
		StateDir: stateDir,
		ResolvePipelineRef: func(context.Context, string) (PipelineRef, bool) {
			return PipelineRef{EncodedID: "pipeline-id", Name: "analytics"}, true
		},
		PlanScheduledRun: testScheduledRunPlan,
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			captured <- req
			if unitErr := completeTestScheduledRunUnits(req); unitErr != nil {
				return RunResult{Status: "error", Error: unitErr.Error()}
			}
			return RunResult{Status: "ok"}
		},
	})
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	service.mu.Lock()
	client := service.riverClient
	service.mu.Unlock()
	require.NotNil(t, client)
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	_, err = client.Insert(ctx, scheduleSignalJobArgs{
		PipelineUUID: "pipeline-uuid", PipelineName: "analytics", Environment: "prod",
		Schedule: "@hourly", Timezone: "UTC",
		Start: start.Format(time.RFC3339Nano), End: start.Add(time.Hour).Format(time.RFC3339Nano),
		SnapshotVersionID: "snapshot-id",
	}, scheduleSignalInsertOpts())
	require.NoError(t, err)

	select {
	case req := <-captured:
		assert.True(t, req.Scheduled)
		assert.NotEmpty(t, req.RunID)
		assert.Equal(t, "pipeline-id", req.PipelineID)
		assert.Equal(t, "snapshot-id", req.SnapshotVersionID)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled execution job did not run")
	}
	require.Eventually(t, func() bool {
		return countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs WHERE status = ?`, string(RunStatusSuccess)) == 1
	}, 5*time.Second, 25*time.Millisecond)
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM river_job WHERE kind = ?`, scheduleSignalJobKind))
	assert.Equal(t, 1, countRows(t, store, `SELECT COUNT(*) FROM river_job WHERE kind = ?`, pipelineRunJobKind))
	var executionArgs string
	require.NoError(t, store.db.QueryRowContext(ctx, `
		SELECT json(args) FROM river_job WHERE kind = ? LIMIT 1`, pipelineRunJobKind).Scan(&executionArgs))
	assert.Contains(t, executionArgs, `"run_id"`)
	assert.NotContains(t, executionArgs, `"pipeline_uuid"`)
}
