package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cloudbigquery "cloud.google.com/go/bigquery"
	"github.com/bruin-data/bruin/pkg/ansisql"
	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type bigQueryRenderParityConnection struct {
	query               string
	datasetPreparations int
}

func (c *bigQueryRenderParityConnection) RunQueryWithoutResult(_ context.Context, q *query.Query) error {
	c.query = q.Query
	return nil
}

func (c *bigQueryRenderParityConnection) Ping(context.Context) error { return nil }

func (c *bigQueryRenderParityConnection) Select(context.Context, *query.Query) ([][]interface{}, error) {
	return nil, nil
}

func (c *bigQueryRenderParityConnection) SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error) {
	return &query.QueryResult{}, nil
}

func (c *bigQueryRenderParityConnection) UpdateTableMetadataIfNotExist(context.Context, *pipeline.Asset) error {
	return nil
}

func (c *bigQueryRenderParityConnection) IsPartitioningOrClusteringMismatch(context.Context, *cloudbigquery.TableMetadata, *pipeline.Asset) bool {
	return false
}

func (c *bigQueryRenderParityConnection) CreateDataSetIfNotExist(*pipeline.Asset, context.Context) error {
	c.datasetPreparations++
	return nil
}

func (c *bigQueryRenderParityConnection) IsMaterializationTypeMismatch(context.Context, *cloudbigquery.TableMetadata, *pipeline.Asset) bool {
	return false
}

func (c *bigQueryRenderParityConnection) DropTableOnMismatch(context.Context, string, *pipeline.Asset) error {
	return nil
}

func (c *bigQueryRenderParityConnection) BuildTableExistsQuery(string) (string, error) {
	return "SELECT 1", nil
}

func (c *bigQueryRenderParityConnection) UsesApplicationDefaultCredentials() bool { return false }

type snowflakeRenderParityConnection struct {
	query              string
	schemaPreparations int
}

func (c *snowflakeRenderParityConnection) RunQueryWithoutResult(_ context.Context, q *query.Query) error {
	c.query = q.Query
	return nil
}

func (c *snowflakeRenderParityConnection) Select(context.Context, *query.Query) ([][]interface{}, error) {
	return nil, nil
}

func (c *snowflakeRenderParityConnection) Ping(context.Context) error { return nil }

func (c *snowflakeRenderParityConnection) SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error) {
	return &query.QueryResult{}, nil
}

func (c *snowflakeRenderParityConnection) CreateSchemaIfNotExist(context.Context, *pipeline.Asset) error {
	c.schemaPreparations++
	return nil
}

func (c *snowflakeRenderParityConnection) PushColumnDescriptions(context.Context, *pipeline.Asset) error {
	return nil
}

func (c *snowflakeRenderParityConnection) RecreateTableOnMaterializationTypeMismatch(context.Context, *pipeline.Asset) error {
	return nil
}

func (c *snowflakeRenderParityConnection) SelectOnlyLastResult(context.Context, *query.Query) ([][]interface{}, error) {
	return nil, nil
}

