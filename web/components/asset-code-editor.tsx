"use client";

import type { Monaco } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";
import { lazy, Suspense, useMemo, useState } from "react";

import { SqlFormatOverlayButton } from "@/components/sql-format-overlay-button";
import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { WebAsset } from "@/lib/types";

const MonacoEditor = lazy(async () => {
  const module = await loadMonacoEditorModule();
  return { default: module.default };
});

export function AssetCodeEditor({
  asset,
  containerClassName,
  editorModelPath,
  editorValue,
  editorValueMode = "controlled",
  editorHighlighted,
  helpMode,
  highlightStyle,
  isSqlAsset,
  formatShortcutLabel,
  mobile,
  monacoTheme,
  onChange,
  onBeforeMount,
  onFormat,
  onMount,
}: {
  asset: WebAsset | null;
  containerClassName?: string;
  editorModelPath: string;
  editorValue: string;
  editorValueMode?: "controlled" | "initial";
  editorHighlighted: boolean;
  helpMode: boolean;
  highlightStyle?: React.CSSProperties;
  isSqlAsset: boolean;
  formatShortcutLabel: string;
  mobile: boolean;
  monacoTheme: string;
  onChange: (value?: string) => void;
  onBeforeMount: (monaco: Monaco) => void;
  onFormat: () => void;
  onMount: (editor: MonacoNS.editor.IStandaloneCodeEditor, monaco: Monaco) => void;
}) {
  const [showFormatButton, setShowFormatButton] = useState(false);
  const isPythonAsset = Boolean(
    asset && (asset.path.toLowerCase().endsWith(".py") || asset.type?.toLowerCase() === "python"),
  );
  const editorOptions = useMemo(
    () => ({
      minimap: { enabled: false },
      fontSize: 13,
      fixedOverflowWidgets: true,
      // Monaco normally suppresses suggestions inside strings. Plain Python
      // query("...") literals are projected into SQL, so let that provider
      // participate during ordinary typing rather than only after Ctrl+Space.
      quickSuggestions: isPythonAsset ? { other: true, comments: false, strings: true } : true,
      suggestOnTriggerCharacters: true,
      // Track the container via Monaco's own ResizeObserver. Without this the
      // editor only re-measures on a window resize, so when a sibling (e.g. the
      // parse-error banner) appears and shrinks the container, Monaco keeps its
      // stale height and overflows — which oscillates a scrollbar until the
      // window is resized.
      automaticLayout: true,
    }),
    [isPythonAsset],
  );

  const handlePointerActivity = () => {
    if (!isSqlAsset) {
      return;
    }

    setShowFormatButton(true);
  };

  return (
    <div
      className={`relative overflow-hidden ${
        containerClassName ?? `${mobile ? "min-h-[240px]" : "h-[55%]"} border-b`
      } ${helpMode && editorHighlighted ? "ring-2 ring-primary/70 ring-inset" : ""}`}
      style={helpMode && editorHighlighted ? highlightStyle : undefined}
      onMouseMove={handlePointerActivity}
      onMouseLeave={() => setShowFormatButton(false)}
    >
      {isSqlAsset ? (
        <SqlFormatOverlayButton
          visible={showFormatButton}
          shortcutLabel={formatShortcutLabel}
          onFormat={onFormat}
        />
      ) : null}
      <Suspense
        fallback={
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Loading editor...
          </div>
        }
      >
        <MonacoEditor
          language={asset ? editorLanguageForAsset(asset) : "sql"}
          path={editorModelPath}
          saveViewState
          keepCurrentModel
          {...(editorValueMode === "initial"
            ? { defaultValue: editorValue }
            : { value: editorValue })}
          theme={monacoTheme}
          beforeMount={onBeforeMount}
          onChange={onChange}
          onMount={onMount}
          options={editorOptions}
        />
      </Suspense>
    </div>
  );
}

function editorLanguageForAsset(asset: WebAsset): "sql" | "python" | "yaml" {
  const lowerPath = asset.path.toLowerCase();
  if (lowerPath.endsWith(".py") || asset.type?.toLowerCase() === "python") {
    return "python";
  }
  if (lowerPath.endsWith(".yml") || lowerPath.endsWith(".yaml")) {
    return "yaml";
  }
  return "sql";
}
