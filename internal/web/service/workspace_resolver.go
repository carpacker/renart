package service

import (
	"context"
	"fmt"
	"path/filepath"

	bruinpath "github.com/bruin-data/bruin/pkg/path"
	"github.com/bruin-data/bruin/pkg/pipeline"
)

type PipelineLoader func(context.Context, string) (*pipeline.Pipeline, error)

type WorkspaceResolver struct {
	workspaceRoot string
	loadPipeline  PipelineLoader
}

func NewWorkspaceResolver(workspaceRoot string, loadPipeline PipelineLoader) *WorkspaceResolver {
	return &WorkspaceResolver{workspaceRoot: workspaceRoot, loadPipeline: loadPipeline}
}

func (r *WorkspaceResolver) JoinPath(relPath string) (string, error) {
	return SafeJoin(r.workspaceRoot, relPath)
}

func (r *WorkspaceResolver) DecodePipelineID(pipelineID string) (string, string, error) {
	relPipelinePath, err := DecodeID(pipelineID)
	if err != nil {
		return "", "", err
	}
	absPipelinePath, err := r.JoinPath(relPipelinePath)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(relPipelinePath), absPipelinePath, nil
}

func (r *WorkspaceResolver) DecodeAssetID(assetID string) (string, string, error) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return "", "", err
	}
	absAssetPath, err := r.JoinPath(relAssetPath)
	if err != nil {
		return "", "", err
	}
	return filepath.ToSlash(relAssetPath), absAssetPath, nil
}

func (r *WorkspaceResolver) LoadPipelineByID(ctx context.Context, pipelineID string) (string, string, *pipeline.Pipeline, error) {
	if r.loadPipeline == nil {
		return "", "", nil, fmt.Errorf("workspace resolver is missing a pipeline loader")
	}
	relPipelinePath, absPipelinePath, err := r.DecodePipelineID(pipelineID)
	if err != nil {
		return "", "", nil, err
	}
	parsed, err := r.loadPipeline(ctx, absPipelinePath)
	if err != nil {
		return "", "", nil, err
	}
	return relPipelinePath, absPipelinePath, parsed, nil
}

func (r *WorkspaceResolver) ResolveAssetByID(ctx context.Context, assetID string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
	if r.loadPipeline == nil {
		return "", nil, nil, fmt.Errorf("workspace resolver is missing a pipeline loader")
	}
	relAssetPath, absAssetPath, err := r.DecodeAssetID(assetID)
	if err != nil {
		return "", nil, nil, err
	}
	return r.ResolveAssetByPath(ctx, relAssetPath, absAssetPath)
}

func (r *WorkspaceResolver) ResolveAssetByPath(ctx context.Context, relAssetPath, absAssetPath string) (string, *pipeline.Pipeline, *pipeline.Asset, error) {
	if r.loadPipeline == nil {
		return "", nil, nil, fmt.Errorf("workspace resolver is missing a pipeline loader")
	}
	if relAssetPath == "" {
		var err error
		relAssetPath, err = filepath.Rel(r.workspaceRoot, absAssetPath)
		if err != nil {
			relAssetPath = absAssetPath
		}
	}

	pipelinePath, err := findPipelineRootForAsset(absAssetPath)
	if err != nil {
		return "", nil, nil, err
	}
	parsed, err := r.loadPipeline(ctx, pipelinePath)
	if err != nil {
		return "", nil, nil, err
	}

	normalizedTarget := filepath.ToSlash(relAssetPath)
	for _, current := range parsed.Assets {
		currentRelPath, relErr := assetPathRelativeToRoot(r.workspaceRoot, current)
		if relErr != nil {
			continue
		}
		if currentRelPath == normalizedTarget {
			return normalizedTarget, parsed, current, nil
		}
	}

	return "", nil, nil, ErrAssetNotFound
}

func assetPathRelativeToRoot(workspaceRoot string, asset *pipeline.Asset) (string, error) {
	assetPath := asset.ExecutableFile.Path
	if assetPath == "" {
		assetPath = asset.DefinitionFile.Path
	}
	relAssetPath, err := filepath.Rel(workspaceRoot, assetPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relAssetPath), nil
}

func findPipelineRootForAsset(absAssetPath string) (string, error) {
	return bruinpath.GetPipelineRootFromTask(absAssetPath, PipelineDefinitionFiles)
}
