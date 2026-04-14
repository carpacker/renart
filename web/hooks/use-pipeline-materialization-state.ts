"use client";

import { useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect } from "react";

import {
  enrichedPipelineAtom,
  materializationByAssetIdAtom,
  MaterializationByAssetId,
} from "@/lib/atoms/domains/results";
import { resolvedActivePipelineAtom } from "@/lib/atoms/domains/workspace";
import { getPipelineMaterialization } from "@/lib/api";
import { WebPipeline } from "@/lib/types";

export function usePipelineMaterializationState(): {
  enrichedPipeline: WebPipeline | null;
  refreshPipelineMaterialization: (pipelineId: string) => Promise<void>;
} {
  const activePipeline = useAtomValue(resolvedActivePipelineAtom);
  const enrichedPipeline = useAtomValue(enrichedPipelineAtom);
  const setMaterializationByAssetId = useSetAtom(materializationByAssetIdAtom);

  const refreshPipelineMaterialization = useCallback(
    async (pipelineId: string) => {
      const response = await getPipelineMaterialization(pipelineId);
      const mapped = response.assets.reduce<MaterializationByAssetId>(
        (acc, item) => {
          acc[item.asset_id] = {
            is_materialized: item.is_materialized,
            freshness_status: item.freshness_status,
            materialized_as: item.materialized_as,
            row_count: item.row_count,
            connection: item.connection,
            materialization_type: item.materialization_type,
          };
          return acc;
        },
        {}
      );

      setMaterializationByAssetId(mapped);
    },
    [setMaterializationByAssetId]
  );

  useEffect(() => {
    if (!activePipeline) {
      return;
    }

    let cancelled = false;
    const timer = window.setTimeout(() => {
      void refreshPipelineMaterialization(activePipeline).catch(() => {
        if (!cancelled) {
          setMaterializationByAssetId({});
        }
      });
    }, 0);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [
    activePipeline,
    refreshPipelineMaterialization,
    setMaterializationByAssetId,
  ]);

  return {
    enrichedPipeline,
    refreshPipelineMaterialization,
  };
}
