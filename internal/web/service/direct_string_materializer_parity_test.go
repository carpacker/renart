package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectStringWarehouseRegistryMatchesSharedMaterializer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		assetType        pipeline.AssetType
		connectionFamily string
		createsSchema    bool
	}{
		{name: "mssql", assetType: pipeline.AssetTypeMsSQLQuery, connectionFamily: "mssql"},
		{name: "vertica", assetType: pipeline.AssetTypeVerticaQuery, connectionFamily: "vertica"},
		{name: "fabric", assetType: pipeline.AssetTypeFabricQuery, connectionFamily: "fabric", createsSchema: true},
		{name: "fabric legacy", assetType: pipeline.AssetTypeFabricQueryLegacy, connectionFamily: "fabric", createsSchema: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipelineYAML := fmt.Sprintf(`
name: analytics
default_connections:
  %s: warehouse-default
`, test.connectionFamily)
			assetSQL := fmt.Sprintf(`
/* @bruin
name: analytics.report
type: %s
materialization:
  type: table
  strategy: append
hooks:
  pre:
    - query: "SELECT 'pre'"
  post:
    - query: "SELECT 'post'"
@bruin */
select 1 as report_id
`, test.assetType)
			_, root := writeTypeCheckWorkspace(t, pipelineYAML, map[string]string{"report.sql": assetSQL})
			materializer, supported, err := newDirectStringExecutionMaterializer(test.assetType, false)
			require.NoError(t, err)
			require.True(t, supported)
			expected, err := materializer.Render(&pipeline.Asset{
				Name: "analytics.report",
				Type: test.assetType,
				Materialization: pipeline.Materialization{
					Type:     pipeline.MaterializationTypeTable,
					Strategy: pipeline.MaterializationStrategyAppend,
				},
				Hooks: pipeline.Hooks{
					Pre:  []pipeline.Hook{{Query: "SELECT 'pre'"}},
					Post: []pipeline.Hook{{Query: "SELECT 'post'"}},
				},
			}, "select 1 as report_id")
			require.NoError(t, err)

			result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{})
			require.NoError(t, err)
			var renderedExecution string
			var schemaStages int
			for _, stage := range result.Stages {
				switch stage.Kind {
				case "execution_sql":
					assert.Equal(t, AssetRenderStageStatusOK, stage.Status)
					assert.Equal(t, AssetRenderFidelityExact, stage.Fidelity)
					renderedExecution = stage.Content
				case "schema_preparation":
					schemaStages++
					assert.Equal(t, AssetRenderFidelitySemantic, stage.Fidelity)
				}
			}
			require.NotEmpty(t, renderedExecution)
			if test.createsSchema {
				assert.Equal(t, 1, schemaStages)
			} else {
				assert.Zero(t, schemaStages, "the runtime does not prepare schemas for this destination")
			}
			assert.Equal(t, expected, renderedExecution)

			connection := &stubSchemaQuerier{}
			executor := newCompatDirectExecutor(root, "")
			executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
				return &stubConnectionManager{conn: connection}, nil
			}
			_, err = executor.RunAsset(context.Background(), RunAssetRequest{
				AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
			}, nil)
			require.NoError(t, err)

			assert.Equal(t, expected, connection.query)
			assert.Contains(t, connection.query, "SELECT 'pre';")
			assert.Contains(t, connection.query, "SELECT 'post';")
		})
	}
}

