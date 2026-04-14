"use client";

import { useMemo, useRef } from "react";

import { NewAssetKind } from "@/components/new-asset-node";
import { getAssetViewMode } from "@/lib/asset-visualization";
import { WebAsset, WebPipeline, WorkspaceState } from "@/lib/types";
import {
  buildSuggestedAssetName,
  normalizeAssetName,
} from "@/lib/workspace-shell-helpers";

type UseWorkspaceDerivedStateInput = {
  workspace: WorkspaceState | null;
  enrichedPipeline: WebPipeline | null;
};

export function useWorkspaceDerivedState({
  workspace,
  enrichedPipeline,
}: UseWorkspaceDerivedStateInput): {
  existingAssetNames: Set<string>;
  defaultAssetNamesByKind: Record<NewAssetKind, string>;
  visualAssets: WebAsset[];
} {
  const existingAssetNames = useMemo(() => {
    const names = new Set<string>();
    for (const item of workspace?.pipelines ?? []) {
      for (const pipelineAsset of item.assets ?? []) {
        names.add(normalizeAssetName(pipelineAsset.name));
      }
    }
    return names;
  }, [workspace?.pipelines]);

  const defaultAssetNamesByKind = useMemo(() => {
    const pipelineName = enrichedPipeline?.name;

    return {
      sql: buildSuggestedAssetName("sql", existingAssetNames, pipelineName),
      python: buildSuggestedAssetName(
        "python",
        existingAssetNames,
        pipelineName
      ),
      ingestr: buildSuggestedAssetName(
        "ingestr",
        existingAssetNames,
        pipelineName
      ),
    };
  }, [enrichedPipeline?.name, existingAssetNames]);

  // Compute a stable string key from the visual asset IDs so that the
  // visualAssets array reference only changes when the set of IDs does.
  const visualIdKey = useMemo(() => {
    return (enrichedPipeline?.assets ?? [])
      .filter((a) => getAssetViewMode(a.meta) !== null)
      .map((a) => a.id)
      .sort()
      .join(",");
  }, [enrichedPipeline]);

  const visualAssetsRef = useRef<WebAsset[]>([]);

  const visualAssets = useMemo(() => {
    const filtered = (enrichedPipeline?.assets ?? []).filter(
      (pipelineAsset) => {
        const mode = getAssetViewMode(pipelineAsset.meta);
        return mode === "chart" || mode === "table" || mode === "markdown";
      }
    );
    visualAssetsRef.current = filtered;
    return filtered;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visualIdKey]);

  return {
    existingAssetNames,
    defaultAssetNamesByKind,
    visualAssets,
  };
}
