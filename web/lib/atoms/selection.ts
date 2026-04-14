import { atom } from "jotai";

import { WebAsset, WebPipeline } from "@/lib/types";

import { workspaceAtom } from "./workspace";

export const routeSelectionAtom = atom<{
  pipeline: string | null;
  asset: string | null;
}>({
  pipeline: null,
  asset: null,
});

export const activePipelineAtom = atom<string | null>((get) => {
  return get(routeSelectionAtom).pipeline;
});

export const selectedAssetAtom = atom<string | null>((get) => {
  return get(routeSelectionAtom).asset;
});

export const resolvedActivePipelineAtom = atom<string | null>((get) => {
  const workspace = get(workspaceAtom);
  const activePipeline = get(activePipelineAtom);

  if (!workspace || workspace.pipelines.length === 0) {
    return activePipeline;
  }

  const selectedPipeline =
    workspace.pipelines.find((pipeline) => pipeline.id === activePipeline) ??
    workspace.pipelines[0];

  return selectedPipeline.id;
});

export const pipelineAtom = atom<WebPipeline | null>((get) => {
  const workspace = get(workspaceAtom);
  const activePipeline = get(resolvedActivePipelineAtom);

  if (!workspace || !activePipeline) {
    return null;
  }

  return (
    workspace.pipelines.find((pipeline) => pipeline.id === activePipeline) ??
    null
  );
});

export const resolvedSelectedAssetAtom = atom<string | null>((get) => {
  const pipeline = get(pipelineAtom);
  const selectedAsset = get(selectedAssetAtom);

  if (!pipeline) {
    return selectedAsset;
  }

  const pipelineAssets = pipeline.assets ?? [];

  const pipelineAsset =
    pipelineAssets.find((asset) => asset.id === selectedAsset) ??
    pipelineAssets[0] ??
    null;

  return pipelineAsset?.id ?? null;
});

export const selectedAssetDataAtom = atom<WebAsset | null>((get) => {
  const pipeline = get(pipelineAtom);
  const selectedAsset = get(resolvedSelectedAssetAtom);

  if (!pipeline || !selectedAsset) {
    return null;
  }

  return (pipeline.assets ?? []).find((asset) => asset.id === selectedAsset) ?? null;
});
