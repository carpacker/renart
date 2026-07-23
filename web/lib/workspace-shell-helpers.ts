import type { AssetCreationKind } from "@/lib/asset-creation-profile";

export function buildSuggestedAssetName(
  kind: AssetCreationKind,
  existingNames: Set<string>,
  pipelineName?: string | null,
): string {
  const pipelinePrefix = slugifyPipelinePrefix(pipelineName);
  const prefixByKind: Record<AssetCreationKind, string> = {
    sql: `${pipelinePrefix}.my_sql_asset_`,
    python: `${pipelinePrefix}.my_python_asset_`,
    load: `${pipelinePrefix}.my_load_asset_`,
    api: `${pipelinePrefix}.my_api_asset_`,
    seed: `${pipelinePrefix}.my_seed_asset_`,
    sensor: `${pipelinePrefix}.my_sensor_asset_`,
  };

  const prefix = prefixByKind[kind];
  let index = 1;
  while (existingNames.has(`${prefix}${index}`)) {
    index += 1;
  }

  return `${prefix}${index}`;
}

function slugifyPipelinePrefix(input?: string | null): string {
  const normalized = (input ?? "").trim().toLowerCase();
  if (!normalized) {
    return "default";
  }

  const slug = normalized
    .replace(/[^a-z0-9\s_-]/g, "")
    .replace(/[\s_]+/g, "_")
    .replace(/-+/g, "_")
    .replace(/^_+|_+$/g, "");

  return slug || "default";
}
