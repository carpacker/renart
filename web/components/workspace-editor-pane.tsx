"use client";

import { CSSProperties } from "react";
import { Eye, Network, Settings2 } from "lucide-react";
import { UseFormReturn } from "react-hook-form";
import { Panel } from "react-resizable-panels";

import { AssetCodeEditor } from "@/components/asset-code-editor";
import { AssetEditorConfigurationTab } from "@/components/asset-editor-configuration-tab";
import { AssetEditorDependenciesTab } from "@/components/asset-editor-dependencies-tab";
import { AssetEditorHeader } from "@/components/asset-editor-header";
import { AssetEditorVisualizationTab } from "@/components/asset-editor-visualization-tab";
import { WorkspaceEditorFooter } from "@/components/workspace-editor-footer";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { useAssetMonaco } from "@/hooks/use-asset-monaco";
import { useWorkspaceEditorDerivedState } from "@/hooks/use-workspace-editor-derived-state";
import { MaterializeScope } from "@/lib/materialize-scope";
import { WebAsset } from "@/lib/types";
import { InspectDiagnosticSnapshot } from "@/lib/inspect-diagnostics";

export type AssetConfigForm = {
  type: string;
  materialization: string;
  custom_checks: string;
  columns: string;
};

type WorkspaceEditorPaneProps = {
  asset: WebAsset | null;
  pipelineId: string | null;
  helpMode: boolean;
  actionHighlighted: boolean;
  editorHighlighted: boolean;
  visualizationHighlighted: boolean;
  highlightStyle?: CSSProperties;
  materializeLoading: boolean;
  inspectLoading: boolean;
  deleteLoading: boolean;
  assetRenameLoading?: boolean;
  editorValue: string;
  editorDisplayValue: string;
  monacoTheme: string;
  assetEditorTab: "configuration" | "checks" | "visualization" | "dependencies";
  form: UseFormReturn<AssetConfigForm>;
  assetPreviewRows: Record<string, unknown>[];
  inspectDiagnosticSnapshot?: InspectDiagnosticSnapshot | null;
  onEditorTabChange: (
    value: "configuration" | "checks" | "visualization" | "dependencies"
  ) => void;
  onEditorChange: (value?: string) => void;
  onMaterializeSelectedAsset: (scope?: MaterializeScope) => void;
  onInspectSelectedAsset: () => void;
  onSaveSelectedAsset: () => Promise<false | "saved" | "already-saved">;
  onOpenDeleteDialog: () => void;
  onAssetNameChange: (assetName: string) => Promise<boolean>;
  onAssetTypeChange: (assetType: string) => void;
  onMaterializationTypeChange: (materializationType: string) => void;
  onSaveVisualizationSettings: (
    visualizationMeta: Record<string, string>
  ) => void;
  onSaveManualUpstreams: (upstreams: string[]) => void;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
  mobile?: boolean;
  availableAssetTypes: string[];
  availableDependencyNames: string[];
};

