import { useAtomValue } from "jotai";
import { Filter, RotateCw, Search, Sparkles } from "lucide-react";
import { useMemo } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset, WebPipeline } from "@/lib/types";
import { labelForRedesignMaterializationState, useRedesignAssetMaterializationStatus } from "@/hooks/use-redesign-asset-materialization-status";

import { assets, edges, type AssetKind } from "./redesign-data";
import { RedesignLineageCanvas, assetNameParts, type RedesignLineageCanvasAsset } from "./lineage-canvas";
import { PageHeader, RedesignPage, RedesignPanel } from "./redesign-primitives";

function catalogAssetsForPipeline(pipeline: WebPipeline): RedesignLineageCanvasAsset[] {
  return pipeline.assets.map((asset) => catalogAssetFromWorkspace(asset, pipeline));
}

function catalogAssetFromWorkspace(asset: WebAsset, pipeline: WebPipeline): RedesignLineageCanvasAsset {
  const canonicalName = asset.name || assetFileName(asset.path);
  const { prefix, title } = assetNameParts(canonicalName);
  return {
    id: asset.id,
    name: canonicalName,
    displayName: title,
    prefix: prefix ?? assetDirectory(asset.path, pipeline.path),
    kind: kindForAssetType(asset.type),
    group: prefix ?? "ASSETS",
    integration: integrationForAsset(asset),
    description: asset.meta?.description ?? asset.path,
    dir: assetDirectory(asset.path, pipeline.path),
    status: asset.is_materialized ? "success" : "pending",
    materializedAt: asset.is_materialized ? "current" : "not materialized",
    pipelineId: pipeline.id,
    isMaterialized: asset.is_materialized,
    upstreams: asset.upstreams,
    x: 0,
    y: 0,
  };
}

function kindForAssetType(type: string): AssetKind {
  const normalized = type.toLowerCase();
  if (normalized.includes("python")) return "python";
  if (normalized.includes("sling")) return "sling";
  if (normalized.includes("ingestr")) return "ingestr";
  if (normalized.includes("source")) return "source";
  if (normalized.includes("test")) return "unittest";
  return "sql";
}

function integrationForAsset(asset: WebAsset) {
  if (asset.connection) return asset.connection;
  const provider = asset.type.split(".")[0]?.toLowerCase();
  if (provider === "duckdb") return "DuckDB";
  if (provider === "python") return "Python";
  if (provider === "sling") return "Sling";
  if (provider === "ingestr") return "ingestr";
  return provider || "Asset";
}

function assetDirectory(assetPath: string, pipelinePath: string) {
  const pipelineRoot = pipelinePath.replace(/\/?pipeline\.ya?ml$/i, "");
  let relative = assetPath;
  if (pipelineRoot && relative.startsWith(`${pipelineRoot}/`)) {
    relative = relative.slice(pipelineRoot.length + 1);
  }
  if (relative.startsWith("assets/")) {
    relative = relative.slice("assets/".length);
  }
  const dir = relative.split("/").slice(0, -1).join("/");
  return dir || undefined;
}

function assetFileName(assetPath: string) {
  const file = assetPath.split("/").pop() ?? assetPath;
  return file.replace(/\.[^.]+$/, "");
}

export function RedesignCatalogPage() {
  const workspace = useAtomValue(workspaceAtom);
  const catalogAssets = useMemo<RedesignLineageCanvasAsset[]>(
    () => workspace?.pipelines.length ? workspace.pipelines.flatMap(catalogAssetsForPipeline) : assets,
    [workspace?.pipelines]
  );
  const materializationAssets = useMemo(
    () => catalogAssets.map((asset) => ({
      id: asset.id,
      name: asset.name,
      pipelineId: asset.pipelineId,
      isMaterialized: asset.isMaterialized ?? (asset.status === "success" || asset.status === "ok"),
    })),
    [catalogAssets]
  );
  const materializationStatusByAssetId = useRedesignAssetMaterializationStatus(materializationAssets);
  const displayedCatalogAssets = useMemo(
    () => catalogAssets.map((asset) => ({
      ...asset,
      status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
      materializedAt: labelForRedesignMaterializationState(materializationStatusByAssetId[asset.id]),
    })),
    [catalogAssets, materializationStatusByAssetId]
  );
  const catalogLinks = workspace?.pipelines.length
    ? undefined
    : edges.map(([source, target]) => ({ source, target }));

  return (
    <RedesignPage>
      <PageHeader
        title="Catalog"
        subtitle="Explore asset lineage across data_platform"
        actions={<Button variant="outline" size="sm"><RotateCw className="size-3.5" />Reload</Button>}
      />
      <div className="flex items-center gap-2 px-3 pb-2">
        <Button variant="outline" size="sm"><Filter className="size-3.5" />Filter</Button>
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-8" placeholder="Type an asset subset...  (ex: ++revenue_daily)" />
        </div>
        <Button size="sm"><Sparkles className="size-3.5" />Materialize all</Button>
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <RedesignPanel className="h-full">
          <RedesignLineageCanvas assets={displayedCatalogAssets} links={catalogLinks} />
        </RedesignPanel>
      </div>
    </RedesignPage>
  );
}
