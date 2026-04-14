"use client";

import { Dispatch, SetStateAction, useEffect, useMemo, useState } from "react";

import {
  findEnvironmentByName,
  getSelectedEnvironmentNameFromResponse,
  trimOptionalValue,
} from "@/lib/settings-form-utils";
import {
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

export type EnvironmentMode = "edit" | "create" | "clone";

type EnvironmentFormState = {
  name: string;
  schemaPrefix: string;
  setAsDefault: boolean;
  cloneSourceName: string;
};

export function useWorkspaceEnvironmentForm({
  defaultEnvironment,
  environments,
  mode,
  onCloneEnvironment,
  onCreateEnvironment,
  onDeleteEnvironment,
  onModeChange,
  onSelectedEnvironmentChange,
  onUpdateEnvironment,
  selectedEnvironmentName,
}: {
  defaultEnvironment?: string;
  environments: WorkspaceConfigEnvironment[];
  mode: EnvironmentMode;
  onCloneEnvironment: (input: {
    source_name: string;
    target_name: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  onCreateEnvironment: (input: {
    name: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  onDeleteEnvironment: (name: string) => Promise<WorkspaceConfigResponse>;
  onModeChange: (mode: EnvironmentMode) => void;
  onSelectedEnvironmentChange: (name: string | null) => void;
  onUpdateEnvironment: (input: {
    name: string;
    new_name?: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  selectedEnvironmentName?: string | null;
}) {
  const [environmentForm, setEnvironmentForm] = useState<EnvironmentFormState>({
    name: "",
    schemaPrefix: "",
    setAsDefault: false,
    cloneSourceName: "",
  });

  const activeEnvironment = useMemo(
    () => findEnvironmentByName(environments, selectedEnvironmentName),
    [environments, selectedEnvironmentName]
  );

  useEffect(() => {
    if (mode === "create") {
      setEnvironmentForm({
        name: "",
        schemaPrefix: "",
        setAsDefault: environments.length === 0,
        cloneSourceName: selectedEnvironmentName ?? "",
      });
      return;
    }

    if (mode === "clone") {
      const sourceName = selectedEnvironmentName ?? environments[0]?.name ?? "";
      const sourceEnvironment = findEnvironmentByName(environments, sourceName);
      setEnvironmentForm({
        name: "",
        schemaPrefix: sourceEnvironment?.schema_prefix ?? "",
        setAsDefault: false,
        cloneSourceName: sourceName,
      });
      return;
    }

    if (!activeEnvironment) {
      setEnvironmentForm({
        name: "",
        schemaPrefix: "",
        setAsDefault: false,
        cloneSourceName: "",
      });
      return;
    }

    setEnvironmentForm({
      name: activeEnvironment.name,
      schemaPrefix: activeEnvironment.schema_prefix ?? "",
      setAsDefault: activeEnvironment.name === defaultEnvironment,
      cloneSourceName: activeEnvironment.name,
    });
  }, [activeEnvironment, defaultEnvironment, environments, mode, selectedEnvironmentName]);

  const handleSave = async () => {
    if (mode === "create") {
      const response = await onCreateEnvironment({
        name: environmentForm.name.trim(),
        schema_prefix: trimOptionalValue(environmentForm.schemaPrefix),
        set_as_default: environmentForm.setAsDefault,
      });
      onModeChange("edit");
      onSelectedEnvironmentChange(
        getSelectedEnvironmentNameFromResponse(response, environmentForm.name.trim())
      );
      return;
    }

    if (mode === "clone") {
      const response = await onCloneEnvironment({
        source_name: environmentForm.cloneSourceName,
        target_name: environmentForm.name.trim(),
        schema_prefix: trimOptionalValue(environmentForm.schemaPrefix),
        set_as_default: environmentForm.setAsDefault,
      });
      onModeChange("edit");
      onSelectedEnvironmentChange(
        getSelectedEnvironmentNameFromResponse(response, environmentForm.name.trim())
      );
      return;
    }

    if (!activeEnvironment) {
      return;
    }

    const response = await onUpdateEnvironment({
      name: activeEnvironment.name,
      new_name: environmentForm.name.trim(),
      schema_prefix: trimOptionalValue(environmentForm.schemaPrefix),
      set_as_default: environmentForm.setAsDefault,
    });
    onSelectedEnvironmentChange(
      getSelectedEnvironmentNameFromResponse(
        response,
        environmentForm.name.trim() || activeEnvironment.name
      )
    );
  };

  const handleDelete = async () => {
    if (!activeEnvironment) {
      return;
    }

    const response = await onDeleteEnvironment(activeEnvironment.name);
    onModeChange("edit");
    onSelectedEnvironmentChange(getSelectedEnvironmentNameFromResponse(response));
  };

  return {
    activeEnvironment,
    environmentForm,
    setEnvironmentForm: setEnvironmentForm as Dispatch<SetStateAction<EnvironmentFormState>>,
    handleDelete,
    handleSave,
  };
}
