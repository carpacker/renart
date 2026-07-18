import { fetchJSONWithBody } from "@/lib/api-core";
import type {
  AssetRenderRequest,
  AssetRenderResult,
  AssetRenderStage,
} from "@/lib/generated/api-types";

export type AssetRenderFidelity = AssetRenderStage["fidelity"];
export type AssetRenderStageStatus = AssetRenderStage["status"];
export type AssetRenderStatus = AssetRenderResult["status"];

export type {
  AssetRenderRequest,
  AssetRenderResult,
  AssetRenderStage,
  AssetRenderTarget,
} from "@/lib/generated/api-types";

export function renderAsset(assetId: string, request: AssetRenderRequest) {
  return fetchJSONWithBody<AssetRenderResult>(
    `/api/assets/${encodeURIComponent(assetId)}/render`,
    "POST",
    request,
  );
}
