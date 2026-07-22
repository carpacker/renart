package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

func (e *HybridBruinExecutor) applyFillAssetDependencies(ctx context.Context, targetPath string) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct fill-asset-dependencies requires a pipeline builder")
	}

	sqlParserInstance, err := newDependencyParser(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create sql parser: %w", err)
	}
	defer sqlParserInstance.Close()

	jinjaRenderer := jinja.NewRendererWithYesterday("test-pipeline", "test-run-id")
	builder := e.newPipelineBuilder()
	fs := afero.NewOsFs()

	if directPathReferencesAsset(targetPath) {
		resolvedTargetPath := resolveDirectPath(e.workspaceRoot, targetPath)
		resolver := NewWorkspaceResolver(e.workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
			return builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
		})
		_, foundPipeline, asset, err := resolver.ResolveAssetByPath(ctx, "", resolvedTargetPath)
		if err != nil {
			return nil, err
		}
		if err := updateDirectAssetDependencies(ctx, asset, foundPipeline, sqlParserInstance, jinjaRenderer, fs); err != nil {
			return nil, err
		}
		return json.Marshal(directFillAssetDependenciesResponse{Status: "success", Message: "Asset dependencies updated successfully"})
	}

	resolvedTargetPath := resolveDirectPath(e.workspaceRoot, targetPath)
	foundPipeline, err := builder.CreatePipelineFromPath(ctx, resolvedTargetPath, pipeline.WithMutate())
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline at '%s': %w", resolvedTargetPath, err)
	}

	processedAssets := 0
	successfulAssets := 0
	failedAssets := 0
	for _, asset := range foundPipeline.Assets {
		if asset == nil {
			continue
		}
		processedAssets++
		if err := updateDirectAssetDependencies(ctx, asset, foundPipeline, sqlParserInstance, jinjaRenderer, fs); err != nil {
			failedAssets++
			continue
		}
		successfulAssets++
	}

	resp := directFillAssetDependenciesResponse{
		Status:           "success",
		ProcessedAssets:  processedAssets,
		SuccessfulAssets: successfulAssets,
		FailedAssets:     failedAssets,
	}
	return json.Marshal(resp)
}

func (e *HybridBruinExecutor) applyFillColumnsFromDB(ctx context.Context, targetPath string) ([]byte, error) {
	fs := afero.NewOsFs()
	if directPathReferencesAsset(targetPath) {
		pp, err := getDirectPipelineAndAsset(ctx, e.workspaceRoot, targetPath, fs)
		if err != nil {
			return nil, err
		}
		status, err := fillDirectColumnsFromDB(ctx, pp, fs, "", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to fill columns from DB for asset '%s': %w", pp.Asset.Name, err)
		}
		return json.Marshal(directFillColumnsAssetResponse{Status: status, Asset: pp.Asset.Name})
	}

	builder := e.newPipelineBuilder()
	foundPipeline, err := builder.CreatePipelineFromPath(ctx, resolveDirectPath(e.workspaceRoot, targetPath), pipeline.WithMutate())
	if err != nil {
		return nil, fmt.Errorf("failed to build pipeline at '%s': %w", targetPath, err)
	}
	repoRoot, err := git.FindRepoFromPath(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find the git repository root: %w", err)
	}
	cm, err := loadSelectedConfigFS(fs, filepath.Join(repoRoot.Path, ".bruin.yml"), "")
	if err != nil {
		return nil, fmt.Errorf("failed to load the config file: %w", err)
	}

	updatedAssets := []string{}
	skippedAssets := []string{}
	failedAssets := []string{}
	for _, asset := range foundPipeline.Assets {
		if asset == nil {
			continue
		}
		pp := &directPipelineInfo{Pipeline: foundPipeline, Asset: asset, Config: cm}
		status, err := fillDirectColumnsFromDB(ctx, pp, fs, "", nil)
		switch status {
		case fillStatusUpdated:
			updatedAssets = append(updatedAssets, asset.Name)
		case fillStatusSkipped:
			skippedAssets = append(skippedAssets, asset.Name)
		case fillStatusFailed:
			failedAssets = append(failedAssets, asset.Name)
			_ = err
		}
	}

	resp := directFillColumnsPipelineResponse{
		Status:            "success",
		UpdatedAssetNames: updatedAssets,
		SkippedAssetNames: skippedAssets,
		FailedAssetNames:  failedAssets,
		ProcessedAssets:   len(foundPipeline.Assets),
	}
	return json.Marshal(resp)
}
