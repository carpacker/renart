package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineRunPersistsEffectiveContextBeforeExecution(t *testing.T) {
	t.Parallel()
	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".bruin.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(strings.TrimSpace(`
default_environment: prod
environments:
  prod:
    config:
      full_refresh_restricted: true
    connections: {}
`)+"\n"), 0o644))

	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		ConfigPath:          configPath,
		Executor:            executor,
		SelectedEnvironment: func() string { return "prod" },
	})
	var resolved ResolvedPipelineRunContext
	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID:  EncodeID("pipelines/orders/pipeline.yml"),
		Scheduled:   true,
		FullRefresh: true,
		StartDate:   "2026-07-16T08:00:00Z",
		EndDate:     "2026-07-16T09:00:00Z",
		OnContextResolved: func(value ResolvedPipelineRunContext) error {
			assert.Empty(t, executor.runPipelineReqs, "context must be durable before execution starts")
			resolved = value
			return nil
		},
	}, nil, nil)

	require.Equal(t, "ok", result.Status, result.Error)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, "prod", resolved.Environment)
	assert.Equal(t, time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC), resolved.WinStart)
	assert.Equal(t, time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC), resolved.WinEnd)
	assert.False(t, resolved.FullRefresh, "the environment restriction is part of the effective context")
	assert.False(t, resolved.Backfill)
	assert.Equal(t, sensorModeWait, resolved.SensorMode)
	assert.False(t, executor.runPipelineReqs[0].FullRefresh)
	assert.Equal(t, resolved.SensorMode, executor.runPipelineReqs[0].SensorMode)
}

func TestPipelineRunStopsBeforeExecutionWhenContextPersistenceFails(t *testing.T) {
	t.Parallel()
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{Executor: executor})
	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID: EncodeID("pipelines/orders/pipeline.yml"),
		StartDate:  "2026-07-16T08:00:00Z",
		EndDate:    "2026-07-16T09:00:00Z",
		OnContextResolved: func(ResolvedPipelineRunContext) error {
			return errors.New("state database is unavailable")
		},
	}, nil, nil)

	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "persist resolved run context")
	assert.Empty(t, executor.runPipelineReqs)
}
