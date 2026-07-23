"use client";

import { useSetAtom } from "jotai";
import { useCallback, useRef } from "react";

import { updateAsset } from "@/lib/api-assets-crud";
import { changedAssetIdsAtom } from "@/lib/atoms/domains/results";

type PendingAssetSave = {
  pipelineId: string;
  assetId: string;
  content: string;
};

function throwSaveFailures(failures: unknown[]): void {
  if (failures.length === 1) {
    throw failures[0];
  }
  if (failures.length > 1) {
    throw new AggregateError(failures, "Could not save all edited assets");
  }
}

export function useDebouncedAssetSave(delay = 500) {
  const setChangedAssetIds = useSetAtom(changedAssetIdsAtom);
  const timersByAssetRef = useRef<Record<string, ReturnType<typeof setTimeout>>>({});
  const pendingByAssetRef = useRef<Record<string, PendingAssetSave>>({});
  const inFlightByAssetRef = useRef<Record<string, Promise<void>>>({});
  const inFlightSavesRef = useRef(new Set<Promise<void>>());
  const failuresByAssetRef = useRef(new Map<string, unknown>());
  const saveSequenceByAssetRef = useRef<Record<string, number>>({});

  const runSaveNow = useCallback(
    async (pending: PendingAssetSave) => {
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
    },
    [setChangedAssetIds],
  );

  const queueSave = useCallback(
    (pending: PendingAssetSave, restoreOnFailure = true) => {
      const saveSequence = (saveSequenceByAssetRef.current[pending.assetId] ?? 0) + 1;
      saveSequenceByAssetRef.current[pending.assetId] = saveSequence;
      const previous = inFlightByAssetRef.current[pending.assetId];
      const save = (previous ? previous.catch(() => undefined) : Promise.resolve()).then(
        async () => {
          try {
            await runSaveNow(pending);
            failuresByAssetRef.current.delete(pending.assetId);
          } catch (error) {
            failuresByAssetRef.current.set(pending.assetId, error);
            // Keep the unsaved value available for an explicit retry. Never
            // replace a newer edit that was scheduled while this request ran.
            if (
              restoreOnFailure &&
              saveSequenceByAssetRef.current[pending.assetId] === saveSequence &&
              !pendingByAssetRef.current[pending.assetId]
            ) {
              pendingByAssetRef.current[pending.assetId] = pending;
            }
            throw error;
          }
        },
      );

      const finish = (completed: Promise<void>) => {
        inFlightSavesRef.current.delete(completed);
        if (inFlightByAssetRef.current[pending.assetId] === completed) {
          delete inFlightByAssetRef.current[pending.assetId];
        }
      };
      const tracked: Promise<void> = save.then(
        () => finish(tracked),
        (error) => {
          finish(tracked);
          throw error;
        },
      );

      inFlightByAssetRef.current[pending.assetId] = tracked;
      inFlightSavesRef.current.add(tracked);
      // Timer/unmount initiated saves do not have an immediate caller. The
      // failure remains recorded above and is rethrown by awaitAllSaves.
      void tracked.catch(() => undefined);
      return tracked;
    },
    [runSaveNow],
  );

  const startPendingAssetSave = useCallback(
    (assetId: string) => {
      const timer = timersByAssetRef.current[assetId];
      if (timer) {
        clearTimeout(timer);
        delete timersByAssetRef.current[assetId];
      }

      const pending = pendingByAssetRef.current[assetId];
      if (!pending) {
        return undefined;
      }

      delete pendingByAssetRef.current[assetId];
      return queueSave(pending);
    },
    [queueSave],
  );

  const flushAssetSave = useCallback(
    async (assetId: string) => {
      const started = startPendingAssetSave(assetId);
      const active = started ?? inFlightByAssetRef.current[assetId];
      if (active) {
        await active;
      }
      if (failuresByAssetRef.current.has(assetId)) {
        throw failuresByAssetRef.current.get(assetId);
      }
    },
    [startPendingAssetSave],
  );

  const startAllPendingSaves = useCallback(() => {
    for (const assetId of Object.keys(timersByAssetRef.current)) {
      clearTimeout(timersByAssetRef.current[assetId]);
      delete timersByAssetRef.current[assetId];
    }

    for (const assetId of Object.keys(pendingByAssetRef.current)) {
      startPendingAssetSave(assetId);
    }
  }, [startPendingAssetSave]);

  const awaitAllSaves = useCallback(async () => {
    while (true) {
      startAllPendingSaves();
      const active = Array.from(inFlightSavesRef.current);
      if (active.length > 0) {
        await Promise.allSettled(active);
      }

      // A newer queued value can successfully supersede an earlier failed
      // request. Only failures that remain after the queue drains are fatal.
      throwSaveFailures(Array.from(failuresByAssetRef.current.values()));

      if (
        Object.keys(pendingByAssetRef.current).length === 0 &&
        inFlightSavesRef.current.size === 0
      ) {
        return;
      }
    }
  }, [startAllPendingSaves]);

  const flushAllSaves = awaitAllSaves;

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
        void startPendingAssetSave(assetId)?.catch(() => undefined);
      }, delay);
    },
    [delay, startPendingAssetSave],
  );

  const hasPendingAssetSave = useCallback((assetId: string) => {
    return Boolean(pendingByAssetRef.current[assetId] || inFlightByAssetRef.current[assetId]);
  }, []);

  const saveAssetNow = useCallback(
    async (pipelineId: string, assetId: string, content: string) => {
      const timer = timersByAssetRef.current[assetId];
      if (timer) {
        clearTimeout(timer);
        delete timersByAssetRef.current[assetId];
      }

      delete pendingByAssetRef.current[assetId];

      try {
        await queueSave({ pipelineId, assetId, content }, false);
        return true;
      } catch {
        return false;
      }
    },
    [queueSave],
  );

  return {
    scheduleSave,
    flushAssetSave,
    flushAllSaves,
    awaitAllSaves,
    hasPendingAssetSave,
    saveAssetNow,
  };
}
