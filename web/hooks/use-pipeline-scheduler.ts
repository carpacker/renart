import { useAtomValue } from "jotai";
import { useCallback, useEffect, useRef, useState } from "react";

import {
  type GetRunsOptions,
  getRun,
  getRuns,
  getSchedules,
  triggerPipelineRun,
  updatePipelineSchedule,
} from "@/lib/api";
import type { TriggerPipelineRunInput } from "@/lib/api-scheduler";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";
import type {
  PipelineRun,
  PipelineRunLogLine,
  PipelineRunPlan,
  PipelineRunStep,
  PipelineRunUnit,
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
  const [plan, setPlan] = useState<PipelineRunPlan | null>(null);
  const [units, setUnits] = useState<PipelineRunUnit[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyPipeline, setBusyPipeline] = useState<string | null>(null);
  const [loadingRunId, setLoadingRunId] = useState<string | null>(null);
  const [schedulesError, setSchedulesError] = useState<string | null>(null);
  const [runsError, setRunsError] = useState<string | null>(null);
  const [runDetailError, setRunDetailError] = useState<string | null>(null);
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);
  const selectedRunIdRef = useRef(selectedRunId);
  const selectRunRequestIdRef = useRef(0);
  selectedRunIdRef.current = selectedRunId;

  const selectedRunForRequest = selectedRunId
    ? selectedRun?.id === selectedRunId
      ? selectedRun
      : null
    : selectedRun;

  const refreshRuns = useCallback(async () => {
    try {
      const response = await getRuns(runsQuery ?? 100);
      if (response.status !== "ok") {
        throw new Error("The server could not refresh pipeline runs.");
      }
      setRuns(response.runs ?? []);
      setRunsTotal(response.total ?? response.runs?.length ?? 0);
      setRunsLimit(response.limit ?? runsQuery?.limit ?? 100);
      setRunsOffset(response.offset ?? runsQuery?.offset ?? 0);
      setRunsError(null);
    } catch (cause) {
      // Lifecycle SSE handlers intentionally fire refreshes without awaiting
      // them. Keep the last readable records and expose failures instead of
      // creating an unhandled rejection.
      setRunsError(errorMessage(cause, "Pipeline runs could not be refreshed."));
    }
  }, [runsQuery]);

  const refreshSchedules = useCallback(async () => {
    try {
      const response = await getSchedules();
      if (response.status !== "ok") {
        throw new Error("The server could not refresh pipeline schedules.");
      }
      setSchedules(response.schedules ?? []);
      setSchedulesError(null);
    } catch (cause) {
      setSchedulesError(errorMessage(cause, "Pipeline schedules could not be refreshed."));
    }
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      // These resources fail independently. A runs outage must not hide the
      // last readable schedule state (or vice versa).
      await Promise.all([refreshSchedules(), refreshRuns()]);
    } finally {
      setLoading(false);
    }
  }, [refreshRuns, refreshSchedules]);

  const selectRun = useCallback(async (runOrId: PipelineRun | PipelineRun["id"]) => {
    const runId = typeof runOrId === "string" ? runOrId : runOrId.id;
    const requestedRunId = selectedRunIdRef.current;
    if (requestedRunId && requestedRunId !== runId) {
      return;
    }
    const requestId = ++selectRunRequestIdRef.current;
    if (typeof runOrId !== "string") {
      setSelectedRun(runOrId);
    }
    setLoadingRunId(runId);
    setRunDetailError(null);
    try {
      const response = await getRun(runId);
      if (response.status !== "ok") {
        throw new Error(`Run ${runId} could not be loaded.`);
      }
      if (
        selectRunRequestIdRef.current === requestId &&
        (!selectedRunIdRef.current || selectedRunIdRef.current === runId)
      ) {
        setSelectedRun(response.run);
        setLogs(response.logs ?? []);
        setSteps(response.steps ?? []);
        setPlan(response.plan ?? null);
        setUnits(response.units ?? []);
      }
    } catch (cause) {
      if (
        selectRunRequestIdRef.current === requestId &&
        (!selectedRunIdRef.current || selectedRunIdRef.current === runId)
      ) {
        setRunDetailError(errorMessage(cause, `Run ${runId} could not be loaded.`));
      }
    } finally {
      if (selectRunRequestIdRef.current === requestId) {
        setLoadingRunId(null);
      }
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

  const triggerPipeline = useCallback(
    async (pipelineId: string, input: TriggerPipelineRunInput) => {
      const triggeredFromRunDetail = Boolean(selectedRunIdRef.current);
      setBusyPipeline(pipelineId);
      try {
        const response = await triggerPipelineRun(pipelineId, input);
        if (response.status === "ok") {
          setRuns((current) => [
            response.run,
            ...current.filter((run) => run.id !== response.run.id),
          ]);
          // A run-detail route remains bound to its URL. The caller can
          // navigate to response.run.id explicitly, but accepting a rerun must
          // never replace the record shown under the old URL.
          if (!triggeredFromRunDetail && !selectedRunIdRef.current) {
            setSelectedRun(response.run);
            setLogs([]);
            setSteps([]);
            setPlan(null);
            setUnits([]);
          }
        }
        return response;
      } finally {
        setBusyPipeline(null);
      }
    },
    [],
  );

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    if (!selectedRunId) {
      ++selectRunRequestIdRef.current;
      setLoadingRunId(null);
      setSelectedRun(null);
      setLogs([]);
      setSteps([]);
      setPlan(null);
      setUnits([]);
      setRunDetailError(null);
      return;
    }
    setSelectedRun((current) => (current?.id === selectedRunId ? current : null));
    setLogs([]);
    setSteps([]);
    setPlan(null);
    setUnits([]);
    void selectRun(selectedRunId);
  }, [selectedRunId, selectRun]);

  useEffect(() => {
    if (!schedulerRunEvent) {
      return;
    }
    if (schedulerRunEvent.type === "run.log") {
      const { run_id, log } = schedulerRunEvent.run;
      if (selectedRunId ? selectedRunId === run_id : selectedRunForRequest?.id === run_id) {
        setLogs((existing) => [...existing, log]);
      }
      return;
    }
    if (schedulerRunEvent.type === "run.step") {
      const step = schedulerRunEvent.run;
      if (
        selectedRunId ? selectedRunId === step.run_id : selectedRunForRequest?.id === step.run_id
      ) {
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
    if (schedulerRunEvent.type === "run.unit") {
      const { run_id, unit } = schedulerRunEvent.run;
      if (selectedRunId ? selectedRunId === run_id : selectedRunForRequest?.id === run_id) {
        setUnits((existing) =>
          [unit, ...existing.filter((item) => item.position !== unit.position)].sort(
            (a, b) => a.position - b.position,
          ),
        );
      }
      return;
    }
    const run = schedulerRunEvent.run;
    const selected = selectedRunId
      ? selectedRunId === run.id
      : selectedRunForRequest?.id === run.id;
    setRuns((current) => [run, ...current.filter((item) => item.id !== run.id)]);
    if (selected) {
      setSelectedRun((current) => (current?.id === run.id ? run : current));
    }
    // A detail page can open between two streamed log events. Reloading the
    // canonical record at completion closes that subscription race without
    // relying on a final aggregate-output replay.
    if (schedulerRunEvent.type === "run.finished" && selected) {
      void selectRun(run.id);
    }
    void refreshRuns();
    void refreshSchedules();
  }, [
    refreshRuns,
    refreshSchedules,
    schedulerRunEvent,
    selectRun,
    selectedRunForRequest?.id,
    selectedRunId,
  ]);

  return {
    schedules,
    runs,
    runsTotal,
    runsLimit,
    runsOffset,
    selectedRun: selectedRunForRequest,
    logs,
    steps,
    plan,
    units,
    loading,
    schedulesError,
    runsError,
    runDetailError,
    busyPipeline,
    loadingRunId,
    refresh,
    refreshRuns,
    selectRun,
    patchScheduleDraft,
    updateSchedule,
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

function errorMessage(cause: unknown, fallback: string) {
  return cause instanceof Error && cause.message.trim() ? cause.message : fallback;
}
