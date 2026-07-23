import { WebAsset, WebColumn, WorkspaceState } from "@/lib/types";

/**
 * A table known to the SQL editor for autocompletion and go-to-definition.
 *
 * `isWorkspaceAsset` marks tables derived from pipeline assets on the same connection
 * so they rank higher in completion results.
 */
export type SchemaTable = {
  /** Display name used for completion (e.g. "my_schema.customers"). */
  name: string;
  /** Short unqualified name (last segment after the last dot). */
  shortName: string;
  /** Column definitions when available. */
  columns: SchemaColumn[];
  /** True when the table originates from a workspace asset (priority source). */
  isWorkspaceAsset: boolean;
  /** True when the asset is known to have been materialized before. */
  isMaterialized?: boolean;
  /** The asset id — only present for workspace asset tables. */
  assetId?: string;
  /** The pipeline id that owns this asset. */
  pipelineId?: string;
  /** Source asset file path, useful for definition hints. */
  assetPath?: string;
  /** Effective workspace connection name when known. */
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
  sourceMethods?: string[];
};

/**
 * Return the effective connection resolved by the backend after applying
 * pipeline defaults. Never infer one from the asset type: multiple connections
 * can share a platform and only the backend has the pipeline/environment
 * context needed to choose between them.
 */
export function effectiveConnectionForAsset(asset: Pick<WebAsset, "connection">): string | null {
  return asset.connection?.trim() || null;
}

/**
 * Resolve the platform type for an asset's effective connection. This keeps
 * editor behavior tied to the backend-selected connection instead of guessing
 * from the asset type.
 */
export function effectiveConnectionTypeForAsset(
  workspace: Pick<WorkspaceState, "connections">,
  asset: Pick<WebAsset, "connection">,
): string | null {
  const connectionName = effectiveConnectionForAsset(asset);
  if (!connectionName) {
    return null;
  }
  return workspace.connections?.[connectionName]?.trim().toLowerCase() || null;
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
    sourceMethods: ["workspace-load"],
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
  const currentConnection = effectiveConnectionForAsset(currentAsset);

  const tables: SchemaTable[] = [];
  const seen = new Set<string>();

  for (const pipeline of workspace.pipelines) {
    for (const asset of pipeline.assets) {
      const assetConnection = effectiveConnectionForAsset(asset);

      // Skip assets on a different or unresolved connection.
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
        isWorkspaceAsset: true,
        isMaterialized: asset.is_materialized,
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
