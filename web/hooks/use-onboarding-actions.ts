"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useState } from "react";

import { materializeAssetStream } from "@/lib/api";
import { assetEditorTabAtom, editorDraftAtom } from "@/lib/atoms/domains/editor";
import {
  pipelineAtom,
  resolvedSelectedAssetAtom,
} from "@/lib/atoms/domains/workspace";
import {
  buildCreateAssetInput,
  buildOnboardingPythonStarterQuery,
  buildOnboardingSQLStarterQuery,
  buildSuggestedAssetName,
} from "@/lib/workspace-shell-helpers";
import { OnboardingState } from "@/hooks/use-onboarding-state";
import { WebAsset } from "@/lib/types";

type UseOnboardingActionsInput = {
  editorValue: string;
  onboarding: OnboardingState;
  existingAssetNames: Set<string>;
  navigateSelection: (pipelineId: string, assetId: string | null) => void;
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
  refreshPipelineMaterialization: (pipelineId: string) => Promise<void>;
  setMaterializeBatchResult: (
    output: string,
    status: "ok" | "error",
    errorMessage: string
  ) => void;
};

export function useOnboardingActions({
  editorValue,
  onboarding,
  existingAssetNames,
  navigateSelection,
  runCreateAsset,
  runUpdateAsset,
  refreshPipelineMaterialization,
  setMaterializeBatchResult,
}: UseOnboardingActionsInput) {
  const pipeline = useAtomValue(pipelineAtom);
  const selectedAssetId = useAtomValue(resolvedSelectedAssetAtom);
  const pipelineId = pipeline?.id ?? null;
  const [onboardingMaterializeLoading, setOnboardingMaterializeLoading] =
    useState(false);
  const setAssetEditorTab = useSetAtom(assetEditorTabAtom);
  const setEditorDraft = useSetAtom(editorDraftAtom);

  const handleCreateOnboardingAsset = useCallback(
    (kind: "python" | "sql") => {
      if (!pipelineId) {
        return;
      }

      const suggestedName =
        kind === "python"
          ? buildSuggestedAssetName(
              "python",
              existingAssetNames,
              pipeline?.name
            )
          : buildSuggestedAssetName("sql", existingAssetNames, pipeline?.name);
      const createInput = buildCreateAssetInput(
        suggestedName,
        kind === "python" ? "python" : "sql"
      );

      void runCreateAsset(pipelineId, createInput).then((response) => {
        if (response?.asset_id) {
          navigateSelection(pipelineId, response.asset_id);
        }
      });
    },
    [
      existingAssetNames,
      navigateSelection,
      pipeline?.name,
      pipelineId,
      runCreateAsset,
    ]
  );

  const applyTemplateToAsset = useCallback(
    (targetAsset: WebAsset | null, nextContent: string) => {
      if (!pipelineId || !targetAsset) {
        return;
      }

      setAssetEditorTab("configuration");
      setEditorDraft({
        assetId: targetAsset.id,
        content: nextContent,
      });
      navigateSelection(pipelineId, targetAsset.id);

      void runUpdateAsset(pipelineId, targetAsset.id, { content: nextContent });
    },
    [
      navigateSelection,
      pipelineId,
      runUpdateAsset,
      setAssetEditorTab,
      setEditorDraft,
    ]
  );

  const handleApplyPythonStarter = useCallback(() => {
    applyTemplateToAsset(
      onboarding.primaryPythonAsset,
      buildOnboardingPythonStarterQuery()
    );
  }, [applyTemplateToAsset, onboarding.primaryPythonAsset]);

  const handleApplySQLStarter = useCallback(() => {
    if (!onboarding.sqlDraftTarget || !onboarding.primaryPythonAsset) {
      return;
    }

    applyTemplateToAsset(
      onboarding.sqlDraftTarget,
      buildOnboardingSQLStarterQuery(onboarding.primaryPythonAsset.name)
    );
  }, [
    applyTemplateToAsset,
    onboarding.primaryPythonAsset,
    onboarding.sqlDraftTarget,
  ]);

  const handleMaterializeOnboardingAssets = useCallback(() => {
    if (
      onboarding.tutorialAssets.length === 0 ||
      onboardingMaterializeLoading
    ) {
      return;
    }

    setOnboardingMaterializeLoading(true);

    void (async () => {
      try {
        const outputs: string[] = [];
        let encounteredError = false;

        for (const current of onboarding.tutorialAssets) {
          try {
            const result = await materializeAssetStream(current.id, {});
            outputs.push(
              `# ${current.name}\n${(result.output ?? "").trim() || result.status}`
            );
            if (result.status === "error") {
              encounteredError = true;
            }
          } catch (error) {
            encounteredError = true;
            outputs.push(`# ${current.name}\n${String(error)}`);
          }
        }

        setMaterializeBatchResult(
          outputs.join("\n\n"),
          encounteredError ? "error" : "ok",
          encounteredError ? "One or more assets failed." : ""
        );
      } finally {
        if (pipelineId) {
          await refreshPipelineMaterialization(pipelineId).catch(
            () => undefined
          );
        }
        setOnboardingMaterializeLoading(false);
      }
    })();
  }, [
    onboarding.tutorialAssets,
    onboardingMaterializeLoading,
    pipelineId,
    refreshPipelineMaterialization,
    setMaterializeBatchResult,
  ]);

  const handleApplyVisualizationStarter = useCallback(() => {
    if (!pipelineId || !onboarding.primarySQLAsset) {
      return;
    }

    const target = onboarding.primarySQLAsset;
    const mergedMeta: Record<string, string> = {
      ...(target.meta ?? {}),
      web_view: "table",
      web_table_limit: "100",
      web_table_dense: "false",
    };

    setAssetEditorTab("visualization");
    navigateSelection(pipelineId, target.id);

    void runUpdateAsset(pipelineId, target.id, {
      content: target.id === selectedAssetId ? editorValue : target.content,
      meta: mergedMeta,
    });
  }, [
    editorValue,
    navigateSelection,
    onboarding.primarySQLAsset,
    pipelineId,
    runUpdateAsset,
    selectedAssetId,
    setAssetEditorTab,
  ]);

  return {
    onboardingMaterializeLoading,
    handleCreateOnboardingAsset,
    handleApplyPythonStarter,
    handleApplySQLStarter,
    handleMaterializeOnboardingAssets,
    handleApplyVisualizationStarter,
  };
}
