"use client";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { useAssetContentEditing } from "@/hooks/use-asset-content-editing";
import { useAssetMonaco } from "@/hooks/use-asset-monaco";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { WebAsset } from "@/lib/types";

/**
 * Monaco editor for a real workspace asset inside the redesign build page.
 * Reuses the same editing hooks as the classic workspace editor pane, so
 * intellisense, formatting, debounced saves, and go-to-definition behave
 * identically. Requires the build page to keep `routeSelectionAtom` in sync
 * with the asset being shown.
 */
export function RedesignAssetEditor({
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
  const {
    editorDisplayValue,
    editorValue,
    handleEditorChange,
    handleSaveSelectedAsset,
  } = useAssetContentEditing({ asset, pipelineId });
  const {
    editorModelPath,
    formatSQL,
    handleBeforeMount,
    handleMount,
    isSqlAsset,
    shortcutLabel,
  } = useAssetMonaco({
    asset,
    editorValue,
    onGoToAsset,
    onInspect,
    onSave: handleSaveSelectedAsset,
  });

  return (
    <AssetCodeEditor
      asset={asset}
      containerClassName="min-h-0 flex-1"
      editorModelPath={editorModelPath}
      editorValue={editorDisplayValue}
      editorHighlighted={false}
      helpMode={false}
      isSqlAsset={isSqlAsset}
      formatShortcutLabel={shortcutLabel}
      mobile={false}
      monacoTheme={monacoTheme}
      onChange={handleEditorChange}
      onBeforeMount={handleBeforeMount}
      onFormat={formatSQL}
      onMount={handleMount}
    />
  );
}
