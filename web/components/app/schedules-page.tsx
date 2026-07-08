import { useAtomValue } from "jotai";
import { ArchiveRestore, Clock, Loader2, Package, Play, Plus, Search } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
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
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { envScheduleKey, useEnvSchedules } from "@/hooks/use-env-schedules";
import { formatSchedulerDate, usePipelineScheduler } from "@/hooks/use-pipeline-scheduler";
import { triggerPipelineRun } from "@/lib/api";
import type { CatchupPolicy, EnvSchedule } from "@/lib/api-env-schedules";
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
  const { runs } = usePipelineScheduler();
  const envSchedules = useEnvSchedules();
  const [query, setQuery] = useState("");
  const [bucket, setBucket] = useState<(typeof buckets)[number]>("12hr");
  const [newScheduleOpen, setNewScheduleOpen] = useState(false);
  const tickDensity = useTimelineTickDensity();
  const window = timelineWindow(bucket, tickDensity);
  const axis = timelineAxis(window);
  const filteredSchedules = envSchedules.schedules.filter((schedule) => {
    const value = query.trim().toLowerCase();
    return !value ||
      (schedule.pipeline_name ?? "").toLowerCase().includes(value) ||
      schedule.environment.toLowerCase().includes(value) ||
      schedule.cron.toLowerCase().includes(value);
  });

  return (
    <AppPage>
      <PageHeader
        title="Schedules"
        subtitle="One schedule per pipeline and environment; scheduled runs execute the pinned deployed snapshot"
        actions={envSchedules.loading ? <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />Loading</span> : null}
      />
      <div className="flex items-center gap-2 px-3 pb-2">
        <div className="relative min-w-0 flex-1 md:max-w-sm">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-8" placeholder="Filter jobs..." value={query} onChange={(event) => setQuery(event.target.value)} />
        </div>
        <Button variant="outline" size="sm" onClick={() => setNewScheduleOpen(true)}><Plus className="size-3.5" />New schedule</Button>
        <div className="ml-auto hidden overflow-hidden rounded-lg border md:flex">
          {buckets.map((item) => <Button key={item} variant={bucket === item ? "default" : "ghost"} size="sm" className="rounded-none" onClick={() => setBucket(item)}>{item}</Button>)}
        </div>
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <AppPanel className="h-full overflow-auto">
          <TooltipProvider>
          <div className="min-w-[980px]">
            <div className="sticky top-0 z-10 flex h-9 items-center border-b bg-card text-[11px] font-semibold uppercase text-muted-foreground">
              <div className="w-80 px-3">Jobs</div>
              <TimelineAxis axis={axis} />
              <div className="w-56 px-3 text-right">Controls</div>
            </div>
            {envSchedules.loading && filteredSchedules.length === 0 ? (
              <div className="flex h-24 items-center gap-2 px-3 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />Loading schedules...</div>
            ) : null}
            {!envSchedules.loading && filteredSchedules.length === 0 ? (
              <div className="px-3 py-8 text-sm text-muted-foreground">No schedules yet. Use “New schedule” to run a pipeline in an environment.</div>
            ) : null}
            {filteredSchedules.map((schedule) => (
              <EnvScheduleRow
                key={envScheduleKey(schedule)}
                schedule={schedule}
                window={window}
                axis={axis}
                busy={envSchedules.busyKey === envScheduleKey(schedule)}
                activeRun={runs.find((run) => run.pipeline_id === schedule.pipeline_id && run.environment === schedule.environment && (run.status === "queued" || run.status === "running"))}
                onSetStatus={(status) => void envSchedules.setStatus(schedule, status)}
                onArchive={() => void envSchedules.archive(schedule)}
              />
            ))}
            {envSchedules.archived.length > 0 ? (
              <ArchivedSection archived={envSchedules.archived} onRestore={(schedule) => void envSchedules.setStatus(schedule, "active")} />
            ) : null}
          </div>
          </TooltipProvider>
        </AppPanel>
      </div>
      <NewEnvScheduleDialog
        open={newScheduleOpen}
        onOpenChange={setNewScheduleOpen}
        onCreate={async (pipeline, environment, input) => {
          await envSchedules.upsert({ pipeline_uuid: pipeline.uuid ?? "", environment, pipeline_id: pipeline.id }, input);
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
  activeRun,
  onSetStatus,
  onArchive,
}: {
  schedule: EnvSchedule;
  window: TimelineWindow;
  axis: TimelineTick[];
  busy: boolean;
  activeRun?: PipelineRun;
  onSetStatus: (status: "active" | "paused") => void;
  onArchive: () => void;
}) {
  const enabled = schedule.status === "active";
  const timeline: TimelineSchedule = {
    schedule: schedule.cron,
    timezone: schedule.timezone,
    enabled,
    next_run_at: schedule.next_run_at,
  };
  const slots = expectedSlots(timeline, window);
  const [triggering, setTriggering] = useState(false);
  const runPending = busy || triggering || Boolean(activeRun);
  const runLabel = activeRun?.status === "running" ? "Running" : activeRun?.status === "queued" ? "Queued" : "Run";
  const nowLeft = timelineLeft(Date.now(), window);
  const triggerNow = async () => {
    if (!schedule.pipeline_id) return;
    setTriggering(true);
    try {
      await triggerPipelineRun(schedule.pipeline_id, { environment: schedule.environment, trigger: "manual" });
    } finally {
      setTriggering(false);
    }
  };
  return (
    <div className="flex min-h-14 items-center border-b hover:bg-muted/40">
      <div className="flex w-80 min-w-0 items-center gap-3 px-3">
        <Switch checked={enabled} disabled={busy} onCheckedChange={(next) => onSetStatus(next ? "active" : "paused")} />
        <div className="min-w-0">
          <div className="flex items-center gap-2 font-mono text-xs text-primary">
            <Clock className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{schedule.pipeline_name || schedule.pipeline_uuid}</span>
            <span className="rounded-full bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-primary">{schedule.environment}</span>
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            <span className="truncate font-mono">{schedule.cron}</span>
            <span>·</span>
            <span className="truncate">{schedule.timezone || "UTC"}</span>
            {schedule.last_run ? (
              <>
                <span>·</span>
                <span className="truncate">last {schedule.last_run.status} {formatSchedulerDate(schedule.last_run.finished_at ?? schedule.last_run.started_at)}</span>
              </>
            ) : null}
            {schedule.snapshot_version_id ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="inline-flex items-center gap-0.5 truncate"><Package className="size-3" />{schedule.snapshot_version_id.slice(0, 8)}</span>
                </TooltipTrigger>
                <TooltipContent>Pinned deployed snapshot {schedule.snapshot_version_id}</TooltipContent>
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
              />
            </TooltipTrigger>
            <TooltipContent>
              <div className="font-medium">{slot.kind === "persisted" ? "Next scheduled run" : slot.phase === "past" ? "Past expected run" : "Expected run"}</div>
              <div className="font-mono">{formatSchedulerDate(slot.at)}</div>
              {slot.kind === "projected" ? <div className="text-background/70">Projected from the schedule</div> : null}
            </TooltipContent>
          </Tooltip>
        ))}
        {nowLeft !== null ? <NowMarker left={nowLeft} /> : null}
      </div>
      <div className="flex w-56 items-center justify-end gap-2 px-3">
        <span className="text-[10px] uppercase text-muted-foreground">{schedule.catchup_policy.replace("_", " ")}</span>
        <Button size="sm" variant="ghost" disabled={busy} title="Archive schedule (run history is kept)" onClick={onArchive}><ArchiveRestore className="size-3.5" /></Button>
        <Button size="sm" variant="outline" disabled={runPending} onClick={() => void triggerNow()}>{runPending ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}{runLabel}</Button>
      </div>
    </div>
  );
}

