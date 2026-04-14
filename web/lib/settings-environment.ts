import { WorkspaceConfigEnvironment, WorkspaceConfigResponse } from "@/lib/types";

export function resolveEffectiveConfigEnvironment({
  environments,
  selectedEnvironmentName,
  workspaceConfig,
}: {
  environments: WorkspaceConfigEnvironment[];
  selectedEnvironmentName?: string | null;
  workspaceConfig: WorkspaceConfigResponse | null;
}) {
  const environmentName =
    selectedEnvironmentName ||
    workspaceConfig?.selected_environment ||
    workspaceConfig?.default_environment ||
    environments[0]?.name ||
    null;

  return (
    environments.find((environment) => environment.name === environmentName) ?? null
  );
}
