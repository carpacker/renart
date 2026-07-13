"use client";

import type * as MonacoNS from "monaco-editor";
import { useEffect, useRef } from "react";
import { useAtomValue } from "jotai";

import {
  getSQLLSPCompletions,
  getSQLLSPDefinition,
  getSQLLSPDiagnostics,
  getSQLLSPHover,
  getSQLLSPSemanticTokens,
  getSQLLSPSignatureHelp,
  SQLLSPCompletionItem,
  SQLLSPPosition,
  SQLLSPRange,
  SQLLSPSignatureHelp,
} from "@/lib/api-sql-lsp";
import { workspaceAtom } from "@/lib/atoms/domains/workspace";
import {
  findPythonQueryLiterals,
  PythonQueryLiteral,
  pythonQueryLiteralAtOffset,
  sourceOffsetForSQLOffset,
  sqlOffsetForSourceOffset,
} from "@/lib/python-query-literals";
import { WebAsset, WorkspaceState } from "@/lib/types";

const PYTHON_QUERY_SQL_MARKER_OWNER = "renart-python-query-sql";
const SQL_SEMANTIC_KINDS = ["schema", "table", "column", "alias"] as const;

type PythonQueryProviderState = {
  asset: WebAsset | null;
  workspace: WorkspaceState | null;
  onGoToAsset?: (pipelineId: string, assetId: string) => void;
  onGoToCell?: (cellId: string) => void;
};

const pythonQueryEntries = new Map<string, { current: PythonQueryProviderState }>();
const pythonQueryProviderRegistry = new Map<
  typeof MonacoNS,
  { disposable: MonacoNS.IDisposable; refs: number }
>();

/**
 * Projects plain SQL string literals passed to query()/renart.query() into the
 * existing SQL LSP. Python remains the host document; all LSP positions are
 * translated through the literal's source map before Monaco sees them.
 */
