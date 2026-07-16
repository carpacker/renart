package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutionServiceContextValidationStopsExecutor(t *testing.T) {
	t.Parallel()

	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	for _, tt := range []struct {
		name string
		spec PipelineRunSpec
		want string
	}{
		{
			name: "invalid sensor mode",
			spec: PipelineRunSpec{PipelineID: pipelineID, SensorMode: "sometimes"},
			want: "invalid sensor_mode",
		},
		{
			name: "backfill without window",
			spec: PipelineRunSpec{PipelineID: pipelineID, Backfill: true},
			want: "backfill requires an explicit start and end",
		},
		{
			name: "mode conflict",
			spec: PipelineRunSpec{PipelineID: pipelineID, FullRefresh: true, Backfill: true},
			want: "mutually exclusive",
		},
		{
			name: "dry run with explicit window",
			spec: PipelineRunSpec{
				PipelineID: pipelineID,
				DryRun:     true,
				StartDate:  "2026-07-16T08:00:00Z",
				EndDate:    "2026-07-16T09:00:00Z",
			},
			want: "dry_run does not support",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			executor := &stubExecutionExecutor{}
			svc := NewExecutionService(ExecutionDependencies{Executor: executor})

			result := svc.MaterializePipelineRun(context.Background(), tt.spec, nil, nil)

			assert.Equal(t, "error", result.Status)
			assert.Contains(t, result.Error, tt.want)
			assert.Empty(t, executor.runPipelineReqs)
		})
	}
}
