import { useAtomValue } from "jotai";
import { useCallback, useEffect, useState } from "react";

import {
  type GetRunsOptions,
  getRun,
  getRuns,
  getSchedules,
  triggerPipelineRun,
  updatePipelineSchedule,
} from "@/lib/api";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";
import type {
  PipelineRun,
  PipelineRunLogLine,
  PipelineRunStep,
  PipelineSchedule,
} from "@/lib/types";

type SchedulePatch = Partial<
  Pick<PipelineSchedule, "enabled" | "schedule" | "timezone" | "catchup">
>;

export function usePipelineScheduler({
  selectedRunId,
  runsQuery,
}: { selectedRunId?: string; runsQuery?: GetRunsOptions } = {}) {
  const [schedules, setSchedules] = useState<PipelineSchedule[]>([]);
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [runsTotal, setRunsTotal] = useState(0);
  const [runsLimit, setRunsLimit] = useState(runsQuery?.limit ?? 100);
  const [runsOffset, setRunsOffset] = useState(runsQuery?.offset ?? 0);
  const [selectedRun, setSelectedRun] = useState<PipelineRun | null>(null);
  const [logs, setLogs] = useState<PipelineRunLogLine[]>([]);
  const [steps, setSteps] = useState<PipelineRunStep[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyPipeline, setBusyPipeline] = useState<string | null>(null);
  const [loadingRunId, setLoadingRunId] = useState<string | null>(null);
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);

  const refreshRuns = useCallback(async () => {
    const response = await getRuns(runsQuery ?? 100);
    if (response.status === "ok") {
      setRuns(response.runs ?? []);
      setRunsTotal(response.total ?? response.runs?.length ?? 0);
      setRunsLimit(response.limit ?? runsQuery?.limit ?? 100);
      setRunsOffset(response.offset ?? runsQuery?.offset ?? 0);
    }
  }, [runsQuery]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [scheduleResponse, runsResponse] = await Promise.all([
        getSchedules(),
        getRuns(runsQuery ?? 100),
      ]);
      if (scheduleResponse.status === "ok") {
        setSchedules(scheduleResponse.schedules ?? []);
      }
      if (runsResponse.status === "ok") {
        setRuns(runsResponse.runs ?? []);
        setRunsTotal(runsResponse.total ?? runsResponse.runs?.length ?? 0);
        setRunsLimit(runsResponse.limit ?? runsQuery?.limit ?? 100);
        setRunsOffset(runsResponse.offset ?? runsQuery?.offset ?? 0);
      }
    } finally {
      setLoading(false);
    }
  }, [runsQuery]);

  const refreshSchedules = useCallback(async () => {
    const response = await getSchedules();
    if (response.status === "ok") {
      setSchedules(response.schedules ?? []);
    }
  }, []);

  const selectRun = useCallback(async (runOrId: PipelineRun | PipelineRun["id"]) => {
    const runId = typeof runOrId === "string" ? runOrId : runOrId.id;
    if (typeof runOrId !== "string") {
      setSelectedRun(runOrId);
    }
    setLoadingRunId(runId);
    try {
      const response = await getRun(runId);
      if (response.status === "ok") {
        setSelectedRun(response.run);
        setLogs(response.logs ?? []);
        setSteps(response.steps ?? []);
      }
    } finally {
      setLoadingRunId(null);
    }
  }, []);

  const patchScheduleDraft = useCallback((pipelineId: string, patch: SchedulePatch) => {
    setSchedules((current) =>
      current.map((schedule) =>
        schedule.pipeline_id === pipelineId ? { ...schedule, ...patch } : schedule,
      ),
    );
  }, []);

  const updateSchedule = useCallback(
    async (item: PipelineSchedule, patch: SchedulePatch) => {
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
              schedule.pipeline_id === item.pipeline_id
                ? {
                    ...response.schedule,
                    next_run_at: response.schedule.next_run_at ?? schedule.next_run_at,
                  }
                : schedule,
            ),
          );
          void refreshSchedules();
        }
      } finally {
        setBusyPipeline(null);
      }
    },
    [refreshSchedules],
  );

  const triggerNow = useCallback(async (item: PipelineSchedule) => {
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
        setSteps([]);
      }
      return response;
    } finally {
      setBusyPipeline(null);
    }
  }, []);

  const triggerPipeline = useCallback(async (pipelineId: string) => {
    setBusyPipeline(pipelineId);
    try {
      const response = await triggerPipelineRun(pipelineId, { trigger: "manual" });
      if (response.status === "ok") {
        setRuns((current) => [
          response.run,
          ...current.filter((run) => run.id !== response.run.id),
        ]);
        setSelectedRun(response.run);
        setLogs([]);
        setSteps([]);
      }
      return response;
    } finally {
      setBusyPipeline(null);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!selectedRunId) {
      setSelectedRun(null);
      setLogs([]);
      setSteps([]);
      return;
    }
    void selectRun(selectedRunId);
  }, [selectedRunId, selectRun]);

  useEffect(() => {
    if (!schedulerRunEvent) {
      return;
    }
    if (schedulerRunEvent.type === "run.log") {
      const { run_id, log } = schedulerRunEvent.run;
      if (selectedRun?.id === run_id || selectedRunId === run_id) {
        setLogs((existing) => [...existing, log]);
      }
      return;
    }
    if (schedulerRunEvent.type === "run.step") {
      const step = schedulerRunEvent.run;
      if (selectedRun?.id === step.run_id || selectedRunId === step.run_id) {
        setSteps((existing) =>
          [
            step,
            ...existing.filter(
              (item) => !(item.run_id === step.run_id && item.asset === step.asset),
            ),
          ].sort(
            (a, b) =>
              new Date(a.started_at ?? a.finished_at ?? 0).getTime() -
              new Date(b.started_at ?? b.finished_at ?? 0).getTime(),
          ),
        );
      }
      return;
    }
    const run = schedulerRunEvent.run;
    const selected = selectedRun?.id === run.id || selectedRunId === run.id;
    setRuns((current) => [run, ...current.filter((item) => item.id !== run.id)]);
    setSelectedRun((current) => (current?.id === run.id ? run : current));
    // A detail page can open between two streamed log events. Reloading the
    // canonical record at completion closes that subscription race without
    // relying on a final aggregate-output replay.
    if (schedulerRunEvent.type === "run.finished" && selected) {
      void selectRun(run.id);
    }
    void refreshRuns();
    void refreshSchedules();
  }, [refreshRuns, refreshSchedules, schedulerRunEvent, selectRun, selectedRun?.id, selectedRunId]);

  return {
    schedules,
    runs,
    runsTotal,
    runsLimit,
    runsOffset,
    selectedRun,
    logs,
    steps,
    loading,
    busyPipeline,
    loadingRunId,
    refresh,
    refreshRuns,
    selectRun,
    patchScheduleDraft,
    updateSchedule,
    triggerNow,
    triggerPipeline,
  };
}

export function formatSchedulerDate(value?: string) {
  if (!value) {
    return "not started";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
