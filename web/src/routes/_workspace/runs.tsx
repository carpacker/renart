import { createFileRoute } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { Play, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";

import {
  getRun,
  getRuns,
  getSchedules,
  triggerPipelineRun,
  updatePipelineSchedule,
} from "@/lib/api";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";
import type { PipelineRun, PipelineRunLogLine, PipelineSchedule } from "@/lib/types";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";

export const Route = createFileRoute("/_workspace/runs")({
  component: RunsPage,
});

function RunsPage() {
  const [schedules, setSchedules] = useState<PipelineSchedule[]>([]);
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<PipelineRun | null>(null);
  const [logs, setLogs] = useState<PipelineRunLogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyPipeline, setBusyPipeline] = useState<string | null>(null);
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);

  const refresh = async () => {
    setLoading(true);
    try {
      const [scheduleResponse, runsResponse] = await Promise.all([
        getSchedules(),
        getRuns(100),
      ]);
      if (scheduleResponse.status === "ok") {
        setSchedules(scheduleResponse.schedules ?? []);
      }
      if (runsResponse.status === "ok") {
        setRuns(runsResponse.runs ?? []);
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void refresh();
  }, []);

  useEffect(() => {
    if (!schedulerRunEvent) {
      return;
    }

    if (schedulerRunEvent.type === "run.log") {
      const { run_id, log } = schedulerRunEvent.run;
      if (selectedRun?.id === run_id) {
        setLogs((existing) => [...existing, log]);
      }
      return;
    }
    if (schedulerRunEvent.type === "run.step") {
      return;
    }

    const run = schedulerRunEvent.run;
    setRuns((current) => [
      run,
      ...current.filter((item) => item.id !== run.id),
    ]);
    setSelectedRun((current) => (current?.id === run.id ? run : current));
  }, [schedulerRunEvent, selectedRun?.id]);

  const updateSchedule = async (
    item: PipelineSchedule,
    patch: Partial<Pick<PipelineSchedule, "enabled" | "schedule" | "timezone" | "catchup">>,
  ) => {
    setBusyPipeline(item.pipeline_id);
    try {
      const next = { ...item, ...patch };
      const response = await updatePipelineSchedule(item.pipeline_id, {
        enabled: next.enabled,
        schedule: next.schedule,
        timezone: next.timezone || "UTC",
        catchup: next.catchup,
      });
      if (response.status === "ok") {
        setSchedules((current) =>
          current.map((schedule) =>
            schedule.pipeline_id === item.pipeline_id ? response.schedule : schedule,
          ),
        );
      }
    } finally {
      setBusyPipeline(null);
    }
  };

  const triggerNow = async (item: PipelineSchedule) => {
    setBusyPipeline(item.pipeline_id);
    try {
      const response = await triggerPipelineRun(item.pipeline_id, { trigger: "manual" });
      if (response.status === "ok") {
        setRuns((current) => [
          response.run,
          ...current.filter((run) => run.id !== response.run.id),
        ]);
        setSelectedRun(response.run);
        setLogs([]);
      }
    } finally {
      setBusyPipeline(null);
    }
  };

  const selectRun = async (run: PipelineRun) => {
    setSelectedRun(run);
    const response = await getRun(run.id);
    if (response.status === "ok") {
      setLogs(response.logs ?? []);
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-auto p-4 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Runs</h1>
          <p className="text-sm text-muted-foreground">
            Local schedules and pipeline run history from `.renart/state.db`.
          </p>
        </div>
        <Button variant="outline" onClick={() => void refresh()} disabled={loading}>
          <RefreshCw className="size-4" />
          Refresh
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Schedules</CardTitle>
          <CardDescription>
            Toggle local scheduling for pipelines. The source of truth remains `pipeline.yml`.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {schedules.length === 0 ? (
            <p className="text-sm text-muted-foreground">No pipelines found.</p>
          ) : (
            schedules.map((item) => (
              <div
                key={item.pipeline_id}
                className="grid gap-3 rounded-lg border p-3 lg:grid-cols-[minmax(180px,1fr)_160px_160px_120px_auto] lg:items-center"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={item.enabled}
                      disabled={busyPipeline === item.pipeline_id}
                      onCheckedChange={(enabled) => void updateSchedule(item, { enabled })}
                    />
                    <div className="truncate font-medium">{item.pipeline_name}</div>
                  </div>
                  <div className="mt-1 truncate text-xs text-muted-foreground">
                    {item.pipeline_path}
                  </div>
                </div>
                <Input
                  value={item.schedule}
                  placeholder="@hourly"
                  disabled={busyPipeline === item.pipeline_id}
                  onChange={(event) =>
                    setSchedules((current) =>
                      current.map((schedule) =>
                        schedule.pipeline_id === item.pipeline_id
                          ? { ...schedule, schedule: event.target.value }
                          : schedule,
                      ),
                    )
                  }
                  onBlur={(event) => void updateSchedule(item, { schedule: event.target.value })}
                />
                <Input
                  value={item.timezone || "UTC"}
                  disabled={busyPipeline === item.pipeline_id}
                  onChange={(event) =>
                    setSchedules((current) =>
                      current.map((schedule) =>
                        schedule.pipeline_id === item.pipeline_id
                          ? { ...schedule, timezone: event.target.value }
                          : schedule,
                      ),
                    )
                  }
                  onBlur={(event) => void updateSchedule(item, { timezone: event.target.value })}
                />
                <label className="flex items-center gap-2 text-sm">
                  <Switch
                    checked={item.catchup}
                    disabled={busyPipeline === item.pipeline_id}
                    onCheckedChange={(catchup) => void updateSchedule(item, { catchup })}
                  />
                  Catchup
                </label>
                <div className="flex flex-wrap items-center justify-end gap-2">
                  {item.next_run_at ? (
                    <span className="text-xs text-muted-foreground">
                      Next {formatDate(item.next_run_at)}
                    </span>
                  ) : null}
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busyPipeline === item.pipeline_id}
                    onClick={() => void triggerNow(item)}
                  >
                    <Play className="size-3.5" />
                    Trigger
                  </Button>
                </div>
              </div>
            ))
          )}
        </CardContent>
      </Card>

      <div className="grid min-h-0 gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>History</CardTitle>
            <CardDescription>Recent local pipeline runs.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2">
            {runs.length === 0 ? (
              <p className="text-sm text-muted-foreground">No runs yet.</p>
            ) : (
              runs.map((run) => (
                <button
                  key={run.id}
                  type="button"
                  onClick={() => void selectRun(run)}
                  className="flex w-full min-w-0 items-center justify-between gap-3 rounded-lg border p-3 text-left hover:bg-accent"
                >
                  <div className="min-w-0">
                    <div className="truncate font-medium">{run.pipeline}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      {run.trigger} · {formatDate(run.started_at)}
                    </div>
                  </div>
                  <span className={statusPillClass(run.status)}>
                    {run.status}
                  </span>
                </button>
              ))
            )}
          </CardContent>
        </Card>

        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>Run Details</CardTitle>
            <CardDescription>
              {selectedRun ? selectedRun.id : "Select a run to view persisted logs."}
            </CardDescription>
          </CardHeader>
          <CardContent>
            {selectedRun ? (
              <div className="space-y-3">
                <div className="text-sm">
                  <div className="font-medium">{selectedRun.pipeline}</div>
                  <div className="text-muted-foreground">
                    {selectedRun.status} · {formatDate(selectedRun.started_at)}
                  </div>
                  {selectedRun.error ? (
                    <div className="mt-2 rounded-md border border-destructive/30 bg-destructive/10 p-2 text-destructive">
                      {selectedRun.error}
                    </div>
                  ) : null}
                </div>
                <pre className="max-h-[420px] overflow-auto rounded-md bg-muted p-3 text-xs">
                  {logs.length > 0
                    ? logs.map((line) => `[${formatDate(line.at)}] ${line.line}`).join("\n")
                    : "No logs captured."}
                </pre>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No run selected.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function statusPillClass(status: PipelineRun["status"]) {
  const base = "rounded-full px-2 py-0.5 text-xs font-medium";
  if (status === "success") {
    return `${base} bg-emerald-500/15 text-emerald-700 dark:text-emerald-300`;
  }
  if (status === "failed" || status === "cancelled") {
    return `${base} bg-destructive/15 text-destructive`;
  }
  return `${base} bg-muted text-muted-foreground`;
}

function formatDate(value?: string) {
  if (!value) {
    return "not started";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
