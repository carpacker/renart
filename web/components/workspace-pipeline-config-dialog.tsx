"use client";

import {
  useNavigate,
  useParams,
  useRouterState,
} from "@tanstack/react-router";
import {
  AlertCircle,
  ChevronLeft,
  ChevronRight,
  Copy,
  Loader2,
  Plus,
  RotateCcw,
  Save,
  Settings2,
  Trash2,
  X,
} from "lucide-react";
import {
  cloneElement,
  createContext,
  isValidElement,
  type ReactElement,
  type ReactNode,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";

import { useIsMobile } from "@/hooks/use-mobile";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { getPipelineConfig, updatePipelineConfig } from "@/lib/api-pipelines";
import { copyTextToClipboard } from "@/lib/copy-to-clipboard";
import {
  getPipelineScheduleCompletionItems,
  isValidPipelineSchedule,
  PIPELINE_SCHEDULE_LANGUAGE,
  registerPipelineScheduleLanguage,
} from "@/lib/pipeline-yaml-intellisense";
import type {
  PipelineConfigConnection,
  PipelineConfigNotification,
  PipelineConfigResponse,
  PipelineConfigVariable,
  UpdatePipelineConfigRequest,
} from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
  useComboboxAnchor,
} from "@/components/ui/combobox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { MonacoSingleLineInput } from "@/components/ui/monaco-single-line-input";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";

export const PIPELINE_CONFIG_SECTION_ROUTES = {
  General: "/pipelines/$pipelineId/config/general",
  Connections: "/pipelines/$pipelineId/config/connections",
  Execution: "/pipelines/$pipelineId/config/execution",
  Notifications: "/pipelines/$pipelineId/config/notifications",
  Variables: "/pipelines/$pipelineId/config/variables",
  "YAML Preview": "/pipelines/$pipelineId/config/preview",
} as const;

const SECTION_ORDER = [
  "General",
  "Connections",
  "Execution",
  "Notifications",
  "Variables",
  "YAML Preview",
] as const;

type SectionName = (typeof SECTION_ORDER)[number];

type EditablePipelineConfig = Omit<
  UpdatePipelineConfigRequest,
  "variables" | "default_connections"
> & {
  default_connections: PipelineConfigConnection[];
  variables: PipelineConfigVariable[];
};

type WorkspacePipelineConfigContextValue = {
  draft: EditablePipelineConfig | null;
  dirty: boolean;
  error: string | null;
  isMobile: boolean;
  loading: boolean;
  copying: boolean;
  originalConfig: PipelineConfigResponse | null;
  saveMessage: string | null;
  saving: boolean;
  yamlPreview: string;
  closeDialog: () => void;
  discardChanges: () => void;
  goToSection: (section: SectionName) => void;
  handleCopyYaml: () => Promise<void>;
  handleSave: () => Promise<void>;
  setDraft: (next: EditablePipelineConfig) => void;
};

const WorkspacePipelineConfigContext =
  createContext<WorkspacePipelineConfigContextValue | null>(null);

