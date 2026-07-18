package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
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

func TestExecutionServicePersistsExactNeededWindowsAndDownstreamSkips(t *testing.T) {
	t.Parallel()
	_, workspaceRoot := writeTypeCheckWorkspace(t, `
name: analytics
id: 0b73db88-ab55-4ed1-8f50-ef38089fc2d2
schedule: daily
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"orders.sql": `
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: table
@bruin */
select 1 as id
`,
		"report.sql": `
/* @bruin
name: analytics.report
type: duckdb.sql
depends:
  - analytics.orders
materialization:
  type: table
@bruin */
select * from analytics.orders
`,
	})
	store, err := webscheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	defer store.Close()
	ledger := webscheduler.New(webscheduler.Options{Store: store})
	pipelineID := EncodeID("analytics/pipeline.yml")
	ordersID := EncodeID("analytics/assets/orders.sql")
	reportID := EncodeID("analytics/assets/report.sql")
	start := time.Date(2026, 7, 16, 0, 0, 0, 123456789, time.UTC)
	middle := start.Add(24 * time.Hour)
	end := middle.Add(24 * time.Hour)
	firstFinished := start.Add(time.Second)
	secondStarted := middle.Add(time.Second)
	secondFinished := secondStarted.Add(time.Second)
	call := 0
	executor := &stubExecutionExecutor{}
	executor.onRunAsset = func() {
		call++
		executor.runAssetOutput = []byte("window output\n")
		executor.runAssetTargets = &ExecutionTargetSnapshot{
			Version: ExecutionTargetSnapshotVersion, PipelineUUID: "0b73db88-ab55-4ed1-8f50-ef38089fc2d2",
			Entries: map[string]ExecutionTargetSnapshotEntry{
				"analytics.orders": {
					AssetID:        "0b73db88-ab55-4ed1-8f50-ef38089fc2d2:analytics.orders",
					TargetIdentity: "target-orders", TargetFidelity: AssetRenderFidelityExact,
					Fingerprint: "v2:orders", OwnContent: "v2:orders-own",
					ConsumedVarsHash: "consumed", VarsHash: "vars", CoverageMode: ExecutionCoverageMarker,
				},
				"analytics.report": {
					AssetID:        "0b73db88-ab55-4ed1-8f50-ef38089fc2d2:analytics.report",
					TargetIdentity: "target-report", TargetFidelity: AssetRenderFidelityExact,
					Fingerprint: "v2:report", OwnContent: "v2:report-own",
					ConsumedVarsHash: "consumed", VarsHash: "vars", CoverageMode: ExecutionCoverageMarker,
				},
			},
		}
		if call == 1 {
			executor.runAssetErr = nil
			executor.runAssetEvents = []ExecutionAssetEvent{
				{Asset: "analytics.orders", Status: "running", StartedAt: &start},
				{Asset: "analytics.orders", Status: "success", StartedAt: &start, FinishedAt: &firstFinished},
			}
			return
		}
		executor.runAssetErr = errors.New("second gap failed")
		executor.runAssetEvents = []ExecutionAssetEvent{
			{Asset: "analytics.orders", Status: "running", StartedAt: &secondStarted},
			{Asset: "analytics.orders", Status: "failed", StartedAt: &secondStarted, FinishedAt: &secondFinished, Error: "second gap failed"},
		}
	}
	events := bus.New()
	completed := make([]bus.RunCompleted, 0, 2)
	events.OnRunCompleted(func(event bus.RunCompleted) error {
		completed = append(completed, event)
		return nil
	})
	svc := NewExecutionService(ExecutionDependencies{
		WorkspaceRoot: workspaceRoot, Executor: executor, InlineRuns: ledger, Events: events,
		NewPipelineBuilder: func() *pipeline.Builder { return NewRenartPipelineBuilder(afero.NewOsFs()) },
		FindInspectIDs:     func(ids ...string) []string { return ids },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID: pipelineID, UUID: "0b73db88-ab55-4ed1-8f50-ef38089fc2d2", Name: "analytics",
				Assets: []AssetView{{ID: ordersID, Name: "analytics.orders"}, {ID: reportID, Name: "analytics.report"}},
			}}
		},
	})

	result := svc.MaterializeStaleAssetsStream(
		context.Background(), pipelineID, "prod",
		[]StaleAssetPlan{
			{AssetName: "analytics.report", Reason: "stale_upstream"},
			{AssetName: "analytics.orders", Reason: "uncovered_interval", Windows: []ExecutionTimeWindow{{Start: start, End: middle}, {Start: middle, End: end}}},
		},
		start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano), nil, nil,
	)
	require.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "analytics.orders")
	assert.Equal(t, 2, call, "the downstream asset must not execute after its upstream failed")

	runs, err := store.List(context.Background(), webscheduler.RunFilter{PipelineID: pipelineID})
	require.NoError(t, err)
	require.Len(t, runs.Runs, 1)
	run := runs.Runs[0]
	assert.Equal(t, webscheduler.RunStatusFailed, run.Status)
	spec, found, err := store.GetRunSpec(context.Background(), run.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 2, spec.Version)
	assert.Equal(t, "needed", spec.Selection)
	require.NotNil(t, spec.SelectionDetails)
	require.Len(t, spec.SelectionDetails.Units, 3)
	assert.Equal(t, "analytics/assets/orders.sql", spec.SelectionDetails.Units[0].AssetPath)
	units, err := store.ListRunUnits(context.Background(), run.ID)
	require.NoError(t, err)
	require.Len(t, units, 3)
	assert.Equal(t, []string{"analytics.orders", "analytics.orders", "analytics.report"}, []string{units[0].AssetName, units[1].AssetName, units[2].AssetName})
	assert.Equal(t, []webscheduler.PipelineRunUnitStatus{
		webscheduler.PipelineRunUnitSuccess, webscheduler.PipelineRunUnitFailed, webscheduler.PipelineRunUnitSkipped,
	}, []webscheduler.PipelineRunUnitStatus{units[0].Status, units[1].Status, units[2].Status})
	assert.Equal(t, start.Format(time.RFC3339Nano), units[0].StartDate)
	assert.Equal(t, middle.Format(time.RFC3339Nano), units[1].StartDate)
	assert.Equal(t, "uncovered_interval", units[1].Reason)
	assert.Equal(t, "stale_upstream", units[2].Reason)
	require.Len(t, completed, 2)
	assert.Equal(t, run.ID+"/unit/0", completed[0].CompletionID)
	assert.Equal(t, run.ID+"/unit/1", completed[1].CompletionID)
	assert.Equal(t, run.ID, completed[0].RunID)
	_, logs, steps, err := store.Get(context.Background(), run.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, logs)
	require.Len(t, steps, 1)
	assert.Equal(t, "analytics.orders", steps[0].Asset)
	assert.Equal(t, webscheduler.RunStatusFailed, steps[0].Status)
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