func TestBigQueryAndSnowflakeRenderedHooksMatchDirectSubmission(t *testing.T) {
	t.Run("bigquery", func(t *testing.T) {
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  google_cloud_platform: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: bq.sql
materialization:
  type: table
  strategy: append
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select '{{ start_date }}' as window_start
`})
		writeWarehouseRenderConfig(t, root, `
default_environment: default
environments:
  default:
    connections:
      google_cloud_platform:
        - name: warehouse-default
          project_id: renart-test
`)
		req := AssetRenderRequest{StartDate: "2026-07-15T00:00:00Z", EndDate: "2026-07-16T00:00:00Z"}
		outcome := renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/report.sql", req, "")
		execution := requireWarehouseExecutionStage(t, outcome)
		require.Equal(t, AssetRenderFidelityExact, execution.Fidelity)

		connection := &bigQueryRenderParityConnection{}
		executor := newCompatDirectExecutor(root, "")
		executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
			return &stubConnectionManager{conn: connection}, nil
		}
		_, err := executor.RunAsset(context.Background(), RunAssetRequest{
			AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
			StartDate: req.StartDate,
			EndDate:   req.EndDate,
		}, nil)
		require.NoError(t, err)

		assert.Equal(t, execution.Content, connection.query)
		assert.Contains(t, connection.query, "SELECT 'pre 2026-07-15';")
		assert.Contains(t, connection.query, "SELECT 'post 2026-07-16';")
		assert.NotContains(t, connection.query, "{{")
		assert.Equal(t, 1, connection.datasetPreparations)
	})

	t.Run("snowflake", func(t *testing.T) {
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  snowflake: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: sf.sql
materialization:
  type: table
  strategy: append
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select '{{ start_date }}' as window_start
`})
		writeWarehouseRenderConfig(t, root, `
default_environment: default
environments:
  default:
    connections:
      snowflake:
        - name: warehouse-default
          account: renart-test
          username: renart
          password: not-used
          database: analytics
          warehouse: compute
`)
		req := AssetRenderRequest{StartDate: "2026-07-15T00:00:00Z", EndDate: "2026-07-16T00:00:00Z"}
		outcome := renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/report.sql", req, "")
		execution := requireWarehouseExecutionStage(t, outcome)
		require.Equal(t, AssetRenderFidelityExact, execution.Fidelity)

		connection := &snowflakeRenderParityConnection{}
		executor := newCompatDirectExecutor(root, "")
		executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
			return &stubConnectionManager{conn: connection}, nil
		}
		_, err := executor.RunAsset(context.Background(), RunAssetRequest{
			AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
			StartDate: req.StartDate,
			EndDate:   req.EndDate,
		}, nil)
		require.NoError(t, err)

		assert.Equal(t, execution.Content, connection.query)
		assert.Contains(t, connection.query, "SELECT 'pre 2026-07-15';")
		assert.Contains(t, connection.query, "SELECT 'post 2026-07-16';")
		assert.NotContains(t, connection.query, "{{")
		assert.Equal(t, 1, connection.schemaPreparations)
	})
}

func TestBigQueryRenderIncludesInBandQueryAnnotation(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  google_cloud_platform: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: bq.sql
materialization:
  type: table
  strategy: append
@bruin */
select 1 as report_id
`})
	writeWarehouseRenderConfig(t, root, `
default_environment: default
environments:
  default:
    connections:
      google_cloud_platform:
        - name: warehouse-default
          project_id: renart-test
`)

	plain := requireWarehouseExecutionStage(t, renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/report.sql", AssetRenderRequest{}, ""))
	annotated := requireWarehouseExecutionStage(t, renderWarehouseWorkspaceOutcome(
		t,
		root,
		"analytics/assets/report.sql",
		AssetRenderRequest{},
		ansisql.DefaultQueryAnnotations,
	))
	expected, err := ansisql.AddAnnotationComment(
		context.WithValue(context.Background(), pipeline.RunConfigQueryAnnotations, ansisql.DefaultQueryAnnotations),
		&query.Query{Query: plain.Content},
		"analytics.report",
		"main",
		"analytics",
	)
	require.NoError(t, err)
	assert.Equal(t, expected.Query, annotated.Content)
	assert.Equal(t, AssetRenderFidelityExact, annotated.Fidelity)
	assert.Contains(t, annotated.Content, "-- @bruin.config:")
}

func TestBigQueryOperatorPreflightStagesAreConnectionFreeAndSecretFree(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  google_cloud_platform: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: tenant_project.analytics.report
type: bq.sql
materialization:
  type: table
  strategy: append
@bruin */
select 1 as report_id
`})
	writeWarehouseRenderConfig(t, root, `
default_environment: default
environments:
  default:
    connections:
      google_cloud_platform:
        - name: warehouse-default
          project_id: configured-project
          service_account_json: '{"private_key":"TOP_SECRET"}'
          max_billable_bytes: 1048576
          max_query_cost: 0.25
`)

	outcome := renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/report.sql", AssetRenderRequest{FullRefresh: true}, "")
	assert.Equal(t, []string{"query_cost_guard", "dataset_preparation", "target_compatibility", "execution_sql"}, warehouseStageKinds(outcome))
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, outcome.stages[0].Fidelity)
	assert.Equal(t, AssetRenderFidelitySemantic, outcome.stages[1].Fidelity)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, outcome.stages[2].Fidelity)
	assert.Contains(t, outcome.stages[0].Content, `"max_billable_bytes": 1048576`)
	assert.Contains(t, outcome.stages[0].Content, `"max_query_cost_usd": 0.25`)
	assert.Contains(t, outcome.stages[1].Content, `"project": "tenant_project"`)
	for _, stage := range outcome.stages {
		assert.NotContains(t, stage.Content, "TOP_SECRET")
		assert.NotContains(t, stage.Message, "TOP_SECRET")
	}
}

