package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/policy"
)

type stubRunRunner struct {
	args               []string
	output             []byte
	err                error
	runAssetRequest    RunAssetRequest
	runPipelineRequest RunPipelineRequest
}

type stubRunMaterializer struct {
	assetID              string
	assetEnvironment     string
	assetScope           string
	assetStartDate       string
	assetEndDate         string
	assetFullRefresh     bool
	assetBackfill        bool
	assetConfirmation    string
	assetSensorMode      string
	pipelineID           string
	pipelineEnvironment  string
	pipelineDryRun       bool
	pipelineFullRefresh  bool
	pipelineBackfill     bool
	pipelineStartDate    string
	pipelineEndDate      string
	pipelineConfirmation string
	pipelineSensorMode   string
	pipelineIDs          []string
	pipelineResults      map[string]MaterializeResult
	result               MaterializeResult
}

func (s *stubRunMaterializer) MaterializeAssetStreamWithSensorMode(
	_ context.Context,
	assetID, environment, scope, startDate, endDate string,
	fullRefresh, backfill bool,
	confirmedEnvironment, sensorMode string,
	_ func([]byte),
) MaterializeResult {
	s.assetID = assetID
	s.assetEnvironment = environment
	s.assetScope = scope
	s.assetStartDate = startDate
	s.assetEndDate = endDate
	s.assetFullRefresh = fullRefresh
	s.assetBackfill = backfill
	s.assetConfirmation = confirmedEnvironment
	s.assetSensorMode = sensorMode
	return s.result
}

func (s *stubRunMaterializer) MaterializePipelineStreamWithSensorMode(
	_ context.Context,
	pipelineID, environment string,
	dryRun, fullRefresh, backfill bool,
	startDate, endDate, confirmedEnvironment, sensorMode string,
	_ func([]byte),
) MaterializeResult {
	s.pipelineIDs = append(s.pipelineIDs, pipelineID)
	s.pipelineID = pipelineID
	s.pipelineEnvironment = environment
	s.pipelineDryRun = dryRun
	s.pipelineFullRefresh = fullRefresh
	s.pipelineBackfill = backfill
	s.pipelineStartDate = startDate
	s.pipelineEndDate = endDate
	s.pipelineConfirmation = confirmedEnvironment
	s.pipelineSensorMode = sensorMode
	if result, ok := s.pipelineResults[pipelineID]; ok {
		return result
	}
	return s.result
}

func (s *stubRunRunner) RunAsset(_ context.Context, req RunAssetRequest, _ func([]byte)) ([]byte, error) {
	s.runAssetRequest = req
	s.args = []string{"run", req.AssetPath}
	if req.Environment != "" {
		s.args = append(s.args, "--env", req.Environment)
	}
	return s.output, s.err
}

func (s *stubRunRunner) RunPipeline(_ context.Context, req RunPipelineRequest, _ func([]byte)) ([]byte, error) {
	s.runPipelineRequest = req
	s.args = []string{"run", req.Target}
	if req.Environment != "" {
		s.args = append(s.args, "--env", req.Environment)
	}
	return s.output, s.err
}

func (s *stubRunRunner) QueryAsset(_ context.Context, req QueryAssetRequest) ([]byte, error) {
	s.args = []string{"query", req.AssetPath}
	return s.output, s.err
}

func (s *stubRunRunner) QueryConnection(_ context.Context, req QueryConnectionRequest) ([]byte, error) {
	s.args = []string{"query", "--connection", req.ConnectionName, "--query", req.Query}
	return s.output, s.err
}

func (s *stubRunRunner) FormatAsset(_ context.Context, req FormatAssetRequest) ([]byte, error) {
	s.args = []string{"format", req.AssetPath}
	if req.UseSQLFluff {
		s.args = append(s.args, "--sqlfluff")
	}
	return s.output, s.err
}

func (s *stubRunRunner) ApplyPatch(_ context.Context, req PatchRequest) ([]byte, error) {
	s.args = []string{"patch", req.Operation, req.TargetPath}
	return s.output, s.err
}

func (s *stubRunRunner) ImportDatabase(_ context.Context, req ImportDatabaseRequest) ([]byte, error) {
	s.args = []string{"import", "database", req.PipelinePath}
	return s.output, s.err
}

