import {
  fetchJSON,
  fetchJSONWithBody,
  fetchParsedText,
  FillColumnsFromDBResponse,
} from "@/lib/api-core";
import { InferColumnsResponse, WebColumn } from "@/lib/types";

export async function inferAssetColumns(assetId: string) {
  return fetchJSON<InferColumnsResponse>(`/api/assets/${assetId}/columns/infer`, {
    method: "GET",
  });
}

export async function inferAPIAsset(assetId: string) {
  return fetchJSON<{
    status: string;
    request_url: string;
    records_path: string;
    records_count: number;
    records_paths: Array<{ path: string; detail?: string }>;
    columns: WebColumn[];
  }>(`/api/assets/${assetId}/api-infer`, {
    method: "POST",
  });
}

export async function updateAssetColumns(
  assetId: string,
  columns: WebColumn[]
) {
  return fetchJSONWithBody<Record<string, string>>(
    `/api/assets/${assetId}/columns`,
    "PUT",
    { columns }
  );
}

export async function fillAssetColumnsFromDB(assetId: string) {
  const { res, text, parsed } = await fetchParsedText<FillColumnsFromDBResponse>(
    `/api/assets/${assetId}/fill-columns-from-db`,
    {
      method: "POST",
    }
  );
  if (!text) {
    return { status: res.ok ? "ok" : "error" };
  }

  if (!parsed) {
    throw new Error(text || `Request failed: ${res.status}`);
  }

  return parsed;
}
