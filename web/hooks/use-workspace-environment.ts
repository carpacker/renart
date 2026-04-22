"use client";

import {
  useNavigate,
  useRouterState,
} from "@tanstack/react-router";
import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo } from "react";

import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import {
  assetInspectAtom,
  changedAssetIdsAtom,
  emptyAssetInspectState,
} from "@/lib/atoms/domains/results";
import {
  selectedEnvironmentAtom,
  selectedEnvironmentOverrideAtom,
} from "@/lib/atoms/domains/workspace";

export function useWorkspaceEnvironment() {
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const setSelectedEnvironmentOverride = useSetAtom(
    selectedEnvironmentOverrideAtom
  );
  const setAssetInspectState = useSetAtom(assetInspectAtom);
  const setChangedAssetIds = useSetAtom(changedAssetIdsAtom);
  const { normalizedConfigEnvironments } = useWorkspaceSettingsData();
  const navigate = useNavigate();
  const routeState = useRouterState({
    select: (state) => ({
      pathname: state.location.pathname,
      search: state.location.search as { environment?: string },
      environmentId:
        (state.matches[state.matches.length - 1]?.params as
          | { environmentId?: string }
          | undefined)?.environmentId,
    }),
  });

  useEffect(() => {
    setSelectedEnvironmentOverride(
      routeState.search.environment ?? routeState.environmentId
    );
  }, [
    routeState.environmentId,
    routeState.search.environment,
    setSelectedEnvironmentOverride,
  ]);

  useEffect(() => {
    setAssetInspectState(emptyAssetInspectState);
    setChangedAssetIds(new Set<string>());
  }, [selectedEnvironment, setAssetInspectState, setChangedAssetIds]);

  const availableEnvironmentNames = useMemo(
    () =>
      normalizedConfigEnvironments
        .map((environment) => environment.name)
        .filter((name) => name !== "default"),
    [normalizedConfigEnvironments]
  );

  const selectedEnvironmentLabel = selectedEnvironment || "default";

  const handleEnvironmentChange = useCallback(
    (nextEnvironment: string) => {
      const environment =
        nextEnvironment === "__default__" ? undefined : nextEnvironment;

      void navigate({
        to: routeState.pathname,
        search: {
          ...routeState.search,
          environment,
        },
        replace: true,
      });
    },
    [navigate, routeState.pathname, routeState.search]
  );

  return {
    availableEnvironmentNames,
    handleEnvironmentChange,
    selectedEnvironment,
    selectedEnvironmentLabel,
  };
}
