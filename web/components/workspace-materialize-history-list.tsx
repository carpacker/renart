"use client";

import { CircleAlert, CircleCheckBig, Clock3, LoaderCircle } from "lucide-react";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MaterializeHistoryEntry } from "@/lib/atoms/results";

export function WorkspaceMaterializeHistoryList({
  entries,
  selectedEntryId,
  onSelectEntry,
}: {
  entries: MaterializeHistoryEntry[];
  selectedEntryId: string | null;
  onSelectEntry: (entryId: string) => void;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col border-r bg-muted/10">
      <div className="border-b px-3 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        History
      </div>
      <ScrollArea className="min-h-0 flex-1" viewportClassName="p-2">
        {entries.length === 0 ? (
          <div className="flex h-full min-h-0 items-center justify-center rounded border border-dashed bg-background/70 px-3 text-center text-xs text-muted-foreground">
            No materialize runs yet.
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {entries.map((entry) => {
              const selected = entry.id === selectedEntryId;

              return (
                <Button
                  key={entry.id}
                  type="button"
                  variant={selected ? "secondary" : "ghost"}
                  className="h-auto items-start justify-start px-3 py-2 text-left"
                  onClick={() => onSelectEntry(entry.id)}
                >
                  <div className="flex w-full min-w-0 flex-col gap-1">
                    <div className="flex items-center justify-between gap-2">
                      <div className="truncate text-xs font-medium">{entry.label}</div>
                      <MaterializeEntryStatus entry={entry} />
                    </div>
                    <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                      <Clock3 className="size-3" />
                      <span>{new Date(entry.updatedAt).toLocaleTimeString()}</span>
                    </div>
                    <div className="truncate text-[11px] text-muted-foreground">
                      {entry.kind === "pipeline"
                        ? entry.pipelineName ?? "Pipeline run"
                        : entry.assetName ?? "Asset run"}
                    </div>
                    <div className="text-[11px] text-muted-foreground">
                      {entry.kind === "pipeline"
                        ? "Pipeline materialize"
                        : entry.kind === "batch"
                          ? "Batch output"
                          : "Asset materialize"}
                    </div>
                    {entry.timeWindow ? (
                      <div className="truncate text-[11px] text-muted-foreground">
                        {formatTimeWindow(entry.timeWindow)}
                      </div>
                    ) : null}
                  </div>
                </Button>
              );
            })}
          </div>
        )}
      </ScrollArea>
    </div>
  );
}

function formatTimeWindow(window: { start: string; end: string }) {
  const start = new Date(window.start);
  const end = new Date(window.end);
  return `${start.toLocaleString()} - ${end.toLocaleString()}`;
}

function MaterializeEntryStatus({ entry }: { entry: MaterializeHistoryEntry }) {
  if (entry.loading) {
    return <LoaderCircle className="size-3 shrink-0 animate-spin text-muted-foreground" />;
  }

  if (entry.status === "error") {
    return <CircleAlert className="size-3 shrink-0 text-destructive" />;
  }

  if (entry.status === "ok") {
    return <CircleCheckBig className="size-3 shrink-0 text-emerald-600" />;
  }

  return null;
}
