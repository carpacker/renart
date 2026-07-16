package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/policy"
)

func TestQueuedBackfillRechecksConfirmationBeforeExecution(t *testing.T) {
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	newService := func(executor *stubExecutionExecutor) *ExecutionService {
		return NewExecutionService(ExecutionDependencies{
			Executor: executor,
			PolicyFor: func(string) policy.EnvironmentPolicy {
				return policy.EnvironmentPolicy{ConfirmDestructive: true}
			},
		})
	}

	t.Run("missing confirmation fails before side effects", func(t *testing.T) {
		executor := &stubExecutionExecutor{}
		result := newService(executor).MaterializePipelineRun(context.Background(), PipelineRunSpec{
			RunID:       "queued-backfill",
			PipelineID:  pipelineID,
			Environment: "prod",
			Backfill:    true,
			StartDate:   "2026-07-16T08:00:00Z",
			EndDate:     "2026-07-16T09:00:00Z",
		}, nil, nil)

		assert.Equal(t, "error", result.Status)
		assert.Contains(t, result.Error, "requires typing the environment name")
		assert.Empty(t, executor.runPipelineReqs)
	})

	t.Run("matching confirmation executes the requested interval", func(t *testing.T) {
		executor := &stubExecutionExecutor{}
		result := newService(executor).MaterializePipelineRun(context.Background(), PipelineRunSpec{
			RunID:                "queued-backfill",
			PipelineID:           pipelineID,
			Environment:          "prod",
			Backfill:             true,
			StartDate:            "2026-07-16T08:00:00Z",
			EndDate:              "2026-07-16T09:00:00Z",
			ConfirmedEnvironment: "prod",
		}, nil, nil)

		require.Equal(t, "ok", result.Status)
		require.Len(t, executor.runPipelineReqs, 1)
		assert.Equal(t, "2026-07-16T08:00:00Z", executor.runPipelineReqs[0].StartDate)
		assert.Equal(t, "2026-07-16T09:00:00Z", executor.runPipelineReqs[0].EndDate)
	})
}
