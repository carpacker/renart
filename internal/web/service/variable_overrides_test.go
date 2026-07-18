package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariableOverridesMutatorAppliesAndValidatesValues(t *testing.T) {
	t.Parallel()
	root := variableOverridePipeline(t)
	builder := NewRenartPipelineBuilder(afero.NewOsFs())
	require.NoError(t, addVariableOverrides(builder, map[string]any{
		"region": "us",
		"limit":  float64(25),
	}))

	parsed, err := builder.CreatePipelineFromPath(context.Background(), root, pipeline.WithMutate())
	require.NoError(t, err)
	assert.Equal(t, "us", parsed.Variables.Value()["region"])
	assert.Equal(t, int64(25), parsed.Variables.Value()["limit"])
}

func TestValidatePipelineVariableOverridesRejectsUnknownAndInvalidValuesWithoutEchoingThem(t *testing.T) {
	t.Parallel()
	root := variableOverridePipeline(t)

	tests := []struct {
		name      string
		overrides map[string]any
		contains  string
		secret    string
	}{
		{
			name: "unknown", overrides: map[string]any{"missing": "private-value"},
			contains: "no such variable \"missing\"", secret: "private-value",
		},
		{
			name: "wrong type", overrides: map[string]any{"limit": "private-value"},
			contains: "variable \"limit\" does not satisfy its declared schema", secret: "private-value",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePipelineVariableOverrides(
				context.Background(), NewRenartPipelineBuilder(afero.NewOsFs()), root, test.overrides,
			)
			require.ErrorIs(t, err, ErrInvalidVariableOverrides)
			assert.Contains(t, err.Error(), test.contains)
			assert.NotContains(t, err.Error(), test.secret)
		})
	}
}

func variableOverridePipeline(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pipeline.yml"), []byte(`
id: variable-override-pipeline
name: variable_override
variables:
  region:
    type: string
    default: eu
  limit:
    type: integer
    default: 10
`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "assets", "orders.sql"), []byte(`
/* @bruin
name: variable_override.orders
type: duckdb.sql
materialization:
  type: table
@bruin */
select {{ var.limit }} as value, '{{ var.region }}' as region
`), 0o644))
	return root
}
