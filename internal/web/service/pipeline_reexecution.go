package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"

	"renart/internal/web/policy"
	"renart/internal/web/runcontext"
)

// RetainedRunContextValidationRequest contains the private values needed to
// prove that an old reviewed plan is still replayable. It is an internal Go
// contract and must never be serialized into the public run API.
type RetainedRunContextValidationRequest struct {
	PipelineID                  string
	PipelineUUID                string
	Environment                 string
	Source                      PipelinePlanSourceRequest
	VariableOverrides           map[string]any
	ConfigurationAssetNames     []string
	ExpectedSourceMerkle        string
	ExpectedConfigurationDigest string
}

// ValidateRetainedRunContext performs the cheap exactness portion of planning:
// resolve and hash the original source, reapply the retained variables, and
// recompute the secret-free selected-configuration identity. Rendering and
// current data-state selection are deliberately not regenerated—the immutable
// retained plan remains the execution contract.
func (s *PipelinePlanService) ValidateRetainedRunContext(
	ctx context.Context,
	req RetainedRunContextValidationRequest,
) error {
	if s == nil {
		return fmt.Errorf("pipeline planning is unavailable")
	}
	pipelineID := strings.TrimSpace(req.PipelineID)
	pipelineUUID := strings.TrimSpace(req.PipelineUUID)
	if pipelineID == "" || pipelineUUID == "" {
		return fmt.Errorf("the original pipeline identity is unavailable")
	}

	cfg, err := loadSelectedConfigReadOnlyFS(afero.NewOsFs(), s.deps.ConfigPath, req.Environment)
	if err != nil {
		return fmt.Errorf("the original environment configuration is no longer resolvable")
	}
	sourceRequest, err := normalizePipelinePlanSource(req.Source, policy.EnvironmentPolicy{})
	if err != nil {
		return fmt.Errorf("the original source is no longer resolvable: %w", err)
	}
	resolved, deploymentRequired, apiErr := s.resolveSource(
		ctx,
		pipelineID,
		pipelineUUID,
		sourceRequest,
		req.VariableOverrides,
	)
	if apiErr != nil {
		return fmt.Errorf("the original source is no longer resolvable: %s", apiErr.Message)
	}
	if deploymentRequired || resolved == nil {
		return fmt.Errorf("the original source is no longer resolvable")
	}
	defer resolved.cleanup()

	if strings.TrimSpace(resolved.source.MerkleRoot) != strings.TrimSpace(req.ExpectedSourceMerkle) {
		return fmt.Errorf("the original source has changed")
	}

	assetsByName := make(map[string]*pipeline.Asset, len(resolved.parsed.Assets))
	for _, asset := range resolved.parsed.Assets {
		if asset != nil {
			assetsByName[asset.Name] = asset
		}
	}
	selected := make([]*pipeline.Asset, 0, len(req.ConfigurationAssetNames))
	seen := make(map[string]struct{}, len(req.ConfigurationAssetNames))
	for _, rawName := range req.ConfigurationAssetNames {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		asset := assetsByName[name]
		if asset == nil {
			return fmt.Errorf("the original selected asset %q is no longer present", name)
		}
		seen[name] = struct{}{}
		selected = append(selected, asset)
	}

	identity := selectedPipelineConfigurationIdentity(
		s.deps.WorkspaceRoot,
		cfg,
		resolved.parsed,
		selected,
	)
	if identity.Fidelity != runcontext.IdentityFidelityExact || strings.TrimSpace(identity.Digest) == "" {
		return fmt.Errorf("the original selected configuration can no longer be verified")
	}
	if identity.Digest != strings.TrimSpace(req.ExpectedConfigurationDigest) {
		return fmt.Errorf("the selected environment configuration has changed")
	}
	return nil
}
