package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/runcontext"
)

// selectedPipelineConfigurationIdentity projects exactly the selected assets'
// environment controls and connections through the same secret-free identity
// used by asset rendering. Planning and the pre-execution target snapshot both
// call this helper, so confirmation compares like with like.
func selectedPipelineConfigurationIdentity(
	cfg *config.Config,
	pl *pipeline.Pipeline,
	assets []*pipeline.Asset,
) runcontext.Identity {
	if cfg == nil {
		cfg = &config.Config{}
	}
	connectionNames := make([]string, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  "selected asset is nil",
			}
		}
		info := &directPipelineInfo{Pipeline: pl, Asset: asset, Config: cfg}
		primary, err := assetRenderConnectionName(info)
		if err != nil && !assetRenderAssetIsConnectionless(info) {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  fmt.Sprintf("asset %q connection configuration could not be resolved", asset.Name),
			}
		}
		connectionNames = append(connectionNames, assetRenderConfigurationConnectionNames(info, primary)...)
	}
	return runcontext.SelectedConfigurationIdentity(
		strings.TrimSpace(cfg.SelectedEnvironmentName),
		cfg.SelectedEnvironment,
		connectionNames,
	)
}
