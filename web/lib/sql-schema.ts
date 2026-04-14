import { WebAsset, WebColumn, WorkspaceState } from "@/lib/types";

/**
 * A table known to the SQL editor for autocompletion and go-to-definition.
 *
 * `isBruinAsset` marks tables derived from Bruin assets on the same connection
 * so they rank higher in completion results.
 */
export type SchemaTable = {
  /** Display name used for completion (e.g. "my_schema.customers"). */
  name: string;
  /** Short unqualified name (last segment after the last dot). */
  shortName: string;
  /** Column definitions when available. */
  columns: SchemaColumn[];
  /** True when the table originates from a Bruin asset (priority source). */
  isBruinAsset: boolean;
  /** The asset id — only present for Bruin asset tables. */
  assetId?: string;
  /** The pipeline id that owns this asset. */
  pipelineId?: string;
  /** Source asset file path, useful for definition hints. */
  assetPath?: string;
  /** Resolved Bruin connection name when known. */
  connectionName?: string;
  /** Resolved connection platform type when known. */
  connectionType?: string;
  /** Parsed database/catalog name when known. */
  databaseName?: string;
  /** High-level provenance methods that contributed this table. */
  sourceMethods?: string[];
};

export type SchemaColumn = {
  name: string;
  type?: string;
  description?: string;
  primaryKey?: boolean;
};

/** SQL asset type prefixes that target a specific connection platform. */
const SQL_TYPE_PREFIXES: Record<string, string> = {
  "bq.sql": "google_cloud_platform",
  "sf.sql": "snowflake",
  "pg.sql": "postgres",
  "ms.sql": "mssql",
  "my.sql": "mysql",
  "rs.sql": "redshift",
  "duckdb.sql": "duckdb",
  "clickhouse.sql": "clickhouse",
  "databricks.sql": "databricks",
  "synapse.sql": "synapse",
  "fabric.sql": "fabric",
  "fw.sql": "fabric",
  "athena.sql": "athena",
  "trino.sql": "trino",
  "motherduck.sql": "motherduck",
  "oracle.sql": "oracle",
};

/**
 * Derive the "platform family" for an asset type so we can decide which assets
 * share a connection namespace.  Returns `null` for non-SQL types.
 */
export function platformForAssetType(assetType: string): string | null {
  const lower = assetType.toLowerCase();
  return SQL_TYPE_PREFIXES[lower] ?? null;
}

/**
 * Resolve the effective connection name for an asset.
 *
 * If the asset has an explicit `connection` field we use that.  Otherwise we
 * fall back to the platform-default connection.
 */
export function resolveConnection(
  asset: WebAsset,
  connections: Record<string, string>
): string | null {
  if (asset.connection) {
    return asset.connection;
  }

  const platform = platformForAssetType(asset.type);
  if (!platform) {
    return null;
  }

  // Find the first connection whose *type* matches the platform.
  for (const [name, type] of Object.entries(connections)) {
    if (type === platform) {
      return name;
    }
  }

  return null;
}

function toSchemaColumns(columns?: WebColumn[]): SchemaColumn[] {
  if (!columns || columns.length === 0) {
    return [];
  }

  return columns.map((column) => ({
    name: column.name,
    type: column.type,
    description: column.description,
    primaryKey: column.primary_key,
  }));
}

export function parseQualifiedTableName(name: string): {
  shortName: string;
  schemaName?: string;
  databaseName?: string;
} {
  const parts = name
    .split(".")
    .map((part) => part.trim().replace(/^['"`]+|['"`]+$/g, ""))
    .filter(Boolean);

  if (parts.length === 0) {
    return { shortName: name };
  }

  const shortName = parts[parts.length - 1];
  const schemaName = parts.length >= 2 ? parts[parts.length - 2] : undefined;
  const databaseName = parts.length >= 3 ? parts[parts.length - 3] : undefined;

  return {
    shortName,
    schemaName,
    databaseName,
  };
}

/**
 * Build the full schema registry for a given asset's SQL editor.
 *
 * Tables are scoped to the same connection as `currentAsset`, so an asset
 * writing to a DuckDB connection only sees other DuckDB tables.
 */
export function buildSchemaForAsset(
  workspace: WorkspaceState,
  currentAsset: WebAsset,
): SchemaTable[] {
  const connections = workspace.connections ?? {};
  const currentConnection = resolveConnection(currentAsset, connections);

  const tables: SchemaTable[] = [];
  const seen = new Set<string>();

  for (const pipeline of workspace.pipelines) {
    for (const asset of pipeline.assets) {
      const assetConnection = resolveConnection(asset, connections);

      // Skip assets on a different connection (or non-SQL assets).
      if (!assetConnection || assetConnection !== currentConnection) {
        continue;
      }

      const name = asset.name;
      if (!name || seen.has(name.toLowerCase())) {
        continue;
      }
      seen.add(name.toLowerCase());

      const tableParts = parseQualifiedTableName(name);

      tables.push({
        name,
        shortName: tableParts.shortName,
        columns: toSchemaColumns(asset.columns),
        isBruinAsset: true,
        assetId: asset.id,
        pipelineId: pipeline.id,
        assetPath: asset.path,
        connectionName: assetConnection,
        connectionType: connections[assetConnection],
        databaseName: tableParts.databaseName,
        sourceMethods: ["workspace-load"],
      });
    }
  }

  return tables;
}

/**
 * Find the asset table whose name matches a SQL identifier (case-insensitive).
 *
 * Supports matching on the full qualified name (`schema.table`) or the short
 * unqualified name (`table`).  When multiple tables share the same short name
 * we prefer an exact full-name match.
 */
export function findTableByIdentifier(
  tables: SchemaTable[],
  identifier: string,
): SchemaTable | undefined {
  const lower = identifier.toLowerCase();

  // 1. Exact full-name match.
  const exact = tables.find((table) => table.name.toLowerCase() === lower);
  if (exact) {
    return exact;
  }

  // 2. Short-name match.
  return tables.find((table) => table.shortName.toLowerCase() === lower);
}
