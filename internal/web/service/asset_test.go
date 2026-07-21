package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/service/assetmeta"
)

func newAssetTestResolver(workspaceRoot string) *WorkspaceResolver {
	return NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		osFS := afero.NewOsFs()
		builder := pipeline.NewBuilder(
			BuilderConfig,
			pipeline.CreateTaskFromYamlDefinition(osFS),
			pipeline.CreateTaskFromFileComments(osFS),
			osFS,
			nil,
			jinja.VariantRendererFactory,
		)
		return builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
}

type stubDependencyParser struct {
	usedTables                 []string
	missingDependencies        []string
	err                        error
	getMissingDependenciesCall int
	usedTablesCall             int
}

func (s *stubDependencyParser) Start() error {
	return nil
}

func (s *stubDependencyParser) ColumnLineage(string, string, sqlparser.Schema) (*sqlparser.Lineage, error) {
	return nil, nil
}

func (s *stubDependencyParser) UsedTables(string, string) ([]string, error) {
	s.usedTablesCall++
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.usedTables...), nil
}

func (s *stubDependencyParser) RenameTables(string, string, map[string]string) (string, error) {
	return "", nil
}

func (s *stubDependencyParser) AddLimit(string, int, string) (string, error) {
	return "", nil
}

func (s *stubDependencyParser) IsSingleSelectQuery(string, string) (bool, error) {
	return true, nil
}

func (s *stubDependencyParser) GetMissingDependenciesForAsset(*pipeline.Asset, *pipeline.Pipeline, jinja.RendererInterface) ([]string, error) {
	s.getMissingDependenciesCall++
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.missingDependencies...), nil
}

func (s *stubDependencyParser) Close() error {
	return nil
}

func TestApplyManualAssetUpstreamsPreservesInferred(t *testing.T) {
	t.Parallel()

	// analytics.orders is a legacy-tracked inferred dep; analytics.manual_seed
	// is the manual one the user is (re)declaring. The inferred dep must be
	// preserved and the manual one recorded in renart_dep_add, while the legacy
	// key is migrated away.
	asset := &pipeline.Asset{
		Name: "analytics.customers",
		Meta: pipeline.EmptyStringMap{
			assetmeta.KeyLegacyInferredUpstreams: "analytics.orders",
		},
		Upstreams: []pipeline.Upstream{
			{Type: "asset", Value: "analytics.manual_seed", Mode: pipeline.UpstreamModeFull},
			{Type: "asset", Value: "analytics.orders", Mode: pipeline.UpstreamModeFull},
		},
	}
	p := &pipeline.Pipeline{
		Assets: []*pipeline.Asset{
			{Name: "analytics.customers"},
			{Name: "analytics.manual_seed"},
			{Name: "analytics.orders"},
		},
	}

	applyManualAssetUpstreams(asset, p, []string{"analytics.manual_seed"})

	assert.Equal(t, []string{"analytics.orders", "analytics.manual_seed"}, upstreamValues(asset.Upstreams))
	assert.Equal(t, "a:analytics.manual_seed#full", asset.Meta[assetmeta.KeyDepAdd])
	_, hasLegacy := asset.Meta[assetmeta.KeyLegacyInferredUpstreams]
	assert.False(t, hasLegacy, "legacy inferred key should be migrated away")
}

func TestDeriveSQLAssetTypeForIngestrSourceUsesDestinationType(t *testing.T) {
	t.Parallel()

	assetType := deriveSQLAssetTypeForSource(&pipeline.Asset{
		Type:       pipeline.AssetType("ingestr"),
		Parameters: pipeline.ParameterMap{"destination": "duckdb"},
	}, nil, "duckdb-default")

	assert.Equal(t, "duckdb.sql", assetType)
}

func TestDeriveSQLAssetTypeForIngestrSourceMapsDestinationConnectionName(t *testing.T) {
	t.Parallel()

	assetType := deriveSQLAssetTypeForSource(&pipeline.Asset{
		Type:       pipeline.AssetType("ingestr"),
		Parameters: pipeline.ParameterMap{"destination": "duckdb-default"},
	}, &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{"duckdb": "duckdb-default"},
	}, "duckdb-default")

	assert.Equal(t, "duckdb.sql", assetType)
}

