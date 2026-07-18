package scheduler

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReexecuteRunRetainsExactPlanAndStripsScheduleAuthority(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	start := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	executionTime := end
	plan := validPipelineRunPlan(t)
	plan.ExecutionTime = executionTime.Format(time.RFC3339Nano)
	plan.Artifact = pipelineRunPlanArtifact(t, plan)
	original := PipelineRun{
		ID: "scheduled-original", PipelineID: plan.PipelineID, PipelineUUID: plan.PipelineUUID,
		Pipeline: "analytics", Environment: "prod", Trigger: RunTriggerSchedule,
		Status: RunStatusQueued, WinStart: &start, WinEnd: &end,
		SnapshotVersionID: "snapshot-id", FullRefresh: true, SensorMode: "wait",
		ExecutionTime: &executionTime, VariableOverrides: map[string]any{"region": "eu"},
	}
	spec := scheduledRunSpec(original, pipelineRunJobArgs{
		PipelineUUID: original.PipelineUUID, Environment: original.Environment,
		Schedule: "@hourly", Timezone: "UTC", SnapshotVersionID: original.SnapshotVersionID,
		Variables: original.VariableOverrides, FullRefresh: true,
		ConfirmedEnvironment: "prod", SensorMode: "wait",
	})
	spec.Expected = &runExpectedIdentity{
		SourceMerkle: plan.SourceMerkle, ConfigurationDigest: plan.ConfigurationDigest,
	}
	ctx := context.Background()
	originalID, err := store.CreateWithSpecAndPlan(ctx, original, spec, plan)
	require.NoError(t, err)
	require.Equal(t, original.ID, originalID)
	require.NoError(t, store.UpdateRunUnit(ctx, original.ID, PipelineRunUnitEvent{
		Position: 0, Status: PipelineRunUnitRunning, StartedAt: &start,
	}))
	require.NoError(t, store.UpdateRunUnit(ctx, original.ID, PipelineRunUnitEvent{
		Position: 0, Status: PipelineRunUnitSuccess, FinishedAt: &end,
	}))
	require.NoError(t, store.FinalizeExecution(ctx, original.ID, RunStatusSuccess, end.Add(time.Minute), nil, "", nil))

	requests := make(chan RunRequest, 1)
	release := make(chan struct{})
	var validation RunReexecutionValidationRequest
	service := New(Options{
		Store: store, StateDir: stateDir,
		ValidateReexecution: func(_ context.Context, req RunReexecutionValidationRequest) error {
			validation = req
			return nil
		},
		Runner: func(_ context.Context, req RunRequest, _ func(string)) RunResult {
			requests <- req
			<-release
			return RunResult{Status: "ok"}
		},
	})
	serviceCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, service.Start(serviceCtx))
	defer service.Stop()
	defer close(release)

	availability, err := service.GetRunReexecution(ctx, original.ID)
	require.NoError(t, err)
	assert.Equal(t, PipelineRunReexecutionExact, availability.Mode)
	assert.Equal(t, "all", availability.Selection)
	assert.Equal(t, 1, availability.ExecutionUnits)
	assert.Equal(t, original.ID, validation.OriginalRunID)
	assert.Equal(t, map[string]any{"region": "eu"}, validation.VariableOverrides)
	assert.Equal(t, []string{"analytics.orders"}, validation.ConfigurationAssetNames)

	replayed, err := service.ReexecuteRun(ctx, original.ID)
	require.NoError(t, err)
	assert.NotEqual(t, original.ID, replayed.ID)
	assert.Equal(t, RunTriggerManual, replayed.Trigger)
	assert.Equal(t, original.Environment, replayed.Environment)
	assert.Equal(t, original.SnapshotVersionID, replayed.SnapshotVersionID)
	assert.True(t, replayed.FullRefresh)
	assert.Equal(t, "wait", replayed.SensorMode)

	replayedSpec, found, err := store.GetRunSpec(ctx, replayed.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, RunTriggerManual, replayedSpec.Origin)
	assert.Equal(t, runDispatchRiver, replayedSpec.Dispatch)
	assert.Nil(t, replayedSpec.Schedule, "manual re-execution must never inherit schedule watermark authority")
	assert.Equal(t, spec.Requested, replayedSpec.Requested)
	assert.Equal(t, spec.Expected, replayedSpec.Expected)
	assert.Equal(t, spec.Authorization, replayedSpec.Authorization)
	replayedPlan, found, err := store.GetRunPlan(ctx, replayed.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, plan, replayedPlan)

	var argsJSON string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT json(args) FROM river_job WHERE id = ?`, *replayed.RiverJobID).Scan(&argsJSON))
	assert.JSONEq(t, fmt.Sprintf(`{"run_id":%q}`, replayed.ID), argsJSON)

	select {
	case request := <-requests:
		assert.False(t, request.Scheduled)
		assert.Equal(t, map[string]any{"region": "eu"}, request.VariableOverrides)
		assert.Equal(t, "prod", request.ConfirmedEnvironment)
		require.NotNil(t, request.ConfirmedPlan)
		assert.Equal(t, plan, *request.ConfirmedPlan)
	case <-time.After(2 * time.Second):
		t.Fatal("re-executed run did not reach the runner")
	}
}

func TestGetRunReexecutionUsesCurrentSettingsWhenPlanOrContextIsUnavailable(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	start := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	run := PipelineRun{
		ID: "inline-original", PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid",
		Pipeline: "analytics", Environment: "default", Trigger: RunTriggerAPI,
		Status: RunStatusQueued, WinStart: &start, WinEnd: &end,
	}
	_, err = store.CreateWithSpec(context.Background(), run, inlineRunSpec(run, RunSourceWorkingTree, ""))
	require.NoError(t, err)
	require.NoError(t, store.FinalizeExecution(context.Background(), run.ID, RunStatusSuccess, end, nil, "", nil))

	service := New(Options{Store: store})
	availability, err := service.GetRunReexecution(context.Background(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, PipelineRunReexecutionCurrentSettings, availability.Mode)
	assert.Contains(t, availability.Reason, "execution plan was not retained")

	plan := validPipelineRunPlan(t)
	plan.Artifact = pipelineRunPlanArtifact(t, plan)
	plannedRun := PipelineRun{
		ID: "drifted-original", PipelineID: plan.PipelineID, PipelineUUID: plan.PipelineUUID,
		Pipeline: "analytics", Environment: "default", Trigger: RunTriggerManual,
		Status: RunStatusQueued, WinStart: &start, WinEnd: &end, ExecutionTime: &end,
	}
	plannedSpec := manualRunSpec(plannedRun, RunSourceWorkingTree, "")
	plannedSpec.Expected = &runExpectedIdentity{SourceMerkle: plan.SourceMerkle, ConfigurationDigest: plan.ConfigurationDigest}
	_, err = store.CreateWithSpecAndPlan(context.Background(), plannedRun, plannedSpec, plan)
	require.NoError(t, err)
	require.NoError(t, store.FinalizeExecution(context.Background(), plannedRun.ID, RunStatusFailed, end, errors.New("source changed"), "", nil))

	drifted := New(Options{
		Store: store,
		ValidateReexecution: func(context.Context, RunReexecutionValidationRequest) error {
			return fmt.Errorf("the original source has changed")
		},
	})
	availability, err = drifted.GetRunReexecution(context.Background(), plannedRun.ID)
	require.NoError(t, err)
	assert.Equal(t, PipelineRunReexecutionCurrentSettings, availability.Mode)
	assert.Equal(t, "the original source has changed", availability.Reason)
}

func TestReexecuteRunRevalidatesAfterCapabilityRead(t *testing.T) {
	stateDir := t.TempDir()
	store, err := OpenStore(filepath.Join(stateDir, "state.db"))
	require.NoError(t, err)
	defer store.Close()

	start := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	plan := validPipelineRunPlan(t)
	plan.ExecutionTime = end.Format(time.RFC3339Nano)
	plan.Artifact = pipelineRunPlanArtifact(t, plan)
	original := PipelineRun{
		ID: "validation-race-original", PipelineID: plan.PipelineID, PipelineUUID: plan.PipelineUUID,
		Pipeline: "analytics", Environment: "default", Trigger: RunTriggerManual,
		Status: RunStatusQueued, WinStart: &start, WinEnd: &end, ExecutionTime: &end,
	}
	spec := manualRunSpec(original, RunSourceWorkingTree, "")
	spec.Expected = &runExpectedIdentity{
		SourceMerkle: plan.SourceMerkle, ConfigurationDigest: plan.ConfigurationDigest,
	}
	_, err = store.CreateWithSpecAndPlan(context.Background(), original, spec, plan)
	require.NoError(t, err)
	require.NoError(t, store.FinalizeExecution(
		context.Background(), original.ID, RunStatusFailed, end, errors.New("original run failed"), "", nil,
	))

	validationCalls := 0
	service := New(Options{
		Store: store, StateDir: stateDir,
		ValidateReexecution: func(context.Context, RunReexecutionValidationRequest) error {
			validationCalls++
			if validationCalls > 1 {
				return errors.New("the original source has changed")
			}
			return nil
		},
		Runner: func(context.Context, RunRequest, func(string)) RunResult {
			return RunResult{Status: "ok"}
		},
	})
	serviceCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, service.Start(serviceCtx))
	defer service.Stop()

	availability, err := service.GetRunReexecution(context.Background(), original.ID)
	require.NoError(t, err)
	require.Equal(t, PipelineRunReexecutionExact, availability.Mode)

	_, err = service.ReexecuteRun(context.Background(), original.ID)
	var unavailable *ExactReexecutionUnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, "the original source has changed", unavailable.Reason)
	assert.Equal(t, 2, validationCalls)
	runs, err := service.ListRuns(context.Background(), RunFilter{})
	require.NoError(t, err)
	assert.Len(t, runs.Runs, 1, "failed revalidation must not admit a partial run")
}

func TestReexecutionConfigurationAssetNamesUsesFinalNeededUnits(t *testing.T) {
	plan := PipelineRunPlan{
		ExecutionUnits: []PipelineRunExecutionUnit{
			{AssetName: "analytics.customers"},
			{AssetName: "analytics.customers"},
		},
		Preview: &PipelineRunPlanPreview{
			ExecutionUnits: []PipelineRunExecutionUnit{
				{AssetName: "analytics.customers"},
				{AssetName: "analytics.orders"},
			},
		},
	}

	assert.Equal(t, []string{"analytics.customers"}, reexecutionConfigurationAssetNames(plan))
}
