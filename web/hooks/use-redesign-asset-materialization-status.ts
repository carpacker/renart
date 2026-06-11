import { useAtomValue } from "jotai";
import { useEffect, useMemo, useState } from "react";

import { getAssetFreshness, getRun, getRuns } from "@/lib/api";
import { schedulerRunEventAtom } from "@/lib/atoms/domains/results";
import {
  selectedEnvironmentAtom,
  selectedExecutionTimeWindowAtom,
} from "@/lib/atoms/domains/workspace";
import type { PipelineRunStep } from "@/lib/types";

export type RedesignAssetMaterializationStatus = "unknown" | "pending" | "success" | "failed";

export type RedesignAssetMaterializationDisplayState = {
  status: RedesignAssetMaterializationStatus;
  materializedAt?: string;
  loading: boolean;
};

export type RedesignMaterializationAsset = {
  id: string;
  name: string;
  pipelineId?: string;
  isMaterialized?: boolean;
};

type AssetStatusEntry = {
  status: Exclude<RedesignAssetMaterializationStatus, "unknown">;
  recordedAt: number;
  materializedAt?: string;
};

type StatusByKey = Record<string, AssetStatusEntry>;

type RunContextById = Record<string, { pipelineId: string; environment: string; winStart?: string; winEnd?: string }>;

function statusForStep(status: PipelineRunStep["status"]): Exclude<RedesignAssetMaterializationStatus, "unknown"> {
  if (status === "success") return "success";
  if (status === "failed" || status === "cancelled") return "failed";
  return "pending";
}

function stepTimestamp(step: PipelineRunStep) {
  return new Date(step.finished_at ?? step.started_at ?? 0).getTime() || Date.now();
}

function applyStep(current: StatusByKey, step: PipelineRunStep, keys: string[] = [step.asset]): StatusByKey {
  let next = current;
  for (const key of keys) {
    next = applyStepForKey(next, step, key);
  }
  return next;
}

function applyStepForKey(current: StatusByKey, step: PipelineRunStep, key: string): StatusByKey {
  const recordedAt = stepTimestamp(step);
  const existing = current[key];
  const nextStatus = statusForStep(step.status);
  if (existing && existing.recordedAt > recordedAt && !(existing.status === "pending" && nextStatus !== "pending")) {
    return current;
  }
  return {
    ...current,
    [key]: {
      status: nextStatus,
      recordedAt,
      materializedAt: nextStatus === "success" ? (step.finished_at ?? step.started_at) : existing?.materializedAt,
    },
  };
}

function withinSelectedWindow(startedAt: string | undefined, finishedAt: string | undefined, window: { start: string; end: string } | null) {
  if (!window) return true;
  if (!startedAt && !finishedAt) return true;
  const eventTime = new Date(finishedAt ?? startedAt ?? 0).getTime();
  if (!Number.isFinite(eventTime) || eventTime <= 0) return false;
  return eventTime >= new Date(window.start).getTime() && eventTime <= new Date(window.end).getTime();
}

function matchesSelectedEnvironment(environment: string | undefined, selectedEnvironment: string | undefined) {
  if (!selectedEnvironment) return true;
  if (selectedEnvironment === "default") return !environment || environment === "default";
  return environment === selectedEnvironment;
}

function keysForStepAsset(stepAsset: string, assets: RedesignMaterializationAsset[], pipelineId?: string) {
  const shortStepName = stepAsset.split(".").pop() ?? stepAsset;
  const keys = new Set<string>();
  for (const asset of assets) {
    if (pipelineId && asset.pipelineId && asset.pipelineId !== pipelineId) continue;
    const shortAssetName = asset.name.split(".").pop() ?? asset.name;
    if (asset.id === stepAsset || asset.name === stepAsset || asset.name === shortStepName || shortAssetName === stepAsset || shortAssetName === shortStepName || stepAsset.endsWith(`.${asset.name}`)) {
      keys.add(asset.id);
      keys.add(asset.name);
      keys.add(stepAsset);
    }
  }
  return [...keys];
}