export function WorkspacePipelineConfigDialogLayout({
  children,
}: {
  children: ReactNode;
}) {
  const navigate = useNavigate();
  const isMobile = useIsMobile();
  const { pipelineId } = useParams({ from: "/_workspace/pipelines/$pipelineId" });
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const locationState = useRouterState({
    select: (state) =>
      state.location.search as {
        pipeline?: string;
        asset?: string;
        environment?: string;
      },
  });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [copying, setCopying] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [originalConfig, setOriginalConfig] =
    useState<PipelineConfigResponse | null>(null);
  const [draft, setDraftState] = useState<EditablePipelineConfig | null>(null);
  const [yamlPreview, setYamlPreview] = useState("");
  const mountedRef = useRef(false);
  const closingRef = useRef(false);

  const currentSection = getCurrentSectionFromPath(pathname);
  const isSectionListPage = currentSection === null;

  useEffect(() => {
    if (closingRef.current) {
      return;
    }
    if (mountedRef.current && isMobile) {
      return;
    }
    if (currentSection !== null) {
      mountedRef.current = true;
      return;
    }

    mountedRef.current = true;
    if (!isMobile) {
      void navigate({
        to: PIPELINE_CONFIG_SECTION_ROUTES.General,
        params: { pipelineId },
        search: locationState,
        replace: true,
      });
    }
  }, [currentSection, isMobile, locationState, navigate, pipelineId]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setSaveMessage(null);

    void getPipelineConfig(pipelineId)
      .then((response) => {
        if (cancelled) {
          return;
        }
        setOriginalConfig(response);
        setDraftState(toEditableConfig(response));
        setYamlPreview(response.yaml);
      })
      .catch((loadError) => {
        if (cancelled) {
          return;
        }
        setError(
          loadError instanceof Error
            ? loadError.message
            : "Failed to load pipeline settings."
        );
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [pipelineId]);

  const dirty = useMemo(() => {
    if (!originalConfig || !draft) {
      return false;
    }
    return JSON.stringify(toEditableConfig(originalConfig)) !== JSON.stringify(draft);
  }, [draft, originalConfig]);

  const closeDialog = () => {
    closingRef.current = true;
    void navigate({
      to: "/",
      search: {
        pipeline: locationState.pipeline,
        asset: locationState.asset,
        environment: locationState.environment,
      },
      replace: true,
    });
  };

  const goToSection = (section: SectionName) => {
    void navigate({
      to: PIPELINE_CONFIG_SECTION_ROUTES[section],
      params: { pipelineId },
      search: locationState,
    });
  };

  const discardChanges = () => {
    if (!originalConfig) {
      return;
    }
    setDraftState(toEditableConfig(originalConfig));
    setYamlPreview(originalConfig.yaml);
    setSaveMessage(null);
    setError(null);
  };

  const handleSave = async () => {
    if (!draft) {
      return;
    }

    setSaving(true);
    setError(null);
    setSaveMessage(null);
    try {
      const response = await updatePipelineConfig(pipelineId, draft);
      setOriginalConfig(response);
      setDraftState(toEditableConfig(response));
      setYamlPreview(response.yaml);
      setSaveMessage("Saved changes");
    } catch (saveError) {
      setError(
        saveError instanceof Error
          ? saveError.message
          : "Failed to save pipeline settings."
      );
    } finally {
      setSaving(false);
    }
  };

  const handleCopyYaml = async () => {
    if (!yamlPreview) {
      return;
    }
    setCopying(true);
    try {
      if (await copyTextToClipboard(yamlPreview)) {
        setSaveMessage("Copied YAML preview");
      } else {
        setError("Failed to copy YAML preview.");
      }
    } catch {
      setError("Failed to copy YAML preview.");
    } finally {
      setCopying(false);
    }
  };

  const contextValue = useMemo<WorkspacePipelineConfigContextValue>(
    () => ({
      draft,
      dirty,
      error,
      isMobile,
      loading,
      copying,
      originalConfig,
      saveMessage,
      saving,
      yamlPreview,
      closeDialog,
      discardChanges,
      goToSection,
      handleCopyYaml,
      handleSave,
      setDraft: setDraftState,
    }),
    [
      copying,
      dirty,
      draft,
      error,
      isMobile,
      loading,
      originalConfig,
      saveMessage,
      saving,
      yamlPreview,
    ]
  );

  const description = dirty ? "Unsaved changes" : "No unsaved changes";
  const leftHeaderAction =
    isMobile && !isSectionListPage ? (
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label="Back to sections"
        onClick={() => {
          void navigate({
            to: "/pipelines/$pipelineId/config",
            params: { pipelineId },
            search: locationState,
          });
        }}
      >
        <ChevronLeft />
      </Button>
    ) : (
      <div className="rounded-lg border bg-muted/30 p-2 text-muted-foreground">
        <Settings2 className="size-5" />
      </div>
    );

  return (
    <WorkspacePipelineConfigContext.Provider value={contextValue}>
      <Dialog open onOpenChange={(open) => !open && closeDialog()}>
        <DialogContent className="left-0 top-0 flex h-dvh w-screen max-w-none translate-x-0 translate-y-0 flex-col overflow-hidden rounded-none border-0 p-0 md:left-1/2 md:top-1/2 md:h-auto md:max-h-[88vh] md:min-h-[680px] md:w-full md:max-w-6xl md:-translate-x-1/2 md:-translate-y-1/2 md:rounded-lg md:border">
          <DialogHeader className="border-b px-4 py-4 md:px-6 md:py-5">
            <div className="flex items-center gap-3 pr-2 sm:pr-8">
              {leftHeaderAction}
              <div className="min-w-0 flex-1">
                <DialogTitle>{originalConfig?.name ?? "Pipeline settings"}</DialogTitle>
                <DialogDescription>{description}</DialogDescription>
              </div>
              <div className="flex items-center gap-2 self-center">
                <ResponsiveActionButton
                  ariaLabel="Discard changes"
                  disabled={!dirty || saving || loading}
                  icon={<RotateCcw />}
                  label="Discard"
                  onClick={discardChanges}
                  variant="outline"
                />
                <ResponsiveActionButton
                  ariaLabel="Save changes"
                  disabled={!dirty || saving || loading}
                  icon={saving ? <Loader2 className="animate-spin" /> : <Save />}
                  label="Save"
                  onClick={() => {
                    void handleSave();
                  }}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label="Close"
                  onClick={closeDialog}
                >
                  <X />
                </Button>
              </div>
            </div>
          </DialogHeader>

          <div className="flex min-h-0 flex-1 overflow-hidden">
            <aside className="hidden w-48 shrink-0 self-stretch border-r bg-muted/20 p-3 md:block">
              <nav className="space-y-1">
                {SECTION_ORDER.map((item) => (
                  <Button
                    key={item}
                    type="button"
                    variant={currentSection === item ? "secondary" : "ghost"}
                    className="w-full justify-start"
                    onClick={() => goToSection(item)}
                  >
                    {item}
                  </Button>
                ))}
              </nav>
            </aside>

            <div className="min-h-0 min-w-0 flex-1 overflow-auto px-4 py-4 md:px-6 md:py-5">
              {error || saveMessage ? (
                <div className="mb-4 flex min-h-5 items-center gap-2 text-sm text-muted-foreground">
                  {error ? (
                    <>
                      <AlertCircle className="size-4 text-destructive" />
                      <span className="text-destructive">{error}</span>
                    </>
                  ) : saveMessage ? (
                    <span>{saveMessage}</span>
                  ) : null}
                </div>
              ) : null}
              {loading ? (
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  Loading pipeline settings...
                </div>
              ) : (
                children
              )}
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </WorkspacePipelineConfigContext.Provider>
  );
}

export function WorkspacePipelineConfigSectionsPage() {
  const { dirty, goToSection } = useWorkspacePipelineConfig();

  return (
    <div className="space-y-4 md:hidden">
      <SectionIntro
        title="Pipeline Settings"
        description="Choose a section to edit."
      />
      {dirty ? (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-300">
          You have unsaved changes.
        </div>
      ) : null}
      <div className="space-y-2">
        {SECTION_ORDER.map((item) => (
          <Button
            key={item}
            type="button"
            variant="outline"
            className="h-auto w-full justify-between px-4 py-3 text-left"
            onClick={() => goToSection(item)}
          >
            <span className="text-sm font-medium">{item}</span>
            <ChevronRight className="text-muted-foreground" />
          </Button>
        ))}
      </div>
    </div>
  );
}

export function WorkspacePipelineConfigSectionPage({
  section,
}: {
  section: SectionName;
}) {
  const { copying, draft, handleCopyYaml, setDraft, yamlPreview } =
    useWorkspacePipelineConfig();
  const { monacoTheme } = useWorkspaceTheme();

  if (!draft) {
    return <div className="text-sm text-muted-foreground">Pipeline not found.</div>;
  }

  return (
    <PipelineConfigSection
      section={section}
      draft={draft}
      monacoTheme={monacoTheme}
      yamlPreview={yamlPreview}
      copying={copying}
      onCopyYaml={() => {
        void handleCopyYaml();
      }}
      onChange={setDraft}
    />
  );
}

export function useWorkspacePipelineConfig() {
  const context = useContext(WorkspacePipelineConfigContext);
  if (!context) {
    throw new Error(
      "useWorkspacePipelineConfig must be used within WorkspacePipelineConfigDialogLayout"
    );
  }
  return context;
}

function ResponsiveActionButton({
  ariaLabel,
  disabled,
  icon,
  label,
  onClick,
  variant = "default",
}: {
  ariaLabel: string;
  disabled: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
  variant?: "default" | "outline";
}) {
  return (
    <>
      <Button
        type="button"
        variant={variant}
        size="icon-sm"
        className="sm:hidden"
        aria-label={ariaLabel}
        disabled={disabled}
        onClick={onClick}
      >
        {icon}
      </Button>
      <Button
        type="button"
        variant={variant}
        className="hidden sm:inline-flex"
        disabled={disabled}
        onClick={onClick}
      >
        {icon}
        {label}
      </Button>
    </>
  );
}

function PipelineConfigSection({
  section,
  draft,
  monacoTheme,
  yamlPreview,
  copying,
  onCopyYaml,
  onChange,
}: {
  section: SectionName;
  draft: EditablePipelineConfig;
  monacoTheme: string;
  yamlPreview: string;
  copying: boolean;
  onCopyYaml: () => void;
  onChange: (next: EditablePipelineConfig) => void;
}) {
  const scheduleInvalid = !isValidPipelineSchedule(draft.schedule);

  if (section === "General") {
    return (
      <div className="space-y-6">
        <SectionIntro
          title="General"
          description="Basic pipeline identity and schedule."
        />
        <div className="grid gap-4 md:grid-cols-2">
          <LabeledField label="Pipeline Name">
            <Input
              value={draft.name}
              onChange={(event) => onChange({ ...draft, name: event.target.value })}
            />
          </LabeledField>
          <LabeledField label="Owner">
            <Input
              value={draft.owner}
              onChange={(event) => onChange({ ...draft, owner: event.target.value })}
            />
          </LabeledField>
          <LabeledField
            label="Schedule"
            description={
              scheduleInvalid
                ? "Use @daily, @hourly, or a five-field cron expression."
                : undefined
            }
          >
            <MonacoSingleLineInput
              value={draft.schedule}
              onChange={(schedule) => onChange({ ...draft, schedule })}
              aria-invalid={scheduleInvalid}
              placeholder="@daily"
              language={PIPELINE_SCHEDULE_LANGUAGE}
              path="renart-pipeline-schedule.schedule"
              theme={monacoTheme}
              configureMonaco={registerPipelineScheduleLanguage}
              completionProvider={({ monaco, model, position }) => {
                const lineLength = model.getLineMaxColumn(position.lineNumber);
                return {
                  suggestions: getPipelineScheduleCompletionItems({
                    monaco,
                    value: model.getValue(),
                    range: {
                      startLineNumber: position.lineNumber,
                      endLineNumber: position.lineNumber,
                      startColumn: 1,
                      endColumn: lineLength,
                    },
                  }),
                };
              }}
            />
          </LabeledField>
          <LabeledField label="Start Date">
            <Input
              type="date"
              value={draft.start_date}
              onChange={(event) =>
                onChange({ ...draft, start_date: event.target.value })
              }
            />
          </LabeledField>
        </div>
        <LabeledField
          label="Tags"
          description="Add one or more tags used for filtering."
        >
          <MultiValueCombobox
            value={draft.tags}
            onChange={(next) => onChange({ ...draft, tags: next })}
            placeholder="Add tag"
          />
        </LabeledField>
        <LabeledField label="Domains">
          <MultiValueCombobox
            value={draft.domains}
            onChange={(next) => onChange({ ...draft, domains: next })}
            placeholder="Add domain"
          />
        </LabeledField>
      </div>
    );
  }

  if (section === "Connections") {
    return (
      <div className="space-y-6">
        <SectionIntro
          title="Default Connections"
          description="Per-platform connection names inherited by all assets."
        />
        <div className="space-y-4">
          {draft.default_connections.map((item, index) => (
            <div key={index} className="rounded-lg border p-4">
              <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
                <LabeledField label="Platform">
                  <Input
                    value={item.platform}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        default_connections: replaceAt(
                          draft.default_connections,
                          index,
                          {
                            ...item,
                            platform: event.target.value,
                          }
                        ),
                      })
                    }
                    placeholder="snowflake"
                  />
                </LabeledField>
                <LabeledField label="Connection name">
                  <Input
                    value={item.name}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        default_connections: replaceAt(
                          draft.default_connections,
                          index,
                          {
                            ...item,
                            name: event.target.value,
                          }
                        ),
                      })
                    }
                    placeholder="sf-default"
                  />
                </LabeledField>
                <div className="flex items-end">
                  <Button
                    type="button"
                    variant="outline"
                    size="icon-sm"
                    aria-label="Remove connection"
                    onClick={() =>
                      onChange({
                        ...draft,
                        default_connections:
                          draft.default_connections.filter(
                            (_, itemIndex) => itemIndex !== index
                          ),
                      })
                    }
                  >
                    <Trash2 />
                  </Button>
                </div>
              </div>
            </div>
          ))}
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            onChange({
              ...draft,
              default_connections: [
                ...draft.default_connections,
                { platform: "", name: "" },
              ],
            })
          }
        >
          <Plus />
          Add platform
        </Button>
      </div>
    );
  }

  if (section === "Execution") {
    return (
      <div className="space-y-6">
        <SectionIntro
          title="Execution"
          description="Retry behavior, parallelism, and backfill settings."
        />
        <div className="grid gap-4 md:grid-cols-2">
          <LabeledField label="Retries">
            <Input
              type="number"
              min="0"
              value={String(draft.retries)}
              onChange={(event) =>
                onChange({ ...draft, retries: toNumber(event.target.value, 0) })
              }
            />
          </LabeledField>
          <LabeledField label="Rerun Cooldown (s)">
            <Input
              type="number"
              min="0"
              value={
                draft.defaults.rerun_cooldown == null
                  ? ""
                  : String(draft.defaults.rerun_cooldown)
              }
              onChange={(event) =>
                onChange({
                  ...draft,
                  defaults: {
                    ...draft.defaults,
                    rerun_cooldown: toOptionalNumber(event.target.value),
                  },
                })
              }
            />
          </LabeledField>
          <LabeledField label="Concurrency">
            <Input
              type="number"
              min="1"
              value={String(draft.concurrency)}
              onChange={(event) =>
                onChange({ ...draft, concurrency: toNumber(event.target.value, 1) })
              }
            />
          </LabeledField>
          <LabeledField label="Max Active Steps">
            <Input
              type="number"
              min="1"
              value={
                draft.max_active_steps == null ? "" : String(draft.max_active_steps)
              }
              onChange={(event) =>
                onChange({
                  ...draft,
                  max_active_steps: toOptionalNumber(event.target.value),
                })
              }
            />
          </LabeledField>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <LabeledField
            label="Start Offset"
            description="Examples: -1d, 2h, {{ var }}"
          >
            <Input
              value={draft.defaults.start_offset_raw ?? ""}
              onChange={(event) =>
                onChange({
                  ...draft,
                  defaults: {
                    ...draft.defaults,
                    start_offset_raw: event.target.value,
                  },
                })
              }
              placeholder="-1d"
            />
          </LabeledField>
          <LabeledField
            label="End Offset"
            description="Examples: -1d, 2h, {{ var }}"
          >
            <Input
              value={draft.defaults.end_offset_raw ?? ""}
              onChange={(event) =>
                onChange({
                  ...draft,
                  defaults: {
                    ...draft.defaults,
                    end_offset_raw: event.target.value,
                  },
                })
              }
              placeholder="-1d"
            />
          </LabeledField>
        </div>
        <ToggleRow
          label="Catchup"
          description="Backfill missed intervals between start date and now."
          checked={draft.catchup}
          onCheckedChange={(checked) => onChange({ ...draft, catchup: checked })}
        />
        <ToggleRow
          label="Metadata Push to BigQuery"
          description="Export pipeline metadata to BigQuery."
          checked={draft.metadata_push_bigquery}
          onCheckedChange={(checked) =>
            onChange({ ...draft, metadata_push_bigquery: checked })
          }
        />
      </div>
    );
  }

  if (section === "Notifications") {
    return (
      <div className="space-y-6">
        <SectionIntro
          title="Notifications"
          description="Alerts on run success or failure."
        />
        <NotificationCard
          title="Slack"
          enabled={draft.notifications_slack.enabled}
          onEnabledChange={(enabled) =>
            onChange({
              ...draft,
              notifications_slack: { ...draft.notifications_slack, enabled },
            })
          }
        >
          <LabeledField label="Channel">
            <Input
              value={draft.notifications_slack.channel ?? ""}
              onChange={(event) =>
                onChange({
                  ...draft,
                  notifications_slack: {
                    ...draft.notifications_slack,
                    channel: event.target.value,
                  },
                })
              }
              placeholder="#data-alerts"
            />
          </LabeledField>
          <NotificationCheckboxes
            value={draft.notifications_slack}
            onChange={(next) =>
              onChange({ ...draft, notifications_slack: next })
            }
          />
        </NotificationCard>
        <NotificationCard
          title="Microsoft Teams"
          enabled={draft.notifications_teams.enabled}
          onEnabledChange={(enabled) =>
            onChange({
              ...draft,
              notifications_teams: { ...draft.notifications_teams, enabled },
            })
          }
        >
          <LabeledField label="Connection">
            <Input
              value={draft.notifications_teams.connection ?? ""}
              onChange={(event) =>
                onChange({
                  ...draft,
                  notifications_teams: {
                    ...draft.notifications_teams,
                    connection: event.target.value,
                  },
                })
              }
              placeholder="teams-default"
            />
          </LabeledField>
          <NotificationCheckboxes
            value={draft.notifications_teams}
            onChange={(next) =>
              onChange({ ...draft, notifications_teams: next })
            }
          />
        </NotificationCard>
      </div>
    );
  }

  if (section === "Variables") {
    return (
      <div className="space-y-6">
        <SectionIntro
          title="Variables"
          description="Pipeline-scoped parameters using JSON Schema draft-07 style properties."
        />
        <div className="space-y-4">
          {draft.variables.map((variable, index) => (
            <div key={index} className="rounded-lg border p-4">
              <div className="mb-4 flex items-center justify-between gap-2">
                <div className="text-sm font-medium">Variable {index + 1}</div>
                <Button
                  type="button"
                  variant="outline"
                  size="icon-sm"
                  aria-label="Remove variable"
                  onClick={() =>
                    onChange({
                      ...draft,
                      variables: draft.variables.filter(
                        (_, itemIndex) => itemIndex !== index
                      ),
                    })
                  }
                >
                  <Trash2 />
                </Button>
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                <LabeledField label="Name">
                  <Input
                    value={variable.name}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        variables: replaceAt(draft.variables, index, {
                          ...variable,
                          name: event.target.value,
                        }),
                      })
                    }
                    placeholder="target_segment"
                  />
                </LabeledField>
                <LabeledField label="Type">
                  <Input
                    value={variable.type}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        variables: replaceAt(draft.variables, index, {
                          ...variable,
                          type: event.target.value,
                        }),
                      })
                    }
                    placeholder="string"
                  />
                </LabeledField>
              </div>
              <div className="mt-4 grid gap-4 md:grid-cols-2">
                <LabeledField label="Default Value">
                  <Input
                    value={stringifyValue(variable.default_value)}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        variables: replaceAt(draft.variables, index, {
                          ...variable,
                          default_value: parseLooseValue(event.target.value),
                        }),
                      })
                    }
                    placeholder="enterprise"
                  />
                </LabeledField>
                <LabeledField label="Description">
                  <Input
                    value={variable.description ?? ""}
                    onChange={(event) =>
                      onChange({
                        ...draft,
                        variables: replaceAt(draft.variables, index, {
                          ...variable,
                          description: event.target.value,
                        }),
                      })
                    }
                  />
                </LabeledField>
              </div>
              <LabeledField
                label="Extra JSON Schema Properties"
                description="Enter a JSON object for keys like enum, minimum, or maximum."
              >
                <Textarea
                  value={
                    variable.extra ? JSON.stringify(variable.extra, null, 2) : "{}"
                  }
                  onChange={(event) =>
                    onChange({
                      ...draft,
                      variables: replaceAt(draft.variables, index, {
                        ...variable,
                        extra: parseExtraObject(event.target.value),
                      }),
                    })
                  }
                  className="min-h-28 font-mono text-xs"
                />
              </LabeledField>
            </div>
          ))}
        </div>
        <Button
          type="button"
          variant="outline"
          onClick={() =>
            onChange({
              ...draft,
              variables: [
                ...draft.variables,
                {
                  name: "new_variable",
                  type: "string",
                  default_value: "",
                  description: "",
                  extra: {},
                },
              ],
            })
          }
        >
          <Plus />
          Add variable
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <SectionIntro
        title="YAML Preview"
        description="Preview the generated pipeline configuration."
      />
      <div className="rounded-lg border bg-muted/20">
        <pre className="max-h-[52vh] overflow-auto p-4 text-xs leading-6 text-foreground">
          <code>{yamlPreview}</code>
        </pre>
      </div>
      <Button
        type="button"
        variant="outline"
        onClick={onCopyYaml}
        disabled={copying}
      >
        {copying ? <Loader2 className="animate-spin" /> : <Copy />}
        Copy YAML
      </Button>
    </div>
  );
}

