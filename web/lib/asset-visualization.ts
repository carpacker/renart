import { ChartConfig } from "@/components/ui/chart";

export type AssetViewMode = "table" | "chart" | "markdown";

export type LineChartSpec = {
  xKey: string;
  series: string[];
  data: Record<string, unknown>[];
  config: ChartConfig;
};

export type ChartSelection = {
  xKey: string;
  series: string[];
};

export function getAssetViewMode(
  meta?: Record<string, string>
): AssetViewMode | null {
  const value = (meta?.web_view ?? "").trim().toLowerCase();
  if (value === "chart" || value === "markdown" || value === "table") {
    return value;
  }
  return null;
}

export function buildMarkdown(
  meta: Record<string, string> | undefined,
  rows: Record<string, unknown>[]
): string {
  const template = (meta?.web_markdown_template ?? "").trim();
  if (template.length > 0) {
    return interpolateMarkdownTemplate(template, rows);
  }

  return getMarkdownValue(rows, meta?.web_markdown_column);
}

export function getTablePreviewLimit(
  meta?: Record<string, string>,
  fallback = 25
): number {
  const raw = (meta?.web_table_limit ?? "").trim();
  if (!raw) {
    return fallback;
  }

  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return fallback;
  }

  return parsed;
}

export function getTableDenseMode(meta?: Record<string, string>): boolean {
  return (meta?.web_table_dense ?? "").trim().toLowerCase() === "true";
}

export function buildLineChartSpec(
  rows: Record<string, unknown>[],
  meta?: Record<string, string>,
  columns?: string[]
): LineChartSpec | null {
  const availableKeys = collectAvailableKeys(rows, columns);
  if (availableKeys.length < 2) {
    return null;
  }

  const normalizedRows = normalizeChartRows(rows, availableKeys);
  if (normalizedRows.length === 0) {
    return null;
  }

  const selection = resolveChartSelection(meta, normalizedRows, availableKeys);
  if (!selection) {
    return null;
  }

  const { xKey, series } = selection;
  const config = series.reduce<ChartConfig>((acc, key, index) => {
    acc[key] = {
      label: key,
      color: `var(--chart-${(index % 5) + 1})`,
    };
    return acc;
  }, {});

  return {
    xKey,
    series,
    data: normalizedRows,
    config,
  };
}

export function resolveChartSelection(
  meta: Record<string, string> | undefined,
  rows: Record<string, unknown>[],
  columns?: string[]
): ChartSelection | null {
  const availableKeys = collectAvailableKeys(rows, columns);
  if (availableKeys.length < 2) {
    return null;
  }

  const configuredXKey = resolveKeyCaseInsensitive(
    meta?.web_chart_x,
    availableKeys
  );
  const configuredSeries = parseConfiguredSeries(
    meta,
    availableKeys,
    configuredXKey
  );

  const inferredXKey = inferReasonableXKey(availableKeys);
  const xKey = configuredXKey ?? inferredXKey;
  if (!xKey) {
    return null;
  }

  const inferredSeries = inferReasonableSeriesKeys(rows, availableKeys, xKey);
  const series =
    configuredSeries.length > 0 ? configuredSeries : inferredSeries;
  if (series.length === 0) {
    return null;
  }

  return {
    xKey,
    series,
  };
}

function collectAvailableKeys(
  rows: Record<string, unknown>[],
  columns?: string[]
): string[] {
  const orderedKeys: string[] = [];
  const seen = new Set<string>();

  for (const column of columns ?? []) {
    const key = column.trim();
    if (!key || seen.has(key)) {
      continue;
    }

    seen.add(key);
    orderedKeys.push(key);
  }

  for (const row of rows) {
    for (const key of Object.keys(row)) {
      if (seen.has(key)) {
        continue;
      }

      seen.add(key);
      orderedKeys.push(key);
    }
  }

  return orderedKeys;
}

function normalizeChartRows(
  rows: Record<string, unknown>[],
  keys: string[]
): Record<string, unknown>[] {
  return rows.map((row) => {
    const normalized: Record<string, unknown> = {};
    for (const key of keys) {
      normalized[key] = row[key];
    }
    return normalized;
  });
}

