"use client";

import { useSetAtom } from "jotai";
import { useCallback, useEffect, useRef } from "react";

import { changedAssetIdsAtom } from "@/lib/atoms/domains/results";
import { updateAsset } from "@/lib/api";

type PendingAssetSave = {
  pipelineId: string;
  assetId: string;
  content: string;
};

export function useDebouncedAssetSave(delay = 500) {
  const setChangedAssetIds = useSetAtom(changedAssetIdsAtom);
  const timersByAssetRef = useRef<
    Record<string, ReturnType<typeof setTimeout>>
  >({});
  const pendingByAssetRef = useRef<Record<string, PendingAssetSave>>({});

  const runSaveNow = useCallback(
    async (pending: PendingAssetSave) => {
      try {
        await updateAsset(pending.pipelineId, pending.assetId, {
          content: pending.content,
        });

        setChangedAssetIds((prev: Set<string>) => {
          if (prev.has(pending.assetId)) {
            return prev;
          }

          const next = new Set(prev);
          next.add(pending.assetId);
          return next;
        });

        return true;
      } catch {
        return false;
      }
    },
    [setChangedAssetIds]
  );

  const flushAssetSave = useCallback(
    (assetId: string) => {
      const timer = timersByAssetRef.current[assetId];
      if (timer) {
        clearTimeout(timer);
        delete timersByAssetRef.current[assetId];
      }

      const pending = pendingByAssetRef.current[assetId];
      if (!pending) {
        return;
      }

      delete pendingByAssetRef.current[assetId];
      void runSaveNow(pending);
    },
    [runSaveNow]
  );

  const flushAllSaves = useCallback(() => {
    for (const assetId of Object.keys(timersByAssetRef.current)) {
      clearTimeout(timersByAssetRef.current[assetId]);
      delete timersByAssetRef.current[assetId];
    }

    for (const assetId of Object.keys(pendingByAssetRef.current)) {
      const pending = pendingByAssetRef.current[assetId];
      if (!pending) {
        continue;
      }

      delete pendingByAssetRef.current[assetId];
      void runSaveNow(pending);
    }
  }, [runSaveNow]);

  useEffect(() => {
    return () => {
      flushAllSaves();
    };
  }, [flushAllSaves]);

  const scheduleSave = useCallback(
    (pipelineId: string, assetId: string, content: string) => {
      pendingByAssetRef.current[assetId] = {
        pipelineId,
        assetId,
        content,
      };

      const previousTimer = timersByAssetRef.current[assetId];
      if (previousTimer) {
        clearTimeout(previousTimer);
      }

      timersByAssetRef.current[assetId] = setTimeout(() => {
        delete timersByAssetRef.current[assetId];
        const pending = pendingByAssetRef.current[assetId];
        if (!pending) {
          return;
        }

        delete pendingByAssetRef.current[assetId];
        void runSaveNow(pending);
      }, delay);
    },
    [delay, runSaveNow]
  );

  const hasPendingAssetSave = useCallback((assetId: string) => {
    return Boolean(pendingByAssetRef.current[assetId]);
  }, []);

  const saveAssetNow = useCallback(
    async (pipelineId: string, assetId: string, content: string) => {
      const timer = timersByAssetRef.current[assetId];
      if (timer) {
        clearTimeout(timer);
        delete timersByAssetRef.current[assetId];
      }

      delete pendingByAssetRef.current[assetId];

      return runSaveNow({
        pipelineId,
        assetId,
        content,
      });
    },
    [runSaveNow]
  );

  return {
    scheduleSave,
    flushAssetSave,
    flushAllSaves,
    hasPendingAssetSave,
    saveAssetNow,
  };
}
