package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/spf13/afero"
)

type directPipelineInfo struct {
	Pipeline *pipeline.Pipeline
	Asset    *pipeline.Asset
	Config   *config.Config
}

func resolveDirectPipelinePath(pipelinePath string) string {
	pathParts := strings.Split(pipelinePath, "/")
	last := pathParts[len(pathParts)-1]
	if last != "pipeline.yml" && last != "pipeline.yaml" {
		return pipelinePath
	}
	if len(pathParts) == 1 {
		return "."
	}
	return strings.Join(pathParts[:len(pathParts)-1], "/")
}

func resolveDirectPath(workspaceRoot, maybeRelative string) string {
	trimmed := strings.TrimSpace(maybeRelative)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return trimmed
	}
	return filepath.Join(workspaceRoot, filepath.FromSlash(trimmed))
}

func directPathReferencesAsset(inputPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(inputPath))
	for _, suffix := range pipeline.SupportedFileSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return true
		}
	}
	return false
}

func getDirectPipelineAndAsset(ctx context.Context, workspaceRoot, inputPath string, fs afero.Fs) (*directPipelineInfo, error) {
	return getDirectPipelineAndAssetWithConfigLoader(ctx, workspaceRoot, inputPath, fs, "", loadSelectedConfigFS)
}

func getDirectPipelineAndAssetReadOnly(ctx context.Context, workspaceRoot, inputPath string, fs afero.Fs) (*directPipelineInfo, error) {
	return getDirectPipelineAndAssetWithConfigLoader(ctx, workspaceRoot, inputPath, fs, "", loadSelectedConfigReadOnlyFS)
}

// getDirectPipelineAndAssetReadOnlyWithConfigPath resolves source files under
// workspaceRoot while loading configuration from an explicit, server-owned
// path. Snapshot planning uses this to render immutable pipeline files from a
// temporary directory without copying credentials or .bruin.yml into it.
func getDirectPipelineAndAssetReadOnlyWithConfigPath(
	ctx context.Context,
	workspaceRoot string,
	inputPath string,
	fs afero.Fs,
	configFilePath string,
) (*directPipelineInfo, error) {
	if strings.TrimSpace(configFilePath) == "" {
		return nil, fmt.Errorf("configuration path is required")
	}
	return getDirectPipelineAndAssetWithConfigLoader(
		ctx,
		workspaceRoot,
		inputPath,
		fs,
		configFilePath,
		loadSelectedConfigReadOnlyFS,
	)
}

func getDirectPipelineAndAssetWithConfigLoader(
	ctx context.Context,
	workspaceRoot string,
	inputPath string,
	fs afero.Fs,
	configFilePath string,
	loadConfig func(afero.Fs, string, string) (*config.Config, error),
) (*directPipelineInfo, error) {
	resolvedInputPath := resolveDirectPath(workspaceRoot, inputPath)
	if strings.TrimSpace(configFilePath) == "" {
		repoRoot, err := git.FindRepoFromPath(resolvedInputPath)
		if err != nil {
			return nil, err
		}
		configFilePath = filepath.Join(repoRoot.Path, ".bruin.yml")
	}
	cm, err := loadConfig(fs, configFilePath, "")
	if err != nil {
		return nil, err
	}
	resolver := NewWorkspaceResolver(workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		builder := NewRenartPipelineBuilder(fs)
		return builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	})
	_, foundPipeline, asset, err := resolver.ResolveAssetByPath(ctx, "", resolvedInputPath)
	if err != nil {
		return nil, err
	}
	return &directPipelineInfo{Pipeline: foundPipeline, Asset: asset, Config: cm}, nil
}

func getDirectConnectionAndQuery(ctx context.Context, pp *directPipelineInfo, environment, start, end string) (string, interface{}, string, error) {
	if environment != "" {
		if _, err := selectConfigEnvironment(pp.Config, environment); err != nil {
			return "", nil, "", err
		}
	}

	manager, err := newConnectionManagerFromConfig(ctx, pp.Config)
	if err != nil {
		return "", nil, "", err
	}

	connName, err := pp.Pipeline.GetConnectionNameForAsset(pp.Asset)
	if err != nil {
		return "", nil, "", err
	}
	conn := manager.GetConnection(connName)
	if conn == nil {
		return "", nil, "", fmt.Errorf("connection %q not found", connName)
	}

	now := time.Now().UTC()
	timeWindow, err := ResolveExecutionTimeWindow(string(pp.Pipeline.Schedule), start, end, now)
	if err != nil {
		return "", nil, "", err
	}
	renderer := jinja.NewRendererWithStartEndDates(&timeWindow.Start, &timeWindow.End, &now, pp.Pipeline.Name, "renart-query", nil)
	fetchCtx := context.WithValue(ctx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)
	fetchCtx = context.WithValue(fetchCtx, config.EnvironmentNameContextKey, pp.Config.SelectedEnvironmentName)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigStartDate, timeWindow.Start)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigEndDate, timeWindow.End)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigExecutionDate, now)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigRunID, "renart-query")
	extractor := &query.WholeFileExtractor{Fs: afero.NewOsFs(), Renderer: renderer}
	clonedExtractor, err := extractor.CloneForAsset(fetchCtx, pp.Pipeline, pp.Asset)
	if err != nil {
		return "", nil, "", err
	}
	queries, err := clonedExtractor.ExtractQueriesFromString(pp.Asset.ExecutableFile.Content)
	if err != nil {
		return "", nil, "", err
	}
	if len(queries) == 0 {
		return "", nil, "", fmt.Errorf("no query found in asset")
	}

	return connName, conn, queries[0].Query, nil
}
