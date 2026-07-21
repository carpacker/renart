package service

import (
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// pyprojectFile is the uv project manifest holding Python dependencies. Both
// notebooks and pipeline assets standardize on it (over requirements.txt): uv
// installs `[project].dependencies` on the next run.
const pyprojectFile = "pyproject.toml"

// readPyprojectDependencies returns `[project].dependencies` from the
// pyproject.toml at path, or nil when it is missing or unreadable.
func readPyprojectDependencies(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Project struct {
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return doc.Project.Dependencies
}

// writePyprojectDependencies sets `[project].dependencies` in the pyproject.toml
// at path, preserving any other tables and seeding a minimal project table when
// the file is new.
func writePyprojectDependencies(path, projectName string, deps []string) error {
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(raw, &doc)
	}
	project, _ := doc["project"].(map[string]any)
	if project == nil {
		project = map[string]any{}
	}
	if _, ok := project["name"]; !ok {
		project["name"] = projectName
	}
	if _, ok := project["version"]; !ok {
		project["version"] = "0.0.0"
	}
	if _, ok := project["requires-python"]; !ok {
		// 3.10+ matches the Python runtime supported by Renart's asset runner and
		// the injected SDK.
		project["requires-python"] = ">=3.10"
	}
	if deps == nil {
		deps = []string{}
	}
	project["dependencies"] = deps
	doc["project"] = project

	out, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// requirementSpecifiers returns the full, comment-free package specifiers from a
// requirements.txt body (e.g. "pandas>=2.0"), preserving version pins so a
// migration into pyproject.toml does not lose constraints. Editable/option
// lines (starting with "-") are skipped.
func requirementSpecifiers(content string) []string {
	specs := []string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.Split(line, "#")[0])
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		specs = append(specs, line)
	}
	return specs
}

// addDependencySpec appends a package specifier unless one for the same package
// (compared by PEP 503 normalized name) is already present.
func addDependencySpec(deps []string, spec string) []string {
	want := canonicalRequirementName(spec)
	for _, existing := range deps {
		if canonicalRequirementName(existing) == want {
			return deps
		}
	}
	return append(deps, spec)
}
