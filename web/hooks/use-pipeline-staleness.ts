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
};

export type PipelinesStaleness = {
  byPipelineId: Record<string, Record<string, AssetStaleness>>;
  loading: boolean;
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
  const [assetsByPipelineId, setAssetsByPipelineId] = useState<Record<string, AssetStaleness[]>>(
    {},
  );
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (stablePipelineIds.length === 0) {
      setAssetsByPipelineId({});
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
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
        if (!cancelled) setAssetsByPipelineId(Object.fromEntries(entries));
      })
      .catch(() => {
        if (!cancelled) setAssetsByPipelineId({});
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [stablePipelineIds, selectedEnvironment, selectedTimeWindow?.start, selectedTimeWindow?.end]);

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
    setAssetsByPipelineId((current) => ({
      ...current,
      [stalenessEvent.pipeline_id]: stalenessEvent.assets ?? [],
    }));
  }, [
    stablePipelineIds,
    selectedEnvironment,
    selectedTimeWindow?.start,
    selectedTimeWindow?.end,
    stalenessEvent,
  ]);

  return useMemo(() => {
    const byPipelineId: Record<string, Record<string, AssetStaleness>> = {};
    for (const pipelineId of stablePipelineIds) {
      const byAssetName: Record<string, AssetStaleness> = {};
      for (const asset of assetsByPipelineId[pipelineId] ?? []) {
        byAssetName[asset.asset_name] = asset;
      }
      byPipelineId[pipelineId] = byAssetName;
    }
    return { byPipelineId, loading };
  }, [assetsByPipelineId, loading, stablePipelineIds]);
}

export function usePipelineStaleness(pipelineId: string | undefined): PipelineStaleness {
  const pipelines = usePipelinesStaleness(pipelineId ? [pipelineId] : []);
  return useMemo(() => {
    const byAssetName = pipelineId ? (pipelines.byPipelineId[pipelineId] ?? {}) : {};
    return {
      byAssetName,
      staleAssets: Object.values(byAssetName).filter((asset) => isStaleStatus(asset.status)),
      loading: pipelines.loading,
    };
  }, [pipelineId, pipelines.byPipelineId, pipelines.loading]);
}
