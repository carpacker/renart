"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";

import { fillAssetColumnsFromDB } from "@/lib/api";
import { editorDraftAtom } from "@/lib/atoms/domains/editor";
import { enrichedSelectedAssetAtom } from "@/lib/atoms/domains/results";
import { pipelineAtom } from "@/lib/atoms/domains/workspace";
import { VISUALIZATION_META_KEYS } from "@/lib/visualization-meta";

type UseEditorActionsInput = {
  editorValue: string;
  scheduleSave: (pipelineId: string, assetId: string, content: string) => void;
  flushAssetSave: (assetId: string) => void;
  saveAssetNow: (
    pipelineId: string,
    assetId: string,
    content: string
  ) => Promise<boolean>;
  hasPendingAssetSave: (assetId: string) => boolean;
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
  ) => Promise<boolean>;
  runDeleteAsset: (pipelineId: string, assetId: string) => Promise<boolean>;
  runUpdatePipeline: (
    pipelineId: string,
    input: { name?: string; content?: string }
  ) => Promise<boolean>;
  runInspectForAsset: (assetId: string, contentSnapshot?: string) => Promise<unknown>;
  runMaterializeForAsset: (
    assetId: string,
    refresh?: () => Promise<void> | void
  ) => Promise<unknown>;
  refreshPipelineMaterialization: (pipelineId: string) => Promise<void>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
  clearResultsAfterDelete: () => void;
  clearPreviewForAsset: (assetId: string) => void;
};

export function useEditorActions({
  editorValue,
  scheduleSave,
  flushAssetSave,
  runUpdateAsset,
  runDeleteAsset,
  runInspectForAsset,
  runMaterializeForAsset,
  refreshPipelineMaterialization,
  navigateSelection,
  clearResultsAfterDelete,
  clearPreviewForAsset,
  hasPendingAssetSave,
  saveAssetNow,
  runUpdatePipeline,
}: UseEditorActionsInput) {
  const asset = useAtomValue(enrichedSelectedAssetAtom);
  const pipeline = useAtomValue(pipelineAtom);
  const pipelineId = pipeline?.id ?? null;
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const fillColumnsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null
  );
  const setEditorDraft = useSetAtom(editorDraftAtom);

  const handleEditorChange = useCallback(
    (value?: string) => {
      const nextValue = value ?? "";
      if (!asset || !pipelineId) {
        return;
      }

      setEditorDraft((previous) => ({
        ...previous,
        [asset.id]: nextValue,
      }));
      scheduleSave(pipelineId, asset.id, nextValue);

      const isSQLAsset =
        asset.path.toLowerCase().endsWith(".sql") ||
        asset.type.toLowerCase().includes("sql");
      if (!isSQLAsset) {
        return;
      }

      if (fillColumnsTimerRef.current) {
        clearTimeout(fillColumnsTimerRef.current);
      }

      const currentAssetID = asset.id;
      fillColumnsTimerRef.current = setTimeout(() => {
        void fillAssetColumnsFromDB(currentAssetID).catch(() => {
          // noop: best-effort post-edit sync
        });
      }, 1200);
    },
    [asset, pipelineId, scheduleSave, setEditorDraft]
  );

  useEffect(() => {
    return () => {
      if (fillColumnsTimerRef.current) {
        clearTimeout(fillColumnsTimerRef.current);
      }
    };
  }, []);

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

  const handleMaterializeSelectedAsset = useCallback(() => {
    if (!asset) {
      return;
    }

    void runMaterializeForAsset(asset.id, async () => {
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

  const handleSaveSelectedAsset = useCallback(async () => {
    if (!asset || !pipelineId) {
      return false;
    }

    const hasPendingSave = hasPendingAssetSave(asset.id);
    const hasUnsavedChanges = hasPendingSave || editorValue !== asset.content;

    if (!hasUnsavedChanges) {
      return "already-saved";
    }

    const saved = await saveAssetNow(pipelineId, asset.id, editorValue);
    return saved ? "saved" : false;
  }, [
    asset,
    editorValue,
    hasPendingAssetSave,
    pipelineId,
    saveAssetNow,
  ]);

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

      return runUpdateAsset(pipelineId, asset.id, {
        name: trimmedName,
        content: editorValue,
      });
    },
    [asset, editorValue, pipelineId, runUpdateAsset]
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
    handleEditorChange,
    handleSaveVisualizationSettings,
    handleSaveManualUpstreams,
    handleConfirmDeleteAsset,
    handleMaterializeSelectedAsset,
    handleInspectSelectedAsset,
    handleSaveSelectedAsset,
    handlePipelineNameChange,
    handleAssetNameChange,
    handleAssetTypeChange,
    handleMaterializationTypeChange,
  };
}
