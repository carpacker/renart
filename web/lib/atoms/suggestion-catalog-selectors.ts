import { resolveConnection, SchemaTable } from "@/lib/sql-schema";
import { IngestrSuggestion, WebAsset, WorkspaceState } from "@/lib/types";

import {
  ConnectionSuggestionEntry,
  SuggestionCatalogState,
  SuggestionTableState,
} from "./suggestion-types";

export function mergeRemoteSuggestions(
  left: IngestrSuggestion[],
  right: IngestrSuggestion[]
): IngestrSuggestion[] {
  const merged = new Map<string, IngestrSuggestion>();

  for (const item of [...left, ...right]) {
    const key = normalizeTableName(item.value).toLowerCase();
    const current = merged.get(key);

    if (!current) {
      merged.set(key, item);
      continue;
    }

    merged.set(key, {
      value: current.value || item.value,
      kind: current.kind ?? item.kind,
      detail: current.detail ?? item.detail,
    });
  }

  return Array.from(merged.values()).sort((a, b) => a.value.localeCompare(b.value));
}

export function getConnectionSuggestions(
  catalog: SuggestionCatalogState
): ConnectionSuggestionEntry[] {
  return catalog.connections.map((connection) => ({
    name: connection.name,
    type: connection.type,
    databaseName: connection.databaseName,
  }));
}

export function getDatabaseSuggestions(
  catalog: SuggestionCatalogState,
  options: {
    connectionName: string;
    environment?: string;
    prefix?: string;
  }
): string[] {
  const prefix = options.prefix?.trim().toLowerCase() ?? "";

  return catalog.databases
    .filter((database) => {
      if (database.connectionName !== options.connectionName) {
        return false;
      }

      const matchingSource = database.sources.some(
        (source) =>
          source.method === "connection-database-discovery" &&
          (!options.environment || source.environment === options.environment)
      );
      if (!matchingSource) {
        return false;
      }

      if (!prefix) {
        return true;
      }

      return database.name.toLowerCase().includes(prefix);
    })
    .map((database) => database.name)
    .sort((left, right) => left.localeCompare(right));
}

export function getSelectedAssetSuggestionTable(
  catalog: SuggestionCatalogState,
  assetId: string | null | undefined
): SuggestionTableState | null {
  if (!assetId) {
    return null;
  }

  return catalog.tables.find((table) => table.assetId === assetId) ?? null;
}

export function getSelectedAssetColumnEntries(
  table: SuggestionTableState | null
): Array<{ name?: string }> {
  return (table?.columns ?? []).map((column) => ({ name: column.name }));
}

export function getSelectedAssetInspectColumns(
  table: SuggestionTableState | null
): string[] {
  if (!table) {
    return [];
  }

  return table.columns
    .filter((column) => column.sourceMethods.includes("asset-inspect"))
    .map((column) => column.name);
}

export function getSchemaSuggestionTablesForAsset(
  workspace: WorkspaceState | null,
  catalog: SuggestionCatalogState,
  asset: WebAsset | null
): SuggestionTableState[] {
  if (!workspace || !asset) {
    return [];
  }

  const currentConnection = resolveConnection(asset, workspace.connections ?? {});
  if (!currentConnection) {
    return [];
  }

  return catalog.tables.filter(
    (table) => table.connectionName === currentConnection
  );
}

export function toSchemaTables(tables: SuggestionTableState[]): SchemaTable[] {
  return tables.map((table) => ({
    name: table.name,
    shortName: table.shortName,
    columns: table.columns.map((column) => ({
      name: column.name,
      type: column.type,
      description: column.description,
      primaryKey: column.primaryKey,
    })),
    isBruinAsset: table.isBruinAsset,
    assetId: table.assetId,
    pipelineId: table.pipelineId,
    assetPath: table.assetPath,
    connectionName: table.connectionName ?? undefined,
    connectionType: table.connectionType ?? undefined,
    databaseName: table.databaseName ?? undefined,
    sourceMethods: table.sourceMethods,
  }));
}

export function getIngestrTableSuggestionsFromCatalog(
  catalog: SuggestionCatalogState,
  options: {
    connectionName: string;
    environment?: string;
    prefix?: string;
  }
): IngestrSuggestion[] {
  const prefix = options.prefix?.trim().toLowerCase() ?? "";

  return catalog.tables
    .filter((table) => {
      if (table.isBruinAsset || table.connectionName !== options.connectionName) {
        return false;
      }

      const matchingSource = table.sources.some(
        (source) =>
          source.method === "ingestr-suggestions" &&
          (!options.environment || source.environment === options.environment) &&
          doesIngestrSourceMatchPrefix(source.prefix, prefix)
      );

      if (!matchingSource) {
        return false;
      }

      if (!prefix) {
        return true;
      }

      return table.name.toLowerCase().includes(prefix);
    })
    .map((table) => ({
      value: table.name,
      kind: table.remoteSuggestionKind,
      detail: table.remoteSuggestionDetail,
    }))
    .sort((left, right) => left.value.localeCompare(right.value));
}

function normalizeTableName(value: string): string {
  return value.trim().replace(/^['"`]+|['"`]+$/g, "");
}

function doesIngestrSourceMatchPrefix(
  sourcePrefix: string | undefined,
  requestedPrefix: string
): boolean {
  const normalizedSourcePrefix = sourcePrefix?.trim().toLowerCase() ?? "";

  if (requestedPrefix === "") {
    return normalizedSourcePrefix === "";
  }

  if (normalizedSourcePrefix === requestedPrefix) {
    return true;
  }

  if (requestedPrefix.endsWith("/")) {
    return false;
  }

  return (
    normalizedSourcePrefix.endsWith("/") &&
    requestedPrefix.startsWith(normalizedSourcePrefix)
  );
}
