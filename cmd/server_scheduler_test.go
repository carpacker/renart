package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	webscheduler "renart/internal/web/scheduler"
	"renart/internal/web/service"
)

func TestPipelineRunSpecFromSchedulerRequestPreservesDestructiveContext(t *testing.T) {
	req := webscheduler.RunRequest{
		RunID:                "run-id",
		PipelineID:           "pipeline-id",
		Environment:          "prod",
		Start:                "2026-07-16T08:00:00Z",
		End:                  "2026-07-16T09:00:00Z",
		Scheduled:            false,
		SnapshotVersionID:    "snapshot-id",
		FullRefresh:          true,
		Backfill:             true,
		ConfirmedEnvironment: "prod",
		SensorMode:           "skip",
		VariableOverrides:    map[string]any{"region": "eu"},
	}

	spec := pipelineRunSpecFromSchedulerRequest(req)
	assert.Equal(t, req.RunID, spec.RunID)
	assert.Equal(t, req.PipelineID, spec.PipelineID)
	assert.Equal(t, req.Environment, spec.Environment)
	assert.Equal(t, req.Start, spec.StartDate)
	assert.Equal(t, req.End, spec.EndDate)
	assert.Equal(t, req.Scheduled, spec.Scheduled)
	assert.Equal(t, req.SnapshotVersionID, spec.SnapshotVersionID)
	assert.Equal(t, req.FullRefresh, spec.FullRefresh)
	assert.Equal(t, req.Backfill, spec.Backfill)
	assert.Equal(t, req.ConfirmedEnvironment, spec.ConfirmedEnvironment)
	assert.Equal(t, req.SensorMode, spec.SensorMode)
	assert.Equal(t, req.VariableOverrides, spec.VariableOverrides)
}

func TestPersistSchedulerResolvedExecutionUnitsPreservesRuntimePlan(t *testing.T) {
	t.Parallel()
	var persisted []webscheduler.PipelineRunExecutionUnit
	req := webscheduler.RunRequest{
		RunID: "run-id",
		OnExecutionUnitsResolved: func(units []webscheduler.PipelineRunExecutionUnit) error {
			persisted = append([]webscheduler.PipelineRunExecutionUnit(nil), units...)
			return nil
		},
	}
	units := []service.PipelineExecutionUnit{
		{
			Position: 0, AssetID: "pipeline:left", AssetName: "analytics.left",
			StartDate: "2026-07-28T08:00:00Z", EndDate: "2026-07-28T09:00:00Z",
			RenderIndex: 0, Reason: "all",
		},
		{
			Position: 1, AssetID: "pipeline:right", AssetName: "analytics.right",
			StartDate: "2026-07-28T08:00:00Z", EndDate: "2026-07-28T09:00:00Z",
			RenderIndex: 1, Reason: "all", DependencyPositions: []int{0},
		},
	}

	require.NoError(t, persistSchedulerResolvedExecutionUnits(req, units))
	require.Len(t, persisted, 2)
	assert.Equal(t, units[1].AssetID, persisted[1].AssetID)
	assert.Equal(t, units[1].RenderIndex, persisted[1].RenderIndex)
	assert.Equal(t, []int{0}, persisted[1].DependencyPositions)

	units[1].Position = 4
	require.ErrorContains(t, persistSchedulerResolvedExecutionUnits(req, units), "has position 4")
}
