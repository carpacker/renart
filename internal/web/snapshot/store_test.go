package snapshot_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestLatestUsesTheMigrationTieBreaker(t *testing.T) {
	t.Parallel()
	store, db := openTestStoreWithDB(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, versionID := range []string{"version-a", "version-z"} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO renart_snapshots
				(version_id, pipeline_id, merkle_root, manifest, git_dirty, created_at)
			VALUES (?, 'pipeline', 'root', '{}', 0, ?)`, versionID, createdAt)
		require.NoError(t, err)
	}

	latest, err := store.Latest(ctx, "pipeline")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, "version-z", latest.VersionID)
}

func TestDeployRepairsCorruptContentInsteadOfReusingIt(t *testing.T) {
	t.Parallel()
	store, db := openTestStoreWithDB(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml": "id: p\n",
		"assets/a.sql": "select 1",
	})
	first, created, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)
	require.True(t, created)
	hash := first.Manifest["assets/a.sql"]
	_, err = db.ExecContext(ctx, `UPDATE renart_blobs SET content = ? WHERE hash = ?`, []byte("corrupt"), hash)
	require.NoError(t, err)

	second, created, err := store.Deploy(ctx, "p", dir, "")
	require.NoError(t, err)
	assert.True(t, created, "an invalid deployment must not be returned as a no-op")
	assert.NotEqual(t, first.VersionID, second.VersionID)
	assert.Equal(t, first.MerkleRoot, second.MerkleRoot)
	_, err = store.Validate(ctx, second.VersionID, "p")
	require.NoError(t, err)
	_, err = store.Validate(ctx, first.VersionID, "p")
	require.NoError(t, err, "repairing a content-addressed blob also restores older manifests")
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
	assert.True(t, report.Executable)
	assert.Empty(t, report.IntegrityError)
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

func TestMaterializeRejectsInvalidDestinationBeforeSnapshotLookup(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	for _, destDir := range []string{"", ".", filepath.Join("relative", "run")} {
		err := store.Materialize(context.Background(), "missing", destDir)
		require.ErrorContains(t, err, "materialization destination")
		assert.NotContains(t, err.Error(), "load metadata")
	}
}

func TestMaterializeForPipelineExecutionValidatesOwnershipBeforeWriting(t *testing.T) {
	t.Parallel()
	store := openTestStore(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{"pipeline.yml": "id: p\n"})
	deployed, _, err := store.Deploy(ctx, "pipeline-a", dir, "")
	require.NoError(t, err)

	dest := filepath.Join(t.TempDir(), "run")
	err = store.MaterializeForPipelineExecution(ctx, deployed.VersionID, "pipeline-b", dest)
	require.ErrorContains(t, err, "belongs to pipeline pipeline-a")
	_, statErr := os.Stat(filepath.Join(dest, "pipeline.yml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(filepath.Join(dest, ".git"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
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

func TestValidateRejectsWrongPipelineAndCorruptBlob(t *testing.T) {
	t.Parallel()
	store, db := openTestStoreWithDB(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml": "id: p\n",
		"assets/a.sql": "select 1",
	})
	deployed, _, err := store.Deploy(ctx, "pipeline-a", dir, "")
	require.NoError(t, err)

	_, err = store.Validate(ctx, deployed.VersionID, "pipeline-b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "belongs to pipeline pipeline-a")

	hash := deployed.Manifest["assets/a.sql"]
	_, err = db.ExecContext(ctx, `UPDATE renart_blobs SET content = ? WHERE hash = ?`, []byte("tampered"), hash)
	require.NoError(t, err)

	_, err = store.Validate(ctx, deployed.VersionID, "pipeline-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blob hash mismatch")
	assert.Error(t, store.MaterializeForExecution(ctx, deployed.VersionID, t.TempDir()))

	report, err := store.Drift(ctx, "pipeline-a", dir)
	require.NoError(t, err)
	assert.True(t, report.InSync)
	assert.False(t, report.Executable)
	assert.Contains(t, report.IntegrityError, "blob hash mismatch")
}

func TestValidateRejectsManifestThatDoesNotMatchRecordedRoot(t *testing.T) {
	t.Parallel()
	store, db := openTestStoreWithDB(t)
	ctx := context.Background()
	dir := writePipelineDir(t, map[string]string{
		"pipeline.yml": "id: p\n",
		"assets/a.sql": "select 1",
	})
	deployed, _, err := store.Deploy(ctx, "pipeline-a", dir, "")
	require.NoError(t, err)
	pipelineHash := deployed.Manifest["pipeline.yml"]
	_, err = db.ExecContext(ctx, `UPDATE renart_snapshots SET manifest = ? WHERE version_id = ?`,
		`{"pipeline.yml":"`+pipelineHash+`"}`, deployed.VersionID)
	require.NoError(t, err)

	_, err = store.Validate(ctx, deployed.VersionID, "pipeline-a")
	require.ErrorContains(t, err, "manifest root mismatch")
}
