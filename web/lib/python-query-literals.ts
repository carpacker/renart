export type PythonQueryLiteral = {
  sql: string;
  connection?: string;
  callStart: number;
  sourceStart: number;
  sourceEnd: number;
  sqlToSourceOffsets: number[];
};

/**
 * Extract direct query("...") and renart.query("...") calls whose first
 * argument is one ordinary (optionally raw/triple-quoted) Python string.
 * An unterminated literal is included as well: that is the normal state while
 * somebody is typing a new query, and its source map is stable through EOF.
 * Dynamic expressions, f-strings, bytes, comments, and query-looking text
 * inside other strings are deliberately ignored.
 *
 * sqlToSourceOffsets maps every UTF-16 boundary in the decoded SQL value back
 * to a boundary in the Python model. That keeps diagnostics and navigation
 * precise even when the literal contains a basic Python escape such as \n.
 */
export function findPythonQueryLiterals(source: string): PythonQueryLiteral[] {
  const result: PythonQueryLiteral[] = [];

  for (let index = 0; index < source.length;) {
    const current = source[index];
    if (current === "#") {
      index = skipPythonComment(source, index);
      continue;
    }
    if (current === '"' || current === "'") {
      index = scanPythonStringEnd(source, index, false);
      continue;
    }
    if (!isIdentifierStart(current)) {
      index += 1;
      continue;
    }

    const identifierStart = index;
    index += 1;
    while (index < source.length && isIdentifierContinue(source[index])) {
      index += 1;
    }
    if (source.slice(identifierStart, index) !== "query") {
      continue;
    }
    if (!isSupportedQueryCallee(source, identifierStart)) {
      continue;
    }

    const parsed = parseQueryLiteral(source, identifierStart, index);
    if (!parsed) {
      continue;
    }
    result.push(parsed.literal);
    index = parsed.end;
  }

  return result;
}

export function pythonQueryLiteralAtOffset(source: string, sourceOffset: number) {
  return (
    findPythonQueryLiterals(source).find(
      (literal) => sourceOffset >= literal.sourceStart && sourceOffset <= literal.sourceEnd,
    ) ?? null
  );
}

export function sqlOffsetForSourceOffset(literal: PythonQueryLiteral, sourceOffset: number) {
  if (sourceOffset <= literal.sourceStart) {
    return 0;
  }
  if (sourceOffset >= literal.sourceEnd) {
    return literal.sql.length;
  }

  let low = 0;
  let high = literal.sqlToSourceOffsets.length - 1;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (literal.sqlToSourceOffsets[middle] <= sourceOffset) {
      low = middle;
    } else {
      high = middle - 1;
    }
  }
  return low;
}

export function sourceOffsetForSQLOffset(literal: PythonQueryLiteral, sqlOffset: number) {
  const bounded = Math.min(Math.max(sqlOffset, 0), literal.sqlToSourceOffsets.length - 1);
  return literal.sqlToSourceOffsets[bounded] ?? literal.sourceEnd;
}

function parseQueryLiteral(source: string, callStart: number, afterName: number) {
  let index = skipWhitespace(source, afterName);
  if (source[index] !== "(") {
    return null;
  }
  index = skipWhitespace(source, index + 1);

  const prefixStart = index;
  while (index < source.length && /[rRuUbBfF]/.test(source[index])) {
    index += 1;
  }
  const prefix = source.slice(prefixStart, index).toLowerCase();
  if (prefix.includes("f") || prefix.includes("b") || !["", "r", "u"].includes(prefix)) {
    return null;
  }
  if (source[index] !== '"' && source[index] !== "'") {
    return null;
  }

  const parsed = readPythonString(source, index, prefix.includes("r"));
  if (!parsed) {
    return null;
  }
  const afterLiteral = skipWhitespace(source, parsed.end);
  if (parsed.terminated && source[afterLiteral] !== ")" && source[afterLiteral] !== ",") {
    // The SQL must be the complete first argument. Reject concatenation,
    // method calls, and other expressions even when they start with a string,
    // because their runtime value has no stable source map.
    return null;
  }
  const connection = parsed.terminated
    ? readStaticQueryConnection(source, afterLiteral)
    : undefined;
  return {
    literal: {
      sql: parsed.value,
      ...(connection ? { connection } : {}),
      callStart,
      sourceStart: parsed.bodyStart,
      sourceEnd: parsed.bodyEnd,
      sqlToSourceOffsets: parsed.sqlToSourceOffsets,
    },
    end: parsed.end,
  };
}

