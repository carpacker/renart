package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectManagerSuggestedCreateParentDir(t *testing.T) {
	parent := t.TempDir()
	workspace := filepath.Join(parent, "current-project")
	require.NoError(t, os.Mkdir(workspace, 0o755))

	manager := &projectManager{
		defaultID: "default",
		runtimes: map[string]*projectRuntime{
			"default": {root: workspace},
		},
	}

	got, err := manager.SuggestedCreateParentDir()
	require.NoError(t, err)
	require.Equal(t, parent, got)
}

func TestProjectManagerCreateDirectory(t *testing.T) {
	parent := t.TempDir()
	manager := &projectManager{}

	created, err := manager.CreateDirectory(parent, "Data projects")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(parent, "Data projects"), created)
	info, err := os.Stat(created)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	for _, name := range []string{"", ".hidden", "../outside", "nested/child", `nested\\child`} {
		t.Run(name, func(t *testing.T) {
			_, err := manager.CreateDirectory(parent, name)
			require.Error(t, err)
		})
	}
}
