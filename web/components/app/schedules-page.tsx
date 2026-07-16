import { Link } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import {
  AlertTriangle,
  ArchiveRestore,
  CircleCheck,
  Clock,
  Loader2,
  Package,
  Play,
  Plus,
  RefreshCw,
  Search,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { envScheduleKey, useEnvSchedules } from "@/hooks/use-env-schedules";
import { formatSchedulerDate, usePipelineScheduler } from "@/hooks/use-pipeline-scheduler";
import { usePipelineDeploy } from "@/hooks/use-pipeline-deploy";
import { triggerPipelineRun } from "@/lib/api";
import { activePipelineRunConflict } from "@/lib/api-scheduler";
import type { CatchupPolicy, EnvSchedule, UpsertEnvScheduleInput } from "@/lib/api-env-schedules";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import type { PipelineRun } from "@/lib/types";

import { PageHeader, AppPage, AppPanel } from "./app-primitives";

const buckets = ["1hr", "6hr", "12hr", "24hr"] as const;

// TimelineSchedule is the slice of a schedule the timeline rendering needs;
// both legacy single-env and per-environment rows satisfy it.
type TimelineSchedule = {
  schedule: string;
  timezone: string;
  enabled: boolean;
  next_run_at?: string;
};

export function AppSchedulesPage() {
  const {
    runs,
    runsError,
    schedulesError,
    refresh: refreshPipelineScheduler,
  } = usePipelineScheduler();
  const envSchedules = useEnvSchedules();
  const [query, setQuery] = useState("");
  const [bucket, setBucket] = useState<(typeof buckets)[number]>("12hr");
  const [newScheduleOpen, setNewScheduleOpen] = useState(false);
  const tickDensity = useTimelineTickDensity();
  const window = timelineWindow(bucket, tickDensity);
  const axis = timelineAxis(window);
  const filteredSchedules = envSchedules.schedules.filter((schedule) => {
    const value = query.trim().toLowerCase();
    return (
      !value ||
      (schedule.pipeline_name ?? "").toLowerCase().includes(value) ||
      schedule.environment.toLowerCase().includes(value) ||
      schedule.cron.toLowerCase().includes(value)
    );
  });
  const schedulerRefreshError = [runsError, schedulesError].filter(Boolean).join(" ");

  return (
    <AppPage>
      <PageHeader
        title="Schedules"
        subtitle="One schedule per pipeline and environment; scheduled runs execute the pinned deployed snapshot"
        actions={
          envSchedules.loading ? (
            <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" />
              Loading
            </span>
          ) : envSchedules.canMutate ? (
            <Badge variant="secondary">
              <CircleCheck className="size-3" />
              Scheduler active here
            </Badge>
          ) : (
            <Badge variant="outline">Read-only</Badge>
          )
        }
      />
      {!envSchedules.loading && !envSchedules.canMutate ? (
        <div className="px-3 pb-2">
          <Alert
            variant={envSchedules.ownership?.state === "unavailable" ? "destructive" : "default"}
          >
            <AlertTriangle />
            <AlertTitle>
              {envSchedules.ownership?.state === "follower"
                ? "Schedules are managed by another Renart process"
                : "Scheduler unavailable"}
            </AlertTitle>
            <AlertDescription>
              {envSchedules.ownershipReason} Existing schedules remain visible, but changes and runs
              are disabled here.
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      {schedulerRefreshError ? (
        <div className="px-3 pb-2">
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Scheduler activity could not be refreshed</AlertTitle>
            <AlertDescription className="flex items-center justify-between gap-3">
              <span>
                {schedulerRefreshError} Last successfully loaded activity remains visible.
              </span>
              <Button variant="outline" size="xs" onClick={() => void refreshPipelineScheduler()}>
                <RefreshCw />
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      <div className="flex items-center gap-2 px-3 pb-2">
        <div className="relative min-w-0 flex-1 md:max-w-sm">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="Filter jobs..."
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </div>
        <Button
          variant="outline"
          size="sm"
          disabled={!envSchedules.canMutate}
          title={!envSchedules.canMutate ? envSchedules.ownershipReason : undefined}
          onClick={() => setNewScheduleOpen(true)}
        >
          <Plus className="size-3.5" />
          New schedule
        </Button>
        <div className="ml-auto hidden overflow-hidden rounded-lg border md:flex">
          {buckets.map((item) => (
            <Button
              key={item}
              variant={bucket === item ? "default" : "ghost"}
              size="sm"
              className="rounded-none"
              onClick={() => setBucket(item)}
            >
              {item}
            </Button>
          ))}
        </div>
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <AppPanel className="h-full overflow-auto">
          <TooltipProvider>
            <div className="min-w-[1120px]">
              <div className="sticky top-0 z-10 flex h-9 items-center border-b bg-card text-[11px] font-semibold uppercase text-muted-foreground">
                <div className="w-80 px-3">Jobs</div>
                <TimelineAxis axis={axis} />
                <div className="w-[28rem] px-3 text-right">Controls</div>
              </div>
              {envSchedules.loading && filteredSchedules.length === 0 ? (
                <div className="flex h-24 items-center gap-2 px-3 text-sm text-muted-foreground">
                  <Loader2 className="size-4 animate-spin" />
                  Loading schedules...
                </div>
              ) : null}
              {!envSchedules.loading && filteredSchedules.length === 0 ? (
                <div className="px-3 py-8 text-sm text-muted-foreground">
                  No schedules yet. Use “New schedule” to run a pipeline in an environment.
                </div>
              ) : null}
              {filteredSchedules.map((schedule) => (
                <EnvScheduleRow
                  key={envScheduleKey(schedule)}
                  schedule={schedule}
                  window={window}
                  axis={axis}
                  busy={envSchedules.busyKey === envScheduleKey(schedule)}
                  canMutate={envSchedules.canMutate}
                  ownershipReason={envSchedules.ownershipReason}
                  activeRun={runs.find(
                    (run) =>
                      run.pipeline_id === schedule.pipeline_id &&
                      run.environment === schedule.environment &&
                      (run.status === "queued" || run.status === "running"),
                  )}
                  onSetStatus={(status) => envSchedules.setStatus(schedule, status)}
                  onArchive={() => envSchedules.archive(schedule)}
                  onUpdateDeployment={async () => {
                    if (!schedule.pipeline_id) return;
                    await envSchedules.upsert(
                      { ...schedule, pipeline_id: schedule.pipeline_id },
                      {
                        cron: schedule.cron,
                        timezone: schedule.timezone,
                        vars: schedule.vars,
                        catchup_policy: schedule.catchup_policy,
                        paused: schedule.status === "paused",
                        deploy_now: true,
                      },
                    );
                  }}
                />
              ))}
              {envSchedules.archived.length > 0 ? (
                <ArchivedSection
                  archived={envSchedules.archived}
                  canMutate={envSchedules.canMutate}
                  ownershipReason={envSchedules.ownershipReason}
                  onRestore={(schedule) => void envSchedules.setStatus(schedule, "active")}
                />
              ) : null}
            </div>
          </TooltipProvider>
        </AppPanel>
      </div>
      <NewEnvScheduleDialog
        open={newScheduleOpen}
        onOpenChange={setNewScheduleOpen}
        canMutate={envSchedules.canMutate}
        ownershipReason={envSchedules.ownershipReason}
        onCreate={async (pipeline, environment, input) => {
          await envSchedules.upsert(
            { pipeline_uuid: pipeline.uuid ?? "", environment, pipeline_id: pipeline.id },
            input,
          );
        }}
      />
    </AppPage>
  );
}

function EnvScheduleRow({
  schedule,
  window,
  axis,
  busy,
  canMutate,
  ownershipReason,
  activeRun,
  onSetStatus,
  onArchive,
  onUpdateDeployment,
}: {
  schedule: EnvSchedule;
  window: TimelineWindow;
  axis: TimelineTick[];
  busy: boolean;
  canMutate: boolean;
  ownershipReason: string;
  activeRun?: PipelineRun;
  onSetStatus: (status: "active" | "paused") => Promise<void>;
  onArchive: () => Promise<void>;
  onUpdateDeployment: () => Promise<void>;
}) {
  const deployState = usePipelineDeploy(schedule.pipeline_id);
  const configuredEnabled = schedule.status === "active";
  const latestVersion = deployState.status?.version_id;
  const pinnedVersion = schedule.snapshot_version_id?.trim() ?? "";
  const overrideNames = Object.keys(schedule.vars ?? {}).sort();
  const deploymentOutdated = Boolean(
    latestVersion && pinnedVersion && latestVersion !== pinnedVersion,
  );
  const pinnedDeploymentCorrupt = Boolean(
    pinnedVersion &&
    latestVersion === pinnedVersion &&
    deployState.status?.has_snapshot &&
    !deployState.status.executable,
  );
  const [triggering, setTriggering] = useState(false);
  const [actionError, setActionError] = useState<{
    message: string;
    activeRunId?: string;
  } | null>(null);
  const sourceBlockReason = !pinnedVersion
    ? "This schedule needs an exact deployment pin before it can run"
    : pinnedDeploymentCorrupt
      ? `Pinned deployment ${pinnedVersion.slice(0, 8)} failed its integrity check${deployState.status?.integrity_error ? `: ${deployState.status.integrity_error}` : ""}`
      : overrideNames.length > 0
        ? `Schedule overrides are not executable yet: ${overrideNames.join(", ")}`
        : undefined;
  const runBlockReason = !canMutate ? ownershipReason : sourceBlockReason;
  const enabled = configuredEnabled && !sourceBlockReason;
  const timeline: TimelineSchedule = {
    schedule: schedule.cron,
    timezone: schedule.timezone,
    enabled,
    next_run_at: enabled ? schedule.next_run_at : undefined,
  };
  const slots = expectedSlots(timeline, window);
  const runBusy = busy || triggering || Boolean(activeRun);
  const runDisabled = runBusy || Boolean(runBlockReason);
  const runLabel =
    activeRun?.status === "running"
      ? "Running"
      : activeRun?.status === "queued"
        ? "Queued"
        : pinnedVersion
          ? `Run pinned ${pinnedVersion.slice(0, 8)}`
          : "Needs deployment";
  const nowLeft = timelineLeft(Date.now(), window);
  const runWindowDescription = `Environment ${schedule.environment}. This action sends and records no interval; when execution starts, the backend resolves the effective window from the pipeline schedule stored in deployment ${pinnedVersion.slice(0, 8)}.`;
  const triggerNow = async () => {
    if (!schedule.pipeline_id || runBlockReason) return;
    setTriggering(true);
    setActionError(null);
    try {
      await triggerPipelineRun(schedule.pipeline_id, {
        source: "snapshot",
        snapshot_version_id: pinnedVersion,
        environment: schedule.environment,
      });
    } catch (cause) {
      const conflict = activePipelineRunConflict(cause);
      setActionError({
        message: conflict
          ? "A run is already queued or running for this pipeline."
          : cause instanceof Error
            ? cause.message
            : "Failed to queue the run.",
        activeRunId: conflict?.activeRunId,
      });
    } finally {
      setTriggering(false);
    }
  };
  const updateDeployment = async () => {
    setActionError(null);
    try {
      await onUpdateDeployment();
      await deployState.refresh();
    } catch (cause) {
      setActionError({
        message: cause instanceof Error ? cause.message : "Failed to update the deployment.",
      });
    }
  };
  const updateStatus = async (status: "active" | "paused") => {
    setActionError(null);
    try {
      await onSetStatus(status);
    } catch (cause) {
      setActionError({
        message: cause instanceof Error ? cause.message : "Failed to update the schedule.",
      });
    }
  };
  const archive = async () => {
    setActionError(null);
    try {
      await onArchive();
    } catch (cause) {
      setActionError({
        message: cause instanceof Error ? cause.message : "Failed to archive the schedule.",
      });
    }
  };
  return (
    <div className="flex min-h-14 items-center border-b hover:bg-muted/40">
      <div className="flex w-80 min-w-0 items-center gap-3 px-3">
        <Switch
          checked={configuredEnabled}
          disabled={!canMutate || busy || (!configuredEnabled && Boolean(sourceBlockReason))}
          title={!canMutate ? ownershipReason : undefined}
          aria-label={`${configuredEnabled ? "Pause" : "Resume"} ${schedule.pipeline_name || schedule.pipeline_uuid} in ${schedule.environment}`}
          onCheckedChange={(next) => void updateStatus(next ? "active" : "paused")}
        />
        <div className="min-w-0">
          <div className="flex items-center gap-2 font-mono text-xs text-primary">
            <Clock className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{schedule.pipeline_name || schedule.pipeline_uuid}</span>
            <Badge variant="secondary" size="xs">
              {schedule.environment}
            </Badge>
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="truncate font-mono">{schedule.cron}</span>
            <span>·</span>
            <span className="truncate">{schedule.timezone || "UTC"}</span>
            {schedule.last_run ? (
              <>
                <span>·</span>
                <span className="truncate">
                  last {schedule.last_run.status}{" "}
                  {formatSchedulerDate(
                    schedule.last_run.finished_at ?? schedule.last_run.started_at,
                  )}
                </span>
              </>
            ) : null}
            {pinnedVersion ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="inline-flex items-center gap-0.5 truncate" tabIndex={0}>
                    <Package className="size-3" />
                    {pinnedVersion.slice(0, 8)}
                  </span>
                </TooltipTrigger>
                <TooltipContent>Pinned deployed snapshot {pinnedVersion}</TooltipContent>
              </Tooltip>
            ) : (
              <Badge variant="destructive" size="xs">
                Needs deployment
              </Badge>
            )}
            {overrideNames.length > 0 ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="outline" size="xs" tabIndex={0}>
                    Overrides unsupported
                  </Badge>
                </TooltipTrigger>
                <TooltipContent>
                  Stored variables are blocked until execution can preserve them:{" "}
                  {overrideNames.join(", ")}
                </TooltipContent>
              </Tooltip>
            ) : null}
            {deploymentOutdated ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="destructive" size="xs" tabIndex={0}>
                    Older deployment
                  </Badge>
                </TooltipTrigger>
                <TooltipContent className="max-w-80">
                  This schedule runs snapshot {pinnedVersion.slice(0, 8)}. The latest deployment is{" "}
                  {latestVersion?.slice(0, 8)}. Data freshness is tracked separately.
                </TooltipContent>
              </Tooltip>
            ) : null}
            {pinnedDeploymentCorrupt ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge variant="destructive" size="xs" tabIndex={0}>
                    Deployment needs repair
                  </Badge>
                </TooltipTrigger>
                <TooltipContent className="max-w-80">
                  {deployState.status?.integrity_error ??
                    "The pinned deployment failed its integrity check."}
                </TooltipContent>
              </Tooltip>
            ) : null}
          </div>
        </div>
      </div>
      <div className="relative h-14 flex-1 border-x bg-muted/20">
        <TimelineGrid axis={axis} />
        {slots.map((slot, index) => (
          <Tooltip key={`${slot.at}-${slot.kind}-${index}`}>
            <TooltipTrigger asChild>
              <span
                className={slotClassName(slot.kind, enabled, slot.phase)}
                style={{ left: `${slot.left}%`, width: `${slot.width}%` }}
                tabIndex={0}
                role="img"
                aria-label={`${slot.kind === "persisted" ? "Next scheduled run" : slot.phase === "past" ? "Past expected run" : "Expected run"} ${formatSchedulerDate(slot.at)}`}
              />
            </TooltipTrigger>
            <TooltipContent>
              <div className="font-medium">
                {slot.kind === "persisted"
                  ? "Next scheduled run"
                  : slot.phase === "past"
                    ? "Past expected run"
                    : "Expected run"}
              </div>
              <div className="font-mono">{formatSchedulerDate(slot.at)}</div>
              {slot.kind === "projected" ? (
                <div className="text-background/70">Projected from the schedule</div>
              ) : null}
            </TooltipContent>
          </Tooltip>
        ))}
        {nowLeft !== null ? <NowMarker left={nowLeft} /> : null}
      </div>
      <div className="flex w-[28rem] flex-wrap items-center justify-end gap-2 px-3 py-2">
        {actionError ? (
          <div
            className="flex basis-full items-center justify-end gap-1 text-right text-[11px] text-destructive"
            role="alert"
          >
            <span className="truncate">{actionError.message}</span>
            {actionError.activeRunId ? (
              <Button asChild variant="link" size="xs">
                <Link to="/runs/$runId" params={{ runId: actionError.activeRunId }}>
                  Open active run
                </Link>
              </Button>
            ) : null}
          </div>
        ) : null}
        <span
          className="text-[10px] uppercase text-muted-foreground"
          data-testid="schedule-run-window-context"
        >
          {schedule.catchup_policy.replace("_", " ")} · runtime window from pinned pipeline
        </span>
        <Button
          size="sm"
          variant="ghost"
          disabled={!canMutate || busy}
          title={!canMutate ? ownershipReason : "Archive schedule (run history is kept)"}
          onClick={() => void archive()}
        >
          <ArchiveRestore />
        </Button>
        {deploymentOutdated || !pinnedVersion || pinnedDeploymentCorrupt ? (
          <Button
            size="sm"
            variant="secondary"
            disabled={!canMutate || busy}
            title={
              !canMutate ? ownershipReason : "Deploy the current pipeline and update this schedule"
            }
            onClick={() => void updateDeployment()}
          >
            {busy ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <RefreshCw data-icon="inline-start" />
            )}
            {pinnedDeploymentCorrupt
              ? "Repair & pin"
              : pinnedVersion
                ? "Update deployment"
                : "Deploy & pin"}
          </Button>
        ) : null}
        <Tooltip>
          <TooltipTrigger asChild>
            <span tabIndex={0}>
              <Button
                size="sm"
                variant="outline"
                disabled={runDisabled}
                onClick={() => void triggerNow()}
              >
                {runBusy ? (
                  <Loader2 data-icon="inline-start" className="animate-spin" />
                ) : (
                  <Play data-icon="inline-start" />
                )}
                {runLabel}
              </Button>
            </span>
          </TooltipTrigger>
          <TooltipContent className="max-w-80">
            {runBlockReason ?? runWindowDescription}
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
}

function ArchivedSection({
  archived,
  canMutate,
  ownershipReason,
  onRestore,
}: {
  archived: EnvSchedule[];
  canMutate: boolean;
  ownershipReason: string;
  onRestore: (schedule: EnvSchedule) => void;
}) {
  return (
    <div>
      <div className="border-b bg-muted/40 px-3 py-1.5 text-[11px] font-semibold uppercase text-muted-foreground">
        Archived
      </div>
      {archived.map((schedule) => (
        <div
          key={envScheduleKey(schedule)}
          className="flex min-h-10 items-center gap-3 border-b px-3 text-xs text-muted-foreground"
        >
          <span className="truncate font-mono">
            {schedule.pipeline_name || schedule.pipeline_uuid}
          </span>
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px]">
            {schedule.environment}
          </span>
          <span className="truncate font-mono">{schedule.cron}</span>
          <span className="truncate">
            {schedule.archived_reason === "missing"
              ? "pipeline file missing (restores automatically when it reappears)"
              : "archived"}
          </span>
          <span className="ml-auto" />
          {schedule.pipeline_id ? (
            <Button
              size="sm"
              variant="ghost"
              disabled={!canMutate}
              title={!canMutate ? ownershipReason : undefined}
              onClick={() => onRestore(schedule)}
            >
              <ArchiveRestore className="size-3.5" />
              Restore
            </Button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function NewEnvScheduleDialog({
  open,
  onOpenChange,
  canMutate,
  ownershipReason,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canMutate: boolean;
  ownershipReason: string;
  onCreate: (
    pipeline: { id: string; uuid?: string; name: string },
    environment: string,
    input: UpsertEnvScheduleInput,
  ) => Promise<void>;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const pipelines = useMemo(() => workspace?.pipelines ?? [], [workspace?.pipelines]);
  const [pipelineId, setPipelineId] = useState("");
  const [environment, setEnvironment] = useState("");
  const [cron, setCron] = useState("0 * * * *");
  const [timezone, setTimezone] = useState("UTC");
  const [catchupPolicy, setCatchupPolicy] = useState<CatchupPolicy>("skip");
  const deployState = usePipelineDeploy(pipelineId || undefined);
  const [sourceMode, setSourceMode] = useState<"existing" | "deploy">("deploy");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setPipelineId(pipelines[0]?.id ?? "");
      setEnvironment(workspace?.selected_environment ?? "");
      setSourceMode("deploy");
      setError(null);
    }
  }, [open, pipelines, workspace?.selected_environment]);

  const submit = async () => {
    if (!canMutate) {
      setError(ownershipReason);
      return;
    }
    const pipeline = pipelines.find((item) => item.id === pipelineId);
    if (!pipeline || !environment.trim() || !cron.trim()) {
      setError(
        "Pipeline, environment, and cron are required — schedules have no implicit default environment.",
      );
      return;
    }
    const existingVersion = deployState.status?.version_id?.trim();
    if (sourceMode === "existing" && (!existingVersion || !deployState.status?.executable)) {
      setError(
        "Choose a valid deployment, or deploy the saved workspace when creating the schedule.",
      );
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onCreate(
        { id: pipeline.id, uuid: pipeline.uuid, name: pipeline.name },
        environment.trim(),
        {
          cron: cron.trim(),
          timezone: timezone.trim() || "UTC",
          catchup_policy: catchupPolicy,
          ...(sourceMode === "existing"
            ? { snapshot_version_id: existingVersion! }
            : { deploy_now: true }),
        },
      );
      onOpenChange(false);
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "Failed to save schedule.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Clock className="size-4 text-primary" />
            New schedule
          </DialogTitle>
          <DialogDescription>
            Schedules are per pipeline and environment, and execute a deployed snapshot.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Pipeline</span>
            <select
              className="h-9 w-full rounded-md border bg-background px-2 text-sm"
              value={pipelineId}
              onChange={(event) => {
                setPipelineId(event.target.value);
                setSourceMode("deploy");
              }}
            >
              {pipelines.map((pipeline) => (
                <option key={pipeline.id} value={pipeline.id}>
                  {pipeline.name || pipeline.path}
                </option>
              ))}
            </select>
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Environment</span>
            <Input
              value={environment}
              onChange={(event) => setEnvironment(event.target.value)}
              placeholder="prod"
            />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-muted-foreground">Cron</span>
              <Input
                className="font-mono"
                value={cron}
                onChange={(event) => setCron(event.target.value)}
                placeholder="0 * * * *"
              />
            </label>
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-muted-foreground">Timezone</span>
              <Input
                value={timezone}
                onChange={(event) => setTimezone(event.target.value)}
                placeholder="UTC"
              />
            </label>
          </div>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Catch-up policy</span>
            <select
              className="h-9 w-full rounded-md border bg-background px-2 text-sm"
              value={catchupPolicy}
              onChange={(event) => setCatchupPolicy(event.target.value as CatchupPolicy)}
            >
              <option value="skip">Skip missed intervals</option>
              <option value="run_once">Run once to catch up</option>
              <option value="backfill">
                Backfill each missed interval (incremental assets only)
              </option>
            </select>
          </label>
          <div className="space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Run source</span>
            <ToggleGroup
              type="single"
              variant="outline"
              spacing={0}
              value={sourceMode}
              onValueChange={(value) => {
                if (value === "existing" || value === "deploy") setSourceMode(value);
              }}
              className="grid w-full grid-cols-2"
            >
              <ToggleGroupItem
                value="existing"
                className="w-full"
                disabled={
                  deployState.loading ||
                  !deployState.status?.has_snapshot ||
                  !deployState.status.executable
                }
              >
                {deployState.loading
                  ? "Checking deployment…"
                  : deployState.status?.has_snapshot && !deployState.status.executable
                    ? "Deployment needs repair"
                    : deployState.status?.version_id
                      ? `Use ${deployState.status.version_id.slice(0, 8)}`
                      : "No deployment yet"}
              </ToggleGroupItem>
              <ToggleGroupItem value="deploy" className="w-full">
                Deploy saved workspace
              </ToggleGroupItem>
            </ToggleGroup>
            <p className="text-[11px] text-muted-foreground">
              {sourceMode === "existing" && deployState.status?.version_id
                ? `The schedule will stay pinned to deployment ${deployState.status.version_id.slice(0, 8)}.`
                : "Renart will deploy the saved workspace and pin the schedule to that exact deployment."}
            </p>
          </div>
          {error ? <p className="text-xs text-red-600">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            Cancel
          </Button>
          <Button onClick={() => void submit()} disabled={submitting || !canMutate}>
            {submitting ? "Saving…" : "Create schedule"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

type TimelineWindow = {
  start: number;
  end: number;
  bucket: (typeof buckets)[number];
  density: TimelineDensity;
};

type TimelineTick = {
  key: string;
  label: string;
  left: number;
};

type TimelineDensity = "compact" | "regular";

function useTimelineTickDensity() {
  const [density, setDensity] = useState<TimelineDensity>("regular");

  useEffect(() => {
    const media = window.matchMedia("(max-width: 1100px)");
    const update = () => setDensity(media.matches ? "compact" : "regular");
    update();
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);

  return density;
}

function bucketHours(bucket: (typeof buckets)[number]) {
  return {
    "1hr": 1,
    "6hr": 6,
    "12hr": 12,
    "24hr": 24,
  }[bucket];
}

function timelineWindow(
  bucket: (typeof buckets)[number],
  density: TimelineDensity,
): TimelineWindow {
  const stepMs = tickStepMs(bucket, density);
  const now = Date.now();
  const bucketMs = bucketHours(bucket) * 60 * 60 * 1000;
  const start = floorTime(now - bucketMs / 4, stepMs);
  const end = floorTime(now, stepMs) + bucketMs;
  return { start, end, bucket, density };
}

function timelineAxis(window: TimelineWindow): TimelineTick[] {
  const stepMs = tickStepMs(window.bucket, window.density);
  const formatter = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
  const ticks: TimelineTick[] = [];
  for (let time = window.start; time <= window.end + 1; time += stepMs) {
    ticks.push({
      key: `${window.bucket}-${time}`,
      label: formatter.format(new Date(time)),
      left: ((time - window.start) / (window.end - window.start)) * 100,
    });
  }
  return ticks;
}

function tickStepMs(bucket: (typeof buckets)[number], density: TimelineDensity) {
  const minute = 60 * 1000;
  const hour = 60 * minute;
  if (density === "compact") {
    return {
      "1hr": 30 * minute,
      "6hr": 2 * hour,
      "12hr": 4 * hour,
      "24hr": 6 * hour,
    }[bucket];
  }
  return {
    "1hr": 15 * minute,
    "6hr": hour,
    "12hr": 2 * hour,
    "24hr": 4 * hour,
  }[bucket];
}

function floorTime(value: number, stepMs: number) {
  return Math.floor(value / stepMs) * stepMs;
}

function TimelineAxis({ axis }: { axis: TimelineTick[] }) {
  return (
    <div className="relative h-full flex-1 border-x">
      {axis.map((tick) => (
        <span
          key={tick.key}
          className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 whitespace-nowrap px-1 text-center"
          style={{ left: `${tick.left}%` }}
        >
          {tick.label}
        </span>
      ))}
    </div>
  );
}

function TimelineGrid({ axis }: { axis: TimelineTick[] }) {
  return (
    <>
      {axis.map((tick) => (
        <span
          key={tick.key}
          className="absolute inset-y-0 w-px bg-border/60"
          style={{ left: `${tick.left}%` }}
        />
      ))}
    </>
  );
}

function NowMarker({ left }: { left: number }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="absolute inset-y-1 z-10 w-px bg-foreground" style={{ left: `${left}%` }}>
          <span className="absolute -top-0.5 left-1/2 size-1.5 -translate-x-1/2 rounded-full bg-foreground" />
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <div className="font-medium">Now</div>
        <div className="font-mono">{formatSchedulerDate(new Date().toISOString())}</div>
      </TooltipContent>
    </Tooltip>
  );
}

function slotClassName(
  kind: "persisted" | "projected",
  enabled: boolean,
  phase: "past" | "future",
) {
  if (!enabled) {
    return kind === "persisted"
      ? "absolute top-2 h-10 rounded-sm bg-muted-foreground/35"
      : "absolute top-3 h-8 rounded-sm border border-muted-foreground/25 bg-muted-foreground/10";
  }
  return kind === "persisted"
    ? "absolute top-2 h-10 rounded-sm bg-primary"
    : phase === "past"
      ? "absolute top-3 h-8 rounded-sm border border-amber-500/45 bg-amber-500/15"
      : "absolute top-3 h-8 rounded-sm border border-primary/40 bg-primary/15";
}

function expectedSlots(schedule: TimelineSchedule, window: TimelineWindow) {
  const now = Date.now();
  const persistedNext = schedule.next_run_at ? new Date(schedule.next_run_at).getTime() : null;
  const normalized = normalizeSchedule(schedule.schedule);
  const parsed = parseStandardCron(normalized);
  const slots: Array<{
    at: string;
    left: number;
    width: number;
    kind: "persisted" | "projected";
    phase: "past" | "future";
  }> = [];
  const addSlot = (time: number, kind: "persisted" | "projected") => {
    const left = timelineLeft(time, window);
    if (left === null) return;
    slots.push({
      at: new Date(time).toISOString(),
      left,
      width: window.bucket === "1hr" ? 2.5 : 1.4,
      kind,
      phase: time < now ? "past" : "future",
    });
  };

  if (persistedNext && Number.isFinite(persistedNext)) {
    addSlot(persistedNext, "persisted");
  }
  if (!parsed) {
    return slots;
  }
  for (let time = floorTime(window.start, 60 * 1000); time <= window.end; time += 60 * 1000) {
    if (!cronMatches(parsed, time, schedule.timezone)) {
      continue;
    }
    if (persistedNext && Math.abs(time - persistedNext) < 60 * 1000) {
      continue;
    }
    addSlot(time, "projected");
  }
  return slots;
}

function timelineLeft(time: number, window: TimelineWindow) {
  if (time < window.start || time > window.end) return null;
  return ((time - window.start) / (window.end - window.start)) * 100;
}

type CronField = {
  values: Set<number>;
  wildcard: boolean;
};

type ParsedCron = {
  minute: CronField;
  hour: CronField;
  dayOfMonth: CronField;
  month: CronField;
  dayOfWeek: CronField;
};

function normalizeSchedule(schedule: string) {
  const normalized = schedule.trim().toLowerCase();
  if (
    !normalized ||
    normalized === "daily" ||
    normalized === "@daily" ||
    normalized === "@midnight"
  )
    return "0 0 * * *";
  if (normalized === "hourly" || normalized === "@hourly") return "0 * * * *";
  if (normalized === "weekly" || normalized === "@weekly") return "0 0 * * 0";
  if (normalized === "monthly" || normalized === "@monthly") return "0 0 1 * *";
  if (
    normalized === "yearly" ||
    normalized === "annually" ||
    normalized === "@yearly" ||
    normalized === "@annually"
  )
    return "0 0 1 1 *";
  return normalized;
}

function parseStandardCron(schedule: string): ParsedCron | null {
  const fields = schedule.trim().split(/\s+/);
  if (fields.length !== 5) {
    return null;
  }
  const [minute, hour, dayOfMonth, month, dayOfWeek] = fields;
  const parsed = {
    minute: parseCronField(minute, 0, 59),
    hour: parseCronField(hour, 0, 23),
    dayOfMonth: parseCronField(dayOfMonth, 1, 31),
    month: parseCronField(month, 1, 12, monthNames),
    dayOfWeek: parseCronField(dayOfWeek, 0, 7, dayNames),
  };
  if (!parsed.minute || !parsed.hour || !parsed.dayOfMonth || !parsed.month || !parsed.dayOfWeek) {
    return null;
  }
  return parsed as ParsedCron;
}

const monthNames: Record<string, number> = {
  jan: 1,
  feb: 2,
  mar: 3,
  apr: 4,
  may: 5,
  jun: 6,
  jul: 7,
  aug: 8,
  sep: 9,
  oct: 10,
  nov: 11,
  dec: 12,
};

const dayNames: Record<string, number> = {
  sun: 0,
  mon: 1,
  tue: 2,
  wed: 3,
  thu: 4,
  fri: 5,
  sat: 6,
};

function parseCronField(
  value: string,
  min: number,
  max: number,
  aliases: Record<string, number> = {},
) {
  const values = new Set<number>();
  let wildcard = false;
  for (const rawPart of value.split(",")) {
    const [rangePartRaw, stepPart] = rawPart.split("/");
    const rangePart = rangePartRaw.trim().toLowerCase();
    const step = stepPart ? Number(stepPart) : 1;
    if (!Number.isInteger(step) || step <= 0) {
      return null;
    }
    const rangeValues = cronRange(rangePart, min, max, aliases);
    if (!rangeValues) {
      return null;
    }
    wildcard ||= rangeValues.wildcard;
    for (let current = rangeValues.start; current <= rangeValues.end; current += step) {
      values.add(current);
      if (max === 7 && current === 7) {
        values.add(0);
      }
    }
  }
  return { values, wildcard };
}

function cronRange(value: string, min: number, max: number, aliases: Record<string, number>) {
  if (value === "*" || value === "?") {
    return { start: min, end: max, wildcard: true };
  }
  const [startRaw, endRaw] = value.split("-");
  const start = cronNumber(startRaw, aliases);
  const end = cronNumber(endRaw ?? startRaw, aliases);
  if (
    !Number.isInteger(start) ||
    !Number.isInteger(end) ||
    start < min ||
    end > max ||
    start > end
  ) {
    return null;
  }
  return { start, end, wildcard: false };
}

function cronNumber(value: string, aliases: Record<string, number>) {
  const normalized = value.trim().toLowerCase();
  return aliases[normalized] ?? Number(normalized);
}

function cronMatches(parsed: ParsedCron, time: number, timezone: string | undefined) {
  const parts = zonedDateParts(new Date(time), timezone);
  if (!parts) {
    return false;
  }
  const dayOfWeekMatches =
    parsed.dayOfWeek.values.has(parts.dayOfWeek) ||
    (parts.dayOfWeek === 0 && parsed.dayOfWeek.values.has(7));
  const dayMatches =
    parsed.dayOfMonth.wildcard || parsed.dayOfWeek.wildcard
      ? parsed.dayOfMonth.values.has(parts.dayOfMonth) && dayOfWeekMatches
      : parsed.dayOfMonth.values.has(parts.dayOfMonth) || dayOfWeekMatches;
  return (
    parsed.minute.values.has(parts.minute) &&
    parsed.hour.values.has(parts.hour) &&
    dayMatches &&
    parsed.month.values.has(parts.month)
  );
}

function zonedDateParts(date: Date, timezone: string | undefined) {
  try {
    const formatter = new Intl.DateTimeFormat("en-US", {
      timeZone: timezone || "UTC",
      year: "numeric",
      month: "numeric",
      day: "numeric",
      hour: "numeric",
      minute: "numeric",
      hour12: false,
    });
    const values = Object.fromEntries(
      formatter.formatToParts(date).map((part) => [part.type, part.value]),
    );
    const year = Number(values.year);
    const month = Number(values.month);
    const dayOfMonth = Number(values.day);
    const hour = Number(values.hour) % 24;
    const minute = Number(values.minute);
    if (![year, month, dayOfMonth, hour, minute].every(Number.isFinite)) {
      return null;
    }
    return {
      month,
      dayOfMonth,
      dayOfWeek: new Date(Date.UTC(year, month - 1, dayOfMonth)).getUTCDay(),
      hour,
      minute,
    };
  } catch {
    return null;
  }
}
