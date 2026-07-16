package service

import (
	"context"
	"fmt"

	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
)

// resolveAssetHookTemplates returns rendered hooks without changing the parsed
// asset. Callers decide whether to use the returned value for a preview clone or
// assign it to the request-local pipeline used by an executor.
func resolveAssetHookTemplates(
	ctx context.Context,
	pl *pipeline.Pipeline,
	asset *pipeline.Asset,
	renderer jinja.RendererInterface,
) (pipeline.Hooks, error) {
	if asset == nil {
		return pipeline.Hooks{}, fmt.Errorf("asset is required to render hook templates")
	}
	if len(asset.Hooks.Pre) == 0 && len(asset.Hooks.Post) == 0 {
		return asset.Hooks, nil
	}

	assetRenderer, err := renderer.CloneForAsset(ctx, pl, asset)
	if err != nil {
		return pipeline.Hooks{}, fmt.Errorf("build asset hook renderer: %w", err)
	}
	resolved, err := pipeline.ResolveHookTemplatesToNew(asset.Hooks, assetRenderer)
	if err != nil {
		return pipeline.Hooks{}, err
	}

	// Bruin's resolver intentionally rebuilds only pre/post hooks. Preserve the
	// applicability metadata so resolving templates does not discard parsed
	// asset metadata in the request-local pipeline.
	resolved.ApplicableTypes = append([]string(nil), asset.Hooks.ApplicableTypes...)
	return resolved, nil
}

func resolveDirectDuckDBHookTemplates(
	ctx context.Context,
	pl *pipeline.Pipeline,
	asset *pipeline.Asset,
	renderer jinja.RendererInterface,
) error {
	if !supportsExactDuckDBExecutionRender(asset) {
		return nil
	}
	resolved, err := resolveAssetHookTemplates(ctx, pl, asset, renderer)
	if err != nil {
		return fmt.Errorf("render hooks for asset %q: %w", asset.Name, err)
	}
	asset.Hooks = resolved
	return nil
}
