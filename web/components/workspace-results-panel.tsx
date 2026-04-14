"use client";

import { AssetInspectView } from "@/components/asset-inspect-view";
import { InspectWarningCard } from "@/components/inspect-warning-card";
import { WorkspaceMaterializeHistoryList } from "@/components/workspace-materialize-history-list";
import { WorkspaceMaterializeOutputView } from "@/components/workspace-materialize-output-view";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Spinner } from "@/components/ui/spinner";
import { MaterializeHistoryEntry } from "@/lib/atoms/results";
import { extractInspectErrorText } from "@/lib/inspect-errors";
import { AssetInspectResponse } from "@/lib/types";

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
}: Props) {
  const inspectErrorDetails = extractInspectErrorText(inspectResult?.raw_output);

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
                message={inspectResult.error || inspectErrorDetails || "Inspect failed."}
                testId="inspect-warning-empty-state"
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
