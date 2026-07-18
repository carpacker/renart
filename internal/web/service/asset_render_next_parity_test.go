package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/ansisql"
	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mysqlRenderParityConnection struct {
	queries               []string
	createSchemaCallCount int
}

func (c *mysqlRenderParityConnection) RunQueryWithoutResult(_ context.Context, q *query.Query) error {
	c.queries = append(c.queries, q.Query)
	return nil
}

func (c *mysqlRenderParityConnection) Select(_ context.Context, _ *query.Query) ([][]interface{}, error) {
	return [][]interface{}{}, nil
}

func (c *mysqlRenderParityConnection) SelectWithSchema(_ context.Context, _ *query.Query) (*query.QueryResult, error) {
	return &query.QueryResult{}, nil
}

func (c *mysqlRenderParityConnection) BuildTableExistsQuery(string) (string, error) {
	return "SELECT 1", nil
}

func (c *mysqlRenderParityConnection) Ping(context.Context) error {
	return nil
}

func (c *mysqlRenderParityConnection) GetDatabaseSummary(context.Context) (*ansisql.DBDatabase, error) {
	return &ansisql.DBDatabase{}, nil
}

func (c *mysqlRenderParityConnection) CreateSchemaIfNotExist(context.Context, *pipeline.Asset) error {
	c.createSchemaCallCount++
	return nil
}

func TestAssetRenderServiceMySQLAppendMatchesDirectRuntime(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  mysql: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: my.sql
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
	request := AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	}

	connection := &mysqlRenderParityConnection{}
	executor := newCompatDirectExecutor(root, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return &stubConnectionManager{conn: connection}, nil
	}
	_, err := executor.RunAsset(context.Background(), RunAssetRequest{
		AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
		StartDate: request.StartDate,
		EndDate:   request.EndDate,
	}, nil)
	require.NoError(t, err)
	require.Len(t, connection.queries, 1)

	result, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		request,
	)
	require.NoError(t, err)
	execution := requireRenderExecutionStage(t, result)
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)

	assert.Equal(t, execution.Content, connection.queries[0])
	assert.Contains(t, connection.queries[0], "SELECT 'pre 2026-07-15';")
	assert.Contains(t, connection.queries[0], "SELECT 'post 2026-07-16';")
	assert.NotContains(t, connection.queries[0], "{{")
	assert.Equal(t, 1, connection.createSchemaCallCount)
}

func TestMySQLExecutionMaterializationEphemeralIdentifierClassification(t *testing.T) {
	t.Parallel()

	restricted := true
	tests := []struct {
		name        string
		strategy    pipeline.MaterializationStrategy
		fullRefresh bool
		restricted  *bool
		expected    bool
	}{
		{name: "append is deterministic", strategy: pipeline.MaterializationStrategyAppend},
		{name: "delete insert", strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "merge", strategy: pipeline.MaterializationStrategyMerge, expected: true},
		{name: "scd2 by time", strategy: pipeline.MaterializationStrategySCD2ByTime, expected: true},
		{name: "scd2 by column", strategy: pipeline.MaterializationStrategySCD2ByColumn, expected: true},
		{name: "unrestricted full refresh replaces deterministically", strategy: pipeline.MaterializationStrategyDeleteInsert, fullRefresh: true},
		{name: "restricted full refresh keeps generated identifier strategy", strategy: pipeline.MaterializationStrategyDeleteInsert, fullRefresh: true, restricted: &restricted, expected: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			asset := &pipeline.Asset{
				Type:              pipeline.AssetTypeMySQLQuery,
				RefreshRestricted: test.restricted,
				Materialization: pipeline.Materialization{
					Type:     pipeline.MaterializationTypeTable,
					Strategy: test.strategy,
				},
			}
			assert.Equal(t, test.expected, executionMaterializationUsesEphemeralIdentifiers(asset, test.fullRefresh))
		})
	}
}

