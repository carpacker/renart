package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePyprojectDependenciesPreservesOtherTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pyproject.toml")
	require.NoError(t, os.WriteFile(path, []byte(`[project]
name = "example"
version = "1.0.0"

[tool.ruff]
line-length = 100
`), 0o644))

	require.NoError(t, writePyprojectDependencies(path, "ignored", []string{"pandas>=2"}))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "[tool.ruff]")
	assert.Contains(t, string(raw), "line-length = 100")
	assert.Equal(t, []string{"pandas>=2"}, readPyprojectDependencies(path))
}

func TestWritePyprojectDependenciesDoesNotReplaceMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pyproject.toml")
	original := []byte("[project\nname = broken\n")
	require.NoError(t, os.WriteFile(path, original, 0o644))

	err := writePyprojectDependencies(path, "example", []string{"pandas"})
	require.Error(t, err)
	raw, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, raw)
}
