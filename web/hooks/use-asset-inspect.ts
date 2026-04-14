"use client";

import { useAtom, useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo, useRef } from "react";

import { inspectAsset } from "@/lib/api";
import { assetInspectAtom, changedAssetIdsAtom } from "@/lib/atoms/domains/results";
import { normalizeInspectErrorMessage } from "@/lib/inspect-errors";
import { registerAssetColumnsAtom } from "@/lib/atoms/domains/suggestions";
import {
  getAssetViewMode,
  getTablePreviewLimit,
} from "@/lib/asset-visualization";
import { AssetInspectResponse, WebAsset } from "@/lib/types";

const inFlightInspectRequests = new Map<string, Promise<AssetInspectResponse>>();

function inspectFailure(error: unknown): AssetInspectResponse {
  const message = normalizeInspectErrorMessage(String(error));

  return {
    status: "error",
    columns: [],
    rows: [],
    raw_output: "",
    command: [],
    error: message || String(error),
  };
}

function getBaseLimitByAssetId(
  visualAssets: WebAsset[]
): Record<string, number> {
  const limits: Record<string, number> = {};
  for (const asset of visualAssets) {
    limits[asset.id] =
      getAssetViewMode(asset.meta) === "table"
        ? getTablePreviewLimit(asset.meta, 25)
        : 200;
  }
  return limits;
}

function normalizeRequests(requests: Array<{ id: string; limit: number }>) {
  const maxLimitByAssetId: Record<string, number> = {};
  for (const request of requests) {
    const currentLimit = maxLimitByAssetId[request.id] ?? 0;
    if (request.limit > currentLimit) {
      maxLimitByAssetId[request.id] = request.limit;
    }
  }

  return Object.entries(maxLimitByAssetId).map(([id, limit]) => ({ id, limit }));
}

function getInspectRequestKey(assetId: string, limit: number) {
  return `${assetId}:${limit}`;
}

