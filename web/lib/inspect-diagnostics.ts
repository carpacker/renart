import type * as MonacoNS from "monaco-editor";

import { AssetInspectResponse } from "@/lib/types";

export type InspectDiagnosticSnapshot = {
  assetId: string;
  content: string;
  inspect: AssetInspectResponse;
};

type ParsedInspectLocation = {
  message: string;
  lineNumber: number;
  startColumn: number;
  endColumn: number;
};

const MULTILINE_LOCATION_PATTERN = /LINE\s+(\d+):\s*(.*)\n([\s^]*)\^/m;
const INLINE_LOCATION_PATTERN = /LINE\s+(\d+):\s*(.+?)\s+\^/;

export function buildInspectDiagnosticMarker(
  model: MonacoNS.editor.ITextModel,
  snapshot: InspectDiagnosticSnapshot | null,
): MonacoNS.editor.IMarkerData[] {
  if (!snapshot || snapshot.inspect.status !== "error") {
    return [];
  }

  const parsed = parseInspectLocation(snapshot.inspect.error || snapshot.inspect.raw_output);
  if (!parsed) {
    return [];
  }

  if (model.getValue() !== snapshot.content) {
    return [];
  }

  const actualLineText = model.getLineContent(parsed.lineNumber);
  if (!actualLineText) {
    return [];
  }

  const maxColumn = actualLineText.length + 1;
  const startColumn = Math.min(Math.max(1, parsed.startColumn), maxColumn);
  const endColumn = Math.min(
    Math.max(startColumn + 1, parsed.endColumn),
    maxColumn,
  );

  return [{
    severity: 8,
    message: parsed.message,
    startLineNumber: parsed.lineNumber,
    startColumn,
    endLineNumber: parsed.lineNumber,
    endColumn,
  }];
}

function parseInspectLocation(raw: string | undefined): ParsedInspectLocation | null {
  const text = (raw ?? "").trim();
  if (!text) {
    return null;
  }

  const multilineMatch = text.match(MULTILINE_LOCATION_PATTERN);
  if (multilineMatch) {
    const lineNumber = Number(multilineMatch[1]);
    const lineText = multilineMatch[2] ?? "";
    const caretPrefix = multilineMatch[3] ?? "";
    const trimmedLineText = lineText.replace(/\s+$/g, "");
    const startColumn = Math.max(1, caretPrefix.length + 1);
    const highlightedToken = trimmedLineText.slice(startColumn - 1).match(/^\S+/)?.[0] ?? "";
    const endColumn = Math.max(startColumn + Math.max(1, highlightedToken.length), startColumn + 1);

    return {
      message: text,
      lineNumber,
      startColumn,
      endColumn,
    };
  }

  const inlineMatch = text.match(INLINE_LOCATION_PATTERN);
  if (!inlineMatch) {
    return null;
  }

  const lineNumber = Number(inlineMatch[1]);
  const lineText = (inlineMatch[2] ?? "").replace(/\s+$/g, "");
  const endColumn = Math.max(2, lineText.length + 1);

  return {
    message: text,
    lineNumber,
    startColumn: 1,
    endColumn,
  };
}
