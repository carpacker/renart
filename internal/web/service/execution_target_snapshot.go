package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/fingerprint"
	"renart/internal/web/identity"
	"renart/internal/web/matlog"
)

// ExecutionTargetSnapshotVersion is the persisted execution-target contract.
// Bump it when entry semantics or identity derivation change incompatibly.
const ExecutionTargetSnapshotVersion = 3

type ExecutionCoverageMode string

const (
	ExecutionCoverageMarker          ExecutionCoverageMode = "marker"
	ExecutionCoverageUnionIntervals  ExecutionCoverageMode = "union_intervals"
	ExecutionCoverageReplaceInterval ExecutionCoverageMode = "replace_interval"
)

// ExecutionUpstreamSnapshot is the dependency edge used by the fingerprint
// engine. It excludes resolved connection/configuration values; Type and Value
// are the same user-authored dependency coordinates present in pipeline source.
type ExecutionUpstreamSnapshot struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ExecutionTargetSnapshot captures the value-only target and fingerprint
// context resolved immediately before execution. Entries are keyed by the
// canonical asset name used by scheduler steps and completion events.
type ExecutionTargetSnapshot struct {
	Version               int                                     `json:"version"`
	PipelineUUID          string                                  `json:"pipeline_uuid"`
	ConfigurationDigest   string                                  `json:"configuration_digest,omitempty"`
	ConfigurationFidelity string                                  `json:"configuration_fidelity,omitempty"`
	Entries               map[string]ExecutionTargetSnapshotEntry `json:"entries"`
}

// ExecutionTargetSnapshotEntry deliberately contains no resolved object,
// connection, endpoint, or credential-bearing configuration. TargetIdentity
// is the opaque secret-free digest produced by the physical-target resolver;
// runtime-only targets remain explicit through TargetFidelity and an empty
// TargetIdentity.
type ExecutionTargetSnapshotEntry struct {
	AssetID                     string                      `json:"asset_id"`
	TargetIdentity              string                      `json:"target_identity"`
	TargetFidelity              AssetRenderFidelity         `json:"target_fidelity"`
	TargetWriteEvidenceRequired bool                        `json:"target_write_evidence_required,omitempty"`
	WriteResourceKind           string                      `json:"write_resource_kind"`
	WriteResourceIdentity       string                      `json:"write_resource_identity,omitempty"`
	WriteResourceFidelity       AssetRenderFidelity         `json:"write_resource_fidelity"`
	Fingerprint                 string                      `json:"fingerprint"`
	OwnContent                  string                      `json:"own_content"`
	ConsumedVarsHash            string                      `json:"consumed_vars_hash"`
	VarsHash                    string                      `json:"vars_hash"`
	Upstreams                   []ExecutionUpstreamSnapshot `json:"upstreams"`
	CoverageMode                ExecutionCoverageMode       `json:"coverage_mode"`
	RefreshRestricted           bool                        `json:"refresh_restricted"`
}

func (e *HybridBruinExecutor) resolveExecutionTargetSnapshot(
	pl *pipeline.Pipeline,
	cfg *config.Config,
	selectedAssets []*pipeline.Asset,
) (ExecutionTargetSnapshot, error) {
	return e.resolveExecutionTargetSnapshotForSelection(pl, cfg, selectedAssets, selectedAssets)
}

