"use client";

import { Dispatch, ReactNode, SetStateAction } from "react";
import { Plus } from "lucide-react";
import { PanelGroup, PanelResizeHandle } from "react-resizable-panels";

import { WorkspaceCanvasPane, WorkspaceCanvasPaneProps } from "@/components/workspace-canvas-pane";
import { WorkspaceMobileEditorSheet } from "@/components/workspace-mobile-editor-sheet";
import { Button } from "@/components/ui/button";

type WorkspaceMainContentProps = {
  hasPipelines: boolean;
  isMobile: boolean;
  mobileEditorOpen: boolean;
  setMobileEditorOpen: Dispatch<SetStateAction<boolean>>;
  assetPath?: string | null;
  editorPane: ReactNode;
  emptyStateAction: () => void;
  canvasPaneProps: WorkspaceCanvasPaneProps;
};

export function WorkspaceMainContent({
  hasPipelines,
  isMobile,
  mobileEditorOpen,
  setMobileEditorOpen,
  assetPath,
  editorPane,
  emptyStateAction,
  canvasPaneProps,
}: WorkspaceMainContentProps) {
  if (!hasPipelines) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="flex max-w-md flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 px-6 py-10 text-center">
          <div className="rounded-full bg-background p-3 shadow-sm">
            <Plus className="size-5 text-muted-foreground" />
          </div>
          <div className="space-y-1">
            <h2 className="text-lg font-semibold">Create your first pipeline</h2>
            <p className="text-sm text-muted-foreground">
              Pipelines organize your assets and power the lineage canvas.
            </p>
          </div>
          <Button type="button" onClick={emptyStateAction}>
            <Plus className="mr-2 size-4" />
            Create pipeline
          </Button>
        </div>
      </div>
    );
  }

  return (
    <>
      <PanelGroup
        direction={isMobile ? "vertical" : "horizontal"}
        className="h-full min-h-0 min-w-0"
      >
        <WorkspaceCanvasPane {...canvasPaneProps} />
        {!isMobile ? (
          <>
            <PanelResizeHandle className="w-px bg-border" />
            {editorPane}
          </>
        ) : null}
      </PanelGroup>

      {isMobile ? (
        <WorkspaceMobileEditorSheet
          open={mobileEditorOpen}
          onOpenChange={setMobileEditorOpen}
          assetPath={assetPath}
        >
          {editorPane}
        </WorkspaceMobileEditorSheet>
      ) : null}
    </>
  );
}
