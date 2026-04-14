"use client";

import { useEffect, useRef } from "react";
import type * as MonacoNS from "monaco-editor";

import { SqlParseContextResponse } from "@/lib/types";

function decorationClassForKind(kind: string) {
  switch (kind) {
    case "schema":
      return "bruin-sql-token-schema";
    case "table":
      return "bruin-sql-token-table";
    case "column":
      return "bruin-sql-token-column";
    case "alias":
      return "bruin-sql-token-alias";
    default:
      return null;
  }
}

export function useSQLSemanticDecorations(
  editor: MonacoNS.editor.IStandaloneCodeEditor | null,
  parseContext: SqlParseContextResponse | null,
) {
  const decorationIdsRef = useRef<string[]>([]);

  useEffect(() => {
    if (!editor) {
      return;
    }

    if (!parseContext) {
      decorationIdsRef.current = editor.deltaDecorations(decorationIdsRef.current, []);
      return;
    }

    const model = editor.getModel();
    if (!model) {
      decorationIdsRef.current = editor.deltaDecorations(decorationIdsRef.current, []);
      return;
    }

    const decorations: MonacoNS.editor.IModelDeltaDecoration[] = [];
    const seen = new Set<string>();

    const addDecoration = (
      range: {
        line: number;
        col: number;
        end_line: number;
        end_col: number;
      },
      className: string,
      expectedText?: string,
    ) => {
      if (range.line !== range.end_line) {
        return;
      }

      const monacoRange = {
        startLineNumber: range.line,
        startColumn: range.col,
        endLineNumber: range.end_line,
        endColumn: range.end_col,
      };

      const actualText = model.getValueInRange(monacoRange);
      if (!actualText) {
        return;
      }

      if (expectedText) {
        const normalizedActual = actualText.replace(/^['"`]+|['"`]+$/g, "");
        if (normalizedActual !== expectedText) {
          return;
        }
      }

      const key = `${className}:${range.line}:${range.col}:${range.end_col}`;
      if (seen.has(key)) {
        return;
      }

      seen.add(key);
      decorations.push({
        range: monacoRange,
        options: {
          inlineClassName: className,
        },
      });
    };

    for (const table of parseContext.tables ?? []) {
      for (const part of table.parts ?? []) {
        const className = decorationClassForKind(part.kind);
        if (!className) {
          continue;
        }

        addDecoration(part.range, className, part.name);
      }

      if (table.alias_range) {
        addDecoration(table.alias_range, "bruin-sql-token-alias", table.alias || undefined);
      }
    }

    for (const column of parseContext.columns ?? []) {
      for (const part of column.parts ?? []) {
        let resolvedKind = part.kind;
        if (
          part.kind === "table" &&
          column.qualifier &&
          part.name.toLowerCase() === column.qualifier.toLowerCase()
        ) {
          resolvedKind = "alias";
        }

        const className = decorationClassForKind(resolvedKind);
        if (!className) {
          continue;
        }

        addDecoration(part.range, className, part.name);
      }
    }

    decorationIdsRef.current = editor.deltaDecorations(
      decorationIdsRef.current,
      decorations,
    );

    return () => {
      decorationIdsRef.current = editor.deltaDecorations(decorationIdsRef.current, []);
    };
  }, [editor, parseContext]);
}