func (s *stubRunRunner) RunWithRetry(_ context.Context, req QueryAssetRequest, _ int, _ time.Duration) ([]byte, error, int) {
	s.args = []string{"query", req.AssetPath}
	return s.output, s.err, 1
}

func TestRunServiceExecute_DefaultsToRunCommand(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("ok")}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{})

	require.Equal(t, []string{"run", "."}, runner.args)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, 200, result.HTTPCode)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "run", result.Operation.Type)
	assert.Equal(t, ".", result.Operation.Target)
	assert.Equal(t, "ok", result.Output)
}

func TestRunServiceExecute_UsesPipelineTarget(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("done")}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{
		PipelineID:  EncodeID("pipelines/orders/pipeline.yml"),
		Environment: "staging",
	})

	require.Equal(t, []string{"run", "pipelines/orders", "--env", "staging"}, runner.args)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "run", result.Operation.Type)
	assert.Equal(t, "pipelines/orders", result.Operation.Target)
	assert.Equal(t, "staging", result.Operation.Environment)
}

func TestRunServiceExecute_AssetPathOverridesPipelineID(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("asset")}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{
		PipelineID: EncodeID("pipelines/orders/pipeline.yml"),
		AssetPath:  "pipelines/orders/assets/order_items.sql",
	})

	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "pipelines/orders/assets/order_items.sql", result.Operation.Target)
	assert.Equal(t, []string{"run", "pipelines/orders/assets/order_items.sql"}, runner.args)
}

func TestRunServiceExecute_UsesCompletionAwarePipelineMaterializer(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("legacy path must not run")}
	materializer := &stubRunMaterializer{result: MaterializeResult{
		Status:    "ok",
		Operation: runOperation("internal-execution-target", "", "", "staging"),
		Output:    "materialized",
	}}
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	svc := NewRunService(RunDependencies{Executor: runner, Execution: materializer})

	result := svc.Execute(context.Background(), RunRequest{
		PipelineID:           pipelineID,
		Environment:          "staging",
		FullRefresh:          true,
		ConfirmedEnvironment: "staging",
		SensorMode:           "skip",
	})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, 200, result.HTTPCode)
	assert.Equal(t, "materialized", result.Output)
	assert.Equal(t, "pipelines/orders", result.Operation.Target)
	assert.Nil(t, runner.args)
	assert.Equal(t, pipelineID, materializer.pipelineID)
	assert.Equal(t, "staging", materializer.pipelineEnvironment)
	assert.False(t, materializer.pipelineDryRun)
	assert.True(t, materializer.pipelineFullRefresh)
	assert.False(t, materializer.pipelineBackfill)
	assert.Equal(t, "staging", materializer.pipelineConfirmation)
	assert.Equal(t, "skip", materializer.pipelineSensorMode)
}

func TestRunServiceExecute_UsesCompletionAwareAssetMaterializer(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("legacy path must not run")}
	materializer := &stubRunMaterializer{result: MaterializeResult{
		Status:   "error",
		Output:   "partial output",
		Error:    "materialization failed",
		ExitCode: 1,
	}}
	svc := NewRunService(RunDependencies{Executor: runner, Execution: materializer})

	result := svc.Execute(context.Background(), RunRequest{
		AssetPath:   "pipelines/orders/assets/order_items.sql",
		Environment: "staging",
		StartDate:   "2026-07-16T10:00:00+02:00",
		EndDate:     "2026-07-16T11:00:00+02:00",
		SensorMode:  "once",
	})

	require.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Equal(t, "partial output", result.Output)
	assert.Equal(t, "materialization failed", result.Error)
	assert.Equal(t, "pipelines/orders/assets/order_items.sql", result.Operation.Target)
	assert.Nil(t, runner.args)
	assetPath, err := DecodeID(materializer.assetID)
	require.NoError(t, err)
	assert.Equal(t, "pipelines/orders/assets/order_items.sql", assetPath)
	assert.Equal(t, "staging", materializer.assetEnvironment)
	assert.Equal(t, string(MaterializeScopeAsset), materializer.assetScope)
	assert.Equal(t, "2026-07-16T08:00:00Z", materializer.assetStartDate)
	assert.Equal(t, "2026-07-16T09:00:00Z", materializer.assetEndDate)
	assert.Equal(t, "once", materializer.assetSensorMode)
}

