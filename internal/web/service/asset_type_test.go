package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLAssetTypeForConnectionTypeUsesBruinMapping(t *testing.T) {
	t.Parallel()

	tests := map[string]pipeline.AssetType{
		"duckdb":                pipeline.AssetTypeDuckDBQuery,
		"snowflake":             pipeline.AssetTypeSnowflakeQuery,
		"postgres":              pipeline.AssetTypePostgresQuery,
		"google_cloud_platform": pipeline.AssetTypeBigqueryQuery,
		"bigquery":              pipeline.AssetTypeBigqueryQuery,
		"gcp":                   pipeline.AssetTypeBigqueryQuery,
		"mssql":                 pipeline.AssetTypeMsSQLQuery,
		"sqlserver":             pipeline.AssetTypeMsSQLQuery,
	}

	for connectionType, expected := range tests {
		connectionType := connectionType
		expected := expected
		t.Run(connectionType, func(t *testing.T) {
			t.Parallel()

			assetType, ok := sqlAssetTypeForConnectionType(connectionType)
			require.True(t, ok)
			assert.Equal(t, string(expected), assetType)
		})
	}
}

func TestSourceAssetTypeForConnectionTypeUsesBruinMapping(t *testing.T) {
	t.Parallel()

	assetType, ok := sourceAssetTypeForConnectionType("postgres")
	require.True(t, ok)
	assert.Equal(t, pipeline.AssetTypePostgresSource, assetType)
}

func TestConvertDirectSourceTypeToQueryTypeUsesBruinMapping(t *testing.T) {
	t.Parallel()

	assert.Equal(t, pipeline.AssetTypeSnowflakeQuery, convertDirectSourceTypeToQueryType(pipeline.AssetTypeSnowflakeSource))
	assert.Equal(t, pipeline.AssetTypeIngestr, convertDirectSourceTypeToQueryType(pipeline.AssetTypeIngestr))
}
