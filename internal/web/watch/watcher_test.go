package watch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotReportsScheduleDeclarationRemoval(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".renart", "schedules.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o644))
	watcher := New(Config{WorkspaceRoot: root}, nil)

	before, err := watcher.takeSnapshot()
	require.NoError(t, err)
	require.Contains(t, before, ".renart/schedules.yml")
	require.NoError(t, os.Remove(path))
	after, err := watcher.takeSnapshot()
	require.NoError(t, err)

	assert.Equal(t, ".renart/schedules.yml", firstChangedPath(before, after))
	assert.NotEqual(t, hashSnapshot(before), hashSnapshot(after))
}

func TestRelevantPathIncludesScheduleDeclarations(t *testing.T) {
	t.Parallel()
	assert.True(t, IsRelevantPath("/workspace/.renart/schedules.yml"))
}