func TestDeriveSQLAssetTypeForIngestrSourceMapsDestinationConnectionField(t *testing.T) {
	t.Parallel()

	assetType := deriveSQLAssetTypeForSource(&pipeline.Asset{
		Type:       pipeline.AssetType("ingestr"),
		Parameters: pipeline.ParameterMap{"destination_connection": "warehouse"},
	}, &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{"snowflake": "warehouse"},
	}, "warehouse")

	assert.Equal(t, "sf.sql", assetType)
}

func TestDeriveSQLAssetTypeForPythonSourceDoesNotUseConnectionNameAsType(t *testing.T) {
	t.Parallel()

	assetType := deriveSQLAssetTypeForSource(&pipeline.Asset{
		Type: pipeline.AssetType("python"),
	}, nil, "duckdb-default")

	assert.Equal(t, "duckdb.sql", assetType)
}

func TestReconcileSQLAssetDependenciesRemovesOnlyTrackedInferred(t *testing.T) {
	t.Parallel()

	customers := &pipeline.Asset{Name: "analytics.customers"}
	manual := &pipeline.Asset{Name: "analytics.manual_seed"}
	asset := &pipeline.Asset{
		Name: "analytics.orders_report",
		Type: pipeline.AssetTypeDuckDBQuery,
		ExecutableFile: pipeline.ExecutableFile{
			Path:    filepath.Join(t.TempDir(), "orders_report.sql"),
			Content: "select * from analytics.manual_seed",
		},
		Meta: pipeline.EmptyStringMap{
			assetmeta.KeyLegacyInferredUpstreams: "analytics.customers",
		},
		Upstreams: []pipeline.Upstream{
			{Type: "asset", Value: "analytics.manual_seed", Mode: pipeline.UpstreamModeFull},
			{Type: "asset", Value: "analytics.customers", Mode: pipeline.UpstreamModeFull},
		},
	}
	p := &pipeline.Pipeline{
		Name:   "analytics",
		Assets: []*pipeline.Asset{asset, customers, manual},
	}

	parser, err := sqlparser.NewRustSQLParser(false)
	require.NoError(t, err)
	defer parser.Close()

	renderer := jinja.NewRendererWithYesterday("analytics", "test-run")
	require.NoError(t, reconcileSQLAssetDependencies(context.Background(), asset, p, parser, renderer))

	// manual_seed is referenced by SQL (so re-inferred); customers was only
	// tracked by the legacy key and is no longer referenced → dropped. The
	// legacy key is migrated away.
	assert.Equal(t, []string{"analytics.manual_seed"}, upstreamValues(asset.Upstreams))
	_, ok := asset.Meta[assetmeta.KeyLegacyInferredUpstreams]
	assert.False(t, ok)
}

