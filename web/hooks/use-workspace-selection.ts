"use client";

import { useNavigate, useRouterState } from "@tanstack/react-router";
import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect } from "react";

import {
  resolvedActivePipelineAtom,
  resolvedSelectedAssetAtom,
  routeSelectionAtom,
  workspaceAtom,
} from "@/lib/atoms/domains/workspace";

export function useWorkspaceSelection(): {
  activePipeline: string | null;
  selectedAsset: string | null;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
} {
  const navigate = useNavigate();
  const setRouteSelection = useSetAtom(routeSelectionAtom);
  const workspace = useAtomValue(workspaceAtom);
  const activePipeline = useAtomValue(resolvedActivePipelineAtom);
  const selectedAsset = useAtomValue(resolvedSelectedAssetAtom);
  const locationState = useRouterState({
    select: (state) => ({
      pathname: state.location.pathname,
      search: state.location.search as {
        pipeline?: string;
        asset?: string;
      },
    }),
  });

  useEffect(() => {
    if (locationState.pathname !== "/") {
      return;
    }

    setRouteSelection({
      pipeline: locationState.search.pipeline ?? null,
      asset: locationState.search.asset ?? null,
    });
  }, [
    locationState.pathname,
    locationState.search.asset,
    locationState.search.pipeline,
    setRouteSelection,
  ]);

  useEffect(() => {
    if (locationState.pathname !== "/" || !workspace?.pipelines?.length) {
      return;
    }

    const nextPipeline =
      workspace.pipelines.find(
        (pipeline) => pipeline.id === locationState.search.pipeline
      ) ?? workspace.pipelines[0];
    const nextAssets = nextPipeline.assets ?? [];
    const nextAsset =
      nextAssets.find((asset) => asset.id === locationState.search.asset) ??
      nextAssets[0] ??
      null;

    const nextPipelineId = nextPipeline.id;
    const nextAssetId = nextAsset?.id ?? undefined;
    const currentPipelineId = activePipeline ?? locationState.search.pipeline ?? undefined;
    const currentAssetId = selectedAsset ?? locationState.search.asset ?? undefined;

    if (
      currentPipelineId === nextPipelineId &&
      currentAssetId === nextAssetId
    ) {
      return;
    }

    void navigate({
      to: "/",
      search: {
        pipeline: nextPipelineId,
        asset: nextAssetId,
      },
      replace: true,
    });
  }, [
    activePipeline,
    locationState.pathname,
    locationState.search.asset,
    locationState.search.pipeline,
    navigate,
    selectedAsset,
    workspace,
  ]);

  const navigateSelection = useCallback(
    (pipelineId: string, assetId: string | null) => {
      const nextAsset = assetId ?? undefined;

      if (
        locationState.pathname === "/" &&
        locationState.search.pipeline === pipelineId &&
        locationState.search.asset === nextAsset
      ) {
        return;
      }

      void navigate({
        to: "/",
        search: {
          pipeline: pipelineId,
          asset: nextAsset,
        },
      });
    },
    [
      locationState.pathname,
      locationState.search.asset,
      locationState.search.pipeline,
      navigate,
    ]
  );

  return {
    activePipeline,
    selectedAsset,
    navigateSelection,
  };
}
