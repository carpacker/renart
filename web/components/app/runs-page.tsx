import { Link, useNavigate } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import {
  ArrowLeft,
  AlertTriangle,
  ChevronLeft,
  ChevronRight,
  Loader2,
  Play,
  RotateCw,
  Search,
  Terminal,
  X,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { AnsiOutput } from "@/components/ansi-output";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatSchedulerDate, usePipelineScheduler } from "@/hooks/use-pipeline-scheduler";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import { activePipelineRunConflict, type PipelineRunSource } from "@/lib/api-scheduler";
import type { PipelineRun, PipelineRunLogLine, PipelineRunStep } from "@/lib/types";
import { cn } from "@/lib/utils";
import { awaitWorkspaceSaves } from "@/lib/workspace-save-barrier";

import { PageHeader, AppPage, AppPanel, SimpleTable, StatusPill } from "./app-primitives";

const runTabsTriggerClass = "flex-none";
const runStatuses = ["all", "queued", "running", "success", "failed", "cancelled"] as const;
const pageSize = 8;

export type AppRunsSearch = {
  q?: string;
  status?: (typeof runStatuses)[number];
  page?: number;
};

export function normalizeAppRunsSearch(search: Record<string, unknown>): AppRunsSearch {
  const rawPage =
    typeof search.page === "number"
      ? search.page
      : typeof search.page === "string"
        ? Number(search.page)
        : undefined;
  const page = rawPage && Number.isFinite(rawPage) && rawPage > 0 ? Math.floor(rawPage) : undefined;
  return {
    q: typeof search.q === "string" && search.q.trim() ? search.q : undefined,
    status: runStatuses.includes(search.status as never)
      ? (search.status as AppRunsSearch["status"])
      : undefined,
    page,
  };
}

