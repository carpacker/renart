"use client";

import { useAtomValue } from "jotai";
import { useCallback, useState } from "react";

import { enrichedSelectedAssetAtom } from "@/lib/atoms/domains/results";
import { pipelineAtom } from "@/lib/atoms/domains/workspace";
import { MaterializeScope } from "@/lib/materialize-scope";
import { VISUALIZATION_META_KEYS } from "@/lib/visualization-meta";

type UseEditorActionsInput = {
  editorValue: string;
  flushAssetSave: (assetId: string) => void;
  runUpdateAsset: (
    pipelineId: string,
    assetId: string,
    input: {
      name?: string;
      type?: string;
      content?: string;
      materialization_type?: string;
      meta?: Record<string, string>;
      upstreams?: string[];
    }
  ) => Promise<{ status?: string; asset_id?: string } | null>;
  runDeleteAsset: (pipelineId: string, assetId: string) => Promise<boolean>;
  runUpdatePipeline: (
    pipelineId: string,
    input: { name?: string; content?: string }
  ) => Promise<boolean>;
  runInspectForAsset: (assetId: string, contentSnapshot?: string) => Promise<unknown>;
  runMaterializeForAsset: (
    assetId: string,
    scope?: MaterializeScope,
    refresh?: () => Promise<void> | void
  ) => Promise<unknown>;
  refreshPipelineMaterialization: (pipelineId: string) => Promise<void>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
  clearResultsAfterDelete: () => void;
  clearPreviewForAsset: (assetId: string) => void;
};

export function useEditorActions({
  editorValue,
  flushAssetSave,
  runUpdateAsset,
  runDeleteAsset,
  runInspectForAsset,
  runMaterializeForAsset,
  refreshPipelineMaterialization,
  navigateSelection,
  clearResultsAfterDelete,
  clearPreviewForAsset,
  runUpdatePipeline,
}: UseEditorActionsInput) {
  const asset = useAtomValue(enrichedSelectedAssetAtom);
  const pipeline = useAtomValue(pipelineAtom);
  const pipelineId = pipeline?.id ?? null;
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const handleSaveVisualizationSettings = useCallback(
    (visualizationMeta: Record<string, string>) => {
      if (!asset || !pipelineId) {
        return;
      }

      const mergedMeta: Record<string, string> = {
        ...(asset.meta ?? {}),
      };

      for (const key of VISUALIZATION_META_KEYS) {
        delete mergedMeta[key];
      }

      for (const [key, value] of Object.entries(visualizationMeta)) {
        mergedMeta[key] = value;
      }

      void runUpdateAsset(pipelineId, asset.id, {
        content: editorValue,
        meta: mergedMeta,
      });
    },
    [asset, editorValue, pipelineId, runUpdateAsset]
  );

  const handleSaveManualUpstreams = useCallback(
    (upstreams: string[]) => {
      if (!asset || !pipelineId) {
        return;
      }

      // Avoid racing a pending debounced content save against the explicit
      // upstream update request for the same asset.
      flushAssetSave(asset.id);

      void runUpdateAsset(pipelineId, asset.id, {
        content: editorValue,
        upstreams,
      });
    },
    [asset, editorValue, flushAssetSave, pipelineId, runUpdateAsset]
  );

  const handleConfirmDeleteAsset = useCallback(() => {
    if (!asset || !pipelineId || deleteLoading) {
      return;
    }

    setDeleteLoading(true);
    void runDeleteAsset(pipelineId, asset.id)
      .then((deleted) => {
        if (!deleted) {
          return;
        }

        setDeleteDialogOpen(false);
        clearResultsAfterDelete();
        clearPreviewForAsset(asset.id);
        navigateSelection(pipelineId, null);
      })
      .finally(() => setDeleteLoading(false));
  }, [
    asset,
    clearPreviewForAsset,
    clearResultsAfterDelete,
    deleteLoading,
    navigateSelection,
    pipelineId,
    runDeleteAsset,
  ]);

  const handleMaterializeSelectedAsset = useCallback((scope: MaterializeScope = "asset") => {
    if (!asset) {
      return;
    }

    void runMaterializeForAsset(asset.id, scope, async () => {
      if (pipelineId) {
        await refreshPipelineMaterialization(pipelineId).catch(() => undefined);
      }
    });
  }, [
    asset,
    pipelineId,
    refreshPipelineMaterialization,
    runMaterializeForAsset,
  ]);

  const handleInspectSelectedAsset = useCallback(() => {
    if (!asset) {
      return;
    }

    void runInspectForAsset(asset.id, editorValue);
  }, [asset, editorValue, runInspectForAsset]);

  const handlePipelineNameChange = useCallback(
    (pipelineName: string) => {
      if (!pipelineId) {
        return Promise.resolve(false);
      }

      const trimmedName = pipelineName.trim();
      if (!trimmedName || trimmedName === pipeline?.name) {
        return Promise.resolve(true);
      }

      return runUpdatePipeline(pipelineId, {
        name: trimmedName,
      });
    },
    [pipeline?.name, pipelineId, runUpdatePipeline]
  );

  const handleAssetNameChange = useCallback(
    async (assetName: string) => {
      if (!asset || !pipelineId) {
        return false;
      }

      const trimmedName = assetName.trim();
      if (!trimmedName || trimmedName === asset.name) {
        return true;
      }

      const result = await runUpdateAsset(pipelineId, asset.id, {
        name: trimmedName,
        content: editorValue,
      });
      if (result?.asset_id && result.asset_id !== asset.id) {
        navigateSelection(pipelineId, result.asset_id);
      }
      return Boolean(result);
    },
    [asset, editorValue, navigateSelection, pipelineId, runUpdateAsset]
  );

  const handleMaterializationTypeChange = useCallback(
    (materializationType: string) => {
      if (!asset || !pipelineId) {
        return;
      }

      void runUpdateAsset(pipelineId, asset.id, {
        content: editorValue,
        materialization_type: materializationType,
      });
    },
    [asset, editorValue, pipelineId, runUpdateAsset]
  );

  const handleAssetTypeChange = useCallback(
    (assetType: string) => {
      if (!asset || !pipelineId) {
        return;
      }

      const trimmedType = assetType.trim();
      if (!trimmedType || trimmedType === asset.type) {
        return;
      }

      void runUpdateAsset(pipelineId, asset.id, {
        type: trimmedType,
        content: editorValue,
      });
    },
    [asset, editorValue, pipelineId, runUpdateAsset]
  );

  return {
    deleteDialogOpen,
    deleteLoading,
    setDeleteDialogOpen,
    handleSaveVisualizationSettings,
    handleSaveManualUpstreams,
    handleConfirmDeleteAsset,
    handleMaterializeSelectedAsset,
    handleInspectSelectedAsset,
    handlePipelineNameChange,
    handleAssetNameChange,
    handleAssetTypeChange,
    handleMaterializationTypeChange,
  };
}
