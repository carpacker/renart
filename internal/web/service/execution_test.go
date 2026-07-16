package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/bus"
	"renart/internal/web/policy"
)

func newExecutionTestResolver(workspaceRoot string) *WorkspaceResolver {
	return NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		osFS := afero.NewOsFs()
		builder := pipeline.NewBuilder(
			BuilderConfig,
			pipeline.CreateTaskFromYamlDefinition(osFS),
			pipeline.CreateTaskFromFileComments(osFS),
			osFS,
			DefaultGlossaryReader,
			jinja.VariantRendererFactory,
		)
		return builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
}

type stubExecutionExecutor struct {
	runAssetOutput    []byte
	runAssetErr       error
	runAssetChunks    [][]byte
	runAssetRequests  []RunAssetRequest
	runPipelineOutput []byte
	runPipelineErr    error
	runPipelineChunks [][]byte
	runPipelineEvents []ExecutionAssetEvent
	runPipelineReqs   []RunPipelineRequest
	queryConnOutput   []byte
	queryConnErr      error
	queryConnReqs     []QueryConnectionRequest
	runWithRetry      func(context.Context, QueryAssetRequest, int, time.Duration) ([]byte, error, int)
}

func (s *stubExecutionExecutor) RunAsset(_ context.Context, req RunAssetRequest, onChunk func([]byte)) ([]byte, error) {
	s.runAssetRequests = append(s.runAssetRequests, req)
	for _, chunk := range s.runAssetChunks {
		if onChunk != nil {
			onChunk(chunk)
		}
	}
	return s.runAssetOutput, s.runAssetErr
}

func (s *stubExecutionExecutor) RunPipeline(_ context.Context, req RunPipelineRequest, onChunk func([]byte)) ([]byte, error) {
	s.runPipelineReqs = append(s.runPipelineReqs, req)
	for _, chunk := range s.runPipelineChunks {
		if onChunk != nil {
			onChunk(chunk)
		}
	}
	for _, event := range s.runPipelineEvents {
		if req.AssetEvent != nil {
			req.AssetEvent(event)
		}
	}
	return s.runPipelineOutput, s.runPipelineErr
}

func (s *stubExecutionExecutor) QueryAsset(context.Context, QueryAssetRequest) ([]byte, error) {
	return nil, nil
}

func (s *stubExecutionExecutor) QueryConnection(_ context.Context, req QueryConnectionRequest) ([]byte, error) {
	s.queryConnReqs = append(s.queryConnReqs, req)
	return s.queryConnOutput, s.queryConnErr
}

func (s *stubExecutionExecutor) FormatAsset(context.Context, FormatAssetRequest) ([]byte, error) {
	return nil, nil
}

func (s *stubExecutionExecutor) ApplyPatch(context.Context, PatchRequest) ([]byte, error) {
	return nil, nil
}

func (s *stubExecutionExecutor) ImportDatabase(context.Context, ImportDatabaseRequest) ([]byte, error) {
	return nil, nil
}

func (s *stubExecutionExecutor) RunWithRetry(ctx context.Context, req QueryAssetRequest, retries int, delay time.Duration) ([]byte, error, int) {
	if s.runWithRetry != nil {
		return s.runWithRetry(ctx, req, retries, delay)
	}
	return nil, nil, 0
}

func TestExecutionServiceMaterializeAssetStreamPreservesSuccessOutput(t *testing.T) {
	t.Parallel()

	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	executor := &stubExecutionExecutor{
		runAssetOutput: []byte("asset run complete\n"),
		runAssetChunks: [][]byte{[]byte("asset "), []byte("run complete\n")},
	}
	streamed := make([]string, 0)
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })

	svc := NewExecutionService(ExecutionDependencies{
		ConfigPath: "/path/that/does/not/exist",
		Executor:   executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "pipelines/orders/assets/orders.sql", &pipeline.Pipeline{}, &pipeline.Asset{Connection: "warehouse"}, nil
		},
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(...string) []string { return []string{"inspect-1", "inspect-2"} },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:     EncodeID("pipelines/orders/pipeline.yml"),
				UUID:   "orders-uuid",
				Assets: []AssetView{{ID: assetID, Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})

	result := svc.MaterializeAssetStream(context.Background(), assetID, "", "", "", "", false, false, "", func(chunk []byte) {
		streamed = append(streamed, string(chunk))
	})

	require.Len(t, executor.runAssetRequests, 1)
	assert.Equal(t, "pipelines/orders/assets/orders.sql", executor.runAssetRequests[0].AssetPath)
	assert.Equal(t, sensorModeOnce, executor.runAssetRequests[0].SensorMode)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "asset run complete\n", result.Output)
	assert.Empty(t, result.Error)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "run", result.Operation.Type)
	assert.Equal(t, "pipelines/orders/assets/orders.sql", result.Operation.Target)
	assert.Equal(t, []string{"inspect-1", "inspect-2"}, result.ChangedAssetIDs)
	assert.NotNil(t, result.MaterializedAt)
	assert.Equal(t, []string{"asset ", "run complete\n"}, streamed)
	require.Len(t, completed.Assets, 1)
	assert.Equal(t, "analytics.orders", completed.Assets[0].AssetName)
	assert.Equal(t, "succeeded", completed.Assets[0].Status)
}

