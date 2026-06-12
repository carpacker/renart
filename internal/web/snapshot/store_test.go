package snapshot_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	bruingit "github.com/bruin-data/bruin/pkg/git"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"renart/internal/web/scheduler"
	"renart/internal/web/snapshot"
)

func openTestStoreWithDB(t *testing.T) (*snapshot.Store, *sql.DB) {
	t.Helper()
	schedStore, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "state.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = schedStore.Close() })
	return snapshot.NewStore(schedStore.DB()), schedStore.DB()
}

func openTestStore(t *testing.T) *snapshot.Store {
	t.Helper()
	store, _ := openTestStoreWithDB(t)
	return store
}

func writePipelineDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for relPath, content := range files {
		target := filepath.Join(dir, filepath.FromSlash(relPath))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
	}
	return dir
}

func TestDeployAndMaterializeRoundTrip(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml":        "id: p\nname: test\n",
		"assets/a.sql":        "select 1",
		"assets/nested/b.sql": "select 2",
	})

	deployed, created, err := store.Deploy(ctx, "p", dir, "tester")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Len(t, deployed.Manifest, 3)

	dest := t.TempDir()
	require.NoError(t, store.Materialize(ctx, deployed.VersionID, dest))
	content, err := os.ReadFile(filepath.Join(dest, "assets", "nested", "b.sql"))
	require.NoError(t, err)
	assert.Equal(t, "select 2", string(content))
}

func TestDeployDeduplicatesIdenticalContent(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{"pipeline.yml": "id: p\n", "assets/a.sql": "select 1"})

	first, created, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)
	require.True(t, created)

	second, created, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)
	assert.False(t, created, "identical content should not create a new snapshot")
	assert.Equal(t, first.VersionID, second.VersionID)

	// An edit creates a new version; the old one stays materializable.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "a.sql"), []byte("select 1, 2"), 0o644))
	third, created, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)
	assert.True(t, created)
	assert.NotEqual(t, first.VersionID, third.VersionID)

	dest := t.TempDir()
	require.NoError(t, store.Materialize(ctx, first.VersionID, dest))
	content, err := os.ReadFile(filepath.Join(dest, "assets", "a.sql"))
	require.NoError(t, err)
	assert.Equal(t, "select 1", string(content))
}

func TestDeploySkipsJunk(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml":              "id: p\n",
		"assets/a.sql":              "select 1",
		"data.duckdb":               "binary",
		".renart/state.db":          "local",
		"__pycache__/x.cpython.pyc": "cache",
		"assets/__pycache__/y.pyc":  "cache",
		"logs/runs/foo/run.json":    "log",
		".git/objects/aa/bb":        "git",
		"shared/helpers.py":         "def x(): pass",
	})

	deployed, _, err := store.Deploy(context.Background(), "p", dir, "")
	require.NoError(t, err)
	paths := make([]string, 0, len(deployed.Manifest))
	for path := range deployed.Manifest {
		paths = append(paths, path)
	}
	assert.ElementsMatch(t, []string{"pipeline.yml", "assets/a.sql", "shared/helpers.py"}, paths)
}

func TestDriftReport(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml": "id: p\n",
		"assets/a.sql": "select 1",
		"assets/b.sql": "select 2",
	})

	report, err := store.Drift(ctx, "p", dir)
	require.NoError(t, err)
	assert.False(t, report.HasSnapshot)

	deployed, _, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)

	report, err = store.Drift(ctx, "p", dir)
	require.NoError(t, err)
	assert.True(t, report.InSync)
	assert.Equal(t, deployed.VersionID, report.VersionID)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "a.sql"), []byte("select 99"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "assets", "b.sql")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "c.sql"), []byte("select 3"), 0o644))

	report, err = store.Drift(ctx, "p", dir)
	require.NoError(t, err)
	assert.False(t, report.InSync)
	assert.Equal(t, []string{"assets/a.sql"}, report.ChangedFiles)
	assert.Equal(t, []string{"assets/c.sql"}, report.AddedFiles)
	assert.Equal(t, []string{"assets/b.sql"}, report.RemovedFiles)
}

func TestMaterializeForExecutionSatisfiesRepoDiscovery(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml":           "id: p\n",
		"assets/games.asset.yml": "type: ingestr\n",
	})
	deployed, _, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)

	dest := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, store.MaterializeForExecution(ctx, deployed.VersionID, dest))

	// Bruin's ingestr/python operators walk up from the asset path looking
	// for a .git entry; the dummy directory must make the snapshot root
	// discoverable as the repo root.
	repo, err := bruingit.FindRepoFromPath(filepath.Join(dest, "assets"))
	require.NoError(t, err, "ingestr repo discovery must succeed inside a materialized snapshot")
	resolved, err := filepath.EvalSymlinks(repo.Path)
	require.NoError(t, err)
	expected, err := filepath.EvalSymlinks(dest)
	require.NoError(t, err)
	assert.Equal(t, expected, resolved)

	// The snapshot files themselves are intact next to the shim.
	_, err = os.Stat(filepath.Join(dest, "assets", "games.asset.yml"))
	require.NoError(t, err)
}

func TestMaterializeRejectsEscapingPaths(t *testing.T) {
	t.Parallel()
	store, db := openTestStoreWithDB(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{"pipeline.yml": "id: p\n"})
	deployed, _, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)

	// Forge a manifest entry that tries to escape the destination.
	_, err = db.ExecContext(ctx,
		`UPDATE renart_snapshots SET manifest = ? WHERE version_id = ?`,
		`{"../escape.txt": "deadbeef"}`, deployed.VersionID)
	require.NoError(t, err)
	err = store.Materialize(ctx, deployed.VersionID, t.TempDir())
	require.Error(t, err)
}