function interpolateMarkdownTemplate(
  template: string,
  rows: Record<string, unknown>[]
): string {
  const firstRow = rows[0] ?? {};

  return template.replace(/\{\{\s*([^}]+)\s*\}\}/g, (_match, rawExpression) => {
    const expression = String(rawExpression).trim();

    if (expression === "rows.length") {
      return String(rows.length);
    }

    const rowIndexed = expression.match(/^row\[(\d+)\]\.(.+)$/);
    if (rowIndexed) {
      const rowIndex = Number(rowIndexed[1]);
      const key = rowIndexed[2];
      return stringifyTemplateValue(rows[rowIndex]?.[key]);
    }

    const rowPrefixed = expression.match(/^row(\d+)\.(.+)$/);
    if (rowPrefixed) {
      const rowIndex = Number(rowPrefixed[1]);
      const key = rowPrefixed[2];
      return stringifyTemplateValue(rows[rowIndex]?.[key]);
    }

    if (expression.startsWith("first.")) {
      const key = expression.slice("first.".length);
      return stringifyTemplateValue(firstRow[key]);
    }

    return stringifyTemplateValue(firstRow[expression]);
  });
}

function stringifyTemplateValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }

  if (typeof value === "string") {
    let normalized = value
      .replace(/\\r\\n/g, "\n")
      .replace(/\\n/g, "\n")
      .replace(/\\t/g, "\t");

    const trimmed = normalized.trim();
    if (
      trimmed.includes("\n- ") &&
      !trimmed.startsWith("- ") &&
      !trimmed.startsWith("* ")
    ) {
      normalized = `- ${trimmed}`;
    }

    return normalized;
  }

  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }

  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function getMarkdownValue(
  rows: Record<string, unknown>[],
  markdownColumn?: string
): string {
  if (rows.length === 0) {
    return "";
  }

  const preferred = (markdownColumn ?? "").trim();
  const firstRow = rows[0];

  if (preferred && typeof firstRow[preferred] === "string") {
    return firstRow[preferred] as string;
  }

  for (const key of ["markdown", "md", "content", "text"]) {
    const value = firstRow[key];
    if (typeof value === "string") {
      return value;
    }
  }

  for (const value of Object.values(firstRow)) {
    if (typeof value === "string") {
      return value;
    }
  }

  return "";
}

function parseConfiguredSeries(
  meta: Record<string, string> | undefined,
  keys: string[],
  xKey: string | null
): string[] {
  return (meta?.web_chart_series ?? "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean)
    .map((key) => resolveKeyCaseInsensitive(key, keys))
    .filter((key): key is string => Boolean(key))
    .filter((key, index, array) => array.indexOf(key) === index)
    .filter((key) => key !== xKey);
}

function inferReasonableXKey(keys: string[]): string | null {
  const prioritizedPatterns = [
    /(^|_)(date|ds)($|_)/,
    /(^|_)(timestamp|time|datetime)($|_)/,
    /(^|_)(day|week|month|quarter|year|hour|minute)($|_)/,
    /(^|_)(created_at|updated_at|event_time|event_date)($|_)/,
  ];

  for (const pattern of prioritizedPatterns) {
    const match = keys.find((key) => pattern.test(key.toLowerCase()));
    if (match) {
      return match;
    }
  }

  return null;
}

function inferReasonableSeriesKeys(
  rows: Record<string, unknown>[],
  keys: string[],
  xKey: string
): string[] {
  return keys
    .filter((key) => key !== xKey)
    .filter((key) => isLikelyNumericSeries(rows, key))
    .slice(0, 3);
}

function isLikelyNumericSeries(
  rows: Record<string, unknown>[],
  key: string
): boolean {
  let sawNumericValue = false;

  for (const row of rows.slice(0, 50)) {
    const value = row[key];
    if (value === null || value === undefined || value === "") {
      continue;
    }

    if (typeof value === "number") {
      if (!Number.isFinite(value)) {
        return false;
      }
      sawNumericValue = true;
      continue;
    }

    if (typeof value === "string") {
      const trimmed = value.trim();
      if (!trimmed) {
        continue;
      }

      const parsed = Number(trimmed);
      if (!Number.isFinite(parsed)) {
        return false;
      }

      sawNumericValue = true;
      continue;
    }

    return false;
  }

  return sawNumericValue;
}

function resolveKeyCaseInsensitive(
  requestedKey: string | undefined,
  keys: string[]
): string | null {
  const normalizedRequestedKey = (requestedKey ?? "").trim();
  if (!normalizedRequestedKey) {
    return null;
  }

  const directMatch = keys.find((key) => key === normalizedRequestedKey);
  if (directMatch) {
    return directMatch;
  }

  const loweredRequestedKey = normalizedRequestedKey.toLowerCase();
  return keys.find((key) => key.toLowerCase() === loweredRequestedKey) ?? null;
}
