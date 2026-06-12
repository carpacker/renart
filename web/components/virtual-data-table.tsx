"use client";

import { Check, Copy, Loader2 } from "lucide-react";
import { UIEvent, WheelEventHandler, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { ScrollArea } from "@/components/ui/scroll-area";

const tableScrollPositions = new Map<string, { top: number; left: number }>();

type Props = {
  columns: string[];
  rows: Record<string, unknown>[];
  height?: number | string;
  dense?: boolean;
  loading?: boolean;
  canLoadMore?: boolean;
  onLoadMore?: () => void;
  emptyLabel?: string;
  autoLoadMore?: boolean;
  scrollKey?: string;
  onWheelCapture?: WheelEventHandler<HTMLDivElement>;
  frameless?: boolean;
};

export function VirtualDataTable({
  columns,
  rows,
  height = 260,
  dense = false,
  loading = false,
  canLoadMore = false,
  onLoadMore,
  emptyLabel = "No rows returned.",
  autoLoadMore = false,
  scrollKey,
  onWheelCapture,
  frameless = false,
}: Props) {
  const fillAvailableHeight = typeof height === "string";
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const loadMoreRequestedRef = useRef(false);
  const scrollSnapshotRef = useRef({ top: 0, left: 0, height: 0 });
  const [copied, setCopied] = useState(false);
  const [nearBottom, setNearBottom] = useState(true);

  const fallbackColumns = useMemo(() => {
    if (columns.length > 0) {
      return columns;
    }
    const firstRow = rows[0];
    return firstRow ? Object.keys(firstRow) : [];
  }, [columns, rows]);

  const triggerLoadMore = () => {
    if (!canLoadMore || !onLoadMore || loading || loadMoreRequestedRef.current) {
      return;
    }

    loadMoreRequestedRef.current = true;
    onLoadMore();
  };

  const updateNearBottom = (element: HTMLDivElement) => {
    const remaining = element.scrollHeight - element.scrollTop - element.clientHeight;
    setNearBottom(remaining < 96);
    return remaining;
  };

  const copyTable = async () => {
    const tsv = serializeRowsAsTsv(fallbackColumns, rows);
    const html = serializeRowsAsHtmlTable(fallbackColumns, rows);

    if (await writeTableToClipboard({ html, text: tsv })) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    }
  };

  const handleScroll = (event: UIEvent<HTMLDivElement>) => {
    scrollSnapshotRef.current = {
      top: event.currentTarget.scrollTop,
      left: event.currentTarget.scrollLeft,
      height: event.currentTarget.scrollHeight,
    };

    if (scrollKey) {
      tableScrollPositions.set(scrollKey, {
        top: event.currentTarget.scrollTop,
        left: event.currentTarget.scrollLeft,
      });
    }

    if (!autoLoadMore || !canLoadMore || !onLoadMore) {
      updateNearBottom(event.currentTarget);
      return;
    }

    const remaining = updateNearBottom(event.currentTarget);
    if (remaining < 64) {
      triggerLoadMore();
    }
  };

  if (!loading) {
    loadMoreRequestedRef.current = false;
  }

  useLayoutEffect(() => {
    const element = scrollContainerRef.current;
    if (!element) {
      return;
    }

    if (!loadMoreRequestedRef.current && !scrollKey) {
      return;
    }

    const savedPosition = scrollKey ? tableScrollPositions.get(scrollKey) : null;

    if (savedPosition) {
      element.scrollTop = savedPosition.top;
      element.scrollLeft = savedPosition.left;
      return;
    }

    if (loadMoreRequestedRef.current) {
      element.scrollTop = scrollSnapshotRef.current.top;
      element.scrollLeft = scrollSnapshotRef.current.left;
    }
  }, [rows.length, scrollKey]);

  useEffect(() => {
    const element = scrollContainerRef.current;
    if (element) {
      updateNearBottom(element);
    }
  }, [rows.length, loading]);

  const showLoadMoreControl = Boolean(onLoadMore && (canLoadMore || loading) && (nearBottom || loading));

  return (
    <div
      className={`group/table relative overflow-hidden bg-background ${
        frameless ? "" : "rounded border"
      } ${fillAvailableHeight ? "flex h-full min-h-0 flex-col" : ""}`}
    >
      {rows.length > 0 && fallbackColumns.length > 0 ? (
        <Button
          aria-label="Copy table"
          className="absolute right-2 top-2 z-30 h-7 gap-1.5 bg-background/90 px-2 text-[11px] opacity-0 shadow-sm backdrop-blur transition-opacity group-hover/table:opacity-100 focus-visible:opacity-100"
          onClick={copyTable}
          size="sm"
          type="button"
          variant="outline"
        >
          {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
          <span>{copied ? "Copied" : "Copy"}</span>
        </Button>
      ) : null}

      {loading ? (
        <div className="pointer-events-none absolute right-2 top-10 z-20 rounded bg-background/90 p-1 text-muted-foreground shadow-sm">
          <Loader2 className="size-3.5 animate-spin" />
        </div>
      ) : null}

      {showLoadMoreControl ? (
        <Button
          className="absolute bottom-3 left-1/2 z-30 h-8 -translate-x-1/2 gap-2 bg-background/95 px-3 text-[11px] shadow-md backdrop-blur disabled:opacity-70"
          disabled={!canLoadMore || loading}
          onClick={triggerLoadMore}
          size="sm"
          type="button"
          variant="outline"
        >
          {loading ? <Loader2 className="size-3 animate-spin" /> : null}
          {loading ? "Loading more rows..." : "Load more rows"}
        </Button>
      ) : null}

      <ScrollArea
        className={fillAvailableHeight ? "min-h-0 flex-1" : "h-fit max-h-56"}
        horizontalScrollBarClassName="left-12 w-[calc(100%-3rem)]"
        viewportClassName={fillAvailableHeight ? undefined : "max-h-56"}
        viewportRef={scrollContainerRef}
        onViewportScroll={handleScroll}
        onWheelCapture={onWheelCapture}
      >
        <table className="min-w-full border-collapse text-xs">
          <thead className="sticky top-0 z-10 bg-muted/70 backdrop-blur">
            <tr>
              <th
                className={`sticky left-0 z-30 w-12 min-w-12 border-b border-r bg-muted/95 text-right font-medium text-muted-foreground backdrop-blur ${
                  dense ? "px-2 py-1" : "px-2 py-1.5"
                }`}
              >
                #
              </th>
              {fallbackColumns.map((column) => (
                <th
                  className={`sticky top-0 z-20 w-56 min-w-32 max-w-56 border-b border-r bg-muted/90 text-left font-medium whitespace-nowrap backdrop-blur last:border-r-0 ${
                    dense ? "px-2 py-1" : "px-2 py-1.5"
                  }`}
                  key={column}
                >
                  <div className="w-56 max-w-56 truncate">{column}</div>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length > 0 ? (
              rows.map((row, rowIndex) => (
                <tr className="odd:bg-muted/20" key={rowIndex}>
                  <td
                    className={`sticky left-0 z-10 w-12 min-w-12 border-b border-r bg-muted/75 text-right font-mono text-[11px] text-muted-foreground backdrop-blur ${
                      dense ? "px-2 py-0.5" : "px-2 py-1"
                    }`}
                  >
                    {rowIndex + 1}
                  </td>
                  {fallbackColumns.map((column) => {
                    const cell = formatCell(row[column]);

                    return (
                      <td
                        key={`${rowIndex}-${column}`}
                        className={`w-56 min-w-32 max-w-56 border-b border-r align-top last:border-r-0 ${
                          dense ? "px-2 py-0.5" : "px-2 py-1"
                        } ${cell.className}`}
                      >
                        <TableCellContent cell={cell} />
                      </td>
                    );
                  })}
                </tr>
              ))
            ) : (
              <tr>
                <td
                  className="p-3 text-xs text-muted-foreground"
                  colSpan={Math.max(1, fallbackColumns.length + 1)}
                >
                  {emptyLabel}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </ScrollArea>
    </div>
  );
}

type FormattedCell = {
  value: string;
  className: string;
  detailValue: string;
  detailKind: "json" | "text";
};

function TableCellContent({ cell }: { cell: FormattedCell }) {
  return (
    <HoverCard closeDelay={80} openDelay={250}>
      <HoverCardTrigger asChild>
        <button
          className="block w-full max-w-48 truncate rounded-sm text-left hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          type="button"
        >
          {cell.value}
        </button>
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-max min-w-32 max-w-[min(36rem,calc(100vw-2rem))] p-0">
        <ScrollArea className="max-h-[min(24rem,calc(100vh-4rem))]" viewportClassName="max-h-[min(24rem,calc(100vh-4rem))] p-3">
          {cell.detailKind === "json" ? (
            <JsonPreview value={cell.detailValue} />
          ) : (
            <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed">
              {cell.detailValue}
            </pre>
          )}
        </ScrollArea>
      </HoverCardContent>
    </HoverCard>
  );
}

async function writeTableToClipboard({ html, text }: { html: string; text: string }) {
  try {
    if (typeof ClipboardItem !== "undefined" && navigator.clipboard?.write) {
      await navigator.clipboard.write([
        new ClipboardItem({
          "text/html": new Blob([html], { type: "text/html" }),
          "text/plain": new Blob([text], { type: "text/plain" }),
        }),
      ]);
      return true;
    }
  } catch {
    // Fall back to plain text below.
  }

  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall back to the legacy textarea copy path below.
  }

  return copyTextWithTextarea(text);
}

function copyTextWithTextarea(text: string) {
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.left = "-9999px";
  textarea.style.top = "0";
  document.body.appendChild(textarea);
  textarea.select();
  try {
    return document.execCommand("copy");
  } finally {
    document.body.removeChild(textarea);
  }
}

function JsonPreview({ value }: { value: string }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed">
      {tokenizeJson(value).map((token, index) => (
        <span className={token.className} key={index}>
          {token.value}
        </span>
      ))}
    </pre>
  );
}

function formatCell(value: unknown): FormattedCell {
  const base = formatCellValue(value);
  const detail = formatCellDetail(value, base.value);

  return {
    ...base,
    detailValue: detail.value,
    detailKind: detail.kind,
  };
}

function formatCellValue(value: unknown): { value: string; className: string } {
  if (value === null || value === undefined) {
    return { value: "null", className: "text-muted-foreground italic" };
  }

  if (typeof value === "number") {
    return { value: String(value), className: "text-primary font-medium" };
  }

  if (typeof value === "boolean") {
    return {
      value: value ? "true" : "false",
      className: value ? "text-primary" : "text-destructive",
    };
  }

  if (typeof value === "string" && looksLikeDate(value)) {
    return { value, className: "text-sky-700 dark:text-sky-300" };
  }

  if (value instanceof Date) {
    return {
      value: value.toISOString(),
      className: "text-sky-700 dark:text-sky-300",
    };
  }

  if (typeof value === "object") {
    return {
      value: JSON.stringify(value),
      className: "text-muted-foreground",
    };
  }

  return { value: String(value), className: "text-foreground" };
}

function formatCellDetail(value: unknown, fallback: string): { value: string; kind: "json" | "text" } {
  if (value !== null && typeof value === "object") {
    return { value: JSON.stringify(value, null, 2), kind: "json" };
  }

  if (typeof value === "string") {
    const trimmed = value.trim();
    if ((trimmed.startsWith("{") && trimmed.endsWith("}")) || (trimmed.startsWith("[") && trimmed.endsWith("]"))) {
      try {
        return { value: JSON.stringify(JSON.parse(trimmed), null, 2), kind: "json" };
      } catch {
        return { value, kind: "text" };
      }
    }
    return { value, kind: "text" };
  }

  return { value: fallback, kind: "text" };
}

function serializeRowsAsTsv(columns: string[], rows: Record<string, unknown>[]) {
  const header = columns.map(escapeTsvValue).join("\t");
  const body = rows.map((row) => columns.map((column) => escapeTsvValue(formatCellDetail(row[column], formatCellValue(row[column]).value).value)).join("\t"));
  return [header, ...body].join("\n");
}

function serializeRowsAsHtmlTable(columns: string[], rows: Record<string, unknown>[]) {
  const header = `<tr>${columns.map((column) => `<th>${escapeHtml(column)}</th>`).join("")}</tr>`;
  const body = rows
    .map((row) => `<tr>${columns.map((column) => `<td>${escapeHtml(formatCellDetail(row[column], formatCellValue(row[column]).value).value)}</td>`).join("")}</tr>`)
    .join("");
  return `<table><thead>${header}</thead><tbody>${body}</tbody></table>`;
}

function escapeTsvValue(value: string) {
  return value.replace(/\r?\n/g, " ").replace(/\t/g, " ");
}

function escapeHtml(value: string) {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function tokenizeJson(value: string) {
  const tokens: Array<{ value: string; className?: string }> = [];
  const pattern = /("(?:\\.|[^"\\])*"\s*:)|("(?:\\.|[^"\\])*")|\b(true|false)\b|\b(null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;
  let lastIndex = 0;
  for (const match of value.matchAll(pattern)) {
    if (match.index > lastIndex) {
      tokens.push({ value: value.slice(lastIndex, match.index) });
    }
    const token = match[0];
    tokens.push({
      value: token,
      className: match[1]
        ? "text-sky-700 dark:text-sky-300"
        : match[2]
          ? "text-emerald-700 dark:text-emerald-300"
          : match[3]
            ? "text-violet-700 dark:text-violet-300"
            : match[4]
              ? "text-muted-foreground italic"
              : "text-primary",
    });
    lastIndex = match.index + token.length;
  }
  if (lastIndex < value.length) {
    tokens.push({ value: value.slice(lastIndex) });
  }
  return tokens;
}

function looksLikeDate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}([ tT]\d{2}:\d{2}(:\d{2})?)?/.test(value)) {
    return false;
  }

  return !Number.isNaN(Date.parse(value));
}
