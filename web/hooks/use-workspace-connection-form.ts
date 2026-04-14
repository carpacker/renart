"use client";

import { Dispatch, SetStateAction, useEffect, useMemo, useState } from "react";

import {
  buildConnectionFieldDefaults,
  findConnectionByName,
  findEnvironmentByName,
  getFallbackEnvironmentName,
  getSelectedConnectionNameFromEnvironment,
} from "@/lib/settings-form-utils";
import {
  WorkspaceConfigConnectionType,
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

export type ConnectionMode = "edit" | "create";

type ConnectionFormState = {
  environmentName: string;
  name: string;
  type: string;
  values: Record<string, string | number | boolean>;
};

export function useWorkspaceConnectionForm({
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
  requestedConnectionType,
  selectedConnectionName,
  selectedEnvironmentName,
}: {
  connectionTypes: WorkspaceConfigConnectionType[];
  defaultEnvironment?: string;
  environments: WorkspaceConfigEnvironment[];
  mode: ConnectionMode;
  onCreateConnection: (input: {
    environment_name: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  onDeleteConnection: (input: {
    environment_name: string;
    name: string;
  }) => Promise<WorkspaceConfigResponse>;
  onModeChange: (mode: ConnectionMode) => void;
  onSelectedConnectionChange: (name: string | null) => void;
  onSelectedEnvironmentChange: (name: string | null) => void;
  onUpdateConnection: (input: {
    environment_name: string;
    current_name?: string;
    name: string;
    type: string;
    values: Record<string, unknown>;
  }) => Promise<WorkspaceConfigResponse>;
  requestedConnectionType?: string;
  selectedConnectionName?: string | null;
  selectedEnvironmentName?: string | null;
}) {
  const [connectionForm, setConnectionForm] = useState<ConnectionFormState>({
    environmentName: "",
    name: "",
    type: "",
    values: {},
  });

  const activeEnvironment = useMemo(
    () => findEnvironmentByName(environments, selectedEnvironmentName),
    [environments, selectedEnvironmentName]
  );

  const activeConnection = useMemo(
    () => findConnectionByName(activeEnvironment, selectedConnectionName),
    [activeEnvironment, selectedConnectionName]
  );

  const selectedConnectionType = useMemo(
    () =>
      connectionTypes.find(
        (connectionType) => connectionType.type_name === connectionForm.type
      ) ?? null,
    [connectionForm.type, connectionTypes]
  );

  useEffect(() => {
    if (mode === "create") {
      const requestedType = requestedConnectionType?.trim() ?? "";
      const fallbackType =
        connectionTypes.find(
          (connectionType) => connectionType.type_name === requestedType
        )?.type_name ??
        connectionTypes[0]?.type_name ??
        "";
      setConnectionForm({
        environmentName: getFallbackEnvironmentName({
          defaultEnvironment,
          environments,
          selectedEnvironmentName,
        }),
        name: "",
        type: fallbackType,
        values: buildConnectionFieldDefaults({
          connectionTypes,
          typeName: fallbackType,
          existingConnection: null,
        }),
      });
      return;
    }

    if (!activeConnection || !activeEnvironment) {
      setConnectionForm({
        environmentName: selectedEnvironmentName ?? "",
        name: "",
        type: connectionTypes[0]?.type_name ?? "",
        values: buildConnectionFieldDefaults({
          connectionTypes,
          typeName: connectionTypes[0]?.type_name ?? "",
          existingConnection: null,
        }),
      });
      return;
    }

    setConnectionForm({
      environmentName: activeEnvironment.name,
      name: activeConnection.name,
      type: activeConnection.type,
      values: buildConnectionFieldDefaults({
        connectionTypes,
        typeName: activeConnection.type,
        existingConnection: activeConnection,
      }),
    });
  }, [
    activeConnection,
    activeEnvironment,
    connectionTypes,
    defaultEnvironment,
    environments,
    mode,
    requestedConnectionType,
    selectedEnvironmentName,
  ]);

  const handleSave = async () => {
    const payload = {
      environment_name: connectionForm.environmentName,
      name: connectionForm.name.trim(),
      type: connectionForm.type,
      values: connectionForm.values,
    };

    if (mode === "create") {
      const response = await onCreateConnection(payload);
      const environment = findEnvironmentByName(
        response.environments,
        connectionForm.environmentName
      );
      onSelectedEnvironmentChange(connectionForm.environmentName);
      onSelectedConnectionChange(
        getSelectedConnectionNameFromEnvironment(environment, connectionForm.name.trim())
      );
      onModeChange("edit");
      return;
    }

    const response = await onUpdateConnection({
      ...payload,
      current_name: activeConnection?.name,
    });
    const environment = findEnvironmentByName(
      response.environments,
      connectionForm.environmentName
    );
    onSelectedConnectionChange(
      getSelectedConnectionNameFromEnvironment(environment, connectionForm.name.trim())
    );
  };

  const handleDelete = async () => {
    if (!activeConnection || !activeEnvironment) {
      return;
    }

    const response = await onDeleteConnection({
      environment_name: activeEnvironment.name,
      name: activeConnection.name,
    });
    const environment = findEnvironmentByName(response.environments, activeEnvironment.name);
    onSelectedConnectionChange(getSelectedConnectionNameFromEnvironment(environment));
  };

  return {
    activeConnection,
    activeEnvironment,
    connectionForm,
    selectedConnectionType,
    setConnectionForm: setConnectionForm as Dispatch<SetStateAction<ConnectionFormState>>,
    handleDelete,
    handleSave,
  };
}