function SectionIntro({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="space-y-1">
      <h2 className="text-base font-semibold">{title}</h2>
      <p className="text-sm text-muted-foreground">{description}</p>
    </div>
  );
}

function LabeledField({
  label,
  description,
  children,
}: {
  label: string;
  description?: string;
  children: ReactNode;
}) {
  const inputId = useId();
  const descriptionId = description ? `${inputId}-description` : undefined;
  const control = isValidElement(children)
    ? cloneElement(
        children as ReactElement<{ id?: string; "aria-describedby"?: string }>,
        {
          id: (children as ReactElement<{ id?: string }>).props.id ?? inputId,
          "aria-describedby": descriptionId,
        }
      )
    : children;

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={inputId}>{label}</Label>
      {control}
      {description ? (
        <p id={descriptionId} className="text-xs text-muted-foreground">
          {description}
        </p>
      ) : null}
    </div>
  );
}

function ToggleRow({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string;
  description: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-4">
      <div className="space-y-1">
        <div className="text-sm font-medium">{label}</div>
        <div className="text-sm text-muted-foreground">{description}</div>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  );
}

function NotificationCard({
  title,
  enabled,
  onEnabledChange,
  children,
}: {
  title: string;
  enabled: boolean;
  onEnabledChange: (enabled: boolean) => void;
  children: ReactNode;
}) {
  return (
    <div className="rounded-lg border">
      <div className="flex items-center gap-3 px-4 py-3">
        <div className="text-sm font-medium">{title}</div>
        <div className="ml-auto">
          <Switch checked={enabled} onCheckedChange={onEnabledChange} />
        </div>
      </div>
      {enabled ? (
        <>
          <Separator />
          <div className="space-y-4 px-4 py-4">{children}</div>
        </>
      ) : null}
    </div>
  );
}

