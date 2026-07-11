import { fetchJSON } from "@/lib/api-core";
import { AssetFreshnessResponse } from "@/lib/types";

export async function getAssetFreshness(
  options: { environment?: string } = {},
): Promise<AssetFreshnessResponse> {
  const params = new URLSearchParams();
  if (options.environment) params.set("environment", options.environment);
  const query = params.toString();
  return fetchJSON<AssetFreshnessResponse>(`/api/assets/freshness${query ? `?${query}` : ""}`, {
    cache: "no-store",
  });
}
