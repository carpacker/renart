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
import { usePipelinesStaleness } from "@/hooks/use-pipeline-staleness";
import { deleteAsset } from "@/lib/api-assets";
import { assetPresentationFields, type AssetKind } from "@/lib/asset-presentation";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { WebAsset, WebPipeline } from "@/lib/types";
import {
  labelForAppMaterializationState,
  useAppAssetMaterializationStatus,
} from "@/hooks/use-app-asset-materialization-status";

import { kindMeta } from "./app-data";
import { AppLineageCanvas, type AppLineageCanvasAsset } from "./lineage-canvas";
import { PageHeader, AppPage, AppPanel } from "./app-primitives";

function catalogAssetsForPipeline(pipeline: WebPipeline): AppLineageCanvasAsset[] {
  return pipeline.assets.map((asset) => catalogAssetFromWorkspace(asset, pipeline));
}

function catalogAssetFromWorkspace(asset: WebAsset, pipeline: WebPipeline): AppLineageCanvasAsset {
  return {
    ...assetPresentationFields(asset, pipeline),
    status: asset.is_materialized ? "success" : "pending",
    materializedAt: asset.is_materialized ? "current" : "not materialized",
    pipelineId: pipeline.id,
    isMaterialized: asset.is_materialized,
    upstreams: asset.upstreams,
    x: 0,
    y: 0,
  };
}

// Strip asset-selector decorations (++name+, -name) so the box behaves as
// a plain substring filter over asset names.
function normalizeFilterQuery(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/^[+-]+/, "")
    .replace(/[+-]+$/, "");
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
    () => workspace?.pipelines.flatMap(catalogAssetsForPipeline) ?? [],
    [workspace?.pipelines],
  );
  const catalogPipelineIds = useMemo(
    () => [
      ...new Set(
        catalogAssets
          .map((asset) => asset.pipelineId)
          .filter((pipelineId): pipelineId is string => Boolean(pipelineId)),
      ),
    ],
    [catalogAssets],
  );
  const staleness = usePipelinesStaleness(catalogPipelineIds);
  const materializationAssets = useMemo(
    () =>
      catalogAssets.map((asset) => ({
        id: asset.id,
        name: asset.name,
        pipelineId: asset.pipelineId,
        isMaterialized:
          asset.isMaterialized ?? (asset.status === "success" || asset.status === "ok"),
        staleness: asset.pipelineId
          ? staleness.byPipelineId[asset.pipelineId]?.[asset.name]
          : undefined,
      })),
    [catalogAssets, staleness.byPipelineId],
  );
  const materializationStatusByAssetId = useAppAssetMaterializationStatus(materializationAssets);
  const displayedCatalogAssets = useMemo(
    () =>
      catalogAssets.map((asset) => ({
        ...asset,
        status: materializationStatusByAssetId[asset.id]?.status ?? asset.status,
        materializedAt: labelForAppMaterializationState(materializationStatusByAssetId[asset.id]),
        staleness: asset.pipelineId
          ? staleness.byPipelineId[asset.pipelineId]?.[asset.name]
          : undefined,
      })),
    [catalogAssets, materializationStatusByAssetId, staleness.byPipelineId],
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
        actions={
          <Button variant="outline" size="sm">
            <RotateCw className="size-3.5" />
            Reload
          </Button>
        }
      />
      <div className="flex items-center gap-2 px-3 pb-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant={filterActive ? "default" : "outline"} size="sm">
              <Filter className="size-3.5" />
              Filter
              {filterActive
                ? ` (${availableKinds.length - hiddenKinds.size}/${availableKinds.length})`
                : ""}
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
