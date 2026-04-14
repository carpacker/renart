import { SchemaColumn } from "@/lib/sql-schema";

import {
  DynamicAssetColumnObservation,
  DynamicRemoteDatabaseObservation,
  DynamicRemoteTableColumnObservation,
  DynamicRemoteTableObservation,
  DynamicSuggestionState,
  RegisterAssetColumnsPayload,
  RegisterConnectionTablesPayload,
  RemoteTableSuggestionEntry,
} from "./suggestion-types";

export function normalizeRegisteredColumns(
  columns: RegisterAssetColumnsPayload["columns"]
): SchemaColumn[] {
  return columns
    .map((column) => ({
      name: column.name,
      type: column.type,
      description: column.description,
      primaryKey: column.primaryKey,
    }))
    .filter((column) => column.name.trim().length > 0);
}

export function replaceAssetColumnObservation(
  observations: DynamicAssetColumnObservation[],
  nextObservation: DynamicAssetColumnObservation
): DynamicAssetColumnObservation[] {
  const signature = `${nextObservation.method}::${nextObservation.environment ?? ""}`;

  return [
    ...observations.filter(
      (observation) =>
        `${observation.method}::${observation.environment ?? ""}` !== signature
    ),
    nextObservation,
  ];
}

export function replaceConnectionTableObservation(
  observations: DynamicSuggestionState["remoteTablesByConnectionKey"][string],
  nextObservation: DynamicSuggestionState["remoteTablesByConnectionKey"][string][number]
): DynamicSuggestionState["remoteTablesByConnectionKey"][string] {
  const signature = `${nextObservation.method}::${
    nextObservation.environment ?? ""
  }::${nextObservation.databaseName ?? ""}::${nextObservation.prefix ?? ""}`;

  return [
    ...observations.filter(
      (observation) =>
        `${observation.method}::${observation.environment ?? ""}::${
          observation.databaseName ?? ""
        }::${observation.prefix ?? ""}` !== signature
    ),
    nextObservation,
  ];
}

export function replaceConnectionDatabaseObservation(
  observations: DynamicSuggestionState["remoteDatabasesByConnectionKey"][string],
  nextObservation: DynamicRemoteDatabaseObservation
): DynamicSuggestionState["remoteDatabasesByConnectionKey"][string] {
  const signature = `${nextObservation.environment ?? ""}`;

  return [
    ...observations.filter(
      (observation) => `${observation.environment ?? ""}` !== signature
    ),
    nextObservation,
  ];
}

export function replaceRemoteTableColumnObservation(
  observations: DynamicSuggestionState["remoteTableColumnsByTableKey"][string],
  nextObservation: DynamicRemoteTableColumnObservation
): DynamicSuggestionState["remoteTableColumnsByTableKey"][string] {
  const signature = `${nextObservation.environment ?? ""}`;

  return [
    ...observations.filter(
      (observation) => `${observation.environment ?? ""}` !== signature
    ),
    nextObservation,
  ];
}

export function normalizeRegisteredRemoteTables(
  tables: RegisterConnectionTablesPayload["tables"]
): RemoteTableSuggestionEntry[] {
  return tables
    .map((table) => ({
      name: table.name.trim(),
      schemaName: table.schemaName,
      databaseName: table.databaseName,
      kind: table.kind,
      detail: table.detail,
    }))
    .filter((table) => table.name.length > 0);
}

export function mergeRemoteTableEntries(
  left: RemoteTableSuggestionEntry[],
  right: RemoteTableSuggestionEntry[]
): RemoteTableSuggestionEntry[] {
  const merged = new Map<string, RemoteTableSuggestionEntry>();

  for (const item of [...left, ...right]) {
    const key = item.name.toLowerCase();
    const current = merged.get(key);

    if (!current) {
      merged.set(key, item);
      continue;
    }

    merged.set(key, {
      name: current.name || item.name,
      schemaName: current.schemaName ?? item.schemaName,
      databaseName: current.databaseName ?? item.databaseName,
      kind: current.kind ?? item.kind,
      detail: current.detail ?? item.detail,
    });
  }

  return Array.from(merged.values()).sort((left, right) =>
    left.name.localeCompare(right.name)
  );
}
