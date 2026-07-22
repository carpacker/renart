package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	webmodel "renart/internal/web/model"
)

// ErrInvalidPythonDependency distinguishes request validation from filesystem
// and TOML failures at the HTTP boundary.
var ErrInvalidPythonDependency = errors.New("invalid Python dependency")

// PythonDependencies returns the pipeline-root Python dependency contract.
// Legacy requirements.txt entries are included so the first settings save can
// migrate them without silently dropping constraints.
func (s *PipelineService) PythonDependencies(
	ctx context.Context,
	pipelineID string,
) (*webmodel.PipelinePythonDependenciesResponse, error) {
	pyprojectPath, requirementsPath, relPath, err := s.pipelinePythonDependencyPaths(ctx, pipelineID)
	if err != nil {
		return nil, err
	}

	dependencies, err := readPyprojectDependenciesStrict(pyprojectPath)
	if err != nil {
		return nil, err
	}
	if raw, readErr := os.ReadFile(requirementsPath); readErr == nil {
		for _, spec := range requirementSpecifiers(string(raw)) {
			dependencies = addDependencySpec(dependencies, spec)
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}

	if dependencies == nil {
		dependencies = []string{}
	}
	return &webmodel.PipelinePythonDependenciesResponse{
		Status:       "ok",
		PipelineID:   pipelineID,
		Path:         relPath,
		Dependencies: dependencies,
	}, nil
}

// UpdatePythonDependencies replaces the pipeline dependency list in
// pyproject.toml. A legacy pipeline-root requirements.txt is removed only after
// the TOML write succeeds.
func (s *PipelineService) UpdatePythonDependencies(
	ctx context.Context,
	pipelineID string,
	req webmodel.UpdatePipelinePythonDependenciesRequest,
) (string, *webmodel.PipelinePythonDependenciesResponse, error) {
	pyprojectPath, requirementsPath, relPath, err := s.pipelinePythonDependencyPaths(ctx, pipelineID)
	if err != nil {
		return "", nil, err
	}
	dependencies, err := normalizePipelinePythonDependencies(req.Dependencies)
	if err != nil {
		return "", nil, err
	}
	if err := writePyprojectDependencies(pyprojectPath, "renart-pipeline", dependencies); err != nil {
		return "", nil, err
	}
	if err := os.Remove(requirementsPath); err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}

	return relPath, &webmodel.PipelinePythonDependenciesResponse{
		Status:       "ok",
		PipelineID:   pipelineID,
		Path:         relPath,
		Dependencies: dependencies,
	}, nil
}

func (s *PipelineService) pipelinePythonDependencyPaths(
	ctx context.Context,
	pipelineID string,
) (pyprojectPath, requirementsPath, relPyprojectPath string, err error) {
	_, absPipelinePath, _, err := s.resolver().LoadPipelineByID(ctx, pipelineID)
	if err != nil {
		return "", "", "", err
	}
	pipelineRoot := absPipelinePath
	if info, statErr := os.Stat(absPipelinePath); statErr == nil && !info.IsDir() {
		pipelineRoot = filepath.Dir(absPipelinePath)
	}
	pyprojectPath = filepath.Join(pipelineRoot, pyprojectFile)
	requirementsPath = filepath.Join(pipelineRoot, "requirements.txt")
	relPyprojectPath, err = filepath.Rel(s.workspaceRoot, pyprojectPath)
	if err != nil {
		return "", "", "", err
	}
	return pyprojectPath, requirementsPath, filepath.ToSlash(relPyprojectPath), nil
}

func normalizePipelinePythonDependencies(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%w at position %d: package specifier is empty", ErrInvalidPythonDependency, index+1)
		}
		if strings.ContainsAny(value, "\r\n\x00") || strings.HasPrefix(value, "-") {
			return nil, fmt.Errorf("%w %q: expected one PEP 508 package specifier", ErrInvalidPythonDependency, value)
		}
		result = addDependencySpec(result, value)
	}
	return result, nil
}
