import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Database, Trash2 } from "lucide-react";
import { useRef, useState } from "react";

import {
  WorkspaceConnectionForm,
  WorkspaceConnectionFormHandle,
} from "@/components/workspace-connection-form";
import { useWorkspaceSettingsLayout } from "@/components/workspace-settings-layout";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  getWorkspaceConnection,
  getWorkspaceEnvironment,
} from "@/lib/workspace-settings";

export const Route = createFileRoute(
  "/_workspace/settings/environments/$environmentId/connections/$connectionId/"
)({
  component: ConnectionDrawerRouteComponent,
});

function ConnectionDrawerRouteComponent() {
  const navigate = useNavigate();
  const [connectionActions, setConnectionActions] = useState({
    canSave: false,
    canValidate: false,
  });
  const connectionFormRef = useRef<WorkspaceConnectionFormHandle>(null);
  const { connectionId, environmentId } = Route.useParams();
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
  const environment = getWorkspaceEnvironment(workspaceConfig, environmentId);
  const connection = getWorkspaceConnection(environment, connectionId);

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
          <div className="flex items-start gap-3 pr-8">
            <div className="rounded-lg border bg-muted/30 p-2 text-muted-foreground">
              <Database className="size-5" />
            </div>
            <div className="min-w-0">
              <DialogTitle>{connection?.name ?? connectionId}</DialogTitle>
              <DialogDescription>
                {environment ? environment.name : environmentId}
              </DialogDescription>
              {connection ? (
                <div className="mt-3 flex flex-wrap gap-2 text-sm text-muted-foreground">
                  <span className="rounded-full border px-3 py-1">{connection.type}</span>
                  <span className="rounded-full border px-3 py-1">
                    {environment?.name ?? environmentId}
                  </span>
                </div>
              ) : null}
            </div>
          </div>
        </DialogHeader>

        <div className="max-h-[65vh] overflow-auto px-6 py-5">
          {connection ? (
            <WorkspaceConnectionForm
              ref={connectionFormRef}
              configPath={workspaceConfig?.path ?? ".bruin.yml"}
              defaultEnvironment={workspaceConfig?.default_environment}
              selectedEnvironment={environmentId}
              selectedConnectionName={connectionId}
              environments={normalizedConfigEnvironments}
              connectionTypes={workspaceConfig?.connection_types ?? []}
              loading={workspaceConfigLoading}
              busy={workspaceConfigBusy}
              parseError={workspaceConfig?.parse_error}
              statusMessage={workspaceConfigStatusMessage}
              statusTone={workspaceConfigStatusTone}
              mode="edit"
              onModeChange={() => undefined}
              onSelectedEnvironmentChange={() => undefined}
              onSelectedConnectionChange={(nextConnectionId) => {
                void navigate({
                  to: nextConnectionId
                    ? "/settings/environments/$environmentId/connections/$connectionId"
                    : "/settings/environments/$environmentId",
                  params: nextConnectionId
                    ? { environmentId, connectionId: nextConnectionId }
                    : { environmentId },
                  replace: true,
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
          ) : (
            <div className="text-sm text-muted-foreground">Connection not found.</div>
          )}
        </div>

        <DialogFooter className="border-t px-6 py-4">
          {connection ? (
            <>
              <Button
                type="button"
                variant="destructive"
                className="mr-auto"
                disabled={workspaceConfigBusy}
                onClick={async () => {
                  await handleDeleteWorkspaceConnection({
                    environment_name: environmentId,
                    name: connectionId,
                  });
                  void navigate({
                    to: "/settings/environments/$environmentId",
                    params: { environmentId },
                    replace: true,
                  });
                }}
              >
                <Trash2 className="mr-2 size-4" />
                Delete Connection
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  void navigate({
                    to: "/settings/environments/$environmentId",
                    params: { environmentId },
                  });
                }}
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
                Save Changes
              </Button>
            </>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
