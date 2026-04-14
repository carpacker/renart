import { fetchJSON } from "@/lib/api-core";
import { AssetFreshnessResponse } from "@/lib/types";

export async function getAssetFreshness(): Promise<AssetFreshnessResponse> {
  return fetchJSON<AssetFreshnessResponse>("/api/assets/freshness", {
    cache: "no-store",
  });
}
