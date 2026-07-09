package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadRunEnvIncludesResolvedIntervalDates(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 2, 3, 4, 0, time.UTC)
	end := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ctx := context.WithValue(context.Background(), pipeline.RunConfigStartDate, start)
	ctx = context.WithValue(ctx, pipeline.RunConfigEndDate, end)

	env := loadRunEnv(ctx)
	assert.Contains(t, env, "START_DATE=2024-01-01T02:03:04Z")
	assert.Contains(t, env, "END_DATE=2024-01-02T03:04:05Z")
}

func TestSlingMaterializationArgs(t *testing.T) {
	t.Parallel()

	primaryKey := pipeline.Column{Name: "id", PrimaryKey: true}
	tests := []struct {
		name     string
		strategy string
		key      string
		columns  []pipeline.Column
		want     []string
		wantErr  string
	}{
		{name: "replace is Sling default", strategy: "create+replace"},
		{name: "truncate", strategy: "truncate+insert", want: []string{"--mode", "truncate"}},
		{name: "append snapshot", strategy: "append", want: []string{"--mode", "snapshot"}},
		{name: "append with update key", strategy: "append", key: "updated_at", want: []string{"--mode", "incremental", "--update-key", "updated_at"}},
		{name: "merge", strategy: "merge", key: "updated_at", columns: []pipeline.Column{primaryKey}, want: []string{"--mode", "incremental", "--primary-key", "id", "--update-key", "updated_at"}},
		{name: "merge needs primary key", strategy: "merge", wantErr: "primary-key"},
		{name: "reject unsupported", strategy: "time_interval", wantErr: "not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset := &pipeline.Asset{Type: pipeline.AssetType("api"), Columns: tt.columns}
			asset.Materialization.Strategy = pipeline.MaterializationStrategy(tt.strategy)
			asset.Materialization.IncrementalKey = tt.key
			got, err := slingMaterializationArgs(context.Background(), asset)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSlingMaterializationArgsFullRefreshOverridesStrategy(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), pipeline.RunConfigFullRefresh, true)
	asset := &pipeline.Asset{Type: pipeline.AssetType("api")}
	asset.Materialization.Strategy = pipeline.MaterializationStrategy("merge")

	args, err := slingMaterializationArgs(ctx, asset)
	require.NoError(t, err)
	assert.Equal(t, []string{"--mode", "full-refresh"}, args)
}

func TestAssetServiceCreateLoadAssetWritesFlatParamDefinition(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(pipelineRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))

	resolver := newAssetTestResolver(workspaceRoot)
	service := NewAssetService(AssetDependencies{
		Fs:            afero.NewOsFs(),
		WorkspaceRoot: workspaceRoot,
		ResolveAssetByID: func(ctx context.Context, assetID string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return resolver.ResolveAssetByID(ctx, assetID)
		},
		DefaultAssetContent: DefaultAssetContent,
		DerivedAssetContent: DefaultDerivedSQLAssetContent,
		EnsurePythonProject: func(string, string, string) error {
			return nil
		},
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	result, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name: "analytics.orders_load",
		Type: "load",
		Path: "assets/analytics/orders_load.asset.yml",
	})
	require.Nil(t, apiErr)
	// A Load asset is now a single flat-parameter .asset.yml — no .sling.yml sidecar.
	assert.Equal(t, "analytics/assets/analytics/orders_load.asset.yml", result.AssetPath)

	definition, err := os.ReadFile(filepath.Join(pipelineRoot, "assets/analytics/orders_load.asset.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(definition), "type: load")
	assert.Contains(t, string(definition), "parameters:")
	assert.Contains(t, string(definition), "source_connection:")
	assert.Contains(t, string(definition), "destination_connection:")
	assert.NotContains(t, string(definition), "run:")

	_, err = os.Stat(filepath.Join(pipelineRoot, "assets/analytics/orders_load.sling.yml"))
	assert.True(t, os.IsNotExist(err), "no replication sidecar should be written")
}

func TestWorkspaceServiceLoadsFlatParamLoadAsset(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/analytics/move_users.asset.yml"), []byte(
		"name: analytics.move_users\ntype: load\nparameters:\n  source_connection: postgres_prod\n  source_table: public.users\n  destination_connection: duckdb_default\n  destination_table: public.users\n  mode: full-refresh\n",
	), 0o644))

	workspace := NewWorkspaceService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"))
	state, err := workspace.ComputeState(context.Background())
	require.NoError(t, err)
	require.Empty(t, state.Errors)
	require.Len(t, state.Pipelines, 1)
	require.Len(t, state.Pipelines[0].Assets, 1)
	asset := state.Pipelines[0].Assets[0]
	assert.Equal(t, "load", asset.Type)
	assert.Equal(t, "analytics/assets/analytics/move_users.asset.yml", asset.Path)
	assert.Equal(t, "postgres_prod", asset.Parameters["source_connection"])
	assert.Equal(t, "public.users", asset.Parameters["source_table"])
	assert.Equal(t, "duckdb_default", asset.Parameters["destination_connection"])
	assert.Equal(t, "full-refresh", asset.Parameters["mode"])
}

