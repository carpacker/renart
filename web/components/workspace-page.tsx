"use client";

import { useAtom, useAtomValue } from "jotai";
import { useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import "reactflow/dist/style.css";

import { WorkspaceAssetDialogs } from "@/components/workspace-asset-dialogs";
import { WorkspaceMainContent } from "@/components/workspace-main-content";
import { WorkspacePageEffects } from "@/components/workspace-page-effects";
import {
  AssetConfigForm,
  WorkspaceEditorPane,
} from "@/components/workspace-editor-pane";
import {
  assetEditorTabAtom,
  editorValueAtom,
} from "@/lib/atoms/domains/editor";
import {
  enrichedSelectedAssetAtom,
} from "@/lib/atoms/domains/results";
import { workspaceAtom, workspaceSyncSourceAtom } from "@/lib/atoms/domains/workspace";
import { routeSelectionAtom } from "@/lib/atoms/domains/workspace";
import { useIsMobile } from "@/hooks/use-mobile";
import {
  computeGraphLayoutPositions,
} from "@/lib/graph";
import { buildCreateAssetInput } from "@/lib/workspace-shell-helpers";
import { useAssetCanvasInteractions } from "@/hooks/use-asset-canvas-interactions";
import { useDebouncedAssetSave } from "@/hooks/use-debounced-asset-save";
import { useEditorActions } from "@/hooks/use-editor-actions";
import { useWorkspaceGraphController } from "@/hooks/use-workspace-graph-controller";
import { useWorkspaceOnboardingController } from "@/hooks/use-workspace-onboarding-controller";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { useWorkspaceDerivedState } from "@/hooks/use-workspace-derived-state";
import { getAvailableAssetTypes } from "@/lib/asset-types";
import { getWorkspace } from "@/lib/api";

import { useWorkspaceLayout } from "./workspace-layout";

export function WorkspacePage() {
  const {
    assetActions,
    assetResults,
    monacoTheme,
    navigateSelection,
    pipeline,
    pipelineMaterialization,
    sidebarOnboardingMount,
    selectedAsset,
    setSidebarState,
    workspace,
  } = useWorkspaceLayout();
  const [assetEditorTab, setAssetEditorTab] = useAtom(assetEditorTabAtom);
  const editorValue = useAtomValue(editorValueAtom);
  const asset = useAtomValue(enrichedSelectedAssetAtom);
  const routeSelection = useAtomValue(routeSelectionAtom);
  const setWorkspace = useSetAtom(workspaceAtom);
  const setWorkspaceSyncSource = useSetAtom(workspaceSyncSourceAtom);
  const isMobile = useIsMobile();
  const [mobileEditorOpen, setMobileEditorOpen] = useState(false);
  const [assetRenameLoading, setAssetRenameLoading] = useState(false);

  const { scheduleSave, flushAssetSave, hasPendingAssetSave, saveAssetNow } =
    useDebouncedAssetSave(500);
  const {
    workspaceConfig,
    handleCreateWorkspaceConnection,
    handleUpdateWorkspaceConnection,
    loadWorkspaceConfig,
  } = useWorkspaceSettingsData();

  const { enrichedPipeline, refreshPipelineMaterialization } =
    pipelineMaterialization;
  const { existingAssetNames, defaultAssetNamesByKind, visualAssets } =
    useWorkspaceDerivedState({
      workspace,
      enrichedPipeline,
    });

  const {
    areVisualPreviewsReady,
    assetPreviewRows,
    canvasContainerRef,
    clearPreviewForAsset,
    connectedNodeIDs,
    edges,
    graph,
    handleRecomputeGraph,
    inspectByAssetId,
    inspectLoadingByAssetId,
    nodeTypes,
    nodes,
    onEdgesChange,
    onNodesChange,
    reactFlowInstance,
    setEdges,
    setNodes,
    setReactFlowInstance,
    setStoredNodePositions,
    storedNodePositions,
  } = useWorkspaceGraphController({
    asset,
    enrichedPipeline,
    selectedAssetId: selectedAsset,
    selectedInspectRows: assetResults.inspectResult?.rows,
    visualAssets,
  });

  const form = useForm<AssetConfigForm>({
    defaultValues: {
      type: "",
      materialization: "",
      custom_checks: "",
      columns: "",
    },
  });

  const {
    helpMode,
    helpPulseStyle,
    onboardingContent,
    onboardingHelp,
  } = useWorkspaceOnboardingController({
    editorValue,
    existingAssetNames,
    navigateSelection,
    openCreatePipelineDialog: assetActions.openCreatePipelineDialog,
    pipeline,
    refreshPipelineMaterialization,
    runCreateAsset: assetActions.runCreateAsset,
    runUpdateAsset: assetActions.runUpdateAsset,
    setMaterializeBatchResult: assetResults.setMaterializeBatchResult,
    setSidebarState,
  });

  const {
    handlePaneClick,
    handlePaneContextMenu,
    handleNodeDragStop,
    handleNodeClick,
  } = useAssetCanvasInteractions({
    reactFlowInstance,
    canvasContainerRef,
    graphNodes: graph.nodes,
    graphEdges: graph.edges,
    connectedNodeIDs,
    storedNodePositions,
    setStoredNodePositions,
    defaultAssetNamesByKind,
    setNodes,
    setEdges,
    runCreateAsset: assetActions.runCreateAsset,
    navigateSelection,
    isMobile,
    openSelectedAssetEditor: () => setMobileEditorOpen(true),
    buildCreateAssetInput,
  });

  const {
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
    handleAssetNameChange,
    handleAssetTypeChange,
    handleMaterializationTypeChange,
  } = useEditorActions({
    editorValue,
    scheduleSave,
    flushAssetSave,
    saveAssetNow,
    hasPendingAssetSave,
    runUpdateAsset: assetActions.runUpdateAsset,
    runDeleteAsset: assetActions.runDeleteAsset,
    runUpdatePipeline: assetActions.runUpdatePipeline,
    runInspectForAsset: assetResults.runInspectForAsset,
    runMaterializeForAsset: assetResults.runMaterializeForAsset,
    refreshPipelineMaterialization,
    navigateSelection,
    clearResultsAfterDelete: assetResults.clearResultsAfterDelete,
    clearPreviewForAsset,
  });

  const handleRunPipeline = () => {
    if (!pipeline || assetResults.materializeLoading) {
      return;
    }

    void assetResults.runMaterializePipeline(pipeline.id, async () => {
      await refreshPipelineMaterialization(pipeline.id).catch(() => undefined);
    });
  };

  const handleSaveSelectedAssetShortcut = useCallback(async () => {
    const result = await handleSaveSelectedAsset();

    if (result === "saved") {
      assetActions.pushUIMessage("success", "Saved.");
    } else if (result === "already-saved") {
      assetActions.pushUIMessage("success", "Already saved.");
    }

    return result;
  }, [assetActions, handleSaveSelectedAsset]);

  const handleAssetRename = useCallback(
    async (assetName: string) => {
      setAssetRenameLoading(true);
      try {
        return await handleAssetNameChange(assetName);
      } finally {
        setAssetRenameLoading(false);
      }
    },
    [handleAssetNameChange]
  );

  const handleSelectMaterializeEntry = useCallback(
    (entryId: string) => {
      assetResults.selectMaterializeEntry(entryId);

      const entry = assetResults.materializeHistory.find((item) => item.id === entryId);
      if (entry?.assetId && pipeline?.id) {
        navigateSelection(pipeline.id, entry.assetId);
      }
    },
    [assetResults, navigateSelection, pipeline?.id]
  );

  const hasPipelines = workspace.pipelines.length > 0;
  const availableAssetTypes = useMemo(
    () => getAvailableAssetTypes(workspaceConfig?.connection_types ?? []),
    [workspaceConfig?.connection_types]
  );
  const availableDependencyNames = useMemo(
    () =>
      (enrichedPipeline?.assets ?? [])
        .map((candidate) => candidate.name)
        .filter((name): name is string => Boolean(name)),
    [enrichedPipeline?.assets]
  );
  const editorPaneProps = {
    asset,
    pipelineId: pipeline?.id ?? null,
    helpMode,
    actionHighlighted: onboardingHelp.target === "actions",
    editorHighlighted: onboardingHelp.target === "editor",
    visualizationHighlighted: onboardingHelp.target === "visualization",
    highlightStyle: helpPulseStyle,
    materializeLoading: assetResults.materializeLoading,
    inspectLoading: assetResults.inspectLoading,
    deleteLoading,
    assetRenameLoading,
    editorValue,
    monacoTheme,
    assetEditorTab,
    form,
    assetPreviewRows,
    inspectDiagnosticSnapshot: selectedAsset
      ? assetResults.inspectDiagnosticSnapshotByAssetId[selectedAsset] ?? null
      : null,
    onEditorTabChange: setAssetEditorTab,
    onEditorChange: handleEditorChange,
    onMaterializeSelectedAsset: handleMaterializeSelectedAsset,
    onInspectSelectedAsset: handleInspectSelectedAsset,
    onSaveSelectedAsset: handleSaveSelectedAssetShortcut,
    onOpenDeleteDialog: () => setDeleteDialogOpen(true),
    onAssetNameChange: handleAssetRename,
    onAssetTypeChange: handleAssetTypeChange,
    onMaterializationTypeChange: handleMaterializationTypeChange,
    onSaveVisualizationSettings: handleSaveVisualizationSettings,
    onSaveManualUpstreams: handleSaveManualUpstreams,
    onGoToAsset: navigateSelection,
    availableAssetTypes,
    availableDependencyNames,
  } as const;

  const editorPane = <WorkspaceEditorPane {...editorPaneProps} mobile={isMobile} />;
  const handleReloadWorkspace = useCallback(async () => {
    const data = await getWorkspace();
    setWorkspace(data);
    setWorkspaceSyncSource({
      method: "workspace-load",
      recordedAt: new Date().toISOString(),
      revision: data.revision,
    });
  }, [setWorkspace, setWorkspaceSyncSource]);

  return (
    <>
      <WorkspacePageEffects
        asset={asset}
        enrichedPipeline={enrichedPipeline}
        areVisualPreviewsReady={areVisualPreviewsReady}
        inspectByAssetId={inspectByAssetId}
        inspectLoadingByAssetId={inspectLoadingByAssetId}
        storedNodePositions={storedNodePositions}
        setStoredNodePositions={setStoredNodePositions}
        computeInitialPositions={() =>
          computeGraphLayoutPositions(
            enrichedPipeline,
            inspectByAssetId,
            inspectLoadingByAssetId
          )
        }
        form={form}
        isMobile={isMobile}
        setMobileEditorOpen={setMobileEditorOpen}
        selectedAsset={selectedAsset}
        routeSelectedAsset={routeSelection.asset}
        flushAssetSave={flushAssetSave}
        sidebarOnboardingMount={sidebarOnboardingMount}
        onboardingContent={onboardingContent}
      />
      <WorkspaceMainContent
        hasPipelines={hasPipelines}
        isMobile={isMobile}
        mobileEditorOpen={mobileEditorOpen}
        setMobileEditorOpen={setMobileEditorOpen}
        assetPath={asset?.path}
        editorPane={editorPane}
        emptyStateAction={assetActions.openCreatePipelineDialog}
        canvasPaneProps={{
          highlighted: helpMode && onboardingHelp.target === "canvas",
          highlightStyle: helpPulseStyle,
          hasResultData: assetResults.hasResultData,
          canvasContainerRef,
          nodes,
          edges,
          nodeTypes,
          inspectResult: assetResults.inspectResult,
          inspectLoading: assetResults.inspectLoading,
          inspectMeta: asset?.meta,
          materializeLoading: assetResults.materializeLoading,
          pipelineMaterializeLoading: assetResults.pipelineMaterializeLoading,
          hasInspectData: assetResults.hasInspectData,
          effectiveResultTab: assetResults.effectiveResultTab,
          selectedMaterializeEntry: assetResults.selectedMaterializeEntry,
          materializeHistory: assetResults.materializeHistory,
          materializeOutputHtml: assetResults.materializeOutputHtml,
          canLoadMoreInspectRows: assetResults.canLoadMoreInspectRows,
          onLoadMoreInspectRows: assetResults.loadMoreInspectRows,
          onResultTabChange: assetResults.setResultTab,
          onSelectMaterializeEntry: handleSelectMaterializeEntry,
          onInit: setReactFlowInstance,
          onNodesChange,
          onEdgesChange,
          onNodeDragStop: handleNodeDragStop,
          onPaneClick: handlePaneClick,
          onPaneContextMenu: handlePaneContextMenu,
          onNodeClick: handleNodeClick,
          onRecomputeGraph: handleRecomputeGraph,
          onRunPipeline: handleRunPipeline,
          canRunPipeline: Boolean(pipeline),
          showEditorButton: isMobile,
          isEditorButtonDisabled: !asset,
          onOpenEditor: () => setMobileEditorOpen(true),
        }}
      />

      <WorkspaceAssetDialogs
        deleteDialogOpen={deleteDialogOpen}
        deleteLoading={deleteLoading}
        selectedAssetName={asset?.name}
        canDeleteAsset={Boolean(asset && pipeline)}
        onDeleteDialogOpenChange={(open) => {
          if (!deleteLoading) {
            setDeleteDialogOpen(open);
          }
        }}
        onConfirmDeleteAsset={handleConfirmDeleteAsset}
        onCancelDeleteAsset={() => setDeleteDialogOpen(false)}
      />
    </>
  );
}