export function useAssetInspect(visualAssets: WebAsset[] = []) {
  const [inspectState, setInspectState] = useAtom(assetInspectAtom);
  const [changedIds, setChangedIds] = useAtom(changedAssetIdsAtom);
  const registerAssetColumns = useSetAtom(registerAssetColumnsAtom);

  const { byAssetId, loadingByAssetId, requestedLimitsByAssetId } = inspectState;

  const assetIds = useMemo(
    () => visualAssets.map((asset) => asset.id).sort(),
    [visualAssets]
  );
  const assetById = useMemo(
    () => Object.fromEntries(visualAssets.map((asset) => [asset.id, asset])),
    [visualAssets]
  );
  const baseLimitByAssetId = useMemo(
    () => getBaseLimitByAssetId(visualAssets),
    [visualAssets]
  );

  const setChangedIdsRef = useRef(setChangedIds);
  useEffect(() => {
    setChangedIdsRef.current = setChangedIds;
  }, [setChangedIds]);

  const setRequestedLimit = useCallback(
    (assetId: string, limit: number) => {
      setInspectState((previous) => {
        const currentLimit = previous.requestedLimitsByAssetId[assetId];
        if (currentLimit !== undefined && currentLimit >= limit) {
          return previous;
        }

        return {
          ...previous,
          requestedLimitsByAssetId: {
            ...previous.requestedLimitsByAssetId,
            [assetId]: limit,
          },
        };
      });
    },
    [setInspectState]
  );

  const mergeInspectResults = useCallback(
    (
      results: Record<string, AssetInspectResponse>,
      fetchedLimitByAssetId: Record<string, number>
    ) => {
      if (Object.keys(results).length === 0) {
        return;
      }

      setInspectState((previous) => {
        const nextByAssetId = { ...previous.byAssetId };

        for (const [assetId, result] of Object.entries(results)) {
          const previousEntry = previous.byAssetId[assetId];
          const previousResult = previousEntry?.result;
          const nextResult =
            result.status === "error" && previousResult && previousResult.rows.length > 0
              ? {
                  ...previousResult,
                  status: previousResult.status,
                  warning: normalizeInspectErrorMessage(result.error) || previousResult.warning,
                  raw_output: result.raw_output,
                  command: result.command,
                  error: undefined,
                }
              : {
                  ...result,
                  error: normalizeInspectErrorMessage(result.error),
                  warning: undefined,
                };

          nextByAssetId[assetId] = {
            result: nextResult,
            fetchedLimit: fetchedLimitByAssetId[assetId] ?? result.rows.length,
            diagnosticSnapshot: previousEntry?.diagnosticSnapshot,
          };
        }

        return {
          ...previous,
          byAssetId: nextByAssetId,
        };
      });

      for (const [assetId, result] of Object.entries(results)) {
        registerAssetColumns({
          assetId,
          method: "asset-inspect",
          columns: (result.columns ?? []).map((name) => ({ name })),
        });
      }
    },
    [registerAssetColumns, setInspectState]
  );

  const setLoading = useCallback(
    (assetIdsToUpdate: string[], isLoading: boolean) => {
      if (assetIdsToUpdate.length === 0) {
        return;
      }

      setInspectState((previous) => {
        const nextLoadingByAssetId = { ...previous.loadingByAssetId };
        for (const assetId of assetIdsToUpdate) {
          if (isLoading) {
            nextLoadingByAssetId[assetId] = true;
          } else {
            delete nextLoadingByAssetId[assetId];
          }
        }

        return {
          ...previous,
          loadingByAssetId: nextLoadingByAssetId,
        };
      });
    },
    [setInspectState]
  );

  const fetchInspectRequests = useCallback(
    async (
      requests: Array<{ id: string; limit: number }>,
      options?: { force?: boolean }
    ) => {
      const normalizedRequests = normalizeRequests(requests);
      const requestsToFetch = normalizedRequests.filter(
        ({ id, limit }) => options?.force || limit > (byAssetId[id]?.fetchedLimit ?? 0)
      );

      if (requestsToFetch.length === 0) {
        return {} as Record<string, AssetInspectResponse>;
      }

      const assetIdsToFetch = requestsToFetch.map((request) => request.id);
      setLoading(assetIdsToFetch, true);

      try {
        const results = await Promise.all(
          requestsToFetch.map(async ({ id, limit }) => {
            const requestKey = getInspectRequestKey(id, limit);
            const existingRequest = inFlightInspectRequests.get(requestKey);

            if (existingRequest) {
              return [id, await existingRequest] as const;
            }

            const request = (async () => {
              try {
                return await inspectAsset(id, { limit });
              } catch (error) {
                return inspectFailure(error);
              } finally {
                inFlightInspectRequests.delete(requestKey);
              }
            })();

            inFlightInspectRequests.set(requestKey, request);

            try {
              const result = await request;
              return [id, result] as const;
            } catch (error) {
              return [id, inspectFailure(error)] as const;
            }
          })
        );

        const resultByAssetId = Object.fromEntries(results);
        const fetchedLimitByAssetId = Object.fromEntries(
          requestsToFetch.map(({ id, limit }) => [id, limit])
        );

        mergeInspectResults(resultByAssetId, fetchedLimitByAssetId);
        return resultByAssetId;
      } finally {
        setLoading(assetIdsToFetch, false);
      }
    },
    [byAssetId, mergeInspectResults, setLoading]
  );

  const inspectAssetById = useCallback(
    async (
      assetId: string,
      options?: { force?: boolean; limit?: number; contentSnapshot?: string }
    ): Promise<AssetInspectResponse> => {
      const limit =
        options?.limit ??
        requestedLimitsByAssetId[assetId] ??
        baseLimitByAssetId[assetId] ??
        200;

      setRequestedLimit(assetId, limit);

      const cachedEntry = byAssetId[assetId];
      if (!options?.force && cachedEntry && cachedEntry.fetchedLimit >= limit) {
        return cachedEntry.result;
      }

      const results = await fetchInspectRequests([{ id: assetId, limit }], {
        force: true,
      });
      const result = results[assetId] ?? inspectFailure("Inspect request failed.");

      const contentSnapshot = options?.contentSnapshot;
      if (contentSnapshot !== undefined) {
        setInspectState((previous) => {
          const entry = previous.byAssetId[assetId];
          if (!entry) {
            return previous;
          }

          return {
            ...previous,
            byAssetId: {
              ...previous.byAssetId,
              [assetId]: {
                ...entry,
                diagnosticSnapshot: {
                  assetId,
                  content: contentSnapshot,
                  inspect: result,
                },
              },
            },
          };
        });
      }

      return result;
    },
    [
      baseLimitByAssetId,
      byAssetId,
      fetchInspectRequests,
      requestedLimitsByAssetId,
      setInspectState,
      setRequestedLimit,
    ]
  );

  useEffect(() => {
    for (const [assetId, baseLimit] of Object.entries(baseLimitByAssetId)) {
      setRequestedLimit(assetId, baseLimit);
    }
  }, [baseLimitByAssetId, setRequestedLimit]);

  const requestLimits = useMemo(() => {
    const limits: Record<string, number> = {};
    for (const assetId of new Set([...assetIds, ...Object.keys(requestedLimitsByAssetId)])) {
      limits[assetId] =
        requestedLimitsByAssetId[assetId] ?? baseLimitByAssetId[assetId] ?? 200;
    }
    return limits;
  }, [assetIds, baseLimitByAssetId, requestedLimitsByAssetId]);

  useEffect(() => {
    const missingRequests = assetIds
      .filter((assetId) => !byAssetId[assetId])
      .map((assetId) => ({ id: assetId, limit: requestLimits[assetId] ?? 200 }));

    void fetchInspectRequests(missingRequests);
  }, [assetIds, byAssetId, fetchInspectRequests, requestLimits]);

  useEffect(() => {
    const expandedRequests = assetIds
      .filter(
        (assetId) =>
          Boolean(byAssetId[assetId]) &&
          (requestLimits[assetId] ?? 0) > (byAssetId[assetId]?.fetchedLimit ?? 0)
      )
      .map((assetId) => ({ id: assetId, limit: requestLimits[assetId] }));

    void fetchInspectRequests(expandedRequests);
  }, [assetIds, byAssetId, fetchInspectRequests, requestLimits]);

  const relevantChangedKey = useMemo(
    () =>
      Array.from(changedIds)
        .filter((assetId) => assetIds.includes(assetId))
        .sort()
        .join(","),
    [assetIds, changedIds]
  );

  useEffect(() => {
    if (!relevantChangedKey) {
      return;
    }

    const assetIdsToRefresh = relevantChangedKey.split(",").filter(Boolean);

    setChangedIdsRef.current((previous: Set<string>) => {
      const next = new Set(previous);
      let removed = false;
      for (const assetId of assetIdsToRefresh) {
        if (next.delete(assetId)) {
          removed = true;
        }
      }
      return removed ? next : previous;
    });

    void fetchInspectRequests(
      assetIdsToRefresh.map((assetId) => ({
        id: assetId,
        limit: requestLimits[assetId] ?? baseLimitByAssetId[assetId] ?? 200,
      })),
      { force: true }
    );
  }, [baseLimitByAssetId, fetchInspectRequests, relevantChangedKey, requestLimits]);

  const inspectByAssetId = useMemo<Record<string, AssetInspectResponse>>(() => {
    const next: Record<string, AssetInspectResponse> = {};
    for (const assetId of Object.keys(byAssetId)) {
      const entry = byAssetId[assetId];
      if (entry) {
        next[assetId] = entry.result;
      }
    }
    return next;
  }, [byAssetId]);

  const inspectDiagnosticSnapshotByAssetId = useMemo(() => {
    const next: Record<string, NonNullable<(typeof byAssetId)[string]["diagnosticSnapshot"]>> = {};
    for (const assetId of Object.keys(byAssetId)) {
      const snapshot = byAssetId[assetId]?.diagnosticSnapshot;
      if (snapshot) {
        next[assetId] = snapshot;
      }
    }
    return next;
  }, [byAssetId]);

  const inspectLoadingByAssetId = useMemo<Record<string, boolean>>(() => {
    const next: Record<string, boolean> = {};
    for (const assetId of Object.keys(loadingByAssetId)) {
      if (loadingByAssetId[assetId]) {
        next[assetId] = true;
      }
    }
    return next;
  }, [loadingByAssetId]);

  const clearPreviewForAsset = useCallback(
    (assetId: string) => {
      setInspectState((previous) => {
        const nextByAssetId = { ...previous.byAssetId };
        const nextLoadingByAssetId = { ...previous.loadingByAssetId };
        const nextRequestedLimitsByAssetId = {
          ...previous.requestedLimitsByAssetId,
        };

        delete nextByAssetId[assetId];
        delete nextLoadingByAssetId[assetId];
        delete nextRequestedLimitsByAssetId[assetId];

        return {
          ...previous,
          byAssetId: nextByAssetId,
          loadingByAssetId: nextLoadingByAssetId,
          requestedLimitsByAssetId: nextRequestedLimitsByAssetId,
        };
      });
    },
    [setInspectState]
  );

  const canLoadMoreByAssetId = useMemo<Record<string, boolean>>(() => {
    const next: Record<string, boolean> = {};
    for (const assetId of Object.keys(byAssetId)) {
      const asset = assetById[assetId];
      const entry = byAssetId[assetId];
      if (!entry) {
        continue;
      }

      if (asset && getAssetViewMode(asset.meta) !== "table") {
        continue;
      }

      const requestedLimit =
        requestedLimitsByAssetId[assetId] ?? baseLimitByAssetId[assetId] ?? 25;
      if (entry.result.rows.length >= requestedLimit) {
        next[assetId] = true;
      }
    }
    return next;
  }, [assetById, baseLimitByAssetId, byAssetId, requestedLimitsByAssetId]);

  const loadMorePreviewRows = useCallback(
    (assetId: string) => {
      const baseLimit = baseLimitByAssetId[assetId] ?? 25;
      const currentLimit = requestedLimitsByAssetId[assetId] ?? baseLimit;
      setRequestedLimit(assetId, currentLimit + baseLimit);
    },
    [baseLimitByAssetId, requestedLimitsByAssetId, setRequestedLimit]
  );

  const refreshAssets = useCallback(
    async (assetIdsToRefresh: string[]) => {
      await fetchInspectRequests(
        assetIdsToRefresh.map((assetId) => ({
          id: assetId,
          limit: requestLimits[assetId] ?? baseLimitByAssetId[assetId] ?? 200,
        })),
        { force: true }
      );
    },
    [baseLimitByAssetId, fetchInspectRequests, requestLimits]
  );

  const getRowsForAsset = useCallback(
    (assetId: string, limit?: number): Record<string, unknown>[] => {
      const entry = byAssetId[assetId];
      if (!entry) {
        return [];
      }

      if (limit === undefined) {
        return entry.result.rows;
      }

      return entry.result.rows.slice(0, limit);
    },
    [byAssetId]
  );

  return {
    inspectByAssetId,
    inspectDiagnosticSnapshotByAssetId,
    inspectLoadingByAssetId,
    canLoadMoreByAssetId,
    loadMorePreviewRows,
    clearPreviewForAsset,
    inspectAssetById,
    refreshAssets,
    getRowsForAsset,
    requestLimits,
  };
}
