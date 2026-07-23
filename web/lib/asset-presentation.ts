import {
  isAPIAssetType,
  isIngestrAssetType,
  isLoadAssetType,
  isPythonAssetType,
  isSeedAssetType,
  isSensorAssetType,
  isSourceAssetType,
  isSqlAssetType,
  isUnitTestAssetType,
} from "@/lib/asset-types";
import type { WebAsset, WebPipeline } from "@/lib/types";

export type AssetKind =
  | "sql"
  | "python"
  | "api"
  | "load"
  | "seed"
  | "sensor"
  | "source"
  | "ingestr"
  | "unittest"
  | "asset";

export function assetKindForType(assetType?: string | null): AssetKind {
  if (isSqlAssetType(assetType)) return "sql";
  if (isPythonAssetType(assetType)) return "python";
  if (isAPIAssetType(assetType)) return "api";
  if (isLoadAssetType(assetType)) return "load";
  if (isSeedAssetType(assetType)) return "seed";
  if (isSensorAssetType(assetType)) return "sensor";
  if (isSourceAssetType(assetType)) return "source";
  if (isIngestrAssetType(assetType)) return "ingestr";
  if (isUnitTestAssetType(assetType)) return "unittest";
  return "asset";
}

export function assetNameParts(name: string) {
  const parts = name.split(".").filter(Boolean);
  if (parts.length <= 1) {
    return { title: name };
  }
  return {
    prefix: parts.slice(0, -1).join("."),
    title: parts[parts.length - 1],
  };
}

export function assetFileStem(assetPath: string) {
  const file = normalizePath(assetPath).split("/").pop() ?? assetPath;
  return file.replace(/\.(?:asset|source|test)\.ya?ml$/i, "").replace(/\.[^.]+$/, "");
}

export function assetDirectory(assetPath: string, pipelinePath: string) {
  const normalizedAssetPath = normalizePath(assetPath).replace(/^\.\/+/, "");
  const pipelineRoot = normalizePath(pipelinePath)
    .replace(/^\.\/+/, "")
    .replace(/\/?pipeline\.ya?ml$/i, "");
  let relative = normalizedAssetPath;
  if (pipelineRoot && relative.startsWith(`${pipelineRoot}/`)) {
    relative = relative.slice(pipelineRoot.length + 1);
  }
  if (relative.startsWith("assets/")) {
    relative = relative.slice("assets/".length);
  }
  const directory = relative.split("/").slice(0, -1).join("/");
  return directory || undefined;
}

export function assetPresentationFields(asset: WebAsset, pipeline: WebPipeline) {
  const name = asset.name.trim() || assetFileStem(asset.path);
  const directory = assetDirectory(asset.path, pipeline.path);
  const { prefix, title } = assetNameParts(name);
  return {
    id: asset.id,
    name,
    displayName: title,
    prefix: prefix ?? directory,
    kind: assetKindForType(asset.type),
    group: prefix ?? "ASSETS",
    integration: asset.connection?.trim() || asset.type.trim() || "Asset",
    description: asset.meta?.description ?? "",
    dir: directory,
  };
}

function normalizePath(value: string) {
  return value.replaceAll("\\", "/").replace(/\/+/g, "/");
}
