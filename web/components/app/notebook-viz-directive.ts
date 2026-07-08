import { VizKind } from "@/lib/api-notebooks";

// Frontend mirror of the Go @viz directive writer. The settings UI edits the
// directive in the cell body so text stays the single source of truth
// (invariant 3). This only needs to write a sensible default directive for a
// chosen chart kind; the Go parser remains the authority on reading it.
//
// The directive is a `/* @viz(...) */` block comment, not a `--` line comment:
// the SQL formatter rewrites line comments into block comments (often inline,
// before the SELECT), so emitting and matching the block form keeps the
// directive stable across format round-trips.

// Matches either comment form so an existing directive — including the inline
// `/* @viz(...) */ select ...` the formatter produces — can be replaced.
const VIZ_DIRECTIVE = /\/\*\s*@viz\(.*?\)\s*\*\/|--[ \t]*@viz\([^\n]*\)/;

function defaultDirective(kind: VizKind, columns: string[]): string {
  const [first, second] = columns;
  switch (kind) {
    case "table":
      return "/* @viz(table) */";
    case "kpi":
      return `/* @viz(kpi, value: ${first ?? "value"}) */`;
    case "pie":
      return `/* @viz(pie, x: ${first ?? "label"}, y: ${second ?? "value"}) */`;
    default:
      return `/* @viz(${kind}, x: ${first ?? "x"}, y: ${second ?? "y"}) */`;
  }
}

/**
 * Rewrites (or inserts) the `@viz(...)` directive in a cell body for the chosen
 * chart kind, preserving the rest of the SQL. Choosing `table` removes the
 * directive (table is the default). Column hints make the new directive
 * immediately valid against the latest result.
 */
export function applyVizKind(body: string, kind: VizKind, columns: string[]): string {
  const match = VIZ_DIRECTIVE.exec(body);

  if (kind === "table") {
    if (!match) {
      return body;
    }
    const without = body.slice(0, match.index) + body.slice(match.index + match[0].length);
    // Tidy where the directive sat: drop a now-blank leading line, or strip the
    // leftover indentation when it was inline before the SELECT.
    return without.replace(/^[ \t]*\n/, "").replace(/^[ \t]+/, "");
  }

  const directive = defaultDirective(kind, columns);
  if (match) {
    return body.slice(0, match.index) + directive + body.slice(match.index + match[0].length);
  }
  return `${directive}\n${body}`;
}

// Put the @viz directive back on its own line. The SQL formatter pulls the
// `/* @viz(...) */` block comment onto the same line as the following SELECT;
// this restores the line break so the directive reads as a standalone header.
// Idempotent — a directive already on its own line is left untouched.
export function normalizeVizDirectiveLine(body: string): string {
  return body.replace(/(\/\*\s*@viz\(.*?\)\s*\*\/)[ \t]+(?=\S)/, "$1\n");
}
