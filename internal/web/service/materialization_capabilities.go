package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/model"
)

const (
	materializationModeNone         = "none"
	materializationModeView         = "view"
	materializationModeReplace      = "create+replace"
	materializationModeTruncate     = "truncate+insert"
	materializationModeAppend       = "append"
	materializationModeMerge        = "merge"
	materializationModeDeleteInsert = "delete+insert"
	materializationModeTimeInterval = "time_interval"
)

type materializationModeCapability struct {
	model.MaterializationCapability
	Editable bool
}

type materializationProfile struct {
	Modes               []materializationModeCapability
	SupportsFullRefresh bool
}

func materializationCapability(mode, materializationType, strategy string) materializationModeCapability {
	return materializationModeCapability{
		MaterializationCapability: model.MaterializationCapability{
			Mode:     mode,
			Type:     materializationType,
			Strategy: strategy,
		},
		Editable: true,
	}
}

func materializationNoneCapability() materializationModeCapability {
	return materializationCapability(materializationModeNone, "", "")
}

func materializationViewCapability() materializationModeCapability {
	return materializationCapability(materializationModeView, string(pipeline.MaterializationTypeView), "")
}

func materializationTableCapability(mode string) materializationModeCapability {
	capability := materializationCapability(mode, string(pipeline.MaterializationTypeTable), mode)
	switch mode {
	case materializationModeAppend:
		// SQL append has no key contract. Loader-backed profiles opt into an
		// update key below.
	case materializationModeMerge:
		capability.RequiresPrimaryKey = true
	case materializationModeDeleteInsert:
		capability.RequiresIncrementalKey = true
	case materializationModeTimeInterval:
		capability.RequiresIncrementalKey = true
		capability.RequiresTimeGranularity = true
	}
	return capability
}

func baseSQLMaterializationModes() []materializationModeCapability {
	return []materializationModeCapability{
		materializationNoneCapability(),
		materializationViewCapability(),
		materializationTableCapability(materializationModeReplace),
		materializationTableCapability(materializationModeTruncate),
		materializationTableCapability(materializationModeAppend),
		materializationTableCapability(materializationModeMerge),
		materializationTableCapability(materializationModeDeleteInsert),
		materializationTableCapability(materializationModeTimeInterval),
	}
}

func withoutMaterializationModes(modes []materializationModeCapability, excluded ...string) []materializationModeCapability {
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, mode := range excluded {
		excludedSet[mode] = struct{}{}
	}
	result := make([]materializationModeCapability, 0, len(modes))
	for _, mode := range modes {
		if _, skip := excludedSet[mode.Mode]; !skip {
			result = append(result, mode)
		}
	}
	return result
}

func withTableLayoutFields(modes []materializationModeCapability, partitionBy, clusterBy bool) []materializationModeCapability {
	for index := range modes {
		if modes[index].Type != string(pipeline.MaterializationTypeTable) {
			continue
		}
		modes[index].SupportsPartitionBy = partitionBy
		modes[index].SupportsClusterBy = clusterBy
	}
	return modes
}

func loaderMaterializationModes() []materializationModeCapability {
	modes := []materializationModeCapability{
		materializationTableCapability(materializationModeReplace),
		materializationTableCapability(materializationModeTruncate),
		materializationTableCapability(materializationModeAppend),
		materializationTableCapability(materializationModeMerge),
	}
	for index := range modes {
		if modes[index].Mode == materializationModeAppend || modes[index].Mode == materializationModeMerge {
			modes[index].SupportsIncrementalKey = true
		}
	}
	return modes
}

func pythonMaterializationModes(nativeDuckDB bool) []materializationModeCapability {
	modes := append([]materializationModeCapability{materializationNoneCapability()}, loaderMaterializationModes()...)
	if nativeDuckDB {
		for index := range modes {
			// Native DuckDB writes do not use Sling's optional update-key
			// optimization for append or merge.
			modes[index].SupportsIncrementalKey = false
		}
		modes = append(modes, materializationTableCapability(materializationModeDeleteInsert))
		return modes
	}
	// Non-DuckDB Python materialization uses the same Sling load leg as Load
	// and API assets, including optional update keys for append/merge.
	return modes
}

