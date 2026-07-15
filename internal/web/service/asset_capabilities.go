package service

import (
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"renart/internal/web/model"
)

var seedFileTypes = []string{"csv", "parquet", "json", "jsonl", "ndjson", "avro"}

var creatableSeedAssetTypes = []pipeline.AssetType{
	pipeline.AssetTypeAthenaSeed,
	pipeline.AssetTypeBigquerySeed,
	pipeline.AssetTypeClickHouseSeed,
	pipeline.AssetTypeDatabricksSeed,
	pipeline.AssetTypeDorisSeed,
	pipeline.AssetTypeDuckDBSeed,
	pipeline.AssetTypeFabricSeed,
	pipeline.AssetTypeMsSQLSeed,
	pipeline.AssetTypeMySQLSeed,
	pipeline.AssetTypePostgresSeed,
	pipeline.AssetTypeRedshiftSeed,
	pipeline.AssetTypeSnowflakeSeed,
	pipeline.AssetTypeSynapseSeed,
	pipeline.AssetTypeVerticaSeed,
}

type sensorCapabilityDefinition struct {
	assetType       pipeline.AssetType
	variant         string
	connectionTypes []string
}

var creatableSensorAssetTypes = []sensorCapabilityDefinition{
	{pipeline.AssetTypeAthenaSQLSensor, "query", nil},
	{pipeline.AssetTypeAthenaTableSensor, "table", nil},
	{pipeline.AssetTypeBigqueryQuerySensor, "query", nil},
	{pipeline.AssetTypeBigqueryTableSensor, "table", nil},
	{pipeline.AssetTypeClickHouseQuerySensor, "query", nil},
	{pipeline.AssetTypeClickHouseTableSensor, "table", nil},
	{pipeline.AssetTypeDatabricksQuerySensor, "query", nil},
	{pipeline.AssetTypeDatabricksTableSensor, "table", nil},
	{pipeline.AssetTypeDorisQuerySensor, "query", nil},
	{pipeline.AssetTypeDorisTableSensor, "table", nil},
	{pipeline.AssetTypeDremioQuerySensor, "query", nil},
	{pipeline.AssetTypeDuckDBQuerySensor, "query", nil},
	{pipeline.AssetTypeFabricQuerySensor, "query", nil},
	{pipeline.AssetTypeFabricTableSensor, "table", nil},
	{pipeline.AssetTypeMsSQLQuerySensor, "query", nil},
	{pipeline.AssetTypeMsSQLTableSensor, "table", nil},
	{pipeline.AssetTypeMySQLQuerySensor, "query", nil},
	{pipeline.AssetTypeMySQLTableSensor, "table", nil},
	{pipeline.AssetTypePostgresQuerySensor, "query", nil},
	{pipeline.AssetTypePostgresTableSensor, "table", nil},
	{pipeline.AssetTypeRedshiftQuerySensor, "query", nil},
	{pipeline.AssetTypeRedshiftTableSensor, "table", nil},
	{pipeline.AssetTypeS3KeySensor, "key", []string{"aws", "s3"}},
	{pipeline.AssetTypeSailQuerySensor, "query", nil},
	{pipeline.AssetTypeSnowflakeQuerySensor, "query", nil},
	{pipeline.AssetTypeSnowflakeTableSensor, "table", nil},
	{pipeline.AssetTypeSynapseQuerySensor, "query", nil},
	{pipeline.AssetTypeSynapseTableSensor, "table", nil},
	{pipeline.AssetTypeTrinoQuerySensor, "query", nil},
	{pipeline.AssetTypeVerticaQuerySensor, "query", nil},
	{pipeline.AssetTypeVerticaTableSensor, "table", nil},
}

func assetAuthoringCapabilities() []model.AssetAuthoringCapability {
	capabilities := make([]model.AssetAuthoringCapability, 0, len(creatableSeedAssetTypes)+len(creatableSensorAssetTypes))
	for _, assetType := range creatableSeedAssetTypes {
		capabilities = append(capabilities, model.AssetAuthoringCapability{
			Type:               string(assetType),
			Kind:               "seed",
			Variant:            "file",
			ConnectionTypes:    connectionTypesForAssetType(assetType),
			RequiredParameters: []string{"path"},
			DefaultParameters:  map[string]string{"enforce_schema": "true"},
			FileTypes:          append([]string(nil), seedFileTypes...),
			SupportsUpload:     true,
			SupportsURL:        true,
		})
	}
	for _, definition := range creatableSensorAssetTypes {
		connectionTypes := append([]string(nil), definition.connectionTypes...)
		if len(connectionTypes) == 0 {
			connectionTypes = connectionTypesForAssetType(definition.assetType)
		}
		capabilities = append(capabilities, model.AssetAuthoringCapability{
			Type:               string(definition.assetType),
			Kind:               "sensor",
			Variant:            definition.variant,
			ConnectionTypes:    connectionTypes,
			RequiredParameters: sensorRequiredParameters(definition.variant),
			DefaultParameters: map[string]string{
				"poke_interval": "30",
				"timeout":       "24h",
			},
		})
	}
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i].Type < capabilities[j].Type })
	return capabilities
}

func connectionTypesForAssetType(assetType pipeline.AssetType) []string {
	connectionType := pipeline.AssetTypeConnectionMapping[assetType]
	if connectionType == "" {
		return []string{}
	}
	return []string{connectionType}
}

func sensorRequiredParameters(variant string) []string {
	switch variant {
	case "query":
		return []string{"query"}
	case "table":
		return []string{"table"}
	case "key":
		return []string{"bucket_name", "bucket_key"}
	default:
		return []string{}
	}
}

func isSensorAssetType(assetType pipeline.AssetType) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(string(assetType))), ".sensor.")
}
