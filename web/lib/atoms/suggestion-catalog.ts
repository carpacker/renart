import { parseQualifiedTableName, resolveConnection, SchemaColumn } from "@/lib/sql-schema";
import { WebAsset, WebColumn, WorkspaceState } from "@/lib/types";

import { MaterializationByAssetId } from "./materialization";
import {
  ConnectionSuggestionEntry,
  DynamicSuggestionState,
  SuggestionCatalogState,
  SuggestionColumnState,
  SuggestionConnectionState,
  SuggestionDatabaseState,
  SuggestionObservation,
  SuggestionObservationMethod,
  SuggestionTableState,
  SuggestionWorkspaceSyncSource,
} from "./suggestion-types";
export {
  getConnectionSuggestions,
  getDatabaseSuggestions,
  getIngestrTableSuggestionsFromCatalog,
  getSchemaSuggestionTablesForAsset,
  getSelectedAssetColumnEntries,
  getSelectedAssetInspectColumns,
  getSelectedAssetSuggestionTable,
  mergeRemoteSuggestions,
  toSchemaTables,
} from "./suggestion-catalog-selectors";

const SOURCE_PRIORITY: Record<string, number> = {
  postgres: 0,
  duckdb: 1,
  s3: 2,
};

function normalizeTableName(value: string): string {
  return value.trim().replace(/^['"`]+|['"`]+$/g, "");
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

function extractSQLDefinitionColumns(content?: string): SchemaColumn[] {
  const sql = content?.trim();
  if (!sql) {
    return [];
  }

  const selectIndex = findTopLevelKeyword(sql, "select", 0);
  if (selectIndex < 0) {
    return [];
  }

  const fromIndex = findTopLevelKeyword(sql, "from", selectIndex + "select".length);
  const selectListEnd = fromIndex < 0 ? sql.length : fromIndex;
  const selectList = sql.slice(selectIndex + "select".length, selectListEnd);
  const columns: SchemaColumn[] = [];
  const seen = new Set<string>();

  for (const expression of splitTopLevelComma(selectList)) {
    const columnName = extractSelectExpressionColumnName(expression);
    if (!columnName || seen.has(columnName.toLowerCase())) {
      continue;
    }
    seen.add(columnName.toLowerCase());
    columns.push({ name: columnName });
  }

  return columns;
}

function findTopLevelKeyword(sql: string, keyword: string, startIndex: number) {
  const lower = sql.toLowerCase();
  let depth = 0;
  let quote: string | null = null;

  for (let index = startIndex; index < sql.length; index++) {
    const char = sql[index];
    if (quote) {
      if (char === quote) {
        quote = null;
      }
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      quote = char;
      continue;
    }
    if (char === "(") {
      depth++;
      continue;
    }
    if (char === ")") {
      depth = Math.max(0, depth - 1);
      continue;
    }
    if (depth !== 0 || !lower.startsWith(keyword, index)) {
      continue;
    }
    const before = index > 0 ? lower[index - 1] : " ";
    const after = lower[index + keyword.length] ?? " ";
    if (/\w/.test(before) || /\w/.test(after)) {
      continue;
    }
    return index;
  }

  return -1;
}

function splitTopLevelComma(value: string) {
  const result: string[] = [];
  let depth = 0;
  let quote: string | null = null;
  let start = 0;

  for (let index = 0; index < value.length; index++) {
    const char = value[index];
    if (quote) {
      if (char === quote) {
        quote = null;
      }
      continue;
    }
    if (char === "'" || char === '"' || char === "`") {
      quote = char;
      continue;
    }
    if (char === "(") {
      depth++;
      continue;
    }
    if (char === ")") {
      depth = Math.max(0, depth - 1);
      continue;
    }
    if (char === "," && depth === 0) {
      result.push(value.slice(start, index).trim());
      start = index + 1;
    }
  }

  result.push(value.slice(start).trim());
  return result.filter(Boolean);
}

function extractSelectExpressionColumnName(expression: string) {
  if (expression === "*") {
    return null;
  }

  const aliasMatch = expression.match(/\bas\s+([a-zA-Z_][\w$]*|"[^"]+"|`[^`]+`)\s*$/i);
  if (aliasMatch) {
    return normalizeColumnIdentifier(aliasMatch[1]);
  }

  const directColumnMatch = expression.match(/^(?:[a-zA-Z_][\w$]*\.)?([a-zA-Z_][\w$]*)$/);
  if (directColumnMatch) {
    return normalizeColumnIdentifier(directColumnMatch[1]);
  }

  return null;
}

function normalizeColumnIdentifier(value: string) {
  return value.trim().replace(/^["`]+|["`]+$/g, "");
}

function connectionKey(name: string, databaseName?: string | null): string {
  return `${name.toLowerCase()}::${(databaseName ?? "").toLowerCase()}`;
}

function assetTableKey(assetId: string, pipelineId?: string): string {
  return `asset::${pipelineId ?? ""}::${assetId}`;
}

function databaseKey(connectionName: string, databaseName: string): string {
  return `${connectionName.toLowerCase()}::${databaseName.toLowerCase()}`;
}

function externalTableKey(
  connectionName: string,
  tableName: string,
  databaseName?: string | null
): string {
  return `external::${connectionKey(
    connectionName,
    databaseName
  )}::${normalizeTableName(tableName).toLowerCase()}`;
}

function columnKey(tableKey: string, name: string): string {
  return `${tableKey}::${name.toLowerCase()}`;
}

function sourceSignature(source: SuggestionObservation): string {
  return [
    source.method,
    source.prefix ?? "",
    source.pipelineId ?? "",
    source.assetId ?? "",
    source.connectionName ?? "",
    source.connectionType ?? "",
    source.databaseName ?? "",
    source.environment ?? "",
    String(source.workspaceRevision ?? ""),
    source.workspaceEventType ?? "",
    source.workspaceEventPath ?? "",
    source.workspaceLite ? "1" : "0",
  ].join("|");
}

function addSource(
  existing: SuggestionObservation[],
  source: SuggestionObservation
): SuggestionObservation[] {
  const signature = sourceSignature(source);
  if (existing.some((item) => sourceSignature(item) === signature)) {
    return existing;
  }

  return [...existing, source];
}

function addSourceMethod(
  existing: SuggestionObservationMethod[],
  method: SuggestionObservationMethod
): SuggestionObservationMethod[] {
  return existing.includes(method) ? existing : [...existing, method];
}

function mergeColumnValue<T>(
  current: T | undefined,
  next: T | undefined
): T | undefined {
  return current === undefined || current === null || current === ("" as T)
    ? next
    : current;
}

function sortColumns(columns: SuggestionColumnState[]): SuggestionColumnState[] {
  return [...columns].sort((left, right) => left.name.localeCompare(right.name));
}

function sortTables(tables: SuggestionTableState[]): SuggestionTableState[] {
  return [...tables].sort((left, right) => left.name.localeCompare(right.name));
}

function sortConnections(
  connections: SuggestionConnectionState[]
): SuggestionConnectionState[] {
  return [...connections].sort((left, right) => {
    const leftPriority = SOURCE_PRIORITY[left.type] ?? 99;
    const rightPriority = SOURCE_PRIORITY[right.type] ?? 99;

    if (leftPriority !== rightPriority) {
      return leftPriority - rightPriority;
    }

    return left.name.localeCompare(right.name);
  });
}

function buildWorkspaceObservation(
  recordedAt: string,
  options: {
    method: SuggestionObservationMethod;
    pipelineId?: string;
    assetId?: string;
    assetPath?: string;
    connectionName?: string | null;
    connectionType?: string | null;
    databaseName?: string | null;
    workspaceRevision?: number;
    workspaceEventType?: string;
    workspaceEventPath?: string;
    workspaceLite?: boolean;
  }
): SuggestionObservation {
  return {
    method: options.method,
    recordedAt,
    pipelineId: options.pipelineId,
    assetId: options.assetId,
    assetPath: options.assetPath,
    connectionName: options.connectionName,
    connectionType: options.connectionType,
    databaseName: options.databaseName,
    workspaceRevision: options.workspaceRevision,
    workspaceEventType: options.workspaceEventType,
    workspaceEventPath: options.workspaceEventPath,
    workspaceLite: options.workspaceLite,
  };
}

function upsertConnection(
  connections: Map<string, SuggestionConnectionState>,
  input: ConnectionSuggestionEntry,
  source: SuggestionObservation
) {
  const key = connectionKey(input.name, input.databaseName);
  const current = connections.get(key);

  if (!current) {
    connections.set(key, {
      key,
      name: input.name,
      type: input.type,
      databaseName: input.databaseName,
      sourceMethods: [source.method],
      sources: [source],
    });
    return;
  }

  connections.set(key, {
    ...current,
    type: current.type || input.type,
    databaseName: current.databaseName ?? input.databaseName,
    sourceMethods: addSourceMethod(current.sourceMethods, source.method),
    sources: addSource(current.sources, source),
  });
}

function upsertDatabase(
  databases: Map<string, SuggestionDatabaseState>,
  input: {
    name: string;
    connectionName: string;
    connectionType?: string | null;
  },
  source: SuggestionObservation
) {
  const normalizedName = input.name.trim();
  if (!normalizedName) {
    return;
  }

  const key = databaseKey(input.connectionName, normalizedName);
  const current = databases.get(key);
  const next: SuggestionDatabaseState = current
    ? {
        ...current,
        connectionType: current.connectionType ?? input.connectionType,
        sourceMethods: addSourceMethod(current.sourceMethods, source.method),
        sources: addSource(current.sources, source),
      }
    : {
        key,
        name: normalizedName,
        connectionKey: connectionKey(input.connectionName, null),
        connectionName: input.connectionName,
        connectionType: input.connectionType,
        sourceMethods: [source.method],
        sources: [source],
      };

  databases.set(key, next);
}

function upsertTable(
  tables: Map<string, SuggestionTableState>,
  input: Omit<SuggestionTableState, "sourceMethods" | "sources" | "columns">,
  source: SuggestionObservation
): SuggestionTableState {
  const current = tables.get(input.key);

  if (!current) {
    const created: SuggestionTableState = {
      ...input,
      sourceMethods: [source.method],
      sources: [source],
      columns: [],
    };
    tables.set(input.key, created);
    return created;
  }

  const next: SuggestionTableState = {
    ...current,
    shortName: current.shortName || input.shortName,
    schemaName: current.schemaName ?? input.schemaName,
    databaseName: current.databaseName ?? input.databaseName,
    connectionKey: current.connectionKey ?? input.connectionKey,
    connectionName: current.connectionName ?? input.connectionName,
    connectionType: current.connectionType ?? input.connectionType,
    pipelineId: current.pipelineId ?? input.pipelineId,
    assetId: current.assetId ?? input.assetId,
    assetPath: current.assetPath ?? input.assetPath,
    isBruinAsset: current.isBruinAsset || input.isBruinAsset,
    remoteSuggestionKind:
      current.remoteSuggestionKind ?? input.remoteSuggestionKind,
    remoteSuggestionDetail:
      current.remoteSuggestionDetail ?? input.remoteSuggestionDetail,
    sourceMethods: addSourceMethod(current.sourceMethods, source.method),
    sources: addSource(current.sources, source),
    columns: current.columns,
  };

  tables.set(input.key, next);
  return next;
}

function upsertColumns(
  tables: Map<string, SuggestionTableState>,
  tableKey: string,
  columns: SchemaColumn[],
  source: SuggestionObservation
) {
  const table = tables.get(tableKey);
  if (!table || columns.length === 0) {
    return;
  }

  const columnsByKey = new Map(
    table.columns.map((column) => [column.key, column] as const)
  );

  for (const column of columns) {
    const key = columnKey(tableKey, column.name);
    const current = columnsByKey.get(key);
    if (!current) {
      columnsByKey.set(key, {
        key,
        name: column.name,
        type: column.type,
        description: column.description,
        primaryKey: column.primaryKey,
        tableKey,
        sourceMethods: [source.method],
        sources: [source],
      });
      continue;
    }

    columnsByKey.set(key, {
      ...current,
      type: mergeColumnValue(current.type, column.type),
      description: mergeColumnValue(current.description, column.description),
      primaryKey: current.primaryKey ?? column.primaryKey,
      sourceMethods: addSourceMethod(current.sourceMethods, source.method),
      sources: addSource(current.sources, source),
    });
  }

  tables.set(tableKey, {
    ...table,
    columns: sortColumns(Array.from(columnsByKey.values())),
  });
}

function emptyCatalog(): SuggestionCatalogState {
  return {
    connections: [],
    connectionsByKey: {},
    databases: [],
    databasesByKey: {},
    tables: [],
    tablesByKey: {},
  };
}

function buildCatalogFromWorkspace(
  workspace: WorkspaceState | null,
  syncSource: SuggestionWorkspaceSyncSource | null,
  materializationByAssetId: MaterializationByAssetId = {}
): SuggestionCatalogState {
  if (!workspace) {
    return emptyCatalog();
  }

  const connections = new Map<string, SuggestionConnectionState>();
  const databases = new Map<string, SuggestionDatabaseState>();
  const tables = new Map<string, SuggestionTableState>();
  const recordedAt = syncSource?.recordedAt ?? new Date(0).toISOString();
  const method = syncSource?.method ?? "workspace-load";
  const workspaceRevision = workspace.revision;

  for (const [name, type] of Object.entries(workspace.connections ?? {})) {
    upsertConnection(
      connections,
      { name, type },
      buildWorkspaceObservation(recordedAt, {
        method,
        connectionName: name,
        connectionType: type,
        workspaceRevision,
        workspaceEventType: syncSource?.eventType,
        workspaceEventPath: syncSource?.eventPath,
        workspaceLite: syncSource?.lite,
      })
    );
  }

  for (const pipeline of workspace.pipelines ?? []) {
    for (const asset of pipeline.assets ?? []) {
      const materialization = materializationByAssetId[asset.id];
      const connectionName = resolveConnection(asset, workspace.connections ?? {});
      const connectionType = connectionName
        ? workspace.connections?.[connectionName] ?? null
        : null;
      const tableParts = parseQualifiedTableName(asset.name);
      const tableKey = assetTableKey(asset.id, pipeline.id);
      const tableSource = buildWorkspaceObservation(recordedAt, {
        method,
        pipelineId: pipeline.id,
        assetId: asset.id,
        assetPath: asset.path,
        connectionName,
        connectionType,
        databaseName: tableParts.databaseName,
        workspaceRevision,
        workspaceEventType: syncSource?.eventType,
        workspaceEventPath: syncSource?.eventPath,
        workspaceLite: syncSource?.lite,
      });

      if (connectionName && connectionType) {
        upsertConnection(
          connections,
          {
            name: connectionName,
            type: connectionType,
            databaseName: tableParts.databaseName,
          },
          tableSource
        );

        if (tableParts.databaseName) {
          upsertDatabase(
            databases,
            {
              name: tableParts.databaseName,
              connectionName,
              connectionType,
            },
            tableSource
          );
        }
      }

      upsertTable(
        tables,
        {
          key: tableKey,
          name: asset.name,
          shortName: tableParts.shortName,
          schemaName: tableParts.schemaName,
          databaseName: tableParts.databaseName,
          connectionKey: connectionName
            ? connectionKey(connectionName, tableParts.databaseName)
            : null,
          connectionName,
          connectionType,
          pipelineId: pipeline.id,
          assetId: asset.id,
          assetPath: asset.path,
          isBruinAsset: true,
          isMaterialized: materialization?.is_materialized ?? asset.is_materialized,
        },
        tableSource
      );

      upsertColumns(tables, tableKey, toSchemaColumns(asset.columns), tableSource);
      upsertColumns(tables, tableKey, extractSQLDefinitionColumns(asset.content), {
        ...tableSource,
        method: "asset-sql-definition",
      });
    }
  }

  return toCatalogResult(connections, databases, tables);
}

function findWorkspaceAsset(
  workspace: WorkspaceState,
  assetId: string
): { asset: WebAsset; pipelineId: string } | null {
  for (const pipeline of workspace.pipelines ?? []) {
    for (const asset of pipeline.assets ?? []) {
      if (asset.id === assetId) {
        return { asset, pipelineId: pipeline.id };
      }
    }
  }

  return null;
}

function mergeDynamicSuggestions(
  baseCatalog: SuggestionCatalogState,
  workspace: WorkspaceState | null,
  dynamicState: DynamicSuggestionState
): SuggestionCatalogState {
  const connections = new Map(
    Object.values(baseCatalog.connectionsByKey).map((connection) => [
      connection.key,
      connection,
    ])
  );
  const databases = new Map(
    Object.values(baseCatalog.databasesByKey).map((database) => [
      database.key,
      database,
    ])
  );
  const tables = new Map(
    Object.values(baseCatalog.tablesByKey).map((table) => [table.key, table])
  );

  for (const observations of Object.values(
    dynamicState.remoteDatabasesByConnectionKey
  )) {
    for (const observation of observations) {
      const source: SuggestionObservation = {
        method: observation.method,
        recordedAt: observation.recordedAt,
        connectionName: observation.connectionName,
        connectionType: observation.connectionType,
        environment: observation.environment,
      };

      if (observation.connectionType) {
        upsertConnection(
          connections,
          {
            name: observation.connectionName,
            type: observation.connectionType,
          },
          source
        );
      }

      for (const databaseName of observation.databases) {
        upsertDatabase(
          databases,
          {
            name: databaseName,
            connectionName: observation.connectionName,
            connectionType: observation.connectionType,
          },
          {
            ...source,
            databaseName,
          }
        );
      }
    }
  }

  for (const [assetId, observations] of Object.entries(
    dynamicState.assetColumnsByAssetId
  )) {
    const workspaceAsset = workspace ? findWorkspaceAsset(workspace, assetId) : null;
    const tableKey = workspaceAsset
      ? assetTableKey(assetId, workspaceAsset.pipelineId)
      : assetTableKey(assetId);
    const connectionName = workspaceAsset
      ? resolveConnection(workspaceAsset.asset, workspace?.connections ?? {})
      : null;
    const connectionType = connectionName
      ? workspace?.connections?.[connectionName] ?? null
      : null;
    const qualifiedName = normalizeTableName(workspaceAsset?.asset.name ?? assetId);
    const tableParts = parseQualifiedTableName(qualifiedName);

    upsertTable(
      tables,
      {
        key: tableKey,
        name: qualifiedName,
        shortName: tableParts.shortName,
        schemaName: tableParts.schemaName,
        databaseName: tableParts.databaseName,
        connectionKey: connectionName
          ? connectionKey(connectionName, tableParts.databaseName)
          : null,
        connectionName,
        connectionType,
        pipelineId: workspaceAsset?.pipelineId,
        assetId,
        assetPath: workspaceAsset?.asset.path,
        isBruinAsset: true,
      },
      {
        method: observations[0]?.method ?? "asset-column-inference",
        recordedAt: observations[0]?.recordedAt ?? new Date().toISOString(),
        assetId,
        pipelineId: workspaceAsset?.pipelineId,
        assetPath: workspaceAsset?.asset.path,
        connectionName,
        connectionType,
        databaseName: tableParts.databaseName,
      }
    );

    for (const observation of observations) {
      upsertColumns(tables, tableKey, observation.columns, {
        method: observation.method,
        recordedAt: observation.recordedAt,
        assetId,
        pipelineId: workspaceAsset?.pipelineId,
        assetPath: workspaceAsset?.asset.path,
        connectionName,
        connectionType,
        databaseName: tableParts.databaseName,
        environment: observation.environment,
      });
    }
  }

  for (const observations of Object.values(dynamicState.remoteTablesByConnectionKey)) {
    for (const observation of observations) {
      const key = connectionKey(observation.connectionName, observation.databaseName);
      const source: SuggestionObservation = {
        method: observation.method,
        recordedAt: observation.recordedAt,
        prefix: observation.prefix,
        connectionName: observation.connectionName,
        connectionType: observation.connectionType,
        databaseName: observation.databaseName,
        environment: observation.environment,
      };

      if (observation.connectionType) {
        upsertConnection(
          connections,
          {
            name: observation.connectionName,
            type: observation.connectionType,
            databaseName: observation.databaseName,
          },
          source
        );
      }

      if (observation.databaseName) {
        upsertDatabase(
          databases,
          {
            name: observation.databaseName,
            connectionName: observation.connectionName,
            connectionType: observation.connectionType,
          },
          source
        );
      }

      for (const remoteTable of observation.tables) {
        const normalizedName = normalizeTableName(remoteTable.name);
        if (!normalizedName) {
          continue;
        }

        const tableParts = parseQualifiedTableName(normalizedName);
        const databaseName =
          remoteTable.databaseName ??
          observation.databaseName ??
          tableParts.databaseName ??
          null;

        if (databaseName) {
          upsertDatabase(
            databases,
            {
              name: databaseName,
              connectionName: observation.connectionName,
              connectionType: observation.connectionType,
            },
            {
              ...source,
              databaseName,
            }
          );
        }

        upsertTable(
          tables,
          {
            key: externalTableKey(
              observation.connectionName,
              normalizedName,
              databaseName
            ),
            name: normalizedName,
            shortName: tableParts.shortName,
            schemaName: remoteTable.schemaName ?? tableParts.schemaName,
            databaseName,
            connectionKey: key,
            connectionName: observation.connectionName,
            connectionType: observation.connectionType ?? null,
            isBruinAsset: false,
            remoteSuggestionKind: remoteTable.kind,
            remoteSuggestionDetail: remoteTable.detail,
          },
          source
        );
      }
    }
  }

  for (const observations of Object.values(
    dynamicState.remoteTableColumnsByTableKey
  )) {
    for (const observation of observations) {
      const normalizedName = normalizeTableName(observation.tableName);
      if (!normalizedName) {
        continue;
      }

      const tableParts = parseQualifiedTableName(normalizedName);
      const databaseName =
        observation.databaseName ?? tableParts.databaseName ?? null;
      const source: SuggestionObservation = {
        method: observation.method,
        recordedAt: observation.recordedAt,
        connectionName: observation.connectionName,
        connectionType: observation.connectionType,
        databaseName,
        environment: observation.environment,
      };
      const key = connectionKey(observation.connectionName, databaseName);
      const tableKey = externalTableKey(
        observation.connectionName,
        normalizedName,
        databaseName
      );

      if (observation.connectionType) {
        upsertConnection(
          connections,
          {
            name: observation.connectionName,
            type: observation.connectionType,
            databaseName,
          },
          source
        );
      }

      if (databaseName) {
        upsertDatabase(
          databases,
          {
            name: databaseName,
            connectionName: observation.connectionName,
            connectionType: observation.connectionType,
          },
          source
        );
      }

      upsertTable(
        tables,
        {
          key: tableKey,
          name: normalizedName,
          shortName: tableParts.shortName,
          schemaName: tableParts.schemaName,
          databaseName,
          connectionKey: key,
          connectionName: observation.connectionName,
          connectionType: observation.connectionType ?? null,
          isBruinAsset: false,
        },
        source
      );

      upsertColumns(tables, tableKey, observation.columns, source);
    }
  }

  return toCatalogResult(connections, databases, tables);
}

function toCatalogResult(
  connections: Map<string, SuggestionConnectionState>,
  databases: Map<string, SuggestionDatabaseState>,
  tables: Map<string, SuggestionTableState>
): SuggestionCatalogState {
  const sortedConnections = sortConnections(Array.from(connections.values()));
  const sortedDatabases = [...databases.values()].sort((left, right) => {
    const connectionCompare = left.connectionName.localeCompare(right.connectionName);
    if (connectionCompare !== 0) {
      return connectionCompare;
    }

    return left.name.localeCompare(right.name);
  });
  const sortedTables = sortTables(Array.from(tables.values()));

  return {
    connections: sortedConnections,
    connectionsByKey: Object.fromEntries(
      sortedConnections.map((connection) => [connection.key, connection])
    ),
    databases: sortedDatabases,
    databasesByKey: Object.fromEntries(
      sortedDatabases.map((database) => [database.key, database])
    ),
    tables: sortedTables,
    tablesByKey: Object.fromEntries(
      sortedTables.map((table) => [table.key, table])
    ),
  };
}

export function buildSuggestionCatalog(args: {
  workspace: WorkspaceState | null;
  syncSource: SuggestionWorkspaceSyncSource | null;
  dynamicState: DynamicSuggestionState;
  materializationByAssetId?: MaterializationByAssetId;
}): SuggestionCatalogState {
  const baseCatalog = buildCatalogFromWorkspace(
    args.workspace,
    args.syncSource,
    args.materializationByAssetId
  );
  return mergeDynamicSuggestions(baseCatalog, args.workspace, args.dynamicState);
}
