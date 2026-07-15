"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useAssetMonaco } from "@/hooks/use-asset-monaco";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import type { WebAsset } from "@/lib/types";

const SENSOR_QUERY_SAVE_DELAY = 500;

export function SensorQueryEditor({
  asset,
  query: externalQuery,
  onSave,
  onCheck,
  onGoToAsset,
}: {
  asset: WebAsset;
  query: string;
  onSave: (query: string) => Promise<void>;
  onCheck?: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const [query, setQuery] = useState(externalQuery);
  const pendingQueryRef = useRef<string | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savesInFlightRef = useRef(0);

  useEffect(() => {
    if (
      pendingQueryRef.current !== null ||
      saveTimerRef.current !== null ||
      savesInFlightRef.current > 0
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

    savesInFlightRef.current += 1;
    try {
      await onSave(pendingQuery);
      if (pendingQueryRef.current === pendingQuery) {
        pendingQueryRef.current = null;
      }
    } finally {
      savesInFlightRef.current = Math.max(0, savesInFlightRef.current - 1);
    }
  }, [onSave]);

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

  const { editorModelPath, formatSQL, handleBeforeMount, handleMount, isSqlAsset, shortcutLabel } =
    useAssetMonaco({
      asset,
      editorValue: query,
      onGoToAsset,
      onInspect: handleCheck,
      onSave: flushSave,
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
