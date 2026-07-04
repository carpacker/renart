package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertCreatesAndUpdates(t *testing.T) {
	t.Parallel()
	reg := New(filepath.Join(t.TempDir(), "renart", "projects.json"))

	_, err := reg.Upsert(Entry{ID: "a", Name: "alpha", Path: "/w/alpha"})
	require.NoError(t, err)
	_, err = reg.Upsert(Entry{ID: "b", Name: "beta", Path: "/w/beta"})
	require.NoError(t, err)

	// Re-opening project a moves it back to the front with the new path.
	file, err := reg.Upsert(Entry{ID: "a", Name: "alpha2", Path: "/moved/alpha", LastOpenedAt: time.Now().UTC().Add(time.Minute)})
	require.NoError(t, err)
	require.Len(t, file.Projects, 2)
	assert.Equal(t, "a", file.Projects[0].ID)
	assert.Equal(t, "alpha2", file.Projects[0].Name)
	assert.Equal(t, "/moved/alpha", file.Projects[0].Path)
	assert.Equal(t, "local", file.Projects[0].Type)

	loaded, err := reg.Load()
	require.NoError(t, err)
	assert.Equal(t, file, loaded)
}

func TestUpsertRequiresID(t *testing.T) {
	t.Parallel()
	reg := New(filepath.Join(t.TempDir(), "projects.json"))
	_, err := reg.Upsert(Entry{Name: "nameless"})
	require.Error(t, err)
}

func TestRemove(t *testing.T) {
	t.Parallel()
	reg := New(filepath.Join(t.TempDir(), "projects.json"))
	_, err := reg.Upsert(Entry{ID: "a", Name: "alpha", Path: "/w/alpha"})
	require.NoError(t, err)

	file, err := reg.Remove("a")
	require.NoError(t, err)
	assert.Empty(t, file.Projects)

	// Removing again is a no-op.
	file, err = reg.Remove("a")
	require.NoError(t, err)
	assert.Empty(t, file.Projects)
}

func TestLoadCorruptFileStartsOver(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "projects.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o644))

	reg := New(path)
	file, err := reg.Load()
	require.NoError(t, err)
	assert.Empty(t, file.Projects)

	_, err = os.Stat(path + ".corrupt")
	require.NoError(t, err)
}
