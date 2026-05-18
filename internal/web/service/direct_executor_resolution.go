package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/git"
	"github.com/bruin-data/bruin/pkg/jinja"
	bruinpath "github.com/bruin-data/bruin/pkg/path"
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
	resolvedInputPath := resolveDirectPath(workspaceRoot, inputPath)
	repoRoot, err := git.FindRepoFromPath(resolvedInputPath)
	if err != nil {
		return nil, err
	}
	pipelinePath, err := bruinpath.GetPipelineRootFromTask(resolvedInputPath, BuilderConfig.PipelineFileName)
	if err != nil {
		return nil, err
	}
	configFilePath := filepath.Join(repoRoot.Path, ".bruin.yml")
	cm, err := loadSelectedConfigFS(fs, configFilePath, "")
	if err != nil {
		return nil, err
	}
	builder := pipeline.NewBuilder(
		BuilderConfig,
		pipeline.CreateTaskFromYamlDefinition(fs),
		pipeline.CreateTaskFromFileComments(fs),
		fs,
		DefaultGlossaryReader,
	)
	foundPipeline, err := builder.CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate())
	if err != nil {
		return nil, err
	}
	asset, err := builder.CreateAssetFromFile(resolvedInputPath, foundPipeline)
	if err != nil {
		return nil, err
	}
	asset, err = builder.MutateAsset(ctx, asset, foundPipeline)
	if err != nil {
		return nil, err
	}
	return &directPipelineInfo{Pipeline: foundPipeline, Asset: asset, Config: cm}, nil
}

func getDirectConnectionAndQuery(ctx context.Context, pp *directPipelineInfo, environment string) (string, interface{}, string, error) {
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

	renderer := jinja.NewRendererWithYesterday(pp.Pipeline.Name, "renart-query")
	fetchCtx := context.WithValue(ctx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)
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