function formatRedesignMaterializedAt(value?: string) {
  if (!value) return "date unknown";
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function labelForRedesignMaterializationState(state?: RedesignAssetMaterializationDisplayState) {
  if (!state || (state.loading && state.status === "unknown")) return "checking";
  if (state.status === "pending") return "running";
  if (state.status === "success") return formatRedesignMaterializedAt(state.materializedAt);
  if (state.status === "failed") return state.materializedAt ? formatRedesignMaterializedAt(state.materializedAt) : "failed";
  return "no run yet";
}

export function useRedesignAssetMaterializationStatus(assets: RedesignMaterializationAsset[]) {
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const selectedExecutionTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const [statusByKey, setStatusByKey] = useState<StatusByKey>({});
  const [freshnessByName, setFreshnessByName] = useState<Record<string, string | undefined>>({});
  const [runContextById, setRunContextById] = useState<RunContextById>({});
  const [loading, setLoading] = useState(true);
  const pipelineIds = useMemo(() => new Set(assets.map((asset) => asset.pipelineId).filter((value): value is string => Boolean(value))), [assets]);

  useEffect(() => {
    let cancelled = false;

    async function loadRecentStepStatuses() {
      setLoading(true);
      const freshnessResponse = await getAssetFreshness({ environment: selectedEnvironment }).catch(() => null);
      if (!cancelled && freshnessResponse) {
        setFreshnessByName(Object.fromEntries((freshnessResponse.assets ?? []).map((asset) => [asset.asset_name, asset.materialized_at])));
      }
      const runsResponse = await getRuns({ limit: 20, environment: selectedEnvironment }).catch(() => null);
      if (!runsResponse || runsResponse.status !== "ok") {
        if (!cancelled) setLoading(false);
        return;
      }
      const relevantRuns = (runsResponse.runs ?? [])
        .filter((run) => pipelineIds.size === 0 || pipelineIds.has(run.pipeline_id))
        .filter((run) => matchesSelectedEnvironment(run.environment, selectedEnvironment))
        .filter((run) => withinSelectedWindow(run.win_start, run.win_end, selectedExecutionTimeWindow))
        .slice(0, 8);
      const details = await Promise.all(relevantRuns.map((run) => getRun(run.id).catch(() => null)));
      if (cancelled) {
        return;
      }
      setStatusByKey((current) => {
        let next = current;
        for (const detail of details) {
          if (!detail || detail.status !== "ok") continue;
          for (const step of detail.steps ?? []) {
            const keys = keysForStepAsset(step.asset, assets, detail.run.pipeline_id);
            if (keys.length === 0) continue;
            next = applyStep(next, step, keys);
          }
        }
        return next;
      });
      setLoading(false);
    }

    void loadRecentStepStatuses();
    return () => {
      cancelled = true;
    };
  }, [assets, pipelineIds, selectedEnvironment, selectedExecutionTimeWindow]);

  useEffect(() => {
    if (!schedulerRunEvent) {
      return;
    }

    if (schedulerRunEvent.type === "run.queued" || schedulerRunEvent.type === "run.started") {
      const run = schedulerRunEvent.run;
      if (pipelineIds.size > 0 && !pipelineIds.has(run.pipeline_id)) {
        return;
      }
      if (!matchesSelectedEnvironment(run.environment, selectedEnvironment)) {
        return;
      }
      if (!withinSelectedWindow(run.win_start, run.win_end, selectedExecutionTimeWindow)) {
        return;
      }
      setRunContextById((current) => ({
        ...current,
        [run.id]: {
          pipelineId: run.pipeline_id,
          environment: run.environment,
          winStart: run.win_start,
          winEnd: run.win_end,
        },
      }));
      const recordedAt = run.started_at ? new Date(run.started_at).getTime() : Date.now();
      setStatusByKey((current) => {
        const next = { ...current };
        for (const asset of assets) {
          if (asset.pipelineId && asset.pipelineId !== run.pipeline_id) continue;
          next[asset.name] = { status: "pending", recordedAt };
          next[asset.id] = { status: "pending", recordedAt };
        }
        return next;
      });
      return;
    }

    if (schedulerRunEvent.type === "run.finished") {
      const run = schedulerRunEvent.run;
      if (pipelineIds.size > 0 && !pipelineIds.has(run.pipeline_id)) {
        return;
      }
      if (!matchesSelectedEnvironment(run.environment, selectedEnvironment)) {
        return;
      }
      if (!withinSelectedWindow(run.win_start, run.win_end, selectedExecutionTimeWindow)) {
        return;
      }
      void getRun(run.id).then((detail) => {
        if (!detail || detail.status !== "ok") return;
        setStatusByKey((current) => {
          let next = current;
          for (const step of detail.steps ?? []) {
            const keys = keysForStepAsset(step.asset, assets, detail.run.pipeline_id || run.pipeline_id);
            if (keys.length === 0) continue;
            next = applyStep(next, step, keys);
          }
          return next;
        });
      }).catch(() => undefined);
      return;
    }

    if (schedulerRunEvent.type !== "run.step") {
      return;
    }

    const step = schedulerRunEvent.run;
    const runContext = runContextById[step.run_id];
    if (runContext) {
      if (pipelineIds.size > 0 && !pipelineIds.has(runContext.pipelineId)) return;
      if (!matchesSelectedEnvironment(runContext.environment, selectedEnvironment)) return;
      if (!withinSelectedWindow(runContext.winStart, runContext.winEnd, selectedExecutionTimeWindow)) return;
    }
    const keys = keysForStepAsset(step.asset, assets, runContext?.pipelineId);
    if (keys.length === 0) {
      return;
    }
    setStatusByKey((current) => applyStep(current, step, keys));
  }, [assets, pipelineIds, schedulerRunEvent, selectedEnvironment, selectedExecutionTimeWindow]);

  return useMemo(() => {
    const result: Record<string, RedesignAssetMaterializationDisplayState> = {};
    for (const asset of assets) {
      const entry = statusByKey[asset.id] ?? statusByKey[asset.name];
      const materializedAt = entry?.materializedAt ?? freshnessByName[asset.name];
      const unresolved = loading && !entry && !materializedAt;
      result[asset.id] = {
        status: unresolved ? "unknown" : entry?.status ?? (materializedAt || asset.isMaterialized ? "success" : "unknown"),
        materializedAt,
        loading,
      };
    }
    return result;
  }, [assets, freshnessByName, loading, statusByKey]);
}
