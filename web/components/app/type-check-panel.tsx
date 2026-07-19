import { AlertTriangle, Bell, CheckCircle2, Loader2, RotateCw, XCircle } from "lucide-react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Spinner } from "@/components/ui/spinner";
import type { PipelineTypeCheckReport } from "@/lib/api-pipelines";

export function TypeCheckPanel({
  report,
  loading,
  error,
  onRun,
  onSelectAsset,
}: {
  report: PipelineTypeCheckReport | null;
  loading: boolean;
  error: string | null;
  onRun?: () => void;
  onSelectAsset?: (assetId: string) => void;
}) {
  if (loading && !report) {
    return <TypeCheckLoading />;
  }
  if (!report) {
    return (
      <div className="flex h-full min-h-0 flex-col items-center justify-center gap-3 bg-background p-3 text-xs text-muted-foreground">
        {error ? (
          <Alert variant="destructive" className="max-w-lg">
            <AlertTriangle />
            <AlertTitle>Type check failed</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : (
          <span>Type check assets for column and type errors.</span>
        )}
        <Button size="sm" variant="outline" onClick={onRun}>
          <Bell />
          {error ? "Retry type check" : "Run type check"}
        </Button>
      </div>
    );
  }

  const flagged = report.assets.filter((asset) => asset.findings.length > 0);
  const checkedAt = report.start_date ? new Date(report.start_date) : null;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5 text-xs">
        <span className="inline-flex items-center gap-1 text-red-600 dark:text-red-400">
          <XCircle className="size-3.5" />
          {report.summary.errors}
        </span>
        <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="size-3.5" />
          {report.summary.warnings}
        </span>
        <span className="text-muted-foreground">
          {report.summary.assets} asset{report.summary.assets === 1 ? "" : "s"} checked
        </span>
        {checkedAt ? (
          <span className="hidden text-muted-foreground/70 sm:inline">
            · window {checkedAt.toISOString().slice(0, 10)}
          </span>
        ) : null}
        <Button size="xs" variant="outline" className="ml-auto" onClick={onRun} disabled={loading}>
          {loading ? <Loader2 className="animate-spin" /> : <RotateCw />}
          Re-run
        </Button>
      </div>
      {error ? (
        <Alert variant="destructive" className="m-2 shrink-0 w-auto">
          <AlertTriangle />
          <AlertTitle>Latest type check failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <ScrollArea
        className="min-h-0 flex-1"
        viewportClassName="h-full"
        data-testid="type-check-scroll-area"
      >
        <div className="p-2">
          {flagged.length === 0 ? (
            <div className="flex items-center gap-2 px-2 py-3 text-xs text-emerald-600 dark:text-emerald-400">
              <CheckCircle2 className="size-4" />
              No type errors found across {report.summary.assets} asset
              {report.summary.assets === 1 ? "" : "s"}.
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {flagged.map((asset) => (
                <div key={asset.name} className="rounded-md border">
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 border-b bg-muted/30 px-2.5 py-1.5 text-left text-xs hover:bg-muted disabled:cursor-default"
                    onClick={() => asset.id && onSelectAsset?.(asset.id)}
                    disabled={!asset.id}
                  >
                    {asset.status === "error" ? (
                      <XCircle className="size-3.5 shrink-0 text-red-500" />
                    ) : (
                      <AlertTriangle className="size-3.5 shrink-0 text-amber-500" />
                    )}
                    <span className="min-w-0 flex-1 truncate font-mono font-medium">
                      {asset.name}
                    </span>
                    <span className="shrink-0 text-[10px] text-muted-foreground">{asset.type}</span>
                  </button>
                  <ul className="divide-y">
                    {asset.findings.map((finding, index) => (
                      <li key={index} className="flex items-start gap-2 px-2.5 py-1.5 text-xs">
                        {finding.severity === "error" ? (
                          <XCircle className="mt-0.5 size-3.5 shrink-0 text-red-500" />
                        ) : (
                          <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" />
                        )}
                        <span className="min-w-0 flex-1">{finding.message}</span>
                        {finding.line ? (
                          <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                            L{finding.line}:C{finding.column}
                          </span>
                        ) : null}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

function TypeCheckLoading() {
  return (
    <div className="flex h-full min-h-0 items-center justify-center bg-background">
      <div className="flex items-center gap-2 text-xs opacity-80">
        <Spinner />
        <span>Type checking pipeline…</span>
      </div>
    </div>
  );
}
