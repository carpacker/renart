import { createFileRoute, useNavigate } from "@tanstack/react-router";

import { WorkspaceConfigContent } from "@/components/workspace-config-content";
import { WorkspaceConnectionPane } from "@/components/workspace-connection-pane";
import { useWorkspaceSettingsLayout } from "@/components/workspace-settings-layout";
import { WorkspaceSettingsSplitView } from "@/components/workspace-settings-split-view";
import {
  useResolvedWorkspaceConnection,
  useResolvedWorkspaceEnvironment,
} from "@/hooks/use-workspace-config-selection";
import { useIsMobile } from "@/hooks/use-mobile";

export const Route = createFileRoute("/_workspace/settings/connections")({
  validateSearch: (search: Record<string, unknown>) => ({
    environment:
      typeof search.environment === "string" ? search.environment : undefined,
    connection:
      typeof search.connection === "string" ? search.connection : undefined,
    connectionType:
      typeof search.connectionType === "string"
        ? search.connectionType
        : undefined,
    mode: typeof search.mode === "string" ? search.mode : undefined,
  }),
  component: WorkspaceSettingsConnectionsRouteComponent,
});

function WorkspaceSettingsConnectionsRouteComponent() {
  const search = Route.useSearch();
  const navigate = useNavigate();

  return (
    <WorkspaceConnectionsRoutePage
      selectedConnectionName={search.connection}
      requestedConnectionType={search.connectionType}
      requestedMode={search.mode}
      selectedConfigEnvironment={search.environment}
      onSearchChange={(next) =>
        navigate({
          to: "/settings/connections",
          search: {
            environment: next.environment,
            connection: next.connection,
            connectionType: next.connectionType,
            mode: next.mode,
          },
          replace: true,
        })
      }
    />
  );
}

function WorkspaceConnectionsRoutePage({
  selectedConnectionName,
  requestedConnectionType,
  requestedMode,
  selectedConfigEnvironment,
  onSearchChange,
}: {
  selectedConnectionName?: string;
  requestedConnectionType?: string;
  requestedMode?: string;
  selectedConfigEnvironment?: string;
  onSearchChange: (next: {
    environment?: string;
    connection?: string;
    connectionType?: string;
    mode?: "edit" | "create";
  }) => void;
}) {
  const isMobile = useIsMobile();
  const {
    fallbackConfigEnvironment,
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

  const { activeEnvironment, resolvedEnvironmentName } =
    useResolvedWorkspaceEnvironment({
      defaultEnvironment: fallbackConfigEnvironment ?? undefined,
      environments: normalizedConfigEnvironments,
      selectedEnvironmentName: selectedConfigEnvironment,
      onSelectedEnvironmentChange: (name) =>
        onSearchChange({
          environment: name ?? undefined,
          connection: selectedConnectionName,
          connectionType: requestedConnectionType,
          mode: requestedMode === "create" ? "create" : "edit",
        }),
    });

  const { resolvedConnectionName } = useResolvedWorkspaceConnection({
    activeEnvironment,
    selectedConnectionName,
    onSelectedConnectionChange: (name) =>
      onSearchChange({
        environment: resolvedEnvironmentName ?? undefined,
        connection: name ?? undefined,
        connectionType: requestedConnectionType,
        mode: requestedMode === "create" ? "create" : "edit",
      }),
  });

  const effectiveMode = requestedMode === "create" ? "create" : "edit";
  const activeConnectionName =
    effectiveMode === "create" ? null : resolvedConnectionName ?? null;
  const mobilePaneDescription = activeConnectionName
    ? `${resolvedEnvironmentName ?? "Environment"} / ${activeConnectionName}`
    : resolvedEnvironmentName
      ? `${resolvedEnvironmentName} / New connection`
      : "Create or edit a connection.";

  return (
    <WorkspaceSettingsSplitView
      isPaneOpen={
        !isMobile || effectiveMode === "create" || Boolean(activeConnectionName)
      }
      paneKey={`${effectiveMode}:${resolvedEnvironmentName ?? "none"}:${activeConnectionName ?? "new"}`}
      paneTitle="Connection Editor"
      paneDescription={mobilePaneDescription}
      content={
        <WorkspaceConfigContent
          view="connections"
          configPath={workspaceConfig?.path ?? ".bruin.yml"}
          defaultEnvironment={workspaceConfig?.default_environment}
          selectedEnvironmentName={resolvedEnvironmentName}
          environments={normalizedConfigEnvironments}
          selectedConnectionName={resolvedConnectionName}
          loading={workspaceConfigLoading}
          onSelectEnvironment={(name) =>
            onSearchChange({
              environment: name,
              connection: undefined,
              connectionType: requestedConnectionType,
              mode: "edit",
            })
          }
          onSelectConnection={(name) =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? selectedConfigEnvironment,
              connection: name,
              connectionType: requestedConnectionType,
              mode: "edit",
            })
          }
          onCreateEnvironment={() => undefined}
          onCloneEnvironment={() => undefined}
          onCreateConnection={() =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? selectedConfigEnvironment,
              connection: undefined,
              connectionType: requestedConnectionType,
              mode: "create",
            })
          }
        />
      }
      pane={
        <WorkspaceConnectionPane
          configPath={workspaceConfig?.path ?? ".bruin.yml"}
          defaultEnvironment={workspaceConfig?.default_environment}
          selectedEnvironment={resolvedEnvironmentName}
          selectedConnectionName={resolvedConnectionName}
          environments={normalizedConfigEnvironments}
          connectionTypes={workspaceConfig?.connection_types ?? []}
          loading={workspaceConfigLoading}
          busy={workspaceConfigBusy}
          parseError={workspaceConfig?.parse_error}
          statusMessage={workspaceConfigStatusMessage}
          statusTone={workspaceConfigStatusTone}
          mode={effectiveMode}
          requestedConnectionType={requestedConnectionType}
          onModeChange={(mode) =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? selectedConfigEnvironment,
              connection:
                mode === "create" ? undefined : resolvedConnectionName ?? undefined,
              connectionType: requestedConnectionType,
              mode,
            })
          }
          onSelectedEnvironmentChange={(name) =>
            onSearchChange({
              environment: name ?? undefined,
              connection: resolvedConnectionName ?? undefined,
              connectionType: requestedConnectionType,
              mode: effectiveMode,
            })
          }
          onSelectedConnectionChange={(name) =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? selectedConfigEnvironment,
              connection: name ?? undefined,
              connectionType: requestedConnectionType,
              mode: effectiveMode,
            })
          }
          onReload={() => void loadWorkspaceConfig()}
          onCreateConnection={handleCreateWorkspaceConnection}
          onUpdateConnection={handleUpdateWorkspaceConnection}
          onDeleteConnection={handleDeleteWorkspaceConnection}
        />
      }
    />
  );
}
