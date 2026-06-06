"use client";

import type * as MonacoNS from "monaco-editor";
import { useEffect, useRef } from "react";

import {
  formatPythonAsset,
  getPythonCompletions,
  getPythonDiagnostics,
  getPythonGotoDefinition,
  getPythonHover,
  getPythonSignatureHelp,
} from "@/lib/api";
import type {
  PythonCompletion,
  PythonDiagnostic,
  PythonGotoTarget,
  PythonRange,
  PythonSignatureHelp,
  WebAsset,
} from "@/lib/types";

const pythonMarkerOwner = "bruin-python-ty";

export function usePythonIntellisense(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  asset: WebAsset | null,
  content: string,
) {
  const assetRef = useRef<WebAsset | null>(null);
  const isPythonAsset = !!asset?.path.toLowerCase().endsWith(".py");

  useEffect(() => {
    assetRef.current = asset;
  }, [asset]);

  useEffect(() => {
    if (!monaco) {
      return;
    }

    const formatting = monaco.languages.registerDocumentFormattingEditProvider("python", {
      async provideDocumentFormattingEdits(model) {
        const currentAsset = assetRef.current;
        if (!currentAsset?.id || !currentAsset.path.toLowerCase().endsWith(".py")) {
          return [];
        }

        const response = await formatPythonAsset(currentAsset.id, model.getValue()).catch(
          () => null,
        );
        if (!response || response.status !== "ok" || response.content === model.getValue()) {
          return [];
        }
        return [
          {
            range: model.getFullModelRange(),
            text: response.content,
          },
        ];
      },
    });

    const completions = monaco.languages.registerCompletionItemProvider("python", {
      triggerCharacters: ["."],
      async provideCompletionItems(model, position, context, token) {
        const currentAsset = assetRef.current;
        if (!currentAsset?.id || !currentAsset.path.toLowerCase().endsWith(".py")) {
          return { suggestions: [] };
        }

        const controller = new AbortController();
        const cancellation = token.onCancellationRequested(() => controller.abort());
        try {
          const response = await getPythonCompletions(
            currentAsset.id,
            {
              content: model.getValue(),
              line: position.lineNumber,
              column: position.column,
              snippets: shouldUsePythonCompletionSnippets(model, position, context),
            },
            controller.signal,
          );
          if (token.isCancellationRequested || response.status !== "ok") {
            return { suggestions: [] };
          }
          const word = model.getWordUntilPosition(position);
          const range = {
            startLineNumber: position.lineNumber,
            startColumn: word.startColumn,
            endLineNumber: position.lineNumber,
            endColumn: word.endColumn,
          };
          return {
            suggestions: (response.completions ?? []).map((completion, index) =>
              completionToSuggestion(monaco, completion, index, range),
            ),
          };
        } catch (error) {
          if (controller.signal.aborted || error instanceof DOMException) {
            return { suggestions: [] };
          }
          return { suggestions: [] };
        } finally {
          cancellation.dispose();
        }
      },
    });

    const hover = monaco.languages.registerHoverProvider("python", {
      async provideHover(model, position, token) {
        const currentAsset = assetRef.current;
        if (!currentAsset?.id || !currentAsset.path.toLowerCase().endsWith(".py")) {
          return null;
        }
        const controller = new AbortController();
        const cancellation = token.onCancellationRequested(() => controller.abort());
        try {
          const response = await getPythonHover(
            currentAsset.id,
            positionRequest(model, position),
            controller.signal,
          );
          if (token.isCancellationRequested || response.status !== "ok" || !response.hover) {
            return null;
          }
          return {
            contents: [{ value: response.hover.contents, isTrusted: false }],
            range: response.hover.range ? rangeToMonaco(response.hover.range) : undefined,
          };
        } catch {
          return null;
        } finally {
          cancellation.dispose();
        }
      },
    });

    const signature = monaco.languages.registerSignatureHelpProvider("python", {
      signatureHelpTriggerCharacters: ["(", ","],
      signatureHelpRetriggerCharacters: [","],
      async provideSignatureHelp(model, position, token) {
        const currentAsset = assetRef.current;
        if (!currentAsset?.id || !currentAsset.path.toLowerCase().endsWith(".py")) {
          return null;
        }
        const controller = new AbortController();
        const cancellation = token.onCancellationRequested(() => controller.abort());
        try {
          const response = await getPythonSignatureHelp(
            currentAsset.id,
            positionRequest(model, position),
            controller.signal,
          );
          if (token.isCancellationRequested || response.status !== "ok" || !response.signature_help) {
            return null;
          }
          return {
            value: signatureHelpToMonaco(response.signature_help),
            dispose: () => {},
          };
        } catch {
          return null;
        } finally {
          cancellation.dispose();
        }
      },
    });

    const definition = monaco.languages.registerDefinitionProvider("python", {
      async provideDefinition(model, position, token) {
        const currentAsset = assetRef.current;
        if (!currentAsset?.id || !currentAsset.path.toLowerCase().endsWith(".py")) {
          return [];
        }
        const controller = new AbortController();
        const cancellation = token.onCancellationRequested(() => controller.abort());
        try {
          const response = await getPythonGotoDefinition(
            currentAsset.id,
            positionRequest(model, position),
            controller.signal,
          );
          if (token.isCancellationRequested || response.status !== "ok") {
            return [];
          }
          return (response.targets ?? []).map((target) => gotoTargetToLocation(monaco, target));
        } catch {
          return [];
        } finally {
          cancellation.dispose();
        }
      },
    });

    return () => {
      formatting.dispose();
      completions.dispose();
      hover.dispose();
      signature.dispose();
      definition.dispose();
    };
  }, [monaco]);

  useEffect(() => {
    if (!monaco || !editor || !asset?.id || !isPythonAsset) {
      clearPythonMarkers(monaco, editor);
      return;
    }

    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      const model = editor.getModel();
      if (!model) {
        return;
      }

      void getPythonDiagnostics(asset.id, content, controller.signal)
        .then((response) => {
          if (controller.signal.aborted || response.status !== "ok") {
            return;
          }
          monaco.editor.setModelMarkers(
            model,
            pythonMarkerOwner,
            (response.diagnostics ?? []).flatMap((diagnostic) =>
              diagnosticToMarker(monaco, diagnostic),
            ),
          );
        })
        .catch((error) => {
          if (controller.signal.aborted || error?.name === "AbortError") {
            return;
          }
          monaco.editor.setModelMarkers(model, pythonMarkerOwner, []);
        });
    }, 250);

    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [asset?.id, content, editor, isPythonAsset, monaco]);
}