func TestBigQueryOperatorPreflightGatesMatchRuntime(t *testing.T) {
	t.Parallel()
	asset := &pipeline.Asset{
		Name: "analytics.report",
		Type: pipeline.AssetTypeBigqueryQuery,
	}
	info := &directPipelineInfo{Asset: asset, Pipeline: &pipeline.Pipeline{
		Name:               "analytics",
		DefaultConnections: pipeline.EmptyStringMap{"google_cloud_platform": "warehouse-default"},
	}}

	withoutRefresh := assetRenderSemanticOutcome{status: AssetRenderStatusOK}
	appendBigQueryOperatorStages(&withoutRefresh, info, false)
	assert.Empty(t, warehouseStageKinds(withoutRefresh))

	withRefresh := assetRenderSemanticOutcome{status: AssetRenderStatusOK}
	appendBigQueryOperatorStages(&withRefresh, info, true)
	assert.Equal(t, []string{"target_compatibility"}, warehouseStageKinds(withRefresh), "the operator runs its global full-refresh check even without materialization")

	asset.Materialization.Strategy = pipeline.MaterializationStrategyDDL
	ddlRefresh := assetRenderSemanticOutcome{status: AssetRenderStatusOK}
	appendBigQueryOperatorStages(&ddlRefresh, info, true)
	assert.Empty(t, warehouseStageKinds(ddlRefresh), "BigQuery explicitly skips compatibility preflight for DDL")

	softLimit := int64(1024)
	info.Config = &config.Config{SelectedEnvironment: &config.Environment{Connections: &config.Connections{
		GoogleCloudPlatform: []config.GoogleCloudPlatformConnection{{
			ConnectionMetadata:   config.ConnectionMetadata{Name: "warehouse-default"},
			MaxBillableBytesSoft: &softLimit,
		}},
	}}}
	softOnly := assetRenderSemanticOutcome{status: AssetRenderStatusOK}
	appendBigQueryOperatorStages(&softOnly, info, false)
	assert.NotContains(t, warehouseStageKinds(softOnly), "query_cost_guard", "direct asset runs do not enable BigQuery's soft-limit context")
}

