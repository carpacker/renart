"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useAssetMonaco } from "@/hooks/use-asset-monaco";
import { useDebouncedAssetSave } from "@/hooks/use-debounced-asset-save";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { refreshAssetColumnsFromDefinition } from "@/lib/api-asset-transactions";
import { extractParametersText, hasIncompletePlainYAMLKeyLine, spliceParametersText } from "@/lib/api-parameters-yaml";
import { WebAsset } from "@/lib/types";

/**
 * Editor for an API asset that exposes only the request `parameters:` block in
 * Monaco. Everything else (type, connection, columns, meta) is managed through
 * the guided properties panel, so the code surface stays focused on the request
 * spec. Edits are spliced back into the full file, preserving the rest verbatim.
 *
 * Mount this with a `key` of the asset id so switching assets re-seeds the block
 * from the newly-selected file.
 */
export function ApiParametersEditor({
  asset,
  pipelineId,
  onInspect,
  onGoToAsset,
}: {
  asset: WebAsset;
  pipelineId: string;
  onInspect?: () => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const { saveAssetNow } = useDebouncedAssetSave();

  // The whole file is the source of truth on disk; keep the latest copy so a
  // splice always writes edits into the freshest surrounding content (columns,
  // connection, etc. changed via the properties panel or a column refresh).
  const latestContentRef = useRef(asset.content);
  latestContentRef.current = asset.content;

  const [block, setBlock] = useState(() => extractParametersText(asset.content));
  // The block awaiting a debounced write, spliced at fire time (not keystroke
  // time) so a concurrent metadata write isn't clobbered by a stale snapshot.
  const pendingBlockRef = useRef<string | null>(null);
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const columnsTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // A direct navigation can mount this editor before the workspace content
  // model has the asset's file content, seeding an empty block that would
  // otherwise stick forever. Re-seed when content arrives, but never over
  // unsaved keystrokes.
  useEffect(() => {
    if (pendingBlockRef.current !== null || saveTimerRef.current) {
      return;
    }
    const next = extractParametersText(asset.content);
    setBlock((current) => (current === "" && next !== "" ? next : current));
  }, [asset.content]);

  const flushSave = useCallback(() => {
    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
    const pending = pendingBlockRef.current;
    if (pending === null) {
      return;
    }
    pendingBlockRef.current = null;
    void saveAssetNow(pipelineId, asset.id, spliceParametersText(latestContentRef.current, pending));
  }, [asset.id, pipelineId, saveAssetNow]);

  const handleChange = useCallback(
    (value?: string) => {
      const next = value ?? "";
      setBlock(next);
      pendingBlockRef.current = next;

      if (saveTimerRef.current) {
        clearTimeout(saveTimerRef.current);
      }
      saveTimerRef.current = setTimeout(flushSave, 500);

      // Keep inferred columns in sync with the edited request/response spec.
      if (columnsTimerRef.current) {
        clearTimeout(columnsTimerRef.current);
        columnsTimerRef.current = null;
      }
      if (hasIncompletePlainYAMLKeyLine(next)) {
        return;
      }
      const assetId = asset.id;
      columnsTimerRef.current = setTimeout(() => {
        void refreshAssetColumnsFromDefinition(assetId).catch(() => {
          // best-effort post-edit sync
        });
      }, 1200);
    },
    [asset.id, flushSave]
  );

  // Flush a pending save when the editor unmounts (asset switch / navigation).
  useEffect(() => {
    return () => {
      if (columnsTimerRef.current) {
        clearTimeout(columnsTimerRef.current);
      }
      flushSave();
    };
  }, [flushSave]);

  const {
    editorModelPath,
    formatSQL,
    handleBeforeMount,
    handleMount,
    isSqlAsset,
    shortcutLabel,
  } = useAssetMonaco({
    asset,
    editorValue: block,
    onGoToAsset,
    onInspect,
    onSave: flushSave,
  });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <AssetCodeEditor
        asset={asset}
        containerClassName="min-h-0 flex-1"
        editorModelPath={editorModelPath}
        editorValue={block}
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
    </div>
  );
}