function ArchivedSection({ archived, onRestore }: { archived: EnvSchedule[]; onRestore: (schedule: EnvSchedule) => void }) {
  return (
    <div>
      <div className="border-b bg-muted/40 px-3 py-1.5 text-[11px] font-semibold uppercase text-muted-foreground">Archived</div>
      {archived.map((schedule) => (
        <div key={envScheduleKey(schedule)} className="flex min-h-10 items-center gap-3 border-b px-3 text-xs text-muted-foreground">
          <span className="truncate font-mono">{schedule.pipeline_name || schedule.pipeline_uuid}</span>
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-[10px]">{schedule.environment}</span>
          <span className="truncate font-mono">{schedule.cron}</span>
          <span className="truncate">{schedule.archived_reason === "missing" ? "pipeline file missing (restores automatically when it reappears)" : "archived"}</span>
          <span className="ml-auto" />
          {schedule.pipeline_id ? (
            <Button size="sm" variant="ghost" onClick={() => onRestore(schedule)}><ArchiveRestore className="size-3.5" />Restore</Button>
          ) : null}
        </div>
      ))}
    </div>
  );
}

function NewEnvScheduleDialog({
  open,
  onOpenChange,
  onCreate,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (pipeline: { id: string; uuid?: string; name: string }, environment: string, input: { cron: string; timezone: string; catchup_policy: CatchupPolicy; deploy_now: boolean }) => Promise<void>;
}) {
  const workspace = useAtomValue(workspaceAtom);
  const pipelines = useMemo(() => workspace?.pipelines ?? [], [workspace?.pipelines]);
  const [pipelineId, setPipelineId] = useState("");
  const [environment, setEnvironment] = useState("");
  const [cron, setCron] = useState("0 * * * *");
  const [timezone, setTimezone] = useState("UTC");
  const [catchupPolicy, setCatchupPolicy] = useState<CatchupPolicy>("skip");
  const [deployNow, setDeployNow] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) {
      setPipelineId(pipelines[0]?.id ?? "");
      setEnvironment(workspace?.selected_environment ?? "");
      setError(null);
    }
  }, [open, pipelines, workspace?.selected_environment]);

  const submit = async () => {
    const pipeline = pipelines.find((item) => item.id === pipelineId);
    if (!pipeline || !environment.trim() || !cron.trim()) {
      setError("Pipeline, environment, and cron are required — schedules have no implicit default environment.");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onCreate(
        { id: pipeline.id, uuid: pipeline.uuid, name: pipeline.name },
        environment.trim(),
        { cron: cron.trim(), timezone: timezone.trim() || "UTC", catchup_policy: catchupPolicy, deploy_now: deployNow }
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
          <DialogTitle className="flex items-center gap-2"><Clock className="size-4 text-primary" />New schedule</DialogTitle>
          <DialogDescription>Schedules are per pipeline and environment, and execute a deployed snapshot.</DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Pipeline</span>
            <select
              className="h-9 w-full rounded-md border bg-background px-2 text-sm"
              value={pipelineId}
              onChange={(event) => setPipelineId(event.target.value)}
            >
              {pipelines.map((pipeline) => <option key={pipeline.id} value={pipeline.id}>{pipeline.name || pipeline.path}</option>)}
            </select>
          </label>
          <label className="block space-y-1.5">
            <span className="text-xs font-medium text-muted-foreground">Environment</span>
            <Input value={environment} onChange={(event) => setEnvironment(event.target.value)} placeholder="prod" />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-muted-foreground">Cron</span>
              <Input className="font-mono" value={cron} onChange={(event) => setCron(event.target.value)} placeholder="0 * * * *" />
            </label>
            <label className="block space-y-1.5">
              <span className="text-xs font-medium text-muted-foreground">Timezone</span>
              <Input value={timezone} onChange={(event) => setTimezone(event.target.value)} placeholder="UTC" />
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
              <option value="backfill">Backfill each missed interval (incremental assets only)</option>
            </select>
          </label>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <Switch checked={deployNow} onCheckedChange={setDeployNow} />
            Deploy the working tree now and pin this schedule to it
          </label>
          {error ? <p className="text-xs text-red-600">{error}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>Cancel</Button>
          <Button onClick={() => void submit()} disabled={submitting}>{submitting ? "Saving…" : "Create schedule"}</Button>
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

function timelineWindow(bucket: (typeof buckets)[number], density: TimelineDensity): TimelineWindow {
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
        <span key={tick.key} className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 whitespace-nowrap px-1 text-center" style={{ left: `${tick.left}%` }}>{tick.label}</span>
      ))}
    </div>
  );
}

