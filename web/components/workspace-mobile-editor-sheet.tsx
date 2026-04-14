"use client";

import { ReactNode } from "react";

import { WorkspaceMobilePaneSheet } from "@/components/workspace-mobile-pane-sheet";

type WorkspaceMobileEditorSheetProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  assetPath?: string | null;
  children: ReactNode;
};

export function WorkspaceMobileEditorSheet({
  open,
  onOpenChange,
  assetPath,
  children,
}: WorkspaceMobileEditorSheetProps) {
  return (
    <WorkspaceMobilePaneSheet
      open={open}
      onOpenChange={onOpenChange}
      title="Asset Editor"
      description={assetPath ?? "No asset selected"}
    >
      {children}
    </WorkspaceMobilePaneSheet>
  );
}
