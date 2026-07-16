import { useAtomValue } from "jotai";
import { useEffect, useMemo, useState } from "react";

import { getPipelineStaleness, type AssetStaleness } from "@/lib/api-staleness";
import { stalenessEventAtom } from "@/lib/atoms/domains/results";
import {
  selectedEnvironmentAtom,
  selectedExecutionTimeWindowAtom,
} from "@/lib/atoms/domains/workspace";

export type PipelineStaleness = {
  byAssetName: Record<string, AssetStaleness>;
  staleAssets: AssetStaleness[];
  loading: boolean;
  error: string | null;
};

export type PipelinesStaleness = {
  byPipelineId: Record<string, Record<string, AssetStaleness>>;
  loading: boolean;
  error: string | null;
};

type StalenessSnapshot = {
  selectionKey: string;
  assetsByPipelineId: Record<string, AssetStaleness[]>;
};

type StalenessRequestState = {
  selectionKey: string;
  loading: boolean;
  error: string | null;
};

const staleStatuses = new Set([
  "stale_edited",
  "stale_upstream",
  "partial",
  "volatile",
  "never_built",
  "missing",
]);

export function isStaleStatus(status: AssetStaleness["status"]) {
  return staleStatuses.has(status);
}

// sameInstant compares two timestamps that may differ in serialization
// (Go RFC3339 vs Date.toISOString milliseconds); both absent also matches.
function sameInstant(a?: string, b?: string) {
  if (!a && !b) return true;
  if (!a || !b) return false;
  return new Date(a).getTime() === new Date(b).getTime();
}

// usePipelineStaleness fetches the staleness map for the current selection
// (environment + time range) and keeps it live through staleness.updated
// SSE events pushed after saves and run completions.
export function usePipelinesStaleness(pipelineIds: string[]): PipelinesStaleness {
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const selectedTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const stalenessEvent = useAtomValue(stalenessEventAtom);
  const pipelineKey = [...new Set(pipelineIds.filter(Boolean))].sort().join("\n");
  const stablePipelineIds = useMemo(
    () => (pipelineKey ? pipelineKey.split("\n") : []),
    [pipelineKey],
  );
  const selectionKey = useMemo(
    () =>
      JSON.stringify([
        pipelineKey,
        selectedEnvironment ?? "",
        selectedTimeWindow?.start ?? "",
        selectedTimeWindow?.end ?? "",
      ]),
    [pipelineKey, selectedEnvironment, selectedTimeWindow?.end, selectedTimeWindow?.start],
  );
  const [snapshot, setSnapshot] = useState<StalenessSnapshot | null>(null);
  const [requestState, setRequestState] = useState<StalenessRequestState>({
    selectionKey: "",
    loading: false,
    error: null,
  });

  useEffect(() => {
    if (stablePipelineIds.length === 0) {
      setSnapshot(null);
      setRequestState({ selectionKey, loading: false, error: null });
      return;
    }
    let cancelled = false;
    setRequestState({ selectionKey, loading: true, error: null });
    Promise.all(
      stablePipelineIds.map(async (pipelineId) => {
        const response = await getPipelineStaleness(pipelineId, {
          environment: selectedEnvironment,
          start: selectedTimeWindow?.start,
          end: selectedTimeWindow?.end,
        });
        return [pipelineId, response.assets ?? []] as const;
      }),
    )
      .then((entries) => {
        if (cancelled) return;
        setSnapshot({ selectionKey, assetsByPipelineId: Object.fromEntries(entries) });
        setRequestState({ selectionKey, loading: false, error: null });
      })
      .catch((cause) => {
        if (cancelled) return;
        setRequestState({
          selectionKey,
          loading: false,
          error:
            cause instanceof Error && cause.message
              ? cause.message
              : "Freshness could not be loaded.",
        });
      });
    return () => {
      cancelled = true;
    };
  }, [
    selectionKey,
    stablePipelineIds,
    selectedEnvironment,
    selectedTimeWindow?.start,
    selectedTimeWindow?.end,
  ]);

  useEffect(() => {
    if (!stalenessEvent || !stablePipelineIds.includes(stalenessEvent.pipeline_id)) return;
    // Discard pushes computed for a selection we have moved away from:
    // the fetch effect covers the new selection.
    if ((stalenessEvent.environment || "") !== (selectedEnvironment || "")) return;
    if (
      !sameInstant(stalenessEvent.start, selectedTimeWindow?.start) ||
      !sameInstant(stalenessEvent.end, selectedTimeWindow?.end)
    )
      return;
    setSnapshot((current) => ({
      selectionKey,
      assetsByPipelineId: {
        ...(current?.selectionKey === selectionKey ? current.assetsByPipelineId : {}),
        [stalenessEvent.pipeline_id]: stalenessEvent.assets ?? [],
      },
    }));
  }, [
    selectionKey,
    stablePipelineIds,
    selectedEnvironment,
    selectedTimeWindow?.start,
    selectedTimeWindow?.end,
    stalenessEvent,
  ]);

  return useMemo(() => {
    const assetsByPipelineId =
      snapshot?.selectionKey === selectionKey ? snapshot.assetsByPipelineId : {};
    const requestIsCurrent = requestState.selectionKey === selectionKey;
    const byPipelineId: Record<string, Record<string, AssetStaleness>> = {};
    for (const pipelineId of stablePipelineIds) {
      const byAssetName: Record<string, AssetStaleness> = {};
      for (const asset of assetsByPipelineId[pipelineId] ?? []) {
        byAssetName[asset.asset_name] = asset;
      }
      byPipelineId[pipelineId] = byAssetName;
    }
    return {
      byPipelineId,
      loading: stablePipelineIds.length > 0 && (!requestIsCurrent || requestState.loading),
      error: requestIsCurrent ? requestState.error : null,
    };
  }, [requestState, selectionKey, snapshot, stablePipelineIds]);
}

export function usePipelineStaleness(pipelineId: string | undefined): PipelineStaleness {
  const pipelines = usePipelinesStaleness(pipelineId ? [pipelineId] : []);
  return useMemo(() => {
    const byAssetName = pipelineId ? (pipelines.byPipelineId[pipelineId] ?? {}) : {};
    return {
      byAssetName,
      staleAssets: Object.values(byAssetName).filter((asset) => isStaleStatus(asset.status)),
      loading: pipelines.loading,
      error: pipelines.error,
    };
  }, [pipelineId, pipelines.byPipelineId, pipelines.error, pipelines.loading]);
}
