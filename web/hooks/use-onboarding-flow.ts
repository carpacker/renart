"use client";

import { useAtom } from "jotai";
import { useCallback } from "react";
import { useNavigate } from "@tanstack/react-router";

import {
  onboardingBusyAtom,
  onboardingDiscoveryBusyAtom,
  onboardingDiscoveryErrorAtom,
  onboardingDiscoveryStateAtom,
  onboardingDraftValuesAtom,
  onboardingImportFormAtom,
  onboardingImportResultAtom,
  onboardingSelectedTablesAtom,
  onboardingSelectedTypeAtom,
  onboardingStepAtom,
  OnboardingStep,
} from "@/lib/atoms/onboarding";
import {
  importOnboardingDatabase,
  updateOnboardingState,
} from "@/lib/api";
import { buildConnectionFieldDefaults } from "@/lib/settings-form-utils";
import { useOnboardingDiscovery } from "@/hooks/use-onboarding-discovery";
import { useOnboardingPersistence } from "@/hooks/use-onboarding-persistence";
import { OnboardingSessionState, WorkspaceConfigResponse } from "@/lib/types";

type Params = {
  workspaceConfig: WorkspaceConfigResponse;
  onboardingState: OnboardingSessionState;
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
  onReloadConfig: () => Promise<void> | void;
  onReloadWorkspace?: () => Promise<void> | void;
};

