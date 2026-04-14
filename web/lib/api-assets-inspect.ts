import {
  buildQueryString,
  fetchParsedText,
  MaterializeStreamPayload,
  normalizeInspectResponse,
} from "@/lib/api-core";
import { streamMaterialization } from "@/lib/api-streams";
import { AssetInspectResponse } from "@/lib/types";

export async function inspectAsset(
  assetId: string,
  options?: { limit?: number; environment?: string }
) {
  const { res, text, parsed } = await fetchParsedText<AssetInspectResponse>(
    `/api/assets/${assetId}/inspect${buildQueryString({
      limit: options?.limit,
      environment: options?.environment,
    })}`,
    { method: "GET" }
  );

  if (parsed) {
    return normalizeInspectResponse(parsed);
  }

  throw new Error(text || `Request failed: ${res.status}`);
}

export async function materializeAssetStream(
  assetId: string,
  handlers: {
    onChunk?: (chunk: string) => void;
    onDone?: (payload: MaterializeStreamPayload) => void;
  }
) {
  return streamMaterialization(
    `/api/assets/${assetId}/materialize/stream`,
    handlers,
    "Asset materialization stream ended unexpectedly."
  );
}
