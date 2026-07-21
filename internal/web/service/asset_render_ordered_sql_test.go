package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderedSQLRenderStagesMatchDirectBatchRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		assetType  pipeline.AssetType
		family     string
		wantKinds  []string
		wantLabels []string
	}{
		{
			name:       "databricks",
			assetType:  pipeline.AssetTypeDatabricksQuery,
			family:     "databricks",
			wantKinds:  []string{"pre_hook", "execution_sql", "execution_sql", "post_hook"},
			wantLabels: []string{"Pre-hook 1", "Execution SQL 1", "Execution SQL 2", "Post-hook 1"},
		},
		{
			name:       "clickhouse",
			assetType:  pipeline.AssetTypeClickHouse,
			family:     "clickhouse",
			wantKinds:  []string{"pre_hook", "execution_sql", "post_hook"},
			wantLabels: []string{"Pre-hook 1", "Execution SQL 1", "Post-hook 1"},
		},
		{
			name:       "synapse",
			assetType:  pipeline.AssetTypeSynapseQuery,
			family:     "synapse",
			wantKinds:  []string{"pre_hook", "execution_sql", "execution_sql", "post_hook"},
			wantLabels: []string{"Pre-hook 1", "Execution SQL 1", "Execution SQL 2", "Post-hook 1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, root := writeTypeCheckWorkspace(t, fmt.Sprintf(`
name: analytics
default_connections:
  %s: warehouse-default
`, tt.family), map[string]string{"report.sql": fmt.Sprintf(`
/* @bruin
name: analytics.report
type: %s
materialization:
  type: view
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select 'main' as phase
`, tt.assetType)})

			connection, runtimeConnection := newHookParityConnection(tt.assetType)
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

			asset := &pipeline.Asset{
				Name: "analytics.report",
				Type: tt.assetType,
				Materialization: pipeline.Materialization{
					Type: pipeline.MaterializationTypeView,
				},
				Hooks: pipeline.Hooks{
					Pre:  []pipeline.Hook{{Query: "SELECT 'pre 2026-07-15'"}},
					Post: []pipeline.Hook{{Query: "SELECT 'post 2026-07-16'"}},
				},
			}
			stages, supported, err := renderExactQueryBatchExecutionStages(asset, nil, "select 'main' as phase", false)
			require.NoError(t, err)
			require.True(t, supported)

			assert.Equal(t, connection.queries, orderedStageContents(stages))
			assert.Equal(t, tt.wantKinds, orderedStageKinds(stages))
			assert.Equal(t, tt.wantLabels, orderedStageLabels(stages))
			for _, stage := range stages {
				assert.Equal(t, AssetRenderStageStatusOK, stage.Status)
				assert.Equal(t, AssetRenderFidelityExact, stage.Fidelity)
			}
		})
	}
}

func TestOrderedSQLRenderStagesReextractTimeIntervalAfterMaterialization(t *testing.T) {
	t.Parallel()

	for _, assetType := range []pipeline.AssetType{
		pipeline.AssetTypeDatabricksQuery,
		pipeline.AssetTypeClickHouse,
		pipeline.AssetTypeSynapseQuery,
	} {
		t.Run(string(assetType), func(t *testing.T) {
			t.Parallel()

			extractor := &trackingReextractor{}
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
			stages, supported, err := renderExactQueryBatchExecutionStages(asset, extractor, "select event_date from source.events", false)
			require.NoError(t, err)
			require.True(t, supported)
			assert.Equal(t, 1, extractor.calls)
			require.Len(t, extractor.input, 2)
			assert.Contains(t, extractor.input[0], "{{start_date}}")
			require.Len(t, stages, 2)
			assert.NotContains(t, stages[0].Content, "{{")
			assert.Contains(t, stages[0].Content, "2026-07-15")
			assert.Equal(t, []string{"execution_sql", "execution_sql"}, orderedStageKinds(stages))
		})
	}
}

func TestOrderedSQLRenderStagesFallBackToGenericStagesWhenDeclareHoistingReorders(t *testing.T) {
	t.Parallel()

	batch := "BEGIN\n  SELECT 1;\n  SELECT 2;\nEND;"
	materializer := &directQueryBatchExecutionMaterializer{
		materializer: fixedQueryBatchMaterializer{queries: []string{batch}},
		hoister: &orderedTestHoister{queries: []string{
			"DECLARE marker INTEGER;",
			"SET marker = 1;",
			batch,
			"SELECT 'post';",
		}},
	}
	asset := &pipeline.Asset{
		Type: pipeline.AssetTypeSynapseQuery,
		Hooks: pipeline.Hooks{
			Pre: []pipeline.Hook{
				{Query: "SET marker = 1"},
				{Query: "DECLARE marker INTEGER"},
			},
			Post: []pipeline.Hook{{Query: "SELECT 'post'"}},
		},
	}

	stages, supported, err := renderExactQueryBatchExecutionStagesWithMaterializer(asset, nil, "ignored", materializer)
	require.NoError(t, err)
	require.True(t, supported)
	assert.Equal(t, []string{"execution_sql", "execution_sql", "execution_sql", "execution_sql"}, orderedStageKinds(stages))
	assert.Equal(t, []string{"Execution SQL 1", "Execution SQL 2", "Execution SQL 3", "Execution SQL 4"}, orderedStageLabels(stages))
	assert.Equal(t, batch, stages[2].Content, "a multi-statement warehouse batch must stay whole")
}

func TestOrderedSQLRenderStagesKeepProvenanceWhenDeclareHoisterFallsBackOnError(t *testing.T) {
	t.Parallel()

	batch := "BEGIN\n  SELECT 1;\n  SELECT 2;\nEND;"
	materializer := &directQueryBatchExecutionMaterializer{
		materializer: fixedQueryBatchMaterializer{queries: []string{batch}},
		hoister:      &orderedTestHoister{err: errors.New("parser unavailable")},
	}
	asset := &pipeline.Asset{
		Type: pipeline.AssetTypeSynapseQuery,
		Hooks: pipeline.Hooks{
			Pre:  []pipeline.Hook{{Query: "DECLARE marker INTEGER"}},
			Post: []pipeline.Hook{{Query: "SELECT 'post'"}},
		},
	}

	stages, supported, err := renderExactQueryBatchExecutionStagesWithMaterializer(asset, nil, "ignored", materializer)
	require.NoError(t, err)
	require.True(t, supported)
	assert.Equal(t, []string{"pre_hook", "execution_sql", "post_hook"}, orderedStageKinds(stages))
	assert.Equal(t, []string{"Pre-hook 1", "Execution SQL 1", "Post-hook 1"}, orderedStageLabels(stages))
	assert.Equal(t, batch, stages[1].Content)
}

func TestAssetRenderWiresOrderedSQLStages(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		assetType pipeline.AssetType
		family    string
	}{
		{name: "databricks", assetType: pipeline.AssetTypeDatabricksQuery, family: "databricks"},
		{name: "clickhouse", assetType: pipeline.AssetTypeClickHouse, family: "clickhouse"},
		{name: "synapse", assetType: pipeline.AssetTypeSynapseQuery, family: "synapse"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, root := writeTypeCheckWorkspace(t, fmt.Sprintf(`
name: analytics
default_connections:
  %s: warehouse-default
`, test.family), map[string]string{"report.sql": fmt.Sprintf(`
/* @bruin
name: analytics.report
type: %s
materialization:
  type: view
hooks:
  pre:
    - query: "SELECT 'pre'"
  post:
    - query: "SELECT 'post'"
@bruin */
select 'main' as phase
`, test.assetType)})

			result, err := NewAssetRenderService(root).RenderPath(
				context.Background(),
				"analytics/assets/report.sql",
				AssetRenderRequest{},
			)
			require.NoError(t, err)
			assert.Contains(t, renderStageKinds(result.Stages), "pre_hook")
			assert.Contains(t, renderStageKinds(result.Stages), "execution_sql")
			assert.Contains(t, renderStageKinds(result.Stages), "post_hook")
			for _, stage := range result.Stages {
				if stage.Kind == "execution_sql" {
					assert.NotEqual(t, AssetRenderStageStatusUnsupported, stage.Status)
				}
			}
		})
	}
}

func TestAssetRenderDatabricksPreparesCatalogAndSchema(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  databricks: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: MAIN.ANALYTICS.REPORT
type: databricks.sql
materialization:
  type: view
@bruin */
select 1 as report_id
`})

	result, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		AssetRenderRequest{},
	)
	require.NoError(t, err)

	preparation := make([]AssetRenderStage, 0, 2)
	for _, stage := range result.Stages {
		if stage.Kind == "schema_preparation" {
			preparation = append(preparation, stage)
		}
	}
	require.Len(t, preparation, 2)
	assert.Equal(t, "Catalog", preparation[0].Label)
	assert.Equal(t, "CREATE CATALOG IF NOT EXISTS MAIN", preparation[0].Content)
	assert.Equal(t, "Schema", preparation[1].Label)
	assert.Equal(t, "CREATE SCHEMA IF NOT EXISTS MAIN.ANALYTICS", preparation[1].Content)
	for _, stage := range preparation {
		assert.Equal(t, AssetRenderFidelitySemantic, stage.Fidelity)
		assert.True(t, stage.Conditional)
	}
}