// The runtime accepts the connection as either the second positional argument
// or a keyword argument. This deliberately recognizes only a static ordinary
// string: variables and expressions remain runtime-only, while incomplete SQL
// in the first argument continues to be projected as before.
function readStaticQueryConnection(source: string, afterLiteral: number): string | undefined {
  if (source[afterLiteral] !== ",") {
    return undefined;
  }

  let index = skipPythonTrivia(source, afterLiteral + 1);
  const positional = readStaticPythonStringAt(source, index);
  if (positional) {
    return positional.value.trim() || undefined;
  }

  let depth = 0;
  while (index < source.length) {
    const current = source[index];
    if (current === "#") {
      index = skipPythonComment(source, index);
      continue;
    }
    if (current === '"' || current === "'") {
      index = scanPythonStringEnd(source, index, false);
      continue;
    }
    if (current === "(" || current === "[" || current === "{") {
      depth += 1;
      index += 1;
      continue;
    }
    if (current === ")") {
      if (depth === 0) return undefined;
      depth -= 1;
      index += 1;
      continue;
    }
    if (current === "]" || current === "}") {
      depth = Math.max(0, depth - 1);
      index += 1;
      continue;
    }
    if (depth === 0 && isIdentifierStart(current)) {
      const nameStart = index;
      index += 1;
      while (index < source.length && isIdentifierContinue(source[index])) {
        index += 1;
      }
      if (source.slice(nameStart, index) !== "connection") {
        continue;
      }
      index = skipPythonTrivia(source, index);
      if (source[index] !== "=") {
        continue;
      }
      const keyword = readStaticPythonStringAt(source, skipPythonTrivia(source, index + 1));
      return keyword?.value.trim() || undefined;
    }
    index += 1;
  }
  return undefined;
}

function readStaticPythonStringAt(source: string, start: number) {
  let index = start;
  while (index < source.length && /[rRuUbBfF]/.test(source[index])) {
    index += 1;
  }
  const prefix = source.slice(start, index).toLowerCase();
  if (prefix.includes("f") || prefix.includes("b") || !["", "r", "u"].includes(prefix)) {
    return null;
  }
  if (source[index] !== '"' && source[index] !== "'") {
    return null;
  }
  const parsed = readPythonString(source, index, prefix.includes("r"));
  return parsed?.terminated ? parsed : null;
}

function readPythonString(source: string, quoteStart: number, raw: boolean) {
  const quote = source[quoteStart];
  const triple = source.slice(quoteStart, quoteStart + 3) === quote.repeat(3);
  const delimiterLength = triple ? 3 : 1;
  const bodyStart = quoteStart + delimiterLength;
  const sqlToSourceOffsets = [bodyStart];
  let value = "";

  for (let index = bodyStart; index < source.length;) {
    if (
      source[index] === quote &&
      (!triple || source.slice(index, index + delimiterLength) === quote.repeat(delimiterLength))
    ) {
      return {
        value,
        bodyStart,
        bodyEnd: index,
        end: index + delimiterLength,
        sqlToSourceOffsets,
        terminated: true,
      };
    }

    if (source[index] !== "\\") {
      value += source[index];
      index += 1;
      sqlToSourceOffsets.push(index);
      continue;
    }

    // A backslash protects the following quote lexically even in a raw
    // literal. Raw values retain both code units; ordinary strings decode the
    // common Python escapes and map the decoded boundary back to their source.
    if (index + 1 >= source.length) {
      // Preserve a dangling slash in an in-progress literal. Python cannot
      // execute it yet, but dropping the whole projection while the user is
      // between keystrokes also drops SQL completion and highlighting.
      value += source[index];
      index += 1;
      sqlToSourceOffsets.push(index);
      continue;
    }
    if (raw) {
      value += source.slice(index, index + 2);
      sqlToSourceOffsets.push(index + 1, index + 2);
      index += 2;
      continue;
    }

    const decoded = decodePythonEscape(source, index);
    if (decoded.value.length === 0) {
      sqlToSourceOffsets[sqlToSourceOffsets.length - 1] = decoded.end;
    } else {
      value += decoded.value;
      for (let outputIndex = 0; outputIndex < decoded.value.length; outputIndex += 1) {
        sqlToSourceOffsets.push(decoded.end);
      }
    }
    index = decoded.end;
  }

  return {
    value,
    bodyStart,
    bodyEnd: source.length,
    end: source.length,
    sqlToSourceOffsets,
    terminated: false,
  };
}

