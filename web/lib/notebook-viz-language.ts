// Client mirror of the Go @viz directive parser (internal/web/notebook/viz.go).
// The Go side stays the authority that runs at execution time; this module
// powers live editor feedback — diagnostics (squiggles) and completion — so the
// directive is checked as you type instead of only after a run.

export type VizSeverity = "error" | "warning";

/** A diagnostic with a 1-based line and 1-based column span, for Monaco. */
export type VizDiagnostic = {
  message: string;
  severity: VizSeverity;
  line: number;
  startColumn: number;
  endColumn: number;
};

export const VIZ_KINDS = ["table", "bar", "line", "area", "pie", "kpi"] as const;

const VIZ_KIND_SET = new Set<string>(VIZ_KINDS);

export const REQUIRED_VIZ_KEYS: Record<string, string[]> = {
  bar: ["x", "y"],
  line: ["x", "y"],
  area: ["x", "y"],
  pie: ["x", "y"],
  kpi: ["value"],
};

export const KNOWN_VIZ_KEYS: Record<string, string[]> = {
  table: ["limit", "columns"],
  bar: ["x", "y", "stacked", "format", "sort"],
  line: ["x", "y", "stacked", "format", "sort"],
  area: ["x", "y", "stacked", "format", "sort"],
  pie: ["x", "y", "limit"],
  kpi: ["value", "format", "icon", "compare"],
};

// Keys whose value is a column reference (validated against the result).
const COLUMN_KEYS = new Set(["x", "y", "value", "compare"]);

// Column index where `@viz(` begins inside a comment on this line, or -1.
// Recognizes both `-- @viz(...)` line comments and `/* @viz(...) */` block
// comments (the formatter rewrites the former into the latter, often inline
// before the SELECT), mirroring the Go parser.
function indexOfViz(line: string): number {
  const idx = line.indexOf("@viz(");
  if (idx < 0) {
    return -1;
  }
  const prefix = line.slice(0, idx);
  const open = prefix.lastIndexOf("/*");
  if (open >= 0 && !prefix.slice(open).includes("*/")) {
    return idx;
  }
  if (line.trimStart().startsWith("--")) {
    return idx;
  }
  return -1;
}

// Index of the `)` closing the `(` at openIdx, honoring string literals, or
// -1 — so a directive sharing its line with SQL that has its own parens still
// parses (e.g. `/* @viz(bar, x: a) */ select count(*) ...`).
function matchingCloseParen(s: string, openIdx: number): number {
  let depth = 0;
  let inString = "";
  for (let i = openIdx; i < s.length; i++) {
    const c = s[i];
    if (inString) {
      if (c === inString) {
        inString = "";
      }
    } else if (c === "'" || c === '"') {
      inString = c;
    } else if (c === "(") {
      depth++;
    } else if (c === ")") {
      depth--;
      if (depth === 0) {
        return i;
      }
    }
  }
  return -1;
}

/** Split on commas that are not inside [...] or quotes (mirrors Go). */
function splitDirectiveArgs(inner: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let inString = "";
  let current = "";
  for (const c of inner) {
    if (inString) {
      current += c;
      if (c === inString) {
        inString = "";
      }
    } else if (c === "'" || c === '"') {
      inString = c;
      current += c;
    } else if (c === "[") {
      depth++;
      current += c;
    } else if (c === "]") {
      depth--;
      current += c;
    } else if (c === "," && depth === 0) {
      parts.push(current);
      current = "";
    } else {
      current += c;
    }
  }
  if (current.length > 0) {
    parts.push(current);
  }
  return parts;
}

function columnRefs(value: string): string[] {
  const trimmed = value.trim();
  if (trimmed === "") {
    return [];
  }
  if (trimmed.startsWith("[") && trimmed.endsWith("]")) {
    return splitDirectiveArgs(trimmed.slice(1, -1)).map((item) => unquote(item.trim()));
  }
  return [unquote(trimmed)];
}

function unquote(value: string): string {
  if (value.length >= 2) {
    const first = value[0];
    const last = value[value.length - 1];
    if ((first === "'" && last === "'") || (first === '"' && last === '"')) {
      return value.slice(1, -1);
    }
  }
  return value;
}

/**
 * Diagnostics for every @viz directive in a cell body. When `resultColumns`
 * is non-empty, column references (x/y/value/compare) are checked against it.
 */
