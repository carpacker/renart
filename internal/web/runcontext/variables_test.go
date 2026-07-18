package runcontext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValueFreeVariableProvenanceIsSortedAndLastLayerWins(t *testing.T) {
	t.Parallel()

	provenance := ValueFreeVariableProvenance(
		VariableLayer{Source: VariableSourcePipelineDefault, Names: []string{"threshold", "region", ""}},
		VariableLayer{Source: VariableSourceScheduleOverride, Names: []string{"threshold"}},
		VariableLayer{Source: VariableSourceRunOverride, Names: []string{"region"}},
	)

	assert.Equal(t, []VariableProvenance{
		{Name: "region", Source: VariableSourceRunOverride},
		{Name: "threshold", Source: VariableSourceScheduleOverride},
	}, provenance)
}

func TestValueFreeVariableProvenanceContainsNoValues(t *testing.T) {
	t.Parallel()

	typeShape := ValueFreeVariableProvenance(
		VariableLayer{Source: VariableSourcePipelineDefault, Names: []string{"api_token"}},
	)

	assert.Equal(t, []VariableProvenance{{Name: "api_token", Source: VariableSourcePipelineDefault}}, typeShape)
}
