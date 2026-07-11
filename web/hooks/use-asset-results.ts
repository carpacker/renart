"use client";

import AnsiToHtml from "ansi-to-html";
import { useAtom, useAtomValue, useSetAtom } from "jotai";
import { useCallback, useEffect, useMemo, useState } from "react";

import {
  assetResultsAtom,
  changedAssetIdsAtom,
  enrichedSelectedAssetAtom,
  materializingAssetIdsAtom,
  schedulerRunEventAtom,
} from "@/lib/atoms/domains/results";
import {
  pipelineAtom,
  resolvedSelectedAssetAtom,
  selectedExecutionTimeWindowAtom,
  selectedEnvironmentAtom,
} from "@/lib/atoms/domains/workspace";
import { useAssetInspect } from "@/hooks/use-asset-inspect";
import { materializeAssetStream, materializePipelineStream, triggerPipelineRun } from "@/lib/api";
import { buildStalePipelineStream } from "@/lib/api-staleness";
import type { StreamAssetEvent } from "@/lib/api-streams";
import { MaterializeScope, labelForMaterializeScope } from "@/lib/materialize-scope";
import { AssetInspectResponse, WebAsset } from "@/lib/types";

let nextMaterializeHistoryId = 0;

function createMaterializeHistoryId() {
  nextMaterializeHistoryId += 1;
  return `materialize-${Date.now()}-${nextMaterializeHistoryId}`;
}

function createMaterializeEntry(input: {
  id: string;
  kind: "asset" | "pipeline" | "batch";
  label: string;
  assetId?: string | null;
  assetName?: string | null;
  pipelineId?: string | null;
  pipelineName?: string | null;
  runId?: string | null;
  output?: string;
  status?: "ok" | "error" | null;
  error?: string;
  loading?: boolean;
  createdAt: number;
  updatedAt?: number;
  timeWindow?: { start: string; end: string } | null;
}) {
  return {
    id: input.id,
    kind: input.kind,
    label: input.label,
    assetId: input.assetId,
    assetName: input.assetName,
    pipelineId: input.pipelineId,
    pipelineName: input.pipelineName,
    runId: input.runId,
    output: input.output ?? "",
    status: input.status ?? null,
    error: input.error ?? "",
    loading: input.loading ?? false,
    createdAt: input.createdAt,
    updatedAt: input.updatedAt ?? input.createdAt,
    timeWindow: input.timeWindow ?? null,
  };
}

function resolveScopedMaterializingAssetIds(
  assets: WebAsset[],
  assetId: string,
  scope: MaterializeScope,
) {
  const selected = assets.find((candidate) => candidate.id === assetId) ?? null;
  if (!selected || scope === "asset") {
    return [assetId];
  }

  const assetByName = new Map(assets.map((candidate) => [candidate.name, candidate]));
  const downstreamByName = new Map<string, WebAsset[]>();
  for (const candidate of assets) {
    for (const upstreamName of candidate.upstreams ?? []) {
      const downstream = downstreamByName.get(upstreamName) ?? [];
      downstream.push(candidate);
      downstreamByName.set(upstreamName, downstream);
    }
  }

  const selectedNames = new Set<string>();
  const queue = [selected.name];
  while (queue.length > 0) {
    const name = queue.shift();
    if (!name || selectedNames.has(name)) {
      continue;
    }
    selectedNames.add(name);

    const current = assetByName.get(name);
    if (!current) {
      continue;
    }

    if (scope === "asset_with_upstreams" || scope === "asset_with_upstreams_and_downstreams") {
      for (const upstreamName of current.upstreams ?? []) {
        if (assetByName.has(upstreamName)) {
          queue.push(upstreamName);
        }
      }
    }

    if (scope === "asset_with_downstreams" || scope === "asset_with_upstreams_and_downstreams") {
      for (const downstream of downstreamByName.get(name) ?? []) {
        queue.push(downstream.name);
      }
    }
  }

  const ids = assets
    .filter((candidate) => selectedNames.has(candidate.name))
    .map((candidate) => candidate.id);
  return ids.length > 0 ? ids : [assetId];
}