// This proves that RenderPath uses the same Trino materializer factory as
// direct executor construction. Bruin's Trino operator requires a concrete
// *trino.Client, so this is intentionally not described as connection-level
// direct-runtime proof.
func TestAssetRenderServiceTrinoMatchesSharedFactory(t *testing.T) {
	t.Parallel()

	t.Run("append with rendered hooks", func(t *testing.T) {
		t.Parallel()
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  trino: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: trino.sql
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

		result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{
			StartDate: "2026-07-15T00:00:00Z",
			EndDate:   "2026-07-16T00:00:00Z",
		})
		require.NoError(t, err)
		execution := requireRenderExecutionStage(t, result)

		materializer, supported, err := newDirectStringExecutionMaterializer(pipeline.AssetTypeTrinoQuery, false)
		require.NoError(t, err)
		require.True(t, supported)
		expected, err := materializer.Render(&pipeline.Asset{
			Name: "analytics.report",
			Type: pipeline.AssetTypeTrinoQuery,
			Materialization: pipeline.Materialization{
				Type:     pipeline.MaterializationTypeTable,
				Strategy: pipeline.MaterializationStrategyAppend,
			},
			Hooks: pipeline.Hooks{
				Pre:  []pipeline.Hook{{Query: "SELECT 'pre 2026-07-15'"}},
				Post: []pipeline.Hook{{Query: "SELECT 'post 2026-07-16'"}},
			},
		}, "select '2026-07-15' as window_start")
		require.NoError(t, err)

		assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
		assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)
		assert.Equal(t, expected, execution.Content)
		assert.NotContains(t, execution.Content, "{{")
	})

	t.Run("metadata only ddl", func(t *testing.T) {
		t.Parallel()
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  trino: warehouse-default
`, map[string]string{"events.sql": `
/* @bruin
name: analytics.events
type: trino.sql
materialization:
  type: table
  strategy: ddl
columns:
  - name: id
    type: BIGINT
    primary_key: true
  - name: label
    type: VARCHAR
@bruin */
`})

		materializer, supported, err := newDirectStringExecutionMaterializer(pipeline.AssetTypeTrinoQuery, false)
		require.NoError(t, err)
		require.True(t, supported)
		expected, err := materializer.Render(&pipeline.Asset{
			Name: "analytics.events",
			Type: pipeline.AssetTypeTrinoQuery,
			Materialization: pipeline.Materialization{
				Type:     pipeline.MaterializationTypeTable,
				Strategy: pipeline.MaterializationStrategyDDL,
			},
			Columns: []pipeline.Column{
				{Name: "id", Type: "BIGINT", PrimaryKey: true},
				{Name: "label", Type: "VARCHAR"},
			},
		}, "")
		require.NoError(t, err)
		require.NotEmpty(t, expected)

		result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{})
		require.NoError(t, err)
		execution := requireRenderExecutionStage(t, result)
		assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
		assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)
		assert.Equal(t, expected, execution.Content)
		for _, stage := range result.Stages {
			assert.NotEqual(t, "compiled_query", stage.Kind, "metadata-only DDL has no source query to compile")
		}
	})

	t.Run("time interval keeps every rendered statement", func(t *testing.T) {
		t.Parallel()
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  trino: warehouse-default
`, map[string]string{"events.sql": `
/* @bruin
name: analytics.events
type: trino.sql
materialization:
  type: table
  strategy: time_interval
  incremental_key: event_date
  time_granularity: date
@bruin */
select event_date, event_id from source.events
`})

		result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{
			StartDate: "2026-07-15T00:00:00Z",
			EndDate:   "2026-07-16T00:00:00Z",
		})
		require.NoError(t, err)
		execution := requireRenderExecutionStage(t, result)
		assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
		assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)
		assert.Contains(t, execution.Content, `DELETE FROM "analytics"."events"`)
		assert.Contains(t, execution.Content, "DATE '2026-07-15'")
		assert.Contains(t, execution.Content, `INSERT INTO "analytics"."events"`)
		assert.NotContains(t, execution.Content, "{{")
	})
}

func requireRenderExecutionStage(t *testing.T, result AssetRenderResult) AssetRenderStage {
	t.Helper()
	for _, stage := range result.Stages {
		if stage.Kind == "execution_sql" {
			return stage
		}
	}
	t.Fatalf("render result has no execution_sql stage: %#v", result.Stages)
	return AssetRenderStage{}
}