func TestAssetServiceReconcileSQLAssetDependenciesPersistsInferredUpstreams(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets", "analytics")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select *
from analytics.orders
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte(strings.TrimSpace(`
/* @bruin
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as order_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolveAssetByID,
	})

	require.NoError(t, service.reconcileSQLAssetDependencies(context.Background(), "analytics/assets/analytics/customers.sql"))

	content, err := os.ReadFile(filepath.Join(assetsRoot, "customers.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "depends:\n  - analytics.orders")
	// The inferred dep is reconstructable from depends + the projection
	// checksum; the deprecated explicit-list key must not be written.
	assert.NotContains(t, string(content), "renart_inferred_upstreams")
	assert.Contains(t, string(content), "renart_sig_deps:")

	_, parsedPipeline, asset, err := resolveAssetByID(context.Background(), EncodeID("analytics/assets/analytics/customers.sql"))
	require.NoError(t, err)
	require.NotNil(t, parsedPipeline)
	assert.Equal(t, []string{"analytics.orders"}, upstreamValues(asset.Upstreams))
	assert.NotEmpty(t, asset.Meta[assetmeta.KeySigDeps])
	_, hasLegacy := asset.Meta[assetmeta.KeyLegacyInferredUpstreams]
	assert.False(t, hasLegacy)
}

func TestAssetServiceUpdateAllowsIncompleteAPIAssetEdits(t *testing.T) {
	workspaceRoot := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", workspaceRoot, "init").Run())
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\ndefault_connections:\n  duckdb: duckdb-default\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "weather.asset.yml")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
type: api
parameters:
  request:
    url: https://api.weather.gov/alerts
    method: GET
  response:
    records_path: ".features"
`)+"\n"), 0o644))

	// The API asset carries a nested `parameters:` spec, so resolution needs the
	// api-aware builder (the stock reader errors on the nested maps).
	resolver := NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		return NewRenartPipelineBuilder(nil).CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                              workspaceRoot,
		ResolveAssetByID:                           resolver.ResolveAssetByID,
		SuppressWatcher:                            func(string) {},
		PushWorkspaceUpdateImmediate:               func(context.Context, string, string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})
	assetID := EncodeID("analytics/assets/weather.asset.yml")

	connection := "duckdb-default"
	_, apiErr := service.Update(context.Background(), assetID, AssetUpdateRequest{Connection: &connection})
	require.Nil(t, apiErr)

	content, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "connection: duckdb-default")
	// The unmanaged request spec must survive the write.
	assert.Contains(t, string(content), "records_path")

	// Clearing it removes the key again.
	empty := ""
	_, apiErr = service.Update(context.Background(), assetID, AssetUpdateRequest{Connection: &empty})
	require.Nil(t, apiErr)
	content, err = os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "connection:")
	assert.Contains(t, string(content), "records_path")

	// Selecting merge and marking the primary key are separate UI writes. The
	// first write must persist even though the asset is not executable until the
	// second one supplies a primary-key column.
	materializationType := "table"
	materializationStrategy := "merge"
	updateKey := "updated_at"
	_, apiErr = service.Update(context.Background(), assetID, AssetUpdateRequest{
		MaterializationType:     &materializationType,
		MaterializationStrategy: &materializationStrategy,
		IncrementalKey:          &updateKey,
	})
	require.Nil(t, apiErr)
	content, err = os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "type: table")
	assert.Contains(t, string(content), "strategy: merge")
	assert.Contains(t, string(content), "incremental_key: updated_at")

	// Both metadata editors persist column merge keys through this endpoint.
	_, apiErr = service.UpdateAssetColumns(context.Background(), assetID, []any{
		map[string]any{"name": "id", "type": "integer", "primary_key": true},
		map[string]any{"name": "updated_at", "type": "timestamp"},
	})
	require.Nil(t, apiErr)

	content, err = os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "primary_key: true")

	_, _, updatedAsset, resolveErr := resolver.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, resolveErr)
	args, materializationErr := slingMaterializationArgs(context.Background(), updatedAsset)
	require.NoError(t, materializationErr)
	assert.Equal(t, []string{"--mode", "incremental", "--primary-key", "id", "--update-key", "updated_at"}, args)
}

