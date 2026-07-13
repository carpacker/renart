package service

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestSlingMaterializationArgsFullRefreshRespectsRestriction(t *testing.T) {
	t.Parallel()
	ctx, warnings := withExecutionWarnings(context.WithValue(context.Background(), pipeline.RunConfigFullRefresh, true))
	restricted := true
	asset := &pipeline.Asset{Name: "analytics.events", Type: pipeline.AssetType("api"), RefreshRestricted: &restricted}
	asset.Materialization.Strategy = pipeline.MaterializationStrategyAppend

	args, err := slingMaterializationArgs(ctx, asset)
	require.NoError(t, err)
	assert.Equal(t, []string{"--mode", "snapshot"}, args)
	require.Len(t, warnings.snapshot(), 1)
	assert.Contains(t, warnings.snapshot()[0], "Full refresh is restricted")
}

func TestValidateLoaderMaterializationAllowsIncompleteMergeDuringEditing(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Type: pipeline.AssetType("api")}
	asset.Materialization.Type = pipeline.MaterializationTypeTable
	asset.Materialization.Strategy = pipeline.MaterializationStrategyMerge

	require.NoError(t, validateLoaderMaterialization(asset))
	_, err := slingMaterializationArgs(context.Background(), asset)
	require.ErrorContains(t, err, "primary-key")
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
		Name:       "analytics.orders_load",
		Type:       "load",
		Path:       "assets/analytics/orders_load.asset.yml",
		Connection: "duckdb-default",
		Content:    "type: load\nparameters:\n  destination_connection: obsolete\n",
		Parameters: map[string]string{
			loadParamSourceConnection: "postgres-prod",
			loadParamSourceTable:      "public.orders",
		},
	})
	require.Nil(t, apiErr)
	// A Load asset is now a single flat-parameter .asset.yml — no .sling.yml sidecar.
	assert.Equal(t, "analytics/assets/analytics/orders_load.asset.yml", result.AssetPath)

	definition, err := os.ReadFile(filepath.Join(pipelineRoot, "assets/analytics/orders_load.asset.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(definition), "type: load")
	assert.Contains(t, string(definition), "parameters:")
	assert.Contains(t, string(definition), "connection: duckdb-default")
	assert.Contains(t, string(definition), "source_connection: postgres-prod")
	assert.Contains(t, string(definition), "source_table: public.orders")
	assert.Contains(t, string(definition), "strategy: create+replace")
	assert.NotContains(t, string(definition), "destination_connection:")
	assert.NotContains(t, string(definition), "destination_table:")
	assert.NotContains(t, string(definition), "mode:")
	assert.NotContains(t, string(definition), "run:")

	_, err = os.Stat(filepath.Join(pipelineRoot, "assets/analytics/orders_load.sling.yml"))
	assert.True(t, os.IsNotExist(err), "no replication sidecar should be written")
}

func TestAssetServiceCreateDownstreamLoadUsesSourceAndReplaceMaterialization(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets", "analytics")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\ndefault_connections:\n  duckdb: duckdb-default\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte("/* @bruin\nname: analytics.orders\ntype: duckdb.sql\nmaterialization:\n  type: table\n  strategy: create+replace\n@bruin */\nselect 1 as id\n"), 0o644))

	resolver := newAssetTestResolver(workspaceRoot)
	service := NewAssetService(AssetDependencies{
		Fs:            afero.NewOsFs(),
		WorkspaceRoot: workspaceRoot,
		ResolveAssetByID: func(ctx context.Context, assetID string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
			return resolver.ResolveAssetByID(ctx, assetID)
		},
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	result, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:          "analytics.orders_downstream",
		Type:          "load",
		SourceAssetID: EncodeID("analytics/assets/analytics/orders.sql"),
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "analytics/assets/analytics/orders_downstream.asset.yml", result.AssetPath)

	definition, err := os.ReadFile(filepath.Join(workspaceRoot, filepath.FromSlash(result.AssetPath)))
	require.NoError(t, err)
	content := string(definition)
	assert.Contains(t, content, "depends:\n  - analytics.orders")
	assert.Contains(t, content, "source_connection: duckdb-default")
	assert.Contains(t, content, "source_table: analytics.orders")
	assert.Contains(t, content, "strategy: create+replace")
	assert.NotContains(t, content, "destination_connection:")
	assert.NotContains(t, content, "destination_table:")
	assert.NotContains(t, content, "mode:")

	_, _, created, err := resolver.ResolveAssetByID(context.Background(), result.AssetID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.MaterializationStrategyCreateReplace, created.Materialization.Strategy)
}

func TestWorkspaceServiceLoadsFlatParamLoadAsset(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "assets/analytics/move_users.asset.yml"), []byte(
		"name: analytics.move_users\ntype: load\nconnection: duckdb_default\nparameters:\n  source_connection: postgres_prod\n  source_table: public.users\nmaterialization:\n  type: table\n  strategy: create+replace\n",
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
	assert.Equal(t, "duckdb_default", asset.Connection)
	assert.Equal(t, "duckdb_default", asset.ExplicitConnection)
	assert.Equal(t, "postgres_prod", asset.Parameters["source_connection"])
	assert.Equal(t, "public.users", asset.Parameters["source_table"])
	assert.NotContains(t, asset.Parameters, "destination_connection")
	assert.NotContains(t, asset.Parameters, "destination_table")
	assert.NotContains(t, asset.Parameters, "mode")
}

func TestAssetServiceDeleteLoadAssetRemovesDefinition(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets/analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	definitionPath := filepath.Join(pipelineRoot, "assets/analytics/orders.asset.yml")
	require.NoError(t, os.WriteFile(definitionPath, []byte("name: analytics.orders\ntype: load\nconnection: target\nparameters:\n  source_connection: source\n  source_table: public.orders\nmaterialization:\n  type: table\n  strategy: create+replace\n"), 0o644))

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

	response, apiErr := service.Delete(context.Background(), EncodeID("analytics/assets/analytics/orders.asset.yml"))
	require.Nil(t, apiErr)
	assert.Equal(t, "ok", response.Status)
	assert.NoFileExists(t, definitionPath)
}

type loadTestConnectionManager struct {
	connections map[string]string
}

func (m loadTestConnectionManager) GetConnection(name string) any {
	return m.connections[name]
}

func TestHybridBruinExecutorRunsCanonicalLoadAssetWithCLI(t *testing.T) {
	workspaceRoot := t.TempDir()
	fakeLoad := filepath.Join(workspaceRoot, "fake-sling")
	require.NoError(t, os.WriteFile(fakeLoad, []byte("#!/bin/sh\nprintf 'sling %s\\n' \"$*\"\n"), 0o755))
	t.Setenv("RENART_SLING_BINARY", fakeLoad)

	executor := NewHybridBruinExecutor(workspaceRoot, "bruin", nil, nil)
	manager := loadTestConnectionManager{connections: map[string]string{
		"source": "postgresql://source",
		"target": "duckdb://target",
	}}
	var chunks bytes.Buffer
	output, err := executor.runLoadAsset(context.Background(), &pipeline.Pipeline{}, &pipeline.Asset{
		Name:       "analytics.orders",
		Type:       pipeline.AssetType("load"),
		Connection: "target",
		Parameters: pipeline.ParameterMap{
			loadParamSourceConnection: "source",
			loadParamSourceTable:      "public.orders",
		},
	}, manager, func(chunk []byte) {
		_, _ = chunks.Write(chunk)
	})
	require.NoError(t, err)
	assert.Equal(t, output, chunks.Bytes())
	assert.Contains(t, string(output), "sling run --src-conn postgresql://source --src-stream public.orders --tgt-conn duckdb://target --tgt-object analytics.orders")
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

	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))

	executor := NewHybridBruinExecutor(workspaceRoot, "bruin", nil, nil)
	output, err := executor.runLoadAsset(context.Background(), &pipeline.Pipeline{}, &pipeline.Asset{
		Name:       "analytics.orders",
		Type:       pipeline.AssetType("load"),
		Connection: loadLocalConnectionName,
		Parameters: pipeline.ParameterMap{
			loadParamSourceConnection:  loadLocalConnectionName,
			loadParamSourceTable:       "analytics/data/orders.csv",
			loadParamDestinationObject: "analytics/data/orders-copy.csv",
		},
	}, nil, nil)
	require.NoError(t, err)
	assert.Contains(t, string(output), "uv tool run --no-config --python 3.11 --from sling-test-package sling run --src-stream file://"+filepath.ToSlash(filepath.Join(workspaceRoot, "analytics/data/orders.csv")))
	assert.Contains(t, string(output), "--tgt-object file://"+filepath.ToSlash(filepath.Join(workspaceRoot, "analytics/data/orders-copy.csv")))
}
