package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	webmodel "renart/internal/web/model"
)

func writePipelineDependencyFixture(t *testing.T) (string, string, string) {
	t.Helper()
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	require.NoError(t, os.MkdirAll(pipelineRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	return workspaceRoot, pipelineRoot, EncodeID("analytics")
}

func TestPipelineServicePythonDependenciesReturnsCanonicalMissingPath(t *testing.T) {
	workspaceRoot, _, pipelineID := writePipelineDependencyFixture(t)
	service := NewPipelineService(workspaceRoot)

	response, err := service.PythonDependencies(context.Background(), pipelineID)
	require.NoError(t, err)
	assert.Equal(t, "analytics/pyproject.toml", response.Path)
	assert.Empty(t, response.Dependencies)
}

func TestPipelineServiceUpdatePythonDependenciesPreservesPyprojectAndDeduplicates(t *testing.T) {
	workspaceRoot, pipelineRoot, pipelineID := writePipelineDependencyFixture(t)
	pyprojectPath := filepath.Join(pipelineRoot, "pyproject.toml")
	require.NoError(t, os.WriteFile(pyprojectPath, []byte(`[project]
name = "analytics"

[tool.ruff]
line-length = 100
`), 0o644))
	service := NewPipelineService(workspaceRoot)

	relPath, response, err := service.UpdatePythonDependencies(context.Background(), pipelineID, webmodel.UpdatePipelinePythonDependenciesRequest{
		Dependencies: []string{"pandas>=2", "Pandas", "polars"},
	})
	require.NoError(t, err)
	assert.Equal(t, "analytics/pyproject.toml", relPath)
	assert.Equal(t, []string{"pandas>=2", "polars"}, response.Dependencies)
	raw, readErr := os.ReadFile(pyprojectPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(raw), "[tool.ruff]")
	assert.Contains(t, string(raw), "line-length = 100")
}

func TestPipelineServiceUpdatePythonDependenciesMigratesRequirements(t *testing.T) {
	workspaceRoot, pipelineRoot, pipelineID := writePipelineDependencyFixture(t)
	requirementsPath := filepath.Join(pipelineRoot, "requirements.txt")
	require.NoError(t, os.WriteFile(requirementsPath, []byte("pandas>=2\nrequests\n"), 0o644))
	service := NewPipelineService(workspaceRoot)

	loaded, err := service.PythonDependencies(context.Background(), pipelineID)
	require.NoError(t, err)
	assert.Equal(t, []string{"pandas>=2", "requests"}, loaded.Dependencies)

	_, response, err := service.UpdatePythonDependencies(context.Background(), pipelineID, webmodel.UpdatePipelinePythonDependenciesRequest{
		Dependencies: loaded.Dependencies,
	})
	require.NoError(t, err)
	assert.Equal(t, loaded.Dependencies, response.Dependencies)
	_, statErr := os.Stat(requirementsPath)
	assert.True(t, os.IsNotExist(statErr))
	assert.Equal(t, loaded.Dependencies, readPyprojectDependencies(filepath.Join(pipelineRoot, "pyproject.toml")))
}

func TestPipelineServiceUpdatePythonDependenciesRejectsInvalidSpecifier(t *testing.T) {
	workspaceRoot, pipelineRoot, pipelineID := writePipelineDependencyFixture(t)
	service := NewPipelineService(workspaceRoot)

	_, _, err := service.UpdatePythonDependencies(context.Background(), pipelineID, webmodel.UpdatePipelinePythonDependenciesRequest{
		Dependencies: []string{"pandas\nrequests"},
	})
	require.ErrorIs(t, err, ErrInvalidPythonDependency)
	_, statErr := os.Stat(filepath.Join(pipelineRoot, "pyproject.toml"))
	assert.True(t, os.IsNotExist(statErr))
}
