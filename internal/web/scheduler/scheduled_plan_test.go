package scheduler

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func completeTestScheduledRunUnits(req RunRequest) error {
	if req.OnUnit == nil || req.ConfirmedPlan == nil {
		return nil
	}
	for position := range req.ConfirmedPlan.ExecutionUnits {
		started := time.Now().UTC()
		if err := req.OnUnit(PipelineRunUnitEvent{
			Position: position, Status: PipelineRunUnitRunning, StartedAt: &started,
		}); err != nil {
			return err
		}
		finished := started.Add(time.Millisecond)
		if err := req.OnUnit(PipelineRunUnitEvent{
			Position: position, Status: PipelineRunUnitSuccess,
			StartedAt: &started, FinishedAt: &finished,
		}); err != nil {
			return err
		}
	}
	return nil
}

func testScheduledRunPlan(_ context.Context, req ScheduledRunPlanRequest) (ScheduledRunPlanResult, error) {
	planID := strings.Repeat("1", 64)
	sourceMerkle := strings.Repeat("2", 64)
	configurationDigest := strings.Repeat("3", 64)
	selection := PipelineRunPlanSelection{Mode: "all"}
	units := []PipelineRunExecutionUnit{{
		AssetID: "scheduled-asset-id", AssetName: "analytics.scheduled_asset",
		StartDate: req.Start.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		EndDate:   req.End.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
		Reason:    "all",
	}}
	executionTime := req.ExecutionTime.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	artifact, err := json.Marshal(map[string]any{
		"id":            planID,
		"pipeline_id":   req.PipelineID,
		"pipeline_uuid": req.PipelineUUID,
		"source":        map[string]any{"merkle_root": sourceMerkle},
		"context": map[string]any{
			"execution_time": executionTime, "configuration_digest": configurationDigest,
		},
		"selection": selection, "execution_units": units, "assets": []any{},
	})
	if err != nil {
		return ScheduledRunPlanResult{}, err
	}
	return ScheduledRunPlanResult{Plan: PipelineRunPlan{
		Version: PipelineRunPlanVersionV1, PlanID: planID,
		PipelineID: req.PipelineID, PipelineUUID: req.PipelineUUID,
		SourceMerkle: sourceMerkle, ConfigurationDigest: configurationDigest,
		ExecutionTime: executionTime, Selection: selection, ExecutionUnits: units,
		Artifact: artifact,
	}}, nil
}