func TestExecutionServiceEnforcesDestructiveConfirmationAndEmitsFullRefresh(t *testing.T) {
	t.Parallel()

	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	executor := &stubExecutionExecutor{}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })
	svc := NewExecutionService(ExecutionDependencies{
		Executor:            executor,
		SelectedEnvironment: func() string { return "prod" },
		PolicyFor: func(environment string) policy.EnvironmentPolicy {
			assert.Equal(t, "prod", environment)
			return policy.EnvironmentPolicy{ConfirmDestructive: true}
		},
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "pipelines/orders/assets/orders.sql", &pipeline.Pipeline{}, &pipeline.Asset{Connection: "warehouse"}, nil
		},
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(...string) []string { return []string{assetID} },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:     EncodeID("pipelines/orders/pipeline.yml"),
				UUID:   "orders-uuid",
				Assets: []AssetView{{ID: assetID, Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})

	rejected := svc.MaterializeAssetStream(context.Background(), assetID, "", "", "", "", true, false, "wrong", nil)
	assert.Equal(t, "error", rejected.Status)
	assert.Contains(t, rejected.Error, "requires typing")
	assert.Empty(t, executor.runAssetRequests)

	accepted := svc.MaterializeAssetStream(context.Background(), assetID, "", "", "", "", true, false, "prod", nil)
	assert.Equal(t, "ok", accepted.Status)
	require.Len(t, executor.runAssetRequests, 1)
	assert.Equal(t, "prod", executor.runAssetRequests[0].Environment)
	assert.True(t, executor.runAssetRequests[0].FullRefresh)
	assert.True(t, completed.FullRefresh)
}

