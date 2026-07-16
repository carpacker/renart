package snapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceStateDetectsSameSizeInPlaceWrites(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "report.sql")
	require.NoError(t, os.MkdirAll(filepath.Dir(assetPath), 0o755))
	require.NoError(t, os.WriteFile(assetPath, []byte("select 1\n"), 0o644))

	before, err := CollectSourceState(root)
	require.NoError(t, err)
	originalInfo, err := os.Stat(assetPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(assetPath, []byte("select 2\n"), 0o644))
	changedAt := originalInfo.ModTime().Add(time.Second)
	require.NoError(t, os.Chtimes(assetPath, changedAt, changedAt))
	after, err := CollectSourceState(root)
	require.NoError(t, err)
	assert.False(t, before.Equal(after))
}

func TestSourceStateDetectsAtomicReplacementWithPreservedMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	assetPath := filepath.Join(root, "report.sql")
	require.NoError(t, os.WriteFile(assetPath, []byte("select 1\n"), 0o644))
	originalInfo, err := os.Stat(assetPath)
	require.NoError(t, err)
	before, err := CollectSourceState(root)
	require.NoError(t, err)

	replacement := filepath.Join(root, "replacement.sql")
	require.NoError(t, os.WriteFile(replacement, []byte("select 2\n"), originalInfo.Mode()))
	require.NoError(t, os.Chtimes(replacement, originalInfo.ModTime(), originalInfo.ModTime()))
	require.NoError(t, os.Rename(replacement, assetPath))
	after, err := CollectSourceState(root)
	require.NoError(t, err)

	assert.False(t, before.Equal(after), "inode identity must catch replacements even when size and mtime match")
}

func TestSourceStateUsesSnapshotManifestExclusions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "pipeline.yml"), []byte("name: test\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "local.duckdb"), []byte("first"), 0o644))

	before, err := CollectSourceState(root)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "local.duckdb"), []byte("second"), 0o644))
	after, err := CollectSourceState(root)
	require.NoError(t, err)

	assert.True(t, before.Equal(after), "local database changes do not alter snapshot source identity")
}
