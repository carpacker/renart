package service

import (
	"net/http"
	"path/filepath"
	"strings"

	"renart/internal/web/model"
)

// readNotebookDependencies returns the `[project].dependencies` list from a
// notebook's pyproject.toml, or nil when there is none.
func readNotebookDependencies(dir string) []string {
	return readPyprojectDependencies(filepath.Join(dir, pyprojectFile))
}

// writeNotebookDependencies sets `[project].dependencies` in the notebook's
// pyproject.toml, preserving any other tables, and creating a minimal project
// table when the file is new.
func writeNotebookDependencies(dir string, deps []string) error {
	return writePyprojectDependencies(filepath.Join(dir, pyprojectFile), "renart-notebook", deps)
}

// dependenciesFromText splits a newline-separated dependency list (the dialog's
// edit format) into trimmed, comment-free package specifiers.
func dependenciesFromText(content string) []string {
	deps := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			deps = append(deps, trimmed)
		}
	}
	return deps
}

// UpdateDependencies replaces the notebook's Python dependencies (pyproject.toml
// `[project].dependencies`) and returns the refreshed notebook.
func (s *NotebookService) UpdateDependencies(notebookID, content string) (model.Notebook, *APIError) {
	nb, apiErr := s.load(notebookID)
	if apiErr != nil {
		return model.Notebook{}, apiErr
	}
	if err := writeNotebookDependencies(nb.Dir, dependenciesFromText(content)); err != nil {
		return model.Notebook{}, &APIError{Status: http.StatusInternalServerError, Code: "dependencies_update_failed", Message: err.Error()}
	}
	s.pushUpdate(filepath.Join(nb.Dir, pyprojectFile))
	return s.Get(notebookID)
}
