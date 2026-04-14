import {
  WorkspaceConfigConnection,
  WorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

export function findEnvironmentByName(
  environments: WorkspaceConfigEnvironment[],
  environmentName?: string | null
) {
  return environments.find((environment) => environment.name === environmentName) ?? null;
}

export function getFallbackEnvironmentName({
  defaultEnvironment,
  environments,
  selectedEnvironmentName,
}: {
  defaultEnvironment?: string;
  environments: WorkspaceConfigEnvironment[];
  selectedEnvironmentName?: string | null;
}) {
  return selectedEnvironmentName || defaultEnvironment || environments[0]?.name || "";
}

export function getSelectedEnvironmentNameFromResponse(
  response: WorkspaceConfigResponse,
  preferredName?: string | null
) {
  return preferredName || response.default_environment || response.environments[0]?.name || null;
}

export function getSelectedConnectionNameFromEnvironment(
  environment: WorkspaceConfigEnvironment | null | undefined,
  preferredName?: string | null
) {
  return preferredName || environment?.connections[0]?.name || null;
}

export function findConnectionByName(
  environment: WorkspaceConfigEnvironment | null,
  connectionName?: string | null
) {
  return environment?.connections.find((connection) => connection.name === connectionName) ?? null;
}

export function trimOptionalValue(value: string) {
  const trimmed = value.trim();
  return trimmed || undefined;
}

export function buildConnectionFieldDefaults({
  connectionTypes,
  existingConnection,
  previousValues,
  typeName,
}: {
  connectionTypes: Array<{
    type_name: string;
    fields: Array<{
      name: string;
      type: string;
      default_value?: string;
    }>;
  }>; 
  existingConnection: WorkspaceConfigConnection | null;
  previousValues?: Record<string, string | number | boolean>;
  typeName: string;
}) {
  const connectionType = connectionTypes.find((candidate) => candidate.type_name === typeName);
  const values: Record<string, string | number | boolean> = {};

  for (const field of connectionType?.fields ?? []) {
    const existingValue = existingConnection?.values[field.name];
    const previousValue = previousValues?.[field.name];
    if (existingValue !== undefined && existingValue !== null) {
      values[field.name] = existingValue as string | number | boolean;
      continue;
    }
    if (previousValue !== undefined) {
      values[field.name] = previousValue;
      continue;
    }
    if (field.type === "bool") {
      values[field.name] = field.default_value === "true";
      continue;
    }
    if (field.type === "int") {
      values[field.name] = field.default_value ? Number(field.default_value) : "";
      continue;
    }
    values[field.name] = field.default_value ?? "";
  }

  return values;
}
