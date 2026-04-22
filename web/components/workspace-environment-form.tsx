"use client";

import { forwardRef, useEffect, useImperativeHandle } from "react";

import {
  EnvironmentMode,
  useWorkspaceEnvironmentForm,
} from "@/hooks/use-workspace-environment-form";
import {
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

import { WorkspaceEnvironmentFormFields } from "@/components/workspace-environment-form-fields";
import { WorkspaceSettingsFormShell } from "@/components/workspace-settings-form-shell";

export type WorkspaceEnvironmentFormHandle = {
  save: () => Promise<void>;
  canSave: boolean;
};

type WorkspaceEnvironmentFormProps = {
  configPath: string;
  defaultEnvironment?: string;
  selectedEnvironment?: string | null;
  environments: WorkspaceConfigEnvironment[];
  loading: boolean;
  busy: boolean;
  parseError?: string;
  statusMessage?: string | null;
  statusTone?: "error" | "success" | null;
  mode: EnvironmentMode;
  onModeChange: (mode: EnvironmentMode) => void;
  onSelectedEnvironmentChange: (name: string | null) => void;
  onReload: () => void;
  onCreateEnvironment: (input: {
    name: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  onUpdateEnvironment: (input: {
    name: string;
    new_name?: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  onCloneEnvironment: (input: {
    source_name: string;
    target_name: string;
    schema_prefix?: string;
    set_as_default?: boolean;
  }) => Promise<WorkspaceConfigResponse>;
  onDeleteEnvironment: (name: string) => Promise<WorkspaceConfigResponse>;
  showHeader?: boolean;
  showConfigPath?: boolean;
  showReload?: boolean;
  useCard?: boolean;
  onStateChange?: (state: { canSave: boolean }) => void;
};

export const WorkspaceEnvironmentForm = forwardRef<
  WorkspaceEnvironmentFormHandle,
  WorkspaceEnvironmentFormProps
>(function WorkspaceEnvironmentForm({
  configPath,
  defaultEnvironment,
  selectedEnvironment,
  environments,
  loading,
  busy,
  parseError,
  statusMessage,
  statusTone,
  mode,
  onModeChange,
  onSelectedEnvironmentChange,
  onReload,
  onCreateEnvironment,
  onUpdateEnvironment,
  onCloneEnvironment,
  onDeleteEnvironment,
  showHeader = true,
  showConfigPath = true,
  showReload = true,
  useCard = true,
  onStateChange,
}, ref) {
  const {
    activeEnvironment,
    environmentForm,
    setEnvironmentForm,
    handleSave,
  } = useWorkspaceEnvironmentForm({
    defaultEnvironment,
    environments,
    mode,
    onCloneEnvironment,
    onCreateEnvironment,
    onDeleteEnvironment,
    onModeChange,
    onSelectedEnvironmentChange,
    onUpdateEnvironment,
    selectedEnvironmentName: selectedEnvironment,
  });

  const title =
    mode === "create"
      ? "Create Environment"
      : mode === "clone"
        ? "Clone Environment"
        : activeEnvironment
          ? `Edit ${activeEnvironment.name}`
          : "Environment";
  const canSave = !busy && Boolean(environmentForm.name.trim());

  useImperativeHandle(
    ref,
    () => ({
      save: async () => {
        if (!canSave) {
          return;
        }

        await handleSave();
      },
      canSave,
    }),
    [canSave, handleSave]
  );

  useEffect(() => {
    onStateChange?.({ canSave });
  }, [canSave, onStateChange]);

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
      <WorkspaceEnvironmentFormFields
        environmentForm={environmentForm}
        environments={environments}
        mode={mode}
        onCloneSourceChange={(value) =>
          setEnvironmentForm((current) => {
            const sourceEnvironment = environments.find(
              (environment) => environment.name === value
            );

            return {
              ...current,
              cloneSourceName: value,
              schemaPrefix: sourceEnvironment?.schema_prefix ?? current.schemaPrefix,
            };
          })
        }
        onNameChange={(value) =>
          setEnvironmentForm((current) => ({
            ...current,
            name: value,
          }))
        }
        onSchemaPrefixChange={(value) =>
          setEnvironmentForm((current) => ({
            ...current,
            schemaPrefix: value,
          }))
        }
        onSetAsDefaultChange={(checked) =>
          setEnvironmentForm((current) => ({
            ...current,
            setAsDefault: checked,
          }))
        }
      />
    </WorkspaceSettingsFormShell>
  );
});
