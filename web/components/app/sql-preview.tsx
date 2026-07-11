import { cn } from "@/lib/utils";

type SqlTokenType =
  | "comment"
  | "identifier"
  | "jinja-comment"
  | "jinja-content"
  | "jinja-delimiter"
  | "keyword"
  | "number"
  | "operator"
  | "punctuation"
  | "string"
  | "text"
  | "whitespace";

type SqlToken = {
  type: SqlTokenType;
  value: string;
};

const sqlKeywords = new Set([
  "ADD",
  "ALL",
  "ALTER",
  "AND",
  "AS",
  "ASC",
  "BETWEEN",
  "BY",
  "CASE",
  "CAST",
  "CREATE",
  "CROSS",
  "CURRENT_DATE",
  "CURRENT_TIMESTAMP",
  "DELETE",
  "DESC",
  "DISTINCT",
  "DROP",
  "ELSE",
  "END",
  "EXCEPT",
  "FALSE",
  "FROM",
  "FULL",
  "GROUP",
  "HAVING",
  "IF",
  "IN",
  "INNER",
  "INSERT",
  "INTERSECT",
  "INTO",
  "IS",
  "JOIN",
  "LEFT",
  "LIKE",
  "LIMIT",
  "NOT",
  "NULL",
  "ON",
  "OR",
  "ORDER",
  "OUTER",
  "OVER",
  "PARTITION",
  "QUALIFY",
  "RIGHT",
  "SELECT",
  "SET",
  "THEN",
  "TRUE",
  "UNION",
  "UPDATE",
  "USING",
  "WHEN",
  "WHERE",
  "WINDOW",
  "WITH",
]);

const tokenClassName: Record<SqlTokenType, string | undefined> = {
  comment: "text-muted-foreground italic",
  identifier: "text-foreground",
  "jinja-comment": "text-muted-foreground italic",
  "jinja-content": "text-orange-700 dark:text-orange-300",
  "jinja-delimiter": "font-semibold text-amber-700 dark:text-amber-300",
  keyword: "font-semibold text-sky-700 dark:text-sky-300",
  number: "text-violet-700 dark:text-violet-300",
  operator: "text-muted-foreground",
  punctuation: "text-muted-foreground",
  string: "text-emerald-700 dark:text-emerald-300",
  text: undefined,
  whitespace: undefined,
};

export function SqlPreview({ query, className }: { query: string; className?: string }) {
  const tokens = tokenizeSqlPreview(query);

  return (
    <pre
      className={cn(
        "max-h-28 overflow-auto whitespace-pre-wrap border-t bg-background px-2 py-1.5 font-mono text-[11px] leading-relaxed text-foreground",
        className,
      )}
    >
      {tokens.map((token, index) => {
        const classNameForToken = tokenClassName[token.type];
        if (!classNameForToken) {
          return token.value;
        }
        return (
          <span key={`${index}-${token.type}`} className={classNameForToken}>
            {token.value}
          </span>
        );
      })}
    </pre>
  );
}

