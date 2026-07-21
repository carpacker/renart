import type { AssetAuthoringCapability } from "@/lib/types";

export function isSqlAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".sql");
}

export function isSeedAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".seed");
}

export function isSensorAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().includes(".sensor.");
}

export function isQuerySensorAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase().endsWith(".sensor.query");
}

export type AssetColumnRefreshMode = "none" | "api" | "definition" | "materialized";

/**
 * Select the authoritative column-inference source for an asset kind. SQL,
 * Load, and local seeds have enough definition-time context; executable
 * non-SQL assets and URL seeds need their materialized output instead. Sensors
 * gate execution and do not produce a relation at all.
 */
export function getAssetColumnRefreshMode(
  assetType?: string | null,
  parameters?: Record<string, string> | null,
): AssetColumnRefreshMode {
  const normalized = (assetType ?? "").trim().toLowerCase();
  if (isSensorAssetType(normalized)) return "none";
  if (normalized === "api") return "api";
  if (isSqlAssetType(normalized) || normalized === "load") return "definition";
  if (isSeedAssetType(normalized)) {
    return /^https?:\/\//i.test(parameters?.path?.trim() ?? "") ? "materialized" : "definition";
  }
  return "materialized";
}

export function getAssetAuthoringCapability(
  assetType: string | null | undefined,
  capabilities: AssetAuthoringCapability[] | null | undefined,
) {
  const normalized = (assetType ?? "").trim().toLowerCase();
  return (capabilities ?? []).find(
    (capability) => capability.type.trim().toLowerCase() === normalized,
  );
}
