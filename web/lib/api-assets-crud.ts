import { fetchJSON, fetchJSONWithBody } from "@/lib/api-core";
import {
  FormatPythonAssetResponse,
  FormatSQLAssetResponse,
  PythonCompletionsResponse,
  PythonDiagnosticsResponse,
  PythonGotoDefinitionResponse,
  PythonHoverResponse,
  PythonSignatureHelpResponse,
} from "@/lib/types";

export async function createAsset(
  pipelineId: string,
  input: {
    name?: string;
    type?: string;
    path?: string;
    content?: string;
    connection?: string;
    parameters?: Record<string, string>;
    source_asset_id?: string;
    seed_file_name?: string;
    seed_file_content?: string;
  },
) {
  return fetchJSONWithBody<{ status: string; asset_id?: string; asset_path?: string }>(
    `/api/pipelines/${pipelineId}/assets`,
    "POST",
    input,
  );
}

export async function updateAsset(
  pipelineId: string,
  assetId: string,
  input: {
    name?: string;
    type?: string;
    content?: string;
    connection?: string;
    materialization_type?: string;
    materialization_strategy?: string;
    incremental_key?: string;
    partition_by?: string;
    cluster_by?: string[];
    time_granularity?: string;
    owner?: string;
    tags?: string[];
    meta?: Record<string, string>;
    upstreams?: string[];
    parameters?: Record<string, string>;
  },
) {
  return fetchJSONWithBody<Record<string, string>>(
    `/api/pipelines/${pipelineId}/assets/${assetId}`,
    "PUT",
    input,
  );
}

export async function deleteAsset(pipelineId: string, assetId: string) {
  return fetchJSON<Record<string, string>>(`/api/pipelines/${pipelineId}/assets/${assetId}`, {
    method: "DELETE",
  });
}

export async function formatSQLAsset(assetId: string, content: string) {
  return fetchJSONWithBody<FormatSQLAssetResponse>(`/api/assets/${assetId}/format-sql`, "POST", {
    content,
  });
}

export async function formatPythonAsset(assetId: string, content: string) {
  return fetchJSONWithBody<FormatPythonAssetResponse>(
    `/api/assets/${assetId}/format-python`,
    "POST",
    { content },
  );
}

export type AssetPythonDeps = {
  status: "ok" | "error";
  asset_id: string;
  dependencies: string[];
  installed_modules: string[];
};

/** Declared dependencies and installed import names for a Python asset. */
export async function getAssetPythonDeps(assetId: string, signal?: AbortSignal) {
  return fetchJSON<AssetPythonDeps>(
    `/api/assets/${assetId}/python-deps`,
    signal ? { signal } : undefined,
  );
}

/** Add a package to the Python asset's pyproject.toml; returns refreshed deps. */
export async function addAssetPythonDependency(assetId: string, pkg: string) {
  return fetchJSONWithBody<AssetPythonDeps>(`/api/assets/${assetId}/python-deps`, "POST", {
    package: pkg,
  });
}

export async function getPythonDiagnostics(assetId: string, content: string, signal?: AbortSignal) {
  return fetchJSONWithBody<PythonDiagnosticsResponse>(
    `/api/assets/${assetId}/python-diagnostics`,
    "POST",
    { content },
    signal ? { signal } : undefined,
  );
}

export async function getPythonCompletions(
  assetId: string,
  input: {
    content: string;
    line: number;
    column: number;
    snippets: boolean;
  },
  signal?: AbortSignal,
) {
  return fetchJSONWithBody<PythonCompletionsResponse>(
    `/api/assets/${assetId}/python-completions`,
    "POST",
    input,
    signal ? { signal } : undefined,
  );
}

export async function getPythonHover(
  assetId: string,
  input: { content: string; line: number; column: number },
  signal?: AbortSignal,
) {
  return fetchJSONWithBody<PythonHoverResponse>(
    `/api/assets/${assetId}/python-hover`,
    "POST",
    input,
    signal ? { signal } : undefined,
  );
}

export async function getPythonSignatureHelp(
  assetId: string,
  input: { content: string; line: number; column: number },
  signal?: AbortSignal,
) {
  return fetchJSONWithBody<PythonSignatureHelpResponse>(
    `/api/assets/${assetId}/python-signature-help`,
    "POST",
    input,
    signal ? { signal } : undefined,
  );
}

export async function getPythonGotoDefinition(
  assetId: string,
  input: { content: string; line: number; column: number },
  signal?: AbortSignal,
) {
  return fetchJSONWithBody<PythonGotoDefinitionResponse>(
    `/api/assets/${assetId}/python-goto-definition`,
    "POST",
    input,
    signal ? { signal } : undefined,
  );
}
