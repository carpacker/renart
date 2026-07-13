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
