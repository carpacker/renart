package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/bus"
	"renart/internal/web/policy"
	webscheduler "renart/internal/web/scheduler"
)

type stubInlineRunLedger struct {
	admission webscheduler.InlineRunAdmission
	admitErr  error
	started   bool
	targets   []webscheduler.ExecutionTargetSnapshot
	steps     []webscheduler.RunStepEvent
	units     []webscheduler.PipelineRunUnitEvent
	logs      []string
	status    webscheduler.RunStatus
	finishErr error
}

func (s *stubInlineRunLedger) AdmitInlineRun(_ context.Context, admission webscheduler.InlineRunAdmission) (webscheduler.PipelineRun, error) {
	s.admission = admission
	if s.admitErr != nil {
		return webscheduler.PipelineRun{}, s.admitErr
	}
	return webscheduler.PipelineRun{ID: "inline-run-id", PipelineID: admission.PipelineID}, nil
}

func (s *stubInlineRunLedger) StartInlineRun(context.Context, string, time.Time) error {
	s.started = true
	return nil
}

func (s *stubInlineRunLedger) SetInlineRunExecutionTargetSnapshot(_ context.Context, _ string, snapshot webscheduler.ExecutionTargetSnapshot) error {
	s.targets = append(s.targets, snapshot)
	return nil
}

func (s *stubInlineRunLedger) RecordInlineRunStep(_ context.Context, _ string, event webscheduler.RunStepEvent) error {
	s.steps = append(s.steps, event)
	return nil
}

func (s *stubInlineRunLedger) RecordInlineRunUnit(_ context.Context, _ string, event webscheduler.PipelineRunUnitEvent) error {
	s.units = append(s.units, event)
	return nil
}

func (s *stubInlineRunLedger) AppendInlineRunLog(_ context.Context, _ string, line string) error {
	s.logs = append(s.logs, line)
	return nil
}

func (s *stubInlineRunLedger) FinishInlineRun(_ context.Context, _ string, status webscheduler.RunStatus, _ error) error {
	s.status = status
	return s.finishErr
}

func TestExecutionServiceRecordsInlinePipelineRunWithoutChangingStreaming(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	executor := &stubExecutionExecutor{
		runPipelineOutput: []byte("pipeline complete\n"),
		runPipelineChunks: [][]byte{[]byte("pipeline "), []byte("complete\n")},
		runPipelineEvents: []ExecutionAssetEvent{
			{Asset: "analytics.orders", Status: "running", StartedAt: &started},
			{Asset: "analytics.orders", Status: "success", StartedAt: &started, FinishedAt: &finished},
		},
		runPipelineTargets: &ExecutionTargetSnapshot{
			Version: ExecutionTargetSnapshotVersion, PipelineUUID: "orders-uuid",
			Entries: map[string]ExecutionTargetSnapshotEntry{
				"analytics.orders": {
					AssetID: "orders-uuid:analytics.orders", TargetIdentity: "target-orders",
					TargetFidelity: AssetRenderFidelityExact, Fingerprint: "v2:orders",
					OwnContent: "v2:orders-own", ConsumedVarsHash: "consumed", VarsHash: "vars",
					CoverageMode: ExecutionCoverageMarker,
				},
			},
		},
	}
	ledger := &stubInlineRunLedger{}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) error {
		completed = event
		return nil
	})
	svc := NewExecutionService(ExecutionDependencies{
		Executor:   executor,
		InlineRuns: ledger,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID: pipelineID, UUID: "orders-uuid", Name: "analytics",
				Assets: []AssetView{{ID: "asset-id", Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})
	var streamed []string
	ctx := WithExecutionOrigin(context.Background(), webscheduler.RunTriggerCLI)
	result := svc.MaterializePipelineStream(ctx, pipelineID, "prod", false, false, false, "", "", "", func(chunk []byte) {
		streamed = append(streamed, string(chunk))
	})

	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "pipeline complete\n", result.Output)
	assert.Equal(t, []string{"pipeline ", "complete\n"}, streamed)
	assert.Equal(t, webscheduler.RunTriggerCLI, ledger.admission.Origin)
	assert.Equal(t, "orders-uuid", ledger.admission.PipelineUUID)
	assert.Equal(t, "analytics", ledger.admission.PipelineName)
	assert.True(t, ledger.admission.Start.Before(ledger.admission.End))
	assert.True(t, ledger.started)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, "inline-run-id", executor.runPipelineReqs[0].RunID)
	require.Len(t, ledger.targets, 1)
	assert.Equal(t, "orders-uuid", ledger.targets[0].PipelineUUID)
	require.Len(t, ledger.steps, 2)
	assert.Equal(t, webscheduler.RunStatusRunning, ledger.steps[0].Status)
	assert.Equal(t, webscheduler.RunStatusSuccess, ledger.steps[1].Status)
	assert.Equal(t, []string{"pipeline ", "complete\n"}, ledger.logs)
	assert.Equal(t, webscheduler.RunStatusSuccess, ledger.status)
	assert.Equal(t, "inline-run-id", completed.RunID)
	assert.Equal(t, "inline-run-id", completed.CompletionID)
}