export function usePythonQueryIntellisense(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  asset: WebAsset | null,
  content: string,
  onGoToAsset?: (pipelineId: string, assetId: string) => void,
  onGoToCell?: (cellId: string) => void,
) {
  const workspace = useAtomValue(workspaceAtom);
  const providerStateRef = useRef<PythonQueryProviderState>({
    asset,
    workspace,
    onGoToAsset,
    onGoToCell,
  });
  providerStateRef.current = { asset, workspace, onGoToAsset, onGoToCell };
  const isPythonAsset = isPython(asset);

  useEffect(() => {
    if (!monaco || !editor || !isPythonAsset) {
      return;
    }
    const model = editor.getModel();
    if (!model) {
      return;
    }
    const uri = model.uri.toString();
    pythonQueryEntries.set(uri, providerStateRef);
    const release = acquirePythonQueryProviders(monaco);
    return () => {
      pythonQueryEntries.delete(uri);
      release();
    };
  }, [asset?.id, editor, isPythonAsset, monaco]);

  // A Python model tokenizes the entire literal as one Python string. Render
  // SQL lexical tokens and LSP relation tokens as model decorations so the
  // embedded language is visible for both complete and in-progress calls.
  // This also avoids relying on a second document semantic-token provider for
  // the Python language, which Monaco does not compose predictably.
  useEffect(() => {
    if (!monaco || !editor || !asset?.id || !isPythonAsset) {
      return;
    }
    const model = editor.getModel();
    if (!model) {
      return;
    }

    let decorationIds: string[] = [];
    let semanticTimer: number | null = null;
    let refreshRevision = 0;
    let disposed = false;

    const refresh = () => {
      const revision = ++refreshRevision;
      const literals = findPythonQueryLiterals(model.getValue());
      const lexical = embeddedSQLLexicalDecorations(monaco, model, literals);
      decorationIds = editor.deltaDecorations(decorationIds, lexical);

      if (semanticTimer !== null) {
        window.clearTimeout(semanticTimer);
      }
      if (literals.length === 0) {
        return;
      }
      semanticTimer = window.setTimeout(() => {
        void Promise.all(
          literals.map((literal) =>
            getSQLLSPSemanticTokens({
              asset_id: asset.id,
              content: literal.sql,
            }).catch(() => null),
          ),
        ).then((responses) => {
          if (disposed || revision !== refreshRevision) {
            return;
          }
          const semantic = embeddedSQLSemanticDecorations(model, literals, responses);
          decorationIds = editor.deltaDecorations(decorationIds, [...lexical, ...semantic]);
        });
      }, 120);
    };

    refresh();
    const changeSubscription = model.onDidChangeContent(refresh);
    return () => {
      disposed = true;
      changeSubscription.dispose();
      if (semanticTimer !== null) {
        window.clearTimeout(semanticTimer);
      }
      decorationIds = editor.deltaDecorations(decorationIds, []);
    };
  }, [asset?.id, editor, isPythonAsset, monaco]);

  // Ctrl/Cmd+click is explicit navigation (matching SQL cells/assets). Monaco
  // does not know the host string contains SQL, so bridge the click directly.
  useEffect(() => {
    if (!editor || !isPythonAsset || !asset?.id) {
      return;
    }
    const subscription = editor.onMouseDown((event) => {
      if (!event.event.leftButton || (!event.event.ctrlKey && !event.event.metaKey)) {
        return;
      }
      const model = editor.getModel();
      const position = event.target.position;
      if (!model || !position) {
        return;
      }
      const projected = projectPosition(model, position);
      if (!projected) {
        return;
      }
      event.event.preventDefault();
      event.event.stopPropagation();
      void getSQLLSPDefinition({
        asset_id: asset.id,
        content: projected.literal.sql,
        position: projected.sqlPosition,
      })
        .then((response) => {
          const location = (response.locations ?? []).find((candidate) => candidate.asset_id);
          if (!location?.asset_id) {
            return;
          }
          const target = findWorkspaceAsset(providerStateRef.current.workspace, location.asset_id);
          if (!target || target.asset.id === asset.id) {
            return;
          }
          if (target.pipeline) {
            providerStateRef.current.onGoToAsset?.(target.pipeline.id, target.asset.id);
          } else if (target.asset.cell_id) {
            providerStateRef.current.onGoToCell?.(target.asset.cell_id);
          }
        })
        .catch(() => undefined);
    });
    return () => subscription.dispose();
  }, [asset?.id, editor, isPythonAsset]);

  useEffect(() => {
    if (!monaco || !editor || !asset?.id || !isPythonAsset) {
      clearPythonQueryMarkers(monaco, editor);
      return;
    }
    const model = editor.getModel();
    if (!model) {
      return;
    }
    let cancelled = false;
    const timer = window.setTimeout(() => {
      const literals = findPythonQueryLiterals(model.getValue());
      if (literals.length === 0) {
        monaco.editor.setModelMarkers(model, PYTHON_QUERY_SQL_MARKER_OWNER, []);
        return;
      }
      void Promise.all(
        literals.map(async (literal) => ({
          literal,
          response: await getSQLLSPDiagnostics({
            asset_id: asset.id,
            content: literal.sql,
          }),
        })),
      )
        .then((results) => {
          if (cancelled) {
            return;
          }
          monaco.editor.setModelMarkers(
            model,
            PYTHON_QUERY_SQL_MARKER_OWNER,
            results.flatMap(({ literal, response }) =>
              (response.diagnostics ?? []).map((diagnostic) => ({
                message: diagnostic.message,
                code: diagnostic.code,
                source: diagnostic.source ?? "SQL in query()",
                severity:
                  diagnostic.severity === 1
                    ? monaco.MarkerSeverity.Error
                    : monaco.MarkerSeverity.Warning,
                ...sqlRangeToHostRange(model, literal, diagnostic.range),
              })),
            ),
          );
        })
        .catch(() => {
          // Preserve the last-known SQL markers through a transient request
          // failure, matching the ordinary SQL editor's behavior.
        });
    }, 250);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [asset?.id, content, editor, isPythonAsset, monaco]);
}

function acquirePythonQueryProviders(monaco: typeof MonacoNS) {
  let registration = pythonQueryProviderRegistry.get(monaco);
  if (!registration) {
    registration = { disposable: registerPythonQueryProviders(monaco), refs: 0 };
    pythonQueryProviderRegistry.set(monaco, registration);
  }
  registration.refs += 1;
  return () => {
    const current = pythonQueryProviderRegistry.get(monaco);
    if (!current) {
      return;
    }
    current.refs -= 1;
    if (current.refs <= 0) {
      current.disposable.dispose();
      pythonQueryProviderRegistry.delete(monaco);
    }
  };
}

function registerPythonQueryProviders(monaco: typeof MonacoNS): MonacoNS.IDisposable {
  const resolveState = (model: MonacoNS.editor.ITextModel) =>
    pythonQueryEntries.get(model.uri.toString())?.current ?? null;

  const completion = monaco.languages.registerCompletionItemProvider("python", {
    triggerCharacters: ["."],
    async provideCompletionItems(model, position, _context, token) {
      const state = resolveState(model);
      const projected = projectPosition(model, position);
      if (!state?.asset?.id || !projected) {
        return { suggestions: [] };
      }
      const response = await getSQLLSPCompletions({
        asset_id: state.asset.id,
        content: projected.literal.sql,
        position: projected.sqlPosition,
      }).catch(() => null);
      if (!response || token.isCancellationRequested) {
        return { suggestions: [] };
      }
      const word = model.getWordUntilPosition(position);
      const sourceWordStart = model.getOffsetAt({
        lineNumber: position.lineNumber,
        column: word.startColumn,
      });
      const start = model.getPositionAt(Math.max(projected.literal.sourceStart, sourceWordStart));
      const range = new monaco.Range(
        start.lineNumber,
        start.column,
        position.lineNumber,
        word.endColumn,
      );
      return {
        suggestions: (response.completions ?? [])
          .filter((item) => item.kind === 5 || item.kind === 18 || item.kind === 2)
          .map((item) => completionToMonaco(monaco, item, range)),
      };
    },
  });

  const hover = monaco.languages.registerHoverProvider("python", {
    async provideHover(model, position, token) {
      const state = resolveState(model);
      const projected = projectPosition(model, position);
      if (!state?.asset?.id || !projected) {
        return null;
      }
      const response = await getSQLLSPHover({
        asset_id: state.asset.id,
        content: projected.literal.sql,
        position: projected.sqlPosition,
      }).catch(() => null);
      if (!response?.hover || token.isCancellationRequested) {
        return null;
      }
      return {
        contents: [{ value: response.hover.contents, isTrusted: false }],
        range: response.hover.range
          ? sqlRangeToHostRange(model, projected.literal, response.hover.range)
          : undefined,
      };
    },
  });

  const definition = monaco.languages.registerDefinitionProvider("python", {
    async provideDefinition(model, position, token) {
      const state = resolveState(model);
      const projected = projectPosition(model, position);
      if (!state?.asset?.id || !projected) {
        return [];
      }
      const response = await getSQLLSPDefinition({
        asset_id: state.asset.id,
        content: projected.literal.sql,
        position: projected.sqlPosition,
      }).catch(() => null);
      if (!response || token.isCancellationRequested) {
        return [];
      }
      return (response.locations ?? []).flatMap((location) => {
        if (location.asset_id) {
          const target = findWorkspaceAsset(state.workspace, location.asset_id);
          if (!target) {
            return [];
          }
          return [
            {
              uri: ensureAssetPreviewModel(monaco, target.asset),
              range: sqlRangeToMonaco(monaco, location.range),
            },
          ];
        }
        return [
          {
            uri: model.uri,
            range: sqlRangeToHostRange(model, projected.literal, location.range),
          },
        ];
      });
    },
  });

  const signature = monaco.languages.registerSignatureHelpProvider("python", {
    signatureHelpTriggerCharacters: ["(", ","],
    signatureHelpRetriggerCharacters: [","],
    async provideSignatureHelp(model, position, token) {
      const state = resolveState(model);
      const projected = projectPosition(model, position);
      if (!state?.asset?.id || !projected) {
        return null;
      }
      const response = await getSQLLSPSignatureHelp({
        asset_id: state.asset.id,
        content: projected.literal.sql,
        position: projected.sqlPosition,
      }).catch(() => null);
      if (!response?.signature || token.isCancellationRequested) {
        return null;
      }
      return { value: signatureHelpToMonaco(response.signature), dispose: () => undefined };
    },
  });

  return {
    dispose() {
      completion.dispose();
      hover.dispose();
      definition.dispose();
      signature.dispose();
    },
  };
}

function projectPosition(model: MonacoNS.editor.ITextModel, position: MonacoNS.Position) {
  const sourceOffset = model.getOffsetAt(position);
  const literal = pythonQueryLiteralAtOffset(model.getValue(), sourceOffset);
  if (!literal) {
    return null;
  }
  const sqlOffset = sqlOffsetForSourceOffset(literal, sourceOffset);
  return { literal, sqlPosition: sqlPositionAtOffset(literal.sql, sqlOffset) };
}

function sqlPositionAtOffset(sql: string, offset: number): SQLLSPPosition {
  const bounded = Math.min(Math.max(offset, 0), sql.length);
  const before = sql.slice(0, bounded);
  const lastNewline = before.lastIndexOf("\n");
  return {
    line: before.split("\n").length - 1,
    character: bounded - lastNewline - 1,
  };
}

function sqlOffsetAtPosition(sql: string, position: SQLLSPPosition) {
  let offset = 0;
  let line = 0;
  while (line < position.line && offset < sql.length) {
    const newline = sql.indexOf("\n", offset);
    if (newline < 0) {
      return sql.length;
    }
    offset = newline + 1;
    line += 1;
  }
  return Math.min(offset + position.character, sql.length);
}

function sqlRangeToHostRange(
  model: MonacoNS.editor.ITextModel,
  literal: PythonQueryLiteral,
  range: SQLLSPRange,
) {
  const start = model.getPositionAt(
    sourceOffsetForSQLOffset(literal, sqlOffsetAtPosition(literal.sql, range.start)),
  );
  const end = model.getPositionAt(
    sourceOffsetForSQLOffset(literal, sqlOffsetAtPosition(literal.sql, range.end)),
  );
  return {
    startLineNumber: start.lineNumber,
    startColumn: start.column,
    endLineNumber: end.lineNumber,
    endColumn: end.column,
  };
}

function completionToMonaco(
  monaco: typeof MonacoNS,
  item: SQLLSPCompletionItem,
  range: MonacoNS.IRange,
): MonacoNS.languages.CompletionItem {
  return {
    label: item.label,
    kind:
      item.kind === 2
        ? monaco.languages.CompletionItemKind.Keyword
        : item.kind === 5
          ? monaco.languages.CompletionItemKind.Field
          : monaco.languages.CompletionItemKind.Reference,
    detail: item.detail ? `${item.detail} · SQL in query()` : "SQL in query()",
    documentation: item.documentation ? { value: item.documentation } : undefined,
    insertText: item.insertText || item.label,
    range,
    sortText: item.sortText ? `sql-${item.sortText}` : `sql-${item.label}`,
  };
}

function signatureHelpToMonaco(help: SQLLSPSignatureHelp): MonacoNS.languages.SignatureHelp {
  return {
    signatures: help.signatures.map((signature) => ({
      label: signature.label,
      documentation: signature.documentation
        ? { value: signature.documentation, isTrusted: false }
        : undefined,
      parameters: (signature.parameters ?? []).map((parameter) => ({
        label: parameter.label,
        documentation: parameter.documentation,
      })),
      activeParameter: signature.activeParameter,
    })),
    activeSignature: help.activeSignature ?? 0,
    activeParameter: help.activeParameter ?? 0,
  };
}

function findWorkspaceAsset(workspace: WorkspaceState | null, assetId: string) {
  for (const pipeline of workspace?.pipelines ?? []) {
    const asset = pipeline.assets.find((candidate) => candidate.id === assetId);
    if (asset) {
      return { pipeline, asset };
    }
  }
  for (const notebook of workspace?.notebooks ?? []) {
    const asset = notebook.cells.find((candidate) => candidate.id === assetId);
    if (asset) {
      return { pipeline: null, asset };
    }
  }
  return null;
}

function ensureAssetPreviewModel(monaco: typeof MonacoNS, asset: WebAsset) {
  const basename = asset.name.trim() || asset.path.split("/").pop() || asset.id;
  const uri = monaco.Uri.from({
    scheme: "renart-asset",
    path: `/${basename.endsWith(".sql") ? basename : `${basename}.sql`}`,
  });
  const existing = monaco.editor.getModel(uri);
  if (existing) {
    if (existing.getValue() !== asset.content) {
      existing.setValue(asset.content);
    }
    return uri;
  }
  monaco.editor.createModel(asset.content, "sql", uri);
  return uri;
}

function sqlRangeToMonaco(monaco: typeof MonacoNS, range: SQLLSPRange) {
  return new monaco.Range(
    range.start.line + 1,
    range.start.character + 1,
    range.end.line + 1,
    range.end.character + 1,
  );
}

function embeddedSQLLexicalDecorations(
  monaco: typeof MonacoNS,
  model: MonacoNS.editor.ITextModel,
  literals: PythonQueryLiteral[],
): MonacoNS.editor.IModelDeltaDecoration[] {
  const decorations: MonacoNS.editor.IModelDeltaDecoration[] = [];

  for (const literal of literals) {
    const lines = literal.sql.split("\n");
    const tokenLines = monaco.editor.tokenize(literal.sql, "sql");
    let sqlLineOffset = 0;
    for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
      const line = lines[lineIndex];
      const tokens = tokenLines[lineIndex] ?? [];
      for (let tokenIndex = 0; tokenIndex < tokens.length; tokenIndex += 1) {
        const token = tokens[tokenIndex];
        const className = embeddedSQLTokenClass(token.type);
        const endOffset = tokens[tokenIndex + 1]?.offset ?? line.length;
        if (!className || endOffset <= token.offset) {
          continue;
        }
        const start = model.getPositionAt(
          sourceOffsetForSQLOffset(literal, sqlLineOffset + token.offset),
        );
        const end = model.getPositionAt(
          sourceOffsetForSQLOffset(literal, sqlLineOffset + endOffset),
        );
        decorations.push({
          range: {
            startLineNumber: start.lineNumber,
            startColumn: start.column,
            endLineNumber: end.lineNumber,
            endColumn: end.column,
          },
          options: { inlineClassName: className },
        });
      }
      sqlLineOffset += line.length + 1;
    }
  }

  return decorations;
}