func TestExecutionServiceHonorsEnvironmentFullRefreshRestriction(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	configPath := filepath.Join(workspaceRoot, ".bruin.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(strings.TrimSpace(`
default_environment: prod
environments:
  prod:
    config:
      full_refresh_restricted: true
    connections: {}
`)+"\n"), 0o644))

	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	executor := &stubExecutionExecutor{}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })
	svc := NewExecutionService(ExecutionDependencies{
		ConfigPath:          configPath,
		Executor:            executor,
		SelectedEnvironment: func() string { return "prod" },
		PolicyFor: func(string) policy.EnvironmentPolicy {
			return policy.EnvironmentPolicy{ConfirmDestructive: true}
		},
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "pipelines/orders/assets/orders.sql", &pipeline.Pipeline{}, &pipeline.Asset{Connection: "warehouse"}, nil
		},
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(...string) []string { return []string{assetID} },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:     EncodeID("pipelines/orders/pipeline.yml"),
				UUID:   "orders-uuid",
				Assets: []AssetView{{ID: assetID, Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})

	result := svc.MaterializeAssetStream(context.Background(), assetID, "", "", "", "", true, false, "", nil)

	assert.Equal(t, "ok", result.Status)
	require.Len(t, executor.runAssetRequests, 1)
	assert.False(t, executor.runAssetRequests[0].FullRefresh)
	assert.False(t, completed.FullRefresh)
	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "restricted for environment prod")
}

func TestExecutionServiceOnlyBackfillsReplaySafeAssets(t *testing.T) {
	t.Parallel()

	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	resolvedAsset := &pipeline.Asset{
		Type: pipeline.AssetTypeDuckDBQuery,
		Materialization: pipeline.Materialization{
			Type:            pipeline.MaterializationTypeTable,
			Strategy:        pipeline.MaterializationStrategyTimeInterval,
			IncrementalKey:  "event_at",
			TimeGranularity: pipeline.MaterializationTimeGranularityTimestamp,
		},
	}
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		Executor:            executor,
		SelectedEnvironment: func() string { return "prod" },
		PolicyFor: func(string) policy.EnvironmentPolicy {
			return policy.EnvironmentPolicy{ConfirmDestructive: true}
		},
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "pipelines/orders/assets/orders.sql", &pipeline.Pipeline{Schedule: "daily"}, resolvedAsset, nil
		},
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(...string) []string { return []string{assetID} },
	})

	unsafe := *resolvedAsset
	unsafe.Materialization.Strategy = pipeline.MaterializationStrategyAppend
	resolvedAsset = &unsafe
	missingWindow := svc.MaterializeAssetStream(context.Background(), assetID, "", "asset", "", "", false, true, "prod", nil)
	assert.Equal(t, "error", missingWindow.Status)
	assert.Contains(t, missingWindow.Error, "explicit start and end")
	assert.Empty(t, executor.runAssetRequests)
	conflictingMode := svc.MaterializeAssetStream(context.Background(), assetID, "", "asset", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", true, true, "prod", nil)
	assert.Equal(t, "error", conflictingMode.Status)
	assert.Contains(t, conflictingMode.Error, "mutually exclusive")
	assert.Empty(t, executor.runAssetRequests)

	rejected := svc.MaterializeAssetStream(context.Background(), assetID, "", "asset", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", false, true, "prod", nil)
	assert.Equal(t, "error", rejected.Status)
	assert.Contains(t, rejected.Error, "not safe to backfill")
	assert.Empty(t, executor.runAssetRequests)

	resolvedAsset.Materialization.Strategy = pipeline.MaterializationStrategyTimeInterval
	accepted := svc.MaterializeAssetStream(context.Background(), assetID, "", "asset", "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z", false, true, "prod", nil)
	assert.Equal(t, "ok", accepted.Status)
	require.Len(t, executor.runAssetRequests, 1)
	assert.Equal(t, "2024-01-01T00:00:00Z", executor.runAssetRequests[0].StartDate)
	assert.Equal(t, "2024-01-02T00:00:00Z", executor.runAssetRequests[0].EndDate)
}

func TestExecutionServiceMaterializeAssetStreamPreservesFailureOutput(t *testing.T) {
	t.Parallel()

	assetID := EncodeID("pipelines/orders/assets/orders.sql")
	executor := &stubExecutionExecutor{
		runAssetOutput: []byte("asset failed after direct execution\n"),
		runAssetErr:    errors.New("asset failed"),
	}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })

	svc := NewExecutionService(ExecutionDependencies{
		ConfigPath: "/path/that/does/not/exist",
		Executor:   executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "pipelines/orders/assets/orders.sql", &pipeline.Pipeline{}, &pipeline.Asset{Connection: "warehouse"}, nil
		},
		ResolveAssetNameByID: func(string) string { return "analytics.orders" },
		FindInspectIDs:       func(...string) []string { return []string{"inspect-1"} },
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:     EncodeID("pipelines/orders/pipeline.yml"),
				UUID:   "orders-uuid",
				Assets: []AssetView{{ID: assetID, Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})

	result := svc.MaterializeAssetStream(context.Background(), assetID, "", "", "", "", false, false, "", nil)

	require.Len(t, executor.runAssetRequests, 1)
	assert.Equal(t, "error", result.Status)
	assert.Equal(t, "asset failed after direct execution\n", result.Output)
	assert.Equal(t, "asset failed", result.Error)
	assert.Equal(t, 1, result.ExitCode)
	assert.Empty(t, result.ChangedAssetIDs)
	assert.Nil(t, result.MaterializedAt)
	require.Len(t, completed.Assets, 1)
	assert.Equal(t, "failed", completed.Assets[0].Status)
}

func TestExecutionServiceMaterializePipelineStreamPreservesSuccessOutput(t *testing.T) {
	t.Parallel()

	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{
		runPipelineOutput: []byte("pipeline run complete\n"),
		runPipelineChunks: [][]byte{[]byte("pipeline "), []byte("run complete\n")},
	}
	streamed := make([]string, 0)
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })

	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:   pipelineID,
				UUID: "orders-uuid",
				Assets: []AssetView{
					{ID: "asset-1", Name: "analytics.orders"},
					{ID: "asset-2", Name: "analytics.order_items"},
				},
			}}
		},
		Events: events,
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "", false, false, false, "", "", "", func(chunk []byte) {
		streamed = append(streamed, string(chunk))
	})

	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, "pipelines/orders", executor.runPipelineReqs[0].Target)
	assert.Equal(t, sensorModeOnce, executor.runPipelineReqs[0].SensorMode)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, "pipeline run complete\n", result.Output)
	assert.Empty(t, result.Error)
	assert.Equal(t, 0, result.ExitCode)
	assert.Equal(t, "run", result.Operation.Type)
	assert.Equal(t, "pipelines/orders", result.Operation.Target)
	assert.Equal(t, []string{"asset-1", "asset-2"}, result.ChangedAssetIDs)
	assert.NotNil(t, result.MaterializedAt)
	assert.Equal(t, []string{"pipeline ", "run complete\n"}, streamed)
	require.Len(t, completed.Assets, 2)
	assert.Equal(t, "succeeded", completed.Assets[0].Status)
	assert.Equal(t, "succeeded", completed.Assets[1].Status)
}

func TestExecutionServiceScheduledPipelineUsesWaitSensorMode(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		PolicyFor: func(string) policy.EnvironmentPolicy {
			return policy.EnvironmentPolicy{Protected: true}
		},
	})

	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		RunID:       "scheduled-run",
		PipelineID:  pipelineID,
		Environment: "prod",
		Scheduled:   true,
		DryRun:      true,
	}, nil, nil)

	assert.Equal(t, "ok", result.Status)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, sensorModeWait, executor.runPipelineReqs[0].SensorMode)
}

func TestExecutionServiceQueuedManualPipelineUsesInteractiveSemantics(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")

	t.Run("durable run id does not select scheduled sensor mode", func(t *testing.T) {
		executor := &stubExecutionExecutor{}
		svc := NewExecutionService(ExecutionDependencies{Executor: executor})

		result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
			RunID:      "queued-manual-run",
			PipelineID: pipelineID,
			DryRun:     true,
		}, nil, nil)

		assert.Equal(t, "ok", result.Status)
		require.Len(t, executor.runPipelineReqs, 1)
		assert.Equal(t, sensorModeOnce, executor.runPipelineReqs[0].SensorMode)
	})

	t.Run("manual deployment remains interactive in a protected environment", func(t *testing.T) {
		executor := &stubExecutionExecutor{}
		svc := NewExecutionService(ExecutionDependencies{
			Executor: executor,
			PolicyFor: func(string) policy.EnvironmentPolicy {
				return policy.EnvironmentPolicy{Protected: true}
			},
		})

		result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
			RunID:       "queued-manual-run",
			PipelineID:  pipelineID,
			Environment: "prod",
			SnapshotDir: "/tmp/deployed-snapshot",
			DryRun:      true,
		}, nil, nil)

		assert.Equal(t, "error", result.Status)
		assert.Contains(t, result.Error, "protected")
		assert.Empty(t, executor.runPipelineReqs)
	})
}

func TestExecutionServiceHonorsInteractiveSensorModeOverride(t *testing.T) {
	t.Parallel()
	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{Executor: executor})

	result := svc.MaterializePipelineStreamWithSensorMode(
		context.Background(), pipelineID, "", false, false, false, "", "", "", sensorModeSkip, nil,
	)

	assert.Equal(t, "ok", result.Status)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, sensorModeSkip, executor.runPipelineReqs[0].SensorMode)
}

func TestExecutionServicePinnedPipelineUsesSnapshotDefaultWindow(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	workspacePipelineRoot := filepath.Join(workspaceRoot, "pipelines", "orders")
	snapshotRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(workspacePipelineRoot, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(workspacePipelineRoot, "pipeline.yml"),
		[]byte("name: orders\nschedule: daily\n"),
		0o644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(snapshotRoot, "pipeline.yml"),
		[]byte("name: orders\nschedule: hourly\n"),
		0o644,
	))

	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		WorkspaceRoot: workspaceRoot,
		Executor:      executor,
		NewPipelineBuilder: func() *pipeline.Builder {
			return NewRenartPipelineBuilder(afero.NewOsFs())
		},
	})

	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID:  pipelineID,
		SnapshotDir: snapshotRoot,
	}, nil, nil)

	require.Equal(t, "ok", result.Status, result.Error)
	require.Len(t, executor.runPipelineReqs, 1)
	request := executor.runPipelineReqs[0]
	start, err := time.Parse(time.RFC3339, request.StartDate)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, request.EndDate)
	require.NoError(t, err)
	assert.Equal(t, time.Hour, end.Sub(start), "snapshot is hourly while the working tree is daily")
	assert.Equal(t, request.StartDate, result.Operation.StartDate)
	assert.Equal(t, request.EndDate, result.Operation.EndDate)
}

func TestExecutionServicePinnedPipelineKeepsExplicitWindow(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{Executor: executor})
	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID:  EncodeID("pipelines/orders/pipeline.yml"),
		SnapshotDir: t.TempDir(),
		StartDate:   "2026-07-16T08:00:00Z",
		EndDate:     "2026-07-16T09:00:00Z",
	}, nil, nil)

	require.Equal(t, "ok", result.Status, result.Error)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, "2026-07-16T08:00:00Z", executor.runPipelineReqs[0].StartDate)
	assert.Equal(t, "2026-07-16T09:00:00Z", executor.runPipelineReqs[0].EndDate)
	assert.Equal(t, executor.runPipelineReqs[0].StartDate, result.Operation.StartDate)
	assert.Equal(t, executor.runPipelineReqs[0].EndDate, result.Operation.EndDate)
}

func TestExecutionServicePinnedPipelineRejectsUnresolvedTemplatedSchedule(t *testing.T) {
	t.Parallel()

	snapshotRoot := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(snapshotRoot, "pipeline.yml"),
		[]byte("name: orders\nschedule: \"{{ var.value.execution_schedule }}\"\n"),
		0o644,
	))
	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{Executor: executor})

	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID:  EncodeID("pipelines/orders/pipeline.yml"),
		SnapshotDir: snapshotRoot,
	}, nil, nil)

	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "templated deployed pipeline schedule requires an explicit execution window")
	assert.Empty(t, executor.runPipelineReqs)
}

func TestExecutionServiceMaterializePipelineStreamPreservesFailureOutput(t *testing.T) {
	t.Parallel()

	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{
		runPipelineOutput: []byte("pipeline failed during direct execution\n"),
		runPipelineErr:    errors.New("pipeline failed"),
		runPipelineEvents: []ExecutionAssetEvent{
			{Asset: "analytics.orders", Status: "running"},
			{Asset: "analytics.orders", Status: "success"},
			{Asset: "analytics.order_items", Status: "running"},
			{Asset: "analytics.order_items", Status: "failed", Error: "pipeline failed"},
		},
	}
	events := bus.New()
	var completed bus.RunCompleted
	events.OnRunCompleted(func(event bus.RunCompleted) { completed = event })

	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:   pipelineID,
				UUID: "orders-uuid",
				Assets: []AssetView{
					{ID: "asset-1", Name: "analytics.orders"},
					{ID: "asset-2", Name: "analytics.order_items"},
					{ID: "asset-3", Name: "analytics.parabola"},
				},
			}}
		},
		Events: events,
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "", false, false, false, "", "", "", nil)

	require.Len(t, executor.runPipelineReqs, 1)
	assert.Equal(t, "error", result.Status)
	assert.Equal(t, "pipeline failed during direct execution\n", result.Output)
	assert.Equal(t, "pipeline failed", result.Error)
	assert.Equal(t, 1, result.ExitCode)
	assert.Equal(t, []string{"asset-1"}, result.ChangedAssetIDs)
	assert.Nil(t, result.MaterializedAt)
	require.Len(t, completed.Assets, 2)
	assert.Equal(t, "analytics.orders", completed.Assets[0].AssetName)
	assert.Equal(t, "succeeded", completed.Assets[0].Status)
	assert.Equal(t, "analytics.order_items", completed.Assets[1].AssetName)
	assert.Equal(t, "failed", completed.Assets[1].Status)
	for _, asset := range completed.Assets {
		assert.NotEqual(t, "analytics.parabola", asset.AssetName)
	}
}

func TestExecutionServiceMaterializePipelineStreamDryRunDoesNotEmitCompletion(t *testing.T) {
	t.Parallel()

	pipelineID := EncodeID("pipelines/orders/pipeline.yml")
	executor := &stubExecutionExecutor{
		runPipelineOutput: []byte("dry run complete\n"),
	}
	events := bus.New()
	completed := 0
	events.OnRunCompleted(func(bus.RunCompleted) { completed++ })

	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		CurrentPipelines: func() []PipelineView {
			return []PipelineView{{
				ID:     pipelineID,
				Assets: []AssetView{{ID: "asset-1", Name: "analytics.orders"}},
			}}
		},
		Events: events,
	})

	result := svc.MaterializePipelineStream(context.Background(), pipelineID, "", true, false, false, "", "", "", nil)

	require.Len(t, executor.runPipelineReqs, 1)
	assert.True(t, executor.runPipelineReqs[0].DryRun)
	assert.Empty(t, executor.runPipelineReqs[0].StartDate)
	assert.Empty(t, executor.runPipelineReqs[0].EndDate)
	assert.Equal(t, "ok", result.Status)
	assert.Empty(t, result.Operation.StartDate)
	assert.Empty(t, result.Operation.EndDate)
	assert.Empty(t, result.ChangedAssetIDs)
	assert.Nil(t, result.MaterializedAt)
	assert.Zero(t, completed)
}

func TestExecutionServiceDryRunDoesNotResolveExecutionContext(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{}
	contextResolved := false
	svc := NewExecutionService(ExecutionDependencies{Executor: executor})

	result := svc.MaterializePipelineRun(context.Background(), PipelineRunSpec{
		PipelineID: EncodeID("pipelines/orders/pipeline.yml"),
		DryRun:     true,
		OnContextResolved: func(ResolvedPipelineRunContext) error {
			contextResolved = true
			return nil
		},
	}, nil, nil)

	require.Equal(t, "ok", result.Status, result.Error)
	require.Len(t, executor.runPipelineReqs, 1)
	assert.False(t, contextResolved)
	assert.Empty(t, executor.runPipelineReqs[0].StartDate)
	assert.Empty(t, executor.runPipelineReqs[0].EndDate)
	assert.Empty(t, result.Operation.StartDate)
	assert.Empty(t, result.Operation.EndDate)
}

func TestExecutionServiceInspectAssetRejectsWriteQueries(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

copy (select * from analytics.customers) to 'danger.parquet'
`)+"\n"), 0o644))

	resolveAssetByID := newExecutionTestResolver(workspaceRoot).ResolveAssetByID

	svc := NewExecutionService(ExecutionDependencies{
		WorkspaceRoot:    workspaceRoot,
		ConfigPath:       filepath.Join(workspaceRoot, ".bruin.yml"),
		Executor:         &stubExecutionExecutor{},
		ResolveAssetByID: resolveAssetByID,
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/customers.sql"), "200", "", "", "")

	assert.Equal(t, "error", result.Status)
	assert.Equal(t, 400, result.HTTPStatus)
	assert.Equal(t, inspectReadOnlyErrorMessage, result.Error)
	assert.Equal(t, inspectReadOnlyErrorMessage, result.RawOutput)
	assert.Empty(t, result.Rows)
	assert.Empty(t, result.Columns)
}

func TestExecutionServiceInspectNonSQLAssetQueriesMaterializedTable(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{
		queryConnOutput: []byte(`{"columns":["customer_id"],"rows":[{"customer_id":1}]}`),
	}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "analytics/assets/load_customers.yml", &pipeline.Pipeline{
					DefaultConnections: pipeline.EmptyStringMap{"duckdb": "duckdb-default"},
				}, &pipeline.Asset{
					Name: "analytics.customers",
					Type: pipeline.AssetTypeIngestr,
					Parameters: pipeline.ParameterMap{
						"destination": "duckdb",
					},
				}, nil
		},
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/load_customers.yml"), "25", "", "", "")

	require.Len(t, executor.queryConnReqs, 1)
	assert.Equal(t, "duckdb-default", executor.queryConnReqs[0].ConnectionName)
	assert.Equal(t, "select * from analytics.customers limit 25", executor.queryConnReqs[0].Query)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, []string{"customer_id"}, result.Columns)
	assert.Equal(t, []map[string]any{{"customer_id": float64(1)}}, result.Rows)
}

