import { WorkspaceConfigConnectionType } from "@/lib/types";

export const SQL_ASSET_TYPES = [
  "athena.sql",
  "bq.sql",
  "clickhouse.sql",
  "databricks.sql",
  "duckdb.sql",
  "fabric.sql",
  "fw.sql",
  "motherduck.sql",
  "ms.sql",
  "my.sql",
  "oracle.sql",
  "pg.sql",
  "rs.sql",
  "sf.sql",
  "synapse.sql",
  "trino.sql",
  "vertica.sql",
] as const;

export const NON_SQL_ASSET_TYPES = ["python", "ingestr", "r"] as const;

const CONNECTION_TYPE_TO_ASSET_TYPE: Record<string, string> = {
  athena: "athena.sql",
  clickhouse: "clickhouse.sql",
  databricks: "databricks.sql",
  duckdb: "duckdb.sql",
  fabric: "fabric.sql",
  google_cloud_platform: "bq.sql",
  motherduck: "motherduck.sql",
  mssql: "ms.sql",
  mysql: "my.sql",
  oracle: "oracle.sql",
  postgres: "pg.sql",
  redshift: "rs.sql",
  snowflake: "sf.sql",
  synapse: "synapse.sql",
  trino: "trino.sql",
  vertica: "vertica.sql",
};

const ASSET_TYPE_TO_CONNECTION_TYPE = Object.fromEntries(
  Object.entries(CONNECTION_TYPE_TO_ASSET_TYPE).map(([connectionType, assetType]) => [
    assetType,
    connectionType,
  ])
) as Record<string, string>;

export function getAvailableAssetTypes(
  connectionTypes: WorkspaceConfigConnectionType[]
): string[] {
  const mappedSqlTypes = connectionTypes
    .map((connectionType) => CONNECTION_TYPE_TO_ASSET_TYPE[connectionType.type_name])
    .filter((value): value is string => Boolean(value));

  return Array.from(
    new Set([...mappedSqlTypes, ...SQL_ASSET_TYPES, ...NON_SQL_ASSET_TYPES])
  ).sort((left, right) => left.localeCompare(right));
}

export function isSqlAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".sql");
}

export function getConnectionTypeForAssetType(assetType?: string | null) {
  return ASSET_TYPE_TO_CONNECTION_TYPE[(assetType ?? "").trim().toLowerCase()] ?? null;
}