function embeddedSQLTokenClass(tokenType: string) {
  const normalized = tokenType.toLowerCase();
  if (normalized.includes("comment")) {
    return "bruin-python-sql-comment";
  }
  if (normalized.includes("string")) {
    return "bruin-python-sql-string";
  }
  if (normalized.includes("number")) {
    return "bruin-python-sql-number";
  }
  if (normalized.includes("keyword")) {
    return "bruin-python-sql-keyword";
  }
  if (normalized.includes("predefined") || normalized.includes("type")) {
    return "bruin-python-sql-predefined";
  }
  if (normalized.includes("operator") || normalized.includes("delimiter")) {
    return "bruin-python-sql-operator";
  }
  return null;
}

function embeddedSQLSemanticDecorations(
  model: MonacoNS.editor.ITextModel,
  literals: PythonQueryLiteral[],
  responses: Array<{ tokens?: { data: number[] } } | null>,
): MonacoNS.editor.IModelDeltaDecoration[] {
  const decorations: MonacoNS.editor.IModelDeltaDecoration[] = [];

  for (let literalIndex = 0; literalIndex < literals.length; literalIndex += 1) {
    const literal = literals[literalIndex];
    const data = responses[literalIndex]?.tokens?.data ?? [];
    let sqlLine = 0;
    let sqlStart = 0;
    for (let index = 0; index + 4 < data.length; index += 5) {
      const deltaLine = data[index];
      sqlLine += deltaLine;
      sqlStart = deltaLine === 0 ? sqlStart + data[index + 1] : data[index + 1];
      const sqlLength = data[index + 2];
      const hostRange = sqlRangeToHostRange(model, literal, {
        start: { line: sqlLine, character: sqlStart },
        end: { line: sqlLine, character: sqlStart + sqlLength },
      });
      if (hostRange.startLineNumber !== hostRange.endLineNumber) {
        continue;
      }
      const kind = SQL_SEMANTIC_KINDS[data[index + 3]];
      if (!kind || hostRange.startColumn === hostRange.endColumn) {
        continue;
      }
      decorations.push({
        range: hostRange,
        options: { inlineClassName: `bruin-sql-token-${kind}` },
      });
    }
  }
  return decorations;
}

function isPython(asset: WebAsset | null): asset is WebAsset {
  return Boolean(
    asset?.id &&
    (asset.path.toLowerCase().endsWith(".py") || asset.type?.toLowerCase() === "python"),
  );
}

function clearPythonQueryMarkers(
  monaco: typeof MonacoNS | null,
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
) {
  const model = editor?.getModel();
  if (monaco && model) {
    monaco.editor.setModelMarkers(model, PYTHON_QUERY_SQL_MARKER_OWNER, []);
  }
}