func TestRunServiceExecute_DryRunStaysOnExecutor(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("checked")}
	materializer := &stubRunMaterializer{result: MaterializeResult{Status: "error", Error: "must not run"}}
	svc := NewRunService(RunDependencies{Executor: runner, Execution: materializer})

	result := svc.Execute(context.Background(), RunRequest{DryRun: true})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, "checked", result.Output)
	assert.True(t, runner.runPipelineRequest.DryRun)
	assert.Empty(t, materializer.pipelineID)
}

func TestRunServiceExecute_DefaultTargetMaterializesEveryCurrentPipelineInWorkspaceOrder(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("root run")}
	firstID := EncodeID("pipelines/zeta/pipeline.yml")
	secondID := EncodeID("pipelines/alpha/pipeline.yml")
	materializer := &stubRunMaterializer{pipelineResults: map[string]MaterializeResult{
		firstID:  {Status: "ok", Output: "zeta output\n"},
		secondID: {Status: "ok", Output: "alpha output"},
	}}
	svc := NewRunService(RunDependencies{
		Executor:           runner,
		Execution:          materializer,
		CurrentPipelineIDs: func() []string { return []string{firstID, secondID} },
	})

	result := svc.Execute(context.Background(), RunRequest{})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, "zeta output\nalpha output", result.Output)
	assert.Nil(t, runner.args)
	assert.Equal(t, []string{firstID, secondID}, materializer.pipelineIDs)
}

func TestRunServiceExecute_DefaultTargetStopsAtFirstPipelineFailure(t *testing.T) {
	t.Parallel()

	firstID := EncodeID("pipelines/first/pipeline.yml")
	failingID := EncodeID("pipelines/failing/pipeline.yml")
	untouchedID := EncodeID("pipelines/untouched/pipeline.yml")
	materializer := &stubRunMaterializer{pipelineResults: map[string]MaterializeResult{
		firstID:   {Status: "ok", Output: "first output"},
		failingID: {Status: "error", Output: "failing output", Error: "pipeline failed", ExitCode: 1},
		untouchedID: {
			Status: "ok", Output: "must not run",
		},
	}}
	svc := NewRunService(RunDependencies{
		Execution:          materializer,
		CurrentPipelineIDs: func() []string { return []string{firstID, failingID, untouchedID} },
	})

	result := svc.Execute(context.Background(), RunRequest{})

	require.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "pipeline failed", result.Error)
	assert.Equal(t, "first output\nfailing output", result.Output)
	assert.Equal(t, []string{firstID, failingID}, materializer.pipelineIDs)
}

func TestRunServiceExecute_DefaultTargetRejectsWorkspaceWithoutPipelines(t *testing.T) {
	t.Parallel()

	materializer := &stubRunMaterializer{result: MaterializeResult{Status: "ok"}}
	svc := NewRunService(RunDependencies{
		Execution:          materializer,
		CurrentPipelineIDs: func() []string { return nil },
	})

	result := svc.Execute(context.Background(), RunRequest{})

	require.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "workspace has no pipelines to run", result.Error)
	assert.Empty(t, materializer.pipelineIDs)
}

func TestRunServiceExecute_InvalidPipelineID(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{PipelineID: "%%%"})

	assert.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "invalid pipeline id", result.Error)
	assert.Nil(t, runner.args)
}

func TestRunServiceExecute_PropagatesRunnerFailure(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{output: []byte("bad output"), err: errors.New("boom")}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{AssetPath: "assets/foo.sql"})

	require.Equal(t, "run", result.Operation.Type)
	require.Equal(t, "assets/foo.sql", result.Operation.Target)
	assert.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, "assets/foo.sql", result.Operation.AssetPath)
	assert.Equal(t, "bad output", result.Output)
	assert.Equal(t, "boom", result.Error)
}

func TestRunServiceExecuteEnforcesDestructivePolicy(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{}
	svc := NewRunService(RunDependencies{
		Executor:            runner,
		SelectedEnvironment: func() string { return "prod" },
		PolicyFor: func(string) policy.EnvironmentPolicy {
			return policy.EnvironmentPolicy{ConfirmDestructive: true}
		},
	})

	rejected := svc.Execute(context.Background(), RunRequest{FullRefresh: true})
	assert.Equal(t, "error", rejected.Status)
	assert.Equal(t, 403, rejected.HTTPCode)
	assert.Nil(t, runner.args)

	accepted := svc.Execute(context.Background(), RunRequest{FullRefresh: true, ConfirmedEnvironment: "prod"})
	assert.Equal(t, "ok", accepted.Status)
	assert.Equal(t, []string{"run", ".", "--env", "prod"}, runner.args)
}

