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
	args   []string
	output []byte
	err    error
}

func (s *stubRunRunner) RunAsset(_ context.Context, req RunAssetRequest, _ func([]byte)) ([]byte, error) {
	s.args = []string{"run", req.AssetPath}
	if req.Environment != "" {
		s.args = append(s.args, "--env", req.Environment)
	}
	return s.output, s.err
}

func (s *stubRunRunner) RunPipeline(_ context.Context, req RunPipelineRequest, _ func([]byte)) ([]byte, error) {
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

func TestExtractInspectRawOutputUsesErrorField(t *testing.T) {
	t.Parallel()

	output, err := json.Marshal(map[string]any{
		"error": "Catalog Error: Table with name raw_downstream does not exist\nLINE 1: select * from raw_downstream",
	})
	require.NoError(t, err)

	assert.Equal(t, "Catalog Error: Table with name raw_downstream does not exist\nLINE 1: select * from raw_downstream", extractInspectRawOutput(output))
}
