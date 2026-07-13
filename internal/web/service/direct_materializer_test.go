package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubQueryBatchMaterializer struct {
	label string
}

func (s stubQueryBatchMaterializer) Render(*pipeline.Asset, string) ([]string, error) {
	return []string{s.label}, nil
}

func (s stubQueryBatchMaterializer) LogIfFullRefreshAndDDL(interface{}, *pipeline.Asset) error {
	return nil
}

func TestRefreshRestrictedQueryBatchMaterializerSelectsPerAsset(t *testing.T) {
	t.Parallel()

	materializer := refreshRestrictedQueryBatchMaterializer{
		configured: stubQueryBatchMaterializer{label: "configured"},
		full:       stubQueryBatchMaterializer{label: "full"},
	}

	unrestricted, err := materializer.Render(&pipeline.Asset{}, "select 1")
	require.NoError(t, err)
	assert.Equal(t, []string{"full"}, unrestricted)

	restrictedValue := true
	restricted, err := materializer.Render(&pipeline.Asset{RefreshRestricted: &restrictedValue}, "select 1")
	require.NoError(t, err)
	assert.Equal(t, []string{"configured"}, restricted)
}
