import {
  WorkspaceConfigConnection,
  WorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
  WorkspaceConnectionSecretChanges,
} from "@/lib/types";

export function findEnvironmentByName(
  environments: WorkspaceConfigEnvironment[],
  environmentName?: string | null,
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
  preferredName?: string | null,
) {
  return preferredName || response.default_environment || response.environments[0]?.name || null;
}

export function getSelectedConnectionNameFromEnvironment(
  environment: WorkspaceConfigEnvironment | null | undefined,
  preferredName?: string | null,
) {
  return preferredName || environment?.connections[0]?.name || null;
}

export function findConnectionByName(
  environment: WorkspaceConfigEnvironment | null,
  connectionName?: string | null,
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
      is_sensitive?: boolean;
      is_sensitive_file?: boolean;
    }>;
  }>;
  existingConnection: WorkspaceConfigConnection | null;
  previousValues?: Record<string, string | number | boolean | string[]>;
  typeName: string;
}) {
  const connectionType = connectionTypes.find((candidate) => candidate.type_name === typeName);
  const values: Record<string, string | number | boolean | string[]> = {};

  for (const field of connectionType?.fields ?? []) {
    if (field.is_sensitive || field.is_sensitive_file) {
      continue;
    }
    const existingValue = existingConnection?.values[field.name];
    const previousValue = previousValues?.[field.name];
    if (existingValue !== undefined && existingValue !== null) {
      values[field.name] = existingValue as string | number | boolean | string[];
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
    if (field.type === "string_array") {
      values[field.name] = field.default_value
        ? field.default_value
            .split(",")
            .map((item) => item.trim())
            .filter(Boolean)
        : [];
      continue;
    }
    values[field.name] = field.default_value ?? "";
  }

  return values;
}

export function buildConnectionSecretChanges(
  connectionType: WorkspaceConfigConnectionType | null | undefined,
): WorkspaceConnectionSecretChanges {
  const changes: WorkspaceConnectionSecretChanges = {};
  for (const field of connectionType?.fields ?? []) {
    if (field.is_sensitive || field.is_sensitive_file) {
      changes[field.name] = { action: "keep" };
    }
  }
  return changes;
}

export function splitConnectionDraftValues(
  connectionType: WorkspaceConfigConnectionType | null | undefined,
  draftValues: Record<string, unknown>,
  secretStorageModes: Record<string, "local" | "env"> = {},
) {
  const sensitiveFields = new Set(
    (connectionType?.fields ?? [])
      .filter((field) => field.is_sensitive || field.is_sensitive_file)
      .map((field) => field.name),
  );
  const values: Record<string, unknown> = {};
  const secretChanges: WorkspaceConnectionSecretChanges = {};

  for (const [name, value] of Object.entries(draftValues)) {
    if (!sensitiveFields.has(name)) {
      values[name] = value;
      continue;
    }
    const secretValue = typeof value === "string" ? value : String(value ?? "");
    if (secretStorageModes[name] === "env") {
      secretChanges[name] = secretValue
        ? { action: "replace", binding: { ref: `env:${secretValue}` } }
        : { action: "keep" };
    } else {
      secretChanges[name] = secretValue
        ? { action: "replace", value: secretValue }
        : { action: "keep" };
    }
  }
  for (const name of sensitiveFields) {
    secretChanges[name] ??= { action: "keep" };
  }

  return { values, secretChanges };
}

export function connectionSecretsReady({
  connection,
  connectionType,
  secretChanges,
}: {
  connection: WorkspaceConfigConnection | null;
  connectionType: WorkspaceConfigConnectionType | null;
  secretChanges: WorkspaceConnectionSecretChanges;
}) {
  return (connectionType?.fields ?? []).every((field) => {
    if ((!field.is_sensitive && !field.is_sensitive_file) || !field.is_required) {
      return true;
    }
    const change = secretChanges[field.name];
    if (change?.action === "replace") {
      if (change.binding?.ref?.startsWith("env:")) {
        return /^env:[A-Za-z_][A-Za-z0-9_]*$/.test(change.binding.ref);
      }
      return Boolean(change.value);
    }
    if (change?.action === "clear") {
      return false;
    }
    const descriptor = connection?.secret_fields?.[field.name];
    return descriptor?.status === "configured" || Boolean(descriptor?.reference);
  });
}
