import { Link } from "@tanstack/react-router";
import {
  ArrowLeft,
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
import { formatSchedulerDate, usePipelineScheduler } from "@/hooks/use-pipeline-scheduler";
import type { PipelineRun, PipelineRunStep } from "@/lib/types";

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
  const { runs, loading, runsTotal, runsOffset } = usePipelineScheduler({ runsQuery });
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
  const { selectedRun, logs, steps, loadingRunId, triggerPipeline } = usePipelineScheduler({
    selectedRunId: runId,
  });
  const run = selectedRun;
  const stdout = useMemo(
    () =>
      logs.length > 0
        ? logs.map((line) => `[${formatSchedulerDate(line.at)}] ${line.line}`).join("\n")
        : "No stdout captured.",
    [logs],
  );

  if (!run) {
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

  return (
    <AppPage>
      <PageHeader
        title={`Run ${run.id}`}
        subtitle={`Run of ${run.pipeline} · ${steps.length || "unknown"} assets · ${formatRunDuration(run)}`}
        actions={
          <div className="flex items-center gap-2">
            <Button asChild variant="ghost" size="icon-sm">
              <Link to="/runs" search={search}>
                <ArrowLeft className="size-4" />
              </Link>
            </Button>
            <StatusPill status={run.status} />
            <Button size="sm" onClick={() => void triggerPipeline(run.pipeline_id)}>
              <RotateCw className="size-3.5" />
              Re-execute
            </Button>
          </div>
        }
      />
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
                    <Play className="size-3.5" />
                    Events
                  </TabsTrigger>
                  <TabsTrigger value="stdout" className={runTabsTriggerClass}>
                    <Terminal className="size-3.5" />
                    stdout
                  </TabsTrigger>
                  <TabsTrigger value="stderr" className={runTabsTriggerClass}>
                    <Terminal className="size-3.5" />
                    stderr
                  </TabsTrigger>
                </TabsList>
              </ScrollArea>
            </div>
            <TabsContent
              value="events"
              className="m-0 min-h-0 flex-1 overflow-hidden data-[state=inactive]:hidden"
            >
              <RunEventsTable run={run} steps={steps} loading={loadingRunId === run.id} />
            </TabsContent>
            <TabsContent
              value="stdout"
              className="m-0 min-h-0 flex-1 overflow-hidden bg-zinc-950 data-[state=inactive]:hidden"
            >
              <RunTerminalOutput output={stdout} />
            </TabsContent>
            <TabsContent
              value="stderr"
              className="m-0 min-h-0 flex-1 overflow-hidden bg-zinc-950 data-[state=inactive]:hidden"
            >
              <RunTerminalOutput output={run.error ?? "No stderr output."} plain />
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
        <div className="mb-2 flex text-[11px] text-muted-foreground">
          {timelineTicks(bounds).map((tick) => (
            <div key={tick.label} className="min-w-0 flex-1 font-mono">
              {tick.label}
            </div>
          ))}
        </div>
        <div className="space-y-2">
          {steps.length === 0 ? (
            <div className="rounded-md border border-dashed p-6 text-center text-xs text-muted-foreground">
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
  const left = Math.max(0, ((start - bounds.start) / (bounds.end - bounds.start)) * 100);
  const width = Math.max(
    1.2,
    ((Math.max(end, start + 1) - start) / (bounds.end - bounds.start)) * 100,
  );
  const color =
    step.status === "failed"
      ? "bg-red-500"
      : step.status === "running"
        ? "bg-blue-500"
        : "bg-green-500";
  return (
    <div className="relative h-7 rounded bg-muted/40">
      <div
        className={`absolute top-0.5 flex h-6 items-center overflow-hidden rounded px-2 text-[11px] font-mono text-white ${color}`}
        style={{ left: `${left}%`, width: `${Math.min(width, 100 - left)}%` }}
      >
        <span className="truncate">{step.asset}</span>
      </div>
    </div>
  );
}

function RunEventsTable({
  run,
  steps,
  loading,
}: {
  run: PipelineRun;
  steps: PipelineRunStep[];
  loading: boolean;
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
          {events.map((event, index) => (
            <TableRow key={`${event.at}-${event.asset}-${event.type}-${index}`}>
              <TableCell className="h-8 py-1.5 font-mono text-xs text-muted-foreground">
                {formatSchedulerDate(event.at)}
              </TableCell>
              <TableCell className="h-8 py-1.5 font-mono text-xs">{event.asset}</TableCell>
              <TableCell className="h-8 py-1.5">
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] uppercase text-muted-foreground">
                  {event.type}
                </span>
              </TableCell>
              <TableCell className="h-8 py-1.5 text-xs">{event.info}</TableCell>
            </TableRow>
          ))}
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

function RunTerminalOutput({ output, plain }: { output: string; plain?: boolean }) {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [output]);
  return (
    <ScrollArea className="h-full min-h-0" viewportClassName="h-full" viewportRef={viewportRef}>
      {plain ? (
        <pre className="font-console whitespace-pre-wrap p-3 text-xs text-zinc-100">{output}</pre>
      ) : (
        <AnsiOutput
          output={output}
          className="font-console whitespace-pre-wrap p-3 text-xs text-zinc-100"
        />
      )}
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
        type: step.status === "failed" ? "asset_failed" : "asset_success",
        info:
          step.status === "failed"
            ? step.error || `Failed ${step.asset}.`
            : `Finished ${step.asset} in ${formatStepDuration(step)}.`,
      });
    return items;
  });
  if (run.finished_at)
    events.push({
      at: run.finished_at,
      asset: run.pipeline,
      type: run.status === "failed" ? "run_failed" : "run_finished",
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
