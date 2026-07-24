package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type loadConnectionManagerWithDetails struct {
	connection     any
	connectionType string
	details        any
}

func (m loadConnectionManagerWithDetails) GetConnection(string) any        { return m.connection }
func (m loadConnectionManagerWithDetails) GetConnectionDetails(string) any { return m.details }
func (m loadConnectionManagerWithDetails) GetConnectionType(string) string {
	return m.connectionType
}

func TestLoadConnectionURIUsesSlingTrinoProperties(t *testing.T) {
	t.Parallel()

	manager := loadConnectionManagerWithDetails{
		connection:     "trino://renart@trino.internal:8080/postgresql",
		connectionType: "trino",
		details: &config.TrinoConnection{
			ConnectionMetadata: config.ConnectionMetadata{Name: "trino-default"},
			Host:               "trino.internal",
			Port:               8080,
			Username:           "renart",
			Password:           "p@ssword",
			Catalog:            "postgresql",
			Schema:             "analytics",
		},
	}

	uri, err := loadConnectionURI(manager, "trino-default")
	require.NoError(t, err)
	assert.Equal(t, "trino://renart:p%40ssword@trino.internal:8080?catalog=postgresql&schema=analytics", uri)
}

func TestLoadConnectionURIUsesSlingClickHouseProperties(t *testing.T) {
	t.Parallel()

	secure := 1
	manager := loadConnectionManagerWithDetails{
		connection:     "clickhouse://renart:p%40ssword@clickhouse.internal:9000?http_port=8123&secure=1",
		connectionType: "clickhouse",
		details: &config.ClickHouseConnection{
			ConnectionMetadata: config.ConnectionMetadata{Name: "clickhouse-default"},
			Host:               "clickhouse.internal",
			Port:               9000,
			HTTPPort:           8123,
			Username:           "renart",
			Password:           "p@ssword",
			Database:           "analytics",
			Secure:             &secure,
		},
	}

	uri, err := loadConnectionURI(manager, "clickhouse-default")
	require.NoError(t, err)
	assert.Equal(t, "clickhouse://renart:p%40ssword@clickhouse.internal:9000/analytics?secure=true", uri)
}

func TestLoadConnectionURIUsesSlingDuckLakeProperties(t *testing.T) {
	t.Parallel()

	useSSL := false
	manager := loadConnectionManagerWithDetails{
		connection:     "ducklake://?catalog_database=metadata",
		connectionType: "duckdb",
		details: &config.DuckDBConnection{
			ConnectionMetadata: config.ConnectionMetadata{Name: "ducklake-default"},
			Path:               "/tmp/runner.duckdb",
			Lakehouse: &config.LakehouseConfig{
				Format: config.LakehouseFormatDuckLake,
				Catalog: config.CatalogConfig{
					Type:     config.CatalogTypePostgres,
					Host:     "postgres.internal",
					Port:     5432,
					Database: "metadata",
					Auth: config.CatalogAuth{
						Username: "renart",
						Password: "p'ass word",
					},
				},
				Storage: config.StorageConfig{
					Type:     config.StorageTypeS3,
					Path:     "s3://warehouse/data",
					Region:   "us-east-1",
					Endpoint: "minio.internal:9000",
					URLStyle: "path",
					UseSSL:   &useSSL,
					Auth: config.StorageAuth{
						AccessKey:    "access",
						SecretKey:    "secret",
						SessionToken: "session",
					},
				},
			},
		},
	}

	rawURI, err := loadConnectionURI(manager, "ducklake-default")
	require.NoError(t, err)
	parsed, err := url.Parse(rawURI)
	require.NoError(t, err)
	assert.Equal(t, "ducklake", parsed.Scheme)
	assert.Equal(t, "postgres", parsed.Query().Get("catalog_type"))
	assert.Equal(
		t,
		"postgresql://renart:p%27ass%20word@postgres.internal:5432/metadata",
		parsed.Query().Get("catalog_conn_string"),
	)
	assert.Equal(t, "s3://warehouse/data", parsed.Query().Get("data_path"))
	assert.Equal(t, "minio.internal:9000", parsed.Query().Get("s3_endpoint"))
	assert.Equal(t, "access", parsed.Query().Get("s3_access_key_id"))
	assert.Equal(t, "secret", parsed.Query().Get("s3_secret_access_key"))
	assert.Equal(t, "session", parsed.Query().Get("s3_session_token"))
	assert.Equal(t, "path", parsed.Query().Get("url_style"))
	assert.Equal(t, "false", parsed.Query().Get("use_ssl"))
	assert.Empty(t, parsed.Query().Get("catalog_host"))
	assert.Empty(t, parsed.Query().Get("storage_path"))
}