func TestExecutionServicePersistsInlinePipelineThroughSchedulerLedger(t *testing.T) {
	t.Parallel()
	store, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ledger := webscheduler.New(webscheduler.Options{Store: store})
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	started := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	executor := &stubExecutionExecutor{
		runPipelineOutput: []byte("pipeline complete\n"),
		runPipelineChunks: [][]byte{[]byte("pipeline complete\n")},
		runPipelineEvents: []ExecutionAssetEvent{
			{Asset: "analytics.orders", Status: "running", StartedAt: &started},
			{Asset: "analytics.orders", Status: "success", StartedAt: &started, FinishedAt: &finished},
		},
		runPipelineTargets: &ExecutionTargetSnapshot{
			Version: ExecutionTargetSnapshotVersion, PipelineUUID: "orders-uuid",
			Entries: map[string]ExecutionTargetSnapshotEntry{
				"analytics.orders": {
					AssetID: "orders-uuid:analytics.orders", TargetIdentity: "target-orders",
					TargetFidelity: AssetRenderFidelityExact, Fingerprint: "v2:orders",
					OwnContent: "v2:orders-own", ConsumedVarsHash: "consumed", VarsHash: "vars",
					CoverageMode: ExecutionCoverageMarker,
				},
			},
		},
	}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor, InlineRuns: ledger,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID: pipelineID, UUID: "orders-uuid", Name: "analytics",
				Assets: []AssetView{{ID: "asset-id", Name: "analytics.orders"}},
			}}
		},
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "prod", false, false, false, "", "", "", nil)
	require.Equal(t, "ok", result.Status, result.Error)
	runs, err := store.List(context.Background(), webscheduler.RunFilter{PipelineID: pipelineID})
	require.NoError(t, err)
	require.Len(t, runs.Runs, 1)
	run := runs.Runs[0]
	assert.Equal(t, webscheduler.RunStatusSuccess, run.Status)
	assert.Equal(t, webscheduler.RunTriggerAPI, run.Trigger)
	assert.True(t, run.ExecutionContextResolved)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, run.ID, executor.runPipelineReqs[0].RunID)
	_, logs, steps, err := store.Get(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Contains(t, logs[0].Line, "pipeline complete")
	require.Len(t, steps, 1)
	assert.Equal(t, webscheduler.RunStatusSuccess, steps[0].Status)
	spec, found, err := store.GetRunSpec(context.Background(), run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "inline_streaming", spec.Dispatch)
	assert.Equal(t, "orders-uuid", spec.Pipeline.UUID)
	var riverJobs int
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM river_job`).Scan(&riverJobs))
	assert.Zero(t, riverJobs)
}

func TestExecutionServicePersistsInlineAssetSelectionThroughSchedulerLedger(t *testing.T) {
	t.Parallel()
	store, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ledger := webscheduler.New(webscheduler.Options{Store: store})
	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	start := time.Date(2026, 7, 18, 8, 0, 0, 123456789, time.UTC)
	end := start.Add(time.Hour)
	finished := start.Add(time.Second)
	executor := &stubExecutionExecutor{
		runAssetOutput: []byte("asset complete\n"),
		runAssetChunks: [][]byte{[]byte("asset complete\n")},
		runAssetEvents: []ExecutionAssetEvent{
			{Asset: "analytics.orders", Status: "running", StartedAt: &start},
			{Asset: "analytics.orders", Status: "success", StartedAt: &start, FinishedAt: &finished},
		},
		runAssetTargets: &ExecutionTargetSnapshot{
			Version: ExecutionTargetSnapshotVersion, PipelineUUID: "orders-uuid",
			Entries: map[string]ExecutionTargetSnapshotEntry{
				"analytics.orders": {
					AssetID: "orders-uuid:analytics.orders", TargetIdentity: "target-orders",
					TargetFidelity: AssetRenderFidelityExact, Fingerprint: "v2:orders",
					OwnContent: "v2:orders-own", ConsumedVarsHash: "consumed", VarsHash: "vars",
					CoverageMode: ExecutionCoverageMarker,
				},
			},
		},
	}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) error { completed = event; return nil })
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor, InlineRuns: ledger, Events: events,
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(ids ...string) []string { return ids },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID: pipelineID, UUID: "orders-uuid", Name: "analytics",
				Assets: []AssetView{{ID: assetID, Name: "analytics.orders"}},
			}}
		},
	})

	result := svc.MaterializeAssetStream(
		WithExecutionOrigin(context.Background(), webscheduler.RunTriggerCLI),
		assetID, "prod", "asset", start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano),
		false, false, "", nil,
	)
	require.Equal(t, "ok", result.Status, result.Error)
	runs, err := store.List(context.Background(), webscheduler.RunFilter{PipelineID: pipelineID})
	require.NoError(t, err)
	require.Len(t, runs.Runs, 1)
	run := runs.Runs[0]
	assert.Equal(t, webscheduler.RunStatusSuccess, run.Status)
	assert.Equal(t, webscheduler.RunTriggerCLI, run.Trigger)
	assert.Equal(t, run.ID, completed.RunID)
	assert.Equal(t, run.ID, completed.CompletionID)
	units, err := store.ListRunUnits(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, units, 1)
	assert.Equal(t, "analytics.orders", units[0].AssetName)
	assert.Equal(t, "explicit", units[0].Reason)
	assert.Equal(t, start.Format(time.RFC3339Nano), units[0].StartDate)
	assert.Equal(t, webscheduler.PipelineRunUnitSuccess, units[0].Status)
	_, logs, steps, err := store.Get(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Len(t, steps, 1)
	assert.Equal(t, webscheduler.RunStatusSuccess, steps[0].Status)
	var riverJobs int
	require.NoError(t, store.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM river_job`).Scan(&riverJobs))
	assert.Zero(t, riverJobs)
}

