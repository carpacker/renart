import type {
  AssetInspectResponse as GeneratedAssetInspectResponse,
  FormatSQLAssetResponse as GeneratedFormatSQLAssetResponse,
  InferColumnsResponse as GeneratedInferColumnsResponse,
  IngestrSuggestion,
  IngestrSuggestionsResponse,
  OnboardingDiscoveryResponse as GeneratedOnboardingDiscoveryResponse,
  OnboardingImportFormState,
  OnboardingImportResultState,
  OperationMetadata,
  OnboardingPathSuggestionsResponse,
  OnboardingSessionState as GeneratedOnboardingSessionState,
  PipelineConfigConnection as GeneratedPipelineConfigConnection,
  PipelineConfigDefaults as GeneratedPipelineConfigDefaults,
  PipelineConfigNotification as GeneratedPipelineConfigNotification,
  PipelineConfigVariable as GeneratedPipelineConfigVariable,
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
  WebPipelineConfigResponse as GeneratedWebPipelineConfigResponse,
  WebPipeline as GeneratedWebPipeline,
  WebUpdatePipelineConfigRequest as GeneratedWebUpdatePipelineConfigRequest,
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
  OperationMetadata,
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

export type WorkspaceConfigFieldType = "string" | "int" | "bool" | "string_array";

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
  schedule?: string;
  assets: WebAsset[];
};

export type WorkspaceState = Omit<GeneratedWorkspaceState, "pipelines"> & {
  pipelines: WebPipeline[];
};

export type WorkspaceEvent = Omit<GeneratedWorkspaceEvent, "workspace"> & {
  workspace: WorkspaceState;
};

export type PipelineSchedule = {
  pipeline_id: string;
  pipeline_name: string;
  pipeline_path: string;
  schedule: string;
  timezone: string;
  catchup: boolean;
  enabled: boolean;
  next_run_at?: string;
};

export type PipelineRun = {
  id: string;
  pipeline_id: string;
  pipeline: string;
  environment: string;
  trigger: "schedule" | "manual" | "api" | "cli";
  status: "queued" | "running" | "success" | "failed" | "cancelled";
  win_start?: string;
  win_end?: string;
  started_at?: string;
  finished_at?: string;
  error?: string;
  log_ref?: string;
};

export type PipelineRunLogLine = {
  at: string;
  line: string;
};

export type PipelineRunStep = {
  run_id: string;
  asset: string;
  status: PipelineRun["status"];
  started_at?: string;
  finished_at?: string;
  error?: string;
};

export type SchedulesResponse = {
  status: "ok" | "error";
  schedules: PipelineSchedule[];
};

export type UpdateScheduleResponse = {
  status: "ok" | "error";
  schedule: PipelineSchedule;
};

export type TriggerPipelineResponse = {
  status: "ok" | "error";
  run: PipelineRun;
};

export type RunsResponse = {
  status: "ok" | "error";
  runs: PipelineRun[];
};

export type RunDetailResponse = {
  status: "ok" | "error";
  run: PipelineRun;
  logs: PipelineRunLogLine[];
  steps: PipelineRunStep[];
};

export type SourceControlChange = {
  path: string;
  staged_status: string;
  worktree_status: string;
  staged: boolean;
};

export type SourceControlRepository = {
  branch: string;
  clean: boolean;
  changes: SourceControlChange[];
};

export type SourceControlCommit = {
  hash: string;
  message: string;
};

export type SourceControlDiff = {
  path: string;
  staged: boolean;
  patch: string;
};

export type SourceControlStatusResponse = {
  status: "ok" | "error";
  repository: SourceControlRepository;
};

export type SourceControlBranchesResponse = {
  status: "ok" | "error";
  branches: string[];
};

export type SourceControlDiffResponse = {
  status: "ok" | "error";
  diff: SourceControlDiff;
};

export type SourceControlActionResponse = {
  status: "ok" | "error";
  repository?: SourceControlRepository;
};

export type SourceControlCommitResponse = SourceControlActionResponse & {
  commit?: SourceControlCommit;
};

export type OnboardingSessionState = Omit<
  GeneratedOnboardingSessionState,
  "step" | "import_result"
> & {
  step?: "start" | "connection-type" | "connection-config" | "import" | "quickstart" | "success";
  import_result?: OnboardingImportResultState | null;
};

