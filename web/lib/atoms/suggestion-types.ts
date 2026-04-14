import { IngestrSuggestion } from "@/lib/types";
import { SchemaColumn } from "@/lib/sql-schema";

export type SuggestionObservationMethod =
  | "workspace-load"
  | "workspace-event"
  | "asset-inspect"
  | "asset-column-inference"
  | "ingestr-suggestions"
  | "connection-database-discovery"
  | "connection-table-discovery"
  | "connection-column-discovery";

export type SuggestionObservation = {
  method: SuggestionObservationMethod;
  recordedAt: string;
  prefix?: string;
  pipelineId?: string;
  assetId?: string;
  assetPath?: string;
  connectionName?: string | null;
  connectionType?: string | null;
  databaseName?: string | null;
  environment?: string;
  workspaceRevision?: number;
  workspaceEventType?: string;
  workspaceEventPath?: string;
  workspaceLite?: boolean;
};

export type SuggestionColumnState = {
  key: string;
  name: string;
  type?: string;
  description?: string;
  primaryKey?: boolean;
  tableKey: string;
  sourceMethods: SuggestionObservationMethod[];
  sources: SuggestionObservation[];
};

export type SuggestionTableState = {
  key: string;
  name: string;
  shortName: string;
  schemaName?: string;
  databaseName?: string | null;
  connectionKey?: string | null;
  connectionName?: string | null;
  connectionType?: string | null;
  pipelineId?: string;
  assetId?: string;
  assetPath?: string;
  isBruinAsset: boolean;
  remoteSuggestionKind?: string;
  remoteSuggestionDetail?: string;
  sourceMethods: SuggestionObservationMethod[];
  sources: SuggestionObservation[];
  columns: SuggestionColumnState[];
};

export type SuggestionConnectionState = {
  key: string;
  name: string;
  type: string;
  databaseName?: string | null;
  sourceMethods: SuggestionObservationMethod[];
  sources: SuggestionObservation[];
};

export type SuggestionDatabaseState = {
  key: string;
  name: string;
  connectionKey: string;
  connectionName: string;
  connectionType?: string | null;
  sourceMethods: SuggestionObservationMethod[];
  sources: SuggestionObservation[];
};

export type SuggestionCatalogState = {
  connections: SuggestionConnectionState[];
  connectionsByKey: Record<string, SuggestionConnectionState>;
  databases: SuggestionDatabaseState[];
  databasesByKey: Record<string, SuggestionDatabaseState>;
  tables: SuggestionTableState[];
  tablesByKey: Record<string, SuggestionTableState>;
};

export type DynamicAssetColumnObservation = {
  method: Extract<
    SuggestionObservationMethod,
    "asset-inspect" | "asset-column-inference"
  >;
  recordedAt: string;
  environment?: string;
  columns: SchemaColumn[];
};

export type DynamicRemoteTableObservation = {
  method: "ingestr-suggestions" | "connection-table-discovery";
  recordedAt: string;
  environment?: string;
  prefix?: string;
  connectionName: string;
  connectionType?: string;
  databaseName?: string | null;
  tables: RemoteTableSuggestionEntry[];
};

export type DynamicRemoteDatabaseObservation = {
  method: "connection-database-discovery";
  recordedAt: string;
  environment?: string;
  connectionName: string;
  connectionType?: string;
  databases: string[];
};

export type DynamicRemoteTableColumnObservation = {
  method: "connection-column-discovery";
  recordedAt: string;
  environment?: string;
  connectionName: string;
  connectionType?: string;
  databaseName?: string | null;
  tableName: string;
  columns: SchemaColumn[];
};

export type DynamicSuggestionState = {
  assetColumnsByAssetId: Record<string, DynamicAssetColumnObservation[]>;
  remoteDatabasesByConnectionKey: Record<string, DynamicRemoteDatabaseObservation[]>;
  remoteTablesByConnectionKey: Record<string, DynamicRemoteTableObservation[]>;
  remoteTableColumnsByTableKey: Record<string, DynamicRemoteTableColumnObservation[]>;
};

export type RegisterAssetColumnsPayload = {
  assetId: string;
  method: Extract<
    SuggestionObservationMethod,
    "asset-inspect" | "asset-column-inference"
  >;
  columns: Array<{
    name: string;
    type?: string;
    description?: string;
    primaryKey?: boolean;
  }>;
  environment?: string;
};

export type RegisterConnectionTablesPayload = {
  method?: Extract<
    SuggestionObservationMethod,
    "ingestr-suggestions" | "connection-table-discovery"
  >;
  connectionName: string;
  connectionType?: string;
  databaseName?: string | null;
  environment?: string;
  prefix?: string;
  tables: RemoteTableSuggestionEntry[];
};

export type RegisterConnectionDatabasesPayload = {
  connectionName: string;
  connectionType?: string;
  environment?: string;
  databases: string[];
};

export type RegisterRemoteTableColumnsPayload = {
  connectionName: string;
  connectionType?: string;
  databaseName?: string | null;
  tableName: string;
  environment?: string;
  columns: Array<{
    name: string;
    type?: string;
    description?: string;
    primaryKey?: boolean;
  }>;
};

export type ConnectionSuggestionEntry = {
  name: string;
  type: string;
  databaseName?: string | null;
};

export type RemoteTableSuggestionEntry = {
  name: string;
  schemaName?: string;
  databaseName?: string | null;
  kind?: string;
  detail?: string;
};

export type SuggestionWorkspaceSyncSource = {
  method: "workspace-load" | "workspace-event";
  recordedAt: string;
  revision?: number;
  eventType?: string;
  eventPath?: string;
  lite?: boolean;
};

export const emptyDynamicSuggestionState: DynamicSuggestionState = {
  assetColumnsByAssetId: {},
  remoteDatabasesByConnectionKey: {},
  remoteTablesByConnectionKey: {},
  remoteTableColumnsByTableKey: {},
};