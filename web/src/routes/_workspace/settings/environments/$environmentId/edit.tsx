import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Trash2 } from "lucide-react";
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

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/edit"
)({
  component: EditEnvironmentRouteComponent,
});

function EditEnvironmentRouteComponent() {
  const navigate = useNavigate();
  const environmentFormRef = useRef<WorkspaceEnvironmentFormHandle>(null);
  const [environmentActions, setEnvironmentActions] = useState({ canSave: false });
  const { environmentId } = Route.useParams();
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
          void navigate({
            to: "/settings/environments/$environmentId",
            params: { environmentId },
          });
        }
      }}
    >
      <DialogContent className="max-w-xl p-0 overflow-hidden">
        <DialogHeader className="border-b px-6 py-5">
          <DialogTitle>Edit Environment</DialogTitle>
          <DialogDescription>
            Update environment settings.
          </DialogDescription>
        </DialogHeader>
        <div className="px-6 py-5">
          <WorkspaceEnvironmentForm
            ref={environmentFormRef}
            configPath={workspaceConfig?.path ?? ".bruin.yml"}
            defaultEnvironment={workspaceConfig?.default_environment}
            selectedEnvironment={environmentId}
            environments={normalizedConfigEnvironments}
            loading={workspaceConfigLoading}
            busy={workspaceConfigBusy}
            parseError={workspaceConfig?.parse_error}
            statusMessage={workspaceConfigStatusMessage}
            statusTone={workspaceConfigStatusTone}
            mode="edit"
            onModeChange={() => undefined}
            onSelectedEnvironmentChange={(nextEnvironmentId) => {
              void navigate({
                to: nextEnvironmentId
                  ? "/settings/environments/$environmentId"
                  : "/settings/environments",
                params: nextEnvironmentId ? { environmentId: nextEnvironmentId } : undefined,
                replace: true,
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
          <Button
            type="button"
            variant="destructive"
            className="mr-auto"
            disabled={workspaceConfigBusy}
            onClick={async () => {
              await handleDeleteWorkspaceEnvironment(environmentId);
              void navigate({
                to: "/settings/environments",
                replace: true,
              });
            }}
          >
            <Trash2 className="mr-2 size-4" />
            Delete Environment
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() =>
              void navigate({
                to: "/settings/environments/$environmentId",
                params: { environmentId },
              })
            }
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!environmentActions.canSave}
            onClick={() => void environmentFormRef.current?.save()}
          >
            Save Changes
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