func TestBigQuerySensorCostGuardStagesMatchRuntimeGates(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  google_cloud_platform: warehouse-default
`, map[string]string{
		"query-ready.asset.yml": `
name: analytics.query_ready
type: bq.sensor.query
parameters:
  query: select count(*) from analytics.events
`,
		"table-ready.asset.yml": `
name: analytics.table_ready
type: bq.sensor.table
parameters:
  table: analytics.events
`,
	})
	writeWarehouseRenderConfig(t, root, `
default_environment: default
environments:
  default:
    connections:
      google_cloud_platform:
        - name: warehouse-default
          project_id: configured-project
          service_account_json: '{"private_key":"TOP_SECRET"}'
          max_billable_bytes: 2048
          max_query_cost: 0.5
`)

	queryResult, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/query-ready.asset.yml",
		AssetRenderRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, AssetRenderStatusPartial, queryResult.Status)
	assert.Equal(t, []string{"compiled_query", "query_cost_guard", "execution_sql"}, renderStageKinds(queryResult.Stages))
	assert.Contains(t, queryResult.Stages[1].Content, `"max_billable_bytes": 2048`)
	assert.Contains(t, queryResult.Stages[1].Content, `"max_query_cost_usd": 0.5`)

	tableResult, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/table-ready.asset.yml",
		AssetRenderRequest{},
	)
	require.NoError(t, err)
	assert.Equal(t, AssetRenderStatusPartial, tableResult.Status)
	assert.Equal(t, []string{"query_cost_guard", "condition"}, renderStageKinds(tableResult.Stages))

	for _, result := range []AssetRenderResult{queryResult, tableResult} {
		for _, stage := range result.Stages {
			assert.NotContains(t, stage.Content, "TOP_SECRET")
			assert.NotContains(t, stage.Message, "TOP_SECRET")
		}
	}
}

func TestSnowflakeOperatorStagesExposeRuntimeBoundariesAndRefreshDiscrepancy(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  snowflake: warehouse-default
`, map[string]string{"history.sql": `
/* @bruin
name: RAW.HISTORY.CUSTOMERS
type: sf.sql
parameters:
  warehouse: PRIVATE_OVERRIDE
refresh_restricted: true
materialization:
  type: table
  strategy: scd2_by_column
columns:
  - name: id
    type: NUMBER
    primary_key: true
  - name: label
    type: VARCHAR
@bruin */
select 1 as id, 'first' as label
`})
	writeWarehouseRenderConfig(t, root, `
default_environment: prod
environments:
  prod:
    connections:
      snowflake:
        - name: warehouse-default
          account: renart-test
          username: renart
          password: TOP_SECRET
          database: RAW
          warehouse: COMPUTE
`)

	outcome := renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/history.sql", AssetRenderRequest{Environment: "prod", FullRefresh: true}, "")
	assert.Equal(t, []string{"warehouse_selection", "schema_preparation", "schema_preparation", "target_compatibility", "execution_sql"}, warehouseStageKinds(outcome))
	assert.Equal(t, "CREATE DATABASE IF NOT EXISTS RAW", outcome.stages[1].Content)
	assert.Equal(t, "CREATE SCHEMA IF NOT EXISTS RAW.HISTORY", outcome.stages[2].Content)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, outcome.stages[0].Fidelity)
	assert.Equal(t, AssetRenderFidelitySemantic, outcome.stages[1].Fidelity)
	assert.Equal(t, AssetRenderFidelityRuntimeOnly, outcome.stages[3].Fidelity)
	assert.NotContains(t, warehouseStageKinds(outcome), "scd2_migration", "the operator gates migration with requested/global full refresh")
	require.Len(t, outcome.issues, 1)
	assert.Equal(t, "full_refresh_preflight_differs_from_materializer", outcome.issues[0].Code)
	assert.Contains(t, outcome.issues[0].Message, "asset-restricted refresh mode")
	assert.Contains(t, outcome.issues[0].Message, "skips its incremental SCD2 migration")
	for _, stage := range outcome.stages {
		assert.NotContains(t, stage.Content, "PRIVATE_OVERRIDE")
		assert.NotContains(t, stage.Content, "TOP_SECRET")
		assert.NotContains(t, stage.Message, "PRIVATE_OVERRIDE")
		assert.NotContains(t, stage.Message, "TOP_SECRET")
	}

	incremental := renderWarehouseWorkspaceOutcome(t, root, "analytics/assets/history.sql", AssetRenderRequest{Environment: "prod"}, "")
	assert.Contains(t, warehouseStageKinds(incremental), "scd2_migration")
	assert.NotContains(t, warehouseStageKinds(incremental), "target_compatibility")
}

