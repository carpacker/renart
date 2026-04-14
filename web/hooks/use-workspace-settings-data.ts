"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import {
  cloneWorkspaceEnvironment,
  createWorkspaceConnection,
  createWorkspaceEnvironment,
  deleteWorkspaceConnection,
  deleteWorkspaceEnvironment,
  getWorkspaceConfig,
  updateWorkspaceConnection,
  updateWorkspaceEnvironment,
} from "@/lib/api";
import { WorkspaceConfigResponse } from "@/lib/types";

export function useWorkspaceSettingsData() {
  const [workspaceConfig, setWorkspaceConfig] =
    useState<WorkspaceConfigResponse | null>(null);
  const [workspaceConfigLoading, setWorkspaceConfigLoading] = useState(false);
  const [workspaceConfigBusy, setWorkspaceConfigBusy] = useState(false);
  const [workspaceConfigStatusMessage, setWorkspaceConfigStatusMessage] =
    useState<string | null>(null);
  const [workspaceConfigStatusTone, setWorkspaceConfigStatusTone] = useState<
    "error" | "success" | null
  >(null);

  const loadWorkspaceConfig = useCallback(async () => {
    setWorkspaceConfigLoading(true);
    try {
      const response = await getWorkspaceConfig();
      setWorkspaceConfig(response);
      setWorkspaceConfigStatusMessage(null);
      setWorkspaceConfigStatusTone(null);
    } catch (error) {
      setWorkspaceConfigStatusMessage(
        error instanceof Error
          ? error.message
          : "Failed to load workspace config."
      );
      setWorkspaceConfigStatusTone("error");
    } finally {
      setWorkspaceConfigLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadWorkspaceConfig();
  }, [loadWorkspaceConfig]);

  const normalizedConfigEnvironments = useMemo(
    () =>
      [...(workspaceConfig?.environments ?? [])].sort((left, right) =>
        left.name.localeCompare(right.name)
      ),
    [workspaceConfig]
  );

  const fallbackConfigEnvironment = useMemo(
    () =>
      workspaceConfig?.selected_environment ||
      workspaceConfig?.default_environment ||
      normalizedConfigEnvironments[0]?.name ||
      null,
    [normalizedConfigEnvironments, workspaceConfig]
  );

  const runWorkspaceConfigMutation = useCallback(
    async (
      operation: () => Promise<WorkspaceConfigResponse>,
      successMessage: string
    ) => {
      setWorkspaceConfigBusy(true);
      setWorkspaceConfigStatusMessage(null);
      setWorkspaceConfigStatusTone(null);

      try {
        const response = await operation();
        setWorkspaceConfig(response);
        setWorkspaceConfigStatusMessage(successMessage);
        setWorkspaceConfigStatusTone("success");
        return response;
      } catch (error) {
        setWorkspaceConfigStatusMessage(
          error instanceof Error
            ? error.message
            : "Workspace config update failed."
        );
        setWorkspaceConfigStatusTone("error");
        throw error;
      } finally {
        setWorkspaceConfigBusy(false);
      }
    },
    []
  );

  const handleCreateWorkspaceEnvironment = useCallback(
    (input: {
      name: string;
      schema_prefix?: string;
      set_as_default?: boolean;
    }) =>
      runWorkspaceConfigMutation(
        () => createWorkspaceEnvironment(input),
        `Environment "${input.name}" created.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleUpdateWorkspaceEnvironment = useCallback(
    (input: {
      name: string;
      new_name?: string;
      schema_prefix?: string;
      set_as_default?: boolean;
    }) =>
      runWorkspaceConfigMutation(
        () => updateWorkspaceEnvironment(input),
        `Environment "${input.new_name || input.name}" saved.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleCloneWorkspaceEnvironment = useCallback(
    (input: {
      source_name: string;
      target_name: string;
      schema_prefix?: string;
      set_as_default?: boolean;
    }) =>
      runWorkspaceConfigMutation(
        () => cloneWorkspaceEnvironment(input),
        `Environment "${input.target_name}" cloned.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleDeleteWorkspaceEnvironment = useCallback(
    (name: string) =>
      runWorkspaceConfigMutation(
        () => deleteWorkspaceEnvironment(name),
        `Environment "${name}" deleted.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleCreateWorkspaceConnection = useCallback(
    (input: {
      environment_name: string;
      name: string;
      type: string;
      values: Record<string, unknown>;
    }) =>
      runWorkspaceConfigMutation(
        () => createWorkspaceConnection(input),
        `Connection "${input.name}" created.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleUpdateWorkspaceConnection = useCallback(
    (input: {
      environment_name: string;
      current_name?: string;
      name: string;
      type: string;
      values: Record<string, unknown>;
    }) =>
      runWorkspaceConfigMutation(
        () => updateWorkspaceConnection(input),
        `Connection "${input.name}" saved.`
      ),
    [runWorkspaceConfigMutation]
  );

  const handleDeleteWorkspaceConnection = useCallback(
    (input: { environment_name: string; name: string }) =>
      runWorkspaceConfigMutation(
        () => deleteWorkspaceConnection(input),
        `Connection "${input.name}" deleted.`
      ),
    [runWorkspaceConfigMutation]
  );

  return {
    fallbackConfigEnvironment,
    handleCloneWorkspaceEnvironment,
    handleCreateWorkspaceConnection,
    handleCreateWorkspaceEnvironment,
    handleDeleteWorkspaceConnection,
    handleDeleteWorkspaceEnvironment,
    handleUpdateWorkspaceConnection,
    handleUpdateWorkspaceEnvironment,
    loadWorkspaceConfig,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigBusy,
    workspaceConfigLoading,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
  };
}