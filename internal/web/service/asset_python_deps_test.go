package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePythonAsset(t *testing.T, workspaceRoot string) (relPath, assetID string) {
	t.Helper()
	relPath = filepath.Join("ml", "predict.py")
	abs := filepath.Join(workspaceRoot, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("import pandas as pd\nimport requests\n"), 0o644))
	return relPath, EncodeID(relPath)
}

func TestAssetServicePythonDepsReadsRequirementsAndLocalVenv(t *testing.T) {
	workspaceRoot := t.TempDir()
	relPath, assetID := writePythonAsset(t, workspaceRoot)

	// requirements.txt beside the asset declares pandas (with a version pin).
	require.NoError(t, os.WriteFile(
		filepath.Join(workspaceRoot, "ml", "requirements.txt"),
		[]byte("pandas>=2.0\n# a comment\n"),
		0o644,
	))
	// A local .venv at the workspace root provides an installed module.
	site := filepath.Join(workspaceRoot, ".venv", "lib", "python3.11", "site-packages")
	require.NoError(t, os.MkdirAll(filepath.Join(site, "numpy"), 0o755))

	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	resp, apiErr := service.PythonDeps(assetID)
	require.Nil(t, apiErr)
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, []string{"pandas"}, resp.Dependencies)
	assert.Contains(t, resp.InstalledModules, "numpy")
	assert.Contains(t, resp.InstalledModules, "renart")
	_ = relPath
}

func TestAssetServicePythonDepsRejectsNonPython(t *testing.T) {
	workspaceRoot := t.TempDir()
	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	_, apiErr := service.PythonDeps(EncodeID(filepath.Join("ml", "model.sql")))
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Status)
}

func TestAssetServiceAddPythonDependencyMigratesRequirementsToPyproject(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, assetID := writePythonAsset(t, workspaceRoot)
	requirementsPath := filepath.Join(workspaceRoot, "ml", "requirements.txt")
	pyprojectPath := filepath.Join(workspaceRoot, "ml", "pyproject.toml")
	require.NoError(t, os.WriteFile(requirementsPath, []byte("pandas>=2.0\n"), 0o644))

	var pushedEvent, pushedPath string
	service := NewAssetService(AssetDependencies{
		WorkspaceRoot: workspaceRoot,
		PushWorkspaceUpdateImmediate: func(_ context.Context, event, path string) {
			pushedEvent, pushedPath = event, path
		},
	})

	resp, apiErr := service.AddPythonDependency(context.Background(), assetID, AddPythonDependencyRequest{Package: "requests"})
	require.Nil(t, apiErr)
	// Dependencies from pyproject.toml keep their full specifier (version pins).
	assert.Contains(t, resp.Dependencies, "requests")
	assert.Contains(t, resp.Dependencies, "pandas>=2.0")

	// The legacy requirements.txt is folded into pyproject.toml and removed so
	// uv/Bruin run in project mode.
	_, statErr := os.Stat(requirementsPath)
	assert.True(t, os.IsNotExist(statErr), "requirements.txt should be removed after migration")
	deps := readPyprojectDependencies(pyprojectPath)
	assert.Contains(t, deps, "pandas>=2.0", "version pin preserved during migration")
	assert.Contains(t, deps, "requests")

	assert.Equal(t, "asset.dependencies", pushedEvent)
	assert.Equal(t, filepath.ToSlash(filepath.Join("ml", "pyproject.toml")), pushedPath)
}

func TestAssetServiceAddPythonDependencyCreatesPyprojectWhenMissing(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, assetID := writePythonAsset(t, workspaceRoot)

	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	_, apiErr := service.AddPythonDependency(context.Background(), assetID, AddPythonDependencyRequest{Package: "scikit-learn"})
	require.Nil(t, apiErr)

	_, statErr := os.Stat(filepath.Join(workspaceRoot, "ml", "requirements.txt"))
	assert.True(t, os.IsNotExist(statErr), "must not create a requirements.txt")
	deps := readPyprojectDependencies(filepath.Join(workspaceRoot, "ml", "pyproject.toml"))
	assert.Equal(t, []string{"scikit-learn"}, deps)
}