func TestWorkspaceServiceSummarizesLoadReplication(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/analytics/orders.asset.yml"), []byte("name: analytics.orders\ntype: load\nrun: orders.sling.yml\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/analytics/orders.sling.yml"), []byte("source: pg\ntarget: duckdb\nstreams:\n  public.orders:\n    object: analytics.orders\n"), 0o644))

	workspace := NewWorkspaceService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"))
	state, err := workspace.ComputeState(context.Background())
	require.NoError(t, err)
	require.Empty(t, state.Errors)
	require.Len(t, state.Pipelines, 1)
	require.Len(t, state.Pipelines[0].Assets, 1)
	asset := state.Pipelines[0].Assets[0]
	assert.Equal(t, "load", asset.Type)
	assert.Equal(t, "analytics/assets/analytics/orders.sling.yml", asset.Path)
	assert.Equal(t, "pg", asset.Parameters["source"])
	assert.Equal(t, "duckdb", asset.Parameters["target"])
	assert.Equal(t, "public.orders", asset.Parameters["streams"])
}

func TestAssetServiceDeleteLoadAssetRemovesDefinitionAndReplication(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	definitionPath := filepath.Join(pipelineRoot, "assets/analytics/orders.asset.yml")
	replicationPath := filepath.Join(pipelineRoot, "assets/analytics/orders.sling.yml")
	require.NoError(t, os.WriteFile(definitionPath, []byte("name: analytics.orders\ntype: load\nrun: orders.sling.yml\n"), 0o644))
	require.NoError(t, os.WriteFile(replicationPath, []byte("source: pg\ntarget: duckdb\n"), 0o644))

	resolver := newAssetTestResolver(workspaceRoot)
	service := NewAssetService(AssetDependencies{
		Fs:            afero.NewOsFs(),
		WorkspaceRoot: workspaceRoot,
		ResolveAssetByID: func(ctx context.Context, assetID string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return resolver.ResolveAssetByID(ctx, assetID)
		},
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	response, apiErr := service.Delete(context.Background(), EncodeID("analytics/assets/analytics/orders.sling.yml"))
	require.Nil(t, apiErr)
	assert.Equal(t, "ok", response.Status)
	assert.NoFileExists(t, definitionPath)
	assert.NoFileExists(t, replicationPath)
}

func TestHybridBruinExecutorRunsLoadAssetWithCLI(t *testing.T) {
	workspaceRoot := t.TempDir()
	fakeLoad := filepath.Join(workspaceRoot, "fake-sling")
	require.NoError(t, os.WriteFile(fakeLoad, []byte("#!/bin/sh\nprintf 'sling %s %s %s\\n' \"$1\" \"$2\" \"$3\"\n"), 0o755))
	t.Setenv("RENART_SLING_BINARY", fakeLoad)

	replicationPath := filepath.Join(workspaceRoot, "assets/analytics/orders.sling.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(replicationPath), 0o755))
	require.NoError(t, os.WriteFile(replicationPath, []byte("source: pg\ntarget: duckdb\n"), 0o644))

	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(BuilderConfig, pipeline.CreateTaskFromYamlDefinition(osFS), pipeline.CreateTaskFromFileComments(osFS), osFS, DefaultGlossaryReader, jinja.VariantRendererFactory)
		},
	)
	var chunks bytes.Buffer
	output, err := executor.runLoadAsset(context.Background(), &pipeline.Asset{
		Name:           "analytics.orders",
		Type:           pipeline.AssetType("load"),
		ExecutableFile: pipeline.ExecutableFile{Path: replicationPath},
	}, nil, func(chunk []byte) {
		_, _ = chunks.Write(chunk)
	})
	require.NoError(t, err)
	assert.Equal(t, output, chunks.Bytes())
	assert.Contains(t, string(output), "sling run -r assets/analytics/orders.sling.yml")
}

func TestHybridBruinExecutorRunsPipelineLoadAssetFromPipelineRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	fakeLoad := filepath.Join(workspaceRoot, "fake-sling")
	require.NoError(t, os.WriteFile(fakeLoad, []byte("#!/bin/sh\nprintf 'pwd=%s args=%s %s %s\\n' \"$(pwd)\" \"$1\" \"$2\" \"$3\"\n"), 0o755))
	t.Setenv("RENART_SLING_BINARY", fakeLoad)

	replicationPath := filepath.Join(pipelineRoot, "assets/analytics/orders.sling.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(replicationPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(replicationPath, []byte("source: pg\ntarget: duckdb\n"), 0o644))

	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(BuilderConfig, pipeline.CreateTaskFromYamlDefinition(osFS), pipeline.CreateTaskFromFileComments(osFS), osFS, DefaultGlossaryReader, jinja.VariantRendererFactory)
		},
	)
	output, err := executor.runLoadAsset(context.Background(), &pipeline.Asset{
		Name:           "analytics.orders",
		Type:           pipeline.AssetType("load"),
		ExecutableFile: pipeline.ExecutableFile{Path: replicationPath},
	}, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "pwd="+pipelineRoot)
	assert.Contains(t, string(output), "args=run -r assets/analytics/orders.sling.yml")
}

