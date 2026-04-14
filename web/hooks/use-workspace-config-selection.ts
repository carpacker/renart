"use client";

import { useEffect, useMemo } from "react";

import { WorkspaceConfigEnvironment } from "@/lib/types";

export function useResolvedWorkspaceEnvironment({
  defaultEnvironment,
  environments,
  selectedEnvironmentName,
  onSelectedEnvironmentChange,
}: {
  defaultEnvironment?: string;
  environments: WorkspaceConfigEnvironment[];
  selectedEnvironmentName?: string | null;
  onSelectedEnvironmentChange: (name: string | null) => void;
}) {
  const resolvedEnvironmentName = useMemo(() => {
    if (
      selectedEnvironmentName &&
      environments.some(
        (environment) => environment.name === selectedEnvironmentName
      )
    ) {
      return selectedEnvironmentName;
    }

    return defaultEnvironment || environments[0]?.name || null;
  }, [defaultEnvironment, environments, selectedEnvironmentName]);

  useEffect(() => {
    if (resolvedEnvironmentName !== selectedEnvironmentName) {
      onSelectedEnvironmentChange(resolvedEnvironmentName);
    }
  }, [
    onSelectedEnvironmentChange,
    resolvedEnvironmentName,
    selectedEnvironmentName,
  ]);

  const activeEnvironment = useMemo(
    () =>
      environments.find(
        (environment) => environment.name === resolvedEnvironmentName
      ) ?? null,
    [environments, resolvedEnvironmentName]
  );

  return {
    activeEnvironment,
    resolvedEnvironmentName,
  };
}

export function useResolvedWorkspaceConnection({
  activeEnvironment,
  selectedConnectionName,
  onSelectedConnectionChange,
}: {
  activeEnvironment: WorkspaceConfigEnvironment | null;
  selectedConnectionName?: string | null;
  onSelectedConnectionChange: (name: string | null) => void;
}) {
  const resolvedConnectionName = useMemo(() => {
    if (
      selectedConnectionName &&
      activeEnvironment?.connections.some(
        (connection) => connection.name === selectedConnectionName
      )
    ) {
      return selectedConnectionName;
    }

    return activeEnvironment?.connections[0]?.name ?? null;
  }, [activeEnvironment, selectedConnectionName]);

  useEffect(() => {
    if (resolvedConnectionName !== selectedConnectionName) {
      onSelectedConnectionChange(resolvedConnectionName);
    }
  }, [
    onSelectedConnectionChange,
    resolvedConnectionName,
    selectedConnectionName,
  ]);

  const activeConnection = useMemo(
    () =>
      activeEnvironment?.connections.find(
        (connection) => connection.name === resolvedConnectionName
      ) ?? null,
    [activeEnvironment, resolvedConnectionName]
  );

  return {
    activeConnection,
    resolvedConnectionName,
  };
}
