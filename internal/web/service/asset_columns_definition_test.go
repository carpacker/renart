package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInferAssetColumnsFromDefinition checks the asset-as-source-of-truth path:
// a downstream asset's columns are derived from its rendered SQL plus the
// declared columns of its upstream asset — no database is involved.
func TestInferAssetColumnsFromDefinition(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	// Upstream asset with declared columns — the source of truth for types.
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: customer_id
    type: INTEGER
  - name: customer_name
    type: VARCHAR
@bruin */

select 1 as customer_id, 'Ada' as customer_name
`)+"\n"), 0o644))
	// Downstream selecting from the upstream plus a computed column.
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "report.sql"), []byte(strings.TrimSpace(`
/* @bruin
name: analytics.report
type: duckdb.sql
materialization:
  type: view
depends:
  - analytics.customers
@bruin */

select customer_id, upper(customer_name) as shout from analytics.customers
`)+"\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	cols, apiErr := service.InferAssetColumnsFromDefinition(context.Background(), EncodeID("analytics/assets/report.sql"))
	require.Nil(t, apiErr)

	byName := map[string]string{}
	for _, c := range cols {
		byName[c.Name] = c.Type
	}
	// customer_id resolves to its upstream asset's declared type
	assert.Equal(t, "INTEGER", byName["customer_id"])
	// computed column gets a type from the polyglot type annotation
	assert.Equal(t, "VARCHAR", byName["shout"])
}