func TestRunServiceRejectsUnsupportedAssetDryRunAndInvalidWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  RunRequest
		want string
	}{
		{
			name: "asset dry run",
			req:  RunRequest{AssetPath: "assets/foo.sql", DryRun: true},
			want: "asset dry-run is not supported",
		},
		{
			name: "malformed window",
			req: RunRequest{
				StartDate: "not-a-time",
				EndDate:   "2026-07-16T09:00:00Z",
			},
			want: "start must be an RFC3339",
		},
		{
			name: "backfill without window",
			req:  RunRequest{Backfill: true},
			want: "backfill requires an explicit start and end",
		},
		{
			name: "dry run with full refresh",
			req:  RunRequest{DryRun: true, FullRefresh: true},
			want: "dry_run does not support",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &stubRunRunner{}
			svc := NewRunService(RunDependencies{Executor: runner})

			result := svc.Execute(context.Background(), tt.req)

			assert.Equal(t, "error", result.Status)
			assert.Equal(t, 400, result.HTTPCode)
			assert.Contains(t, result.Error, tt.want)
			assert.Nil(t, runner.args)
		})
	}
}

func TestRunServiceNormalizesAndPreservesSensorMode(t *testing.T) {
	t.Parallel()

	runner := &stubRunRunner{}
	svc := NewRunService(RunDependencies{Executor: runner})

	result := svc.Execute(context.Background(), RunRequest{
		StartDate:  "2026-07-16T10:00:00+02:00",
		EndDate:    "2026-07-16T11:00:00+02:00",
		SensorMode: "skip",
	})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, "skip", runner.runPipelineRequest.SensorMode)
	assert.Equal(t, "2026-07-16T08:00:00Z", runner.runPipelineRequest.StartDate)
	assert.Equal(t, "2026-07-16T09:00:00Z", runner.runPipelineRequest.EndDate)
}

func TestRunServiceRejectsUnsafeLegacyAssetBackfill(t *testing.T) {
	t.Parallel()

	_, workspaceRoot := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"unsafe.sql": `
/* @bruin
name: analytics.unsafe
type: duckdb.sql
materialization:
  type: table
@bruin */
select 1 as id
`,
	})
	runner := &stubRunRunner{}
	svc := NewRunService(RunDependencies{Executor: runner, WorkspaceRoot: workspaceRoot})

	result := svc.Execute(context.Background(), RunRequest{
		AssetPath: "analytics/assets/unsafe.sql",
		Backfill:  true,
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})

	assert.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPCode)
	assert.Contains(t, result.Error, "not safe to backfill")
	assert.Nil(t, runner.args)
}

func TestRunServiceAllowsReplaySafeLegacyAssetBackfill(t *testing.T) {
	t.Parallel()

	_, workspaceRoot := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  duckdb: duckdb-default
`, map[string]string{
		"safe.sql": `
/* @bruin
name: analytics.safe
type: duckdb.sql
materialization:
  type: table
  strategy: time_interval
  incremental_key: event_date
  time_granularity: date
@bruin */
select cast('{{ start_date }}' as date) as event_date
`,
	})
	runner := &stubRunRunner{}
	svc := NewRunService(RunDependencies{Executor: runner, WorkspaceRoot: workspaceRoot})

	result := svc.Execute(context.Background(), RunRequest{
		AssetPath: "analytics/assets/safe.sql",
		Backfill:  true,
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	})

	require.Equal(t, "ok", result.Status, result.Error)
	assert.Equal(t, []string{"run", "analytics/assets/safe.sql"}, runner.args)
	assert.Equal(t, "2026-07-15T00:00:00Z", runner.runAssetRequest.StartDate)
	assert.Equal(t, "2026-07-16T00:00:00Z", runner.runAssetRequest.EndDate)
}

func TestExtractInspectRawOutputUsesErrorField(t *testing.T) {
	t.Parallel()

	output, err := json.Marshal(map[string]any{
		"error": "Catalog Error: Table with name raw_downstream does not exist\nLINE 1: select * from raw_downstream",
	})
	require.NoError(t, err)

	assert.Equal(t, "Catalog Error: Table with name raw_downstream does not exist\nLINE 1: select * from raw_downstream", extractInspectRawOutput(output))
}
