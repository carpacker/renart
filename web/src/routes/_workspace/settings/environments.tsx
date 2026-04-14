import { createFileRoute, useNavigate } from "@tanstack/react-router";

import { WorkspaceConfigContent } from "@/components/workspace-config-content";
import { WorkspaceEnvironmentPane } from "@/components/workspace-environment-pane";
import { useWorkspaceSettingsLayout } from "@/components/workspace-settings-layout";
import { WorkspaceSettingsSplitView } from "@/components/workspace-settings-split-view";
import { useResolvedWorkspaceEnvironment } from "@/hooks/use-workspace-config-selection";
import { useIsMobile } from "@/hooks/use-mobile";

export const Route = createFileRoute("/_workspace/settings/environments")({
  validateSearch: (search: Record<string, unknown>) => ({
    environment:
      typeof search.environment === "string" ? search.environment : undefined,
    mode: typeof search.mode === "string" ? search.mode : undefined,
  }),
  component: WorkspaceSettingsEnvironmentsRouteComponent,
});

function WorkspaceSettingsEnvironmentsRouteComponent() {
  const search = Route.useSearch();
  const navigate = useNavigate();

  return (
    <WorkspaceEnvironmentsRoutePage
      mode={search.mode}
      selectedEnvironmentName={search.environment}
      onSearchChange={(next) =>
        navigate({
          to: "/settings/environments",
          search: {
            environment: next.environment,
            mode: next.mode,
          },
          replace: true,
        })
      }
    />
  );
}

export function WorkspaceEnvironmentsRoutePage({
  mode,
  selectedEnvironmentName,
  onSearchChange,
}: {
  mode?: string;
  selectedEnvironmentName?: string;
  onSearchChange: (next: {
    environment?: string;
    mode?: "edit" | "create" | "clone";
  }) => void;
}) {
  const isMobile = useIsMobile();
  const {
    fallbackConfigEnvironment,
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

  const effectiveMode =
    mode === "create" || mode === "clone" ? mode : "edit";

  const { resolvedEnvironmentName } = useResolvedWorkspaceEnvironment({
    defaultEnvironment: fallbackConfigEnvironment ?? undefined,
    environments: normalizedConfigEnvironments,
    selectedEnvironmentName,
    onSelectedEnvironmentChange: (name) =>
      onSearchChange({
        environment: name ?? undefined,
        mode: effectiveMode,
      }),
  });

  const activeEnvironmentName =
    effectiveMode === "edit" ? resolvedEnvironmentName ?? null : null;
  const mobilePaneDescription =
    effectiveMode === "create"
      ? "Create a new environment."
      : effectiveMode === "clone"
        ? `Clone ${resolvedEnvironmentName ?? "an environment"}.`
        : activeEnvironmentName
          ? `Editing ${activeEnvironmentName}`
          : "Create or edit an environment.";

  return (
    <WorkspaceSettingsSplitView
      isPaneOpen={
        !isMobile || effectiveMode !== "edit" || Boolean(activeEnvironmentName)
      }
      paneKey={`${effectiveMode}:${activeEnvironmentName ?? resolvedEnvironmentName ?? "new"}`}
      paneTitle="Environment Editor"
      paneDescription={mobilePaneDescription}
      content={
        <WorkspaceConfigContent
          view="environments"
          configPath={workspaceConfig?.path ?? ".bruin.yml"}
          defaultEnvironment={workspaceConfig?.default_environment}
          selectedEnvironmentName={resolvedEnvironmentName}
          environments={normalizedConfigEnvironments}
          selectedConnectionName={null}
          loading={workspaceConfigLoading}
          onSelectEnvironment={(name) =>
            onSearchChange({ environment: name, mode: "edit" })
          }
          onSelectConnection={() => undefined}
          onCreateEnvironment={() =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? undefined,
              mode: "create",
            })
          }
          onCloneEnvironment={() =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? undefined,
              mode: "clone",
            })
          }
          onCreateConnection={() => undefined}
        />
      }
      pane={
        <WorkspaceEnvironmentPane
          configPath={workspaceConfig?.path ?? ".bruin.yml"}
          defaultEnvironment={workspaceConfig?.default_environment}
          selectedEnvironment={resolvedEnvironmentName}
          environments={normalizedConfigEnvironments}
          loading={workspaceConfigLoading}
          busy={workspaceConfigBusy}
          parseError={workspaceConfig?.parse_error}
          statusMessage={workspaceConfigStatusMessage}
          statusTone={workspaceConfigStatusTone}
          mode={effectiveMode}
          onModeChange={(nextMode) =>
            onSearchChange({
              environment: resolvedEnvironmentName ?? undefined,
              mode: nextMode,
            })
          }
          onSelectedEnvironmentChange={(name) =>
            onSearchChange({
              environment: name ?? undefined,
              mode: effectiveMode,
            })
          }
          onReload={() => void loadWorkspaceConfig()}
          onCreateEnvironment={handleCreateWorkspaceEnvironment}
          onUpdateEnvironment={handleUpdateWorkspaceEnvironment}
          onCloneEnvironment={handleCloneWorkspaceEnvironment}
          onDeleteEnvironment={handleDeleteWorkspaceEnvironment}
        />
      }
    />
  );
}