function shouldUsePythonCompletionSnippets(
  model: MonacoNS.editor.ITextModel,
  position: MonacoNS.Position,
  context: MonacoNS.languages.CompletionContext,
) {
  if (context.triggerCharacter === ".") {
    return false;
  }
  if (position.column > 1 && model.getLineContent(position.lineNumber).at(position.column - 2) === ".") {
    return false;
  }
  return true;
}

function positionRequest(
  model: MonacoNS.editor.ITextModel,
  position: MonacoNS.Position,
) {
  return {
    content: model.getValue(),
    line: position.lineNumber,
    column: position.column,
  };
}

function rangeToMonaco(range: PythonRange): MonacoNS.IRange {
  return {
    startLineNumber: range.start.line,
    startColumn: range.start.column,
    endLineNumber: range.end.line,
    endColumn: range.end.column,
  };
}

function signatureHelpToMonaco(
  help: PythonSignatureHelp,
): MonacoNS.languages.SignatureHelp {
  return {
    signatures: help.signatures.map((signature) => ({
      label: signature.label,
      documentation: signature.documentation
        ? { value: signature.documentation, isTrusted: false }
        : undefined,
      parameters: signature.parameters.map((parameter) => ({
        label: parameter.label,
        documentation: parameter.documentation || parameter.type,
      })),
      activeParameter: signature.active_parameter,
    })),
    activeSignature: help.active_signature ?? 0,
    activeParameter: help.active_parameter ?? 0,
  };
}

