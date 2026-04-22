import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";

import {
  WorkspaceConnectionForm,
  WorkspaceConnectionFormHandle,
} from "@/components/workspace-connection-form";
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
  "/_workspace/settings/environments/$environmentId/connections/new"
)({
  validateSearch: (search: Record<string, unknown>) => ({
    type: typeof search.type === "string" ? search.type : undefined,
  }),
  component: NewConnectionRouteComponent,
});

function NewConnectionRouteComponent() {
  const navigate = useNavigate();
  const connectionFormRef = useRef<WorkspaceConnectionFormHandle>(null);
  const [connectionActions, setConnectionActions] = useState({
    canSave: false,
    canValidate: false,
  });
  const { environmentId } = Route.useParams();
  const { type: requestedConnectionType } = Route.useSearch();
  const {
    handleCreateWorkspaceConnection,
    handleDeleteWorkspaceConnection,
    handleUpdateWorkspaceConnection,
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
          <DialogTitle>Create Connection</DialogTitle>
          <DialogDescription>
            Configure a new connection for this environment.
          </DialogDescription>
        </DialogHeader>
        <div className="px-6 py-5">
          <WorkspaceConnectionForm
            ref={connectionFormRef}
            configPath={workspaceConfig?.path ?? ".bruin.yml"}
            defaultEnvironment={workspaceConfig?.default_environment}
            selectedEnvironment={environmentId}
            selectedConnectionName={null}
            environments={normalizedConfigEnvironments}
            connectionTypes={workspaceConfig?.connection_types ?? []}
            loading={workspaceConfigLoading}
            busy={workspaceConfigBusy}
            parseError={workspaceConfig?.parse_error}
            statusMessage={workspaceConfigStatusMessage}
            statusTone={workspaceConfigStatusTone}
            mode="create"
            requestedConnectionType={requestedConnectionType}
            onModeChange={() => undefined}
            onSelectedEnvironmentChange={() => undefined}
            onSelectedConnectionChange={(connectionId) => {
              if (!connectionId) {
                return;
              }

              void navigate({
                to: "/settings/environments/$environmentId",
                params: { environmentId },
              });
            }}
            onReload={() => void loadWorkspaceConfig()}
            onCreateConnection={handleCreateWorkspaceConnection}
            onUpdateConnection={handleUpdateWorkspaceConnection}
            onDeleteConnection={handleDeleteWorkspaceConnection}
            allowEnvironmentSelection={false}
            showEnvironmentSelector={false}
            showHeader={false}
            useCard={false}
            showConfigPath={false}
            showReload={false}
            showActions={false}
            onStateChange={setConnectionActions}
          />
        </div>
        <DialogFooter className="border-t px-6 py-4">
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
            variant="outline"
            disabled={!connectionActions.canValidate}
            onClick={() => void connectionFormRef.current?.validate()}
          >
            Verify Connection
          </Button>
          <Button
            type="button"
            disabled={!connectionActions.canSave}
            onClick={() => void connectionFormRef.current?.save()}
          >
            Create Connection
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
