package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPyPIIndexFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "pypi-simple-index.txt")
	names := []string{"requests", "numpy", "scikit-learn", "opencv-python"}

	require.NoError(t, writePyPIIndexFile(path, names))
	loaded, err := readPyPIIndexFile(path)
	require.NoError(t, err)
	assert.Equal(t, names, loaded)
}

func TestPyPIIndexStaleness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pypi-simple-index.txt")
	assert.True(t, pypiIndexFileStale(path), "missing file is stale")

	require.NoError(t, writePyPIIndexFile(path, []string{"requests"}))
	assert.False(t, pypiIndexFileStale(path), "freshly written file is not stale")

	old := time.Now().Add(-pypiIndexMaxAge - time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	assert.True(t, pypiIndexFileStale(path), "an old cache is stale")
}
