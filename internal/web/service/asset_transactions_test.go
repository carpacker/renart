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

func newTransactionWorkspace(t *testing.T, header string) (*AssetService, string, string) {
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
	// Sibling assets so dependency names resolve within the pipeline.
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "orders.sql"), []byte("/* @bruin\nname: analytics.orders\ntype: duckdb.sql\n@bruin */\n\nselect 1 as id\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(assetsRoot, "customers.sql"), []byte(header+"\nselect 1 as order_id\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:                workspaceRoot,
		ResolveAssetByID:             newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		SuppressWatcher:              func(string) {},
		PushWorkspaceUpdateImmediate: func(context.Context, string, string) {},
	})
	return service, EncodeID("analytics/assets/customers.sql"), filepath.Join(assetsRoot, "customers.sql")
}

const txCustomersHeader = `/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */`

func TestApplyTransactionDependencyManualAdd(t *testing.T) {
	service, assetID, absPath := newTransactionWorkspace(t, txCustomersHeader)

	res, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:       TxDependencyManualAdd,
		Dependency: &TransactionDependency{Asset: "analytics.orders", Mode: "symbolic"},
	})
	require.Nil(t, apiErr)
	assert.Equal(t, []string{"analytics.orders"}, res.Upstreams)

	content, err := os.ReadFile(absPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "depends:")
	assert.Contains(t, string(content), "a:analytics.orders#symbolic")
}

func TestApplyTransactionDependencyIgnoreAndRestore(t *testing.T) {
	service, assetID, _ := newTransactionWorkspace(t, txCustomersHeader)

	// Add a dependency, then ignore it.
	_, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:       TxDependencyManualAdd,
		Dependency: &TransactionDependency{Asset: "analytics.orders"},
	})
	require.Nil(t, apiErr)

	res, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:          TxDependencyInferredIgnore,
		DependencyKey: "a:analytics.orders#full",
	})
	require.Nil(t, apiErr)
	assert.Empty(t, res.Upstreams, "ignored dependency should be removed from upstreams")

	_, _, asset, err := service.deps.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, err)
	meta := assetmeta.Parse(asset.Meta)
	require.Len(t, meta.DepDrop, 1)
	assert.Equal(t, "a:analytics.orders#full", meta.DepDrop[0])

	// Restore brings it back and clears the drop.
	res, apiErr = service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:          TxDependencyInferredRestore,
		DependencyKey: "a:analytics.orders#full",
	})
	require.Nil(t, apiErr)
	assert.Equal(t, []string{"analytics.orders"}, res.Upstreams)

	_, _, asset, err = service.deps.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, err)
	assert.Empty(t, assetmeta.Parse(asset.Meta).DepDrop)
}

func TestApplyTransactionColumnOwnPreservesTypeOnReconcile(t *testing.T) {
	header := `/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_total
    type: numeric
@bruin */`
	service, assetID, _ := newTransactionWorkspace(t, header)

	// User takes ownership of the column's type.
	_, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:   TxColumnFieldOwn,
		Column: "order_total",
		Field:  "type",
	})
	require.Nil(t, apiErr)

	// A later inference saying integer must not override the owned type.
	result, apiErr := service.ReconcileAssetColumns(context.Background(), assetID, []webmodel.Column{
		{Name: "order_total", Type: "integer"},
	})
	require.Nil(t, apiErr)
	require.Len(t, result.Columns, 1)
	assert.Equal(t, "numeric", result.Columns[0].Type)
}

func TestApplyTransactionColumnDropAndDescription(t *testing.T) {
	header := `/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_id
    type: integer
  - name: debug
    type: integer
@bruin */`
	service, assetID, _ := newTransactionWorkspace(t, header)

	// Set a description on order_id.
	res, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:        TxColumnDescriptionSet,
		Column:      "order_id",
		Description: "the order identifier",
	})
	require.Nil(t, apiErr)
	var orderID *webmodel.Column
	for i := range res.Columns {
		if res.Columns[i].Name == "order_id" {
			orderID = &res.Columns[i]
		}
	}
	require.NotNil(t, orderID)
	assert.Equal(t, "the order identifier", orderID.Description)

	// Drop the debug column.
	res, apiErr = service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:   TxColumnInferredDrop,
		Column: "debug",
	})
	require.Nil(t, apiErr)
	for _, c := range res.Columns {
		if c.Name == "debug" {
			t.Fatalf("debug column should have been dropped: %+v", res.Columns)
		}
	}

	_, _, asset, err := service.deps.ResolveAssetByID(context.Background(), assetID)
	require.NoError(t, err)
	assert.Equal(t, []string{"debug"}, assetmeta.Parse(asset.Meta).ColDrop)
}

func TestApplyTransactionColumnCheckAddAndRemove(t *testing.T) {
	header := `/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
columns:
  - name: order_id
    type: integer
@bruin */`
	service, assetID, _ := newTransactionWorkspace(t, header)

	res, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:   TxColumnCheckAdd,
		Column: "order_id",
		Check:  &webmodel.ColumnCheck{Name: "not_null"},
	})
	require.Nil(t, apiErr)
	require.Len(t, res.Columns, 1)
	require.Len(t, res.Columns[0].Checks, 1)
	assert.Equal(t, "not_null", res.Columns[0].Checks[0].Name)

	res, apiErr = service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{
		Type:   TxColumnCheckRemove,
		Column: "order_id",
		Check:  &webmodel.ColumnCheck{Name: "not_null"},
	})
	require.Nil(t, apiErr)
	require.Len(t, res.Columns, 1)
	assert.Empty(t, res.Columns[0].Checks, "the check should have been removed")
}

func TestApplyTransactionUnknownTypeErrors(t *testing.T) {
	service, assetID, _ := newTransactionWorkspace(t, txCustomersHeader)
	_, apiErr := service.ApplyAssetTransaction(context.Background(), assetID, AssetTransaction{Type: "bogus.tx"})
	require.NotNil(t, apiErr)
}
