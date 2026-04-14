import type {
  AssetInspectResponse as GeneratedAssetInspectResponse,
  FormatSQLAssetResponse as GeneratedFormatSQLAssetResponse,
  InferColumnsResponse as GeneratedInferColumnsResponse,
  IngestrSuggestion,
  IngestrSuggestionsResponse,
  OnboardingDiscoveryResponse as GeneratedOnboardingDiscoveryResponse,
  OnboardingImportFormState,
  OnboardingImportResultState,
  OnboardingPathSuggestionsResponse,
  OnboardingSessionState as GeneratedOnboardingSessionState,
  PipelineMaterializationResponse as GeneratedPipelineMaterializationResponse,
  SqlDiscoveryDatabasesResponse,
  SqlDiscoveryTable,
  SqlDiscoveryTableColumnsResponse,
  SqlDiscoveryTablesResponse,
  SqlParseContextColumn,
  SqlParseContextDiagnostic,
  SqlParseContextPart,
  SqlParseContextRange,
  SqlParseContextResponse as GeneratedSqlParseContextResponse,
  SqlParseContextTable,
  SqlPathSuggestionsResponse,
  WebAsset as GeneratedWebAsset,
  WebPipeline as GeneratedWebPipeline,
  WebColumn,
  WebColumnCheck,
  WorkspaceConfigConnection,
  WorkspaceConfigConnectionType as GeneratedWorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
  WorkspaceConfigFieldDef as GeneratedWorkspaceConfigFieldDef,
  WorkspaceConfigResponse as GeneratedWorkspaceConfigResponse,
  WorkspaceEvent as GeneratedWorkspaceEvent,
  WorkspaceState as GeneratedWorkspaceState,
} from "@/lib/generated/api-types";

export type {
  IngestrSuggestion,
  IngestrSuggestionsResponse,
  OnboardingImportFormState,
  OnboardingImportResultState,
  OnboardingPathSuggestionsResponse,
  SqlDiscoveryDatabasesResponse,
  SqlDiscoveryTable,
  SqlDiscoveryTableColumnsResponse,
  SqlDiscoveryTablesResponse,
  SqlParseContextColumn,
  SqlParseContextDiagnostic,
  SqlParseContextPart,
  SqlParseContextRange,
  SqlParseContextTable,
  SqlPathSuggestionsResponse,
  WebColumn,
  WebColumnCheck,
  WorkspaceConfigConnection,
  WorkspaceConfigEnvironment,
};

export type WorkspaceConfigFieldType = "string" | "int" | "bool";

export type WorkspaceConfigFieldDef = Omit<
  GeneratedWorkspaceConfigFieldDef,
  "type"
> & {
  type: WorkspaceConfigFieldType;
};

export type WorkspaceConfigConnectionType = Omit<
  GeneratedWorkspaceConfigConnectionType,
  "fields"
> & {
  fields: WorkspaceConfigFieldDef[];
};

export type WorkspaceConfigResponse = Omit<
  GeneratedWorkspaceConfigResponse,
  "status" | "connection_types"
> & {
  status: "ok" | "error";
  connection_types: WorkspaceConfigConnectionType[];
};

export type OnboardingDiscoveryResponse = Omit<GeneratedOnboardingDiscoveryResponse, "status"> & {
  status: "ok" | "error";
};

export type SqlParseContextResponse = GeneratedSqlParseContextResponse & {
  status: "ok" | "error";
};

export type WebAsset = GeneratedWebAsset & {
  freshness_status?: "fresh" | "stale";
};

export type WebPipeline = Omit<GeneratedWebPipeline, "assets"> & {
  assets: WebAsset[];
};

export type WorkspaceState = Omit<GeneratedWorkspaceState, "pipelines"> & {
  pipelines: WebPipeline[];
};

export type WorkspaceEvent = Omit<GeneratedWorkspaceEvent, "workspace"> & {
  workspace: WorkspaceState;
};

export type OnboardingSessionState = Omit<
  GeneratedOnboardingSessionState,
  "step" | "import_result"
> & {
  step?: "connection-type" | "connection-config" | "import" | "success";
  import_result?: OnboardingImportResultState | null;
};

export type OnboardingImportResponse = {
  status: "ok" | "error";
  command?: string[];
  output?: string;
  error?: string;
  pipeline_path?: string;
  asset_paths?: string[];
};

export type AssetInspectResponse = GeneratedAssetInspectResponse & {
  status: "ok" | "error";
  command?: string[];
  warning?: string;
};

export type InferColumnsResponse = GeneratedInferColumnsResponse & {
  status: "ok" | "error";
  command?: string[];
};

export type FormatSQLAssetResponse = GeneratedFormatSQLAssetResponse & {
  status: "ok" | "error";
  error?: string;
};

export type PipelineMaterializationResponse = Omit<GeneratedPipelineMaterializationResponse, "assets"> & {
  assets: Array<GeneratedPipelineMaterializationResponse["assets"][number] & {
    freshness_status?: "fresh" | "stale";
  }>;
};

export type AssetFreshnessEntry = {
  asset_name: string;
  materialized_at?: string;
  materialized_status?: string;
  content_changed_at?: string;
};

export type AssetFreshnessResponse = {
  assets: AssetFreshnessEntry[];
};
