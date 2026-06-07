import { useAtomValue } from "jotai";
import { useCallback, useEffect, useState } from "react";

import {
  getRun,
  getRuns,
  getSchedules,
  triggerPipelineRun,
  updatePipelineSchedule,
} from "@/lib/api";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";
import type { PipelineRun, PipelineRunLogLine, PipelineSchedule } from "@/lib/types";

type SchedulePatch = Partial<Pick<PipelineSchedule, "enabled" | "schedule" | "timezone" | "catchup">>;

export function usePipelineScheduler({ selectedRunId }: { selectedRunId?: string } = {}) {
  const [schedules, setSchedules] = useState<PipelineSchedule[]>([]);
  const [runs, setRuns] = useState<PipelineRun[]>([]);
  const [selectedRun, setSelectedRun] = useState<PipelineRun | null>(null);
  const [logs, setLogs] = useState<PipelineRunLogLine[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyPipeline, setBusyPipeline] = useState<string | null>(null);
  const [loadingRunId, setLoadingRunId] = useState<string | null>(null);
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);

  const refresh = useCallback(async () => {
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
  }, []);

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
      }
    } finally {
      setLoadingRunId(null);
    }
  }, []);

  const patchScheduleDraft = useCallback((pipelineId: string, patch: SchedulePatch) => {
    setSchedules((current) => current.map((schedule) => (
      schedule.pipeline_id === pipelineId ? { ...schedule, ...patch } : schedule
    )));
  }, []);

  const updateSchedule = useCallback(async (item: PipelineSchedule, patch: SchedulePatch) => {
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
        setSchedules((current) => current.map((schedule) => (
          schedule.pipeline_id === item.pipeline_id ? { ...response.schedule, next_run_at: response.schedule.next_run_at ?? schedule.next_run_at } : schedule
        )));
        void refreshSchedules();
      }
    } finally {
      setBusyPipeline(null);
    }
  }, [refreshSchedules]);

  const triggerNow = useCallback(async (item: PipelineSchedule) => {
    setBusyPipeline(item.pipeline_id);
    try {
      const response = await triggerPipelineRun(item.pipeline_id, { trigger: "manual" });
      if (response.status === "ok") {
        setRuns((current) => [response.run, ...current.filter((run) => run.id !== response.run.id)]);
        setSelectedRun(response.run);
        setLogs([]);
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
        setRuns((current) => [response.run, ...current.filter((run) => run.id !== response.run.id)]);
        setSelectedRun(response.run);
        setLogs([]);
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
    const run = schedulerRunEvent.run;
    setRuns((current) => [run, ...current.filter((item) => item.id !== run.id)]);
    setSelectedRun((current) => (current?.id === run.id ? run : current));
    void refreshSchedules();
  }, [refreshSchedules, schedulerRunEvent, selectedRun?.id, selectedRunId]);

  return {
    schedules,
    runs,
    selectedRun,
    logs,
    loading,
    busyPipeline,
    loadingRunId,
    refresh,
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
