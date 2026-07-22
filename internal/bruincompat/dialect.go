package bruincompat

import (
	"fmt"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

var assetTypeDialects = map[pipeline.AssetType]string{
	pipeline.AssetTypeBigqueryQuery:     "bigquery",
	pipeline.AssetTypeSnowflakeQuery:    "snowflake",
	pipeline.AssetTypePostgresQuery:     "postgres",
	pipeline.AssetTypeMySQLQuery:        "mysql",
	pipeline.AssetTypeDorisQuery:        "doris",
	pipeline.AssetTypeStarRocksQuery:    "starrocks",
	pipeline.AssetTypeRedshiftQuery:     "redshift",
	pipeline.AssetTypeAthenaQuery:       "athena",
	pipeline.AssetTypeTrinoQuery:        "trino",
	pipeline.AssetTypeDremioQuery:       "trino",
	pipeline.AssetTypeSailQuery:         "trino",
	pipeline.AssetTypeClickHouse:        "clickhouse",
	pipeline.AssetTypeDatabricksQuery:   "databricks",
	pipeline.AssetTypeMsSQLQuery:        "tsql",
	pipeline.AssetTypeSynapseQuery:      "tsql",
	pipeline.AssetTypeDuckDBQuery:       "duckdb",
	pipeline.AssetTypeMotherduckQuery:   "duckdb",
	pipeline.AssetTypeOracleQuery:       "oracle",
	pipeline.AssetTypeFabricQuery:       "fabric",
	pipeline.AssetTypeFabricQueryLegacy: "fabric",
	pipeline.AssetTypeVerticaQuery:      "postgres",
}

// AssetTypeToDialect mirrors Bruin's asset-to-parser dialect mapping without
// importing pkg/sqlparser, whose package-level CGo flags require its Rust
// static library even when RustSQLParser is never constructed.
func AssetTypeToDialect(assetType pipeline.AssetType) (string, error) {
	dialect, ok := assetTypeDialects[assetType]
	if !ok {
		return "", fmt.Errorf("unsupported asset type %s", assetType)
	}
	return dialect, nil
}
