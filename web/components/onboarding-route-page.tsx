"use client";

import { useSetAtom } from "jotai";
import { useCallback } from "react";

import { WorkspaceOnboarding } from "@/components/workspace-onboarding";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { getWorkspace } from "@/lib/api";
import { workspaceAtom, workspaceSyncSourceAtom } from "@/lib/atoms/domains/workspace";
import { OnboardingSessionState } from "@/lib/types";

type OnboardingRoutePageProps = {
  onboardingState: OnboardingSessionState;
};

export function OnboardingRoutePage({ onboardingState }: OnboardingRoutePageProps) {
  const setWorkspace = useSetAtom(workspaceAtom);
  const setWorkspaceSyncSource = useSetAtom(workspaceSyncSourceAtom);
  const {
    workspaceConfig,
    workspaceConfigLoading,
    handleCreateWorkspaceConnection,
    handleUpdateWorkspaceConnection,
    loadWorkspaceConfig,
  } = useWorkspaceSettingsData();

  const handleReloadWorkspace = useCallback(async () => {
    const data = await getWorkspace();
    setWorkspace(data);
    setWorkspaceSyncSource({
      method: "workspace-load",
      recordedAt: new Date().toISOString(),
      revision: data.revision,
    });
  }, [setWorkspace, setWorkspaceSyncSource]);

  if (workspaceConfigLoading || !workspaceConfig) {
    return <div className="flex min-h-screen items-center justify-center text-sm text-muted-foreground">Loading onboarding...</div>;
  }

  return (
    <WorkspaceOnboarding
      workspaceConfig={workspaceConfig}
      onboardingState={onboardingState}
      onCreateConnection={handleCreateWorkspaceConnection}
      onUpdateConnection={handleUpdateWorkspaceConnection}
      onReloadConfig={loadWorkspaceConfig}
      onReloadWorkspace={handleReloadWorkspace}
    />
  );
}
