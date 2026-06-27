package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	webmodel "renart/internal/web/model"
	"renart/internal/web/service/assetmeta"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newColumnReconcileWorkspace(t *testing.T, header string) (*AssetService, string, string) {
	t.Helper()
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
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(header+"\nselect 1 as order_id\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})
	return service, EncodeID("analytics/assets/customers.sql"), filepath.Join(assetsRoot, "customers.sql")
}

func TestReconcileAssetColumnsPreservesUserMetadataAndFlagsStale(t *testing.T) {
	header := strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_id
    type: integer
    description: the order
    primary_key: true
  - name: scratch_note
    type: text
    description: temporary working column
@bruin */`)
	service, assetID, absPath := newColumnReconcileWorkspace(t, header)

	// Inference now reports order_id (type changed) and a brand new column.
	result, apiErr := service.ReconcileAssetColumns(context.Background(), assetID, []webmodel.Column{
		{Name: "order_id", Type: "bigint"},
		{Name: "customer_id", Type: "integer"},
	})
	require.Nil(t, apiErr)

	byName := map[string]webmodel.Column{}
	for _, c := range result.Columns {
		byName[c.Name] = c
	}

	// order_id keeps its user metadata, refreshes its (unowned) type.
	orderID, ok := byName["order_id"]
	require.True(t, ok)
	assert.Equal(t, "bigint", orderID.Type)
	assert.Equal(t, "the order", orderID.Description)
	assert.True(t, orderID.PrimaryKey)

	// customer_id is newly inferred.
	_, ok = byName["customer_id"]
	assert.True(t, ok)

	// scratch_note is no longer inferred but has a description → stale, kept.
	if _, ok := byName["scratch_note"]; !ok {
		t.Fatalf("stale column scratch_note was dropped: %+v", result.Columns)
	}
	require.Len(t, result.ReconcileItems, 1)
	assert.Equal(t, "scratch_note", result.ReconcileItems[0].Column)
	assert.Equal(t, "column.stale", result.ReconcileItems[0].Kind)

	// The provenance checksum is persisted to the file.
	content, err := os.ReadFile(absPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "renart_sig_cols:")
}

func TestReconcileAssetColumnsRespectsTypeOwnership(t *testing.T) {
	header := strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_total
    type: numeric
meta:
  ` + assetmeta.KeyColOwn + `: order_total:type
@bruin */`)
	service, assetID, _ := newColumnReconcileWorkspace(t, header)

	result, apiErr := service.ReconcileAssetColumns(context.Background(), assetID, []webmodel.Column{
		{Name: "order_total", Type: "integer"},
	})
	require.Nil(t, apiErr)
	require.Len(t, result.Columns, 1)
	// Ownership over the type means inference must not override the user's value.
	assert.Equal(t, "numeric", result.Columns[0].Type)
}
