import {
  findConnectionByName,
  findEnvironmentByName,
} from "@/lib/settings-form-utils";
import {
  WorkspaceConfigConnection,
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

export function getWorkspaceEnvironment(
  workspaceConfig: WorkspaceConfigResponse | null,
  environmentName?: string
) {
  return findEnvironmentByName(workspaceConfig?.environments ?? [], environmentName);
}

export function getWorkspaceConnection(
  environment: WorkspaceConfigEnvironment | null,
  connectionName?: string
) {
  return findConnectionByName(environment, connectionName);
}

export function isDefaultEnvironment(
  workspaceConfig: WorkspaceConfigResponse | null,
  environment: WorkspaceConfigEnvironment
) {
  return workspaceConfig?.default_environment === environment.name;
}

export function getConnectionType(connection: WorkspaceConfigConnection) {
  return connection.type;
}
