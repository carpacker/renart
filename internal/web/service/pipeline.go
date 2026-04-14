package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

type PipelineService struct {
	workspaceRoot string
}

func NewPipelineService(workspaceRoot string) *PipelineService {
	return &PipelineService{workspaceRoot: workspaceRoot}
}

func (s *PipelineService) Create(ctx context.Context, relPath, name, content string) (string, error) {
	absPath, err := SafeJoin(s.workspaceRoot, relPath)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return "", err
	}

	if strings.TrimSpace(content) == "" {
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(absPath)
		}
		content = fmt.Sprintf("name: %s\n", name)
	}

	if err := os.WriteFile(filepath.Join(absPath, "pipeline.yml"), []byte(content), 0o644); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) Update(ctx context.Context, pipelineID, name, content string) (string, error) {
	relPath, err := DecodeID(pipelineID)
	if err != nil {
		return "", err
	}

	absPath, err := SafeJoin(s.workspaceRoot, relPath)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(name) != "" && strings.TrimSpace(content) == "" {
		builder := s.newPipelineBuilder()
		parsed, err := builder.CreatePipelineFromPath(ctx, absPath, pipeline.WithMutate(), pipeline.WithOnlyPipeline())
		if err != nil {
			return "", err
		}

		parsed.Name = strings.TrimSpace(name)
		parsed.DefinitionFile.Path = filepath.Join(absPath, "pipeline.yml")

		if err := parsed.Persist(afero.NewOsFs()); err != nil {
			return "", err
		}

		return filepath.ToSlash(relPath), nil
	}

	if err := os.WriteFile(filepath.Join(absPath, "pipeline.yml"), []byte(content), 0o644); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) Delete(pipelineID string) (string, error) {
	relPath, err := DecodeID(pipelineID)
	if err != nil {
		return "", err
	}

	absPath, err := SafeJoin(s.workspaceRoot, relPath)
	if err != nil {
		return "", err
	}

	if err := os.RemoveAll(absPath); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) newPipelineBuilder() *pipeline.Builder {
	osFS := afero.NewOsFs()
	return pipeline.NewBuilder(
		BuilderConfig,
		pipeline.CreateTaskFromYamlDefinition(osFS),
		pipeline.CreateTaskFromFileComments(osFS),
		osFS,
		DefaultGlossaryReader,
	)
}