function tokenizeSqlPreview(input: string): SqlToken[] {
  const tokens: SqlToken[] = [];
  let index = 0;

  while (index < input.length) {
    const jinjaMatch = readJinjaToken(input, index);
    if (jinjaMatch) {
      tokens.push(...jinjaMatch.tokens);
      index = jinjaMatch.nextIndex;
      continue;
    }

    const char = input[index];
    const next = input[index + 1];

    if (isWhitespace(char)) {
      const start = index;
      while (index < input.length && isWhitespace(input[index])) {
        index += 1;
      }
      tokens.push({ type: "whitespace", value: input.slice(start, index) });
      continue;
    }

    if (char === "-" && next === "-") {
      const start = index;
      index += 2;
      while (index < input.length && input[index] !== "\n") {
        index += 1;
      }
      tokens.push({ type: "comment", value: input.slice(start, index) });
      continue;
    }

    if (char === "/" && next === "*") {
      const start = index;
      index += 2;
      while (index < input.length && !(input[index] === "*" && input[index + 1] === "/")) {
        index += 1;
      }
      index = Math.min(input.length, index + 2);
      tokens.push({ type: "comment", value: input.slice(start, index) });
      continue;
    }

    if (char === "'") {
      const start = index;
      index += 1;
      while (index < input.length) {
        if (input[index] === "'" && input[index + 1] === "'") {
          index += 2;
          continue;
        }
        if (input[index] === "'") {
          index += 1;
          break;
        }
        index += 1;
      }
      tokens.push({ type: "string", value: input.slice(start, index) });
      continue;
    }

    if (isNumberStart(input, index)) {
      const start = index;
      index = readNumber(input, index);
      tokens.push({ type: "number", value: input.slice(start, index) });
      continue;
    }

    if (isIdentifierStart(char)) {
      const start = index;
      index += 1;
      while (index < input.length && isIdentifierPart(input[index])) {
        index += 1;
      }
      const value = input.slice(start, index);
      tokens.push({ type: sqlKeywords.has(value.toUpperCase()) ? "keyword" : "identifier", value });
      continue;
    }

    if ("(),.;[]{}".includes(char)) {
      tokens.push({ type: "punctuation", value: char });
      index += 1;
      continue;
    }

    if ("+-*/%=<>!|&:".includes(char)) {
      const start = index;
      index += 1;
      while (index < input.length && "+-*/%=<>!|&:".includes(input[index])) {
        index += 1;
      }
      tokens.push({ type: "operator", value: input.slice(start, index) });
      continue;
    }

    tokens.push({ type: "text", value: char });
    index += 1;
  }

  return tokens;
}

function readJinjaToken(
  input: string,
  index: number,
): { tokens: SqlToken[]; nextIndex: number } | null {
  const open = input.slice(index, index + 2);
  const close = open === "{{" ? "}}" : open === "{%" ? "%}" : open === "{#" ? "#}" : null;
  if (!close) {
    return null;
  }

  const contentStart = index + 2;
  const closeIndex = input.indexOf(close, contentStart);
  const contentEnd = closeIndex === -1 ? input.length : closeIndex;
  const nextIndex = closeIndex === -1 ? input.length : closeIndex + 2;
  const tokens: SqlToken[] = [
    { type: "jinja-delimiter", value: open },
    {
      type: open === "{#" ? "jinja-comment" : "jinja-content",
      value: input.slice(contentStart, contentEnd),
    },
  ];

  if (closeIndex !== -1) {
    tokens.push({ type: "jinja-delimiter", value: close });
  }

  return { tokens, nextIndex };
}

function isWhitespace(char: string) {
  return /\s/.test(char);
}

function isIdentifierStart(char: string) {
  return /[A-Za-z_]/.test(char);
}

function isIdentifierPart(char: string) {
  return /[A-Za-z0-9_$]/.test(char);
}

function isNumberStart(input: string, index: number) {
  const char = input[index];
  const next = input[index + 1];
  return /[0-9]/.test(char) || (char === "." && /[0-9]/.test(next));
}

function readNumber(input: string, index: number) {
  let current = index;

  if (input[current] === ".") {
    current += 1;
  }

  while (current < input.length && /[0-9]/.test(input[current])) {
    current += 1;
  }

  if (input[current] === ".") {
    current += 1;
    while (current < input.length && /[0-9]/.test(input[current])) {
      current += 1;
    }
  }

  if (/[eE]/.test(input[current] ?? "")) {
    const exponentStart = current;
    current += 1;
    if (/[+-]/.test(input[current] ?? "")) {
      current += 1;
    }
    const digitStart = current;
    while (current < input.length && /[0-9]/.test(input[current])) {
      current += 1;
    }
    if (digitStart === current) {
      return exponentStart;
    }
  }

  return current;
}
