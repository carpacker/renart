import { Link, Outlet } from "@tanstack/react-router";
import {
  Boxes,
  CheckCircle2,
  Cloud,
  Copy,
  CreditCard,
  LoaderCircle,
  Pencil,
  Plug,
  Plus,
  Shield,
  Sliders,
  Trash2,
  User,
  Users,
} from "lucide-react";
import { ComponentType, HTMLAttributes, ReactNode, useEffect, useMemo, useState } from "react";

import { WorkspaceConnectionFormFields } from "@/components/workspace-connection-form-fields";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DelimitedCard,
  DelimitedCardAction,
  DelimitedCardContent,
  DelimitedCardDescription,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import { Field, FieldContent, FieldDescription, FieldGroup, FieldTitle } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Switch } from "@/components/ui/switch";
import { useWorkspaceConnectionForm } from "@/hooks/use-workspace-connection-form";
import { useWorkspaceEnvironmentForm } from "@/hooks/use-workspace-environment-form";
import { useWorkspaceSettingsData } from "@/hooks/use-workspace-settings-data";
import { testWorkspaceConnection } from "@/lib/api";
import type { EnvironmentPolicy } from "@/lib/generated/api-types";
import { buildConnectionFieldDefaults } from "@/lib/settings-form-utils";
import type { WorkspaceConfigConnection, WorkspaceConfigEnvironment } from "@/lib/types";
import { cn } from "@/lib/utils";

import { IntegrationBadge, PageHeader, RedesignPage, SimpleTable } from "./redesign-primitives";

const emptyPolicy: EnvironmentPolicy = {
  protected: false,
  deployed_only: false,
  confirm_destructive: false,
};

function policiesEqual(left: EnvironmentPolicy, right: EnvironmentPolicy) {
  return (
    Boolean(left.protected) === Boolean(right.protected) &&
    Boolean(left.deployed_only) === Boolean(right.deployed_only) &&
    Boolean(left.confirm_destructive) === Boolean(right.confirm_destructive)
  );
}

const projectSections = [
  { id: "general", label: "General", icon: Sliders, to: "/redesign/project/general" },
  { id: "environments", label: "Environments", icon: Boxes, to: "/redesign/project/environments" },
  { id: "connections", label: "Connections", icon: Plug, to: "/redesign/project/connections" },
] as const;

const accountSections = [
  { id: "profile", label: "Account", icon: User, to: "/redesign/account/profile" },
  { id: "members", label: "Members", icon: Users, to: "/redesign/account/members" },
  { id: "workspaces", label: "Workspaces", icon: Cloud, to: "/redesign/account/workspaces" },
  { id: "billing", label: "Billing", icon: CreditCard, to: "/redesign/account/billing" },
] as const;

export function RedesignProjectSettingsShell() {
  const { workspaceConfig } = useWorkspaceSettingsData();
  const projectName = workspaceConfig?.project_name || "Project";

  return (
    <SettingsShell
      title="Project settings"
      subtitle={`${projectName} defaults, connections, and environments`}
      eyebrow={`Project · ${projectName}`}
      sections={projectSections}
    />
  );
}

export function RedesignAccountSettingsShell() {
  return (
    <SettingsShell
      title="Account"
      subtitle="Profile, members, workspaces, and billing"
      eyebrow="Account"
      sections={accountSections}
    />
  );
}

function SettingsShell({
  title,
  subtitle,
  eyebrow,
  sections,
}: {
  title: string;
  subtitle: string;
  eyebrow: string;
  sections: ReadonlyArray<{ id: string; label: string; icon: ComponentType<{ className?: string }>; to: string }>;
}) {
  return (
    <RedesignPage>
      <PageHeader title={title} subtitle={subtitle} />
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 md:grid-cols-[16rem_minmax(0,1fr)]">
        <aside className="hidden min-h-0 md:block">
          <div className="sticky top-0 flex flex-col gap-1">
            <div className="px-2 pb-2 text-xs font-medium text-muted-foreground">{eyebrow}</div>
            {sections.map((section) => (
              <SettingsSideLink key={section.id} section={section} />
            ))}
          </div>
        </aside>
        <div className="min-h-0 overflow-hidden">
          <ScrollArea className="mb-3 md:hidden" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
            <div className="flex gap-2 pb-1">
              {sections.map((section) => (
                <SettingsPillLink key={section.id} section={section} />
              ))}
            </div>
          </ScrollArea>
          <ScrollArea className="h-full min-h-0">
            <div className="mx-auto max-w-4xl">
              <Outlet />
            </div>
          </ScrollArea>
        </div>
      </div>
    </RedesignPage>
  );
}