func TestExecutionServiceDoesNotExecuteWhenInlineAdmissionFails(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executed := false
	executor := &stubExecutionExecutor{onRunPipeline: func() { executed = true }}
	ledger := &stubInlineRunLedger{admitErr: errors.New("pipeline already active")}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor, InlineRuns: ledger,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{ID: pipelineID, UUID: "orders-uuid", Name: "analytics"}}
		},
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "prod", false, false, false, "", "", "", nil)
	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "admit durable inline run")
	assert.False(t, executed)
	assert.False(t, ledger.started)
}

func TestExecutionServiceRechecksPolicyAfterInlineAdmission(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executed := false
	checks := 0
	executor := &stubExecutionExecutor{onRunPipeline: func() { executed = true }}
	ledger := &stubInlineRunLedger{}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor, InlineRuns: ledger,
		PolicyFor: func(string) policy.EnvironmentPolicy {
			checks++
			if checks > 1 {
				return policy.EnvironmentPolicy{Protected: true}
			}
			return policy.EnvironmentPolicy{}
		},
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{ID: pipelineID, UUID: "orders-uuid", Name: "analytics"}}
		},
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "prod", false, false, false, "", "", "", nil)
	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "protected")
	assert.Equal(t, 2, checks)
	assert.True(t, ledger.started)
	assert.Equal(t, webscheduler.RunStatusFailed, ledger.status)
	assert.False(t, executed)
}
