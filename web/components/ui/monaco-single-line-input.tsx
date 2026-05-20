"use client";

import type { Monaco } from "@monaco-editor/react";
import type * as MonacoNS from "monaco-editor";
import { lazy, Suspense, useId, useRef } from "react";

import { loadMonacoEditorModule } from "@/lib/load-monaco-editor";
import { defineBruinMonacoThemes } from "@/lib/monaco-theme";
import { cn } from "@/lib/utils";

const MonacoEditor = lazy(async () => {
  const module = await loadMonacoEditorModule();
  return { default: module.default };
});

export type MonacoSingleLineCompletionProvider = (args: {
  monaco: Monaco;
  model: MonacoNS.editor.ITextModel;
  position: MonacoNS.Position;
}) => MonacoNS.languages.ProviderResult<MonacoNS.languages.CompletionList>;

export function MonacoSingleLineInput({
  value,
  onChange,
  onEnter,
  className,
  disabled = false,
  id,
  language = "plaintext",
  "aria-describedby": ariaDescribedBy,
  "aria-invalid": ariaInvalid,
  path,
  placeholder,
  theme = "bruin-vs",
  completionProvider,
}: {
  value: string;
  onChange: (value: string) => void;
  onEnter?: () => void;
  className?: string;
  disabled?: boolean;
  id?: string;
  language?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
  path?: string;
  placeholder?: string;
  theme?: string;
  completionProvider?: MonacoSingleLineCompletionProvider;
}) {
  const generatedId = useId().replace(/:/g, "");
  const editorRef = useRef<MonacoNS.editor.IStandaloneCodeEditor | null>(null);
  const modelUriRef = useRef<string | null>(null);

  const sanitizeValue = (nextValue?: string) =>
    (nextValue ?? "").replace(/\s*[\r\n]+\s*/g, " ");

  return (
    <div
      id={id}
      role="textbox"
      aria-describedby={ariaDescribedBy}
      aria-invalid={ariaInvalid}
      aria-readonly={disabled}
      aria-multiline={false}
      tabIndex={disabled ? -1 : 0}
      className={cn(
        "dark:bg-input/30 border-input focus-within:border-ring focus-within:ring-ring/50 aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 aria-invalid:border-destructive dark:aria-invalid:border-destructive/50 h-8 w-full min-w-0 rounded-lg border bg-transparent text-base transition-colors focus-within:ring-3 aria-invalid:ring-3 md:text-sm",
        disabled && "pointer-events-none cursor-not-allowed opacity-50",
        className
      )}
      onFocus={() => editorRef.current?.focus()}
    >
      <div className="relative h-full min-w-0 rounded-lg [&_.monaco-editor]:rounded-lg [&_.overflow-guard]:rounded-lg">
        {!value && placeholder ? (
          <div className="pointer-events-none absolute inset-x-2.5 top-1/2 z-10 -translate-y-1/2 truncate text-muted-foreground">
            {placeholder}
          </div>
        ) : null}
        <Suspense
          fallback={
            <div className="flex h-full items-center px-2.5 text-muted-foreground">
              Loading...
            </div>
          }
        >
          <MonacoEditor
            height="100%"
            language={language}
            path={path ?? `renart-single-line-${generatedId}.${language}`}
            value={value}
            theme={theme}
            beforeMount={defineBruinMonacoThemes}
            onChange={(nextValue) => {
              const sanitized = sanitizeValue(nextValue);
              if (nextValue !== undefined && sanitized !== nextValue) {
                editorRef.current?.setValue(sanitized);
              }
              onChange(sanitized);
            }}
            onMount={(editor, monaco) => {
              defineBruinMonacoThemes(monaco);
              editorRef.current = editor;
              modelUriRef.current = editor.getModel()?.uri.toString() ?? null;
              editor.addCommand(monaco.KeyCode.Enter, () => onEnter?.());
              editor.addCommand(monaco.KeyMod.Shift | monaco.KeyCode.Enter, () => onEnter?.());
              editor.addCommand(monaco.KeyCode.Escape, () => {
                if (editor.getContribution("editor.contrib.suggestController")) {
                  editor.trigger("keyboard", "hideSuggestWidget", null);
                }
              });

              if (completionProvider) {
                const provider: MonacoNS.languages.CompletionItemProvider = {
                  triggerCharacters: ["@", "*", " "],
                  provideCompletionItems: (model, position) => {
                    if (model.uri.toString() !== modelUriRef.current) {
                      return { suggestions: [] };
                    }
                    return completionProvider({ monaco, model, position });
                  },
                };
                const disposable = monaco.languages.registerCompletionItemProvider(
                  language,
                  provider
                );
                editor.onDidDispose(() => disposable.dispose());
              }
            }}
            options={{
              automaticLayout: true,
              contextmenu: false,
              cursorBlinking: "smooth",
              fixedOverflowWidgets: false,
              folding: false,
              glyphMargin: false,
              hideCursorInOverviewRuler: true,
              lineDecorationsWidth: 10,
              lineNumbers: "off",
              lineNumbersMinChars: 0,
              minimap: { enabled: false },
              overviewRulerBorder: false,
              overviewRulerLanes: 0,
              padding: { top: 6, bottom: 0 },
              readOnly: disabled,
              renderLineHighlight: "none",
              renderValidationDecorations: "off",
              scrollBeyondLastColumn: 0,
              scrollBeyondLastLine: false,
              scrollbar: {
                alwaysConsumeMouseWheel: false,
                horizontal: "hidden",
                vertical: "hidden",
              },
              suggestOnTriggerCharacters: true,
              tabFocusMode: true,
              wordWrap: "off",
            }}
          />
        </Suspense>
      </div>
    </div>
  );
}