function NotificationCheckboxes({
  value,
  onChange,
}: {
  value: PipelineConfigNotification;
  onChange: (next: PipelineConfigNotification) => void;
}) {
  return (
    <div className="flex flex-wrap gap-6">
      <label className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={value.success}
          onCheckedChange={(checked) =>
            onChange({ ...value, success: Boolean(checked) })
          }
        />
        On success
      </label>
      <label className="flex items-center gap-2 text-sm">
        <Checkbox
          checked={value.failure}
          onCheckedChange={(checked) =>
            onChange({ ...value, failure: Boolean(checked) })
          }
        />
        On failure
      </label>
    </div>
  );
}

function MultiValueCombobox({
  value,
  onChange,
  placeholder,
}: {
  value: string[];
  onChange: (next: string[]) => void;
  placeholder: string;
}) {
  const anchor = useComboboxAnchor();
  const normalizedValue = compactUnique(value);
  const [draft, setDraft] = useState("");
  const draftAsItem = draft.trim();

  const items = useMemo(() => {
    if (!draftAsItem) {
      return normalizedValue;
    }
    if (normalizedValue.includes(draftAsItem)) {
      return normalizedValue;
    }
    return [...normalizedValue, draftAsItem];
  }, [draftAsItem, normalizedValue]);

  const commitDraft = () => {
    const additions = splitCommaSeparated(draft);
    if (additions.length === 0) {
      return;
    }
    onChange(compactUnique([...normalizedValue, ...additions]));
    setDraft("");
  };

  const toggleValue = (item: string) => {
    const alreadySelected = normalizedValue.some(
      (current) => current.toLowerCase() === item.toLowerCase()
    );

    if (alreadySelected) {
      onChange(
        normalizedValue.filter(
          (current) => current.toLowerCase() !== item.toLowerCase()
        )
      );
      setDraft("");
      return;
    }

    onChange(compactUnique([...normalizedValue, item]));
    setDraft("");
  };

  return (
    <Combobox
      multiple
      autoHighlight
      items={items}
      value={normalizedValue}
      onValueChange={(nextValue) =>
        onChange(
          Array.isArray(nextValue) ? compactUnique(nextValue as string[]) : []
        )
      }
    >
      <ComboboxChips ref={anchor} className="w-full">
        <ComboboxValue>
          {(values) => (
            <>
              {(values as string[]).map((item) => (
                <ComboboxChip key={item}>{item}</ComboboxChip>
              ))}
              <ComboboxChipsInput
                value={draft}
                placeholder={placeholder}
                onChange={(event) => setDraft(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === ",") {
                    event.preventDefault();
                    commitDraft();
                  }
                }}
              />
            </>
          )}
        </ComboboxValue>
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>No values yet.</ComboboxEmpty>
        <ComboboxList>
          {(item) => (
            <ComboboxItem
              key={item}
              value={item}
              onClick={() => toggleValue(item)}
            >
              {item}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

function getCurrentSectionFromPath(pathname: string): SectionName | null {
  if (pathname.endsWith("/general")) {
    return "General";
  }
  if (pathname.endsWith("/connections")) {
    return "Connections";
  }
  if (pathname.endsWith("/execution")) {
    return "Execution";
  }
  if (pathname.endsWith("/notifications")) {
    return "Notifications";
  }
  if (pathname.endsWith("/variables")) {
    return "Variables";
  }
  if (pathname.endsWith("/preview")) {
    return "YAML Preview";
  }
  return null;
}

function toEditableConfig(config: PipelineConfigResponse): EditablePipelineConfig {
  return {
    name: config.name,
    schedule: config.schedule ?? "",
    start_date: config.start_date ?? "",
    owner: config.owner ?? "",
    tags: [...(config.tags ?? [])],
    domains: [...(config.domains ?? [])],
    default_connections: [...(config.default_connections ?? [])],
    catchup: config.catchup,
    metadata_push_bigquery: config.metadata_push_bigquery,
    retries: config.retries,
    concurrency: config.concurrency,
    max_active_steps: config.max_active_steps,
    notifications_slack: { ...config.notifications_slack },
    notifications_teams: { ...config.notifications_teams },
    defaults: { ...config.defaults },
    variables: config.variables.map((item) => ({
      ...item,
      extra: item.extra ? { ...item.extra } : {},
    })),
  };
}

function splitCommaSeparated(value: string) {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function compactUnique(values: string[]) {
  const result: string[] = [];
  const seen = new Set<string>();
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed) {
      continue;
    }
    const key = trimmed.toLowerCase();
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    result.push(trimmed);
  }
  return result;
}

function replaceAt<T>(items: T[], index: number, value: T) {
  return items.map((item, itemIndex) => (itemIndex === index ? value : item));
}

function toNumber(value: string, fallback: number) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function toOptionalNumber(value: string) {
  if (value.trim() === "") {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function stringifyValue(value: unknown) {
  if (value == null) {
    return "";
  }
  if (typeof value === "string") {
    return value;
  }
  return JSON.stringify(value);
}

function parseLooseValue(value: string): unknown {
  const trimmed = value.trim();
  if (trimmed === "") {
    return "";
  }
  if (trimmed === "true") {
    return true;
  }
  if (trimmed === "false") {
    return false;
  }
  if (trimmed === "null") {
    return null;
  }
  const numeric = Number(trimmed);
  if (!Number.isNaN(numeric) && trimmed === String(numeric)) {
    return numeric;
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    return value;
  }
}

function parseExtraObject(value: string) {
  try {
    const parsed = JSON.parse(value);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
  } catch {
    return {};
  }
  return {};
}