function decodePythonEscape(source: string, start: number): { value: string; end: number } {
  const escaped = source[start + 1];
  const common: Record<string, string> = {
    "\\": "\\",
    "'": "'",
    '"': '"',
    a: "\u0007",
    b: "\b",
    f: "\f",
    n: "\n",
    r: "\r",
    t: "\t",
    v: "\v",
  };
  if (escaped in common) {
    return { value: common[escaped], end: start + 2 };
  }
  if (escaped === "\n") {
    return { value: "", end: start + 2 };
  }
  if (escaped === "\r" && source[start + 2] === "\n") {
    return { value: "", end: start + 3 };
  }

  const hexLength = escaped === "x" ? 2 : escaped === "u" ? 4 : escaped === "U" ? 8 : 0;
  if (hexLength > 0) {
    const digits = source.slice(start + 2, start + 2 + hexLength);
    if (digits.length === hexLength && /^[0-9a-f]+$/i.test(digits)) {
      const codePoint = Number.parseInt(digits, 16);
      if (codePoint <= 0x10ffff) {
        return { value: String.fromCodePoint(codePoint), end: start + 2 + hexLength };
      }
    }
  }

  if (/[0-7]/.test(escaped)) {
    const digits = source.slice(start + 1).match(/^[0-7]{1,3}/)?.[0] ?? escaped;
    return {
      value: String.fromCodePoint(Number.parseInt(digits, 8)),
      end: start + 1 + digits.length,
    };
  }

  // Python currently preserves unknown escapes in string values (while
  // warning about them), so preserve the two source characters as SQL too.
  return { value: source.slice(start, start + 2), end: start + 2 };
}

function scanPythonStringEnd(source: string, quoteStart: number, raw: boolean) {
  const parsed = readPythonString(source, quoteStart, raw);
  return parsed?.end ?? source.length;
}

function isSupportedQueryCallee(source: string, queryStart: number) {
  let beforeQuery = queryStart - 1;
  while (beforeQuery >= 0 && /\s/.test(source[beforeQuery])) {
    beforeQuery -= 1;
  }
  if (beforeQuery < 0 || source[beforeQuery] !== ".") {
    return true;
  }
  let qualifierEnd = beforeQuery - 1;
  while (qualifierEnd >= 0 && /\s/.test(source[qualifierEnd])) {
    qualifierEnd -= 1;
  }
  let qualifierStart = qualifierEnd + 1;
  while (qualifierStart > 0 && isIdentifierContinue(source[qualifierStart - 1])) {
    qualifierStart -= 1;
  }
  return source.slice(qualifierStart, qualifierEnd + 1) === "renart";
}

function skipWhitespace(source: string, start: number) {
  let index = start;
  while (index < source.length && /\s/.test(source[index])) {
    index += 1;
  }
  return index;
}

function skipPythonTrivia(source: string, start: number) {
  let index = start;
  while (index < source.length) {
    index = skipWhitespace(source, index);
    if (source[index] !== "#") {
      return index;
    }
    index = skipPythonComment(source, index);
  }
  return index;
}

function skipPythonComment(source: string, start: number) {
  const newline = source.indexOf("\n", start);
  return newline < 0 ? source.length : newline + 1;
}

function isIdentifierStart(value: string | undefined) {
  return !!value && /[A-Za-z_]/.test(value);
}

function isIdentifierContinue(value: string | undefined) {
  return !!value && /[A-Za-z0-9_]/.test(value);
}