func TestOrderedSQLRandomIdentifierClassification(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		assetType   pipeline.AssetType
		strategy    pipeline.MaterializationStrategy
		fullRefresh bool
		expected    bool
	}{
		{name: "databricks default", assetType: pipeline.AssetTypeDatabricksQuery, expected: true},
		{name: "databricks delete insert", assetType: pipeline.AssetTypeDatabricksQuery, strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "databricks full refresh append", assetType: pipeline.AssetTypeDatabricksQuery, strategy: pipeline.MaterializationStrategyAppend, fullRefresh: true, expected: true},
		{name: "databricks full refresh scd2", assetType: pipeline.AssetTypeDatabricksQuery, strategy: pipeline.MaterializationStrategySCD2ByColumn, fullRefresh: true},
		{name: "clickhouse delete insert", assetType: pipeline.AssetTypeClickHouse, strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "clickhouse full refresh delete insert", assetType: pipeline.AssetTypeClickHouse, strategy: pipeline.MaterializationStrategyDeleteInsert, fullRefresh: true},
		{name: "synapse default", assetType: pipeline.AssetTypeSynapseQuery, expected: true},
		{name: "synapse delete insert", assetType: pipeline.AssetTypeSynapseQuery, strategy: pipeline.MaterializationStrategyDeleteInsert, expected: true},
		{name: "synapse full refresh append", assetType: pipeline.AssetTypeSynapseQuery, strategy: pipeline.MaterializationStrategyAppend, fullRefresh: true, expected: true},
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
			assert.Equal(t, test.expected, executionMaterializationUsesEphemeralIdentifiers(asset, test.fullRefresh))
		})
	}
}

