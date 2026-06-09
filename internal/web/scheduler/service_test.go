package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	runs, err := service.ListRuns(context.Background(), RunFilter{PipelineID: "pipeline-id"})
	require.NoError(t, err)
	require.Len(t, runs, 1)
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
