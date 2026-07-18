"use client";

import type { Monaco } from "@monaco-editor/react";
import { AlertTriangle, Check, Copy, FileCode2, ShieldCheck } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from "react";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import type {
  AssetRenderFidelity,
  AssetRenderResult,
  AssetRenderStage,
  AssetRenderStageStatus,
} from "@/lib/api-asset-render";
import { copyTextToClipboard } from "@/lib/copy-to-clipboard";
import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import { cn } from "@/lib/utils";

const MonacoEditor = lazy(async () => {
  const module = await loadMonacoEditorModule();
  return { default: module.default };
});

export function AssetRenderView({
  result,
  loading,
  error,
  onRetry,
}: {
  result: AssetRenderResult | null;
  loading: boolean;
  error: string | null;
  onRetry: () => void;
}) {
  const [selectedStage, setSelectedStage] = useState("");
  const [copied, setCopied] = useState(false);
  const stageKeys = useMemo(
    () => result?.stages.map((stage, index) => `${index}:${stage.kind}`) ?? [],
    [result],
  );

  useEffect(() => {
    setSelectedStage(
      stageKeys.find((key, index) => result?.stages[index]?.content) ?? stageKeys[0] ?? "",
    );
    setCopied(false);
  }, [result?.asset.id, result?.provenance.source.merkle_root, stageKeys]);

  const selectedIndex = stageKeys.indexOf(selectedStage);
  const stage = selectedIndex >= 0 ? result?.stages[selectedIndex] : result?.stages[0];
  const copyStage = useCallback(async () => {
    if (!stage?.content || !(await copyTextToClipboard(stage.content))) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }, [stage?.content]);

  if (loading && !result) {
    return <RenderCentered loading message="Rendering saved asset…" />;
  }
  if (error && !result) {
    return (
      <div className="flex h-full min-h-0 items-center justify-center overflow-auto p-3">
        <Alert variant="destructive" className="max-w-xl">
          <AlertTriangle />
          <AlertTitle>Render failed</AlertTitle>
          <AlertDescription className="flex items-center justify-between gap-3">
            <span>{error}</span>
            <Button variant="outline" size="xs" onClick={onRetry}>
              Retry
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }
  if (!result) {
    return <RenderCentered message="Render an asset to preview its saved operations here." />;
  }

  const context = result.provenance.context;
  const sourceLabel =
    result.provenance.source.kind === "working_tree"
      ? `Saved workspace · ${result.provenance.source.merkle_root.slice(0, 8)}`
      : `Deployment · ${result.provenance.source.merkle_root.slice(0, 8)}`;
  const configurationTitle = context.configuration_digest
    ? `Configuration ${context.configuration_digest.slice(0, 8)} · ${context.configuration_fidelity}`
    : context.configuration_message || "Configuration identity is only available at runtime";
  const variableProvenance = context.variable_provenance ?? [];
  const variableTitle = variableProvenance
    .map((variable) => `${variable.name} — ${variableSourceLabel(variable.source)}`)
    .join("\n");
  const fingerprintTitle = result.asset.fingerprint
    ? `Asset/DAG fingerprint ${result.asset.fingerprint}`
    : "Asset/DAG fingerprint unavailable";
  const target = result.asset.target;
  const targetTitle = [
    target.object ? `${target.kind}: ${target.object}` : target.kind,
    target.identity ? `Physical target ${target.identity}` : target.message,
  ]
    .filter(Boolean)
    .join("\n");

  return (
    <div
      className="flex h-full min-h-0 flex-col bg-background"
      data-testid="asset-render-view"
      aria-busy={loading}
    >
      <div className="shrink-0 border-b bg-muted/20 px-2 py-1.5">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-[11px]">
          <Badge variant="outline" size="xs">
            Preview — not executed
          </Badge>
          <span className="max-w-48 truncate font-mono font-medium" title={result.asset.name}>
            {result.asset.name}
          </span>
          <span className="text-muted-foreground">·</span>
          <span className="truncate font-medium" title={result.provenance.source.merkle_root}>
            {sourceLabel}
          </span>
          <span className="text-muted-foreground">·</span>
          <span title={configurationTitle}>{context.environment || "default"}</span>
          <span className="text-muted-foreground">·</span>
          <span>{formatRenderWindow(context.start_date, context.end_date)}</span>
          <span className="text-muted-foreground">·</span>
          <span>{context.full_refresh ? "full refresh" : "incremental"}</span>
          {result.asset.dialect ? (
            <>
              <span className="text-muted-foreground">·</span>
              <span>{result.asset.dialect}</span>
            </>
          ) : null}
          {result.asset.connection_name ? (
            <>
              <span className="text-muted-foreground">·</span>
              <span className="truncate font-mono">{result.asset.connection_name}</span>
            </>
          ) : null}
          {result.asset.fingerprint ? (
            <Badge variant="muted" size="xs" title={fingerprintTitle}>
              DAG {result.asset.fingerprint.slice(0, 8)}
            </Badge>
          ) : null}
          {target.kind !== "none" || target.fidelity !== "exact" ? (
            <Badge
              variant={target.fidelity === "exact" ? "secondary" : "muted"}
              size="xs"
              title={targetTitle}
            >
              Target {target.identity ? target.identity.slice(0, 8) : "runtime-only"}
            </Badge>
          ) : null}
          {variableProvenance.length > 0 ? (
            <Badge variant="muted" size="xs" title={variableTitle}>
              {variableProvenance.length} pipeline{" "}
              {variableProvenance.length === 1 ? "variable" : "variables"}
            </Badge>
          ) : null}
          {context.configuration_fidelity === "runtime_only" ? (
            <Badge variant="muted" size="xs" title={configurationTitle}>
              Config runtime-only
            </Badge>
          ) : null}
          {result.redactions.length > 0 ? (
            <Badge variant="muted" size="xs" title="Known credential values were masked or omitted">
              <ShieldCheck className="size-3" data-icon="inline-start" /> Credentials redacted
            </Badge>
          ) : null}
          {loading ? <Spinner className="ml-auto size-3.5" /> : null}
        </div>
        {error ? (
          <div className="mt-1 flex items-center gap-1.5 text-[11px] text-destructive" role="alert">
            <AlertTriangle className="size-3" />
            <span className="min-w-0 truncate">Refresh failed: {error}</span>
          </div>
        ) : null}
        {result.issues.length > 0 ? (
          <div
            className={cn(
              "mt-1 truncate text-[11px]",
              result.issues.some((issue) => issue.severity === "error")
                ? "text-destructive"
                : "text-amber-700 dark:text-amber-300",
            )}
            title={result.issues.map((issue) => issue.message).join("\n")}
          >
            {result.issues.map((issue) => issue.message).join(" · ")}
          </div>
        ) : null}
        <div className="mt-1.5 flex min-w-0 items-center gap-1.5">
          <div className="min-w-0 flex-1 overflow-x-auto pb-px">
            <ToggleGroup
              type="single"
              value={stageKeys.includes(selectedStage) ? selectedStage : stageKeys[0]}
              onValueChange={(value) => value && setSelectedStage(value)}
              variant="outline"
              size="sm"
              spacing={0}
              aria-label="Rendered operation"
            >
              {result.stages.map((item, index) => (
                <ToggleGroupItem
                  key={stageKeys[index]}
                  value={stageKeys[index]}
                  title={item.message || assetRenderStageLabel(item)}
                >
                  {assetRenderStageLabel(item)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          </div>
          {stage ? <StageStatusBadge status={stage.status} fidelity={stage.fidelity} /> : null}
          <Button
            variant="outline"
            size="xs"
            className="shrink-0"
            onClick={() => void copyStage()}
            disabled={!stage?.content}
            aria-label="Copy rendered operation"
          >
            {copied ? <Check data-icon="inline-start" /> : <Copy data-icon="inline-start" />}
            {copied ? "Copied" : "Copy"}
          </Button>
        </div>
        {stage?.message ? (
          <p className="mt-1 truncate text-[11px] text-muted-foreground" title={stage.message}>
            {stage.message}
          </p>
        ) : null}
      </div>

      <div className="min-h-0 flex-1">
        {stage?.content ? (
          <ReadOnlyRenderedOperation
            content={stage.content}
            language={stage.language || "sql"}
            modelKey={`${result.asset.id ?? result.asset.name}:${result.provenance.source.merkle_root}:${selectedStage}`}
          />
        ) : (
          <RenderCentered
            message={stage?.message || "This operation cannot be rendered statically yet."}
          />
        )}
      </div>
    </div>
  );
}

function variableSourceLabel(source: string) {
  switch (source) {
    case "pipeline_default":
      return "pipeline default";
    case "schedule_override":
      return "schedule override";
    case "run_override":
      return "run override";
    default:
      return source.replaceAll("_", " ");
  }
}

export function ReadOnlyRenderedOperation({
  content,
  language,
  modelKey,
}: {
  content: string;
  language: string;
  modelKey: string;
}) {
  const { monacoTheme } = useWorkspaceTheme();
  const beforeMount = useCallback((monaco: Monaco) => defineBruinMonacoThemes(monaco), []);
  const extension = language === "sql" ? "sql" : language === "json" ? "json" : "txt";

  return (
    <Suspense fallback={<RenderCentered loading message="Loading preview…" />}>
      <MonacoEditor
        language={language}
        path={`inmemory://renart/render/${encodeURIComponent(modelKey)}.${extension}`}
        value={content}
        theme={monacoTheme}
        beforeMount={beforeMount}
        options={{
          readOnly: true,
          domReadOnly: true,
          automaticLayout: true,
          minimap: { enabled: false },
          fontSize: 12,
          folding: true,
          lineNumbersMinChars: 3,
          renderLineHighlight: "none",
          scrollBeyondLastLine: false,
          wordWrap: "on",
        }}
      />
    </Suspense>
  );
}

function StageStatusBadge({
  status,
  fidelity,
}: {
  status: AssetRenderStageStatus;
  fidelity: AssetRenderFidelity;
}) {
  const label =
    status === "error"
      ? "render error"
      : status === "unsupported" || fidelity === "unsupported"
        ? "not renderable"
        : fidelity === "runtime_only"
          ? "runtime-dependent"
          : fidelity;
  return (
    <Badge
      variant={
        status === "error"
          ? "destructive"
          : status === "unsupported" || fidelity === "unsupported"
            ? "outline"
            : fidelity === "exact"
              ? "secondary"
              : "muted"
      }
      size="xs"
      className="shrink-0"
    >
      {label}
    </Badge>
  );
}

function RenderCentered({ message, loading = false }: { message: string; loading?: boolean }) {
  return (
    <div className="flex h-full min-h-0 items-center justify-center gap-2 px-4 text-center text-xs text-muted-foreground">
      {loading ? (
        <Spinner className="size-4" />
      ) : (
        <FileCode2 className="size-4" data-icon="inline-start" />
      )}
      <span>{message}</span>
    </div>
  );
}

export function assetRenderStageLabel(stage: AssetRenderStage) {
  if (stage.label) return stage.label;
  switch (stage.kind) {
    case "compiled_query":
      return "Compiled query";
    case "execution_sql":
      return "Execution SQL";
    case "schema_preparation":
      return "Schema preparation";
    default:
      return stage.kind.replaceAll("_", " ");
  }
}

function formatRenderWindow(start: string, end: string) {
  const format = (value: string) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : date.toISOString().slice(0, 16).replace("T", " ");
  };
  return `${format(start)}–${format(end)} UTC`;
}
