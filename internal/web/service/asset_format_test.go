package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetServiceFormatSQLUsesPolyglotAndPersistsResult(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetPath := filepath.Join("analytics", "assets", "customers.sql")
	absAssetPath := filepath.Join(workspaceRoot, assetPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absAssetPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte(strings.TrimSpace(`
name: analytics
schedule: daily
start_date: "2024-01-01"
default_connections:
  duckdb: duckdb-default
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(absAssetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

select old_value from source_table
`)+"\n"), 0o644))

	var suppressedPath string
	var pushedEvent string
	var pushedPath string
	var pushedIDs []string
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		SuppressWatcher:  func(path string) { suppressedPath = path },
		PushWorkspaceUpdateImmediateWithChangedIDs: func(_ context.Context, event, path string, ids []string) {
			pushedEvent = event
			pushedPath = path
			pushedIDs = ids
		},
	})

	assetID := EncodeID(assetPath)
	response, apiErr := service.FormatSQL(context.Background(), assetID, FormatSQLAssetRequest{
		Content: "select a,b from t where x=1",
	})
	require.Nil(t, apiErr)

	expectedSQL := "SELECT\n  a,\n  b\nFROM t\nWHERE\n  x = 1"
	assert.Equal(t, "ok", response.Status)
	assert.Equal(t, assetID, response.AssetID)
	assert.Equal(t, expectedSQL, response.Content)
	assert.Empty(t, response.Error)

	fileBytes, err := os.ReadFile(absAssetPath)
	require.NoError(t, err)
	fileContent := string(fileBytes)
	assert.Contains(t, fileContent, "name: analytics.customers")
	assert.Contains(t, fileContent, "type: duckdb.sql")
	assert.Equal(t, expectedSQL, strings.TrimSpace(ExtractExecutableContent(fileContent)))
	assert.Equal(t, assetPath, suppressedPath)
	assert.Equal(t, "asset.updated", pushedEvent)
	assert.Equal(t, assetPath, pushedPath)
	assert.Equal(t, []string{assetID}, pushedIDs)
}

func TestSQLFormatDialectForAssetTypeUsesBruinDialect(t *testing.T) {
	assert.Equal(t, "postgresql", sqlFormatDialectForAssetType(pipeline.AssetTypePostgresQuery))
	assert.Equal(t, "databricks", sqlFormatDialectForAssetType(pipeline.AssetTypeDatabricksQuery))
	assert.Equal(t, "generic", sqlFormatDialectForAssetType(pipeline.AssetTypePython))
}
