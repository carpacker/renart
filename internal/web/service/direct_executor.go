package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

type HybridBruinExecutor struct {
	newConnectionManager func(context.Context, string) (config.ConnectionAndDetailsGetter, error)
	newPipelineBuilder   func() *pipeline.Builder
	workspaceRoot        string
}

func NewHybridBruinExecutor(
	workspaceRoot string,
	binaryPath string,
	newConnectionManager func(context.Context, string) (config.ConnectionAndDetailsGetter, error),
	newPipelineBuilder func() *pipeline.Builder,
) *HybridBruinExecutor {
	return &HybridBruinExecutor{
		newConnectionManager: newConnectionManager,
		newPipelineBuilder:   newPipelineBuilder,
		workspaceRoot:        workspaceRoot,
	}
}

func (e *HybridBruinExecutor) FormatAsset(ctx context.Context, req FormatAssetRequest) ([]byte, error) {
	_ = ctx
	assetPath := resolveDirectPath(e.workspaceRoot, req.AssetPath)
	osFS := afero.NewOsFs()
	builder := pipeline.NewBuilder(
		BuilderConfig,
		pipeline.CreateTaskFromYamlDefinition(osFS),
		pipeline.CreateTaskFromFileComments(osFS),
		osFS,
		DefaultGlossaryReader,
		jinja.VariantRendererFactory,
	)
	asset, err := builder.CreateAssetFromFile(assetPath, nil)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, fmt.Errorf("no valid asset found in the file")
	}
	if err := asset.Persist(afero.NewOsFs()); err != nil {
		return nil, err
	}
	return []byte(""), nil
}

func (e *HybridBruinExecutor) ApplyPatch(ctx context.Context, req PatchRequest) ([]byte, error) {
	switch req.Operation {
	case "fill-asset-dependencies":
		return e.applyFillAssetDependencies(ctx, req.TargetPath)
	case "fill-columns-from-db":
		return e.applyFillColumnsFromDB(ctx, req.TargetPath)
	default:
		return nil, fmt.Errorf("direct patch operation %q is not supported", req.Operation)
	}
}

func (e *HybridBruinExecutor) RunWithRetry(
	ctx context.Context,
	req QueryAssetRequest,
	retries int,
	initialDelay time.Duration,
) ([]byte, error, int) {
	attempt := 0
	delay := initialDelay
	for {
		attempt++
		output, err := e.QueryAsset(ctx, req)
		if err == nil {
			return output, nil, attempt
		}
		if !IsDuckDBLockError(err, output) || attempt > retries {
			return output, err, attempt
		}
		select {
		case <-ctx.Done():
			return output, ctx.Err(), attempt
		case <-time.After(delay):
		}
		delay *= 2
	}
}
