"use client";

import { CSSProperties } from "react";

import { WorkspaceEnvironmentFormFields } from "@/components/workspace-environment-form-fields";
import { WorkspaceConfigPaneLayout } from "@/components/workspace-config-pane-layout";
import {
  EnvironmentMode,
  useWorkspaceEnvironmentForm,
} from "@/hooks/use-workspace-environment-form";
import {
  WorkspaceConfigEnvironment,
  WorkspaceConfigResponse,
} from "@/lib/types";

type WorkspaceEnvironmentPaneProps = {
  configPath: string;
  defaultEnvironment?: string;
  selectedEnvironment?: string | null;
  environments: WorkspaceConfigEnvironment[];
  loading: boolean;
  busy: boolean;
  parseError?: string;
  helpMode?: boolean;
  highlighted?: boolean;
  highlightStyle?: CSSProperties;
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
};

export function WorkspaceEnvironmentPane({
  configPath,
  defaultEnvironment,
  selectedEnvironment,
  environments,
  loading,
  busy,
  parseError,
  helpMode = false,
  highlighted = false,
  highlightStyle,
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
}: WorkspaceEnvironmentPaneProps) {
  const { activeEnvironment, environmentForm, setEnvironmentForm, handleDelete, handleSave } =
    useWorkspaceEnvironmentForm({
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

  return (
    <WorkspaceConfigPaneLayout
      title="Environment Editor"
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
      <WorkspaceEnvironmentFormFields
        activeEnvironmentExists={Boolean(activeEnvironment)}
        busy={busy}
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
        onDelete={() => void handleDelete()}
        onNameChange={(value) =>
          setEnvironmentForm((current) => ({
            ...current,
            name: value,
          }))
        }
        onSave={() => void handleSave()}
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
    </WorkspaceConfigPaneLayout>
  );
}
