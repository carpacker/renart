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

const staleStatuses = new Set([
  "stale_edited",
  "stale_upstream",
  "partial",
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
export function usePipelineStaleness(pipelineId: string | undefined): PipelineStaleness {
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const selectedTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const stalenessEvent = useAtomValue(stalenessEventAtom);
  const [assets, setAssets] = useState<AssetStaleness[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!pipelineId) {
      setAssets([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    getPipelineStaleness(pipelineId, {
      environment: selectedEnvironment,
      start: selectedTimeWindow?.start,
      end: selectedTimeWindow?.end,
    })
      .then((response) => {
        if (!cancelled) setAssets(response.assets ?? []);
      })
      .catch(() => {
        if (!cancelled) setAssets([]);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [pipelineId, selectedEnvironment, selectedTimeWindow?.start, selectedTimeWindow?.end]);

  useEffect(() => {
    if (!pipelineId || !stalenessEvent) return;
    if (stalenessEvent.pipeline_id !== pipelineId) return;
    // Discard pushes computed for a selection we have moved away from:
    // the fetch effect covers the new selection.
    if ((stalenessEvent.environment || "") !== (selectedEnvironment || "")) return;
    if (
      !sameInstant(stalenessEvent.start, selectedTimeWindow?.start) ||
      !sameInstant(stalenessEvent.end, selectedTimeWindow?.end)
    )
      return;
    setAssets(stalenessEvent.assets ?? []);
  }, [
    pipelineId,
    selectedEnvironment,
    selectedTimeWindow?.start,
    selectedTimeWindow?.end,
    stalenessEvent,
  ]);

  return useMemo(() => {
    const byAssetName: Record<string, AssetStaleness> = {};
    for (const asset of assets) {
      byAssetName[asset.asset_name] = asset;
    }
    return {
      byAssetName,
      staleAssets: assets.filter((asset) => isStaleStatus(asset.status)),
      loading,
    };
  }, [assets, loading]);
}
