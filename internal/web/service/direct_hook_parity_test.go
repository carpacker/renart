package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hookParityBatchConnection struct {
	queries []string
}

type databricksHookParityBatchConnection struct {
	*hookParityBatchConnection
}

func (c *databricksHookParityBatchConnection) CreateSchemaIfNotExist(_ context.Context, _ *pipeline.Asset, _ string) error {
	return nil
}

func newHookParityConnection(assetType pipeline.AssetType) (*hookParityBatchConnection, any) {
	recorder := &hookParityBatchConnection{}
	if assetType == pipeline.AssetTypeDatabricksQuery {
		return recorder, &databricksHookParityBatchConnection{hookParityBatchConnection: recorder}
	}
	return recorder, recorder
}

func (c *hookParityBatchConnection) RunQueryWithoutResult(_ context.Context, queryToRun *query.Query) error {
	c.queries = append(c.queries, queryToRun.Query)
	return nil
}

func (c *hookParityBatchConnection) Select(_ context.Context, _ *query.Query) ([][]interface{}, error) {
	return nil, nil
}

func (c *hookParityBatchConnection) SelectWithSchema(_ context.Context, _ *query.Query) (*query.QueryResult, error) {
	return &query.QueryResult{}, nil
}

func (c *hookParityBatchConnection) Ping(_ context.Context) error {
	return nil
}

func (c *hookParityBatchConnection) CreateSchemaIfNotExist(_ context.Context, _ *pipeline.Asset) error {
	return nil
}

func (c *hookParityBatchConnection) GetResultsLocation() string {
	return "s3://renart-test/query-results"
}

func TestDirectBatchWarehousesExecuteRenderedHooksInOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		assetType        pipeline.AssetType
		connectionFamily string
	}{
		{name: "athena", assetType: pipeline.AssetTypeAthenaQuery, connectionFamily: "athena"},
		{name: "databricks", assetType: pipeline.AssetTypeDatabricksQuery, connectionFamily: "databricks"},
		{name: "clickhouse", assetType: pipeline.AssetTypeClickHouse, connectionFamily: "clickhouse"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			pipelineYAML := fmt.Sprintf(`
name: analytics
default_connections:
  %s: warehouse-default
`, test.connectionFamily)
			assetSQL := fmt.Sprintf(`
/* @bruin
name: analytics.report
type: %s
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select 'main' as phase
`, test.assetType)
			_, root := writeTypeCheckWorkspace(t, pipelineYAML, map[string]string{"report.sql": assetSQL})

			connection, runtimeConnection := newHookParityConnection(test.assetType)
			executor := newCompatDirectExecutor(root, "")
			executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
				return &stubConnectionManager{conn: runtimeConnection}, nil
			}

			_, err := executor.RunAsset(context.Background(), RunAssetRequest{
				AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
				StartDate: "2026-07-15T00:00:00Z",
				EndDate:   "2026-07-16T00:00:00Z",
			}, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"SELECT 'pre 2026-07-15';",
				"select 'main' as phase",
				"SELECT 'post 2026-07-16';",
			}, connection.queries)
		})
	}
}

func TestRemainingStringWarehouseMaterializersUseHookHoister(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		assetType pipeline.AssetType
	}{
		{name: "bigquery", assetType: pipeline.AssetTypeBigqueryQuery},
		{name: "mysql", assetType: pipeline.AssetTypeMySQLQuery},
		{name: "snowflake", assetType: pipeline.AssetTypeSnowflakeQuery},
		{name: "trino", assetType: pipeline.AssetTypeTrinoQuery},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			materializer, supported, err := newDirectStringExecutionMaterializer(test.assetType, false)
			require.NoError(t, err)
			require.True(t, supported)
			require.NotNil(t, materializer.Hoister)

			rendered, err := materializer.Render(&pipeline.Asset{
				Name: "analytics.report",
				Type: test.assetType,
				Hooks: pipeline.Hooks{
					Pre:  []pipeline.Hook{{Query: "SELECT 'pre'"}},
					Post: []pipeline.Hook{{Query: "SELECT 'post'"}},
				},
			}, "SELECT 'main'")
			require.NoError(t, err)
			assert.Equal(t, "SELECT 'pre';\nSELECT 'main';\nSELECT 'post';", rendered)
		})
	}
}
