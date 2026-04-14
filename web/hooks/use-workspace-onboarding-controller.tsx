"use client";

import { CSSProperties, useEffect, useMemo, useState } from "react";

import { WorkspaceOnboardingPanel } from "@/components/workspace-onboarding-panel";
import { useOnboardingActions } from "@/hooks/use-onboarding-actions";
import { useOnboardingState } from "@/hooks/use-onboarding-state";
import { WorkspaceSidebarState } from "@/components/workspace-layout";
import { WebPipeline } from "@/lib/types";

export function useWorkspaceOnboardingController({
  editorValue,
  existingAssetNames,
  navigateSelection,
  openCreatePipelineDialog,
  pipeline,
  refreshPipelineMaterialization,
  runCreateAsset,
  runUpdateAsset,
  setMaterializeBatchResult,
  setSidebarState,
}: {
  editorValue: string;
  existingAssetNames: Set<string>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
  openCreatePipelineDialog: () => void;
  pipeline: WebPipeline | null;
  refreshPipelineMaterialization: (pipelineId: string) => Promise<void>;
  runCreateAsset: (
    pipelineId: string,
    input: { name?: string; type?: string; path?: string; content?: string }
  ) => Promise<{ asset_id?: string } | null>;
  runUpdateAsset: (
    pipelineId: string,
    assetId: string,
    input: {
      content?: string;
      materialization_type?: string;
      meta?: Record<string, string>;
    }
  ) => Promise<boolean>;
  setMaterializeBatchResult: (
    output: string,
    status: "ok" | "error",
    errorMessage: string
  ) => void;
  setSidebarState: (state: WorkspaceSidebarState) => void;
}) {
  const [showOnboarding, setShowOnboarding] = useState(true);
  const [helpMode, setHelpMode] = useState(false);
  const { onboarding, onboardingHelp } = useOnboardingState(pipeline);

  const {
    onboardingMaterializeLoading,
    handleCreateOnboardingAsset,
    handleApplyPythonStarter,
    handleApplySQLStarter,
    handleMaterializeOnboardingAssets,
    handleApplyVisualizationStarter,
  } = useOnboardingActions({
    editorValue,
    onboarding,
    existingAssetNames,
    navigateSelection,
    runCreateAsset,
    runUpdateAsset,
    refreshPipelineMaterialization,
    setMaterializeBatchResult,
  });

  const helpPulseStyle = useMemo<CSSProperties>(
    () => ({ animation: "bruin-help-scale 520ms ease-in-out infinite alternate" }),
    []
  );

  const onboardingContent = useMemo(
    () => (
      <WorkspaceOnboardingPanel
        helpMode={helpMode}
        onboarding={onboarding}
        onboardingHelp={onboardingHelp}
        onboardingMaterializeLoading={onboardingMaterializeLoading}
        pipelineExists={Boolean(pipeline)}
        showOnboarding={showOnboarding}
        onApplyPythonStarter={handleApplyPythonStarter}
        onApplySQLStarter={handleApplySQLStarter}
        onApplyVisualizationStarter={handleApplyVisualizationStarter}
        onCreateOnboardingAsset={handleCreateOnboardingAsset}
        onCreatePipeline={openCreatePipelineDialog}
        onHide={() => setShowOnboarding(false)}
        onMaterializeOnboardingAssets={handleMaterializeOnboardingAssets}
        onShow={() => setShowOnboarding(true)}
        onToggleHelp={() => setHelpMode((previous) => !previous)}
      />
    ),
    [
      handleApplyPythonStarter,
      handleApplySQLStarter,
      handleApplyVisualizationStarter,
      handleCreateOnboardingAsset,
      handleMaterializeOnboardingAssets,
      helpMode,
      onboarding,
      onboardingHelp,
      onboardingMaterializeLoading,
      openCreatePipelineDialog,
      pipeline,
      showOnboarding,
    ]
  );

  useEffect(() => {
    setSidebarState({
      highlighted: helpMode && onboardingHelp.target === "sidebar",
      highlightStyle:
        helpMode && onboardingHelp.target === "sidebar" ? helpPulseStyle : undefined,
    });

    return () => setSidebarState({});
  }, [helpMode, helpPulseStyle, onboardingHelp.target, setSidebarState]);

  return {
    helpMode,
    helpPulseStyle,
    onboardingContent,
    onboardingHelp,
  };
}
