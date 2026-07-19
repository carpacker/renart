export const CLIPBOARD_SEED_FORMATS = ["auto", "csv", "tsv", "json", "jsonl", "text"] as const;

export type ClipboardSeedFormat = (typeof CLIPBOARD_SEED_FORMATS)[number];
export type DetectedClipboardSeedFormat = Exclude<ClipboardSeedFormat, "auto">;

export type PreparedClipboardSeed = {
  detectedFormat: DetectedClipboardSeedFormat;
  inputFormat: DetectedClipboardSeedFormat;
  fileName: string;
  fileType: "csv" | "json" | "jsonl";
  mimeType: string;
  content: string;
};

export function detectClipboardSeedFormat(value: string): DetectedClipboardSeedFormat {
  const normalized = normalizeNewlines(value).trim();
  if (!normalized) return "text";

  try {
    const parsed: unknown = JSON.parse(normalized);
    if (parsed !== null && typeof parsed === "object") return "json";
  } catch {
    // Continue with line- and delimiter-based detection.
  }

  const lines = normalized.split("\n").filter((line) => line.trim());
  if (
    lines.length > 1 &&
    lines.every((line) => {
      try {
        const parsed: unknown = JSON.parse(line);
        return parsed !== null && typeof parsed === "object";
      } catch {
        return false;
      }
    })
  ) {
    return "jsonl";
  }

  if (hasConsistentDelimitedRows(normalized, "\t")) return "tsv";
  if (hasConsistentDelimitedRows(normalized, ",")) return "csv";
  return "text";
}

export function prepareClipboardSeed(
  value: string,
  format: ClipboardSeedFormat,
  fileStem = "pasted-seed",
): PreparedClipboardSeed {
  const normalized = normalizeNewlines(value);
  if (!normalized.trim()) throw new Error("The clipboard does not contain any seed data.");

  const detectedFormat = detectClipboardSeedFormat(normalized);
  const inputFormat = format === "auto" ? detectedFormat : format;
  const safeFileStem = clipboardSeedFileStem(fileStem);
  switch (inputFormat) {
    case "json": {
      try {
        JSON.parse(normalized);
      } catch {
        throw new Error("The pasted content is not valid JSON.");
      }
      return {
        detectedFormat,
        inputFormat,
        fileName: `${safeFileStem}.json`,
        fileType: "json",
        mimeType: "application/json",
        content: ensureTrailingNewline(normalized.trim()),
      };
    }
    case "jsonl": {
      const lines = normalized.split("\n").filter((line) => line.trim());
      if (
        lines.length === 0 ||
        !lines.every((line) => {
          try {
            JSON.parse(line);
            return true;
          } catch {
            return false;
          }
        })
      ) {
        throw new Error("Each non-empty line must contain valid JSON.");
      }
      return {
        detectedFormat,
        inputFormat,
        fileName: `${safeFileStem}.jsonl`,
        fileType: "jsonl",
        mimeType: "application/x-ndjson",
        content: ensureTrailingNewline(lines.join("\n")),
      };
    }
    case "tsv": {
      const rows = parseDelimitedRows(normalized, "\t");
      if (!rows || rows.every((row) => row.length < 2)) {
        throw new Error("The pasted content does not contain a valid tab-separated table.");
      }
      return {
        detectedFormat,
        inputFormat,
        fileName: `${safeFileStem}.csv`,
        fileType: "csv",
        mimeType: "text/csv",
        content: ensureTrailingNewline(rows.map(csvRow).join("\n")),
      };
    }
    case "csv": {
      const rows = parseDelimitedRows(normalized, ",");
      if (!rows) throw new Error("The pasted content is not valid CSV.");
      return {
        detectedFormat,
        inputFormat,
        fileName: `${safeFileStem}.csv`,
        fileType: "csv",
        mimeType: "text/csv",
        content: ensureTrailingNewline(normalized.trimEnd()),
      };
    }
    case "text": {
      const lines = normalized.split("\n");
      while (lines.length > 0 && lines.at(-1) === "") lines.pop();
      return {
        detectedFormat,
        inputFormat,
        fileName: `${safeFileStem}.csv`,
        fileType: "csv",
        mimeType: "text/csv",
        content: ensureTrailingNewline(["value", ...lines.map((line) => csvCell(line))].join("\n")),
      };
    }
  }
}

export function clipboardSeedFileStem(value: string) {
  const lastPathPart = value.trim().split(/[\\/]/).at(-1) ?? "";
  const withoutExtension = lastPathPart.replace(/\.[^.]+$/, "");
  const normalized = withoutExtension
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^[.-]+|[.-]+$/g, "");
  return normalized || "pasted-seed";
}

export function clipboardSeedFormatLabel(format: DetectedClipboardSeedFormat) {
  return (
    {
      csv: "CSV",
      tsv: "TSV",
      json: "JSON",
      jsonl: "JSON Lines",
      text: "plain text",
    } as const
  )[format];
}

function normalizeNewlines(value: string) {
  return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
}

function ensureTrailingNewline(value: string) {
  return value.endsWith("\n") ? value : `${value}\n`;
}

function hasConsistentDelimitedRows(value: string, delimiter: string) {
  const rows = parseDelimitedRows(value, delimiter)?.filter((row) =>
    row.some((cell) => cell.trim()),
  );
  if (!rows || rows.length === 0 || rows[0].length < 2) return false;
  return rows.every((row) => row.length === rows[0].length);
}

function parseDelimitedRows(value: string, delimiter: string): string[][] | null {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let quoted = false;

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (character === '"') {
      if (quoted && value[index + 1] === '"') {
        cell += '"';
        index += 1;
      } else {
        quoted = !quoted;
      }
    } else if (character === delimiter && !quoted) {
      row.push(cell);
      cell = "";
    } else if (character === "\n" && !quoted) {
      row.push(cell);
      rows.push(row);
      row = [];
      cell = "";
    } else {
      cell += character;
    }
  }
  if (quoted) return null;
  row.push(cell);
  if (row.length > 1 || row[0] || rows.length === 0) rows.push(row);
  return rows;
}

function csvRow(row: string[]) {
  return row.map(csvCell).join(",");
}

function csvCell(value: string) {
  return /[",\n]/.test(value) ? `"${value.replaceAll('"', '""')}"` : value;
}
