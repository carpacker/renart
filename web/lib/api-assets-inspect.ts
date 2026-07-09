import {
  buildQueryString,
  fetchParsedText,
  MaterializeStreamPayload,
  normalizeInspectResponse,
} from "@/lib/api-core";
import { streamMaterialization } from "@/lib/api-streams";
import { MaterializeScope } from "@/lib/materialize-scope";
import { AssetInspectResponse } from "@/lib/types";

export async function inspectAsset(
  assetId: string,
  options?: { limit?: number; environment?: string; timeWindow?: { start: string; end: string } }
) {
  const { res, text, parsed } = await fetchParsedText<AssetInspectResponse>(
    `/api/assets/${assetId}/inspect${buildQueryString({
      limit: options?.limit,
      environment: options?.environment,
      start_date: options?.timeWindow?.start,
      end_date: options?.timeWindow?.end,
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
  },
  options?: { environment?: string; scope?: MaterializeScope; timeWindow?: { start: string; end: string }; fullRefresh?: boolean }
) {
  return streamMaterialization(
    `/api/assets/${assetId}/materialize/stream${buildQueryString({
      environment: options?.environment,
      scope: options?.scope,
      start_date: options?.timeWindow?.start,
      end_date: options?.timeWindow?.end,
    full_refresh: options?.fullRefresh ? "true" : undefined,
    })}`,
    handlers,
    "Asset materialization stream ended unexpectedly."
  );
}