func TestExecutionServiceInspectLoadAssetQueriesDestinationConnection(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{
		queryConnOutput: []byte(`{"columns":["id"],"rows":[{"id":7}]}`),
	}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "analytics/assets/load_orders.asset.yml", &pipeline.Pipeline{}, &pipeline.Asset{
				Name:       "analytics.orders",
				Type:       pipeline.AssetType(loadAssetType),
				Connection: "duckdb-default",
				Parameters: pipeline.ParameterMap{
					"source_connection": "postgres-prod",
					"source_table":      "public.orders",
				},
			}, nil
		},
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/load_orders.asset.yml"), "25", "", "", "")

	require.Len(t, executor.queryConnReqs, 1)
	assert.Equal(t, "duckdb-default", executor.queryConnReqs[0].ConnectionName)
	assert.Equal(t, "select * from analytics.orders limit 25", executor.queryConnReqs[0].Query)
	assert.Equal(t, "ok", result.Status)
	assert.Equal(t, []string{"id"}, result.Columns)
}

func TestExecutionServiceInspectLoadAssetToLocalFileReturnsInfo(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "analytics/assets/load_local.asset.yml", &pipeline.Pipeline{}, &pipeline.Asset{
				Name:       "analytics.local_dump",
				Type:       pipeline.AssetType(loadAssetType),
				Connection: "local",
				Parameters: pipeline.ParameterMap{
					"source_connection":  "duckdb-default",
					"source_table":       "analytics.orders",
					"destination_object": "./blub.csv",
				},
			}, nil
		},
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/load_local.asset.yml"), "25", "", "", "")

	assert.Equal(t, "info", result.Status)
	assert.Equal(t, 200, result.HTTPStatus)
	assert.Contains(t, result.Info, "./blub.csv")
	assert.Empty(t, result.Error)
	assert.Empty(t, executor.queryConnReqs, "a local-file load asset must not run a connection query")
}

