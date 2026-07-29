package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	webmodel "renart/internal/web/model"
)

func TestPipelineDefaultConnectionsMustReferenceConfiguredPairs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, ".bruin.yml"),
		[]byte(`default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: duckdb-files/default.duckdb
`),
		0o600,
	))

	service := NewPipelineService(root)
	require.NoError(t, service.validateConfiguredDefaultConnections(
		pipeline.EmptyStringMap{"duckdb": "duckdb-default"},
	))

	err := service.validateConfiguredDefaultConnections(
		pipeline.EmptyStringMap{"duckdb": "missing"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPipelineDefaultConnection)
	assert.Contains(t, err.Error(), `connection "missing" is not configured`)

	err = service.validateConfiguredDefaultConnections(
		pipeline.EmptyStringMap{"postgres": "duckdb-default"},
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPipelineDefaultConnection)
	assert.Contains(t, err.Error(), `platform "postgres" has no configured project connections`)
}

func TestNormalizeDefaultConnectionsRejectsIncompleteAndDuplicateRows(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		input []webmodel.PipelineConfigConnection
	}{
		{
			name:  "missing platform",
			input: []webmodel.PipelineConfigConnection{{Name: "duckdb-default"}},
		},
		{
			name:  "missing connection",
			input: []webmodel.PipelineConfigConnection{{Platform: "duckdb"}},
		},
		{
			name: "duplicate platform",
			input: []webmodel.PipelineConfigConnection{
				{Platform: "duckdb", Name: "one"},
				{Platform: "duckdb", Name: "two"},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := normalizeDefaultConnections(testCase.input)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidPipelineDefaultConnection))
		})
	}
}

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