export function WorkspaceEditorPane({
  asset,
  pipelineId,
  helpMode,
  actionHighlighted,
  editorHighlighted,
  visualizationHighlighted,
  highlightStyle,
  materializeLoading,
  inspectLoading,
  deleteLoading,
  assetRenameLoading,
  editorValue,
  editorDisplayValue,
  monacoTheme,
  assetEditorTab,
  form,
  assetPreviewRows,
  inspectDiagnosticSnapshot,
  onEditorTabChange,
  onEditorChange,
  onMaterializeSelectedAsset,
  onInspectSelectedAsset,
  onSaveSelectedAsset,
  onOpenDeleteDialog,
  onAssetNameChange,
  onAssetTypeChange,
  onMaterializationTypeChange,
  onSaveVisualizationSettings,
  onSaveManualUpstreams,
  onGoToAsset,
  mobile = false,
  availableAssetTypes,
  availableDependencyNames,
}: WorkspaceEditorPaneProps) {
  const selectedAssetType = form.watch("type");
  const {
    activeConfigEnvironment,
    assetInspectColumns,
    debugResolvedUpstreamTables,
    declaredColumnNames,
    inferredUpstreamNames,
    manualUpstreamNames,
    mergedColumnNames,
    requiredConnectionType,
    schemaSuggestionTables,
    schemaTables,
    showMissingConnectionWarning,
  } = useWorkspaceEditorDerivedState({
    asset,
    selectedAssetType,
  });

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
    inspectDiagnosticSnapshot: inspectDiagnosticSnapshot ?? null,
    onGoToAsset,
    onInspect: onInspectSelectedAsset,
    onSave: onSaveSelectedAsset,
  });

  const content = (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <AssetEditorHeader
        assetName={asset?.name}
        assetPath={asset?.path}
        helpMode={helpMode}
        actionHighlighted={actionHighlighted}
        highlightStyle={highlightStyle}
        hasAsset={Boolean(asset)}
        assetRenameLoading={assetRenameLoading}
        materializeLoading={materializeLoading}
        inspectLoading={inspectLoading}
        deleteLoading={deleteLoading}
        onRenameAsset={onAssetNameChange}
        onMaterialize={onMaterializeSelectedAsset}
        onInspect={onInspectSelectedAsset}
        onDelete={onOpenDeleteDialog}
      />

      <div className="flex min-h-0 min-w-0 flex-1 overflow-hidden">
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <AssetCodeEditor
            asset={asset}
            editorModelPath={editorModelPath}
            editorValue={editorDisplayValue}
            editorHighlighted={editorHighlighted}
            helpMode={helpMode}
            highlightStyle={highlightStyle}
            formatShortcutLabel={shortcutLabel}
            isSqlAsset={isSqlAsset}
            mobile={mobile}
            monacoTheme={monacoTheme}
            onChange={onEditorChange}
            onBeforeMount={handleBeforeMount}
            onFormat={formatSQL}
            onMount={handleMount}
          />

          <ScrollArea className="min-h-0 min-w-0 flex-1" viewportClassName="p-4">
            <Tabs
              className="flex min-w-0 flex-1 flex-col"
              onValueChange={(value) =>
                onEditorTabChange(
                  value as
                    | "configuration"
                    | "checks"
                    | "visualization"
                    | "dependencies"
                )
              }
              value={assetEditorTab}
            >
                <TabsList
                  className={`@container/tabs-list grid w-full min-w-0 grid-cols-3 ${
                    helpMode && visualizationHighlighted
                      ? "quickstart-tour-spotlight quickstart-tour-halo ring-2 ring-primary/70 ring-offset-2"
                      : ""
                  }`}
                  style={
                    helpMode && visualizationHighlighted
                      ? highlightStyle
                      : undefined
                  }
                >
                  <TabsTrigger value="configuration">
                    <Settings2 className="size-4" />
                    <span className="truncate @max-[360px]/tabs-list:hidden">Configuration</span>
                  </TabsTrigger>
                  <TabsTrigger value="dependencies">
                    <Network className="size-4" />
                    <span className="truncate @max-[360px]/tabs-list:hidden">Dependencies</span>
                  </TabsTrigger>
                  <TabsTrigger data-testid="editor-preview-tab" value="visualization">
                    <Eye className="size-4" />
                    <span className="truncate @max-[360px]/tabs-list:hidden">Preview</span>
                  </TabsTrigger>
                </TabsList>

                <TabsContent className="mt-3 space-y-3" value="configuration">
                  <AssetEditorConfigurationTab
                    activeConfigEnvironmentName={activeConfigEnvironment?.name}
                    availableAssetTypes={availableAssetTypes}
                    form={form}
                    onAssetTypeChange={onAssetTypeChange}
                    onMaterializationTypeChange={onMaterializationTypeChange}
                    requiredConnectionType={requiredConnectionType}
                    showMissingConnectionWarning={showMissingConnectionWarning}
                  />
                </TabsContent>

                <TabsContent className="mt-3 space-y-3" value="checks">
                  <div className="grid gap-1">
                    <Label>Custom checks</Label>
                    <Textarea
                      className="min-h-24"
                      {...form.register("custom_checks")}
                    />
                  </div>
                </TabsContent>

                <TabsContent className="mt-3 space-y-3" value="dependencies">
                  <AssetEditorDependenciesTab
                    assetName={asset?.name}
                    manualUpstreamNames={manualUpstreamNames}
                    inferredUpstreamNames={inferredUpstreamNames}
                    availableDependencyNames={availableDependencyNames}
                    disabled={!pipelineId}
                    onSaveManualUpstreams={onSaveManualUpstreams}
                  />
                </TabsContent>

                <TabsContent className="mt-3 space-y-3" value="visualization">
                  <AssetEditorVisualizationTab
                    asset={asset}
                    assetPreviewRows={assetPreviewRows}
                    columnNames={mergedColumnNames}
                    monacoTheme={monacoTheme}
                    pipelineId={pipelineId}
                    onSaveVisualizationSettings={onSaveVisualizationSettings}
                  />
                </TabsContent>
            </Tabs>

            <div className="mt-3">
              <WorkspaceEditorFooter
                asset={asset}
                assetInspectColumns={assetInspectColumns}
                debugResolvedUpstreamTables={debugResolvedUpstreamTables}
                declaredColumnNames={declaredColumnNames}
                mergedColumnNames={mergedColumnNames}
                schemaSuggestionTables={schemaSuggestionTables}
                schemaTablesCount={schemaTables.length}
              />
            </div>
          </ScrollArea>
        </div>
        </div>
    </div>
  );

  if (mobile) {
    return content;
  }

  return <Panel defaultSize={32} minSize={24}>{content}</Panel>;
}
