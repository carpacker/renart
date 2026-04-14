"use client";

import { CSSProperties, MutableRefObject } from "react";
import { FilePenLine, LoaderCircle, Rows3 } from "lucide-react";
import {
  Background,
  Controls,
  Edge,
  Node,
  NodeTypes,
  ReactFlow,
  ReactFlowInstance,
} from "reactflow";
import { Panel, PanelGroup, PanelResizeHandle } from "react-resizable-panels";

import { WorkspaceResultsPanel } from "@/components/workspace-results-panel";
import { Button } from "@/components/ui/button";
import { MaterializeHistoryEntry } from "@/lib/atoms/results";
import { AssetInspectResponse } from "@/lib/types";

export type WorkspaceCanvasPaneProps = {
  highlighted: boolean;
  highlightStyle?: CSSProperties;
  hasResultData: boolean;
  canvasContainerRef: MutableRefObject<HTMLDivElement | null>;
  nodes: Node[];
  edges: Edge[];
  nodeTypes: NodeTypes;
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
  onResultTabChange: (tab: "inspect" | "materialize") => void;
  onSelectMaterializeEntry: (entryId: string) => void;
  onInit: (instance: ReactFlowInstance) => void;
  onNodesChange: Parameters<typeof ReactFlow>[0]["onNodesChange"];
  onEdgesChange: Parameters<typeof ReactFlow>[0]["onEdgesChange"];
  onNodeDragStop: Parameters<typeof ReactFlow>[0]["onNodeDragStop"];
  onPaneClick: Parameters<typeof ReactFlow>[0]["onPaneClick"];
  onPaneContextMenu: Parameters<typeof ReactFlow>[0]["onPaneContextMenu"];
  onNodeClick: Parameters<typeof ReactFlow>[0]["onNodeClick"];
  onRecomputeGraph: () => void;
  onRunPipeline?: () => void;
  canRunPipeline?: boolean;
  showEditorButton?: boolean;
  isEditorButtonDisabled?: boolean;
  onOpenEditor?: () => void;
};

export function WorkspaceCanvasPane({
  highlighted,
  highlightStyle,
  hasResultData,
  canvasContainerRef,
  nodes,
  edges,
  nodeTypes,
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
  onInit,
  onNodesChange,
  onEdgesChange,
  onNodeDragStop,
  onPaneClick,
  onPaneContextMenu,
  onNodeClick,
  onRecomputeGraph,
  onRunPipeline,
  canRunPipeline = false,
  showEditorButton = false,
  isEditorButtonDisabled = false,
  onOpenEditor,
}: WorkspaceCanvasPaneProps) {
  return (
    <Panel
      className={highlighted ? "ring-2 ring-primary/70 ring-inset" : ""}
      style={highlighted ? highlightStyle : undefined}
      defaultSize={50}
      minSize={30}
    >
      <PanelGroup direction="vertical">
        <Panel defaultSize={hasResultData ? 72 : 100} minSize={45}>
          <div className="relative h-full" ref={canvasContainerRef}>
            <div className="absolute right-3 top-3 z-10 flex gap-2">
              {onRunPipeline ? (
                <Button
                  onClick={onRunPipeline}
                  size="sm"
                  type="button"
                  disabled={!canRunPipeline || pipelineMaterializeLoading}
                >
                  {pipelineMaterializeLoading ? (
                    <LoaderCircle className="mr-2 size-3.5 animate-spin" />
                  ) : (
                    <PlayIcon className="mr-2 size-3.5" />
                  )}
                  {pipelineMaterializeLoading ? "Running pipeline..." : "Run pipeline"}
                </Button>
              ) : null}
              {showEditorButton ? (
                <Button
                  onClick={onOpenEditor}
                  size="sm"
                  type="button"
                  variant="outline"
                  disabled={isEditorButtonDisabled}
                >
                  <FilePenLine className="mr-2 size-3.5" />
                  Edit asset
                </Button>
              ) : null}
              <Button
                onClick={onRecomputeGraph}
                size="sm"
                type="button"
                variant="outline"
              >
                <Rows3 className="mr-2 size-3.5" />
                Reload layout
              </Button>
            </div>
            <ReactFlow
              nodes={nodes}
              edges={edges}
              nodeTypes={nodeTypes}
              panActivationKeyCode={null}
              minZoom={0.1}
              onInit={onInit}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onNodeDragStop={onNodeDragStop}
              onPaneClick={onPaneClick}
              onPaneContextMenu={onPaneContextMenu}
              onNodeClick={onNodeClick}
            >
              <Background />
              <Controls />
            </ReactFlow>
          </div>
        </Panel>

        {hasResultData && (
          <>
            <PanelResizeHandle className="h-px bg-border" />

            <Panel defaultSize={28} minSize={20}>
              <WorkspaceResultsPanel
                inspectResult={inspectResult}
                inspectLoading={inspectLoading}
                inspectMeta={inspectMeta}
                materializeLoading={materializeLoading}
                pipelineMaterializeLoading={pipelineMaterializeLoading}
                hasInspectData={hasInspectData}
                effectiveResultTab={effectiveResultTab}
                selectedMaterializeEntry={selectedMaterializeEntry}
                materializeHistory={materializeHistory}
                materializeOutputHtml={materializeOutputHtml}
                canLoadMoreInspectRows={canLoadMoreInspectRows}
                onLoadMoreInspectRows={onLoadMoreInspectRows}
                onResultTabChange={onResultTabChange}
                onSelectMaterializeEntry={onSelectMaterializeEntry}
              />
            </Panel>
          </>
        )}
      </PanelGroup>
    </Panel>
  );
}

function PlayIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className}
      aria-hidden="true"
    >
      <path d="M8 5.14v13.72a1 1 0 0 0 1.5.86l10-6.86a1 1 0 0 0 0-1.72l-10-6.86A1 1 0 0 0 8 5.14Z" />
    </svg>
  );
}