export function AppRunsPage({
  search = {},
  onSearchChange,
}: {
  search?: AppRunsSearch;
  onSearchChange?: (search: AppRunsSearch) => void;
}) {
  const q = search.q ?? "";
  const status = search.status ?? "all";
  const requestedPage = search.page ?? 1;
  const runsQuery = useMemo(
    () => ({
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
      q: q.trim() || undefined,
      status: status === "all" ? undefined : status,
    }),
    [q, requestedPage, status],
  );
  const { runs, loading, runsTotal, runsOffset, runsError, refreshRuns } = usePipelineScheduler({
    runsQuery,
  });
  const pages = Math.max(1, Math.ceil(runsTotal / pageSize));
  const page = Math.min(requestedPage, pages);
  const visibleRuns = runs;
  const updateSearch = (next: AppRunsSearch) => onSearchChange?.({ ...search, ...next });

  useEffect(() => {
    if (requestedPage > pages) {
      updateSearch({ page: pages });
    }
  }, [pages, requestedPage]);

  return (
    <AppPage>
      <PageHeader
        title="Runs"
        subtitle="Local pipeline run history from .renart/state.db"
        actions={
          <div className="flex min-w-0 items-center gap-2">
            <div className="relative h-8 w-56 shrink-0 rounded-md border bg-background sm:w-72">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={q}
                onChange={(event) => updateSearch({ q: event.target.value || undefined, page: 1 })}
                placeholder="Search runs..."
                className="h-full border-0 bg-transparent pl-8 pr-14 text-xs shadow-none focus-visible:ring-0"
              />
              {loading ? (
                <Loader2
                  aria-label="Loading runs"
                  className="pointer-events-none absolute right-8 top-1/2 size-3.5 -translate-y-1/2 animate-spin text-muted-foreground"
                />
              ) : null}
              {q ? (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="absolute right-1 top-1/2 -translate-y-1/2"
                  onClick={() => updateSearch({ q: undefined, page: 1 })}
                >
                  <X className="size-3.5" />
                </Button>
              ) : null}
            </div>
          </div>
        }
      />
      {runsError ? (
        <div className="px-3 pb-2">
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Runs could not be refreshed</AlertTitle>
            <AlertDescription className="flex items-center justify-between gap-3">
              <span>{runsError} The last successfully loaded rows remain visible.</span>
              <Button variant="outline" size="xs" onClick={() => void refreshRuns()}>
                <RotateCw />
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-1.5 px-3 pb-2">
        {runStatuses.map((item) => (
          <Button
            key={item}
            variant={status === item ? "secondary" : "outline"}
            size="xs"
            className="capitalize"
            onClick={() => updateSearch({ status: item === "all" ? undefined : item, page: 1 })}
          >
            {item}
          </Button>
        ))}
      </div>
      <div className="min-h-0 flex-1 px-3 pb-3">
        <AppPanel className="flex h-full min-h-0 flex-col overflow-hidden">
          <SimpleTable
            columns={[
              "Status",
              "Run ID",
              "Pipeline",
              "Environment",
              "Trigger",
              "Started",
              "Duration",
              "",
            ]}
            rows={visibleRuns.map((run) => [
              <StatusPill key="status" status={run.status} />,
              <Link
                key="id"
                to="/runs/$runId"
                params={{ runId: run.id }}
                search={search}
                className="font-mono text-primary hover:underline"
              >
                {run.id}
              </Link>,
              <span key="pipeline" className="font-mono">
                {run.pipeline}
              </span>,
              run.environment || "default",
              <span key="trigger" className="capitalize">
                {run.trigger}
              </span>,
              formatSchedulerDate(run.started_at),
              formatRunDuration(run),
              <Button key="open" asChild variant="ghost" size="icon-sm">
                <Link to="/runs/$runId" params={{ runId: run.id }} search={search}>
                  <ChevronRight className="size-4" />
                </Link>
              </Button>,
            ])}
          />
          <div className="flex h-11 items-center gap-3 border-t px-3 text-xs text-muted-foreground">
            <span>
              {runsTotal === 0
                ? "0 runs"
                : `${runsOffset + 1}-${runsOffset + visibleRuns.length} of ${runsTotal}`}
            </span>
            <div className="flex-1" />
            <Button
              variant="outline"
              size="xs"
              disabled={page <= 1}
              onClick={() => updateSearch({ page: page - 1 })}
            >
              <ChevronLeft className="size-3" />
              Prev
            </Button>
            <span className="font-mono">
              {page} / {pages}
            </span>
            <Button
              variant="outline"
              size="xs"
              disabled={page >= pages}
              onClick={() => updateSearch({ page: page + 1 })}
            >
              Next
              <ChevronRight className="size-3" />
            </Button>
          </div>
        </AppPanel>
      </div>
    </AppPage>
  );
}

export function AppRunDetailPage({
  runId,
  search = {},
}: {
  runId: string;
  search?: AppRunsSearch;
}) {
  const navigate = useNavigate();
  const workspace = useAtomValue(workspaceAtom);
  const {
    selectedRun,
    logs,
    steps,
    loadingRunId,
    busyPipeline,
    runDetailError,
    selectRun,
    triggerPipeline,
  } = usePipelineScheduler({
    selectedRunId: runId,
  });
  const run = selectedRun;
  const [rerunError, setRerunError] = useState<{
    message: string;
    linkedRunId?: string;
    title?: string;
    linkLabel?: string;
  } | null>(null);
  const output = useMemo(() => combineRunOutput(logs, run?.error), [logs, run?.error]);
  const assetIdsByName = useMemo(() => {
    const pipeline = workspace?.pipelines.find((candidate) => candidate.id === run?.pipeline_id);
    return new Map(pipeline?.assets.map((asset) => [asset.name, asset.id]) ?? []);
  }, [run?.pipeline_id, workspace?.pipelines]);
  const runAgain = async () => {
    if (!run) return;
    const executionContextResolved = run.execution_context_resolved === true;
    const hasCompleteRecordedWindow = Boolean(run.win_start && run.win_end);
    if (executionContextResolved && !hasCompleteRecordedWindow) {
      setRerunError({
        message:
          "This run's resolved execution context has no complete window and cannot be reused safely.",
      });
      return;
    }
    const source: PipelineRunSource = run.snapshot_version_id
      ? { source: "snapshot", snapshot_version_id: run.snapshot_version_id }
      : { source: "working_tree" };
    setRerunError(null);
    let acceptedRunId: string;
    try {
      if (source.source === "working_tree") {
        await awaitWorkspaceSaves();
      }
      const response = await triggerPipeline(run.pipeline_id, {
        ...source,
        ...(executionContextResolved && run.win_start && run.win_end
          ? {
              environment: run.environment,
              start: run.win_start,
              end: run.win_end,
            }
          : {}),
      });
      if (response.status !== "ok" || !response.run?.id) {
        throw new Error("The rerun was not accepted.");
      }
      acceptedRunId = response.run.id;
    } catch (cause) {
      const conflict = activePipelineRunConflict(cause);
      setRerunError({
        message: conflict
          ? "A run is already queued or running for this pipeline."
          : cause instanceof Error
            ? cause.message
            : "Failed to queue the run.",
        linkedRunId: conflict?.activeRunId,
        title: conflict ? "Pipeline already running" : undefined,
        linkLabel: conflict ? "Open active run" : undefined,
      });
      return;
    }
    try {
      await navigate({
        to: "/runs/$runId",
        params: { runId: acceptedRunId },
        search,
      });
    } catch (cause) {
      setRerunError({
        message: `Run ${acceptedRunId} was queued, but its details could not be opened${cause instanceof Error && cause.message ? `: ${cause.message}` : "."}`,
        linkedRunId: acceptedRunId,
        title: "Rerun queued",
        linkLabel: "Open queued run",
      });
    }
  };

  if (!run) {
    if (runDetailError) {
      return (
        <AppPage>
          <PageHeader title={`Run ${runId}`} subtitle="Run details could not be loaded" />
          <div className="px-3 pb-3">
            <Alert variant="destructive">
              <AlertTriangle />
              <AlertTitle>Run details unavailable</AlertTitle>
              <AlertDescription className="flex items-center justify-between gap-3">
                <span>{runDetailError}</span>
                <Button variant="outline" size="sm" onClick={() => void selectRun(runId)}>
                  <RotateCw />
                  Retry
                </Button>
              </AlertDescription>
            </Alert>
          </div>
        </AppPage>
      );
    }
    return (
      <AppPage>
        <PageHeader
          title="Run"
          subtitle="Loading run details"
          actions={<Loader2 className="size-4 animate-spin text-muted-foreground" />}
        />
      </AppPage>
    );
  }

  const sourceLabel = run.snapshot_version_id
    ? `deployment ${run.snapshot_version_id.slice(0, 8)}`
    : "saved workspace";
  const rerunSourceLabel = run.snapshot_version_id
    ? `deployment ${run.snapshot_version_id.slice(0, 8)}`
    : "current saved workspace";
  const executionContextResolved = run.execution_context_resolved === true;
  const rerunEnvironmentLabel = executionContextResolved
    ? run.environment || "default"
    : "current default resolved at start";
  const rerunButtonLabel = run.snapshot_version_id
    ? `Run deployment ${run.snapshot_version_id.slice(0, 8)} with defaults`
    : "Run current workspace with defaults";
  const compactRerunButtonLabel = "Run with defaults";
  const hasRecordedWindow = executionContextResolved && Boolean(run.win_start && run.win_end);
  const hasIncompleteRecordedWindow = executionContextResolved && !hasRecordedWindow;
  const rerunWindowLabel = hasRecordedWindow
    ? `${formatSchedulerDate(run.win_start)} → ${formatSchedulerDate(run.win_end)}`
    : hasIncompleteRecordedWindow
      ? "resolved context is incomplete; rerun unavailable"
      : "current pipeline default resolved at start";
  const rerunDescription = `Source: ${run.snapshot_version_id ? `deployment ${run.snapshot_version_id}` : "current saved workspace"}. ${executionContextResolved ? `Recorded environment: ${rerunEnvironmentLabel}. Recorded window: ${rerunWindowLabel}.` : "The original effective environment and window are unavailable; current defaults are resolved when the rerun starts."} Default execution mode is used; full-refresh, backfill, sensor mode, variables, selection, authorization, and schedule-only context are not replayed.`;
  const rerunUnavailable = hasIncompleteRecordedWindow;
  const runEnvironmentLabel = executionContextResolved
    ? run.environment || "default"
    : "execution context unavailable";

  return (
    <AppPage>
      <PageHeader
        title={`Run ${run.id}`}
        subtitle={`Run of ${run.pipeline} · ${runEnvironmentLabel} · ${sourceLabel} · ${steps.length || "unknown"} assets · ${formatRunDuration(run)}`}
        actions={
          <div className="flex items-center gap-2">
            <Button asChild variant="ghost" size="icon-sm">
              <Link to="/runs" search={search}>
                <ArrowLeft className="size-4" />
              </Link>
            </Button>
            <StatusPill status={run.status} />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  size="sm"
                  onClick={() => void runAgain()}
                  disabled={busyPipeline === run.pipeline_id || rerunUnavailable}
                  aria-busy={busyPipeline === run.pipeline_id}
                  aria-label={rerunButtonLabel}
                  aria-describedby="run-again-context"
                >
                  {busyPipeline === run.pipeline_id ? (
                    <Loader2 data-icon="inline-start" className="animate-spin" />
                  ) : (
                    <RotateCw data-icon="inline-start" />
                  )}
                  <span className="hidden xl:inline">{rerunButtonLabel}</span>
                  <span className="xl:hidden">{compactRerunButtonLabel}</span>
                </Button>
              </TooltipTrigger>
              <TooltipContent className="max-w-sm">{rerunDescription}</TooltipContent>
            </Tooltip>
          </div>
        }
      />
      <div
        id="run-again-context"
        className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 pb-2 text-xs text-muted-foreground"
        data-testid="run-again-context"
      >
        <span>
          Rerun source <span className="font-medium text-foreground">{rerunSourceLabel}</span>
        </span>
        <span aria-hidden="true">·</span>
        <span>
          Environment <span className="font-medium text-foreground">{rerunEnvironmentLabel}</span>
        </span>
        <span aria-hidden="true">·</span>
        <span>{hasRecordedWindow ? `Recorded window ${rerunWindowLabel}` : rerunWindowLabel}</span>
        <span aria-hidden="true">·</span>
        <span>
          Mode <span className="font-medium text-foreground">default execution</span>
        </span>
      </div>
      {rerunError ? (
        <div className="px-3 pb-2">
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>{rerunError.title ?? "Could not start rerun"}</AlertTitle>
            <AlertDescription className="flex flex-wrap items-center gap-2">
              <span>{rerunError.message}</span>
              {rerunError.linkedRunId ? (
                <Button asChild variant="outline" size="xs">
                  <Link
                    to="/runs/$runId"
                    params={{ runId: rerunError.linkedRunId }}
                    search={search}
                  >
                    {rerunError.linkLabel ?? "Open run"}
                  </Link>
                </Button>
              ) : null}
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      {runDetailError ? (
        <div className="px-3 pb-2">
          <Alert variant="destructive">
            <AlertTriangle />
            <AlertTitle>Run details could not be refreshed</AlertTitle>
            <AlertDescription className="flex items-center justify-between gap-3">
              <span>{runDetailError} Showing the last successfully loaded details.</span>
              <Button variant="outline" size="xs" onClick={() => void selectRun(runId)}>
                <RotateCw />
                Retry
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      ) : null}
      <div className="flex min-h-0 flex-1 flex-col gap-3 px-3 pb-3">
        <RunTimelinePanel run={run} steps={steps} />
        <AppPanel className="min-h-0 flex-1 overflow-hidden">
          <Tabs
            defaultValue="events"
            className="flex h-full min-h-0 flex-col gap-0 overflow-hidden"
          >
            <div className="border-b px-2 py-1">
              <ScrollArea
                className="min-w-0"
                horizontalScrollBarClassName="hidden"
                viewportClassName="w-full"
              >
                <TabsList className="w-max max-w-none">
                  <TabsTrigger value="events" className={runTabsTriggerClass}>
                    <Play />
                    Events
                  </TabsTrigger>
                  <TabsTrigger value="output" className={runTabsTriggerClass}>
                    <Terminal />
                    Output
                  </TabsTrigger>
                </TabsList>
              </ScrollArea>
            </div>
            <TabsContent
              value="events"
              className="m-0 min-h-0 flex-1 overflow-hidden data-[state=inactive]:hidden"
            >
              <RunEventsTable
                run={run}
                steps={steps}
                loading={loadingRunId === run.id}
                assetIdsByName={assetIdsByName}
              />
            </TabsContent>
            <TabsContent
              value="output"
              className="m-0 min-h-0 flex-1 overflow-hidden bg-zinc-950 data-[state=inactive]:hidden"
            >
              <RunTerminalOutput output={output} />
            </TabsContent>
          </Tabs>
        </AppPanel>
      </div>
    </AppPage>
  );
}

function RunTimelinePanel({ run, steps }: { run: PipelineRun; steps: PipelineRunStep[] }) {
  const now = useNow(run.status === "running");
  const bounds = timelineBounds(run, steps, now);
  const counts = countSteps(steps);
  return (
    <AppPanel className="grid shrink-0 grid-cols-1 overflow-hidden lg:grid-cols-[minmax(0,1fr)_18rem]">
      <div className="min-w-0 p-3">
        <div className="grid grid-cols-[minmax(7rem,12rem)_minmax(0,1fr)] items-center gap-x-3 gap-y-2">
          <div aria-hidden="true" />
          <div className="flex text-[11px] text-muted-foreground">
            {timelineTicks(bounds).map((tick) => (
              <div key={tick.label} className="min-w-0 flex-1 font-mono">
                {tick.label}
              </div>
            ))}
          </div>
          {steps.length === 0 ? (
            <div className="col-span-2 rounded-md border border-dashed p-6 text-center text-xs text-muted-foreground">
              Asset timings will appear here for direct backend runs.
            </div>
          ) : null}
          {steps.map((step) => (
            <StepBar key={`${step.run_id}-${step.asset}`} step={step} bounds={bounds} now={now} />
          ))}
        </div>
      </div>
      <div className="border-t p-2 lg:border-l lg:border-t-0">
        {[
          ["Preparing", counts.queued],
          ["Executing", counts.running],
          ["Errored", counts.failed],
          ["Succeeded", counts.success],
          ["Cancelled", counts.cancelled],
        ].map(([label, count]) => (
          <div
            key={label}
            className="flex h-9 items-center justify-between rounded-md px-2 text-xs hover:bg-muted/60"
          >
            <span className="font-medium">{label}</span>
            <span className="font-mono text-muted-foreground">{count}</span>
          </div>
        ))}
      </div>
    </AppPanel>
  );
}

function StepBar({
  step,
  bounds,
  now,
}: {
  step: PipelineRunStep;
  bounds: { start: number; end: number };
  now: number;
}) {
  const start = new Date(step.started_at ?? step.finished_at ?? bounds.start).getTime();
  const end = step.finished_at ? new Date(step.finished_at).getTime() : now;
  const rawLeft = ((start - bounds.start) / (bounds.end - bounds.start)) * 100;
  const width = Math.min(
    100,
    Math.max(1.2, ((Math.max(end, start + 1) - start) / (bounds.end - bounds.start)) * 100),
  );
  const left = Math.min(Math.max(0, rawLeft), 100 - width);
  const duration = formatDurationMs(Math.max(0, end - start));
  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            className="min-w-0 break-words font-mono text-[11px] leading-4"
            data-testid="run-timeline-asset-label"
          >
            {step.asset}
          </span>
        </TooltipTrigger>
        <TooltipContent>{step.asset}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className="relative h-7 rounded bg-muted/40"
            data-testid="run-timeline-track"
            data-asset={step.asset}
          >
            <div
              className={cn(
                "absolute top-0.5 h-6 min-w-px rounded",
                step.status === "failed"
                  ? "bg-destructive"
                  : step.status === "running"
                    ? "bg-primary/60"
                    : step.status === "success"
                      ? "bg-primary"
                      : "bg-muted-foreground/45",
              )}
              data-testid="run-timeline-bar"
              data-status={step.status}
              style={{ left: `${left}%`, width: `${width}%` }}
            />
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <span className="font-mono">{step.asset}</span>
          <span className="ml-1 capitalize">
            · {step.status} · {duration}
          </span>
        </TooltipContent>
      </Tooltip>
    </>
  );
}

function RunEventsTable({
  run,
  steps,
  loading,
  assetIdsByName,
}: {
  run: PipelineRun;
  steps: PipelineRunStep[];
  loading: boolean;
  assetIdsByName: Map<string, string>;
}) {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const events = runEvents(run, steps);
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [events.length, loading]);
  return (
    <ScrollArea className="h-full min-h-0" viewportClassName="h-full" viewportRef={viewportRef}>
      <Table>
        <TableHeader className="sticky top-0 z-10 bg-card">
          <TableRow>
            <TableHead className="h-8 w-40 text-xs uppercase text-muted-foreground">
              Timestamp
            </TableHead>
            <TableHead className="h-8 w-44 text-xs uppercase text-muted-foreground">
              Asset
            </TableHead>
            <TableHead className="h-8 w-28 text-xs uppercase text-muted-foreground">Type</TableHead>
            <TableHead className="h-8 text-xs uppercase text-muted-foreground">Info</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.map((event, index) => {
            const assetId = assetIdsByName.get(event.asset);
            const badge = runEventBadge(event.type);
            return (
              <TableRow key={`${event.at}-${event.asset}-${event.type}-${index}`}>
                <TableCell className="h-8 py-1.5 font-mono text-xs text-muted-foreground">
                  {formatSchedulerDate(event.at)}
                </TableCell>
                <TableCell className="h-8 py-1.5 font-mono text-xs">
                  {assetId ? (
                    <Link
                      to="/pipelines/$pipelineId/assets/$assetId/split"
                      params={{ pipelineId: run.pipeline_id, assetId }}
                      className="text-primary hover:underline"
                    >
                      {event.asset}
                    </Link>
                  ) : (
                    event.asset
                  )}
                </TableCell>
                <TableCell className="h-8 py-1.5">
                  <Badge
                    variant={badge.variant}
                    size="xs"
                    className="font-mono uppercase"
                    data-event-type={event.type}
                    data-event-tone={badge.tone}
                  >
                    {event.type}
                  </Badge>
                </TableCell>
                <TableCell className="h-8 py-1.5 text-xs">{event.info}</TableCell>
              </TableRow>
            );
          })}
          {!loading && events.length === 0 ? (
            <TableRow>
              <TableCell colSpan={4} className="py-6 text-center text-xs text-muted-foreground">
                No high-level events captured.
              </TableCell>
            </TableRow>
          ) : null}
        </TableBody>
      </Table>
    </ScrollArea>
  );
}

function runEventBadge(type: string): {
  variant: "default" | "secondary" | "destructive" | "muted";
  tone: "success" | "progress" | "failure" | "cancelled";
} {
  if (type.endsWith("_failed")) {
    return { variant: "destructive", tone: "failure" };
  }
  if (type.endsWith("_cancelled")) {
    return { variant: "muted", tone: "cancelled" };
  }
  if (type === "asset_start") {
    return { variant: "secondary", tone: "progress" };
  }
  return { variant: "default", tone: "success" };
}

function combineRunOutput(logs: PipelineRunLogLine[], error?: string) {
  const captured = logs.map((log) => log.line).join("");
  const terminalError = error?.trim();
  if (!terminalError || captured.includes(terminalError)) {
    return captured || "No output captured.";
  }

  const separator = captured && !captured.endsWith("\n") ? "\n" : "";
  return `${captured}${separator}${terminalError}\n`;
}

function RunTerminalOutput({ output }: { output: string }) {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [output]);
  return (
    <ScrollArea className="h-full min-h-0" viewportClassName="h-full" viewportRef={viewportRef}>
      <AnsiOutput
        output={output}
        className="font-console whitespace-pre-wrap p-3 text-xs text-zinc-100"
      />
    </ScrollArea>
  );
}

