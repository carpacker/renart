package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// PythonDepsResponse reports the Python dependency state of a single Python
// asset: the packages it declares (requirements.txt / pyproject.toml) and the
// import names actually installed in the project's local virtualenv. The editor
// uses these to flag imports that are not yet declared, mirroring notebooks.
type PythonDepsResponse struct {
	Status           string   `json:"status"`
	AssetID          string   `json:"asset_id"`
	Dependencies     []string `json:"dependencies"`
	InstalledModules []string `json:"installed_modules"`
}

// AddPythonDependencyRequest adds one package specifier to a Python asset's
// requirements.txt.
type AddPythonDependencyRequest struct {
	Package string `json:"package"`
}

// PythonDeps returns the declared dependencies and installed import names for a
// Python asset.
func (s *AssetService) PythonDeps(assetID string) (PythonDepsResponse, *APIError) {
	relAssetPath, absAssetPath, apiErr := s.resolvePythonAssetPath(assetID)
	if apiErr != nil {
		return PythonDepsResponse{}, apiErr
	}
	return PythonDepsResponse{
		Status:           "ok",
		AssetID:          assetID,
		Dependencies:     s.pythonDependencyNames(absAssetPath),
		InstalledModules: s.assetInstalledModules(relAssetPath),
	}, nil
}

// AddPythonDependency appends a package specifier to the Python asset's nearest
// requirements.txt (creating one beside the asset when none exists), then
// returns the refreshed dependency state. uv installs the new package on the
// asset's next run, matching the notebook flow.
func (s *AssetService) AddPythonDependency(ctx context.Context, assetID string, req AddPythonDependencyRequest) (PythonDepsResponse, *APIError) {
	_, absAssetPath, apiErr := s.resolvePythonAssetPath(assetID)
	if apiErr != nil {
		return PythonDepsResponse{}, apiErr
	}
	pkg := strings.TrimSpace(req.Package)
	if pkg == "" {
		return PythonDepsResponse{}, newAPIError(400, "invalid_package", "package is required")
	}
	pyprojectPath, err := s.addAssetPyprojectDependency(absAssetPath, pkg)
	if err != nil {
		return PythonDepsResponse{}, newAPIError(500, "dependency_add_failed", err.Error())
	}
	if pyprojectPath != "" && s.deps.PushWorkspaceUpdateImmediate != nil {
		if rel, relErr := filepath.Rel(s.deps.WorkspaceRoot, pyprojectPath); relErr == nil {
			s.deps.PushWorkspaceUpdateImmediate(ctx, "asset.dependencies", filepath.ToSlash(rel))
		}
	}
	return s.PythonDeps(assetID)
}

// addAssetPyprojectDependency adds pkg to the asset's pyproject.toml
// `[project].dependencies` (creating one when absent), skipping packages already
// declared. A legacy requirements.txt beside the chosen pyproject.toml is folded
// in and removed so uv runs in project mode (Bruin otherwise prefers
// requirements.txt). Returns the pyproject.toml path that was written.
func (s *AssetService) addAssetPyprojectDependency(absAssetPath, pkg string) (string, error) {
	pyprojectPath := s.assetPyprojectPath(absAssetPath)
	deps := readPyprojectDependencies(pyprojectPath)

	requirementsPath := filepath.Join(filepath.Dir(pyprojectPath), "requirements.txt")
	migrate := false
	if raw, err := os.ReadFile(requirementsPath); err == nil {
		for _, spec := range requirementSpecifiers(string(raw)) {
			deps = addDependencySpec(deps, spec)
		}
		migrate = true
	}

	deps = addDependencySpec(deps, pkg)
	if err := writePyprojectDependencies(pyprojectPath, "renart-pipeline", deps); err != nil {
		return "", err
	}
	if migrate {
		// Best-effort: leave a stray requirements.txt only if removal fails.
		_ = os.Remove(requirementsPath)
	}
	return pyprojectPath, nil
}

// assetPyprojectPath chooses where the asset's dependencies live: an existing
// pyproject.toml up the tree, else beside a legacy requirements.txt (so it can be
// migrated in place), else beside the asset.
func (s *AssetService) assetPyprojectPath(absAssetPath string) string {
	startDir := filepath.Dir(absAssetPath)
	if existing := nearestPythonDependencyFile(startDir, s.deps.WorkspaceRoot, pyprojectFile); existing != "" {
		return existing
	}
	if requirements := nearestPythonDependencyFile(startDir, s.deps.WorkspaceRoot, "requirements.txt"); requirements != "" {
		return filepath.Join(filepath.Dir(requirements), pyprojectFile)
	}
	return filepath.Join(startDir, pyprojectFile)
}

// canonicalRequirementName normalizes a package/requirement name per PEP 503 so
// "scikit_learn" and "scikit-learn" compare equal.
func canonicalRequirementName(spec string) string {
	name := spec
	if match := pythonRequirementPattern.FindStringSubmatch(spec); len(match) == 2 {
		name = match[1]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.NewReplacer("_", "-", ".", "-").Replace(name)
}

// assetInstalledModules returns the top-level import names installed in the
// project's local virtualenv(s) reachable from the asset — the ground truth for
// "is this import resolvable here", regardless of how its package is named.
func (s *AssetService) assetInstalledModules(relAssetPath string) []string {
	return installedModulesFromSitePackages(s.assetLocalSitePackages(relAssetPath))
}

// assetLocalSitePackages collects the site-packages directories of the local
// .venv/venv virtualenvs found from the asset's directory up to the workspace
// root. It deliberately ignores the global uv/bruin caches (those resolve
// almost anything and would mask undeclared imports); only project-local
// environments count as installed for the missing-dependency hint.
func (s *AssetService) assetLocalSitePackages(relAssetPath string) []string {
	root := filepath.Clean(s.deps.WorkspaceRoot)
	if root == "" || root == "." {
		return nil
	}
	dirs := []string{}
	dir := filepath.Join(root, filepath.Dir(relAssetPath))
	for {
		dirs = append(dirs, pythonSitePackagesDirs(filepath.Join(dir, ".venv"))...)
		dirs = append(dirs, pythonSitePackagesDirs(filepath.Join(dir, "venv"))...)
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || len(parent) < len(root) {
			break
		}
		dir = parent
	}
	return uniqueStrings(dirs)
}
