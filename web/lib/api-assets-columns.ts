import {
  fetchJSON,
  fetchJSONWithBody,
  fetchParsedText,
  FillColumnsFromDBResponse,
} from "@/lib/api-core";
import type {
  APIInferResult,
  ColumnInferencePreview,
  ColumnSchemaResolution,
  ColumnSchemaSyncResult,
} from "@/lib/generated/api-types";
import { InferColumnsResponse, WebColumn } from "@/lib/types";

export async function inferAssetColumns(assetId: string) {
  return fetchJSON<InferColumnsResponse>(`/api/assets/${assetId}/columns/infer`, {
    method: "GET",
  });
}

export async function inferAPIAsset(assetId: string) {
  return fetchJSON<APIInferResult>(`/api/assets/${assetId}/api-infer`, {
    method: "POST",
  });
}

export async function previewAssetColumns(assetId: string, source: string, environment?: string) {
  return fetchJSONWithBody<ColumnInferencePreview>(
    `/api/assets/${assetId}/columns/preview`,
    "POST",
    { source, environment },
  );
}

export async function syncAssetColumns(
  assetId: string,
  additionalSources: string[],
  environment?: string,
) {
  return fetchJSONWithBody<ColumnSchemaSyncResult>(`/api/assets/${assetId}/columns/sync`, "POST", {
    additional_sources: additionalSources,
    environment,
  });
}

export async function applyAssetColumnSchemaResolution(
  assetId: string,
  sync: Pick<ColumnSchemaSyncResult, "managed_columns" | "candidate_columns" | "columns">,
  resolutions: ColumnSchemaResolution[],
) {
  return fetchJSONWithBody<{ status: string; columns: WebColumn[] }>(
    `/api/assets/${assetId}/columns/sync/apply`,
    "POST",
    {
      managed_columns: sync.managed_columns,
      candidate_columns: sync.candidate_columns,
      current_columns: sync.columns ?? [],
      resolutions,
    },
  );
}

export async function updateAssetColumns(assetId: string, columns: WebColumn[]) {
  return fetchJSONWithBody<Record<string, string>>(`/api/assets/${assetId}/columns`, "PUT", {
    columns,
  });
}

export async function fillAssetColumnsFromDB(assetId: string) {
  const { res, text, parsed } = await fetchParsedText<FillColumnsFromDBResponse>(
    `/api/assets/${assetId}/fill-columns-from-db`,
    {
      method: "POST",
    },
  );
  if (!text) {
    return { status: res.ok ? "ok" : "error" };
  }

  if (!parsed) {
    throw new Error(text || `Request failed: ${res.status}`);
  }

  return parsed;
}
