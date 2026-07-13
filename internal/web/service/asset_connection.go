package service

import (
	"errors"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// defaultPipelineTargetConnection resolves the warehouse a connectionless
// materializing asset should use. API, Python, and Load assets all share this
// rule so "Auto" means the same thing in every editor and execution path.
func defaultPipelineTargetConnection(pl *pipeline.Pipeline) (string, error) {
	if pl == nil {
		return "", errors.New("pipeline is required to resolve the default connection")
	}

	majorityType := pl.GetMajorityAssetTypesFromSQLAssets(pipeline.AssetTypeDuckDBQuery)
	if connectionName, err := pl.GetConnectionNameForAsset(&pipeline.Asset{Type: majorityType}); err == nil {
		if connectionName = strings.TrimSpace(connectionName); connectionName != "" {
			return connectionName, nil
		}
	}

	if len(pl.DefaultConnections) == 1 {
		for _, connectionName := range pl.DefaultConnections {
			if connectionName = strings.TrimSpace(connectionName); connectionName != "" {
				return connectionName, nil
			}
		}
	}

	return "", errors.New("the pipeline has no resolvable default target connection")
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
