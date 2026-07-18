package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPipelineTargetUsesOnlyConfiguredDefaultWithoutSQLMajority(t *testing.T) {
	t.Parallel()

	pl := &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{"postgres": "warehouse"},
		Assets: []*pipeline.Asset{
			{Name: "analytics.api", Type: pipeline.AssetType(apiAssetType)},
			{Name: "analytics.load", Type: pipeline.AssetType(loadAssetType)},
		},
	}

	connection, err := defaultPipelineTargetConnection(pl)
	require.NoError(t, err)
	assert.Equal(t, "warehouse", connection)
}

func TestDefaultPipelineTargetKeepsSQLMajorityAheadOfUnrelatedSingleton(t *testing.T) {
	t.Parallel()

	pl := &pipeline.Pipeline{
		DefaultConnections: pipeline.EmptyStringMap{"duckdb": "local"},
		Assets: []*pipeline.Asset{
			{Name: "analytics.report", Type: pipeline.AssetTypePostgresQuery},
		},
	}

	connection, err := defaultPipelineTargetConnection(pl)
	require.NoError(t, err)
	assert.Equal(t, "postgres-default", connection)
}

func TestDefaultPipelineTargetKeepsDuckDBFallbackWithoutConfiguration(t *testing.T) {
	t.Parallel()

	connection, err := defaultPipelineTargetConnection(&pipeline.Pipeline{})
	require.NoError(t, err)
	assert.Equal(t, "duckdb-default", connection)
}

func TestPipelineSQLMajorityCandidateIncludesIngestrDestination(t *testing.T) {
	t.Parallel()

	pl := &pipeline.Pipeline{Assets: []*pipeline.Asset{{
		Type: pipeline.AssetTypeIngestr,
		Parameters: pipeline.ParameterMap{
			"destination": "postgres",
		},
	}}}

	assert.True(t, pipelineHasSQLMajorityCandidate(pl))
}
