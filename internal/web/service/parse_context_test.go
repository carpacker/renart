package service

import (
	"testing"

	"renart/internal/web/sqlintelligence"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetTypeToDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		assetType pipeline.AssetType
		expected  string
	}{
		{pipeline.AssetTypeDuckDBQuery, "duckdb"},
		{pipeline.AssetTypePostgresQuery, "postgres"},
		{pipeline.AssetTypeBigqueryQuery, "bigquery"},
		{pipeline.AssetTypeSynapseQuery, "tsql"},
	}

	for _, tt := range tests {
		t.Run(string(tt.assetType), func(t *testing.T) {
			t.Parallel()
			dialect, err := AssetTypeToDialect(tt.assetType)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, dialect)
		})
	}
}

func TestAssetTypeToDialect_Unsupported(t *testing.T) {
	t.Parallel()

	_, err := AssetTypeToDialect(pipeline.AssetTypePython)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported asset type")
}

func TestBuildParseContextSchema_MergesSuggestedAndAssetColumns(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Name: "marts.orders",
		Columns: []pipeline.Column{
			{Name: "order_id", Type: "integer"},
			{Name: "customer_id", Type: "integer"},
		},
	}

	schema := BuildParseContextSchema(asset, []ParseContextSchemaTable{
		{
			Name: "raw.customers",
			Columns: []ParseContextSchemaColumn{
				{Name: "id", Type: "integer"},
				{Name: "email", Type: "varchar"},
			},
		},
		{
			Name: " ",
			Columns: []ParseContextSchemaColumn{{Name: "ignored", Type: "text"}},
		},
	})

	assert.Equal(t, map[string]string{"id": "integer", "email": "varchar"}, schema["raw.customers"])
	assert.Equal(t, map[string]string{"order_id": "integer", "customer_id": "integer"}, schema["marts.orders"])
	_, exists := schema[" "]
	assert.False(t, exists)
}

func TestBuildParseContextSchema_SkipsBlankColumnNames(t *testing.T) {
	t.Parallel()

	schema := BuildParseContextSchema(nil, []ParseContextSchemaTable{
		{
			Name: "raw.events",
			Columns: []ParseContextSchemaColumn{
				{Name: "", Type: "text"},
				{Name: "event_id", Type: "uuid"},
			},
		},
	})

	assert.Equal(t, map[string]string{"event_id": "uuid"}, schema["raw.events"])
}

func TestParseContextWithSchema_DuckDBPathReferenceDoesNotProduceUnresolvedTableDiagnostic(t *testing.T) {
	t.Parallel()

	parseContext, err := sqlintelligence.ParseContextWithSchema(
		`select * from "./customers.csv"`,
		"duckdb",
		sqlintelligence.Schema{
			"analytics.orders": {"order_id": "integer"},
		},
	)
	require.NoError(t, err)
	require.NotNil(t, parseContext)

	assert.Empty(t, parseContext.Errors)
	assert.Len(t, parseContext.Tables, 1)
	assert.Equal(t, "./customers.csv", parseContext.Tables[0].Name)
	assert.NotContains(t, parseContext.Diagnostics, sqlintelligence.ParseContextDiagnostic{
		Message:  "Unresolved table: ./customers.csv",
		Severity: "error",
	})

	for _, diagnostic := range parseContext.Diagnostics {
		assert.NotEqual(t, "Unresolved table: ./customers.csv", diagnostic.Message)
	}
}
