"use client";

import { ReactNode, useEffect, useState } from "react";
import { Panel, PanelGroup, PanelResizeHandle } from "react-resizable-panels";

import { useIsMobile } from "@/hooks/use-mobile";
import { WorkspaceMobilePaneSheet } from "@/components/workspace-mobile-pane-sheet";

export function WorkspaceSettingsSplitView({
  content,
  pane,
  paneKey,
  paneTitle = "Editor",
  paneDescription,
  isPaneOpen = true,
  onPaneOpenChange,
}: {
  content: ReactNode;
  pane: ReactNode;
  paneKey?: string;
  paneTitle?: string;
  paneDescription?: string | null;
  isPaneOpen?: boolean;
  onPaneOpenChange?: (open: boolean) => void;
}) {
  const isMobile = useIsMobile();
  const [mobilePaneOpen, setMobilePaneOpen] = useState(isPaneOpen);

  useEffect(() => {
    setMobilePaneOpen(isPaneOpen);
  }, [isPaneOpen, paneKey]);

  const handleMobilePaneOpenChange = (open: boolean) => {
    setMobilePaneOpen(open);
    onPaneOpenChange?.(open);
  };

  if (isMobile) {
    return (
      <>
        <div className="h-full min-h-0 overflow-hidden">{content}</div>
        <WorkspaceMobilePaneSheet
          open={mobilePaneOpen}
          onOpenChange={handleMobilePaneOpenChange}
          title={paneTitle}
          description={paneDescription}
        >
          <div key={`mobile:${paneKey ?? "default"}`} className="h-full min-h-0">
            {pane}
          </div>
        </WorkspaceMobilePaneSheet>
      </>
    );
  }

  return (
    <PanelGroup
      direction="horizontal"
      className="h-full min-h-0 overflow-hidden"
    >
      <Panel defaultSize={50} minSize={30}>
        {content}
      </Panel>

      <PanelResizeHandle className="w-px bg-border" />

      <Panel key={`desktop:${paneKey ?? "default"}`} defaultSize={32} minSize={24}>
        {pane}
      </Panel>
    </PanelGroup>
  );
}