function SettingsSideLink({ section }: { section: { label: string; icon: ComponentType<{ className?: string }>; to: string } }) {
  return (
    <Link
      to={section.to}
      className="flex h-9 items-center gap-2 rounded-md px-2.5 text-sm text-muted-foreground hover:bg-background hover:text-foreground"
      activeProps={{ className: "bg-background text-foreground shadow-sm font-medium" }}
    >
      <section.icon className="size-4" />
      {section.label}
    </Link>
  );
}

function SettingsPillLink({ section }: { section: { label: string; icon: ComponentType<{ className?: string }>; to: string } }) {
  return (
    <Link to={section.to} className="shrink-0" activeProps={{ className: "text-primary" }}>
      {({ isActive }) => (
        <Badge variant={isActive ? "default" : "outline"} className="h-8 px-3">
          <section.icon className="size-3.5" />
          {section.label}
        </Badge>
      )}
    </Link>
  );
}

export function RedesignProjectGeneralPage() {
  const {
    handleUpdateWorkspaceEnvironment,
    handleUpdateWorkspaceProject,
    loadWorkspaceConfig,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigBusy,
    workspaceConfigLoading,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
  } = useWorkspaceSettingsData();
  const [projectName, setProjectName] = useState("");
  const [defaultEnvironment, setDefaultEnvironment] = useState("");

  useEffect(() => {
    void loadWorkspaceConfig();
  }, [loadWorkspaceConfig]);

  useEffect(() => {
    setProjectName(workspaceConfig?.project_name || "");
  }, [workspaceConfig?.project_name]);

  useEffect(() => {
    setDefaultEnvironment(workspaceConfig?.default_environment || normalizedConfigEnvironments[0]?.name || "");
  }, [normalizedConfigEnvironments, workspaceConfig?.default_environment]);

  const selectedDefaultEnv = normalizedConfigEnvironments.find((environment) => environment.name === defaultEnvironment);
  const projectNameDirty = projectName.trim() !== (workspaceConfig?.project_name || "");

  return (
    <div className="flex flex-col gap-4">
      <SettingsStatus message={workspaceConfigStatusMessage} tone={workspaceConfigStatusTone} />
      <SettingsCard
        title="Project"
        action={
          <Button
            size="sm"
            disabled={workspaceConfigBusy || !projectName.trim() || !projectNameDirty}
            onClick={() => void handleUpdateWorkspaceProject({ name: projectName.trim() })}
          >
            Save name
          </Button>
        }
      >
        <PlainFieldGroup className="md:grid-cols-2">
          <PlainField>
            <Label>Project name</Label>
            <Input value={projectName} onChange={(event) => setProjectName(event.target.value)} placeholder="data_platform" />
          </PlainField>
          <ReadonlyField label="Project id" value={workspaceConfig?.project_id || "Assigned on first load"} mono />
          <ReadonlyField label="Workspace path" value={workspaceConfig?.workspace_path || "Loading..."} mono />
          <ReadonlyField label="Config file" value={workspaceConfig?.path || ".bruin.yml"} mono />
        </PlainFieldGroup>
      </SettingsCard>
      <SettingsCard
        title="Default environment"
        action={
          <Button
            size="sm"
            disabled={workspaceConfigBusy || workspaceConfigLoading || !selectedDefaultEnv}
            onClick={() => {
              if (!selectedDefaultEnv) return;
              void handleUpdateWorkspaceEnvironment({
                name: selectedDefaultEnv.name,
                schema_prefix: selectedDefaultEnv.schema_prefix,
                set_as_default: true,
              });
            }}
          >
            Save default
          </Button>
        }
      >
        <PlainFieldGroup>
          <PlainField>
            <Label>Environment</Label>
            <Select value={defaultEnvironment || undefined} onValueChange={setDefaultEnvironment}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Select environment" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {normalizedConfigEnvironments.map((environment) => (
                    <SelectItem key={environment.name} value={environment.name}>
                      {environment.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          </PlainField>
        </PlainFieldGroup>
      </SettingsCard>
    </div>
  );
}

type EnvironmentSheetState =
  | { mode: "create" }
  | { mode: "clone"; name: string }
  | { mode: "edit"; name: string };

export function RedesignProjectEnvironmentsPage() {
  const settings = useWorkspaceSettingsData();
  const {
    loadWorkspaceConfig,
    loadWorkspaceEnvironmentPolicy,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
    workspaceEnvironmentPolicies,
  } = settings;
  const [sheetState, setSheetState] = useState<EnvironmentSheetState | null>(null);

  useEffect(() => {
    void loadWorkspaceConfig();
  }, [loadWorkspaceConfig]);

  useEffect(() => {
    for (const environment of normalizedConfigEnvironments) {
      if (!workspaceEnvironmentPolicies[environment.name]) {
        void loadWorkspaceEnvironmentPolicy(environment.name);
      }
    }
  }, [loadWorkspaceEnvironmentPolicy, normalizedConfigEnvironments, workspaceEnvironmentPolicies]);

  return (
    <div className="flex min-h-0 flex-col gap-4">
      <SettingsStatus message={workspaceConfigStatusMessage} tone={workspaceConfigStatusTone} />
      <SettingsCard
        title="Environments"
        description="Each environment carries its own connections and guardrails."
        action={
          <Button size="sm" onClick={() => setSheetState({ mode: "create" })}>
            <Plus data-icon="inline-start" />
            New environment
          </Button>
        }
      >
        <div className="flex flex-col">
          {normalizedConfigEnvironments.length === 0 ? (
            <p className="text-sm text-muted-foreground">No environments are configured yet.</p>
          ) : (
            normalizedConfigEnvironments.map((environment) => (
              <EnvironmentRow
                key={environment.name}
                environment={environment}
                defaultEnvironment={workspaceConfig?.default_environment}
                policy={workspaceEnvironmentPolicies[environment.name]}
                onSelect={() => setSheetState({ mode: "edit", name: environment.name })}
              />
            ))
          )}
        </div>
      </SettingsCard>
      <EnvironmentSheet state={sheetState} onStateChange={setSheetState} settings={settings} />
    </div>
  );
}

function EnvironmentSheet({
  state,
  onStateChange,
  settings,
}: {
  state: EnvironmentSheetState | null;
  onStateChange: (state: EnvironmentSheetState | null) => void;
  settings: ReturnType<typeof useWorkspaceSettingsData>;
}) {
  const {
    handleCloneWorkspaceEnvironment,
    handleCreateWorkspaceEnvironment,
    handleDeleteWorkspaceEnvironment,
    handleUpdateWorkspaceEnvironment,
    handleUpdateWorkspaceEnvironmentPolicy,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigBusy,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
    workspaceEnvironmentPolicies,
  } = settings;
  const mode = state?.mode ?? "edit";
  const selectedEnvironmentName = state?.mode === "create" ? null : state?.name ?? null;

  const { activeEnvironment, environmentForm, handleDelete, handleSave, setEnvironmentForm } =
    useWorkspaceEnvironmentForm({
      defaultEnvironment: workspaceConfig?.default_environment,
      environments: normalizedConfigEnvironments,
      mode,
      onCloneEnvironment: handleCloneWorkspaceEnvironment,
      onCreateEnvironment: handleCreateWorkspaceEnvironment,
      onDeleteEnvironment: handleDeleteWorkspaceEnvironment,
      onModeChange: () => {},
      onSelectedEnvironmentChange: () => {},
      onUpdateEnvironment: handleUpdateWorkspaceEnvironment,
      selectedEnvironmentName,
    });

  const editName = state?.mode === "edit" ? state.name : null;
  const storedPolicy = (editName ? workspaceEnvironmentPolicies[editName] : null) ?? emptyPolicy;
  const [policyDraft, setPolicyDraft] = useState<EnvironmentPolicy>(emptyPolicy);

  useEffect(() => {
    setPolicyDraft(storedPolicy);
  }, [editName, storedPolicy]);

  const close = () => onStateChange(null);

  const save = async () => {
    if (!state) return;
    try {
      await handleSave();
      if (state.mode === "edit") {
        const nextName = environmentForm.name.trim() || state.name;
        if (!policiesEqual(policyDraft, storedPolicy)) {
          await handleUpdateWorkspaceEnvironmentPolicy(nextName, policyDraft);
        }
      }
      close();
    } catch {
      // Keep the sheet open; the error alert below shows what failed.
    }
  };

  const remove = async () => {
    try {
      await handleDelete();
      close();
    } catch {
      // Keep the sheet open; the error alert below shows what failed.
    }
  };

  const title =
    mode === "create"
      ? "New environment"
      : mode === "clone"
        ? `Clone ${environmentForm.cloneSourceName || "environment"}`
        : activeEnvironment
          ? activeEnvironment.name
          : "Environment";
  const description =
    mode === "create"
      ? "Add an environment to this project."
      : mode === "clone"
        ? "Copy an environment including its connections and guardrails."
        : "Rename, set defaults, and adjust guardrails.";

  return (
    <Sheet open={state !== null} onOpenChange={(open) => !open && close()}>
      <SheetContent className="w-full sm:max-w-lg">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <Boxes className="size-4 text-primary" />
            {title}
          </SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-auto px-4">
          <PlainFieldGroup>
            {workspaceConfigStatusTone === "error" ? (
              <SettingsStatus message={workspaceConfigStatusMessage} tone={workspaceConfigStatusTone} />
            ) : null}
            {mode === "clone" ? (
              <PlainField>
                <Label>Source</Label>
                <Select
                  value={environmentForm.cloneSourceName || undefined}
                  onValueChange={(value) => {
                    const source = normalizedConfigEnvironments.find((environment) => environment.name === value);
                    setEnvironmentForm((current) => ({
                      ...current,
                      cloneSourceName: value,
                      schemaPrefix: source?.schema_prefix ?? current.schemaPrefix,
                    }));
                  }}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue placeholder="Select source" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {normalizedConfigEnvironments.map((environment) => (
                        <SelectItem key={environment.name} value={environment.name}>
                          {environment.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </PlainField>
            ) : null}
            <PlainField>
              <Label>Name</Label>
              <Input
                value={environmentForm.name}
                onChange={(event) => setEnvironmentForm((current) => ({ ...current, name: event.target.value }))}
                placeholder="prod"
              />
            </PlainField>
            <PlainField>
              <Label>Schema prefix</Label>
              <Input
                value={environmentForm.schemaPrefix}
                onChange={(event) => setEnvironmentForm((current) => ({ ...current, schemaPrefix: event.target.value }))}
                placeholder="analytics_"
              />
            </PlainField>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldTitle>Default environment</FieldTitle>
                <FieldDescription>Use this environment when no explicit environment is selected.</FieldDescription>
              </FieldContent>
              <Switch
                checked={environmentForm.setAsDefault}
                onCheckedChange={(checked) => setEnvironmentForm((current) => ({ ...current, setAsDefault: checked }))}
              />
            </Field>
            {mode === "edit" ? (
              <div className="grid gap-3 border-t pt-4">
                <div>
                  <h3 className="text-sm font-medium">Execution policy</h3>
                  <p className="text-sm text-muted-foreground">
                    Renart-only guardrails stored in .renart/environments.yml, applied on save.
                  </p>
                </div>
                <EnvironmentPolicyFields policy={policyDraft} disabled={workspaceConfigBusy} onChange={setPolicyDraft} />
              </div>
            ) : null}
            {mode === "edit" && activeEnvironment ? (
              <div className="grid gap-2 border-t pt-4">
                <h3 className="text-sm font-medium">Connections</h3>
                {activeEnvironment.connections.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No connections in this environment.</p>
                ) : (
                  <div className="flex flex-wrap gap-2">
                    {activeEnvironment.connections.map((connection) => (
                      <Badge key={connection.name} variant="outline" className="gap-1.5 font-mono">
                        {connection.name}
                        <span className="font-sans text-muted-foreground">{connection.type}</span>
                      </Badge>
                    ))}
                  </div>
                )}
                <p className="text-xs text-muted-foreground">
                  Manage them in the{" "}
                  <Link to="/redesign/project/connections" className="underline underline-offset-2">
                    Connections
                  </Link>{" "}
                  tab.
                </p>
              </div>
            ) : null}
          </PlainFieldGroup>
        </div>
        <SheetFooter>
          <div className="flex w-full items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              {mode === "edit" && activeEnvironment ? (
                <>
                  <ConfirmDeleteButton
                    disabled={workspaceConfigBusy}
                    label="Delete"
                    onConfirm={() => void remove()}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={workspaceConfigBusy}
                    onClick={() => onStateChange({ mode: "clone", name: activeEnvironment.name })}
                  >
                    <Copy data-icon="inline-start" />
                    Clone
                  </Button>
                </>
              ) : null}
            </div>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" disabled={workspaceConfigBusy} onClick={close}>
                Cancel
              </Button>
              <Button size="sm" disabled={workspaceConfigBusy || !environmentForm.name.trim()} onClick={() => void save()}>
                {mode === "create" ? "Create environment" : mode === "clone" ? "Clone environment" : "Save changes"}
              </Button>
            </div>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

type ConnectionSheetState =
  | { mode: "create"; environment: string | null }
  | { mode: "edit"; environment: string; connection: string };

export function RedesignProjectConnectionsPage() {
  const settings = useWorkspaceSettingsData();
  const {
    loadWorkspaceConfig,
    loadWorkspaceEnvironmentPolicy,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
    workspaceEnvironmentPolicies,
  } = settings;
  const [sheetState, setSheetState] = useState<ConnectionSheetState | null>(null);

  useEffect(() => {
    void loadWorkspaceConfig();
  }, [loadWorkspaceConfig]);

  useEffect(() => {
    for (const environment of normalizedConfigEnvironments) {
      if (!workspaceEnvironmentPolicies[environment.name]) {
        void loadWorkspaceEnvironmentPolicy(environment.name);
      }
    }
  }, [loadWorkspaceEnvironmentPolicy, normalizedConfigEnvironments, workspaceEnvironmentPolicies]);

  return (
    <div className="flex min-h-0 flex-col gap-4">
      <SettingsStatus message={workspaceConfigStatusMessage} tone={workspaceConfigStatusTone} />
      {normalizedConfigEnvironments.length === 0 ? (
        <SettingsCard title="Connections">
          <p className="text-sm text-muted-foreground">
            Create an environment first; connections always belong to one.
          </p>
        </SettingsCard>
      ) : (
        normalizedConfigEnvironments.map((environment) => (
          <SettingsCard
            key={environment.name}
            title={
              <span className="flex items-center gap-2">
                <span className="font-mono">{environment.name}</span>
                {environment.name === workspaceConfig?.default_environment ? (
                  <Badge variant="secondary">Default</Badge>
                ) : null}
                {workspaceEnvironmentPolicies[environment.name]?.protected ? (
                  <Badge variant="outline" className="text-destructive">Protected</Badge>
                ) : null}
              </span>
            }
            description={`${environment.connections.length} connection${environment.connections.length === 1 ? "" : "s"}`}
            action={
              <Button
                size="sm"
                variant="outline"
                onClick={() => setSheetState({ mode: "create", environment: environment.name })}
              >
                <Plus data-icon="inline-start" />
                Add
              </Button>
            }
          >
            {environment.connections.length === 0 ? (
              <p className="text-sm text-muted-foreground">No connections in this environment yet.</p>
            ) : (
              <div className="flex flex-col">
                {environment.connections.map((connection) => (
                  <ConnectionRow
                    key={connection.name}
                    connection={connection}
                    onSelect={() =>
                      setSheetState({ mode: "edit", environment: environment.name, connection: connection.name })
                    }
                  />
                ))}
              </div>
            )}
          </SettingsCard>
        ))
      )}
      <ConnectionSheet state={sheetState} onClose={() => setSheetState(null)} settings={settings} />
    </div>
  );
}

function ConnectionRow({
  connection,
  onSelect,
}: {
  connection: WorkspaceConfigConnection;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      className="group flex items-center gap-3 border-b px-3 py-2.5 text-left last:border-b-0 hover:bg-muted/50"
      onClick={onSelect}
    >
      <span className="min-w-0 flex-1 truncate font-mono text-sm font-medium">{connection.name}</span>
      <IntegrationBadge name={connection.type} />
      {connection.sling_category ? <Badge variant="secondary">{connection.sling_category}</Badge> : null}
      <Pencil className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
    </button>
  );
}

function ConnectionSheet({
  state,
  onClose,
  settings,
}: {
  state: ConnectionSheetState | null;
  onClose: () => void;
  settings: ReturnType<typeof useWorkspaceSettingsData>;
}) {
  const {
    handleCreateWorkspaceConnection,
    handleDeleteWorkspaceConnection,
    handleUpdateWorkspaceConnection,
    normalizedConfigEnvironments,
    workspaceConfig,
    workspaceConfigBusy,
    workspaceConfigStatusMessage,
    workspaceConfigStatusTone,
  } = settings;
  const mode = state?.mode ?? "edit";
  // Stable identity matters: this array is an effect dependency inside
  // useWorkspaceConnectionForm, and a fresh [] per render loops the effect.
  const connectionTypes = useMemo(
    () => workspaceConfig?.connection_types ?? [],
    [workspaceConfig?.connection_types]
  );
  const [validateBusy, setValidateBusy] = useState(false);
  const [validateMessage, setValidateMessage] = useState<string | null>(null);
  const [validateTone, setValidateTone] = useState<"error" | "success" | null>(null);

  useEffect(() => {
    setValidateMessage(null);
    setValidateTone(null);
  }, [state]);

  const form = useWorkspaceConnectionForm({
    connectionTypes: connectionTypes,
    defaultEnvironment: workspaceConfig?.default_environment,
    environments: normalizedConfigEnvironments,
    mode,
    onCreateConnection: handleCreateWorkspaceConnection,
    onDeleteConnection: handleDeleteWorkspaceConnection,
    onModeChange: () => {},
    onSelectedConnectionChange: () => {},
    onSelectedEnvironmentChange: () => {},
    onUpdateConnection: handleUpdateWorkspaceConnection,
    selectedConnectionName: state?.mode === "edit" ? state.connection : null,
    selectedEnvironmentName: state?.environment ?? null,
  });

  const validateConnection = async () => {
    setValidateBusy(true);
    setValidateMessage(null);
    setValidateTone(null);
    try {
      const response = await testWorkspaceConnection({
        environment_name: form.connectionForm.environmentName,
        current_name: form.activeConnection?.name,
        name: form.connectionForm.name,
        type: form.connectionForm.type,
        values: form.connectionForm.values,
      });
      setValidateMessage(response.message ?? "Connection validated.");
      setValidateTone("success");
    } catch (error) {
      setValidateMessage(error instanceof Error ? error.message : "Connection validation failed.");
      setValidateTone("error");
    } finally {
      setValidateBusy(false);
    }
  };

  const save = async () => {
    try {
      await form.handleSave();
      onClose();
    } catch {
      // Keep the sheet open; the error alert below shows what failed.
    }
  };

  const remove = async () => {
    try {
      await form.handleDelete();
      onClose();
    } catch {
      // Keep the sheet open; the error alert below shows what failed.
    }
  };

  return (
    <Sheet open={state !== null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full sm:max-w-xl">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <Plug className="size-4 text-primary" />
            {mode === "create" ? "New connection" : form.activeConnection?.name ?? "Connection"}
          </SheetTitle>
          <SheetDescription>
            {mode === "create"
              ? "Credentials are stored in the project config, scoped to one environment."
              : `Stored in the project config under the ${state?.environment ?? ""} environment.`}
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-auto px-4">
          <div className="grid gap-4">
            {workspaceConfigStatusTone === "error" ? (
              <SettingsStatus message={workspaceConfigStatusMessage} tone={workspaceConfigStatusTone} />
            ) : null}
            <WorkspaceConnectionFormFields
              busy={workspaceConfigBusy}
              canValidate={Boolean(form.connectionForm.environmentName && form.connectionForm.name.trim() && form.connectionForm.type)}
              connectionForm={form.connectionForm}
              connectionTypes={connectionTypes}
              environments={normalizedConfigEnvironments}
              mode={mode}
              selectedConnectionType={form.selectedConnectionType}
              selectedEnvironment={state?.environment ?? null}
              environmentDisabled={mode === "edit"}
              validateBusy={validateBusy}
              validateMessage={validateMessage}
              validateTone={validateTone}
              showActions={false}
              onEnvironmentChange={(value) => form.setConnectionForm((current) => ({ ...current, environmentName: value }))}
              onFieldValueChange={(fieldName, value) => form.setConnectionForm((current) => ({ ...current, values: { ...current.values, [fieldName]: value } }))}
              onNameChange={(value) => form.setConnectionForm((current) => ({ ...current, name: value }))}
              onSave={() => void save()}
              onTypeChange={(value) =>
                form.setConnectionForm((current) => ({
                  ...current,
                  type: value,
                  values: buildConnectionFieldDefaults({
                    connectionTypes: connectionTypes,
                    existingConnection: null,
                    previousValues: current.values,
                    typeName: value,
                  }),
                }))
              }
              onValidate={() => void validateConnection()}
            />
          </div>
        </div>
        <SheetFooter>
          <div className="flex w-full items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              {mode === "edit" && form.activeConnection ? (
                <ConfirmDeleteButton
                  disabled={workspaceConfigBusy}
                  label="Delete"
                  onConfirm={() => void remove()}
                />
              ) : null}
            </div>
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={workspaceConfigBusy || validateBusy || !form.connectionForm.name.trim()}
                onClick={() => void validateConnection()}
              >
                {validateBusy ? <LoaderCircle data-icon="inline-start" className="animate-spin" /> : <CheckCircle2 data-icon="inline-start" />}
                Verify
              </Button>
              <Button
                size="sm"
                disabled={workspaceConfigBusy || !form.connectionForm.environmentName || !form.connectionForm.name.trim() || !form.connectionForm.type}
                onClick={() => void save()}
              >
                {mode === "create" ? "Create connection" : "Save changes"}
              </Button>
            </div>
          </div>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}

export function RedesignAccountProfilePage() {
  return (
    <SettingsCard title="Profile">
      <div className="grid gap-4 md:grid-cols-2">
        <MockField label="Name" value="Jane Doe" />
        <MockField label="Email" value="jane@acme.io" />
      </div>
    </SettingsCard>
  );
}

export function RedesignAccountMembersPage() {
  return (
    <SettingsCard title="Members & permissions" action={<Button size="sm"><Plus data-icon="inline-start" />Invite</Button>}>
      <SimpleTable columns={["Member", "Email", "Role"]} rows={[["Jane Doe", "jane@acme.io", "Owner"], ["Lukas R.", "lukas@acme.io", "Editor"], ["Sam Lee", "sam@acme.io", "Viewer"], ["CI Bot", "ci@acme.io", "Editor"]]} />
      <p className="mt-3 flex items-center gap-2 text-xs text-muted-foreground"><Shield className="size-3.5" />Owner manages billing and members, Editor builds pipelines, Viewer is read-only.</p>
    </SettingsCard>
  );
}

export function RedesignAccountWorkspacesPage() {
  return (
    <div className="flex flex-col gap-4">
      {["data_platform", "marketing_analytics"].map((workspace, index) => (
        <SettingsCard key={workspace} title={workspace} action={index === 0 ? <Badge variant="secondary">Connected</Badge> : <Button size="sm">Connect</Button>}>
          <p className="text-sm text-muted-foreground">{index === 0 ? "Cloud workspace · branch main · synced 2m ago" : "Local workspace · not connected"}</p>
        </SettingsCard>
      ))}
    </div>
  );
}

export function RedesignAccountBillingPage() {
  return (
    <SettingsCard title="Billing" action={<Button variant="outline" size="sm">Manage plan</Button>}>
      <div className="grid gap-4 md:grid-cols-3">
        {[
          ["Seats used", "4 / 5"],
          ["Pipeline runs", "1,284 / mo"],
          ["Cloud compute", "38 hrs"],
        ].map(([label, value]) => (
          <div key={label} className="grid gap-1">
            <div className="text-sm text-muted-foreground">{label}</div>
            <div className="text-lg font-semibold">{value}</div>
          </div>
        ))}
      </div>
    </SettingsCard>
  );
}

function SettingsCard({
  title,
  description,
  action,
  children,
}: {
  title: ReactNode;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <DelimitedCard>
      <DelimitedCardHeader>
        <div className="min-w-0">
          <DelimitedCardTitle>{title}</DelimitedCardTitle>
          {description ? <DelimitedCardDescription>{description}</DelimitedCardDescription> : null}
        </div>
        {action ? <DelimitedCardAction>{action}</DelimitedCardAction> : null}
      </DelimitedCardHeader>
      <DelimitedCardContent>{children}</DelimitedCardContent>
    </DelimitedCard>
  );
}

function SettingsStatus({ message, tone }: { message?: string | null; tone?: "error" | "success" | null }) {
  if (!message || !tone) return null;
  return (
    <Alert variant={tone === "error" ? "destructive" : "default"}>
      <AlertTitle>{tone === "error" ? "Settings update failed" : "Settings saved"}</AlertTitle>
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}

function ConfirmDeleteButton({
  disabled,
  label,
  onConfirm,
}: {
  disabled: boolean;
  label: string;
  onConfirm: () => void;
}) {
  const [armed, setArmed] = useState(false);
  return (
    <Button
      size="sm"
      variant={armed ? "destructive" : "outline"}
      disabled={disabled}
      onBlur={() => setArmed(false)}
      onClick={() => {
        if (armed) {
          setArmed(false);
          onConfirm();
          return;
        }
        setArmed(true);
      }}
    >
      <Trash2 data-icon="inline-start" />
      {armed ? "Confirm delete" : label}
    </Button>
  );
}

function ReadonlyField({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <PlainField>
      <Label>{label}</Label>
      <Input value={value} readOnly className={mono ? "font-mono" : undefined} />
    </PlainField>
  );
}

function MockField({ label, value }: { label: string; value: string }) {
  return (
    <PlainField>
      <Label>{label}</Label>
      <Input defaultValue={value} />
    </PlainField>
  );
}

function PlainFieldGroup({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("grid gap-4", className)} {...props} />;
}

function PlainField({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("grid gap-2", className)} {...props} />;
}

function EnvironmentRow({
  environment,
  defaultEnvironment,
  policy,
  onSelect,
}: {
  environment: WorkspaceConfigEnvironment;
  defaultEnvironment?: string;
  policy?: EnvironmentPolicy;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      className="group flex items-center gap-3 border-b px-3 py-3 text-left last:border-b-0 hover:bg-muted/50"
      onClick={onSelect}
    >
      <Boxes className="size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate font-mono font-medium">{environment.name}</span>
          {environment.name === defaultEnvironment ? <Badge variant="secondary">Default</Badge> : null}
          {policy?.protected ? <Badge variant="outline" className="text-destructive">Protected</Badge> : null}
        </div>
        <div className="mt-1 truncate text-xs text-muted-foreground">{environment.schema_prefix || "No schema prefix"}</div>
      </div>
      <span className="shrink-0 text-xs text-muted-foreground">
        {environment.connections.length} connection{environment.connections.length === 1 ? "" : "s"}
      </span>
      <Pencil className="size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100" />
    </button>
  );
}

function EnvironmentPolicyFields({
  policy,
  disabled,
  onChange,
}: {
  policy: EnvironmentPolicy;
  disabled: boolean;
  onChange: (policy: EnvironmentPolicy) => void;
}) {
  return (
    <FieldGroup>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>Protected</FieldTitle>
          <FieldDescription>Disable interactive execution for this environment.</FieldDescription>
        </FieldContent>
        <Switch disabled={disabled} checked={policy.protected} onCheckedChange={(checked) => onChange({ ...policy, protected: checked })} />
      </Field>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>Deployed only</FieldTitle>
          <FieldDescription>Only run deployed snapshots for this environment.</FieldDescription>
        </FieldContent>
        <Switch disabled={disabled} checked={policy.deployed_only} onCheckedChange={(checked) => onChange({ ...policy, deployed_only: checked })} />
      </Field>
      <Field orientation="horizontal">
        <FieldContent>
          <FieldTitle>Confirm destructive operations</FieldTitle>
          <FieldDescription>Require typing the environment name before destructive runs.</FieldDescription>
        </FieldContent>
        <Switch disabled={disabled} checked={policy.confirm_destructive} onCheckedChange={(checked) => onChange({ ...policy, confirm_destructive: checked })} />
      </Field>
    </FieldGroup>
  );
}