func TestMSSQLAndVerticaDeleteInsertRenderRuntimeOnly(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name             string
		assetType        pipeline.AssetType
		connectionFamily string
	}{
		{name: "mssql", assetType: pipeline.AssetTypeMsSQLQuery, connectionFamily: "mssql"},
		{name: "vertica", assetType: pipeline.AssetTypeVerticaQuery, connectionFamily: "vertica"},
	} {
		t.Run(test.name, func(t *testing.T) {
			pipelineYAML := fmt.Sprintf("name: analytics\ndefault_connections:\n  %s: warehouse-default\n", test.connectionFamily)
			assetSQL := fmt.Sprintf(`
/* @bruin
name: analytics.events
type: %s
materialization:
  type: table
  strategy: delete+insert
  incremental_key: event_date
@bruin */
select 1 as id, current_date as event_date
`, test.assetType)
			_, root := writeTypeCheckWorkspace(t, pipelineYAML, map[string]string{"events.sql": assetSQL})

			result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{})
			require.NoError(t, err)
			var execution *AssetRenderStage
			for i := range result.Stages {
				if result.Stages[i].Kind == "execution_sql" {
					execution = &result.Stages[i]
				}
				assert.NotEqual(t, "schema_preparation", result.Stages[i].Kind)
			}
			require.NotNil(t, execution)
			assert.Equal(t, AssetRenderFidelityRuntimeOnly, execution.Fidelity)
			assert.Contains(t, execution.Content, "__bruin_tmp_")
			assert.Contains(t, execution.Message, "temporary table identifiers")
		})
	}
}

func TestStringWarehouseEphemeralIdentifierClassification(t *testing.T) {
	restricted := true
	for _, test := range []struct {
		name        string
		assetType   pipeline.AssetType
		fullRefresh bool
		restricted  *bool
		expected    bool
	}{
		{name: "mssql incremental", assetType: pipeline.AssetTypeMsSQLQuery, expected: true},
		{name: "vertica incremental", assetType: pipeline.AssetTypeVerticaQuery, expected: true},
		{name: "fabric deterministic", assetType: pipeline.AssetTypeFabricQuery},
		{name: "legacy fabric deterministic", assetType: pipeline.AssetTypeFabricQueryLegacy},
		{name: "mssql unrestricted full refresh", assetType: pipeline.AssetTypeMsSQLQuery, fullRefresh: true},
		{name: "vertica restricted full refresh", assetType: pipeline.AssetTypeVerticaQuery, fullRefresh: true, restricted: &restricted, expected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			asset := &pipeline.Asset{
				Type:              test.assetType,
				RefreshRestricted: test.restricted,
				Materialization: pipeline.Materialization{
					Type:     pipeline.MaterializationTypeTable,
					Strategy: pipeline.MaterializationStrategyDeleteInsert,
				},
			}
			assert.Equal(t, test.expected, executionMaterializationUsesEphemeralIdentifiers(asset, test.fullRefresh))
		})
	}
}

func TestMSSQLMetadataOnlyDDLMatchesDirectRuntime(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  mssql: warehouse-default
`, map[string]string{"events.sql": `
/* @bruin
name: analytics.events
type: ms.sql
materialization:
  type: table
  strategy: ddl
columns:
  - name: id
    type: int
    primary_key: true
  - name: label
    type: varchar(100)
@bruin */
`})

	result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/events.sql", AssetRenderRequest{})
	require.NoError(t, err)
	var renderedExecution string
	for _, stage := range result.Stages {
		assert.NotEqual(t, "schema_preparation", stage.Kind)
		assert.NotEqual(t, "compiled_query", stage.Kind, "metadata-only DDL has no source query to compile")
		if stage.Kind == "execution_sql" {
			renderedExecution = stage.Content
			assert.Equal(t, AssetRenderFidelityExact, stage.Fidelity)
		}
	}
	require.NotEmpty(t, renderedExecution)
	assert.Contains(t, renderedExecution, "IF SCHEMA_ID")
	assert.Contains(t, renderedExecution, "CREATE TABLE")

	connection := &stubSchemaQuerier{}
	executor := newCompatDirectExecutor(root, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return &stubConnectionManager{conn: connection}, nil
	}
	_, err = executor.RunAsset(context.Background(), RunAssetRequest{
		AssetPath: filepath.Join(root, "analytics", "assets", "events.sql"),
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, renderedExecution, connection.query)
}
