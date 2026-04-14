"use client";

import { WebAsset } from "@/lib/types";

import { useAssetInspect } from "./use-asset-inspect";

export function useAssetPreviews(visualAssets: WebAsset[]) {
  const {
    inspectByAssetId,
    inspectLoadingByAssetId,
    canLoadMoreByAssetId,
    loadMorePreviewRows,
    clearPreviewForAsset,
    getRowsForAsset,
  } = useAssetInspect(visualAssets);

  return {
    inspectByAssetId,
    inspectLoadingByAssetId,
    canLoadMoreByAssetId,
    loadMorePreviewRows,
    clearPreviewForAsset,
    getRowsForAsset,
  };
}
