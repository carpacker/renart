package scheduler

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlineRunUsesDurableSpecSlotAndLifecycleWithoutRiver(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()

	var events []any
	service := New(Options{
		Store: store,
		Publish: func(event any) {
			events = append(events, event)
		},
	})
	ctx := context.Background()
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	run, err := service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
		Environment: "prod", Origin: RunTriggerAPI, Source: RunSourceWorkingTree,
		Start: start, End: end, ExecutionTime: start.Add(30 * time.Minute),
		VariableOverrides: map[string]any{"region": "eu"}, SensorMode: "once",
	})
	require.NoError(t, err)
	require.NotEmpty(t, run.ID)
	assert.Equal(t, RunStatusQueued, run.Status)
	assert.True(t, run.ExecutionContextResolved)
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM river_job`))

	spec, found, err := store.GetRunSpec(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, runDispatchInlineStreaming, spec.Dispatch)
	assert.Equal(t, RunTriggerAPI, spec.Origin)
	assert.Equal(t, runSelectionAll, spec.Selection)
	assert.Equal(t, map[string]any{"region": "eu"}, spec.Requested.Variables)
	assert.Equal(t, "pipeline-uuid", spec.Pipeline.UUID)
	_, _, executable, err := service.prepareRun(ctx, 0, pipelineRunJobArgs{RunID: run.ID})
	require.ErrorContains(t, err, "River worker cannot execute dispatch")
	assert.False(t, executable)

	_, err = service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
		Environment: "dev", Origin: RunTriggerCLI, Source: RunSourceWorkingTree,
		Start: start, End: end, SensorMode: "once",
	})
	var active *PipelineRunActiveError
	require.ErrorAs(t, err, &active)
	assert.Equal(t, run.ID, active.ActiveRunID)

	require.NoError(t, service.StartInlineRun(ctx, run.ID, start.Add(time.Minute)))
	snapshot := testExecutionTargetSnapshot()
	require.NoError(t, service.SetInlineRunExecutionTargetSnapshot(ctx, run.ID, snapshot))
	started := start.Add(2 * time.Minute)
	finished := start.Add(3 * time.Minute)
	require.NoError(t, service.RecordInlineRunStep(ctx, run.ID, RunStepEvent{
		Asset: "analytics.orders", Status: RunStatusRunning, StartedAt: &started,
	}))
	require.NoError(t, service.RecordInlineRunStep(ctx, run.ID, RunStepEvent{
		Asset: "analytics.orders", Status: RunStatusSuccess, StartedAt: &started, FinishedAt: &finished,
	}))
	require.NoError(t, service.AppendInlineRunLog(ctx, run.ID, "materialized analytics.orders\n"))
	require.NoError(t, service.FinishInlineRun(ctx, run.ID, RunStatusSuccess, nil))

	persisted, logs, steps, err := store.Get(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusSuccess, persisted.Status)
	assert.True(t, persisted.ExecutionContextResolved)
	require.NotNil(t, persisted.ExecutionTargetSnapshot)
	assert.Equal(t, snapshot.PipelineUUID, persisted.ExecutionTargetSnapshot.PipelineUUID)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Line, "analytics.orders")
	require.Len(t, steps, 1)
	assert.Equal(t, RunStatusSuccess, steps[0].Status)
	assert.GreaterOrEqual(t, len(events), 6)

	// Terminalization releases both path and stable-UUID aliases.
	retry, err := service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
		Environment: "dev", Origin: RunTriggerCLI, Source: RunSourceWorkingTree,
		Start: start, End: end, SensorMode: "once",
	})
	require.NoError(t, err)
	assert.Equal(t, RunTriggerCLI, retry.Trigger)
}

func TestScheduledRunSpecRejectsInlineDispatch(t *testing.T) {
	t.Parallel()
	_, _, spec, _ := scheduledOccurrenceFixture(t)
	spec.Dispatch = runDispatchInlineStreaming
	require.ErrorContains(t, spec.validate(), "scheduled run spec requires River dispatch")
}

func TestInlineAssetRunPersistsVersionedSelectionAndUnits(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	service := New(Options{Store: store})
	ctx := context.Background()
	start := time.Date(2026, 7, 18, 8, 0, 0, 123456789, time.UTC)
	end := start.Add(time.Hour)

	run, err := service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineUUID: "pipeline-uuid", PipelineName: "analytics",
		Environment: "prod", Origin: RunTriggerAPI, Source: RunSourceWorkingTree,
		Start: start, End: end, ExecutionTime: start.Add(time.Minute), SensorMode: "once",
		Selection: RunSelection{
			Mode: RunSelectionAsset, Scope: "asset_with_upstreams", AnchorAssetID: "asset-orders",
			Units: []RunSelectionUnit{
				{AssetID: "asset-customers", AssetName: "analytics.customers", AssetPath: "analytics/assets/customers.sql", Start: &start, End: &end, Reason: "required_upstream"},
				{AssetID: "asset-orders", AssetName: "analytics.orders", AssetPath: "analytics/assets/orders.sql", Start: &start, End: &end, Reason: "explicit"},
			},
		},
	})
	require.NoError(t, err)
	spec, found, err := store.GetRunSpec(ctx, run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, runSpecVersionV2, spec.Version)
	assert.Equal(t, runSelectionAsset, spec.Selection)
	require.NotNil(t, spec.SelectionDetails)
	assert.Equal(t, "asset_with_upstreams", spec.SelectionDetails.Scope)
	assert.Equal(t, "asset-orders", spec.SelectionDetails.AnchorAssetID)
	require.Len(t, spec.SelectionDetails.Units, 2)
	assert.Equal(t, "analytics/assets/customers.sql", spec.SelectionDetails.Units[0].AssetPath)

	units, err := store.ListRunUnits(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, units, 2)
	assert.Equal(t, []string{"analytics.customers", "analytics.orders"}, []string{units[0].AssetName, units[1].AssetName})
	assert.Equal(t, start.Format(time.RFC3339Nano), units[0].StartDate)
	assert.Equal(t, PipelineRunUnitQueued, units[0].Status)

	require.NoError(t, service.StartInlineRun(ctx, run.ID, start))
	require.NoError(t, service.RecordInlineRunUnit(ctx, run.ID, PipelineRunUnitEvent{Position: 0, Status: PipelineRunUnitRunning, StartedAt: &start}))
	require.NoError(t, service.RecordInlineRunUnit(ctx, run.ID, PipelineRunUnitEvent{Position: 0, Status: PipelineRunUnitSuccess, FinishedAt: &end}))
	require.NoError(t, service.RecordInlineRunUnit(ctx, run.ID, PipelineRunUnitEvent{Position: 1, Status: PipelineRunUnitRunning, StartedAt: &start}))
	require.NoError(t, service.RecordInlineRunUnit(ctx, run.ID, PipelineRunUnitEvent{Position: 1, Status: PipelineRunUnitSuccess, FinishedAt: &end}))
	require.NoError(t, service.FinishInlineRun(ctx, run.ID, RunStatusSuccess, nil))
}

func TestInlineAssetRunRejectsUnsafeOrInconsistentSelection(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	service := New(Options{Store: store})
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	_, err = service.AdmitInlineRun(context.Background(), InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineName: "analytics", Origin: RunTriggerAPI,
		Source: RunSourceWorkingTree, Start: start, End: end,
		Selection: RunSelection{
			Mode: RunSelectionAsset, Scope: "asset", AnchorAssetID: "asset-orders",
			Units: []RunSelectionUnit{{
				AssetID: "asset-orders", AssetName: "analytics.orders", AssetPath: "../orders.sql",
				Start: &start, End: &end, Reason: "explicit",
			}},
		},
	})
	require.ErrorContains(t, err, "workspace-relative")
	assert.Equal(t, 0, countRows(t, store, `SELECT COUNT(*) FROM pipeline_runs`))
}

func TestInterruptedInlineRunIsRecoveredWithoutPhysicalReplay(t *testing.T) {
	t.Parallel()
	store, err := OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	service := New(Options{Store: store})
	ctx := context.Background()
	start := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	run, err := service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineName: "analytics", Environment: "prod",
		Origin: RunTriggerAPI, Source: RunSourceWorkingTree,
		Start: start, End: start.Add(time.Hour), SensorMode: "once",
	})
	require.NoError(t, err)
	require.NoError(t, service.StartInlineRun(ctx, run.ID, start.Add(time.Minute)))

	recovery, err := store.ReconcileInterruptedState(ctx, orphanedRunError)
	require.NoError(t, err)
	assert.Equal(t, []string{run.ID}, recovery.RunIDs)
	assert.Zero(t, recovery.RiverJobsRequeued)
	assert.Zero(t, recovery.RiverJobsCancelled)
	persisted, _, _, err := store.Get(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, RunStatusFailed, persisted.Status)
	assert.True(t, strings.Contains(persisted.Error, "interrupted"))

	_, err = service.AdmitInlineRun(ctx, InlineRunAdmission{
		PipelineID: "pipeline-id", PipelineName: "analytics", Environment: "prod",
		Origin: RunTriggerAPI, Source: RunSourceWorkingTree,
		Start: start, End: start.Add(time.Hour), SensorMode: "once",
	})
	require.NoError(t, err)
}
