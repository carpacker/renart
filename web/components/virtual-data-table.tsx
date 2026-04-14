"use client";

import { Loader2 } from "lucide-react";
import { UIEvent, useLayoutEffect, useMemo, useRef } from "react";

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
}: Props) {
  const fillAvailableHeight = typeof height === "string";
  const scrollContainerRef = useRef<HTMLDivElement | null>(null);
  const loadMoreRequestedRef = useRef(false);
  const scrollSnapshotRef = useRef({ top: 0, left: 0, height: 0 });

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
      return;
    }

    const element = event.currentTarget;
    const remaining = element.scrollHeight - element.scrollTop - element.clientHeight;
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

  return (
    <div
      className={`relative overflow-hidden rounded border bg-background ${
        fillAvailableHeight ? "flex h-full min-h-0 flex-col" : ""
      }`}
    >
      {loading ? (
        <div className="pointer-events-none absolute right-2 top-1.5 z-20 rounded bg-background/90 p-1 text-muted-foreground shadow-sm">
          <Loader2 className="size-3.5 animate-spin" />
        </div>
      ) : null}

      <div
        ref={scrollContainerRef}
        className={fillAvailableHeight ? "min-h-0 flex-1 overflow-auto" : "overflow-auto max-h-56 h-fit"}
        onScroll={handleScroll}
      >
        <table className="min-w-full border-collapse text-xs">
          <thead className="sticky top-0 z-10 bg-muted/70 backdrop-blur">
            <tr>
              {fallbackColumns.map((column) => (
                <th
                  className={`min-w-32 border-b border-r text-left font-medium whitespace-nowrap last:border-r-0 ${
                    dense ? "px-2 py-1" : "px-2 py-1.5"
                  }`}
                  key={column}
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length > 0 ? (
              rows.map((row, rowIndex) => (
                <tr className="odd:bg-muted/20" key={rowIndex}>
                  {fallbackColumns.map((column) => {
                    const cell = formatCell(row[column]);

                    return (
                      <td
                        key={`${rowIndex}-${column}`}
                        className={`min-w-32 border-b border-r align-top last:border-r-0 ${
                          dense ? "px-2 py-0.5" : "px-2 py-1"
                        } ${cell.className}`}
                        title={cell.value}
                      >
                        <div className="whitespace-nowrap">{cell.value}</div>
                      </td>
                    );
                  })}
                </tr>
              ))
            ) : (
              <tr>
                <td
                  className="p-3 text-xs text-muted-foreground"
                  colSpan={Math.max(1, fallbackColumns.length)}
                >
                  {emptyLabel}
                </td>
              </tr>
            )}

            {(canLoadMore || loading) && onLoadMore ? (
              <tr>
                <td
                  className="sticky bottom-0 bg-background/95 p-2 backdrop-blur"
                  colSpan={Math.max(1, fallbackColumns.length)}
                >
                  <button
                    className="flex w-full items-center justify-center gap-2 rounded border border-dashed px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground disabled:cursor-default disabled:hover:bg-transparent"
                    disabled={!canLoadMore || loading}
                    onClick={triggerLoadMore}
                    type="button"
                  >
                    {loading ? <Loader2 className="size-3 animate-spin" /> : null}
                    <span>
                      {loading
                        ? "Loading more rows..."
                        : canLoadMore
                          ? "Load more rows"
                          : "All available rows loaded"}
                    </span>
                  </button>
                </td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function formatCell(value: unknown): { value: string; className: string } {
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

function looksLikeDate(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}([ tT]\d{2}:\d{2}(:\d{2})?)?/.test(value)) {
    return false;
  }

  return !Number.isNaN(Date.parse(value));
}
