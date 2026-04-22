import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";

import {
  WorkspaceEnvironmentForm,
  WorkspaceEnvironmentFormHandle,
} from "@/components/workspace-environment-form";
import { useWorkspaceSettingsLayout } from "@/components/workspace-settings-layout";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";

export const Route = createFileRoute("/_workspace/settings/environments/new")({
  component: NewEnvironmentRouteComponent,
});

function NewEnvironmentRouteComponent() {
  const navigate = useNavigate();
  const environmentFormRef = useRef<WorkspaceEnvironmentFormHandle>(null);
  const [environmentActions, setEnvironmentActions] = useState({ canSave: false });
  const {
    handleCloneWorkspaceEnvironment,
    handleCreateWorkspaceEnvironment,
    handleDeleteWorkspaceEnvironment,
    handleUpdateWorkspaceEnvironment,
    loadWorkspaceConfig,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigBusy,
    workspaceConfigLoading,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
  } = useWorkspaceSettingsLayout();

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) {
          void navigate({ to: "/settings/environments" });
        }
      }}
    >
      <DialogContent className="max-w-xl p-0 overflow-hidden">
        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle>Create Environment</DialogTitle>
          <DialogDescription>
            Create a new environment for this project.
          </DialogDescription>
        </DialogHeader>
        <div className="px-6 py-5">
          <WorkspaceEnvironmentForm
            ref={environmentFormRef}
            configPath={workspaceConfig?.path ?? ".bruin.yml"}
            defaultEnvironment={workspaceConfig?.default_environment}
            selectedEnvironment={null}
            environments={normalizedConfigEnvironments}
            loading={workspaceConfigLoading}
            busy={workspaceConfigBusy}
            parseError={workspaceConfig?.parse_error}
            statusMessage={workspaceConfigStatusMessage}
            statusTone={workspaceConfigStatusTone}
            mode="create"
            onModeChange={() => undefined}
            onSelectedEnvironmentChange={(environmentId) => {
              if (!environmentId) {
                void navigate({ to: "/settings/environments" });
                return;
              }

              void navigate({
                to: "/settings/environments/$environmentId",
                params: { environmentId },
              });
            }}
            onReload={() => void loadWorkspaceConfig()}
            onCreateEnvironment={handleCreateWorkspaceEnvironment}
            onUpdateEnvironment={handleUpdateWorkspaceEnvironment}
            onCloneEnvironment={handleCloneWorkspaceEnvironment}
            onDeleteEnvironment={handleDeleteWorkspaceEnvironment}
            showHeader={false}
            showConfigPath={false}
            showReload={false}
            useCard={false}
            onStateChange={setEnvironmentActions}
          />
        </div>
        <DialogFooter className="border-t px-6 py-4">
          <Button type="button" variant="outline" onClick={() => void navigate({ to: "/settings/environments" })}>
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!environmentActions.canSave}
            onClick={() => void environmentFormRef.current?.save()}
          >
            Create Environment
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