function useNow(active: boolean) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!active) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [active]);
  return now;
}

function timelineBounds(run: PipelineRun, steps: PipelineRunStep[], now: number) {
  const times = [
    run.started_at,
    run.finished_at,
    ...steps.flatMap((step) => [step.started_at, step.finished_at]),
  ]
    .map((value) => (value ? new Date(value).getTime() : NaN))
    .filter(Number.isFinite);
  const start = Math.min(...times, now);
  const end = Math.max(...times, run.status === "running" ? now : 0);
  return { start, end: Math.max(end, start + 1000) };
}

function timelineTicks(bounds: { start: number; end: number }) {
  const duration = bounds.end - bounds.start;
  return Array.from({ length: 5 }, (_, index) => {
    const offset = (duration / 4) * index;
    const seconds = Math.round(offset / 1000);
    return {
      label: seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`,
    };
  });
}

function countSteps(steps: PipelineRunStep[]) {
  return {
    queued: steps.filter((step) => step.status === "queued").length,
    running: steps.filter((step) => step.status === "running").length,
    failed: steps.filter((step) => step.status === "failed").length,
    success: steps.filter((step) => step.status === "success").length,
    cancelled: steps.filter((step) => step.status === "cancelled").length,
  };
}

function runEvents(run: PipelineRun, steps: PipelineRunStep[]) {
  const events = steps.flatMap((step) => {
    const items: Array<{ at: string; asset: string; type: string; info: string }> = [];
    if (step.started_at)
      items.push({
        at: step.started_at,
        asset: step.asset,
        type: "asset_start",
        info: `Started ${step.asset}.`,
      });
    if (step.finished_at)
      items.push({
        at: step.finished_at,
        asset: step.asset,
        type:
          step.status === "failed"
            ? "asset_failed"
            : step.status === "cancelled"
              ? "asset_cancelled"
              : "asset_success",
        info:
          step.status === "failed"
            ? step.error || `Failed ${step.asset}.`
            : step.status === "cancelled"
              ? step.error || `Cancelled ${step.asset}.`
              : `Finished ${step.asset} in ${formatStepDuration(step)}.`,
      });
    return items;
  });
  if (run.finished_at)
    events.push({
      at: run.finished_at,
      asset: run.pipeline,
      type:
        run.status === "failed"
          ? "run_failed"
          : run.status === "cancelled"
            ? "run_cancelled"
            : "run_finished",
      info: run.error || `Run ${run.status}.`,
    });
  return events.sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime());
}

function formatStepDuration(step: PipelineRunStep) {
  if (!step.started_at || !step.finished_at) return "-";
  return formatDurationMs(
    new Date(step.finished_at).getTime() - new Date(step.started_at).getTime(),
  );
}

function formatRunDuration(run: PipelineRun) {
  if (!run.started_at || !run.finished_at) return run.status === "running" ? "running" : "-";
  return formatDurationMs(new Date(run.finished_at).getTime() - new Date(run.started_at).getTime());
}

function formatDurationMs(ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return "-";
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.round(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return minutes > 0 ? `${minutes}m ${remainder}s` : `${seconds}s`;
}
