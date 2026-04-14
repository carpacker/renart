"use client";

import { AlertCircle, RefreshCcw } from "lucide-react";
import { CSSProperties, ReactNode } from "react";

import { Button } from "@/components/ui/button";

type WorkspaceConfigPaneLayoutProps = {
  title: string;
  configPath: string;
  loading: boolean;
  busy: boolean;
  parseError?: string;
  helpMode?: boolean;
  highlighted?: boolean;
  highlightStyle?: CSSProperties;
  statusMessage?: string | null;
  statusTone?: "error" | "success" | null;
  onReload: () => void;
  children: ReactNode;
};

export function WorkspaceConfigPaneLayout({
  title,
  configPath,
  loading,
  busy,
  parseError,
  helpMode = false,
  highlighted = false,
  highlightStyle,
  statusMessage,
  statusTone,
  onReload,
  children,
}: WorkspaceConfigPaneLayoutProps) {
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col border-l bg-background">
      <div
        className={`border-b px-4 py-3 ${
          helpMode && highlighted ? "ring-2 ring-primary/70 ring-inset" : ""
        }`}
        style={helpMode && highlighted ? highlightStyle : undefined}
      >
        <div className="mb-2 text-sm font-semibold">{title}</div>
        <div className="text-xs opacity-70">{configPath}</div>
        <div className="mt-2 flex flex-wrap gap-2">
          <Button size="sm" type="button" variant="outline" disabled={loading || busy} onClick={onReload}>
            <RefreshCcw className="mr-1 inline size-3" />
            Reload
          </Button>
        </div>
        {statusMessage && (
          <div
            className={`mt-2 rounded-md border px-2 py-1 text-xs ${
              statusTone === "error"
                ? "border-destructive/40 bg-destructive/10 text-destructive"
                : "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
            }`}
          >
            {statusMessage}
          </div>
        )}
        {parseError && (
          <div className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-xs text-amber-800 dark:text-amber-300">
            <div className="flex items-center gap-1 font-medium">
              <AlertCircle className="size-3" />
              Workspace config could not be parsed
            </div>
            <div className="mt-1 whitespace-pre-wrap">{parseError}</div>
          </div>
        )}
      </div>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden p-4">
        <div className="min-h-0 flex-1 overflow-auto">{children}</div>
      </div>
    </div>
  );
}