func TestExecutionServiceInspectSensorReturnsInfoWithoutQueryingTable(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "analytics/assets/upstream_ready.asset.yml", &pipeline.Pipeline{}, &pipeline.Asset{
				Name: "analytics.upstream_ready",
				Type: pipeline.AssetTypeDuckDBQuerySensor,
			}, nil
		},
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/upstream_ready.asset.yml"), "25", "", "", "")

	assert.Equal(t, "info", result.Status)
	assert.Equal(t, 200, result.HTTPStatus)
	assert.Contains(t, result.Info, "do not materialize previewable data")
	assert.Empty(t, executor.queryConnReqs)
}

func TestExecutionServiceInspectNonSQLAssetReportsMissingMaterializedTable(t *testing.T) {
	t.Parallel()

	executor := &stubExecutionExecutor{queryConnErr: errors.New("table not found")}
	svc := NewExecutionService(ExecutionDependencies{
		Executor: executor,
		ResolveAssetByID: func(context.Context, string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return "analytics/assets/task.py", &pipeline.Pipeline{
					DefaultConnections: pipeline.EmptyStringMap{"duckdb": "duckdb-default"},
				}, &pipeline.Asset{
					Name: "analytics.python_task",
					Type: pipeline.AssetTypePython,
				}, nil
		},
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/task.py"), "25", "", "", "")

	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "Materialize the asset first")
	require.Len(t, executor.queryConnReqs, 1)
}

func TestExecutionServiceInspectAssetDetectsMissingRenartUpstream(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets", "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "players.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.players
type: duckdb.sql
materialization:
  type: table
@bruin */

select 1 as player_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "player_stats.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.player_stats
type: duckdb.sql
materialization:
  type: table
depends:
  - analytics.players
@bruin */

select * from analytics.players
`)+"\n"), 0o644))

	resolveAssetByID := newExecutionTestResolver(workspaceRoot).ResolveAssetByID

	queryErr := errors.New("Catalog Error: Table with name analytics.players does not exist")
	svc := NewExecutionService(ExecutionDependencies{
		WorkspaceRoot: workspaceRoot,
		ConfigPath:    filepath.Join(workspaceRoot, ".bruin.yml"),
		Executor: &stubExecutionExecutor{runWithRetry: func(context.Context, QueryAssetRequest, int, time.Duration) ([]byte, error, int) {
			return nil, queryErr, 1
		}},
		ResolveAssetByID: resolveAssetByID,
	})

	result := svc.InspectAsset(context.Background(), EncodeID("analytics/assets/analytics/player_stats.sql"), "25", "", "", "")

	assert.Equal(t, "error", result.Status)
	assert.Equal(t, []string{EncodeID("analytics/assets/analytics/players.sql")}, result.MissingUpstreamAssetIDs)
	assert.Equal(t, []string{"analytics.players"}, result.MissingUpstreamAssetNames)
	assert.True(t, result.MissingUpstreamAssetsMaterializable)
}