func TestLoadConnectionURIUsesSlingStarRocksProperties(t *testing.T) {
	t.Parallel()

	manager := loadConnectionManagerWithDetails{
		connection:     "starrocks://renart:p%40ssword@starrocks.internal:9030/analytics?http_port=8030&replication_num=1",
		connectionType: "starrocks",
		details: &config.StarRocksConnection{
			ConnectionMetadata: config.ConnectionMetadata{Name: "starrocks-default"},
			Host:               "starrocks.internal",
			Port:               9030,
			HTTPPort:           8030,
			Username:           "renart",
			Password:           "p@ssword",
			Database:           "analytics",
			ReplicationNum:     1,
		},
	}

	payload, err := loadConnectionURI(manager, "starrocks-default")
	require.NoError(t, err)
	var properties map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &properties))
	assert.Equal(t, "starrocks", properties["type"])
	assert.Equal(t, "starrocks.internal", properties["host"])
	assert.Equal(t, float64(9030), properties["port"])
	assert.Equal(t, "analytics", properties["database"])
	assert.Equal(t, "renart", properties["user"])
	assert.Equal(t, "p@ssword", properties["password"])
	assert.Equal(t, "http://starrocks.internal:8030", properties["fe_url"])
	assert.NotContains(t, payload, "http_port")
	assert.NotContains(t, payload, "replication_num")
}

func TestLoadConnectionURIUsesSupportedPostgresSSLModeForSling(t *testing.T) {
	t.Parallel()

	manager := loadConnectionManagerWithDetails{
		connection:     "postgresql://renart:s3cret@postgres.internal:5432/analytics?sslmode=allow&application_name=renart",
		connectionType: "postgres",
	}

	uri, warning, err := loadConnectionURIWithWarning(manager, "postgres-default")
	require.NoError(t, err)
	assert.Equal(t, "postgresql://renart:s3cret@postgres.internal:5432/analytics?application_name=renart&sslmode=verify-ca", uri)
	assert.Contains(t, warning, "Sling does not support PostgreSQL sslmode \"allow\"")
	assert.Contains(t, warning, "postgres-default")
	assert.NotContains(t, warning, "s3cret")
}

func TestNormalizeSlingPostgresSSLModeLeavesOtherModesAndDriversAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
	}{
		{name: "supported postgres mode", uri: "postgres://localhost/db?sslmode=require"},
		{name: "duckdb option with same name", uri: "duckdb:///tmp/data.db?sslmode=allow"},
		{name: "postgres without ssl mode", uri: "postgresql://localhost/db"},
		{name: "malformed URI", uri: "postgresql://localhost/%"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := normalizeSlingPostgresSSLMode(tt.uri)
			assert.False(t, changed)
			assert.Equal(t, tt.uri, got)
		})
	}
}

func TestLoadRunEnvIncludesResolvedIntervalDates(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 2, 3, 4, 0, time.UTC)
	end := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	ctx := context.WithValue(context.Background(), pipeline.RunConfigStartDate, start)
	ctx = context.WithValue(ctx, pipeline.RunConfigEndDate, end)

	env := loadRunEnv(ctx)
	assert.Contains(t, env, "SLING_DISABLE_TELEMETRY=true")
	assert.Contains(t, env, slingLoadedAtDisabledEnv)
	assert.Contains(t, env, "DEBUGINFOD_URLS=")
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

func TestSlingMaterializationArgsFullRefreshOverridesIncrementalStrategy(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), pipeline.RunConfigFullRefresh, true)
	asset := &pipeline.Asset{Type: pipeline.AssetType("api")}
	asset.Materialization.Strategy = pipeline.MaterializationStrategy("merge")

	args, err := slingMaterializationArgs(ctx, asset)
	require.NoError(t, err)
	assert.Equal(t, []string{"--mode", "full-refresh"}, args)
}

func TestSlingMaterializationArgsFullRefreshPreservesTruncateStrategy(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), pipeline.RunConfigFullRefresh, true)
	asset := &pipeline.Asset{Type: pipeline.AssetType("load")}
	asset.Materialization.Strategy = pipeline.MaterializationStrategyTruncateInsert

	args, err := slingMaterializationArgs(ctx, asset)
	require.NoError(t, err)
	assert.Equal(t, []string{"--mode", "truncate"}, args)
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
	require.NoError(t, os.WriteFile(fakeLoad, []byte("#!/bin/sh\nprintf 'sling %s loaded_at=%s source=%s target=%s\\n' \"$*\" \"$SLING_LOADED_AT_COLUMN\" \"$RENART_SLING_SOURCE\" \"$RENART_SLING_TARGET\"\n"), 0o755))
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
	assert.Contains(t, string(output), "sling run --src-conn RENART_SLING_SOURCE --src-stream public.orders --tgt-conn RENART_SLING_TARGET --tgt-object analytics.orders")
	assert.Contains(t, string(output), "source=postgresql://source target=duckdb://target")
	assert.Contains(t, string(output), "loaded_at=false")
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
