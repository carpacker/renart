package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotebookInstalledModulesScansSitePackages(t *testing.T) {
	venv := t.TempDir()
	site := filepath.Join(venv, "lib", "python3.11", "site-packages")
	require.NoError(t, os.MkdirAll(site, 0o755))

	// A regular package directory whose import name differs from its PyPI name.
	require.NoError(t, os.MkdirAll(filepath.Join(site, "skimage"), 0o755))
	// A single-file module.
	require.NoError(t, os.WriteFile(filepath.Join(site, "six.py"), []byte("# six"), 0o644))
	// A compiled extension (e.g. opencv-python ships cv2 as a .so).
	require.NoError(t, os.WriteFile(filepath.Join(site, "cv2.cpython-311-x86_64-linux-gnu.so"), []byte{}, 0o644))
	// dist-info with top_level.txt providing an extra import name.
	distInfo := filepath.Join(site, "PyYAML-6.0.dist-info")
	require.NoError(t, os.MkdirAll(distInfo, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(distInfo, "top_level.txt"), []byte("yaml\n_yaml\n"), 0o644))
	// Noise that must be ignored.
	require.NoError(t, os.MkdirAll(filepath.Join(site, "__pycache__"), 0o755))

	modules := notebookInstalledModules(venv)
	assert.Contains(t, modules, "skimage")
	assert.Contains(t, modules, "six")
	assert.Contains(t, modules, "cv2")
	assert.Contains(t, modules, "yaml")
	assert.Contains(t, modules, "renart")
	assert.NotContains(t, modules, "__pycache__")
	assert.NotContains(t, modules, "PyYAML-6.0.dist-info")
}

func TestNotebookInstalledModulesIncludesInjectedSDKWithoutVenv(t *testing.T) {
	assert.Equal(t, []string{"pandas", "pyarrow", "renart"}, notebookInstalledModules(filepath.Join(t.TempDir(), "does-not-exist")))
}
