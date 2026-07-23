import type { AssetAuthoringCapability } from "@/lib/types";

export function normalizeAssetType(assetType?: string | null) {
  return (assetType ?? "").trim().toLowerCase();
}

export function isSqlAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType).endsWith(".sql");
}

export function isSeedAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType).endsWith(".seed");
}

export function isSensorAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType).includes(".sensor.");
}

export function isQuerySensorAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType).endsWith(".sensor.query");
}

export function isPythonAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType) === "python";
}

export function isAPIAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType) === "api";
}

export function isLoadAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType) === "load";
}

export function isIngestrAssetType(assetType?: string | null) {
  return normalizeAssetType(assetType) === "ingestr";
}

export function isSourceAssetType(assetType?: string | null) {
  const normalized = normalizeAssetType(assetType);
  return normalized === "source" || normalized.endsWith(".source");
}

export function isUnitTestAssetType(assetType?: string | null) {
  const normalized = normalizeAssetType(assetType);
  return normalized === "test" || normalized === "unit_test";
}

export function usesPythonSource(
  asset?: {
    type?: string | null;
    path?: string | null;
  } | null,
) {
  return Boolean(
    asset &&
    (isPythonAssetType(asset.type) || (asset.path ?? "").trim().toLowerCase().endsWith(".py")),
  );
}

export function usesSQLSource(
  asset?: {
    type?: string | null;
    path?: string | null;
  } | null,
) {
  return Boolean(
    asset &&
    (isSqlAssetType(asset.type) || (asset.path ?? "").trim().toLowerCase().endsWith(".sql")),
  );
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
  const normalized = normalizeAssetType(assetType);
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
  const normalized = normalizeAssetType(assetType);
  return (capabilities ?? []).find(
    (capability) => capability.type.trim().toLowerCase() === normalized,
  );
}
