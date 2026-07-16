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