export function useAssetResults() {
  const [results, setResults] = useAtom(assetResultsAtom);
  const setChangedAssetIds = useSetAtom(changedAssetIdsAtom);
  const [pipelineMaterializeLoading, setPipelineMaterializeLoading] = useState(false);
  const [assetMaterializeLoading, setAssetMaterializeLoading] = useState(false);
  const [materializingAssetIds, setMaterializingAssetIds] = useAtom(materializingAssetIdsAtom);
  const asset = useAtomValue(enrichedSelectedAssetAtom);
  const pipeline = useAtomValue(pipelineAtom);
  const selectedEnvironment = useAtomValue(selectedEnvironmentAtom);
  const pipelineId = pipeline?.id ?? null;
  const selectedAssetId = useAtomValue(resolvedSelectedAssetAtom);
  const selectedExecutionTimeWindow = useAtomValue(selectedExecutionTimeWindowAtom);
  const schedulerRunEvent = useAtomValue(schedulerRunEventAtom);
  const inspectAssets = useMemo(() => (asset ? [asset] : []), [asset]);
  const {
    inspectAssetById,
    inspectByAssetId,
    inspectDiagnosticSnapshotByAssetId,
    inspectLoadingByAssetId,
    canLoadMoreByAssetId,
    loadMorePreviewRows,
  } = useAssetInspect(inspectAssets);
  const { resultTab, selectedMaterializeEntryId, materializeHistory } = results;

  const inspectResult = selectedAssetId ? (inspectByAssetId[selectedAssetId] ?? null) : null;
  const inspectLoading = selectedAssetId
    ? (inspectLoadingByAssetId[selectedAssetId] ?? false)
    : false;
  const canLoadMoreInspectRows = selectedAssetId
    ? Boolean(canLoadMoreByAssetId[selectedAssetId])
    : false;

  const effectiveMaterializeLoading = assetMaterializeLoading || pipelineMaterializeLoading;

  const setResultTab = (tab: "inspect" | "materialize") => {
    setResults((previous) => ({
      ...previous,
      resultTab: tab,
    }));
  };

  const selectMaterializeEntry = (entryId: string) => {
    const entry = materializeHistory.find((item) => item.id === entryId);
    if (!entry) {
      return;
    }

    setResults((previous) => ({
      ...previous,
      resultTab: "materialize",
      selectedMaterializeEntryId: entryId,
    }));
  };

  const hasInspectData = Boolean(inspectResult);
  const selectedMaterializeEntry =
    materializeHistory.find((entry) => entry.id === selectedMaterializeEntryId) ?? null;
  const hasResultData =
    hasInspectData ||
    resultTab === "materialize" ||
    materializeHistory.length > 0 ||
    inspectLoading ||
    effectiveMaterializeLoading;

  const ansiConverter = useMemo(() => new AnsiToHtml({ escapeXML: true }), []);
  const materializeOutputHtml = useMemo(() => {
    const normalized = (selectedMaterializeEntry?.output ?? "").replace(/\r\n/g, "\n");
    return ansiConverter.toHtml(normalized);
  }, [ansiConverter, selectedMaterializeEntry?.output]);

  const effectiveResultTab = useMemo(() => {
    if (resultTab === "inspect" && !hasInspectData && materializeHistory.length > 0) {
      return "materialize" as const;
    }

    return resultTab;
  }, [hasInspectData, materializeHistory.length, resultTab]);

  const loadMoreInspectRows = () => {
    if (selectedAssetId) {
      loadMorePreviewRows(selectedAssetId);
    }
  };

  const upsertMaterializeEntry = (
    entryId: string,
    updater: (
      previous: (typeof materializeHistory)[number] | null,
    ) => (typeof materializeHistory)[number],
  ) => {
    setResults((previous) => {
      const existingEntry =
        previous.materializeHistory.find((entry) => entry.id === entryId) ?? null;
      const nextEntry = updater(existingEntry);
      const nextHistory = [
        nextEntry,
        ...previous.materializeHistory.filter((entry) => entry.id !== entryId),
      ].sort((left, right) => right.updatedAt - left.updatedAt);

      return {
        ...previous,
        resultTab: "materialize",
        selectedMaterializeEntryId: entryId,
        materializeHistory: nextHistory,
      };
    });
  };

  useEffect(() => {
    if (!schedulerRunEvent) {
      return;
    }

    setResults((previous) => {
      let matched = false;
      const nextHistory = previous.materializeHistory.map((entry) => {
        if (schedulerRunEvent.type === "run.log") {
          if (entry.runId !== schedulerRunEvent.run.run_id) {
            return entry;
          }
          matched = true;
          const line = schedulerRunEvent.run.log.line;
          return {
            ...entry,
            output: `${entry.output}${entry.output ? "\n" : ""}${line}`,
            loading: true,
            updatedAt: Date.now(),
          };
        }

        if (schedulerRunEvent.type === "run.step") {
          return entry;
        }

        if (entry.runId !== schedulerRunEvent.run.id) {
          return entry;
        }
        matched = true;
        if (schedulerRunEvent.type === "run.finished") {
          const status: "ok" | "error" =
            schedulerRunEvent.run.status === "success" ? "ok" : "error";
          return {
            ...entry,
            status,
            error: schedulerRunEvent.run.error ?? "",
            loading: false,
            updatedAt: Date.now(),
          };
        }
        return {
          ...entry,
          output: `${entry.output}${entry.output ? "\n" : ""}${schedulerRunEvent.run.status === "running" ? "Run started." : "Run queued."}`,
          loading: true,
          updatedAt: Date.now(),
        };
      });

      if (!matched) {
        return previous;
      }

      return {
        ...previous,
        materializeHistory: nextHistory,
      };
    });
  }, [schedulerRunEvent, setResults]);

  const runInspectForAsset = useCallback(
    async (assetId: string, contentSnapshot?: string) => {
      try {
        const result = await inspectAssetById(assetId, {
          force: true,
          limit: 200,
          contentSnapshot,
          timeWindow: selectedExecutionTimeWindow ?? undefined,
        });
        if (result.rows.length > 0 || result.error) {
          setResultTab("inspect");
        }
        return result;
      } catch (error) {
        const failure: AssetInspectResponse = {
          status: "error",
          columns: [],
          rows: [],
          raw_output: "",
          operation: { type: "inspect" },
          error: String(error),
        };
        setResultTab("inspect");
        return failure;
      }
    },
    [inspectAssetById, selectedExecutionTimeWindow],
  );

  const runMaterializeForAsset = useCallback(
    async (
      assetId: string,
      scope: MaterializeScope = "asset",
      refresh?: () => Promise<void> | void,
      overrides?: {
        assetName?: string;
        timeWindow?: { start: string; end: string } | null;
        fullRefresh?: boolean;
      },
    ) => {
      const entryId = createMaterializeHistoryId();
      const startedAt = Date.now();
      // Resolve the asset being built rather than assuming it is the selected one:
      // the stale-build flow materializes assets other than the active selection,
      // and the entry must carry the correct name, label and time window.
      const targetAssetName =
        overrides?.assetName ??
        pipeline?.assets.find((candidate) => candidate.id === assetId)?.name ??
        asset?.name ??
        null;
      const entryTimeWindow =
        overrides?.timeWindow !== undefined ? overrides.timeWindow : selectedExecutionTimeWindow;
      const actionLabel = overrides?.fullRefresh ? "Full refresh" : labelForMaterializeScope(scope);
      const materializeLabel = targetAssetName ? `${actionLabel}: ${targetAssetName}` : actionLabel;
      const scopedMaterializingIds = resolveScopedMaterializingAssetIds(
        pipeline?.assets ?? [],
        assetId,
        scope,
      );

      setAssetMaterializeLoading(true);
      setMaterializingAssetIds(
        (previous: Set<string>) => new Set([...previous, ...scopedMaterializingIds]),
      );
      upsertMaterializeEntry(entryId, () => ({
        ...createMaterializeEntry({
          id: entryId,
          kind: "asset",
          label: materializeLabel,
          assetId,
          assetName: targetAssetName,
          pipelineId: pipelineId ?? null,
          pipelineName: pipeline?.name ?? null,
          loading: true,
          createdAt: startedAt,
          timeWindow: entryTimeWindow,
        }),
      }));

      try {
        const result = await materializeAssetStream(
          assetId,
          {
            onChunk: (chunk) => {
              upsertMaterializeEntry(entryId, (previous) => ({
                ...(previous ??
                  createMaterializeEntry({
                    id: entryId,
                    kind: "asset",
                    label: materializeLabel,
                    assetId,
                    assetName: targetAssetName,
                    pipelineId: pipelineId ?? null,
                    pipelineName: pipeline?.name ?? null,
                    loading: true,
                    createdAt: startedAt,
                    timeWindow: entryTimeWindow,
                  })),
                output: (previous?.output ?? "") + chunk,
                loading: true,
                updatedAt: Date.now(),
              }));
            },
          },
          {
            environment: selectedEnvironment,
            scope,
            timeWindow: entryTimeWindow ?? undefined,
            fullRefresh: overrides?.fullRefresh,
          },
        );
        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ??
            createMaterializeEntry({
              id: entryId,
              kind: "asset",
              label: materializeLabel,
              assetId,
              assetName: targetAssetName,
              pipelineId: pipelineId ?? null,
              pipelineName: pipeline?.name ?? null,
              loading: true,
              createdAt: startedAt,
              timeWindow: entryTimeWindow,
            })),
          output: result.output ?? previous?.output ?? "",
          status: result.status ?? "error",
          error: result.error ?? "",
          warnings: result.warnings ?? [],
          loading: false,
          updatedAt: Date.now(),
        }));

        const affectedIds = result.changed_asset_ids;
        if (affectedIds && affectedIds.length > 0) {
          setChangedAssetIds((prev: Set<string>) => {
            const next = new Set(prev);
            for (const id of affectedIds) {
              next.add(id);
            }
            return next;
          });
        } else {
          setChangedAssetIds((prev: Set<string>) => new Set([...prev, assetId]));
        }

        return result;
      } catch (error) {
        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ??
            createMaterializeEntry({
              id: entryId,
              kind: "asset",
              label: materializeLabel,
              assetId,
              assetName: targetAssetName,
              pipelineId: pipelineId ?? null,
              pipelineName: pipeline?.name ?? null,
              loading: true,
              createdAt: startedAt,
              timeWindow: entryTimeWindow,
            })),
          output: (previous?.output ?? "") + (previous?.output ? "\n" : "") + String(error),
          status: "error",
          error: String(error),
          loading: false,
          updatedAt: Date.now(),
        }));
        return null;
      } finally {
        setAssetMaterializeLoading(false);
        setMaterializingAssetIds((previous: Set<string>) => {
          const next = new Set(previous);
          for (const id of scopedMaterializingIds) {
            next.delete(id);
          }
          return next;
        });
        if (refresh) {
          await refresh();
        }
      }
    },
    [
      asset?.name,
      pipeline,
      pipelineId,
      selectedEnvironment,
      selectedExecutionTimeWindow,
      setChangedAssetIds,
      setResultTab,
    ],
  );

  const runMaterializePipeline = useCallback(
    async (
      pipelineId: string,
      refresh?: () => Promise<void> | void,
      options?: { dryRun?: boolean },
    ) => {
      const entryId = createMaterializeHistoryId();
      const startedAt = Date.now();
      const pipelineMaterializingIds = pipeline?.assets.map((current) => current.id) ?? [];

      setPipelineMaterializeLoading(true);
      if (!options?.dryRun) {
        setMaterializingAssetIds(
          (previous: Set<string>) => new Set([...previous, ...pipelineMaterializingIds]),
        );
      }
      upsertMaterializeEntry(entryId, () => ({
        ...createMaterializeEntry({
          id: entryId,
          kind: "pipeline",
          label: pipeline?.name
            ? `${options?.dryRun ? "Dry run" : "Pipeline"}: ${pipeline.name}`
            : options?.dryRun
              ? "Pipeline dry run"
              : "Pipeline materialize",
          pipelineId,
          pipelineName: pipeline?.name ?? null,
          loading: true,
          createdAt: startedAt,
          timeWindow: selectedExecutionTimeWindow,
        }),
      }));

      try {
        if (!options?.dryRun) {
          const response = await triggerPipelineRun(pipelineId, {
            environment: selectedEnvironment,
            start: selectedExecutionTimeWindow?.start,
            end: selectedExecutionTimeWindow?.end,
            trigger: "manual",
          });
          const run = response.run;
          upsertMaterializeEntry(entryId, (previous) => ({
            ...(previous ??
              createMaterializeEntry({
                id: entryId,
                kind: "pipeline",
                label: pipeline?.name ? `Pipeline: ${pipeline.name}` : "Pipeline materialize",
                pipelineId,
                pipelineName: pipeline?.name ?? null,
                runId: run.id,
                loading: true,
                createdAt: startedAt,
                timeWindow: selectedExecutionTimeWindow,
              })),
            runId: run.id,
            output: `Queued manual River run ${run.id} for ${run.pipeline || pipeline?.name || "pipeline"}.`,
            status: null,
            error: "",
            loading: true,
            updatedAt: Date.now(),
          }));
          return { status: "ok", output: "", error: "", changed_asset_ids: [] };
        }

        const result = await materializePipelineStream(
          pipelineId,
          {
            onChunk: (chunk) => {
              upsertMaterializeEntry(entryId, (previous) => ({
                ...(previous ??
                  createMaterializeEntry({
                    id: entryId,
                    kind: "pipeline",
                    label: pipeline?.name
                      ? `${options?.dryRun ? "Dry run" : "Pipeline"}: ${pipeline.name}`
                      : options?.dryRun
                        ? "Pipeline dry run"
                        : "Pipeline materialize",
                    pipelineId,
                    pipelineName: pipeline?.name ?? null,
                    loading: true,
                    createdAt: startedAt,
                    timeWindow: selectedExecutionTimeWindow,
                  })),
                output: (previous?.output ?? "") + chunk,
                loading: true,
                updatedAt: Date.now(),
              }));
            },
          },
          {
            environment: selectedEnvironment,
            dryRun: options?.dryRun,
            timeWindow: selectedExecutionTimeWindow ?? undefined,
          },
        );

        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ??
            createMaterializeEntry({
              id: entryId,
              kind: "pipeline",
              label: pipeline?.name
                ? `${options?.dryRun ? "Dry run" : "Pipeline"}: ${pipeline.name}`
                : options?.dryRun
                  ? "Pipeline dry run"
                  : "Pipeline materialize",
              pipelineId,
              pipelineName: pipeline?.name ?? null,
              loading: true,
              createdAt: startedAt,
              timeWindow: selectedExecutionTimeWindow,
            })),
          output: result.output ?? previous?.output ?? "",
          status: result.status ?? "error",
          error: result.error ?? "",
          warnings: result.warnings ?? [],
          loading: false,
          updatedAt: Date.now(),
        }));

        const affectedIds = result.changed_asset_ids ?? [];
        if (affectedIds.length > 0) {
          setChangedAssetIds((prev: Set<string>) => {
            const next = new Set(prev);
            for (const id of affectedIds) {
              next.add(id);
            }
            return next;
          });
        }

        return result;
      } catch (error) {
        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ??
            createMaterializeEntry({
              id: entryId,
              kind: "pipeline",
              label: pipeline?.name
                ? `${options?.dryRun ? "Dry run" : "Pipeline"}: ${pipeline.name}`
                : options?.dryRun
                  ? "Pipeline dry run"
                  : "Pipeline materialize",
              pipelineId,
              pipelineName: pipeline?.name ?? null,
              loading: true,
              createdAt: startedAt,
              timeWindow: selectedExecutionTimeWindow,
            })),
          output: (previous?.output ?? "") + (previous?.output ? "\n" : "") + String(error),
          status: "error",
          error: String(error),
          loading: false,
          updatedAt: Date.now(),
        }));
        return null;
      } finally {
        setPipelineMaterializeLoading(false);
        setMaterializingAssetIds((previous: Set<string>) => {
          const next = new Set(previous);
          for (const id of pipelineMaterializingIds) {
            next.delete(id);
          }
          return next;
        });
        if (!options?.dryRun && refresh) {
          await refresh();
        }
      }
    },
    [pipeline, selectedEnvironment, selectedExecutionTimeWindow, setChangedAssetIds],
  );

  // runBuildStale delegates the whole "build stale assets" operation to the
  // server: one streamed run in dependency order, one history entry, one log.
  const runBuildStale = useCallback(
    async (
      targetPipelineId: string,
      options?: {
        assetIds?: string[];
        onAssetEvent?: (event: StreamAssetEvent) => void;
      },
    ) => {
      const entryId = createMaterializeHistoryId();
      const startedAt = Date.now();
      const label = pipeline?.name ? `Build stale: ${pipeline.name}` : "Build stale assets";
      const staleMaterializingIds = options?.assetIds ?? [];
      const baseEntry = () =>
        createMaterializeEntry({
          id: entryId,
          kind: "batch",
          label,
          pipelineId: targetPipelineId,
          pipelineName: pipeline?.name ?? null,
          loading: true,
          createdAt: startedAt,
          timeWindow: selectedExecutionTimeWindow,
        });

      setPipelineMaterializeLoading(true);
      setMaterializingAssetIds(
        (previous: Set<string>) => new Set([...previous, ...staleMaterializingIds]),
      );
      upsertMaterializeEntry(entryId, () => ({ ...baseEntry() }));

      try {
        const result = await buildStalePipelineStream(
          targetPipelineId,
          {
            onChunk: (chunk) => {
              upsertMaterializeEntry(entryId, (previous) => ({
                ...(previous ?? baseEntry()),
                output: (previous?.output ?? "") + chunk,
                loading: true,
                updatedAt: Date.now(),
              }));
            },
            onAssetEvent: options?.onAssetEvent,
          },
          {
            environment: selectedEnvironment,
            start: selectedExecutionTimeWindow?.start,
            end: selectedExecutionTimeWindow?.end,
          },
        );

        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ?? baseEntry()),
          output: result.output ?? previous?.output ?? "",
          status: result.status ?? "error",
          error: result.error ?? "",
          warnings: result.warnings ?? [],
          loading: false,
          updatedAt: Date.now(),
        }));

        const affectedIds = result.changed_asset_ids ?? [];
        if (affectedIds.length > 0) {
          setChangedAssetIds((prev: Set<string>) => {
            const next = new Set(prev);
            for (const id of affectedIds) {
              next.add(id);
            }
            return next;
          });
        }

        return result;
      } catch (error) {
        upsertMaterializeEntry(entryId, (previous) => ({
          ...(previous ?? baseEntry()),
          output: (previous?.output ?? "") + (previous?.output ? "\n" : "") + String(error),
          status: "error",
          error: String(error),
          loading: false,
          updatedAt: Date.now(),
        }));
        return null;
      } finally {
        setPipelineMaterializeLoading(false);
        setMaterializingAssetIds((previous: Set<string>) => {
          const next = new Set(previous);
          for (const id of staleMaterializingIds) {
            next.delete(id);
          }
          return next;
        });
      }
    },
    [pipeline, selectedEnvironment, selectedExecutionTimeWindow, setChangedAssetIds],
  );

  const setMaterializeBatchResult = (
    output: string,
    status: "ok" | "error",
    errorMessage: string,
  ) => {
    const entryId = createMaterializeHistoryId();
    const now = Date.now();

    setResults((previous) => ({
      ...previous,
      resultTab: "materialize",
      selectedMaterializeEntryId: entryId,
      materializeHistory: [
        {
          id: entryId,
          kind: "batch",
          label: "Tutorial materialize",
          assetId: selectedAssetId,
          assetName: asset?.name ?? null,
          pipelineId: pipelineId ?? null,
          pipelineName: pipeline?.name ?? null,
          output,
          status,
          error: errorMessage,
          loading: false,
          createdAt: now,
          updatedAt: now,
        },
        ...previous.materializeHistory,
      ],
    }));
  };

  const clearResultsAfterDelete = () => {
    setResults((previous) => {
      const remainingHistory = previous.materializeHistory.filter(
        (entry) => entry.assetId !== selectedAssetId,
      );

      return {
        ...previous,
        materializeHistory: remainingHistory,
        selectedMaterializeEntryId: remainingHistory.some(
          (entry) => entry.id === previous.selectedMaterializeEntryId,
        )
          ? previous.selectedMaterializeEntryId
          : (remainingHistory[0]?.id ?? null),
      };
    });
  };

  return {
    inspectResult,
    inspectDiagnosticSnapshotByAssetId,
    inspectLoadingByAssetId,
    inspectLoading,
    materializeLoading: effectiveMaterializeLoading,
    materializingAssetIds,
    pipelineMaterializeLoading,
    hasInspectData,
    hasMaterializeData: true,
    hasResultData,
    effectiveResultTab,
    materializeOutputHtml,
    selectedMaterializeEntry,
    materializeHistory,
    canLoadMoreInspectRows,
    loadMoreInspectRows,
    setResultTab,
    selectMaterializeEntry,
    runInspectForAsset,
    runMaterializeForAsset,
    runMaterializePipeline,
    runBuildStale,
    setMaterializeBatchResult,
    clearResultsAfterDelete,
  };
}
