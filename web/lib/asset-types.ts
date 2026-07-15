import type { AssetAuthoringCapability, WorkspaceConfigConnectionType } from "@/lib/types";

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

export const SEED_ASSET_TYPES = [
  "athena.seed",
  "bq.seed",
  "clickhouse.seed",
  "databricks.seed",
  "doris.seed",
  "duckdb.seed",
  "fabric.seed",
  "fw.seed",
  "ms.seed",
  "my.seed",
  "pg.seed",
  "rs.seed",
  "sf.seed",
  "synapse.seed",
  "vertica.seed",
] as const;

export const NON_SQL_ASSET_TYPES = ["python", "ingestr", "load", "api", "r"] as const;

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
  ]),
) as Record<string, string>;

export function getAvailableAssetTypes(
  connectionTypes: WorkspaceConfigConnectionType[],
  assetCapabilities: AssetAuthoringCapability[] = [],
): string[] {
  const mappedSqlTypes = connectionTypes
    .map((connectionType) => CONNECTION_TYPE_TO_ASSET_TYPE[connectionType.type_name])
    .filter((value): value is string => Boolean(value));

  return Array.from(
    new Set([
      ...mappedSqlTypes,
      ...SQL_ASSET_TYPES,
      ...SEED_ASSET_TYPES,
      ...NON_SQL_ASSET_TYPES,
      ...assetCapabilities.map((capability) => capability.type),
    ]),
  ).sort((left, right) => left.localeCompare(right));
}

export function isSqlAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".sql");
}

export function isSeedAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".seed");
}

export function isSensorAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().includes(".sensor.");
}

export type AssetColumnRefreshMode = "none" | "api" | "definition" | "materialized";

/**
 * Select the authoritative column-inference source for an asset kind. SQL,
 * Load, and local seeds have enough definition-time context; executable
 * non-SQL assets and URL seeds need their materialized output instead. Sensors
 * gate execution and do not produce a relation at all.
 */
export function getAssetColumnRefreshMode(
  assetType?: string | null,
  parameters?: Record<string, string> | null,
): AssetColumnRefreshMode {
  const normalized = (assetType ?? "").trim().toLowerCase();
  if (isSensorAssetType(normalized)) return "none";
  if (normalized === "api") return "api";
  if (isSqlAssetType(normalized) || normalized === "load") return "definition";
  if (isSeedAssetType(normalized)) {
    return /^https?:\/\//i.test(parameters?.path?.trim() ?? "") ? "materialized" : "definition";
  }
  return "materialized";
}

export function getAssetAuthoringCapability(
  assetType: string | null | undefined,
  capabilities: AssetAuthoringCapability[] | null | undefined,
) {
  const normalized = (assetType ?? "").trim().toLowerCase();
  return (capabilities ?? []).find(
    (capability) => capability.type.trim().toLowerCase() === normalized,
  );
}

export function groupAssetTypesByKind(assetTypes: string[]) {
  const uniqueTypes = Array.from(
    new Set(assetTypes.map((type) => type.trim()).filter(Boolean)),
  ).sort((left, right) => left.localeCompare(right));

  return {
    sql: uniqueTypes.filter((type) => isSqlAssetType(type)),
    seed: uniqueTypes.filter((type) => isSeedAssetType(type)),
    sensor: uniqueTypes.filter((type) => isSensorAssetType(type)),
    nonSql: uniqueTypes.filter(
      (type) => !isSqlAssetType(type) && !isSeedAssetType(type) && !isSensorAssetType(type),
    ),
  };
}

export function getConnectionTypeForAssetType(assetType?: string | null) {
  return ASSET_TYPE_TO_CONNECTION_TYPE[(assetType ?? "").trim().toLowerCase()] ?? null;
}

export function getConfiguredConnectionTypes(connections?: Record<string, string> | null) {
  return new Set(
    Object.values(connections ?? {})
      .map((value) => value.trim())
      .filter(Boolean),
  );
}

export function getPreferredSqlAssetType(connections?: Record<string, string> | null) {
  const configuredConnectionTypes = getConfiguredConnectionTypes(connections);
  for (const connectionType of configuredConnectionTypes) {
    const assetType = CONNECTION_TYPE_TO_ASSET_TYPE[connectionType];
    if (assetType) {
      return assetType;
    }
  }

  return "duckdb.sql";
}

export function groupAssetTypesByConfiguredConnections(
  assetTypes: string[],
  connections?: Record<string, string> | null,
) {
  const configuredConnectionTypes = getConfiguredConnectionTypes(connections);
  const configured: string[] = [];
  const notConfigured: string[] = [];
  const other: string[] = [];

  for (const assetType of assetTypes) {
    const connectionType = getConnectionTypeForAssetType(assetType);
    if (!connectionType) {
      other.push(assetType);
      continue;
    }
    if (configuredConnectionTypes.has(connectionType)) {
      configured.push(assetType);
      continue;
    }
    notConfigured.push(assetType);
  }

  return { configured, notConfigured, other };
}
