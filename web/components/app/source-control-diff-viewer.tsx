import type { Monaco } from "@monaco-editor/react";
import { Loader2 } from "lucide-react";
import { lazy, Suspense, useCallback } from "react";

import { ScrollArea } from "@/components/ui/scroll-area";
import { useWorkspaceTheme } from "@/hooks/use-workspace-theme";
import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import type { SourceControlDiff } from "@/lib/types";
import { cn } from "@/lib/utils";

const MonacoDiffEditor = lazy(async () => {
  const module = await loadMonacoEditorModule();
  return { default: module.DiffEditor };
});

export function SourceControlDiffViewer({
  diff,
  loading,
  className,
}: {
  diff: SourceControlDiff | null;
  loading: boolean;
  className?: string;
}) {
  if (loading) {
    return (
      <div
        className={cn(
          "flex items-center gap-2 rounded-lg border p-3 text-xs text-muted-foreground",
          className,
        )}
      >
        <Loader2 className="size-3.5 animate-spin" />
        Loading diff...
      </div>
    );
  }
  if (!diff) {
    return (
      <div
        className={cn(
          "flex min-w-0 items-center justify-center rounded-lg border border-dashed p-4 text-center text-xs text-muted-foreground",
          className,
        )}
      >
        Select a changed file to preview its diff.
      </div>
    );
  }

  const files = diff.files?.length ? diff.files : [diff];
  const grouped = files.length > 1;
  return (
    <div
      className={cn("flex min-h-0 min-w-0 flex-col overflow-hidden rounded-lg border", className)}
    >
      <div className="flex h-8 shrink-0 items-center gap-2 border-b bg-muted/50 px-2 text-xs">
        <span className="min-w-0 flex-1 truncate font-mono">{diff.path}</span>
        {grouped ? (
          <span className="text-[10px] text-muted-foreground">
            {files.length} file{files.length === 1 ? "" : "s"}
          </span>
        ) : null}
        <span className="rounded bg-background px-1.5 py-0.5 text-[10px] uppercase text-muted-foreground">
          {diff.staged ? "staged" : "worktree"}
        </span>
      </div>
      {grouped ? (
        <ScrollArea className="min-h-0 flex-1" viewportClassName="h-full">
          <div className="flex flex-col gap-3 p-3">
            {files.map((file) => (
              <SourceControlDiffFile
                key={`${file.staged ? "staged" : "worktree"}:${file.path}`}
                file={file}
                showHeader
                style={{ height: groupedDiffHeight(file) }}
              />
            ))}
          </div>
        </ScrollArea>
      ) : (
        <SourceControlDiffFile file={files[0]} className="min-h-0 flex-1" />
      )}
    </div>
  );
}

function SourceControlDiffFile({
  file,
  showHeader = false,
  className,
  style,
}: {
  file: SourceControlDiff;
  showHeader?: boolean;
  className?: string;
  style?: React.CSSProperties;
}) {
  const language = sourceControlLanguageForPath(file.path);
  const { monacoTheme } = useWorkspaceTheme();
  const beforeMount = useCallback((monaco: Monaco) => defineBruinMonacoThemes(monaco), []);

  return (
    <div
      data-testid="source-control-diff-file"
      data-language={language}
      aria-label={`Diff for ${file.path}`}
      className={cn(
        "flex min-h-0 min-w-0 flex-col overflow-hidden bg-background",
        showHeader && "rounded-md border",
        className,
      )}
      style={style}
    >
      {showHeader ? (
        <div className="flex h-7 shrink-0 items-center border-b bg-muted/40 px-2 font-mono text-[11px]">
          <span className="truncate">{file.path}</span>
        </div>
      ) : null}
      {file.binary ? (
        <div className="flex min-h-32 flex-1 items-center justify-center p-4 text-xs text-muted-foreground">
          {file.patch || "Binary diff not shown."}
        </div>
      ) : file.original === file.modified ? (
        <div className="flex min-h-32 flex-1 items-center justify-center p-4 text-xs text-muted-foreground">
          No textual diff available.
        </div>
      ) : (
        <Suspense
          fallback={
            <div className="flex min-h-32 flex-1 items-center justify-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="size-3.5 animate-spin" />
              Loading diff editor...
            </div>
          }
        >
          <MonacoDiffEditor
            original={file.original}
            modified={file.modified}
            language={language}
            originalModelPath={diffModelPath(file, "original")}
            modifiedModelPath={diffModelPath(file, "modified")}
            theme={monacoTheme}
            beforeMount={beforeMount}
            options={{
              automaticLayout: true,
              diffCodeLens: false,
              domReadOnly: true,
              enableSplitViewResizing: false,
              folding: true,
              fontSize: 12,
              glyphMargin: false,
              ignoreTrimWhitespace: false,
              lineDecorationsWidth: 16,
              lineNumbersMinChars: 3,
              minimap: { enabled: false },
              originalEditable: false,
              readOnly: true,
              renderOverviewRuler: false,
              renderSideBySide: false,
              scrollBeyondLastLine: false,
              wordWrap: "off",
            }}
          />
        </Suspense>
      )}
    </div>
  );
}

function diffModelPath(file: SourceControlDiff, side: "original" | "modified") {
  const state = file.staged ? "staged" : "worktree";
  return `inmemory://renart/source-control/${state}/${side}/${encodeURIComponent(file.path)}`;
}

function groupedDiffHeight(file: SourceControlDiff) {
  if (file.binary || file.original === file.modified) return 160;
  const lines = Math.max(file.original.split("\n").length, file.modified.split("\n").length);
  return Math.min(480, Math.max(180, lines * 19 + 58));
}

export function sourceControlLanguageForPath(path: string) {
  const lower = path.toLowerCase();
  const fileName = lower.split("/").pop() ?? lower;
  if (fileName === "dockerfile") return "dockerfile";
  if (lower.endsWith(".sql")) return "sql";
  if (lower.endsWith(".py")) return "python";
  if (lower.endsWith(".yml") || lower.endsWith(".yaml")) return "yaml";
  if (lower.endsWith(".json") || lower.endsWith(".jsonl")) return "json";
  if (lower.endsWith(".md") || lower.endsWith(".mdx")) return "markdown";
  if (lower.endsWith(".ts")) return "typescript";
  if (lower.endsWith(".tsx")) return "typescript";
  if (lower.endsWith(".js") || lower.endsWith(".jsx")) return "javascript";
  if (lower.endsWith(".go")) return "go";
  if (lower.endsWith(".css")) return "css";
  if (lower.endsWith(".scss")) return "scss";
  if (lower.endsWith(".html")) return "html";
  if (lower.endsWith(".xml")) return "xml";
  if (lower.endsWith(".toml")) return "ini";
  if (lower.endsWith(".sh") || lower.endsWith(".bash")) return "shell";
  return "plaintext";
}