func TestAssetServiceAddPythonDependencyDefaultsToPipelinePyproject(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	assetPath := filepath.Join("analytics", "assets", "models", "predict.py")
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(workspaceRoot, assetPath)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, assetPath), []byte("def materialize():\n    return None\n"), 0o644))

	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	_, apiErr := service.AddPythonDependency(context.Background(), EncodeID(assetPath), AddPythonDependencyRequest{Package: "polars>=1"})
	require.Nil(t, apiErr)

	assert.Equal(t, []string{"polars>=1"}, readPyprojectDependencies(filepath.Join(pipelineRoot, "pyproject.toml")))
	_, statErr := os.Stat(filepath.Join(filepath.Dir(filepath.Join(workspaceRoot, assetPath)), "pyproject.toml"))
	assert.True(t, os.IsNotExist(statErr), "must not create an asset-local manifest inside a pipeline")
}

func TestAssetServiceAddPythonDependencyAppendsToExistingPyproject(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, assetID := writePythonAsset(t, workspaceRoot)
	pyprojectPath := filepath.Join(workspaceRoot, "ml", "pyproject.toml")
	require.NoError(t, writePyprojectDependencies(pyprojectPath, "renart-pipeline", []string{"scikit_learn==1.4"}))

	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	_, apiErr := service.AddPythonDependency(context.Background(), assetID, AddPythonDependencyRequest{Package: "scikit-learn"})
	require.Nil(t, apiErr)

	deps := readPyprojectDependencies(pyprojectPath)
	assert.Equal(t, []string{"scikit_learn==1.4"}, deps, "must not duplicate an already-declared package (PEP 503 normalized)")
}

func TestEnsurePythonProjectFileCreatesPyprojectNotRequirements(t *testing.T) {
	workspaceRoot := t.TempDir()
	relPath := filepath.Join("ml", "new_model.py")
	abs := filepath.Join(workspaceRoot, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("import pandas as pd\n"), 0o644))

	require.NoError(t, EnsurePythonProjectFile(abs, "python", relPath))

	_, statErr := os.Stat(filepath.Join(workspaceRoot, "ml", "requirements.txt"))
	assert.True(t, os.IsNotExist(statErr), "must not create requirements.txt")
	assert.Equal(t, []string{"pandas"}, readPyprojectDependencies(filepath.Join(workspaceRoot, "ml", "pyproject.toml")))
}

func TestEnsurePythonProjectFileSkipsWhenAncestorDeclaresDeps(t *testing.T) {
	workspaceRoot := t.TempDir()
	relPath := filepath.Join("ml", "sub", "new_model.py")
	abs := filepath.Join(workspaceRoot, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("import pandas as pd\n"), 0o644))
	// A pipeline-level pyproject.toml already declares deps up the tree.
	require.NoError(t, writePyprojectDependencies(filepath.Join(workspaceRoot, "ml", "pyproject.toml"), "renart-pipeline", []string{"pandas"}))

	require.NoError(t, EnsurePythonProjectFile(abs, "python", relPath))

	_, statErr := os.Stat(filepath.Join(workspaceRoot, "ml", "sub", "pyproject.toml"))
	assert.True(t, os.IsNotExist(statErr), "must not create a redundant pyproject.toml when an ancestor declares deps")
}

func TestEnsurePythonProjectFileDefaultsToPipelineRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	pipelineRoot := filepath.Join(workspaceRoot, "analytics")
	relPath := filepath.Join("analytics", "assets", "models", "new_model.py")
	abs := filepath.Join(workspaceRoot, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pipelineRoot, "pipeline.yml"), []byte("name: analytics\n"), 0o644))
	require.NoError(t, os.WriteFile(abs, []byte("import pandas as pd\n"), 0o644))

	require.NoError(t, EnsurePythonProjectFile(abs, "python", relPath))

	assert.Equal(t, []string{"pandas"}, readPyprojectDependencies(filepath.Join(pipelineRoot, "pyproject.toml")))
	_, statErr := os.Stat(filepath.Join(filepath.Dir(abs), "pyproject.toml"))
	assert.True(t, os.IsNotExist(statErr), "must not create an asset-local manifest inside a pipeline")
}

func TestAssetServiceAddPythonDependencyRejectsEmpty(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, assetID := writePythonAsset(t, workspaceRoot)
	service := NewAssetService(AssetDependencies{WorkspaceRoot: workspaceRoot})
	_, apiErr := service.AddPythonDependency(context.Background(), assetID, AddPythonDependencyRequest{Package: "  "})
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Status)
}
