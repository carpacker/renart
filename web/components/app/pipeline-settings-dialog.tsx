import { Link } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import {
  AlertTriangle,
  Braces,
  CalendarClock,
  Database,
  ExternalLink,
  Loader2,
  Package,
  Plus,
  Settings2,
  Trash2,
} from "lucide-react";
import { useCallback, useEffect, useId, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  getPipelineConfig,
  getPipelinePythonDependencies,
  updatePipelineConfig,
  updatePipelinePythonDependencies,
} from "@/lib/api-pipelines";
import { selectedEnvironmentAtom } from "@/lib/atoms/domains/workspace";
import type {
  PipelineConfigConnection,
  PipelineConfigVariable,
  UpdatePipelineConfigRequest,
} from "@/lib/types";
import { cn } from "@/lib/utils";

import { MultiValueInput } from "./multi-value-input";

const pipelineSettingsSections = [
  { id: "general", label: "General", icon: Settings2 },
  { id: "schedule", label: "Schedule", icon: CalendarClock },
  { id: "connections", label: "Connections", icon: Database },
  { id: "python", label: "Python", icon: Package },
  { id: "variables", label: "Variables", icon: Braces },
] as const;

export type PipelineSettingsSection = (typeof pipelineSettingsSections)[number]["id"];

type PipelineConfigDraft = UpdatePipelineConfigRequest;

