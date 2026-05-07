"use client";

import { useMemo } from "react";

import { AssetInspectView } from "@/components/asset-inspect-view";
import { InspectWarningCard } from "@/components/inspect-warning-card";
import { WorkspaceMaterializeHistoryList } from "@/components/workspace-materialize-history-list";
import { WorkspaceMaterializeOutputView } from "@/components/workspace-materialize-output-view";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Spinner } from "@/components/ui/spinner";
import { MaterializeHistoryEntry } from "@/lib/atoms/results";
import { extractInspectErrorText, extractMissingReferencedObjects } from "@/lib/inspect-errors";
import { MaterializeScope } from "@/lib/materialize-scope";
import { AssetInspectResponse, WebAsset } from "@/lib/types";

type Props = {
  inspectResult: AssetInspectResponse | null;
  inspectLoading: boolean;
  inspectMeta?: Record<string, string>;
  materializeLoading: boolean;
  pipelineMaterializeLoading?: boolean;
  hasInspectData: boolean;
  effectiveResultTab: "inspect" | "materialize";
  selectedMaterializeEntry: MaterializeHistoryEntry | null;
  materializeHistory: MaterializeHistoryEntry[];
  materializeOutputHtml: string | null;
  canLoadMoreInspectRows?: boolean;
  onLoadMoreInspectRows?: () => void;
  onResultTabChange: (value: "inspect" | "materialize") => void;
  onSelectMaterializeEntry: (entryId: string) => void;
  onMaterializeMissingUpstreams?: (scope: MaterializeScope) => void;
  selectedAsset?: WebAsset | null;
  pipelineAssets?: WebAsset[];
};

export function WorkspaceResultsPanel({
  inspectResult,
  inspectLoading,
  inspectMeta,
  materializeLoading,
  pipelineMaterializeLoading = false,
  hasInspectData,
  effectiveResultTab,
  selectedMaterializeEntry,
  materializeHistory,
  materializeOutputHtml,
  canLoadMoreInspectRows,
  onLoadMoreInspectRows,
  onResultTabChange,
  onSelectMaterializeEntry,
  onMaterializeMissingUpstreams,
  selectedAsset,
  pipelineAssets = [],
}: Props) {
  const inspectErrorDetails = extractInspectErrorText(inspectResult?.raw_output);
  const fallbackMissingUpstreamNames = useMemo(() => {
    if (!selectedAsset || !inspectResult?.error) {
      return [];
    }

    const referenced = new Set([
      ...extractMissingReferencedObjects(inspectResult.error),
      ...extractMissingReferencedObjects(inspectErrorDetails),
    ]);
    if (referenced.size === 0) {
      return [];
    }

    const upstreamSet = new Set(selectedAsset.upstreams ?? []);
    return pipelineAssets
      .filter((asset) => upstreamSet.has(asset.name))
      .map((asset) => asset.name)
      .filter((name) => referenced.has(name.toLowerCase()));
  }, [inspectErrorDetails, inspectResult?.error, pipelineAssets, selectedAsset]);
  const missingUpstreamNames =
    inspectResult?.missing_upstream_asset_names?.length
      ? inspectResult.missing_upstream_asset_names
      : fallbackMissingUpstreamNames;
  const missingUpstreamsMaterializable = missingUpstreamNames.length > 0;
  const inspectMessage = missingUpstreamsMaterializable
    ? `One or more upstream Renart assets are not materialized yet: ${missingUpstreamNames.join(", ")}. Materialize the upstream assets, then inspect again.`
    : inspectResult?.error || inspectErrorDetails || "Inspect failed.";

  return (
    <div
      className="flex h-full min-h-0 flex-col border-t bg-muted/20"
      data-testid="workspace-results-panel"
    >
      <Tabs
        className="flex min-h-0 flex-1 flex-col"
        onValueChange={(value) =>
          onResultTabChange(value as "inspect" | "materialize")
        }
        value={effectiveResultTab}
      >
        <div className="flex items-center justify-between border-b px-3 py-2">
          <TabsList>
            <TabsTrigger disabled={!hasInspectData} value="inspect">
              Inspect
            </TabsTrigger>
            <TabsTrigger value="materialize">
              Materialize
            </TabsTrigger>
          </TabsList>

          <div className="text-[11px] opacity-70">
            {effectiveResultTab === "inspect"
              ? `${inspectResult?.rows.length ?? 0} rows`
              : `${materializeHistory.length} runs`}
          </div>
        </div>

        <TabsContent className="min-h-0 flex flex-1 flex-col overflow-hidden p-2" value="inspect">
          {inspectLoading && !inspectResult ? (
            <LoadingState label="Inspecting asset..." />
          ) : inspectResult?.error ? (
            <div className="flex h-full min-h-0 items-center justify-center p-6">
              <InspectWarningCard
                message={inspectMessage}
                testId="inspect-warning-empty-state"
                actions={
                  missingUpstreamsMaterializable && onMaterializeMissingUpstreams ? (
                    <Button
                      size="sm"
                      type="button"
                      onClick={() => onMaterializeMissingUpstreams("asset_with_upstreams")}
                    >
                      Materialize upstream assets
                    </Button>
                  ) : undefined
                }
              />
            </div>
          ) : (
            <AssetInspectView
              columns={inspectResult?.columns ?? []}
              rows={inspectResult?.rows ?? []}
              meta={inspectMeta}
              loading={inspectLoading}
              canLoadMore={canLoadMoreInspectRows}
              onLoadMore={onLoadMoreInspectRows}
              warning={inspectResult?.warning}
            />
          )}
        </TabsContent>

        <TabsContent className="min-h-0 flex flex-1 flex-col overflow-hidden p-2" value="materialize">
          <div className="flex h-full min-h-0 overflow-hidden rounded border bg-background">
            <div className="w-56 min-w-0">
              <WorkspaceMaterializeHistoryList
                entries={materializeHistory}
                selectedEntryId={selectedMaterializeEntry?.id ?? null}
                onSelectEntry={onSelectMaterializeEntry}
              />
            </div>
            <div className="min-w-0 flex-1 p-2">
              <WorkspaceMaterializeOutputView
                entry={selectedMaterializeEntry}
                outputHtml={materializeOutputHtml ?? ""}
                pipelineMaterializeLoading={pipelineMaterializeLoading}
              />
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function LoadingState({ label }: { label: string }) {
  return (
    <div className="flex h-full min-h-0 items-center justify-center rounded border bg-background">
      <div className="flex items-center gap-2 text-xs opacity-80">
        <Spinner className="size-4" />
        <span>{label}</span>
      </div>
    </div>
  );
}
