package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
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

func TestInferAPIAssetColumnsFromOpenAPIDefinition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`openapi: 3.0.3
paths:
  /games:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  games:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: integer
                        white_username:
                          type: string
                        rated:
                          type: boolean
`))
	}))
	defer server.Close()

	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "quickstart")
	assetsRoot := filepath.Join(pipelineRoot, "assets")
	require.NoError(t, os.MkdirAll(assetsRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: quickstart\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "games.asset.yml"), []byte(`name: quickstart.games
type: api

parameters:
  openapi:
    url: `+server.URL+`
  request:
    url: https://api.example.com/games
  response:
    records_path: games
`), 0o644))

	resolver := NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		return NewRenartPipelineBuilder(afero.NewOsFs()).CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             resolver.ResolveAssetByID,
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})

	cols, apiErr := service.InferAssetColumnsFromDefinition(context.Background(), EncodeID("quickstart/assets/games.asset.yml"))
	require.Nil(t, apiErr)

	byName := map[string]string{}
	for _, c := range cols {
		byName[c.Name] = c.Type
	}
	assert.Equal(t, "integer", byName["id"])
	assert.Equal(t, "boolean", byName["rated"])
	assert.Equal(t, "string", byName["white_username"])
}
