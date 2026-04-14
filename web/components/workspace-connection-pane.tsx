"use client";

import { CSSProperties, useState } from "react";

import { WorkspaceConnectionFormFields } from "@/components/workspace-connection-form-fields";
import { WorkspaceConfigPaneLayout } from "@/components/workspace-config-pane-layout";
import { testWorkspaceConnection } from "@/lib/api";
import {
  ConnectionMode,
  useWorkspaceConnectionForm,
} from "@/hooks/use-workspace-connection-form";
import {
  WorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

type WorkspaceConnectionPaneProps = {
  configPath: string;
  defaultEnvironment?: string;
  selectedEnvironment?: string | null;
  selectedConnectionName?: string | null;
  environments: WorkspaceConfigEnvironment[];
  connectionTypes: WorkspaceConfigConnectionType[];
  loading: boolean;
  busy: boolean;
  parseError?: string;
  helpMode?: boolean;
  highlighted?: boolean;
  highlightStyle?: CSSProperties;
  statusMessage?: string | null;
  statusTone?: "error" | "success" | null;
  mode: ConnectionMode;
  requestedConnectionType?: string;
  onModeChange: (mode: ConnectionMode) => void;
  onSelectedEnvironmentChange: (name: string | null) => void;
  onSelectedConnectionChange: (name: string | null) => void;
  onReload: () => void;
  onCreateConnection: (input: {
    environment_name: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onUpdateConnection: (input: {
    environment_name: string;
    current_name?: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onDeleteConnection: (input: {
    environment_name: string;
    name: string;
  }) => Promise<WorkspaceConfigResponse>;
};

export function WorkspaceConnectionPane({
  configPath,
  defaultEnvironment,
  selectedEnvironment,
  selectedConnectionName,
  environments,
  connectionTypes,
  loading,
  busy,
  parseError,
  helpMode = false,
  highlighted = false,
  highlightStyle,
  statusMessage,
  statusTone,
  mode,
  requestedConnectionType,
  onModeChange,
  onSelectedEnvironmentChange,
  onSelectedConnectionChange,
  onReload,
  onCreateConnection,
  onUpdateConnection,
  onDeleteConnection,
}: WorkspaceConnectionPaneProps) {
  const {
    activeConnection,
    connectionForm,
    selectedConnectionType,
    setConnectionForm,
    handleDelete,
    handleSave,
  } = useWorkspaceConnectionForm({
    connectionTypes,
    defaultEnvironment,
    environments,
    mode,
    onCreateConnection,
    onDeleteConnection,
    onModeChange,
    onSelectedConnectionChange,
    onSelectedEnvironmentChange,
    onUpdateConnection,
    selectedConnectionName,
    selectedEnvironmentName: selectedEnvironment,
    requestedConnectionType,
  });
  const [validateBusy, setValidateBusy] = useState(false);
  const [validateMessage, setValidateMessage] = useState<string | null>(null);
  const [validateTone, setValidateTone] = useState<"error" | "success" | null>(null);

  const canValidate =
    Boolean(connectionForm.environmentName) && Boolean(connectionForm.name.trim());

  const handleValidate = async () => {
    if (!canValidate) {
      return;
    }

    setValidateBusy(true);
    setValidateMessage(null);
    setValidateTone(null);
    try {
      const response = await testWorkspaceConnection({
        environment_name: connectionForm.environmentName,
        current_name: activeConnection?.name,
        name: connectionForm.name.trim(),
        type: connectionForm.type,
        values: connectionForm.values,
      });
      setValidateMessage(response.message ?? "Connection validated.");
      setValidateTone("success");
    } catch (error) {
      setValidateMessage(
        error instanceof Error ? error.message : "Connection validation failed."
      );
      setValidateTone("error");
    } finally {
      setValidateBusy(false);
    }
  };

  return (
    <WorkspaceConfigPaneLayout
      title="Connection Editor"
      configPath={configPath}
      loading={loading}
      busy={busy}
      parseError={parseError}
      helpMode={helpMode}
      highlighted={highlighted}
      highlightStyle={highlightStyle}
      statusMessage={statusMessage}
      statusTone={statusTone}
      onReload={onReload}
    >
      <WorkspaceConnectionFormFields
        activeConnectionExists={Boolean(activeConnection)}
        busy={busy}
        canValidate={canValidate}
        connectionForm={connectionForm}
        connectionTypes={connectionTypes}
        environments={environments}
        mode={mode}
        selectedConnectionType={selectedConnectionType}
        selectedEnvironment={selectedEnvironment}
        validateBusy={validateBusy}
        validateMessage={validateMessage}
        validateTone={validateTone}
        onDelete={() => void handleDelete()}
        onEnvironmentChange={(value) => {
          onSelectedEnvironmentChange(value);
          onModeChange("edit");
          setConnectionForm((current) => ({
            ...current,
            environmentName: value,
          }));
        }}
        onFieldValueChange={(fieldName, value) =>
          setConnectionForm((current) => ({
            ...current,
            values: {
              ...current.values,
              [fieldName]: value,
            },
          }))
        }
        onNameChange={(value) =>
          setConnectionForm((current) => ({
            ...current,
            name: value,
          }))
        }
        onSave={() => void handleSave()}
        onTypeChange={(value) =>
          setConnectionForm((current) => ({
            ...current,
            type: value,
            values: buildTypeValues({
              activeConnection,
              connectionTypes,
              mode,
              previousValues: current.values,
              typeName: value,
            }),
          }))
        }
        onValidate={() => void handleValidate()}
      />
    </WorkspaceConfigPaneLayout>
  );
}

function buildTypeValues({
  activeConnection,
  connectionTypes,
  mode,
  previousValues,
  typeName,
}: {
  activeConnection: { type: string } | null;
  connectionTypes: WorkspaceConfigConnectionType[];
  mode: ConnectionMode;
  previousValues: Record<string, string | number | boolean>;
  typeName: string;
}) {
  const connectionType = connectionTypes.find(
    (candidate) => candidate.type_name === typeName
  );
  const values: Record<string, string | number | boolean> = {};

  for (const field of connectionType?.fields ?? []) {
    const previousValue = previousValues[field.name];
    if (mode === "edit" && activeConnection?.type === typeName && previousValue !== undefined) {
      values[field.name] = previousValue;
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