export type OnboardingImportResponse = {
  status: "ok" | "error";
  operation?: OperationMetadata;
  output?: string;
  error?: string;
  pipeline_path?: string;
  asset_paths?: string[];
};

export type OnboardingImportSummary = {
  database?: string;
  importedTables?: number;
  mergedTables?: number;
  pipelinePath?: string;
  processedAssets?: number;
  successfulAssets?: number;
  failedAssets?: number;
  warnings: string[];
};

export type AssetInspectResponse = GeneratedAssetInspectResponse & {
  status: "ok" | "error";
  operation?: OperationMetadata;
  warning?: string;
  missing_upstream_asset_ids?: string[];
  missing_upstream_asset_names?: string[];
  missing_upstream_assets_materializable?: boolean;
};

export type InferColumnsResponse = GeneratedInferColumnsResponse & {
  status: "ok" | "error";
  operation?: OperationMetadata;
};

export type FormatSQLAssetResponse = GeneratedFormatSQLAssetResponse & {
  status: "ok" | "error";
  error?: string;
};

export type FormatPythonAssetResponse = {
  status: "ok" | "error";
  asset_id: string;
  content: string;
  error?: string;
};

export type PythonDiagnosticsResponse = {
  status: "ok" | "error";
  asset_id: string;
  diagnostics?: PythonDiagnostic[];
  error?: string;
};

export type PythonCompletionsResponse = {
  status: "ok" | "error";
  asset_id: string;
  completions?: PythonCompletion[];
  error?: string;
};

export type PythonHoverResponse = {
  status: "ok" | "error";
  asset_id: string;
  hover?: PythonHover;
  error?: string;
};

export type PythonSignatureHelpResponse = {
  status: "ok" | "error";
  asset_id: string;
  signature_help?: PythonSignatureHelp;
  error?: string;
};

export type PythonGotoDefinitionResponse = {
  status: "ok" | "error";
  asset_id: string;
  targets?: PythonGotoTarget[];
  error?: string;
};

export type PythonDiagnostic = {
  id: string;
  message: string;
  severity: "info" | "warning" | "error" | "fatal";
  range?: PythonRange;
  display?: string;
};

export type PythonRange = {
  start: PythonPosition;
  end: PythonPosition;
};

export type PythonPosition = {
  line: number;
  column: number;
};

export type PythonCompletion = {
  label: string;
  kind?: string;
  detail?: string;
  insert_text?: string;
  insert_text_format: "plaintext" | "snippet";
  documentation?: string;
  module_name?: string;
  additional_text_edits?: PythonTextEdit[];
};

export type PythonTextEdit = {
  range: PythonRange;
  text: string;
};

export type PythonHover = {
  contents: string;
  range?: PythonRange;
};

export type PythonSignatureHelp = {
  signatures: PythonSignature[];
  active_signature?: number;
  active_parameter?: number;
};

export type PythonSignature = {
  label: string;
  documentation?: string;
  parameters: PythonSignatureParameter[];
  active_parameter?: number;
};

export type PythonSignatureParameter = {
  label: string;
  name: string;
  type: string;
  documentation?: string;
};

export type PythonGotoTarget = {
  path: string;
  focus_range: PythonRange;
  full_range: PythonRange;
};

export type PipelineMaterializationResponse = Omit<GeneratedPipelineMaterializationResponse, "assets"> & {
  assets: Array<GeneratedPipelineMaterializationResponse["assets"][number] & {
    freshness_status?: "fresh" | "stale";
  }>;
};

export type PipelineConfigConnection = GeneratedPipelineConfigConnection;

export type PipelineConfigNotification = GeneratedPipelineConfigNotification;

export type PipelineConfigDefaults = GeneratedPipelineConfigDefaults;

export type PipelineConfigVariable = GeneratedPipelineConfigVariable & {
  extra?: Record<string, unknown>;
};

export type PipelineConfigResponse = Omit<
  GeneratedWebPipelineConfigResponse,
  "status" | "variables"
> & {
  status: "ok" | "error";
  variables: PipelineConfigVariable[];
};

export type UpdatePipelineConfigRequest = Omit<
  GeneratedWebUpdatePipelineConfigRequest,
  "variables"
> & {
  variables: PipelineConfigVariable[];
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