func TestAssetServiceUpdateRoundTripsCompleteMaterializationBlock(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	assetPath := filepath.Join(assetsRoot, "events.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.events
type: bq.sql
columns:
  - name: event_at
    type: TIMESTAMP
  - name: customer_id
    type: STRING
  - name: region
    type: STRING
@bruin */

select current_timestamp() as event_at, '1' as customer_id, 'eu' as region
`)+"\n"), 0o644))

	resolver := newAssetTestResolver(workspaceRoot)
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolver.ResolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})
	assetID := EncodeID("analytics/assets/events.sql")
	materializationType := "table"
	strategy := "time_interval"
	incrementalKey := "event_at"
	partitionBy := "DATE(event_at)"
	timeGranularity := "timestamp"

	_, apiErr := service.Update(context.Background(), assetID, AssetUpdateRequest{
		MaterializationType:     &materializationType,
		MaterializationStrategy: &strategy,
		IncrementalKey:          &incrementalKey,
		PartitionBy:             &partitionBy,
		ClusterBy:               []string{"customer_id", " region ", "customer_id"},
		TimeGranularity:         &timeGranularity,
	})
	require.Nil(t, apiErr)

	content, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "strategy: time_interval")
	assert.Contains(t, string(content), "incremental_key: event_at")
	assert.Contains(t, string(content), "partition_by: DATE(event_at)")
	assert.Contains(t, string(content), "cluster_by:")
	assert.Contains(t, string(content), "time_granularity: timestamp")

	_, _, updated, err := resolver.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.MaterializationStrategyTimeInterval, updated.Materialization.Strategy)
	assert.Equal(t, "event_at", updated.Materialization.IncrementalKey)
	assert.Equal(t, "DATE(event_at)", updated.Materialization.PartitionBy)
	assert.Equal(t, []string{"customer_id", "region"}, updated.Materialization.ClusterBy)
	assert.Equal(t, pipeline.MaterializationTimeGranularityTimestamp, updated.Materialization.TimeGranularity)

	invalidGranularity := "hour"
	_, apiErr = service.Update(context.Background(), assetID, AssetUpdateRequest{TimeGranularity: &invalidGranularity})
	require.NotNil(t, apiErr)
	assert.Equal(t, "invalid_time_granularity", apiErr.Code)
	_, _, unchanged, err := resolver.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, pipeline.MaterializationTimeGranularityTimestamp, unchanged.Materialization.TimeGranularity)
}

func TestAssetServiceUpdatePersistsManualUpstreamsInHeader(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as customer_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "manual_seed.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.manual_seed
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as seed_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})

	content := "select 1 as customer_id"
	_, apiErr := service.Update(context.Background(), EncodeID("analytics/assets/customers.sql"), AssetUpdateRequest{
		Content:   &content,
		Upstreams: []string{"analytics.manual_seed"},
	})
	require.Nil(t, apiErr)

	fileContent, err := os.ReadFile(filepath.Join(assetsRoot, "customers.sql"))
	require.NoError(t, err)
	assert.Contains(t, string(fileContent), "depends:\n  - analytics.manual_seed")

	_, _, asset, err := resolveAssetByID(context.Background(), EncodeID("analytics/assets/customers.sql"))
	require.NoError(t, err)
	assert.Equal(t, []string{"analytics.manual_seed"}, upstreamValues(asset.Upstreams))
}

func TestAssetServiceUpdateManualUpstreamsWithUnchangedContentReconcilesDependencies(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as order_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "manual_seed.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.manual_seed
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as seed_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.orders
meta:
  renart_inferred_upstreams: analytics.orders
@bruin */

select 1 as customer_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID
	parser := &stubDependencyParser{missingDependencies: []string{"analytics.orders"}}
	previousFactory := newDependencyParser
	newDependencyParser = func() (sqlparser.Parser, error) { return parser, nil }
	t.Cleanup(func() {
		newDependencyParser = previousFactory
	})

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})

	content := "select 1 as customer_id"
	_, apiErr := service.Update(context.Background(), EncodeID("analytics/assets/customers.sql"), AssetUpdateRequest{
		Content:   &content,
		Upstreams: []string{"analytics.manual_seed"},
	})
	require.Nil(t, apiErr)
	assert.NotZero(t, parser.getMissingDependenciesCall)

	_, _, asset, err := resolveAssetByID(context.Background(), EncodeID("analytics/assets/customers.sql"))
	require.NoError(t, err)
	assert.Equal(t, []string{"analytics.orders", "analytics.manual_seed"}, upstreamValues(asset.Upstreams))
	assert.Equal(t, "a:analytics.manual_seed#full", asset.Meta[assetmeta.KeyDepAdd])
	assert.NotEmpty(t, asset.Meta[assetmeta.KeySigDeps])
	fileContent, err := os.ReadFile(filepath.Join(assetsRoot, "customers.sql"))
	require.NoError(t, err)
	// inferred deps are listed before manual ones (§19)
	assert.Contains(t, string(fileContent), "depends:\n  - analytics.orders\n  - analytics.manual_seed")
}

func TestAssetServiceCreateReconcilesSQLDependenciesImmediately(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets", "analytics")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as order_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             resolveAssetByID,
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		SourceAssetID: EncodeID("analytics/assets/analytics/orders.sql"),
	})
	require.Nil(t, apiErr)
	require.Equal(t, "ok", response.Status)
	require.Equal(t, "analytics/assets/analytics/orders_child_1.sql", response.AssetPath)

	assetPath := filepath.Join(workspaceRoot, filepath.FromSlash(response.AssetPath))
	content, err := os.ReadFile(assetPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "name:")
	assert.NotContains(t, string(content), "source connection:")
	assert.Contains(t, string(content), "depends:\n  - analytics.orders")
	assert.NotContains(t, string(content), "renart_inferred_upstreams")
	assert.Contains(t, string(content), "renart_sig_deps:")

	_, _, asset, err := resolveAssetByID(context.Background(), response.AssetID)
	require.NoError(t, err)
	assert.Equal(t, []string{"analytics.orders"}, upstreamValues(asset.Upstreams))
	assert.NotEmpty(t, asset.Meta[assetmeta.KeySigDeps])
	assert.Equal(t, "analytics.orders_child_1", asset.Name)
}

func TestAssetServiceCreateMergesExecutableContentIntoSQLTemplate(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets", "analytics"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:              "analytics.report",
		Type:              "duckdb.sql",
		Path:              "assets/analytics/report.sql",
		ExecutableContent: "select 7 as value\n",
	})
	require.Nil(t, apiErr)
	require.Equal(t, "ok", response.Status)

	content, err := os.ReadFile(filepath.Join(workspaceRoot, filepath.FromSlash(response.AssetPath)))
	require.NoError(t, err)
	assert.Contains(t, string(content), "@bruin")
	assert.Contains(t, string(content), "type: duckdb.sql")
	assert.Equal(t, "select 7 as value", strings.TrimSpace(ExtractExecutableContent(string(content))))
}

func TestAssetServiceCreateDownstreamFromUnprefixedSource(t *testing.T) {
	t.Parallel()

	// A notebook-promoted asset lives directly under assets/ with an unprefixed
	// name. A downstream asset created from it carries a user-provided prefixed
	// name, and its path must encode that prefix so bruin infers it back.
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "cell_4.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: cell_4
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             resolveAssetByID,
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:          "marts.cell_4_downstream",
		SourceAssetID: EncodeID("analytics/assets/cell_4.sql"),
	})
	require.Nil(t, apiErr)
	require.Equal(t, "ok", response.Status)
	require.Equal(t, "analytics/assets/marts/cell_4_downstream.sql", response.AssetPath)

	_, _, asset, err := resolveAssetByID(context.Background(), response.AssetID)
	require.NoError(t, err)
	assert.Equal(t, "marts.cell_4_downstream", asset.Name)
	assert.Equal(t, []string{"cell_4"}, upstreamValues(asset.Upstreams))
}

func TestAssetServiceCreateRejectsUnprefixedAssetName(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	_, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name: "orders",
		Type: "duckdb.sql",
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, "missing_asset_prefix", apiErr.Code)
}

func TestAssetServiceCreateWritesDroppedSeedFile(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(filepath.Join(pipelineRoot, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		DefaultAssetContent:          DefaultAssetContent,
		DerivedAssetContent:          DefaultDerivedSQLAssetContent,
		EnsurePythonProject:          func(string, string, string) error { return nil },
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	response, apiErr := service.Create(context.Background(), EncodeID("analytics"), CreateAssetParams{
		Name:            "analytics.regional_customers",
		Type:            "duckdb.seed",
		Path:            "assets/analytics/regional_customers.asset.yml",
		Content:         "name: analytics.regional_customers\ntype: duckdb.seed\n\nparameters:\n  path: ./regional_customers.csv\n",
		SeedFileName:    "regional_customers.csv",
		SeedFileContent: "customer_id,customer_name\n10,Lin\n",
	})
	require.Nil(t, apiErr)
	assert.Equal(t, "analytics/assets/analytics/regional_customers.asset.yml", response.AssetPath)

	seedContent, err := os.ReadFile(filepath.Join(pipelineRoot, "assets/analytics/regional_customers.csv"))
	require.NoError(t, err)
	assert.Equal(t, "customer_id,customer_name\n10,Lin\n", string(seedContent))
}

func TestAssetServiceUpdateReconcilesSQLDependenciesImmediately(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as order_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.manual_seed
@bruin */

select 1 as customer_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})

	content := "select *\nfrom analytics.orders\n"
	_, apiErr := service.Update(context.Background(), EncodeID("analytics/assets/customers.sql"), AssetUpdateRequest{
		Content: &content,
	})
	require.Nil(t, apiErr)

	_, _, asset, err := resolveAssetByID(context.Background(), EncodeID("analytics/assets/customers.sql"))
	require.NoError(t, err)
	// analytics.manual_seed was an untracked file dep → adopted as manual;
	// analytics.orders is now inferred from the SQL.
	assert.Equal(t, []string{"analytics.orders", "analytics.manual_seed"}, upstreamValues(asset.Upstreams))
	assert.Equal(t, "a:analytics.manual_seed#full", asset.Meta[assetmeta.KeyDepAdd])
	assert.NotEmpty(t, asset.Meta[assetmeta.KeySigDeps])
}

func TestAssetServiceUpdateChangedContentReconcilesDependenciesViaParser(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.orders
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as order_id
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.manual_seed
@bruin */

select 1 as customer_id
`)+"\n"), 0o644))

	resolveAssetByID := newAssetTestResolver(workspaceRoot).ResolveAssetByID
	parser := &stubDependencyParser{missingDependencies: []string{"analytics.orders"}}
	previousFactory := newDependencyParser
	newDependencyParser = func() (sqlparser.Parser, error) { return parser, nil }
	t.Cleanup(func() {
		newDependencyParser = previousFactory
	})

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: resolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})

	content := "select *\nfrom analytics.orders\n"
	_, apiErr := service.Update(context.Background(), EncodeID("analytics/assets/customers.sql"), AssetUpdateRequest{
		Content: &content,
	})
	require.Nil(t, apiErr)
	assert.NotZero(t, parser.getMissingDependenciesCall)

	_, _, asset, err := resolveAssetByID(context.Background(), EncodeID("analytics/assets/customers.sql"))
	require.NoError(t, err)
	assert.Equal(t, []string{"analytics.orders", "analytics.manual_seed"}, upstreamValues(asset.Upstreams))
	assert.Equal(t, "a:analytics.manual_seed#full", asset.Meta[assetmeta.KeyDepAdd])
	fileContent, err := os.ReadFile(filepath.Join(assetsRoot, "customers.sql"))
	require.NoError(t, err)
	assert.NotContains(t, string(fileContent), "renart_inferred_upstreams")
	assert.Contains(t, string(fileContent), "renart_sig_deps:")
}

