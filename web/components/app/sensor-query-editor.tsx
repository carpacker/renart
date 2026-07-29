"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useAssetMonaco } from "@/hooks/use-asset-monaco";
import { useWorkspaceSaveParticipant } from "@/hooks/use-workspace-save-participant";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import type { WebAsset } from "@/lib/types";

const SENSOR_QUERY_SAVE_DELAY = 500;

export function SensorQueryEditor({
  asset,
  query: externalQuery,
  onSave,
  onCheck,
  onGoToAsset,
  onGoToJinjaVariable,
}: {
  asset: WebAsset;
  query: string;
  onSave: (query: string) => Promise<void>;
  onCheck?: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
  onGoToJinjaVariable?: (variableName: string) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const [query, setQuery] = useState(externalQuery);
  const pendingQueryRef = useRef<string | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savesInFlightRef = useRef(new Set<Promise<void>>());
  const lastSaveRef = useRef<Promise<void>>(Promise.resolve());
  const saveFailureRef = useRef<{ error: unknown } | null>(null);
  const saveSequenceRef = useRef(0);

  useEffect(() => {
    if (
      pendingQueryRef.current !== null ||
      saveTimerRef.current !== null ||
      savesInFlightRef.current.size > 0
    ) {
      return;
    }
    setQuery(externalQuery);
  }, [externalQuery]);

  const flushSave = useCallback(async () => {
    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
    const pendingQuery = pendingQueryRef.current;
    if (pendingQuery === null) {
      return;
    }

    pendingQueryRef.current = null;
    const saveSequence = ++saveSequenceRef.current;
    // Preserve edit order even when another debounce fires while a request is
    // still in flight. A failed older value must not overwrite a newer save.
    const save = lastSaveRef.current
      .catch(() => undefined)
      .then(() => onSave(pendingQuery))
      .then(
        () => {
          saveFailureRef.current = null;
        },
        (error) => {
          saveFailureRef.current = { error };
          if (saveSequenceRef.current === saveSequence && pendingQueryRef.current === null) {
            pendingQueryRef.current = pendingQuery;
          }
          throw error;
        },
      );
    const tracked: Promise<void> = save.then(
      () => {
        savesInFlightRef.current.delete(tracked);
      },
      (error) => {
        savesInFlightRef.current.delete(tracked);
        throw error;
      },
    );
    lastSaveRef.current = tracked;
    savesInFlightRef.current.add(tracked);
    void tracked.catch(() => undefined);
    await tracked;
  }, [onSave]);

  const awaitPendingSaves = useCallback(async () => {
    while (true) {
      await flushSave();
      const active = Array.from(savesInFlightRef.current);
      if (active.length > 0) {
        await Promise.allSettled(active);
      }
      if (saveFailureRef.current) {
        throw saveFailureRef.current.error;
      }
      if (pendingQueryRef.current === null && savesInFlightRef.current.size === 0) {
        return;
      }
    }
  }, [flushSave]);
  useWorkspaceSaveParticipant(awaitPendingSaves);

  const handleChange = useCallback(
    (value?: string) => {
      const next = value ?? "";
      setQuery(next);
      pendingQueryRef.current = next;
      if (saveTimerRef.current) {
        clearTimeout(saveTimerRef.current);
      }
      saveTimerRef.current = setTimeout(() => {
        void flushSave().catch(() => undefined);
      }, SENSOR_QUERY_SAVE_DELAY);
    },
    [flushSave],
  );

  useEffect(() => {
    return () => {
      if (saveTimerRef.current) {
        clearTimeout(saveTimerRef.current);
        saveTimerRef.current = null;
      }
      void flushSave().catch(() => undefined);
    };
  }, [flushSave]);

  const handleCheck = useCallback(() => {
    void flushSave()
      .then(() => onCheck?.())
      .catch(() => undefined);
  }, [flushSave, onCheck]);

  const handleEditorSave = useCallback(() => {
    void flushSave().catch(() => undefined);
  }, [flushSave]);

  const { editorModelPath, formatSQL, handleBeforeMount, handleMount, isSqlAsset, shortcutLabel } =
    useAssetMonaco({
      asset,
      editorValue: query,
      onGoToAsset,
      onGoToJinjaVariable,
      onInspect: handleCheck,
      onSave: handleEditorSave,
    });

  return (
    <AssetCodeEditor
      asset={asset}
      containerClassName="min-h-0 flex-1"
      editorModelPath={editorModelPath}
      editorValue={query}
      editorHighlighted={false}
      helpMode={false}
      isSqlAsset={isSqlAsset}
      formatShortcutLabel={shortcutLabel}
      mobile={false}
      monacoTheme={monacoTheme}
      onChange={handleChange}
      onBeforeMount={handleBeforeMount}
      onFormat={formatSQL}
      onMount={handleMount}
    />
  );
}