type fixedQueryBatchMaterializer struct {
	queries []string
}

func (m fixedQueryBatchMaterializer) Render(*pipeline.Asset, string) ([]string, error) {
	return append([]string(nil), m.queries...), nil
}

func (m fixedQueryBatchMaterializer) LogIfFullRefreshAndDDL(interface{}, *pipeline.Asset) error {
	return nil
}

type orderedTestHoister struct {
	queries []string
	err     error
}

func (h *orderedTestHoister) HoistDeclares(sql string, _ pipeline.AssetType) (string, error) {
	return sql, h.err
}

func (h *orderedTestHoister) HoistDeclaresList(queries []string, _ pipeline.AssetType) ([]string, error) {
	if h.err != nil {
		return queries, h.err
	}
	return append([]string(nil), h.queries...), nil
}

type trackingReextractor struct {
	calls int
	input []string
}

func (e *trackingReextractor) ExtractQueriesFromString(string) ([]*query.Query, error) {
	return nil, errors.New("not used")
}

func (e *trackingReextractor) CloneForAsset(context.Context, *pipeline.Pipeline, *pipeline.Asset) (query.QueryExtractor, error) {
	return e, nil
}

func (e *trackingReextractor) ReextractQueriesFromSlice(content []string) ([]string, error) {
	e.calls++
	e.input = append([]string(nil), content...)
	replacer := strings.NewReplacer(
		"{{start_date}}", "2026-07-15",
		"{{end_date}}", "2026-07-16",
	)
	result := make([]string, len(content))
	for index := range content {
		result[index] = replacer.Replace(content[index])
	}
	return result, nil
}

func orderedStageContents(stages []AssetRenderStage) []string {
	result := make([]string, len(stages))
	for index := range stages {
		result[index] = stages[index].Content
	}
	return result
}

func orderedStageKinds(stages []AssetRenderStage) []string {
	result := make([]string, len(stages))
	for index := range stages {
		result[index] = stages[index].Kind
	}
	return result
}

func orderedStageLabels(stages []AssetRenderStage) []string {
	result := make([]string, len(stages))
	for index := range stages {
		result[index] = stages[index].Label
	}
	return result
}

func renderStageKinds(stages []AssetRenderStage) []string {
	result := make([]string, len(stages))
	for index := range stages {
		result[index] = stages[index].Kind
	}
	return result
}
