package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupTemporaryDirectoriesIsAllowlistedAndProcessSafe(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	processStartedAt := now.AddDate(0, 0, -7)
	cutoff := now.Add(-24 * time.Hour)

	eligible := makeTempCleanupEntry(t, root, "renart-plan-abandoned", now.AddDate(0, 0, -30), true)
	currentProcess := makeTempCleanupEntry(t, root, "renart-plan-current", now.AddDate(0, 0, -2), true)
	fresh := makeTempCleanupEntry(t, root, "renart-snapshot-fresh", now.Add(-time.Hour), true)
	unknown := makeTempCleanupEntry(t, root, "unrelated-old", now.AddDate(0, 0, -30), true)
	file := makeTempCleanupEntry(t, root, "renart-plan-file", now.AddDate(0, 0, -30), false)
	victim := makeTempCleanupEntry(t, root, "victim", now.AddDate(0, 0, -30), true)
	symlink := filepath.Join(root, "renart-plan-symlink")
	require.NoError(t, os.Symlink(victim, symlink))

	removed, err := CleanupTemporaryDirectories(root, cutoff, processStartedAt)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.NoDirExists(t, eligible)
	assert.DirExists(t, currentProcess)
	assert.DirExists(t, fresh)
	assert.DirExists(t, unknown)
	assert.FileExists(t, file)
	assert.DirExists(t, victim)
	_, err = os.Lstat(symlink)
	require.NoError(t, err)
}

func makeTempCleanupEntry(
	t *testing.T,
	root string,
	name string,
	modified time.Time,
	directory bool,
) string {
	t.Helper()
	path := filepath.Join(root, name)
	if directory {
		require.NoError(t, os.Mkdir(path, 0o700))
	} else {
		require.NoError(t, os.WriteFile(path, []byte("keep"), 0o600))
	}
	require.NoError(t, os.Chtimes(path, modified, modified))
	return path
}
