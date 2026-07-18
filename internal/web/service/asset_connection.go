package service

import (
	"errors"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// defaultPipelineTargetConnection resolves the warehouse a connectionless
// materializing asset should use. Renart-owned API and Load assets share this
// rule so "Auto" means the same thing in every editor and execution path.
func defaultPipelineTargetConnection(pl *pipeline.Pipeline) (string, error) {
	if pl == nil {
		return "", errors.New("pipeline is required to resolve the default connection")
	}

	// With no SQL/ingestr destination to establish a warehouse majority, a
	// single configured default is the only unambiguous target. Resolve it
	// before Bruin can synthesize the magic duckdb-default fallback.
	if !pipelineHasSQLMajorityCandidate(pl) && len(pl.DefaultConnections) == 1 {
		for _, connectionName := range pl.DefaultConnections {
			if connectionName = strings.TrimSpace(connectionName); connectionName != "" {
				return connectionName, nil
			}
		}
	}

	majorityType := pl.GetMajorityAssetTypesFromSQLAssets(pipeline.AssetTypeDuckDBQuery)
	if connectionName, err := pl.GetConnectionNameForAsset(&pipeline.Asset{Type: majorityType}); err == nil {
		if connectionName = strings.TrimSpace(connectionName); connectionName != "" {
			return connectionName, nil
		}
	}

	return "", errors.New("the pipeline has no resolvable default target connection")
}

func pipelineHasSQLMajorityCandidate(pl *pipeline.Pipeline) bool {
	if pl == nil {
		return false
	}
	for _, asset := range pl.Assets {
		if asset == nil {
			continue
		}
		assetType := asset.Type
		if assetType == pipeline.AssetTypeIngestr {
			destination, ok := asset.Parameters.GetString("destination")
			if !ok {
				continue
			}
			mapped, ok := pipeline.IngestrTypeConnectionMapping[destination]
			if !ok {
				continue
			}
			assetType = mapped
		}
		if isQueryAssetType(assetType) {
			return true
		}
	}
	return false
}

func loadConnectionNameForAsset(asset *pipeline.Asset, pl *pipeline.Pipeline) (string, error) {
	if asset == nil {
		return "", errors.New("load asset is required")
	}
	if connectionName := strings.TrimSpace(asset.Connection); connectionName != "" {
		return connectionName, nil
	}
	return defaultPipelineTargetConnection(pl)
}

// targetConnectionNameForAsset is the canonical effective target-connection
// resolver used by workspace DTOs, execution, inspection, and materialization
// status. The asset's own Connection field remains the explicit override.
func targetConnectionNameForAsset(asset *pipeline.Asset, pl *pipeline.Pipeline) (string, error) {
	switch {
	case isAPIAsset(asset):
		return apiConnectionNameForAsset(asset, pl)
	case isLoadAsset(asset):
		return loadConnectionNameForAsset(asset, pl)
	case asset == nil:
		return "", errors.New("asset is required")
	case pl == nil:
		return "", errors.New("pipeline is required")
	default:
		return pl.GetConnectionNameForAsset(asset)
	}
}