func (e *HybridBruinExecutor) resolveExecutionTargetSnapshotForSelection(
	pl *pipeline.Pipeline,
	cfg *config.Config,
	targetAssets []*pipeline.Asset,
	configurationAssets []*pipeline.Asset,
) (ExecutionTargetSnapshot, error) {
	if pl == nil {
		return ExecutionTargetSnapshot{}, fmt.Errorf("pipeline is required")
	}
	pipelineID := strings.TrimSpace(pl.LegacyID)
	if pipelineID == "" {
		return ExecutionTargetSnapshot{}, fmt.Errorf("pipeline %q has no stable id", pl.Name)
	}

	vars := fingerprint.EffectiveVars(pl, nil)
	engine := e.fingerprintEngine
	if engine == nil {
		engine = fingerprint.NewEngine()
	}
	results, err := engine.DAG(pl, vars)
	if err != nil {
		return ExecutionTargetSnapshot{}, err
	}

	entries := make(map[string]ExecutionTargetSnapshotEntry, len(targetAssets))
	varsHash := fingerprint.AllVarsHash(vars)
	for _, asset := range targetAssets {
		if asset == nil {
			return ExecutionTargetSnapshot{}, fmt.Errorf("selected asset is nil")
		}
		assetName := asset.Name
		if strings.TrimSpace(assetName) == "" {
			return ExecutionTargetSnapshot{}, fmt.Errorf("selected asset has no name")
		}
		if _, exists := entries[assetName]; exists {
			return ExecutionTargetSnapshot{}, fmt.Errorf("selected asset %q is duplicated", assetName)
		}

		assetID := identity.AssetID(pipelineID, assetName)
		result, ok := results[assetID]
		if !ok {
			return ExecutionTargetSnapshot{}, fmt.Errorf("fingerprint result is missing for asset %q", assetName)
		}
		target := resolveAssetPhysicalTarget(e.workspaceRoot, &directPipelineInfo{
			Pipeline: pl,
			Asset:    asset,
			Config:   cfg,
		})
		entry := ExecutionTargetSnapshotEntry{
			AssetID:                     assetID,
			TargetIdentity:              target.Identity,
			TargetFidelity:              target.Fidelity,
			TargetWriteEvidenceRequired: pythonTargetWriteEvidenceRequired(asset, target),
			WriteResourceKind:           target.WriteResource.Kind,
			WriteResourceIdentity:       target.WriteResource.Identity,
			WriteResourceFidelity:       target.WriteResource.Fidelity,
			Fingerprint:                 string(result.FP),
			OwnContent:                  string(result.OwnContent),
			ConsumedVarsHash:            result.ConsumedVarsHash,
			VarsHash:                    varsHash,
			CoverageMode:                executionCoverageMode(asset),
			RefreshRestricted:           asset.RefreshRestricted != nil && *asset.RefreshRestricted,
		}
		entry.Upstreams = make([]ExecutionUpstreamSnapshot, 0, len(asset.Upstreams))
		for _, upstream := range asset.Upstreams {
			entry.Upstreams = append(entry.Upstreams, ExecutionUpstreamSnapshot{
				Type:  upstream.Type,
				Value: upstream.Value,
			})
		}
		entries[assetName] = entry
	}

	configurationIdentity := selectedPipelineConfigurationIdentity(
		e.workspaceRoot,
		cfg,
		pl,
		configurationAssets,
	)
	return ExecutionTargetSnapshot{
		Version:               ExecutionTargetSnapshotVersion,
		PipelineUUID:          pipelineID,
		ConfigurationDigest:   configurationIdentity.Digest,
		ConfigurationFidelity: string(configurationIdentity.Fidelity),
		Entries:               entries,
	}, nil
}

func pythonTargetWriteEvidenceRequired(asset *pipeline.Asset, target AssetRenderTarget) bool {
	return asset != nil && asset.Type == pipeline.AssetTypePython &&
		asset.Materialization.Type == pipeline.MaterializationTypeTable &&
		target.Fidelity == AssetRenderFidelityExact && target.Identity != ""
}

func executionCoverageMode(asset *pipeline.Asset) ExecutionCoverageMode {
	if !matlog.IntervalAware(asset) {
		return ExecutionCoverageMarker
	}
	if matlog.BackfillSafe(asset) {
		return ExecutionCoverageUnionIntervals
	}
	return ExecutionCoverageReplaceInterval
}

func (e *HybridBruinExecutor) notifyExecutionTargetsResolved(
	pl *pipeline.Pipeline,
	cfg *config.Config,
	selectedAssets []*pipeline.Asset,
	callback func(ExecutionTargetSnapshot) error,
) error {
	if callback == nil {
		return nil
	}
	snapshot, err := e.resolveExecutionTargetSnapshot(pl, cfg, selectedAssets)
	if err != nil {
		return fmt.Errorf("resolve execution target snapshot: %w", err)
	}
	if err := callback(snapshot); err != nil {
		return fmt.Errorf("persist execution target snapshot: %w", err)
	}
	return nil
}

func (e *HybridBruinExecutor) notifyExecutionTargetsResolvedForSelection(
	pl *pipeline.Pipeline,
	cfg *config.Config,
	targetAssets []*pipeline.Asset,
	configurationAssets []*pipeline.Asset,
	callback func(ExecutionTargetSnapshot) error,
) error {
	if callback == nil {
		return nil
	}
	snapshot, err := e.resolveExecutionTargetSnapshotForSelection(pl, cfg, targetAssets, configurationAssets)
	if err != nil {
		return fmt.Errorf("resolve execution target snapshot: %w", err)
	}
	if err := callback(snapshot); err != nil {
		return fmt.Errorf("persist execution target snapshot: %w", err)
	}
	return nil
}
