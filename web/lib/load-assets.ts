import type { WorkspaceConfigConnection, WorkspaceConfigResponse } from "@/lib/types";

export const LOCAL_LOAD_CONNECTION = "local";

export const LOCAL_LOAD_CONNECTION_OPTION: WorkspaceConfigConnection = {
  name: LOCAL_LOAD_CONNECTION,
  type: "local file",
  values: {},
  load_category: "file",
};

export function isLocalLoadConnection(name: string | undefined) {
  return (name ?? "").trim().toLowerCase() === LOCAL_LOAD_CONNECTION;
}

export function loadConnectionsForEnvironment(
  workspaceConfig: WorkspaceConfigResponse | null | undefined,
  environment: string | undefined,
) {
  const environments = workspaceConfig?.environments ?? [];
  const active =
    environments.find((candidate) => candidate.name === environment) ??
    environments.find(
      (candidate) =>
        candidate.name ===
        (workspaceConfig?.selected_environment || workspaceConfig?.default_environment),
    ) ??
    environments[0];

  return (active?.connections ?? []).filter((connection) => Boolean(connection.load_category));
}

export function loadConnectionCategory(
  connections: WorkspaceConfigConnection[],
  name: string | undefined,
) {
  if (isLocalLoadConnection(name)) return "file";
  return connections.find((connection) => connection.name === name)?.load_category;
}

export function loadTargetNeedsDestinationObject(category: string | undefined) {
  return category === "storage" || category === "file";
}
