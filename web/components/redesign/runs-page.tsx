import { Link } from "@tanstack/react-router";
import { ArrowLeft, ChevronLeft, ChevronRight, Loader2, MousePointer2, Play, RotateCw, Search, Terminal, X } from "lucide-react";
import { useEffect, useMemo, useRef } from "react";

import { AnsiOutput } from "@/components/ansi-output";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  DelimitedCardContent,
  DelimitedCardHeader,
  DelimitedCardTitle,
} from "@/components/ui/delimited-card";
import { formatSchedulerDate, usePipelineScheduler } from "@/hooks/use-pipeline-scheduler";
import type { PipelineRun, PipelineRunLogLine } from "@/lib/types";

import { PageHeader, RedesignPage, RedesignPanel, SimpleTable, StatusPill } from "./redesign-primitives";

const runTabsTriggerClass = "flex-none";
const runStatuses = ["all", "queued", "running", "success", "failed", "cancelled"] as const;
const pageSize = 8;

export type RedesignRunsSearch = {
  q?: string;
  status?: (typeof runStatuses)[number];
  page?: number;
};

export function normalizeRedesignRunsSearch(search: Record<string, unknown>): RedesignRunsSearch {
  const page = typeof search.page === "number" && search.page > 0 ? Math.floor(search.page) : undefined;
  return {
    q: typeof search.q === "string" && search.q.trim() ? search.q : undefined,
    status: runStatuses.includes(search.status as never) ? (search.status as RedesignRunsSearch["status"]) : undefined,
    page,
  };
}

