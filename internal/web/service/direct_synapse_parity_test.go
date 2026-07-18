package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectSynapseUsesBruinBatchExecutionSemantics(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  synapse: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: synapse.sql
materialization:
  type: view
hooks:
  pre:
    - query: "SELECT 'pre {{ start_date }}'"
  post:
    - query: "SELECT 'post {{ end_date }}'"
@bruin */
select 'main' as phase
`})

	connection := &hookParityBatchConnection{}
	executor := newCompatDirectExecutor(root, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return &stubConnectionManager{conn: connection}, nil
	}

	_, err := executor.RunAsset(context.Background(), RunAssetRequest{
		AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"SELECT 'pre 2026-07-15';",
		"DROP VIEW IF EXISTS analytics.report;",
		"CREATE VIEW analytics.report AS select 'main' as phase;",
		"SELECT 'post 2026-07-16';",
	}, connection.queries)
}

func TestDirectSynapseFullRefreshHonorsAssetRestriction(t *testing.T) {
	t.Parallel()

	materializer, supported, err := newDirectQueryBatchExecutionMaterializer(pipeline.AssetTypeSynapseQuery, true)
	require.NoError(t, err)
	require.True(t, supported)

	restricted := true
	restrictedQueries, err := materializer.Render(&pipeline.Asset{
		Name:              "analytics.report",
		Type:              pipeline.AssetTypeSynapseQuery,
		RefreshRestricted: &restricted,
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyAppend,
		},
	}, "select 1 as id")
	require.NoError(t, err)
	assert.Equal(t, []string{"INSERT INTO analytics.report select 1 as id"}, restrictedQueries)

	unrestrictedQueries, err := materializer.Render(&pipeline.Asset{
		Name: "analytics.report",
		Type: pipeline.AssetTypeSynapseQuery,
		Materialization: pipeline.Materialization{
			Type:     pipeline.MaterializationTypeTable,
			Strategy: pipeline.MaterializationStrategyAppend,
		},
	}, "select 1 as id")
	require.NoError(t, err)
	require.Len(t, unrestrictedQueries, 1)
	assert.Contains(t, unrestrictedQueries[0], "#bruin_tmp_")
}