export function vizDiagnostics(content: string, resultColumns: string[] = []): VizDiagnostic[] {
  const diagnostics: VizDiagnostic[] = [];
  const present = new Set(resultColumns.map((column) => column.toLowerCase()));
  let seenDirective = false;

  content.split("\n").forEach((line, lineIndex) => {
    const col = indexOfViz(line);
    if (col < 0) {
      return;
    }
    const lineNumber = lineIndex + 1;
    const open = line.indexOf("(", col);
    const close = open >= 0 ? matchingCloseParen(line, open) : -1;
    // Squiggle from `@viz` to the directive's closing paren (not the whole
    // line, which may carry SQL after an inline block comment).
    const span = {
      line: lineNumber,
      startColumn: col + 1,
      endColumn: (close >= 0 ? close + 1 : line.length) + 1,
    };

    if (seenDirective) {
      diagnostics.push({
        ...span,
        severity: "warning",
        message: "duplicate @viz directive ignored; the first one wins",
      });
      return;
    }
    seenDirective = true;

    if (open < 0 || close < 0) {
      diagnostics.push({ ...span, severity: "error", message: "malformed @viz directive: expected @viz(kind, ...)" });
      return;
    }

    const inner = line.slice(open + 1, close);
    const parts = splitDirectiveArgs(inner);
    if (parts.length === 0 || parts[0].trim() === "") {
      diagnostics.push({ ...span, severity: "error", message: "@viz requires a kind: table, bar, line, area, pie, or kpi" });
      return;
    }

    const kind = parts[0].trim();
    if (!VIZ_KIND_SET.has(kind)) {
      diagnostics.push({
        ...span,
        severity: "error",
        message: `unknown chart kind "${kind}"; expected table, bar, line, area, pie, or kpi`,
      });
      return;
    }

    const options: Record<string, string> = {};
    for (const rawPair of parts.slice(1)) {
      const pair = rawPair.trim();
      if (pair === "") {
        continue;
      }
      const colon = pair.indexOf(":");
      if (colon < 0) {
        diagnostics.push({ ...span, severity: "error", message: `expected key:value, got "${pair}"` });
        continue;
      }
      const key = pair.slice(0, colon).trim();
      const value = pair.slice(colon + 1).trim();
      const known = KNOWN_VIZ_KEYS[kind];
      if (known && !known.includes(key)) {
        diagnostics.push({ ...span, severity: "warning", message: `unknown key "${key}" for ${kind} charts` });
      }
      options[key] = value;
    }

    for (const required of REQUIRED_VIZ_KEYS[kind] ?? []) {
      if (!(required in options)) {
        diagnostics.push({ ...span, severity: "error", message: `${kind} charts require "${required}"` });
      }
    }

    if (present.size > 0) {
      for (const key of COLUMN_KEYS) {
        if (!(key in options)) {
          continue;
        }
        for (const column of columnRefs(options[key])) {
          if (column && !present.has(column.toLowerCase())) {
            diagnostics.push({ ...span, severity: "warning", message: `column "${column}" not in result` });
          }
        }
      }
    }
  });

  return diagnostics;
}

export type VizCompletionMode =
  | { mode: "kind" }
  | { mode: "key"; kind: string; usedKeys: string[] }
  | { mode: "column"; kind: string; key: string };

/**
 * Decide what to complete at `cursorCol` (0-based) on a directive line, or
 * null when the cursor is not inside a `-- @viz(...)` directive.
 */
export function vizCompletionAt(line: string, cursorCol: number): VizCompletionMode | null {
  const vizIdx = indexOfViz(line);
  if (vizIdx < 0) {
    return null;
  }
  const parenIdx = line.indexOf("(", vizIdx);
  if (parenIdx < 0 || cursorCol <= parenIdx) {
    return null;
  }
  // Do not offer completion once the cursor is past the directive's closing
  // paren (matching paren, so inline SQL parens afterwards are ignored).
  const closeIdx = matchingCloseParen(line, parenIdx);
  if (closeIdx >= 0 && cursorCol > closeIdx) {
    return null;
  }

  const inner = line.slice(parenIdx + 1, cursorCol);
  const segments = splitDirectiveArgs(inner);
  if (segments.length <= 1) {
    return { mode: "kind" };
  }

  const kind = segments[0].trim();
  const currentSegment = segments[segments.length - 1];
  const colon = currentSegment.indexOf(":");
  if (colon >= 0) {
    const key = currentSegment.slice(0, colon).trim();
    if (COLUMN_KEYS.has(key)) {
      return { mode: "column", kind, key };
    }
    return null;
  }

  const usedKeys = segments.slice(1, -1).map((segment) => {
    const idx = segment.indexOf(":");
    return idx >= 0 ? segment.slice(0, idx).trim() : segment.trim();
  });
  return { mode: "key", kind, usedKeys };
}
