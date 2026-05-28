"use client";

import { Play } from "lucide-react";
import { useEffect, useRef } from "react";

import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import { MaterializeHistoryEntry } from "@/lib/atoms/results";

export function WorkspaceMaterializeOutputView({
  entry,
  outputHtml,
  pipelineMaterializeLoading,
}: {
  entry: MaterializeHistoryEntry | null;
  outputHtml: string;
  pipelineMaterializeLoading?: boolean;
}) {
  const materializeOutputRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const element = materializeOutputRef.current;
    if (!element) {
      return;
    }

    element.scrollTop = element.scrollHeight;
  }, [entry?.id, entry?.loading, outputHtml]);

  if (!entry) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center rounded border border-dashed bg-background px-4 text-center text-sm text-muted-foreground">
        Select a materialize run from the history or run an asset to see output here.
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      {entry.loading && pipelineMaterializeLoading ? (
        <div className="flex items-center gap-2 rounded border border-primary/30 bg-primary/5 px-2 py-1 text-xs text-primary">
          <Play className="size-3.5 animate-pulse fill-current" />
          Running pipeline...
        </div>
      ) : null}
      {entry.timeWindow ? (
        <div className="rounded border bg-muted/30 px-2 py-1 text-xs text-muted-foreground">
          Period: {new Date(entry.timeWindow.start).toLocaleString()} - {new Date(entry.timeWindow.end).toLocaleString()}
        </div>
      ) : null}
      {entry.status === "error" ? (
        <div className="rounded border border-destructive/40 bg-destructive/10 px-2 py-1 text-xs text-destructive">
          Materialization failed
          {entry.error ? `: ${entry.error}` : ""}
        </div>
      ) : null}
      <div
        className={`min-h-0 flex flex-1 flex-col overflow-hidden rounded border ${
          entry.status === "error"
            ? "border-destructive/40 bg-destructive/5"
            : "bg-background"
        }`}
      >
        <ScrollArea
          className="min-h-0 flex-1"
          viewportClassName="p-2"
          viewportRef={materializeOutputRef}
        >
          <pre
            className="whitespace-pre-wrap font-mono text-[11px]"
            dangerouslySetInnerHTML={{ __html: outputHtml }}
          />
        </ScrollArea>
        {entry.loading ? (
          <div className="flex items-center gap-2 border-t bg-muted/40 px-2 py-1 font-mono text-[11px] text-muted-foreground">
            <Spinner className="size-3.5" />
            <span>
              {entry.kind === "pipeline"
                ? "Waiting for pipeline output..."
                : "Waiting for asset output..."}
            </span>
          </div>
        ) : null}
      </div>
    </div>
  );
}