func TestHybridBruinExecutorRunsLoadAssetThroughUvWhenNoBinaryOverrideExists(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	fakeUv := filepath.Join(workspaceRoot, "fake-uv")
	require.NoError(t, os.WriteFile(fakeUv, []byte("#!/bin/sh\nprintf 'uv %s\\n' \"$*\"\n"), 0o755))
	t.Setenv("RENART_SLING_BINARY", "")
	t.Setenv("SLING_BINARY", "")
	t.Setenv("RENART_UV_BINARY", fakeUv)
	t.Setenv("RENART_SLING_PACKAGE", "sling-test-package")

	replicationPath := filepath.Join(pipelineRoot, "assets/analytics/orders.sling.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(replicationPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(replicationPath, []byte("source: pg\ntarget: duckdb\n"), 0o644))

	executor := NewHybridBruinExecutor(
		workspaceRoot,
		"bruin",
		nil,
		func() *pipeline.Builder {
			osFS := afero.NewOsFs()
			return pipeline.NewBuilder(BuilderConfig, pipeline.CreateTaskFromYamlDefinition(osFS), pipeline.CreateTaskFromFileComments(osFS), osFS, DefaultGlossaryReader, jinja.VariantRendererFactory)
		},
	)
	output, err := executor.runLoadAsset(context.Background(), &pipeline.Asset{
		Name:           "analytics.orders",
		Type:           pipeline.AssetType("load"),
		ExecutableFile: pipeline.ExecutableFile{Path: replicationPath},
	}, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "uv tool run --no-config --python 3.11 --from sling-test-package sling run -r assets/analytics/orders.sling.yml")
}
