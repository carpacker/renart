import { useNavigate } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { Filter, Loader2, RotateCw, Search } from "lucide-react";
import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { useAssetResults } from "@/hooks/use-asset-results";
import { deleteAsset } from "@/lib/api-assets";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset, WebPipeline } from "@/lib/types";
import { labelForAppMaterializationState, useAppAssetMaterializationStatus } from "@/hooks/use-app-asset-materialization-status";

import { assets, edges, kindMeta, type AssetKind } from "./app-data";
import { AppLineageCanvas, assetNameParts, type AppLineageCanvasAsset } from "./lineage-canvas";
import { PageHeader, AppPage, AppPanel } from "./app-primitives";

function catalogAssetsForPipeline(pipeline: WebPipeline): AppLineageCanvasAsset[] {
  return pipeline.assets.map((asset) => catalogAssetFromWorkspace(asset, pipeline));
}

function catalogAssetFromWorkspace(asset: WebAsset, pipeline: WebPipeline): AppLineageCanvasAsset {
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
  if (normalized.includes("load")) return "load";
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
  if (provider === "load") return "Load";
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

// Strip Bruin asset-selector decorations (++name+, -name) so the box behaves as
// a plain substring filter over asset names.
function normalizeFilterQuery(value: string) {
  return value.trim().toLowerCase().replace(/^[+-]+/, "").replace(/[+-]+$/, "");
}

export type AppCatalogSearch = { asset?: string };

export function normalizeAppCatalogSearch(search: Record<string, unknown>): AppCatalogSearch {
  return {
    asset: typeof search.asset === "string" && search.asset ? search.asset : undefined,
  };
}

export function AppCatalogPage({ selectedAssetId }: { selectedAssetId?: string } = {}) {
  const workspace = useAtomValue(workspaceAtom);
  const navigate = useNavigate();
  const assetResults = useAssetResults();
  const [query, setQuery] = useState("");
  const [hiddenKinds, setHiddenKinds] = useState<Set<AssetKind>>(() => new Set());

  const catalogAssets = useMemo<AppLineageCanvasAsset[]>(
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
  const materializationStatusByAssetId = useAppAssetMaterializationStatus(materializationAssets);
  const displayedCatalogAssets = useMemo(
    () => catalogAssets.map((asset) => ({
      ...asset,
      status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
      materializedAt: labelForAppMaterializationState(materializationStatusByAssetId[asset.id]),
    })),
    [catalogAssets, materializationStatusByAssetId]
  );

  const availableKinds = useMemo(() => {
    const kinds = new Set<AssetKind>();
    for (const asset of catalogAssets) {
      kinds.add(asset.kind);
    }
    return [...kinds];
  }, [catalogAssets]);

  const filteredAssets = useMemo(() => {
    const normalizedQuery = normalizeFilterQuery(query);
    return displayedCatalogAssets.filter((asset) => {
      if (hiddenKinds.has(asset.kind)) {
        return false;
      }
      if (normalizedQuery && !asset.name.toLowerCase().includes(normalizedQuery)) {
        return false;
      }
      return true;
    });
  }, [displayedCatalogAssets, hiddenKinds, query]);

  const catalogLinks = useMemo(() => {
    if (workspace?.pipelines.length) {
      return undefined;
    }
    const visibleIds = new Set(filteredAssets.map((asset) => asset.id));
    return edges
      .map(([source, target]) => ({ source, target }))
      .filter((edge) => visibleIds.has(edge.source) && visibleIds.has(edge.target));
  }, [filteredAssets, workspace?.pipelines.length]);

  const toggleKind = (kind: AssetKind) => {
    setHiddenKinds((current) => {
      const next = new Set(current);
      if (next.has(kind)) {
        next.delete(kind);
      } else {
        next.add(kind);
      }
      return next;
    });
  };

  const runAsset = (assetId: string) => {
    void assetResults.runMaterializeForAsset(assetId);
  };
  const removeAsset = async (assetId: string) => {
    const target = catalogAssets.find((asset) => asset.id === assetId);
    if (!target?.pipelineId) {
      return;
    }
    // The workspace event stream refreshes the atom once the file is gone.
    await deleteAsset(target.pipelineId, assetId);
  };
  const openInBuild = (assetId: string) => {
    const target = catalogAssets.find((asset) => asset.id === assetId);
    if (!target?.pipelineId) {
      return;
    }
    void navigate({
      to: "/pipelines/$pipelineId/assets/$assetId/canvas",
      params: { pipelineId: target.pipelineId, assetId },
    });
  };

  const filterActive = hiddenKinds.size > 0;

  return (
    <AppPage>
      <PageHeader
        title="Catalog"
        subtitle="Explore asset lineage across data_platform"
        actions={<Button variant="outline" size="sm"><RotateCw className="size-3.5" />Reload</Button>}
      />
      <div className="flex items-center gap-2 px-3 pb-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant={filterActive ? "default" : "outline"} size="sm">
              <Filter className="size-3.5" />
              Filter{filterActive ? ` (${availableKinds.length - hiddenKinds.size}/${availableKinds.length})` : ""}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-44">
            <DropdownMenuLabel>Asset type</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {availableKinds.map((kind) => (
              <DropdownMenuCheckboxItem
                key={kind}
                checked={!hiddenKinds.has(kind)}
                onCheckedChange={() => toggleKind(kind)}
                onSelect={(event) => event.preventDefault()}
              >
                {kindMeta[kind]?.label ?? kind}
              </DropdownMenuCheckboxItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="Filter assets by name...  (ex: revenue_daily)"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <AppPanel className="h-full">
          {workspace ? (
            <AppLineageCanvas
              assets={filteredAssets}
              links={catalogLinks}
              selectedAssetId={selectedAssetId}
              focusAssetId={selectedAssetId}
              onRunAsset={runAsset}
              onDeleteAsset={removeAsset}
              onGoToAsset={openInBuild}
              goToLabel="Open in build"
            />
          ) : (
            <div className="flex h-full items-center justify-center">
              <Loader2 className="size-6 animate-spin text-muted-foreground" />
            </div>
          )}
        </AppPanel>
      </div>
    </AppPage>
  );
}