export function RedesignRunsPage({
  selectedRunId,
  search = {},
  onSearchChange,
}: {
  selectedRunId?: string;
  search?: RedesignRunsSearch;
  onSearchChange?: (search: RedesignRunsSearch) => void;
}) {
  const { runs, selectedRun, logs, loading, loadingRunId, triggerPipeline } = usePipelineScheduler({ selectedRunId });
  const q = search.q ?? "";
  const status = search.status ?? "all";
  const filteredRuns = runs.filter((run) => {
    const matchesStatus = status === "all" || run.status === status;
    const query = q.trim().toLowerCase();
    const matchesQuery = !query || run.pipeline.toLowerCase().includes(query) || run.id.toLowerCase().includes(query);
    return matchesStatus && matchesQuery;
  });
  const pages = Math.max(1, Math.ceil(filteredRuns.length / pageSize));
  const page = Math.min(search.page ?? 1, pages);
  const start = (page - 1) * pageSize;
  const visibleRuns = filteredRuns.slice(start, start + pageSize);
  const updateSearch = (next: RedesignRunsSearch) => onSearchChange?.({ ...search, ...next });

  return (
    <RedesignPage>
      <PageHeader
        title="Runs"
        subtitle="Local pipeline run history from .renart/state.db"
        actions={(
          <div className="flex min-w-0 items-center gap-2">
            <div className="flex h-8 min-w-0 items-center gap-2 rounded-md border bg-background px-2">
              <Search className="size-3.5 text-muted-foreground" />
              <Input value={q} onChange={(event) => updateSearch({ q: event.target.value || undefined, page: 1 })} placeholder="Search runs..." className="h-7 min-w-0 border-0 bg-transparent px-0 text-xs shadow-none focus-visible:ring-0" />
              {q ? <Button variant="ghost" size="icon-sm" onClick={() => updateSearch({ q: undefined, page: 1 })}><X className="size-3.5" /></Button> : null}
            </div>
            {loading ? <span className="flex items-center gap-1.5 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />Loading</span> : null}
          </div>
        )}
      />
      <div className="flex flex-wrap items-center gap-1.5 px-3 pb-2">
        {runStatuses.map((item) => (
          <Button key={item} variant={status === item ? "secondary" : "outline"} size="xs" className="capitalize" onClick={() => updateSearch({ status: item === "all" ? undefined : item, page: 1 })}>
            {item}{item !== "all" ? <span className="ml-1 text-[10px] text-muted-foreground">{runs.filter((run) => run.status === item).length}</span> : null}
          </Button>
        ))}
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 px-3 pb-3 xl:grid-cols-[minmax(0,1fr)_420px]">
        <RedesignPanel className="flex min-h-0 flex-col overflow-hidden">
          <SimpleTable
            columns={["Status", "Run ID", "Pipeline", "Environment", "Trigger", "Started", "Duration"]}
            rows={visibleRuns.map((run) => [
              <StatusPill key="status" status={run.status} />,
              <Link key="id" to="/redesign/runs/$runId" params={{ runId: run.id }} search={search} className="font-mono text-primary hover:underline">{run.id}</Link>,
              <span key="pipeline" className="font-mono">{run.pipeline}</span>,
              run.environment || "default",
              <span key="trigger" className="capitalize">{run.trigger}</span>,
              formatSchedulerDate(run.started_at),
              formatRunDuration(run),
            ])}
          />
          <div className="flex h-11 items-center gap-3 border-t px-3 text-xs text-muted-foreground">
            <span>{filteredRuns.length === 0 ? "0 runs" : `${start + 1}-${start + visibleRuns.length} of ${filteredRuns.length}`}</span>
            <div className="flex-1" />
            <Button variant="outline" size="xs" disabled={page <= 1} onClick={() => updateSearch({ page: page - 1 })}><ChevronLeft className="size-3" />Prev</Button>
            <span className="font-mono">{page} / {pages}</span>
            <Button variant="outline" size="xs" disabled={page >= pages} onClick={() => updateSearch({ page: page + 1 })}>Next<ChevronRight className="size-3" /></Button>
          </div>
        </RedesignPanel>
        {selectedRun ? <RunDetailPanel run={selectedRun} logs={logs} loading={loadingRunId === selectedRun.id} onReexecute={() => void triggerPipeline(selectedRun.pipeline_id)} /> : <NoRunSelectedPanel />}
      </div>
    </RedesignPage>
  );
}

function NoRunSelectedPanel() {
  return (
    <RedesignPanel className="hidden min-h-0 flex-col items-center justify-center p-8 text-center xl:flex">
      <div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <MousePointer2 className="size-5" />
      </div>
      <h2 className="mt-4 text-sm font-semibold">No run selected</h2>
      <p className="mt-1 max-w-64 text-xs text-muted-foreground">Choose a run from the table to inspect the timeline, events, stdout, and stderr.</p>
    </RedesignPanel>
  );
}

function RunDetailPanel({ run, logs, loading, onReexecute }: { run: PipelineRun; logs: PipelineRunLogLine[]; loading: boolean; onReexecute: () => void }) {
  const stdout = useMemo(() => logs.length > 0 ? logs.map((line) => `[${formatSchedulerDate(line.at)}] ${line.line}`).join("\n") : "No stdout captured.", [logs]);
  return (
    <RedesignPanel className="min-h-0 flex flex-col overflow-hidden">
          <DelimitedCardHeader>
            <Button asChild variant="ghost" size="icon-sm">
              <Link to="/redesign/runs"><ArrowLeft className="size-4" /></Link>
            </Button>
            <DelimitedCardTitle>Run {run.id}</DelimitedCardTitle>
            <StatusPill status={run.status} />
            <Button className="ml-auto" size="sm" onClick={onReexecute}><RotateCw className="size-3.5" />Re-execute</Button>
          </DelimitedCardHeader>
          <DelimitedCardContent className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden p-3">
            <div className="grid shrink-0 gap-2 rounded-lg border bg-muted/30 p-3 text-xs sm:grid-cols-2">
              <RunField label="Pipeline" value={run.pipeline} />
              <RunField label="Environment" value={run.environment || "default"} />
              <RunField label="Trigger" value={run.trigger} />
              <RunField label="Duration" value={formatRunDuration(run)} />
              <RunField label="Started" value={formatSchedulerDate(run.started_at)} />
              <RunField label="Finished" value={formatSchedulerDate(run.finished_at)} />
              {run.win_start || run.win_end ? <RunField label="Window" value={`${formatSchedulerDate(run.win_start)} - ${formatSchedulerDate(run.win_end)}`} className="sm:col-span-2" /> : null}
              {run.error ? <div className="rounded-md border border-red-200 bg-red-50 p-2 text-red-700 sm:col-span-2">{run.error}</div> : null}
            </div>
            <Tabs defaultValue="events" className="min-h-0 flex-1 gap-0 overflow-hidden">
              <ScrollArea className="min-w-0 shrink-0" horizontalScrollBarClassName="hidden" viewportClassName="w-full">
                <TabsList className="w-max max-w-none">
                  <TabsTrigger value="events" className={runTabsTriggerClass}><Play className="size-3.5" />Events</TabsTrigger>
                  <TabsTrigger value="stdout" className={runTabsTriggerClass}><Terminal className="size-3.5" />stdout</TabsTrigger>
                  <TabsTrigger value="stderr" className={runTabsTriggerClass}><Terminal className="size-3.5" />stderr</TabsTrigger>
                </TabsList>
              </ScrollArea>
              <div className="flex min-h-0 flex-1 flex-col pt-3">
                <TabsContent value="events" className="m-0 min-h-0 overflow-hidden rounded-lg border data-[state=inactive]:hidden">
                  <RunEventsTable logs={logs} loading={loading} />
                </TabsContent>
                <TabsContent value="stdout" className="m-0 min-h-0 overflow-hidden rounded-lg border bg-zinc-950 data-[state=inactive]:hidden">
                  <RunTerminalOutput output={stdout} />
                </TabsContent>
                <TabsContent value="stderr" className="m-0 min-h-0 overflow-hidden rounded-lg border bg-zinc-950 data-[state=inactive]:hidden">
                  <RunTerminalOutput output={run.error ?? "No stderr output."} plain />
                </TabsContent>
              </div>
            </Tabs>
          </DelimitedCardContent>
        </RedesignPanel>
  );
}

function RunEventsTable({ logs, loading }: { logs: PipelineRunLogLine[]; loading: boolean }) {
  const viewportRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
  }, [logs.length, loading]);

  return (
    <ScrollArea className="h-full min-h-0" viewportClassName="h-full" viewportRef={viewportRef}>
      <Table>
        <TableHeader className="sticky top-0 z-10 bg-card">
          <TableRow>
            <TableHead className="h-8 w-36 text-xs uppercase text-muted-foreground">Timestamp</TableHead>
            <TableHead className="h-8 text-xs uppercase text-muted-foreground">Log</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {logs.map((line, index) => (
            <TableRow key={`${line.at}-${index}`}>
              <TableCell className="h-8 py-1.5 font-mono text-xs text-muted-foreground">{formatSchedulerDate(line.at)}</TableCell>
              <TableCell className="h-8 py-1.5 font-mono text-xs whitespace-pre-wrap">{line.line}</TableCell>
            </TableRow>
          ))}
          {!loading && logs.length === 0 ? (
            <TableRow>
              <TableCell colSpan={2} className="py-6 text-center text-xs text-muted-foreground">No logs captured.</TableCell>
            </TableRow>
          ) : null}
          {loading ? (
            <TableRow>
              <TableCell colSpan={2} className="py-3 text-xs text-muted-foreground">
                <span className="inline-flex items-center gap-2"><Loader2 className="size-3.5 animate-spin" />Loading logs...</span>
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
        <AnsiOutput output={output} className="font-console whitespace-pre-wrap p-3 text-xs text-zinc-100" />
      )}
    </ScrollArea>
  );
}

function RunField({ label, value, className }: { label: string; value: string; className?: string }) {
  return <div className={className}><div className="text-[11px] text-muted-foreground">{label}</div><div className="truncate font-mono">{value}</div></div>;
}

function formatRunDuration(run: PipelineRun) {
  if (!run.started_at || !run.finished_at) {
    return run.status === "running" ? "running" : "-";
  }
  const ms = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime();
  if (!Number.isFinite(ms) || ms < 0) {
    return "-";
  }
  if (ms < 1000) {
    return `${ms}ms`;
  }
  const seconds = Math.round(ms / 1000);
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return minutes > 0 ? `${minutes}m ${remainder}s` : `${seconds}s`;
}
