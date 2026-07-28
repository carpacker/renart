package service

import (
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionContractUsesRelationCoordinationForAuditedDuckDBAsset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	connection := config.DuckDBConnection{
		ConnectionMetadata: targetMetadata("warehouse"),
		Path:               filepath.Join(root, "warehouse.duckdb"),
	}
	cfg := &config.Config{SelectedEnvironment: &config.Environment{
		Connections: &config.Connections{DuckDB: []config.DuckDBConnection{connection}},
	}}
	asset := materializedTargetAsset(
		pipeline.AssetTypeDuckDBQuery,
		"analytics.customers",
		"warehouse",
	)
	pl := &pipeline.Pipeline{LegacyID: "pipeline-id", Assets: []*pipeline.Asset{asset}}
	target := resolveAssetPhysicalTarget(root, &directPipelineInfo{
		Asset: asset, Pipeline: pl, Config: cfg,
	})

	contract, err := executionContractForAsset(root, cfg, pl, asset, target)
	require.NoError(t, err)
	require.Equal(t, PipelinePlanResourceIsolationResources, contract.MutationResources.Isolation)
	require.Equal(t, PipelinePlanResourceIsolationResources, contract.CoordinationResources.Isolation)
	require.Len(t, contract.MutationResources.Claims, 1)
	assert.Equal(t, contract.MutationResources.Claims, contract.CoordinationResources.Claims)

	databaseResource := exactAssetWriteResource(assetWriteResourceDuckDB, connection.Path, "")
	assert.NotEqual(t, databaseResource.Identity, contract.CoordinationResources.Claims[0].Identity)
}

func TestExecutionContractKeepsWholeDatabaseCoordinationForUnauditedDuckDBAsset(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	connection := config.DuckDBConnection{
		ConnectionMetadata: targetMetadata("warehouse"),
		Path:               filepath.Join(root, "warehouse.duckdb"),
	}
	cfg := &config.Config{SelectedEnvironment: &config.Environment{
		Connections: &config.Connections{DuckDB: []config.DuckDBConnection{connection}},
	}}
	asset := materializedTargetAsset(
		pipeline.AssetTypeDuckDBQuery,
		"analytics.customers",
		"warehouse",
	)
	asset.Materialization.Strategy = pipeline.MaterializationStrategyDDL
	pl := &pipeline.Pipeline{LegacyID: "pipeline-id", Assets: []*pipeline.Asset{asset}}
	target := resolveAssetPhysicalTarget(root, &directPipelineInfo{
		Asset: asset, Pipeline: pl, Config: cfg,
	})

	contract, err := executionContractForAsset(root, cfg, pl, asset, target)
	require.NoError(t, err)
	databaseResource := exactAssetWriteResource(assetWriteResourceDuckDB, connection.Path, "")
	assert.Equal(t, []PipelinePlanResourceClaim{{
		Kind: assetWriteResourceDuckDB, Identity: databaseResource.Identity,
	}}, contract.CoordinationResources.Claims)
}
