package bruincompat

import (
	"context"
	"testing"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/require"
)

func TestDependencyParserFindsOnlyMissingPipelineAssets(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{
		Name: "analytics.report",
		Type: pipeline.AssetTypeDuckDBQuery,
		ExecutableFile: pipeline.ExecutableFile{
			Content: "select * from analytics.orders join external.customers using (id) join analytics.existing using (id)",
		},
		Upstreams: []pipeline.Upstream{{Type: "asset", Value: "analytics.existing"}},
	}
	pl := &pipeline.Pipeline{
		Name: "analytics",
		Assets: []*pipeline.Asset{
			asset,
			{Name: "analytics.orders"},
			{Name: "analytics.existing"},
		},
	}

	parser := NewDependencyParser(context.Background())
	missing, err := parser.GetMissingDependenciesForAsset(asset, pl, jinja.NewRendererWithYesterday("analytics", "test"))
	require.NoError(t, err)
	require.Equal(t, []string{"analytics.orders"}, missing)
}