func TestEnvironmentRefreshRestrictionDisablesGlobalWarehousePreflight(t *testing.T) {
	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  google_cloud_platform: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: bq.sql
materialization:
  type: table
  strategy: append
@bruin */
select 1 as report_id
`})
	writeWarehouseRenderConfig(t, root, `
default_environment: prod
environments:
  prod:
    config:
      full_refresh_restricted: true
    connections:
      google_cloud_platform:
        - name: warehouse-default
          project_id: renart-test
`)

	outcome := renderWarehouseWorkspaceOutcome(
		t,
		root,
		"analytics/assets/report.sql",
		AssetRenderRequest{Environment: "prod", FullRefresh: true},
		"",
	)
	assert.NotContains(t, warehouseStageKinds(outcome), "target_compatibility")
	assert.Empty(t, outcome.issues, "the operator and materializer both receive the restricted run-scoped mode")
	execution := requireWarehouseExecutionStage(t, outcome)
	assert.Contains(t, execution.Content, "INSERT INTO analytics.report")
	assert.NotContains(t, execution.Content, "CREATE OR REPLACE TABLE")
}

func TestBigQuerySnowflakeEphemeralIdentifierClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		assetType        pipeline.AssetType
		strategy         pipeline.MaterializationStrategy
		effectiveRefresh bool
		expected         bool
	}{
		{name: "bigquery delete insert", assetType: pipeline.AssetTypeBigqueryQuery, strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "snowflake delete insert", assetType: pipeline.AssetTypeSnowflakeQuery, strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "bigquery merge", assetType: pipeline.AssetTypeBigqueryQuery, strategy: pipeline.MaterializationStrategyMerge},
		{name: "snowflake scd2", assetType: pipeline.AssetTypeSnowflakeQuery, strategy: pipeline.MaterializationStrategySCD2ByColumn},
		{name: "full refresh is deterministic", assetType: pipeline.AssetTypeBigqueryQuery, strategy: pipeline.MaterializationStrategyDeleteInsert, effectiveRefresh: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			asset := &pipeline.Asset{
				Type: test.assetType,
				Materialization: pipeline.Materialization{
					Type:     pipeline.MaterializationTypeTable,
					Strategy: test.strategy,
				},
			}
			assert.Equal(t, test.expected, executionMaterializationUsesEphemeralIdentifiers(asset, test.effectiveRefresh))
		})
	}
}

func TestBigQuerySnowflakeTimeIntervalRenderRequiresExtractor(t *testing.T) {
	t.Parallel()

	for _, assetType := range []pipeline.AssetType{
		pipeline.AssetTypeBigqueryQuery,
		pipeline.AssetTypeSnowflakeQuery,
	} {
		t.Run(string(assetType), func(t *testing.T) {
			t.Parallel()
			asset := &pipeline.Asset{
				Name: "analytics.events",
				Type: assetType,
				Materialization: pipeline.Materialization{
					Type:            pipeline.MaterializationTypeTable,
					Strategy:        pipeline.MaterializationStrategyTimeInterval,
					IncrementalKey:  "event_date",
					TimeGranularity: pipeline.MaterializationTimeGranularityDate,
				},
			}
			_, err := renderBigQuerySnowflakeMaterializerSQL(asset, nil, "SELECT event_date FROM source.events", false)
			require.EqualError(t, err, "time_interval execution rendering requires a query extractor")
		})
	}
}

func renderWarehouseWorkspaceOutcome(t *testing.T, root, assetPath string, req AssetRenderRequest, annotations string) assetRenderSemanticOutcome {
	t.Helper()
	fs := afero.NewOsFs()
	info, err := getDirectPipelineAndAssetReadOnly(context.Background(), root, filepath.Join(root, filepath.FromSlash(assetPath)), fs)
	require.NoError(t, err)
	_, err = selectConfigEnvironment(info.Config, req.Environment)
	require.NoError(t, err)
	applySelectedEnvironmentRefreshRestriction(info.Config, info.Pipeline.Assets)
	executionFullRefresh := req.FullRefresh && !selectedEnvironmentRestrictsFullRefresh(info.Config)
	effectiveFullRefresh := executionFullRefresh && !assetRefreshRestricted(info.Asset)
	executionTime := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	timeWindow, err := ResolveExecutionTimeWindow(string(info.Pipeline.Schedule), req.StartDate, req.EndDate, executionTime)
	require.NoError(t, err)
	renderer, err := buildAssetPlanRenderer(fs, info.Pipeline, timeWindow, executionTime, assetRenderPreviewRunID)
	require.NoError(t, err)
	renderCtx := assetPlanRenderContext(context.Background(), info.Config, timeWindow, executionTime, assetRenderPreviewRunID, effectiveFullRefresh)
	if annotations != "" {
		renderCtx = context.WithValue(renderCtx, pipeline.RunConfigQueryAnnotations, annotations)
	}
	extractor := newDirectSQLQueryExtractor(fs, renderer, info.Asset.Type)
	assetExtractor, err := extractor.CloneForAsset(renderCtx, info.Pipeline, info.Asset)
	require.NoError(t, err)
	source, err := querySourceForRenderAsset(info.Asset)
	require.NoError(t, err)
	queries, err := assetExtractor.ExtractQueriesFromString(source)
	require.NoError(t, err)
	compiled, err := compiledQueryForRenderAsset(info.Asset, queries)
	require.NoError(t, err)
	return renderBigQuerySnowflakeExecution(
		renderCtx,
		info,
		renderer,
		assetExtractor,
		compiled,
		executionFullRefresh,
		effectiveFullRefresh,
	)
}

func requireWarehouseExecutionStage(t *testing.T, outcome assetRenderSemanticOutcome) AssetRenderStage {
	t.Helper()
	for _, stage := range outcome.stages {
		if stage.Kind == "execution_sql" {
			return stage
		}
	}
	t.Fatalf("execution_sql stage not found in %#v", outcome.stages)
	return AssetRenderStage{}
}

func warehouseStageKinds(outcome assetRenderSemanticOutcome) []string {
	kinds := make([]string, 0, len(outcome.stages))
	for _, stage := range outcome.stages {
		kinds = append(kinds, stage.Kind)
	}
	return kinds
}

func writeWarehouseRenderConfig(t *testing.T, root, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte(strings.TrimSpace(content)+"\n"), 0o644))
}