// Pipeline settings live in pipeline.yml, while Python dependencies live in the
// pipeline-root pyproject.toml. Both write through Go endpoints and SSE then
// reconciles the workspace.
export function PipelineSettingsDialog({
  open,
  onOpenChange,
  pipelineId,
  initialSection,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  pipelineId: string;
  initialSection?: PipelineSettingsSection;
}) {
  const [section, setSection] = useState<PipelineSettingsSection>(initialSection ?? "general");
  const [draft, setDraft] = useState<PipelineConfigDraft | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [inferredDefaultConnections, setInferredDefaultConnections] = useState<
    PipelineConfigConnection[]
  >([]);
  const [pythonDependencies, setPythonDependencies] = useState<string[]>([]);
  const [initialPythonDependencies, setInitialPythonDependencies] = useState<string[]>([]);
  const [pythonDependencyPath, setPythonDependencyPath] = useState("");

  // Re-fetch whenever the dialog opens so the form always reflects on-disk state
  // (a code edit or CLI run may have changed pipeline.yml since last time).
  useEffect(() => {
    if (!open) return;
    setSection(initialSection ?? "general");
    setError(null);
    setLoading(true);
    setDraft(null);
    setInferredDefaultConnections([]);
    setPythonDependencies([]);
    setInitialPythonDependencies([]);
    setPythonDependencyPath("");
    let cancelled = false;
    Promise.allSettled([getPipelineConfig(pipelineId), getPipelinePythonDependencies(pipelineId)])
      .then(([configResult, pythonResult]) => {
        if (cancelled) return;
        const messages: string[] = [];
        if (configResult.status === "fulfilled") {
          const config = configResult.value;
          setDraft(configResponseToDraft(config));
          setInferredDefaultConnections(config.inferred_default_connections ?? []);
        } else {
          messages.push(errorMessage(configResult.reason, "Failed to load pipeline settings."));
        }
        if (pythonResult.status === "fulfilled") {
          const python = pythonResult.value;
          setPythonDependencies(python.dependencies ?? []);
          setInitialPythonDependencies(python.dependencies ?? []);
          setPythonDependencyPath(python.path);
        } else {
          messages.push(errorMessage(pythonResult.reason, "Failed to load Python dependencies."));
        }
        setError(messages.length > 0 ? messages.join(" ") : null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, pipelineId, initialSection]);

  const update = useCallback(
    <K extends keyof PipelineConfigDraft>(key: K, value: PipelineConfigDraft[K]) => {
      setDraft((current) => (current ? { ...current, [key]: value } : current));
    },
    [],
  );

  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setError(null);
    try {
      const dependenciesChanged = !sameStringArray(pythonDependencies, initialPythonDependencies);
      const [response, dependencyResponse] = await Promise.all([
        updatePipelineConfig(pipelineId, draft),
        dependenciesChanged
          ? updatePipelinePythonDependencies(pipelineId, {
              dependencies: pythonDependencies,
            })
          : Promise.resolve(null),
      ]);
      setDraft(configResponseToDraft(response));
      setInferredDefaultConnections(response.inferred_default_connections ?? []);
      if (dependencyResponse) {
        setPythonDependencies(dependencyResponse.dependencies);
        setInitialPythonDependencies(dependencyResponse.dependencies);
        setPythonDependencyPath(dependencyResponse.path);
      }
      onOpenChange(false);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Failed to save pipeline settings.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(42rem,90dvh)] min-h-0 flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader className="shrink-0">
          <DialogTitle>
            Pipeline settings{" "}
            <span className="font-mono text-xs text-muted-foreground">
              · {draft?.name || pipelineId}
            </span>
          </DialogTitle>
          <DialogDescription>
            Edit version-controlled pipeline configuration and runtime dependencies.
          </DialogDescription>
        </DialogHeader>
        <Tabs
          value={section}
          onValueChange={(value) => setSection(value as PipelineSettingsSection)}
          orientation="vertical"
          className="grid min-h-0 flex-1 grid-rows-[auto_minmax(0,1fr)] gap-4 md:grid-cols-[10.5rem_minmax(0,1fr)] md:grid-rows-1"
        >
          <div className="flex gap-2 overflow-x-auto md:hidden">
            {pipelineSettingsSections.map((item) => (
              <Button
                key={item.id}
                type="button"
                variant={section === item.id ? "secondary" : "ghost"}
                className="shrink-0 justify-start"
                onClick={() => setSection(item.id)}
              >
                {item.label}
              </Button>
            ))}
          </div>
          <TabsList
            aria-label="Pipeline settings sections"
            className="hidden h-full min-h-0 w-full self-stretch items-stretch justify-start border bg-muted/30 p-1 group-data-vertical/tabs:h-full md:flex"
          >
            {pipelineSettingsSections.map(({ id, label, icon: Icon }) => (
              <TabsTrigger key={id} value={id} className="h-8 flex-none justify-start px-2">
                <Icon data-icon="inline-start" />
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
          <ScrollArea
            className="h-full min-h-0 rounded-lg border"
            data-testid="pipeline-settings-content"
          >
            <div className="flex flex-col gap-4 p-4">
              {loading ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  Loading settings…
                </div>
              ) : !draft ? (
                <p className="text-sm text-muted-foreground">Pipeline settings are unavailable.</p>
              ) : (
                pipelineSettingsSections.map((item) => (
                  <TabsContent key={item.id} value={item.id} className="m-0">
                    <PipelineSettingsSectionBody
                      section={item.id}
                      draft={draft}
                      update={update}
                      inferredDefaultConnections={inferredDefaultConnections}
                      pythonDependencies={pythonDependencies}
                      onPythonDependenciesChange={setPythonDependencies}
                      pythonDependencyPath={pythonDependencyPath}
                    />
                  </TabsContent>
                ))
              )}
            </div>
          </ScrollArea>
        </Tabs>
        {error ? (
          <Alert variant="destructive" className="shrink-0">
            <AlertTriangle />
            <AlertTitle>Could not load or save pipeline settings</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter className="shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={() => void save()} disabled={saving || loading || !draft}>
            {saving ? (
              <>
                <Loader2 className="size-4 animate-spin" />
                Saving…
              </>
            ) : (
              "Save changes"
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function configResponseToDraft(config: {
  name?: string;
  schedule?: string;
  start_date?: string;
  owner?: string;
  tags?: string[];
  domains?: string[];
  default_connections?: PipelineConfigConnection[];
  inferred_default_connections?: PipelineConfigConnection[];
  catchup?: boolean;
  metadata_push_bigquery?: boolean;
  retries?: number;
  concurrency?: number;
  max_active_steps?: number;
  notifications_slack?: PipelineConfigDraft["notifications_slack"];
  notifications_teams?: PipelineConfigDraft["notifications_teams"];
  defaults?: PipelineConfigDraft["defaults"];
  variables?: PipelineConfigVariable[];
}): PipelineConfigDraft {
  const notification = (value?: PipelineConfigDraft["notifications_slack"]) => ({
    enabled: value?.enabled ?? false,
    channel: value?.channel ?? "",
    connection: value?.connection ?? "",
    success: value?.success ?? false,
    failure: value?.failure ?? true,
  });
  return {
    name: config.name ?? "",
    schedule: config.schedule ?? "",
    start_date: config.start_date ?? "",
    owner: config.owner ?? "",
    tags: config.tags ?? [],
    domains: config.domains ?? [],
    default_connections: config.default_connections ?? [],
    catchup: config.catchup ?? false,
    metadata_push_bigquery: config.metadata_push_bigquery ?? false,
    retries: config.retries ?? 0,
    concurrency: config.concurrency ?? 0,
    max_active_steps: config.max_active_steps,
    notifications_slack: notification(config.notifications_slack),
    notifications_teams: notification(config.notifications_teams),
    defaults: config.defaults ?? {},
    variables: config.variables ?? [],
  };
}

function PipelineSettingsSectionBody({
  section,
  draft,
  update,
  inferredDefaultConnections,
  pythonDependencies,
  onPythonDependenciesChange,
  pythonDependencyPath,
}: {
  section: PipelineSettingsSection;
  draft: PipelineConfigDraft;
  update: <K extends keyof PipelineConfigDraft>(key: K, value: PipelineConfigDraft[K]) => void;
  inferredDefaultConnections: PipelineConfigConnection[];
  pythonDependencies: string[];
  onPythonDependenciesChange: (value: string[]) => void;
  pythonDependencyPath: string;
}) {
  const environment = useAtomValue(selectedEnvironmentAtom);
  if (section === "general") {
    return (
      <>
        <SettingsTextField
          label="Pipeline name"
          value={draft.name}
          onChange={(value) => update("name", value)}
          placeholder="my_pipeline"
        />
        <SettingsTextField
          label="Owner"
          value={draft.owner}
          onChange={(value) => update("owner", value)}
          placeholder="team@acme.io"
        />
        <SettingsMultiValueField
          label="Tags"
          value={draft.tags}
          onChange={(value) => update("tags", value)}
          placeholder="Add tag"
        />
        <SettingsMultiValueField
          label="Domains"
          value={draft.domains}
          onChange={(value) => update("domains", value)}
          placeholder="Add domain"
        />
        <FieldGroup className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <SettingsNumberField
            label="Retries"
            value={draft.retries}
            onChange={(value) => update("retries", value ?? 0)}
            min={0}
          />
          <SettingsNumberField
            label="Overlapping pipeline runs"
            value={draft.concurrency}
            onChange={(value) => update("concurrency", value ?? 0)}
            hint="Limits how many runs of this pipeline may overlap. This is separate from parallel assets inside one run."
            min={0}
          />
          <SettingsNumberField
            className="sm:col-span-2"
            label="Maximum active steps"
            value={draft.max_active_steps}
            onChange={(value) => update("max_active_steps", value)}
            hint="Leave blank to run one asset at a time. Values above 1 let independent assets overlap; dependencies, connections, and shared targets can still serialize them."
            min={1}
            placeholder="1"
          />
        </FieldGroup>
        <SettingsToggleField
          label="Push metadata to BigQuery"
          description="Sync asset metadata to BigQuery after each run."
          checked={draft.metadata_push_bigquery}
          onChange={(value) => update("metadata_push_bigquery", value)}
        />
      </>
    );
  }
  if (section === "schedule") {
    return (
      <>
        <SettingsTextField
          label="Schedule"
          value={draft.schedule}
          onChange={(value) => update("schedule", value)}
          placeholder="@daily"
          hint="A cron expression or preset like @daily / @hourly."
        />
        <SettingsTextField
          label="Start date"
          value={draft.start_date}
          onChange={(value) => update("start_date", value)}
          placeholder="2024-01-01"
        />
        <SettingsToggleField
          label="Catchup"
          description="Backfill every schedule interval missed since the start date."
          checked={draft.catchup}
          onChange={(value) => update("catchup", value)}
        />
        <div className="grid gap-3 border-t pt-4">
          <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            Interval defaults
          </div>
          <SettingsNumberField
            label="Rerun cooldown (seconds)"
            value={draft.defaults.rerun_cooldown}
            onChange={(value) => update("defaults", { ...draft.defaults, rerun_cooldown: value })}
          />
          <div className="grid grid-cols-2 gap-3">
            <SettingsTextField
              label="Start offset"
              value={draft.defaults.start_offset_raw ?? ""}
              onChange={(value) =>
                update("defaults", { ...draft.defaults, start_offset_raw: value || undefined })
              }
              placeholder="-1d"
            />
            <SettingsTextField
              label="End offset"
              value={draft.defaults.end_offset_raw ?? ""}
              onChange={(value) =>
                update("defaults", { ...draft.defaults, end_offset_raw: value || undefined })
              }
              placeholder="0d"
            />
          </div>
        </div>
      </>
    );
  }
  if (section === "connections") {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          Default connection per platform. Assets that don&apos;t name a connection use these.
        </p>
        <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
          Pipeline overrides
        </div>
        {draft.default_connections.length === 0 ? (
          <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">
            No overrides in pipeline.yml.
          </p>
        ) : (
          draft.default_connections.map((connection, index) => (
            <div key={index} className="flex items-end gap-2">
              <SettingsTextField
                className="flex-1"
                label={index === 0 ? "Platform" : undefined}
                value={connection.platform}
                onChange={(value) =>
                  update(
                    "default_connections",
                    replaceAt(draft.default_connections, index, { ...connection, platform: value }),
                  )
                }
                placeholder="gcp"
              />
              <SettingsTextField
                className="flex-1"
                label={index === 0 ? "Connection" : undefined}
                value={connection.name}
                onChange={(value) =>
                  update(
                    "default_connections",
                    replaceAt(draft.default_connections, index, { ...connection, name: value }),
                  )
                }
                placeholder="bq-prod"
              />
              <PipelineConnectionSettingsLink
                environment={environment}
                connection={connection.name}
              />
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove connection"
                onClick={() =>
                  update("default_connections", removeAt(draft.default_connections, index))
                }
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          ))
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            update("default_connections", [
              ...draft.default_connections,
              { platform: "", name: "" },
            ])
          }
        >
          <Plus className="size-3.5" />
          Add connection
        </Button>
        {inferredDefaultConnections.length > 0 ? (
          <div className="space-y-2 border-t pt-3">
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
                Inferred defaults
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                These are inferred from asset types when no pipeline override exists.
              </p>
            </div>
            {inferredDefaultConnections.map((connection) => (
              <div
                key={`${connection.platform}:${connection.name}`}
                data-testid="inferred-default-connection"
                className="flex items-center gap-3 rounded-md border bg-muted/30 px-3 py-2"
              >
                <div className="min-w-0 flex-1">
                  <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                    Platform
                  </div>
                  <div className="truncate font-mono text-xs">{connection.platform}</div>
                </div>
                <div className="min-w-0 flex-1">
                  <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
                    Connection
                  </div>
                  <div className="truncate font-mono text-xs">{connection.name}</div>
                </div>
                <Badge variant="outline">Inferred</Badge>
                <PipelineConnectionSettingsLink
                  environment={environment}
                  connection={connection.name}
                />
              </div>
            ))}
          </div>
        ) : null}
      </div>
    );
  }
  if (section === "python") {
    return (
      <div className="space-y-4">
        <div>
          <p className="text-sm font-medium">Pipeline dependencies</p>
          <p className="mt-1 text-xs text-muted-foreground">
            Packages are shared by Python assets in this pipeline and installed by uv on their next
            run.
          </p>
        </div>
        <SettingsMultiValueField
          label="Packages"
          value={pythonDependencies}
          onChange={onPythonDependenciesChange}
          placeholder="Add package, for example pandas>=2"
        />
        <p className="text-xs text-muted-foreground">
          Stored in <span className="font-mono">{pythonDependencyPath || "pyproject.toml"}</span>.
          Existing pipeline-level requirements.txt dependencies are migrated on save.
        </p>
      </div>
    );
  }
  if (section === "variables") {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          Pipeline variables available to assets via{" "}
          <span className="font-mono">{"{{ var.name }}"}</span>.
        </p>
        {draft.variables.map((variable, index) => (
          <div key={index} className="space-y-2 rounded-md border p-3">
            <div className="flex items-end gap-2">
              <SettingsTextField
                className="flex-1"
                label="Name"
                value={variable.name}
                onChange={(value) =>
                  update(
                    "variables",
                    replaceAt(draft.variables, index, { ...variable, name: value }),
                  )
                }
                placeholder="lookback_days"
              />
              <SettingsTextField
                className="w-28"
                label="Type"
                value={variable.type}
                onChange={(value) =>
                  update(
                    "variables",
                    replaceAt(draft.variables, index, { ...variable, type: value }),
                  )
                }
                placeholder="integer"
              />
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove variable"
                onClick={() => update("variables", removeAt(draft.variables, index))}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
            <SettingsTextField
              label="Default"
              value={variableValueToText(variable.default_value)}
              onChange={(value) =>
                update(
                  "variables",
                  replaceAt(draft.variables, index, { ...variable, default_value: value }),
                )
              }
              placeholder="30"
            />
            <SettingsTextField
              label="Description"
              value={variable.description ?? ""}
              onChange={(value) =>
                update(
                  "variables",
                  replaceAt(draft.variables, index, {
                    ...variable,
                    description: value || undefined,
                  }),
                )
              }
            />
          </div>
        ))}
        <Button
          variant="outline"
          size="sm"
          onClick={() =>
            update("variables", [
              ...draft.variables,
              { name: "", type: "string", default_value: "" },
            ])
          }
        >
          <Plus className="size-3.5" />
          Add variable
        </Button>
      </div>
    );
  }
  return null;
}

function PipelineConnectionSettingsLink({
  environment,
  connection,
}: {
  environment?: string;
  connection: string;
}) {
  const name = connection.trim();
  if (!name) return null;
  return (
    <Button asChild variant="ghost" size="icon-sm">
      <Link
        to="/project/connections"
        search={{ environment: environment || undefined, connection: name }}
        aria-label={`Open ${name} in project connection settings`}
        title={`Open ${name} in project connection settings`}
      >
        <ExternalLink />
      </Link>
    </Button>
  );
}

function SettingsTextField({
  label,
  value,
  onChange,
  placeholder,
  hint,
  className,
}: {
  label?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  hint?: string;
  className?: string;
}) {
  return (
    <label className={cn("block space-y-1.5", className)}>
      {label ? <span className="text-xs font-medium text-muted-foreground">{label}</span> : null}
      <Input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      {hint ? <span className="block text-[11px] text-muted-foreground">{hint}</span> : null}
    </label>
  );
}

function SettingsMultiValueField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
}) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <MultiValueInput value={value} onChange={onChange} placeholder={placeholder} />
    </label>
  );
}

function SettingsNumberField({
  className,
  label,
  value,
  onChange,
  hint,
  min,
  placeholder,
}: {
  className?: string;
  label: string;
  value?: number;
  onChange: (value: number | undefined) => void;
  hint?: string;
  min?: number;
  placeholder?: string;
}) {
  const id = useId();
  return (
    <Field className={cn("gap-1.5", className)}>
      <FieldLabel htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </FieldLabel>
      <Input
        id={id}
        type="number"
        min={min}
        placeholder={placeholder}
        value={value ?? ""}
        onChange={(event) => {
          const raw = event.target.value.trim();
          onChange(raw === "" ? undefined : Number(raw));
        }}
      />
      {hint ? <FieldDescription className="text-[11px]">{hint}</FieldDescription> : null}
    </Field>
  );
}

function SettingsToggleField({
  label,
  description,
  checked,
  onChange,
  compact,
}: {
  label: string;
  description?: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  compact?: boolean;
}) {
  if (compact) {
    return (
      <label className="flex items-center gap-2 text-sm">
        <Switch checked={checked} onCheckedChange={onChange} />
        {label}
      </label>
    );
  }
  return (
    <div className="flex items-start justify-between gap-3">
      <span>
        <span className="block text-sm font-medium">{label}</span>
        {description ? <span className="text-xs text-muted-foreground">{description}</span> : null}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </div>
  );
}

function replaceAt<T>(list: T[], index: number, value: T): T[] {
  return list.map((item, itemIndex) => (itemIndex === index ? value : item));
}

function removeAt<T>(list: T[], index: number): T[] {
  return list.filter((_, itemIndex) => itemIndex !== index);
}

function variableValueToText(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value;
  return String(value);
}

function sameStringArray(left: string[], right: string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function errorMessage(cause: unknown, fallback: string): string {
  return cause instanceof Error ? cause.message : fallback;
}