func materializationProfileFor(asset *pipeline.Asset, destinationType string) materializationProfile {
	if asset == nil {
		return materializationProfile{}
	}

	refreshRestricted := asset.RefreshRestricted != nil && *asset.RefreshRestricted
	withFullRefresh := func(modes []materializationModeCapability) materializationProfile {
		return materializationProfile{Modes: modes, SupportsFullRefresh: !refreshRestricted}
	}

	if isAPIAsset(asset) || isLoadAsset(asset) {
		return withFullRefresh(loaderMaterializationModes())
	}
	if asset.Type == pipeline.AssetTypePython {
		destinationType = normalizeConnectionType(destinationType)
		nativeDuckDB := destinationType == "duckdb" || destinationType == "motherduck"
		return withFullRefresh(pythonMaterializationModes(nativeDuckDB))
	}

	if !isQueryAssetType(asset.Type) || !isDirectRunAssetTypeSupported(asset.Type) {
		// Ingestr, R, seeds, sources, and sensors keep their dedicated runtime
		// configuration. The generic materialization editor must not imply that
		// they share SQL/Python semantics.
		return materializationProfile{}
	}
	if asset.Type == pipeline.AssetTypeOracleQuery {
		// Renart's direct Oracle operator intentionally runs query-only SQL and
		// rejects materialization, even though Bruin's package has a materializer.
		return materializationProfile{Modes: []materializationModeCapability{materializationNoneCapability()}}
	}

	modes := baseSQLMaterializationModes()
	switch asset.Type {
	case pipeline.AssetTypeFabricQuery, pipeline.AssetTypeFabricQueryLegacy:
		modes = withoutMaterializationModes(modes, materializationModeTimeInterval)
	case pipeline.AssetTypeClickHouse:
		modes = withoutMaterializationModes(modes, materializationModeMerge)
	case pipeline.AssetTypeTrinoQuery:
		modes = withoutMaterializationModes(modes, materializationModeMerge)
	}
	switch asset.Type {
	case pipeline.AssetTypeBigqueryQuery:
		modes = withTableLayoutFields(modes, true, true)
	case pipeline.AssetTypeAthenaQuery, pipeline.AssetTypeTrinoQuery:
		modes = withTableLayoutFields(modes, true, false)
	case pipeline.AssetTypeSnowflakeQuery:
		modes = withTableLayoutFields(modes, false, true)
	}
	if asset.Type == pipeline.AssetTypeClickHouse {
		for index := range modes {
			if modes[index].Mode == materializationModeReplace || modes[index].Mode == materializationModeDeleteInsert {
				modes[index].RequiresPrimaryKey = true
			}
		}
	}
	return withFullRefresh(modes)
}

func editableMaterializationCapabilities(profile materializationProfile) []model.MaterializationCapability {
	capabilities := make([]model.MaterializationCapability, 0, len(profile.Modes))
	for _, capability := range profile.Modes {
		if capability.Editable {
			capabilities = append(capabilities, capability.MaterializationCapability)
		}
	}
	return capabilities
}

func supportsFullRefreshForAsset(asset *pipeline.Asset, profile materializationProfile) bool {
	if asset == nil || !profile.SupportsFullRefresh {
		return false
	}
	if asset.Materialization.Type == pipeline.MaterializationTypeTable {
		// Full refresh means "temporarily render as create+replace". Bruin keeps
		// DDL untouched, and Renart does not yet expose the larger contracts of
		// other advanced strategies, so only advertise the action for a mode in
		// the guided capability profile.
		_, supported := materializationCapabilityForMode(profile, normalizedMaterializationMode(asset))
		return supported
	}
	// Load/API assets always write a table; an omitted block is their canonical
	// create+replace default rather than SQL/Python's run-only mode.
	return (isAPIAsset(asset) || isLoadAsset(asset)) && asset.Materialization.Type == pipeline.MaterializationTypeNone
}

func supportsAdvancedSQLMaterialization(assetType pipeline.AssetType, mode string) bool {
	switch mode {
	case string(pipeline.MaterializationStrategyDDL):
		switch assetType {
		case pipeline.AssetTypeDuckDBQuery,
			pipeline.AssetTypeMotherduckQuery,
			pipeline.AssetTypePostgresQuery,
			pipeline.AssetTypeRedshiftQuery,
			pipeline.AssetTypeBigqueryQuery,
			pipeline.AssetTypeAthenaQuery,
			pipeline.AssetTypeDatabricksQuery,
			pipeline.AssetTypeMySQLQuery,
			pipeline.AssetTypeSnowflakeQuery,
			pipeline.AssetTypeMsSQLQuery,
			pipeline.AssetTypeSynapseQuery,
			pipeline.AssetTypeFabricQuery,
			pipeline.AssetTypeFabricQueryLegacy,
			pipeline.AssetTypeClickHouse,
			pipeline.AssetTypeTrinoQuery,
			pipeline.AssetTypeVerticaQuery:
			return true
		}
	case string(pipeline.MaterializationStrategySCD2ByTime),
		string(pipeline.MaterializationStrategySCD2ByColumn):
		switch assetType {
		case pipeline.AssetTypeDuckDBQuery,
			pipeline.AssetTypeMotherduckQuery,
			pipeline.AssetTypePostgresQuery,
			pipeline.AssetTypeRedshiftQuery,
			pipeline.AssetTypeBigqueryQuery,
			pipeline.AssetTypeAthenaQuery,
			pipeline.AssetTypeDatabricksQuery,
			pipeline.AssetTypeMySQLQuery,
			pipeline.AssetTypeSnowflakeQuery,
			pipeline.AssetTypeTrinoQuery:
			return true
		}
	case string(pipeline.MaterializationStrategyDataVaultHub),
		string(pipeline.MaterializationStrategyDataVaultLink),
		string(pipeline.MaterializationStrategyDataVaultSatellite):
		switch assetType {
		case pipeline.AssetTypeDuckDBQuery,
			pipeline.AssetTypeMotherduckQuery,
			pipeline.AssetTypePostgresQuery,
			pipeline.AssetTypeRedshiftQuery:
			return true
		}
	}
	return false
}