export function useOnboardingFlow({
  workspaceConfig,
  onboardingState,
  onCreateConnection,
  onUpdateConnection,
  onReloadConfig,
  onReloadWorkspace,
}: Params) {
  const navigate = useNavigate();
  const [step, setStep] = useAtom(onboardingStepAtom);
  const [selectedType, setSelectedType] = useAtom(onboardingSelectedTypeAtom);
  const [busy, setBusy] = useAtom(onboardingBusyAtom);
  const [discoveryBusy, setDiscoveryBusy] = useAtom(onboardingDiscoveryBusyAtom);
  const [discoveryError, setDiscoveryError] = useAtom(onboardingDiscoveryErrorAtom);
  const [discoveryState, setDiscoveryState] = useAtom(onboardingDiscoveryStateAtom);
  const [selectedTables, setSelectedTables] = useAtom(onboardingSelectedTablesAtom);
  const [draftValues, setDraftValues] = useAtom(onboardingDraftValuesAtom);
  const [importForm, setImportForm] = useAtom(onboardingImportFormAtom);
  const [importResult, setImportResult] = useAtom(onboardingImportResultAtom);

  const {
    connectionTypes,
    defaultDraftValues,
    defaultEnvironment,
    featuredTypes,
    persistState,
  } = useOnboardingPersistence({
    workspaceConfig,
    onboardingState,
    selectedType,
    step,
    draftValues,
    importForm,
    selectedTables,
    importResult,
    setDraftValues,
  });

  const connectionName = `${selectedType}-default`;
  const { runDiscovery } = useOnboardingDiscovery({
    step,
    selectedType,
    connectionName,
    defaultEnvironment,
    draftValues,
    importForm,
    selectedTables,
    discoveryBusy,
    discoveryState,
    setDiscoveryBusy,
    setDiscoveryError,
    setDiscoveryState,
    setDraftValues,
    setImportForm,
    setSelectedTables,
    setStep,
  });

  const navigateToStep = useCallback(
    async (nextStep: OnboardingStep) => {
      setStep(nextStep);
      await persistState({ step: nextStep });
      const nextPath =
        nextStep === "connection-type"
          ? "/onboarding"
          : nextStep === "connection-config"
            ? "/onboarding/connection"
            : nextStep === "import"
              ? "/onboarding/import"
              : "/onboarding/success";
      void navigate({ to: nextPath, replace: true });
    },
    [navigate, persistState, setStep]
  );

  const chooseType = useCallback(
    async (nextType: string) => {
      const nextDraftValues = applyOnboardingDraftDefaults(
        nextType,
        buildConnectionFieldDefaults({
          connectionTypes,
          existingConnection: null,
          typeName: nextType,
        })
      );
      setSelectedType(nextType);
      setDraftValues(nextDraftValues);
      setDiscoveryState({ status: "ok", databases: [], tables: [] });
      setDiscoveryError(null);
      setSelectedTables([]);
      setImportResult(null);
      setStep("connection-config");
      await persistState({
        step: "connection-config",
        selected_type: nextType,
        draft_values: nextDraftValues,
        import_form: {
          pipeline_name: importForm.pipelineName,
        },
        selected_tables: [],
        import_result: null,
      });
      void navigate({
        to: "/onboarding/connection",
        replace: true,
        search: { pipeline: undefined, asset: undefined },
      });
    },
    [connectionTypes, importForm.pipelineName, navigate, persistState, setDiscoveryError, setDiscoveryState, setDraftValues, setImportResult, setSelectedTables, setSelectedType, setStep]
  );

  const handleSaveAndImport = useCallback(async () => {
    if (!selectedType || !connectionName || (selectedType !== "duckdb" && !importForm.database.trim())) {
      return;
    }

    setBusy(true);
    try {
      const values = {
        ...draftValues,
        ...(selectedType === "duckdb" ? {} : { database: importForm.database.trim() }),
      };

      await onReloadConfig();
      const existingConnection = workspaceConfig.environments
        .find((item) => item.name === defaultEnvironment)
        ?.connections.find((item) => item.name === connectionName);

      if (existingConnection) {
        await onUpdateConnection({
          environment_name: defaultEnvironment,
          current_name: connectionName,
          name: connectionName,
          type: selectedType,
          values,
        });
      } else {
        await onCreateConnection({
          environment_name: defaultEnvironment,
          name: connectionName,
          type: selectedType,
          values,
        });
      }

      const response = await importOnboardingDatabase({
        connection_name: connectionName,
        environment_name: defaultEnvironment,
        pipeline_name: importForm.pipelineName.trim(),
        schema: importForm.schema.trim(),
        pattern: importForm.pattern.trim(),
        tables: normalizeOnboardingTableNames(selectedTables),
        disable_columns: importForm.disableColumns,
        create_if_missing: true,
      });

      setImportResult(response);
      if (response.status === "ok") {
        await onReloadWorkspace?.();
        setStep("success");
        await updateOnboardingState({
          active: true,
          step: "success",
          selected_type: selectedType,
          environment_name: defaultEnvironment,
          draft_values: values,
          import_form: {
            database: importForm.database,
            pipeline_name: importForm.pipelineName,
            schema: importForm.schema,
            pattern: importForm.pattern,
            disable_columns: importForm.disableColumns,
          },
          selected_tables: selectedTables,
          import_result: {
            output: response.output,
            error: response.error,
            pipeline_path: response.pipeline_path,
            asset_paths: response.asset_paths,
          },
        });
        void navigate({
          to: "/onboarding/success",
          replace: true,
          search: { pipeline: undefined, asset: undefined },
        });
      }
    } finally {
      setBusy(false);
    }
  }, [connectionName, defaultEnvironment, draftValues, importForm, onCreateConnection, onReloadConfig, onReloadWorkspace, onUpdateConnection, selectedTables, selectedType, setBusy, setImportResult, setStep, workspaceConfig.environments, navigate]);

  const handleSkip = useCallback(async () => {
    await updateOnboardingState({ active: false });
    void navigate({ to: "/", replace: true, search: { pipeline: undefined, asset: undefined } });
  }, [navigate]);

  const handleComplete = useCallback(async () => {
    await updateOnboardingState({ active: false });
    await onReloadConfig();
    void navigate({ to: "/", replace: true, search: { pipeline: undefined, asset: undefined } });
  }, [navigate, onReloadConfig]);

  const importDisabled =
    busy ||
    discoveryBusy ||
    !importForm.pipelineName.trim() ||
    (selectedType !== "duckdb" && !selectedTables.length) ||
    (selectedType !== "duckdb" && !importForm.database.trim());

  const updateImportFormField = useCallback(
    async (field: "database" | "pipelineName" | "schema" | "pattern", value: string) => {
      const nextImportForm = {
        ...importForm,
        [field]: value,
      };
      setImportForm(nextImportForm);
      await persistState({
        import_form: {
          database: nextImportForm.database,
          pipeline_name: nextImportForm.pipelineName,
          schema: nextImportForm.schema,
          pattern: nextImportForm.pattern,
          disable_columns: nextImportForm.disableColumns,
        },
      });
    },
    [importForm, persistState, setImportForm]
  );

  const updateSelectedTables = useCallback(
    async (nextSelectedTables: string[]) => {
      setSelectedTables(nextSelectedTables);
      await persistState({ selected_tables: nextSelectedTables });
    },
    [persistState, setSelectedTables]
  );

  return {
    connectionName,
    defaultDraftValues,
    defaultEnvironment,
    discoveryBusy,
    discoveryError,
    discoveryState,
    draftValues,
    featuredTypes,
    busy,
    importDisabled,
    importForm,
    importResult,
    navigateToStep,
    handleComplete,
    handleSaveAndImport,
    handleSkip,
    handleSelectDatabase: async (database: string) => {
      setImportForm((current) => ({ ...current, database }));
      await runDiscovery(draftValues, database);
    },
    runDiscovery,
    selectedTables,
    selectedType,
    updateImportFormField,
    updateSelectedTables,
    chooseType,
    step,
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

function normalizeOnboardingTableNames(tables: string[]) {
  return tables
    .map((table) => table.trim())
    .filter(Boolean)
    .map((table) => {
      const parts = table.split(".");
      return parts.length >= 3 ? parts.slice(-2).join(".") : table;
    });
}
