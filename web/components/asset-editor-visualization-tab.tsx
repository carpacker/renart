"use client";

import { lazy, Suspense } from "react";

import { WebAsset } from "@/lib/types";

const VisualizationSettingsEditor = lazy(async () => {
  const module = await import("@/components/visualization-settings-editor");
  return { default: module.VisualizationSettingsEditor };
});

export function AssetEditorVisualizationTab({
  asset,
  assetPreviewRows,
  columnNames,
  monacoTheme,
  pipelineId,
  onSaveVisualizationSettings,
}: {
  asset: WebAsset | null;
  assetPreviewRows: Record<string, unknown>[];
  columnNames: string[];
  monacoTheme: string;
  pipelineId: string | null;
  onSaveVisualizationSettings: (visualizationMeta: Record<string, string>) => void;
}) {
  if (!asset) {
    return null;
  }

  return (
    <Suspense
      fallback={
        <div className="rounded border bg-muted/20 p-4 text-sm text-muted-foreground">
          Loading visualization settings...
        </div>
      }
    >
      <VisualizationSettingsEditor
        columnNames={columnNames}
        disabled={!pipelineId}
        key={asset.id}
        meta={asset.meta}
        monacoTheme={monacoTheme}
        previewRows={assetPreviewRows}
        onSave={onSaveVisualizationSettings}
      />
    </Suspense>
  );
}