func normalizedMaterializationMode(asset *pipeline.Asset) string {
	if asset == nil {
		return materializationModeNone
	}
	materializationType := strings.ToLower(strings.TrimSpace(string(asset.Materialization.Type)))
	strategy := normalizeMaterializationStrategy(string(asset.Materialization.Strategy))
	if materializationType == "" {
		if isAPIAsset(asset) || isLoadAsset(asset) {
			return materializationModeReplace
		}
		return materializationModeNone
	}
	if materializationType == string(pipeline.MaterializationTypeView) {
		return materializationModeView
	}
	if materializationType == string(pipeline.MaterializationTypeTable) && strategy == "" {
		return materializationModeReplace
	}
	return strategy
}

func normalizeMaterializationStrategy(strategy string) string {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case "create_replace", "full-refresh", "full_refresh":
		return materializationModeReplace
	case "truncate_insert", "truncate":
		return materializationModeTruncate
	case "delete_insert":
		return materializationModeDeleteInsert
	default:
		return normalized
	}
}

func materializationCapabilityForMode(profile materializationProfile, mode string) (materializationModeCapability, bool) {
	for _, capability := range profile.Modes {
		if capability.Mode == mode {
			return capability, true
		}
	}
	return materializationModeCapability{}, false
}

func validateMaterializationCapability(asset *pipeline.Asset, destinationType string) error {
	if asset == nil {
		return fmt.Errorf("asset is required to validate materialization")
	}
	profile := materializationProfileFor(asset, destinationType)
	mode := normalizedMaterializationMode(asset)
	if _, ok := materializationCapabilityForMode(profile, mode); ok {
		if asset.Materialization.Type == pipeline.MaterializationTypeView && strings.TrimSpace(string(asset.Materialization.Strategy)) != "" {
			return fmt.Errorf("view materialization does not support strategy %q", asset.Materialization.Strategy)
		}
		return nil
	}

	// Advanced Bruin SQL strategies remain hand-authored until Renart has
	// dedicated editors for their larger field contracts, but saves and type
	// checks still use the concrete warehouse materializer's support matrix.
	if isQueryAssetType(asset.Type) && supportsAdvancedSQLMaterialization(asset.Type, mode) {
		return nil
	}

	return fmt.Errorf("materialization mode %q is not supported for %s assets on this destination", mode, asset.Type)
}

func materializationDestinationType(asset *pipeline.Asset, pl *pipeline.Pipeline, connectionTypes map[string]string) string {
	if asset == nil {
		return ""
	}
	if connectionType, ok := pipeline.AssetTypeConnectionMapping[asset.Type]; ok && isQueryAssetType(asset.Type) {
		return normalizeConnectionType(connectionType)
	}

	connectionName, err := targetConnectionNameForAsset(asset, pl)
	if err == nil {
		if strings.EqualFold(strings.TrimSpace(connectionName), loadLocalConnectionName) {
			return loadLocalConnectionName
		}
		for name, connectionType := range connectionTypes {
			if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(connectionName)) {
				return normalizeConnectionType(connectionType)
			}
		}
		if queryType, ok := sqlAssetTypeForConnectionName(pl, connectionName); ok {
			return normalizeConnectionType(pipeline.AssetTypeConnectionMapping[pipeline.AssetType(queryType)])
		}
		// An explicit named connection must never inherit the pipeline's majority
		// warehouse when its concrete type is unavailable. Keeping this unknown is
		// conservative (notably, Python will not be offered DuckDB-only modes).
		if strings.TrimSpace(asset.Connection) != "" {
			return ""
		}
	}

	if pl != nil {
		majorityType := pl.GetMajorityAssetTypesFromSQLAssets(pipeline.AssetTypeDuckDBQuery)
		if connectionType, ok := pipeline.AssetTypeConnectionMapping[majorityType]; ok {
			return normalizeConnectionType(connectionType)
		}
	}
	return ""
}