function gotoTargetToLocation(
  monaco: typeof MonacoNS,
  target: PythonGotoTarget,
): MonacoNS.languages.Location {
  return {
    uri: monaco.Uri.file(target.path.startsWith("/") ? target.path : `/${target.path}`),
    range: rangeToMonaco(target.focus_range),
  };
}

function completionToSuggestion(
  monaco: typeof MonacoNS,
  completion: PythonCompletion,
  index: number,
  range: MonacoNS.IRange,
): MonacoNS.languages.CompletionItem {
  const importDetail = completion.module_name ? `import ${completion.module_name}` : undefined;
  return {
    label: completion.label,
    kind: completionKindToMonaco(monaco, completion.kind),
    range,
    detail: [completion.detail, importDetail].filter(Boolean).join(" ") || undefined,
    insertText: completion.insert_text || completion.label,
    insertTextRules:
      completion.insert_text_format === "snippet"
        ? monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet
        : undefined,
    documentation: completion.documentation
      ? { value: completion.documentation, isTrusted: false }
      : undefined,
    sortText: String(index).padStart(4, "0"),
    additionalTextEdits: completion.additional_text_edits?.map((edit) => ({
      range: {
        startLineNumber: edit.range.start.line,
        startColumn: edit.range.start.column,
        endLineNumber: edit.range.end.line,
        endColumn: edit.range.end.column,
      },
      text: edit.text,
    })),
  };
}

function completionKindToMonaco(monaco: typeof MonacoNS, kind?: string) {
  switch (kind) {
    case "method":
      return monaco.languages.CompletionItemKind.Method;
    case "function":
      return monaco.languages.CompletionItemKind.Function;
    case "constructor":
      return monaco.languages.CompletionItemKind.Constructor;
    case "field":
      return monaco.languages.CompletionItemKind.Field;
    case "variable":
      return monaco.languages.CompletionItemKind.Variable;
    case "class":
      return monaco.languages.CompletionItemKind.Class;
    case "interface":
      return monaco.languages.CompletionItemKind.Interface;
    case "module":
      return monaco.languages.CompletionItemKind.Module;
    case "property":
      return monaco.languages.CompletionItemKind.Property;
    case "enum":
      return monaco.languages.CompletionItemKind.Enum;
    case "keyword":
      return monaco.languages.CompletionItemKind.Keyword;
    case "snippet":
      return monaco.languages.CompletionItemKind.Snippet;
    case "constant":
      return monaco.languages.CompletionItemKind.Constant;
    case "enum-member":
      return monaco.languages.CompletionItemKind.EnumMember;
    case "struct":
      return monaco.languages.CompletionItemKind.Struct;
    case "operator":
      return monaco.languages.CompletionItemKind.Operator;
    case "type-parameter":
      return monaco.languages.CompletionItemKind.TypeParameter;
    case "text":
    default:
      return monaco.languages.CompletionItemKind.Text;
  }
}

function diagnosticToMarker(
  monaco: typeof MonacoNS,
  diagnostic: PythonDiagnostic,
): MonacoNS.editor.IMarkerData[] {
  const range = diagnostic.range;
  if (!range) {
    return [];
  }
  return [
    {
      severity: severityToMarker(monaco, diagnostic.severity),
      message: diagnostic.message,
      startLineNumber: range.start.line,
      startColumn: range.start.column,
      endLineNumber: range.end.line,
      endColumn: range.end.column,
    },
  ];
}

function severityToMarker(
  monaco: typeof MonacoNS,
  severity: PythonDiagnostic["severity"],
) {
  switch (severity) {
    case "info":
      return monaco.MarkerSeverity.Info;
    case "warning":
      return monaco.MarkerSeverity.Warning;
    case "error":
    case "fatal":
    default:
      return monaco.MarkerSeverity.Error;
  }
}

function clearPythonMarkers(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
) {
  const model = editor?.getModel();
  if (monaco && model) {
    monaco.editor.setModelMarkers(model, pythonMarkerOwner, []);
  }
}
