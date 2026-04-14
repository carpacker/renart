"use client";

import { useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo, useRef } from "react";

import { syncOnboardingAtomsAtom } from "@/lib/atoms/onboarding";
import { updateOnboardingState } from "@/lib/api";
import { buildConnectionFieldDefaults } from "@/lib/settings-form-utils";
import {
  OnboardingImportResponse,
  OnboardingSessionState,
  WorkspaceConfigConnectionType,
  WorkspaceConfigResponse,
} from "@/lib/types";

const FEATURED_TYPES = ["postgres", "duckdb", "snowflake", "google_cloud_platform", "redshift", "databricks"];

type Params = {
  workspaceConfig: WorkspaceConfigResponse;
  onboardingState: OnboardingSessionState;
  selectedType: string;
  step: string;
  draftValues: Record<string, string | number | boolean>;
  importForm: {
    database: string;
    pipelineName: string;
    schema: string;
    pattern: string;
    disableColumns: boolean;
  };
  selectedTables: string[];
  importResult: OnboardingImportResponse | null;
  setDraftValues: (values: Record<string, string | number | boolean>) => void;
};

export function useOnboardingPersistence({
  workspaceConfig,
  onboardingState,
  selectedType,
  step,
  draftValues,
  importForm,
  selectedTables,
  importResult,
  setDraftValues,
}: Params) {
  const setSyncedState = useSetAtom(syncOnboardingAtomsAtom);
  const environments = workspaceConfig.environments;
  const connectionTypes = workspaceConfig.connection_types;
  const defaultEnvironment =
    onboardingState.environment_name ||
    workspaceConfig.selected_environment ||
    workspaceConfig.default_environment ||
    environments[0]?.name ||
    "default";

  const featuredTypes = useMemo(
    () =>
      FEATURED_TYPES.map((type) => connectionTypes.find((item) => item.type_name === type)).filter(Boolean) as WorkspaceConfigConnectionType[],
    [connectionTypes]
  );

  const lastSyncedStateRef = useRef<string>("");

  useEffect(() => {
    const nextSyncKey = JSON.stringify(onboardingState);
    if (lastSyncedStateRef.current === nextSyncKey) {
      return;
    }
    lastSyncedStateRef.current = nextSyncKey;

    setSyncedState({
      ...onboardingState,
      selected_type_fallback: featuredTypes[0]?.type_name || "postgres",
    });
  }, [featuredTypes, onboardingState, setSyncedState]);

  const defaultDraftValues = useMemo(
    () =>
      applyOnboardingDraftDefaults(
        selectedType,
        buildConnectionFieldDefaults({
          connectionTypes,
          existingConnection: null,
          typeName: selectedType,
        })
      ),
    [connectionTypes, selectedType]
  );

  useEffect(() => {
    if (
      step !== "connection-config" ||
      !selectedType ||
      Object.keys(draftValues).length > 0 ||
      Object.keys(onboardingState.draft_values ?? {}).length > 0
    ) {
      return;
    }
    setDraftValues(defaultDraftValues);
  }, [defaultDraftValues, draftValues, onboardingState.draft_values, selectedType, setDraftValues, step]);

  const persistState = useCallback(
    async (overrides: Partial<OnboardingSessionState> = {}) => {
      await updateOnboardingState({
        active: true,
        step: step as OnboardingSessionState["step"],
        selected_type: selectedType,
        environment_name: defaultEnvironment,
        draft_values: draftValues,
        import_form: {
          database: importForm.database,
          pipeline_name: importForm.pipelineName,
          schema: importForm.schema,
          pattern: importForm.pattern,
          disable_columns: importForm.disableColumns,
        },
        selected_tables: selectedTables,
        import_result: importResult
          ? {
              output: importResult.output,
              error: importResult.error,
              pipeline_path: importResult.pipeline_path,
              asset_paths: importResult.asset_paths,
            }
          : null,
        ...overrides,
      });
    },
    [defaultEnvironment, draftValues, importForm, importResult, selectedTables, selectedType, step]
  );

  return {
    connectionTypes,
    defaultDraftValues,
    defaultEnvironment,
    featuredTypes,
    persistState,
  };
}

function applyOnboardingDraftDefaults(
  typeName: string,
  values: Record<string, string | number | boolean>
) {
  if (typeName === "postgres") {
    return {
      ...values,
      ssl_mode: "disable",
    };
  }

  return values;
}
