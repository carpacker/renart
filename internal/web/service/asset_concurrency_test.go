package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConcurrencyAssetWorkspace(t *testing.T) (*AssetService, string, string) {
	t.Helper()
	workspaceRoot := t.TempDir()
	assetPath := filepath.Join("analytics", "assets", "customers.sql")
	absAssetPath := filepath.Join(workspaceRoot, assetPath)
	require.NoError(t, os.MkdirAll(filepath.Join(workspaceRoot, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(absAssetPath), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".bruin.yml"), []byte(strings.TrimSpace(`
default_environment: default
environments:
  default:
    connections:
      duckdb:
        - name: duckdb-default
          path: local.db
`)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "analytics", "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(absAssetPath, []byte(strings.TrimSpace(`
/* @bruin
name: analytics.customers
type: duckdb.sql
materialization:
  type: view
@bruin */

select 1 as id
`)+"\n"), 0o644))

	service := NewAssetService(AssetDependencies{
		WorkspaceRoot:    workspaceRoot,
		ResolveAssetByID: newAssetTestResolver(workspaceRoot).ResolveAssetByID,
		SuppressWatcher:  func(string) {},
		PushWorkspaceUpdateImmediateWithChangedIDs: func(context.Context, string, string, []string) {},
	})
	return service, EncodeID(assetPath), absAssetPath
}

// TestAssetServiceUpdateKeepsHeaderUnderConcurrentSaves guards against the bug
// where overlapping content saves raced their truncate+write cycles, letting one
// goroutine read a torn/empty file, drop the @bruin header, and persist a
// headerless file — which made the asset disappear from the workspace. With the
// per-asset file lock every save must observe a complete file.
func TestAssetServiceUpdateKeepsHeaderUnderConcurrentSaves(t *testing.T) {
	previousFactory := newDependencyParser
	newDependencyParser = func() (sqlparser.Parser, error) { return &stubDependencyParser{usedTables: []string{"analytics.orders"}}, nil }
	t.Cleanup(func() { newDependencyParser = previousFactory })

	service, assetID, absAssetPath := writeConcurrencyAssetWorkspace(t)

	const workers = 24
	const rounds = 6
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				content := fmt.Sprintf("select col_%d_%d from analytics.orders", worker, r)
				_, _ = service.Update(context.Background(), assetID, AssetUpdateRequest{Content: &content})
			}
		}(w)
	}
	wg.Wait()

	fileBytes, err := os.ReadFile(absAssetPath)
	require.NoError(t, err)
	fileContent := string(fileBytes)

	// The header must never be lost — that is what made the asset vanish.
	assert.Contains(t, fileContent, "/* @bruin", "asset lost its @bruin header under concurrent saves")
	assert.Contains(t, fileContent, "@bruin */", "asset lost its closing @bruin marker under concurrent saves")
	assert.Contains(t, fileContent, "type: duckdb.sql")
	// The executable body must be one of the values that was written, intact.
	assert.Regexp(t, `select col_\d+_\d+ from analytics\.orders`, ExtractExecutableContent(fileContent))
}

// TestAssetServiceUpdateIncompleteSQLSucceeds verifies a content save with an
// incomplete (mid-typing) query no longer fails: the content is persisted and
// the header preserved, rather than returning a spurious dependency error.
func TestAssetServiceUpdateIncompleteSQLSucceeds(t *testing.T) {
	previousFactory := newDependencyParser
	newDependencyParser = func() (sqlparser.Parser, error) {
		return &stubDependencyParser{err: fmt.Errorf("could not parse incomplete query")}, nil
	}
	t.Cleanup(func() { newDependencyParser = previousFactory })

	service, assetID, absAssetPath := writeConcurrencyAssetWorkspace(t)

	incomplete := "select * from "
	resp, apiErr := service.Update(context.Background(), assetID, AssetUpdateRequest{Content: &incomplete})
	require.Nil(t, apiErr, "incomplete SQL save should not error")
	assert.Equal(t, "ok", resp.Status)

	fileBytes, err := os.ReadFile(absAssetPath)
	require.NoError(t, err)
	fileContent := string(fileBytes)
	assert.Contains(t, fileContent, "@bruin */", "header must be preserved for incomplete SQL")
	assert.Contains(t, fileContent, "select * from")
}
