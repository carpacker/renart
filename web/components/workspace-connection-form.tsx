"use client";

import { forwardRef, useEffect, useImperativeHandle, useState } from "react";

import { WorkspaceConnectionFormFields } from "@/components/workspace-connection-form-fields";
import { WorkspaceSettingsFormShell } from "@/components/workspace-settings-form-shell";
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

export type WorkspaceConnectionFormHandle = {
  save: () => Promise<void>;
  validate: () => Promise<void>;
  canSave: boolean;
  canValidate: boolean;
};

type WorkspaceConnectionFormProps = {
  configPath: string;
  defaultEnvironment?: string;
  selectedEnvironment?: string | null;
  selectedConnectionName?: string | null;
  environments: WorkspaceConfigEnvironment[];
  connectionTypes: WorkspaceConfigConnectionType[];
  loading: boolean;
  busy: boolean;
  parseError?: string;
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
  allowEnvironmentSelection?: boolean;
  showEnvironmentSelector?: boolean;
  showHeader?: boolean;
  showConfigPath?: boolean;
  showReload?: boolean;
  useCard?: boolean;
  showActions?: boolean;
  onStateChange?: (state: { canSave: boolean; canValidate: boolean }) => void;
};

export const WorkspaceConnectionForm = forwardRef<
  WorkspaceConnectionFormHandle,
  WorkspaceConnectionFormProps
>(function WorkspaceConnectionForm({
  configPath,
  defaultEnvironment,
  selectedEnvironment,
  selectedConnectionName,
  environments,
  connectionTypes,
  loading,
  busy,
  parseError,
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
  allowEnvironmentSelection = true,
  showEnvironmentSelector = true,
  showHeader = true,
  showConfigPath = true,
  showReload = true,
  useCard = true,
  showActions = true,
  onStateChange,
}, ref) {
  const {
    activeConnection,
    connectionForm,
    selectedConnectionType,
    setConnectionForm,
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
  const canSave =
    !busy &&
    Boolean(connectionForm.environmentName) &&
    Boolean(connectionForm.name.trim()) &&
    Boolean(connectionForm.type);

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

  const title =
    mode === "create"
      ? "Create Connection"
      : activeConnection
        ? `Edit ${activeConnection.name}`
        : "Connection";

  useImperativeHandle(
    ref,
    () => ({
      save: async () => {
        if (!canSave) {
          return;
        }

        await handleSave();
      },
      validate: async () => {
        if (!canValidate || busy || validateBusy) {
          return;
        }

        await handleValidate();
      },
      canSave,
      canValidate: canValidate && !busy && !validateBusy,
    }),
    [busy, canSave, canValidate, handleSave, validateBusy]
  );

  useEffect(() => {
    onStateChange?.({
      canSave,
      canValidate: canValidate && !busy && !validateBusy,
    });
  }, [busy, canSave, canValidate, onStateChange, validateBusy]);

  return (
    <WorkspaceSettingsFormShell
      title={title}
      configPath={configPath}
      loading={loading}
      busy={busy}
      parseError={parseError}
      statusMessage={statusMessage}
      statusTone={statusTone}
      onReload={onReload}
      showHeader={showHeader}
      showConfigPath={showConfigPath}
      showReload={showReload}
      useCard={useCard}
    >
      <WorkspaceConnectionFormFields
        busy={busy}
        canValidate={canValidate}
        connectionForm={connectionForm}
        connectionTypes={connectionTypes}
        environments={environments}
        mode={mode}
        selectedConnectionType={selectedConnectionType}
        selectedEnvironment={selectedEnvironment}
        environmentDisabled={!allowEnvironmentSelection}
        showEnvironmentSelector={showEnvironmentSelector}
        validateBusy={validateBusy}
        validateMessage={validateMessage}
        validateTone={validateTone}
        showActions={showActions}
        onEnvironmentChange={(value) => {
          if (!allowEnvironmentSelection) {
            return;
          }

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
    </WorkspaceSettingsFormShell>
  );
});

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
  previousValues: Record<string, string | number | boolean | string[]>;
  typeName: string;
}) {
  const connectionType = connectionTypes.find(
    (candidate) => candidate.type_name === typeName
  );
  const values: Record<string, string | number | boolean | string[]> = {};

  for (const field of connectionType?.fields ?? []) {
    const previousValue = previousValues[field.name];
    if (
      mode === "edit" &&
      activeConnection?.type === typeName &&
      previousValue !== undefined
    ) {
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
    if (field.type === "string_array") {
      values[field.name] = field.default_value
        ? field.default_value.split(",").map((item) => item.trim()).filter(Boolean)
        : [];
      continue;
    }
    values[field.name] = field.default_value ?? "";
  }

  return values;
}