func TestReconcileSQLAssetDependenciesResolvesSameSchemaUnqualifiedNames(t *testing.T) {
	t.Parallel()

	orders := &pipeline.Asset{Name: "analytics.orders"}
	asset := &pipeline.Asset{
		Name: "analytics.customers",
		Type: pipeline.AssetTypeDuckDBQuery,
		ExecutableFile: pipeline.ExecutableFile{
			Path:    filepath.Join(t.TempDir(), "customers.sql"),
			Content: "select * from orders",
		},
	}
	p := &pipeline.Pipeline{
		Name:   "analytics",
		Assets: []*pipeline.Asset{asset, orders},
	}

	parser, err := sqlparser.NewRustSQLParser(false)
	require.NoError(t, err)
	defer parser.Close()

	renderer := jinja.NewRendererWithYesterday("analytics", "test-run")
	require.NoError(t, reconcileSQLAssetDependencies(context.Background(), asset, p, parser, renderer))

	assert.Equal(t, []string{"analytics.orders"}, upstreamValues(asset.Upstreams))
	assert.NotEmpty(t, asset.Meta[assetmeta.KeySigDeps])
}

func TestDeriveDownstreamAssetName_PreservesPrefix(t *testing.T) {
	t.Parallel()

	pl := &pipeline.Pipeline{
		Assets: []*pipeline.Asset{
			{Name: "marts.orders"},
			{Name: "marts.orders_child_1"},
		},
	}

	assert.Equal(t, "marts.orders_child_2", deriveDownstreamAssetName("marts.orders", pl))
}

func upstreamValues(upstreams []pipeline.Upstream) []string {
	values := make([]string, 0, len(upstreams))
	for _, upstream := range upstreams {
		values = append(values, upstream.Value)
	}
	return values
}