function TimelineGrid({ axis }: { axis: TimelineTick[] }) {
  return <>{axis.map((tick) => <span key={tick.key} className="absolute inset-y-0 w-px bg-border/60" style={{ left: `${tick.left}%` }} />)}</>;
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

function slotClassName(kind: "persisted" | "projected", enabled: boolean, phase: "past" | "future") {
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
  const slots: Array<{ at: string; left: number; width: number; kind: "persisted" | "projected"; phase: "past" | "future" }> = [];
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
  if (!normalized || normalized === "daily" || normalized === "@daily" || normalized === "@midnight") return "0 0 * * *";
  if (normalized === "hourly" || normalized === "@hourly") return "0 * * * *";
  if (normalized === "weekly" || normalized === "@weekly") return "0 0 * * 0";
  if (normalized === "monthly" || normalized === "@monthly") return "0 0 1 * *";
  if (normalized === "yearly" || normalized === "annually" || normalized === "@yearly" || normalized === "@annually") return "0 0 1 1 *";
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

function parseCronField(value: string, min: number, max: number, aliases: Record<string, number> = {}) {
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
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < min || end > max || start > end) {
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
  const dayOfWeekMatches = parsed.dayOfWeek.values.has(parts.dayOfWeek) || (parts.dayOfWeek === 0 && parsed.dayOfWeek.values.has(7));
  const dayMatches = parsed.dayOfMonth.wildcard || parsed.dayOfWeek.wildcard
    ? parsed.dayOfMonth.values.has(parts.dayOfMonth) && dayOfWeekMatches
    : parsed.dayOfMonth.values.has(parts.dayOfMonth) || dayOfWeekMatches;
  return parsed.minute.values.has(parts.minute) &&
    parsed.hour.values.has(parts.hour) &&
    dayMatches &&
    parsed.month.values.has(parts.month);
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
    const values = Object.fromEntries(formatter.formatToParts(date).map((part) => [part.type, part.value]));
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
