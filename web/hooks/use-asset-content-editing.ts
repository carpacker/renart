"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect, useRef } from "react";

import { useDebouncedAssetSave } from "@/hooks/use-debounced-asset-save";
import { refreshAssetColumnsFromDefinition } from "@/lib/api-asset-transactions";
import { fillAssetColumnsFromDB } from "@/lib/api";
import { editorDraftAtom, editorProgrammaticContentAtom } from "@/lib/atoms/domains/editor";
import { WebAsset } from "@/lib/types";

export type SaveSelectedAssetResult = false | "saved" | "already-saved";

/**
 * Content-editing state for an asset: the draft-aware editor value,
 * debounced writes to the backend on change, and an explicit save for the
 * Ctrl/Cmd+S path. Shared by the workspace editor pane and the app
 * build editor.
 */
export function useAssetContentEditing({
  asset,
  pipelineId,
  saveDelay = 500,
}: {
  asset: WebAsset | null;
  pipelineId: string | null;
  saveDelay?: number;
}) {
  const editorDraft = useAtomValue(editorDraftAtom);
  const editorProgrammaticContent = useAtomValue(editorProgrammaticContentAtom);
  const setEditorDraft = useSetAtom(editorDraftAtom);
  const { scheduleSave, flushAssetSave, flushAllSaves, hasPendingAssetSave, saveAssetNow } =
    useDebouncedAssetSave(saveDelay);
  const fillColumnsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastEditorChangeRef = useRef<{
    assetId: string;
    value: string;
  } | null>(null);
  const displayValueRef = useRef<{ assetId: string | null; value: string }>({
    assetId: null,
    value: "",
  });
  const appliedProgrammaticRevisionRef = useRef<Record<string, number>>({});

  const editorValue = asset ? (editorDraft[asset.id] ?? asset.content) : "";

  // Monaco applies a changed `value` prop by replacing the whole document in
  // a post-commit effect. While typing, that effect can run after further
  // keystrokes and wipe them out. So the value handed to the editor only
  // moves for changes that did not originate from the editor itself (asset
  // switch, SSE update, format) - the editor already has its own keystrokes.
  const assetId = asset?.id ?? null;
  const programmaticContent = assetId ? editorProgrammaticContent[assetId] : undefined;
  const hasUnappliedProgrammaticContent = Boolean(
    assetId &&
    programmaticContent &&
    appliedProgrammaticRevisionRef.current[assetId] !== programmaticContent.revision,
  );
  const lastEditorChange = lastEditorChangeRef.current;
  const changeCameFromEditor =
    assetId !== null &&
    lastEditorChange?.assetId === assetId &&
    lastEditorChange.value === editorValue;
  if (assetId && hasUnappliedProgrammaticContent && programmaticContent) {
    displayValueRef.current = { assetId, value: programmaticContent.content };
    appliedProgrammaticRevisionRef.current[assetId] = programmaticContent.revision;
  } else if (displayValueRef.current.assetId !== assetId || !changeCameFromEditor) {
    displayValueRef.current = { assetId, value: editorValue };
  }
  const editorDisplayValue = displayValueRef.current.value;

  const handleEditorChange = useCallback(
    (value?: string) => {
      const nextValue = value ?? "";
      if (!asset || !pipelineId) {
        return;
      }

      lastEditorChangeRef.current = { assetId: asset.id, value: nextValue };
      setEditorDraft((previous) => ({
        ...previous,
        [asset.id]: nextValue,
      }));
      scheduleSave(pipelineId, asset.id, nextValue);

      const isSQLAsset =
        asset.path.toLowerCase().endsWith(".sql") || asset.type.toLowerCase().includes("sql");
      const isAPIAsset = asset.type.toLowerCase() === "api";
      if (!isSQLAsset && !isAPIAsset) {
        return;
      }

      if (fillColumnsTimerRef.current) {
        clearTimeout(fillColumnsTimerRef.current);
      }

      const currentAssetID = asset.id;
      fillColumnsTimerRef.current = setTimeout(() => {
        const refresh = isAPIAsset
          ? refreshAssetColumnsFromDefinition(currentAssetID)
          : fillAssetColumnsFromDB(currentAssetID);
        void refresh.catch(() => {
          // noop: best-effort post-edit sync
        });
      }, 1200);
    },
    [asset, pipelineId, scheduleSave, setEditorDraft],
  );

  useEffect(() => {
    return () => {
      if (fillColumnsTimerRef.current) {
        clearTimeout(fillColumnsTimerRef.current);
      }
    };
  }, []);

  const handleSaveSelectedAsset = useCallback(async (): Promise<SaveSelectedAssetResult> => {
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
  }, [asset, editorValue, hasPendingAssetSave, pipelineId, saveAssetNow]);

  return {
    editorDisplayValue,
    editorValue,
    flushAllSaves,
    flushAssetSave,
    handleEditorChange,
    handleSaveSelectedAsset,
    hasPendingAssetSave,
    saveAssetNow,
    scheduleSave,
  };
}
