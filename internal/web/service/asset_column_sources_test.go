package service

import (
	"context"
	"os"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	webmodel "renart/internal/web/model"
)

func TestColumnInferenceSourcesAreAssetCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		asset      *pipeline.Asset
		connection string
		want       []string
	}{
		{
			name:       "sql definition and current table",
			asset:      &pipeline.Asset{Type: pipeline.AssetTypeDuckDBQuery},
			connection: "warehouse",
			want:       []string{columnSourceDefinition, columnSourceMaterialized},
		},
		{
			name:       "api definition live response and current table",
			asset:      &pipeline.Asset{Type: pipeline.AssetType("api")},
			connection: "warehouse",
			want:       []string{columnSourceDefinition, columnSourceLiveResponse, columnSourceMaterialized},
		},
		{
			name:       "local seed file and current table",
			asset:      &pipeline.Asset{Type: pipeline.AssetTypeDuckDBSeed, Parameters: pipeline.ParameterMap{"path": "./users.csv"}},
			connection: "warehouse",
			want:       []string{columnSourceDefinition, columnSourceMaterialized},
		},
		{
			name:       "remote seed only current table",
			asset:      &pipeline.Asset{Type: pipeline.AssetTypeDuckDBSeed, Parameters: pipeline.ParameterMap{"path": "https://example.com/users.csv"}},
			connection: "warehouse",
			want:       []string{columnSourceMaterialized},
		},
		{
			name:       "sensor has no schema",
			asset:      &pipeline.Asset{Type: pipeline.AssetTypePostgresQuerySensor},
			connection: "warehouse",
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources := columnInferenceSourcesForAsset(tt.asset, tt.connection)
			ids := make([]string, 0, len(sources))
			for _, source := range sources {
				ids = append(ids, source.ID)
			}
			assert.Equal(t, tt.want, ids)
		})
	}
}

func TestCompareColumnSchemasReportsMeaningfulDrift(t *testing.T) {
	drift := compareColumnSchemas(
		[]pipeline.Column{
			{Name: "id", Type: "INTEGER"},
			{Name: "display_name", Type: "VARCHAR"},
			{Name: "legacy", Type: "TEXT"},
		},
		[]WorkspaceColumn{
			{Name: "id", Type: "int32"},
			{Name: "display_name", Type: "string"},
			{Name: "created_at", Type: "TIMESTAMP"},
		},
	)

	assert.Equal(t, 1, drift.Added)
	assert.Equal(t, 1, drift.Removed)
	assert.Equal(t, 0, drift.TypeChanged)
	assert.Equal(t, 2, drift.Unchanged)
	assert.Equal(t, []webmodel.ColumnSchemaDriftItem{
		{Column: "created_at", Kind: "added", InferredType: "TIMESTAMP"},
		{Column: "legacy", Kind: "removed", CurrentType: "TEXT"},
	}, drift.Items)
}

func TestPreviewAssetColumnsDoesNotPersistUntilApplied(t *testing.T) {
	header := `/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_id
    type: BIGINT
@bruin */`
	service, assetID, absPath := newColumnReconcileWorkspace(t, header)
	before, err := os.ReadFile(absPath)
	require.NoError(t, err)

	preview, apiErr := service.PreviewAssetColumns(context.Background(), assetID, columnSourceDefinition, "")
	require.Nil(t, apiErr)
	require.Equal(t, "ok", preview.Status)
	assert.Equal(t, columnSourceDefinition, preview.Source.ID)
	assert.Equal(t, 1, preview.Drift.TypeChanged)
	assert.Equal(t, "INTEGER", preview.Columns[0].Type)

	after, err := os.ReadFile(absPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}
