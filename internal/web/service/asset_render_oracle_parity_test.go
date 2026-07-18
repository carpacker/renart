package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetRenderServiceOracleMatchesDirectRuntime(t *testing.T) {
	t.Parallel()

	_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  oracle: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: oracle.sql
@bruin */
select '{{ start_date }}' as window_start from dual
`})

	request := AssetRenderRequest{
		StartDate: "2026-07-15T00:00:00Z",
		EndDate:   "2026-07-16T00:00:00Z",
	}
	result, err := NewAssetRenderService(root).RenderPath(
		context.Background(),
		"analytics/assets/report.sql",
		request,
	)
	require.NoError(t, err)
	execution := requireRenderExecutionStage(t, result)
	assert.Equal(t, AssetRenderStageStatusOK, execution.Status)
	assert.Equal(t, AssetRenderFidelityExact, execution.Fidelity)

	connection := &stubSchemaQuerier{}
	executor := newCompatDirectExecutor(root, "")
	executor.newConnectionManager = func(context.Context, string) (config.ConnectionAndDetailsGetter, error) {
		return &stubConnectionManager{conn: connection}, nil
	}
	_, err = executor.RunAsset(context.Background(), RunAssetRequest{
		AssetPath: filepath.Join(root, "analytics", "assets", "report.sql"),
		StartDate: request.StartDate,
		EndDate:   request.EndDate,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, execution.Content, connection.query)
	assert.Contains(t, execution.Content, "'2026-07-15'")
}

func TestAssetRenderServiceOracleReportsDirectRuntimeLimits(t *testing.T) {
	t.Parallel()

	t.Run("declared hooks are not fabricated", func(t *testing.T) {
		t.Parallel()
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  oracle: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: oracle.sql
hooks:
  pre:
    - query: "select 'pre' from dual"
  post:
    - query: "select 'post' from dual"
@bruin */
select 'main' from dual
`})

		result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{})
		require.NoError(t, err)
		assert.Equal(t, AssetRenderStatusPartial, result.Status)
		execution := requireRenderExecutionStage(t, result)
		assert.Equal(t, "select 'main' from dual", execution.Content)
		assert.Contains(t, execution.Message, "does not execute")
		require.Len(t, result.Issues, 1)
		assert.Equal(t, "oracle_hooks_unsupported", result.Issues[0].Code)
	})

	t.Run("materialization matches the direct runtime error", func(t *testing.T) {
		t.Parallel()
		_, root := writeTypeCheckWorkspace(t, `
name: analytics
default_connections:
  oracle: warehouse-default
`, map[string]string{"report.sql": `
/* @bruin
name: analytics.report
type: oracle.sql
materialization:
  type: table
@bruin */
select 1 as id from dual
`})

		result, err := NewAssetRenderService(root).RenderPath(context.Background(), "analytics/assets/report.sql", AssetRenderRequest{})
		require.NoError(t, err)
		assert.Equal(t, AssetRenderStatusPartial, result.Status)
		execution := requireRenderExecutionStage(t, result)
		assert.Equal(t, AssetRenderStageStatusError, execution.Status)
		assert.Equal(t, "direct oracle execution only supports assets without materialization", execution.Message)
	})
}
