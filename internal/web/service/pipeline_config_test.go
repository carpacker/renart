package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"

	webmodel "renart/internal/web/model"
)

func TestInferPipelineDefaultConnections(t *testing.T) {
	t.Parallel()

	parsed := &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{
			"snowflake": "warehouse",
		},
		Assets: []*pipeline.Asset{
			{Name: "analytics.customers", Type: pipeline.AssetTypeDuckDBQuery},
			{Name: "analytics.orders", Type: pipeline.AssetTypeDuckDBQuery},
			{Name: "analytics.python", Type: pipeline.AssetTypePython},
			{Name: "analytics.snowflake", Type: pipeline.AssetTypeSnowflakeQuery},
			{Name: "analytics.postgres", Type: pipeline.AssetTypePostgresQuery, Connection: "custom-postgres"},
		},
	}

	assert.Equal(t, []webmodel.PipelineConfigConnection{
		{Platform: "duckdb", Name: "duckdb-default"},
	}, inferPipelineDefaultConnections(parsed))
}

func TestInferPipelineDefaultConnectionsSkipsExplicitIngestrDestination(t *testing.T) {
	t.Parallel()

	parsed := &pipeline.Pipeline{Assets: []*pipeline.Asset{{
		Name: "analytics.load",
		Type: pipeline.AssetTypeIngestr,
		Parameters: pipeline.ParameterMap{
			"destination":            "duckdb",
			"destination_connection": "warehouse",
		},
	}}}

	assert.Empty(t, inferPipelineDefaultConnections(parsed))
}

func TestInferPipelineDefaultConnectionsSkipsExplicitAPITarget(t *testing.T) {
	t.Parallel()

	parsed := &pipeline.Pipeline{Assets: []*pipeline.Asset{
		{
			Name: "analytics.explicit_api",
			Type: pipeline.AssetType(apiAssetType),
			ExecutableFile: pipeline.ExecutableFile{Content: `type: api
parameters:
  load:
    target: warehouse
`},
		},
		{
			Name: "analytics.default_api",
			Type: pipeline.AssetType(apiAssetType),
			ExecutableFile: pipeline.ExecutableFile{Content: `type: api
parameters:
  request:
    url: https://example.com/data
`},
		},
	}}

	assert.Equal(t, []webmodel.PipelineConfigConnection{
		{Platform: "duckdb", Name: "duckdb-default"},
	}, inferPipelineDefaultConnections(parsed))
}

func TestReferencedPipelineConnectionsIncludesResolvedAndMultiConnectionAssets(t *testing.T) {
	t.Parallel()

	parsed := &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{"duckdb": "warehouse"},
		Assets: []*pipeline.Asset{
			{Name: "analytics.orders", Type: pipeline.AssetTypeDuckDBQuery},
			{
				Name: "analytics.import",
				Type: pipeline.AssetTypeIngestr,
				Parameters: pipeline.ParameterMap{
					"source_connection":      "source-postgres",
					"destination":            "duckdb",
					"destination_connection": "warehouse",
				},
			},
			{
				Name: "analytics.api",
				Type: pipeline.AssetType(apiAssetType),
				ExecutableFile: pipeline.ExecutableFile{Content: `type: api
parameters:
  request:
    url: https://example.com/data
  load:
    target: api-target
`},
			},
		},
	}

	assert.Equal(t, []webmodel.PipelineReferencedConnection{
		{Name: "api-target", Assets: []string{"analytics.api"}},
		{Name: "source-postgres", Assets: []string{"analytics.import"}},
		{Name: "warehouse", Assets: []string{"analytics.import", "analytics.orders"}},
	}, referencedPipelineConnections(parsed))
}
