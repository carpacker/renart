package service

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const renartSDKModule = "renart"

var renartRuntimeModules = []string{"pandas", "pyarrow", renartSDKModule}

// notebookVenvDir is the uv project virtualenv for a notebook (kept under
// .renart so it stays out of the git-tracked notebook folder).
func notebookVenvDir(workspaceRoot, notebookDir string) string {
	return filepath.Join(workspaceRoot, ".renart", "notebooks", "venvs", filepath.Base(notebookDir))
}

// notebookInstalledModules returns top-level import names available in a
// notebook's virtualenv or injected by the runner. The embedded SDK guarantees
// renart, pandas, and pyarrow even before a notebook virtualenv exists.
func notebookInstalledModules(venvDir string) []string {
	return withRenartRuntimeModules(installedModulesFromSitePackages(pythonSitePackagesDirs(venvDir)))
}

// withRenartRuntimeModules adds the modules guaranteed by the injected SDK wheel.
// They are available even though they need not live in the persistent venv.
func withRenartRuntimeModules(modules []string) []string {
	available := make(map[string]bool, len(modules)+len(renartRuntimeModules))
	for _, module := range modules {
		available[module] = true
	}
	for _, module := range renartRuntimeModules {
		if !available[module] {
			modules = append(modules, module)
			available[module] = true
		}
	}
	sort.Strings(modules)
	return modules
}

// installedModulesFromSitePackages scans the given site-packages directories and
// returns the sorted set of top-level import names they provide. Shared by
// notebooks (one managed venv) and pipeline assets (the project's local venvs).
func installedModulesFromSitePackages(sitePackagesDirs []string) []string {
	modules := map[string]struct{}{}
	for _, site := range sitePackagesDirs {
		entries, err := os.ReadDir(site)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "" || name == "__pycache__" || strings.HasPrefix(name, ".") {
				continue
			}
			if entry.IsDir() {
				switch {
				case strings.HasSuffix(name, ".dist-info"):
					for _, top := range readTopLevelModules(filepath.Join(site, name)) {
						modules[top] = struct{}{}
					}
				case strings.HasSuffix(name, ".egg-info"), strings.HasSuffix(name, ".egg-link"):
					// metadata, not an import
				default:
					modules[name] = struct{}{}
				}
				continue
			}
			// Top-level single-file modules and C extensions (e.g. cv2.so).
			if module := topLevelModuleFromFile(name); module != "" {
				modules[module] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(modules))
	for module := range modules {
		out = append(out, module)
	}
	sort.Strings(out)
	return out
}

func pythonSitePackagesDirs(venvDir string) []string {
	if venvDir == "" {
		return nil
	}
	dirs := make([]string, 0, 2)
	for _, pattern := range []string{
		filepath.Join(venvDir, "lib", "python*", "site-packages"),
		filepath.Join(venvDir, "Lib", "site-packages"),
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			dirs = append(dirs, matches...)
		}
	}
	return dirs
}

func topLevelModuleFromFile(name string) string {
	ext := filepath.Ext(name)
	if ext != ".py" && ext != ".so" && ext != ".pyd" {
		return ""
	}
	// Strip the extension and any platform tag (cv2.cpython-311-…​.so → cv2).
	stem := strings.SplitN(strings.TrimSuffix(name, ext), ".", 2)[0]
	if stem == "" || stem == "__init__" {
		return ""
	}
	return stem
}

func readTopLevelModules(distInfoDir string) []string {
	file, err := os.Open(filepath.Join(distInfoDir, "top_level.txt"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var modules []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Only top-level names (skip nested paths like "foo/bar").
		if line != "" && !strings.ContainsAny(line, "/\\") {
			modules = append(modules, line)
		}
	}
	return modules
}
