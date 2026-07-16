package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	webscheduler "renart/internal/web/scheduler"
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
}
