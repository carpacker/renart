import { lazy, Suspense, type WheelEvent } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, Loader2 } from "lucide-react";

import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from "recharts";

import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { VirtualDataTable } from "@/components/virtual-data-table";
import { AssetViewMode, LineChartSpec } from "@/lib/asset-visualization";

const ReactMarkdown = lazy(() => import("react-markdown"));

interface AssetNodePreviewProps {
  assetId: string;
  previewMode: AssetViewMode | null;
  chartType: string;
  chart: LineChartSpec | null;
  markdown: string;
  dense?: boolean;
  previewColumns: string[];
  previewRows: Record<string, unknown>[];
  previewError?: string;
  isPreviewLoading: boolean;
  canLoadMorePreviewRows?: boolean;
  onLoadMorePreviewRows?: () => void;
}

export function AssetNodePreview({
  assetId,
  previewMode,
  chartType,
  chart,
  dense = false,
  markdown,
  previewColumns,
  previewRows,
  previewError,
  isPreviewLoading,
  canLoadMorePreviewRows,
  onLoadMorePreviewRows,
}: AssetNodePreviewProps) {
  if (previewError) {
    return (
      <div className="mt-2 px-2 py-2" data-testid="asset-preview-warning-card">
        <Alert>
          <AlertTriangle className="text-amber-500" />
	  <AlertTitle>Preview failed</AlertTitle>
          <AlertDescription>Fetching preview data failed.</AlertDescription>
        </Alert>
      </div>
    );
  }

  if (!previewMode) {
    return null;
  }

  if (isPreviewLoading) {
    if (previewMode === "table" && previewColumns.length > 0) {
      return (
        <div className="mt-2">
          <TablePreview
            assetId={assetId}
            canLoadMore={canLoadMorePreviewRows}
            columns={previewColumns}
            dense={dense}
            loading={isPreviewLoading}
            onLoadMore={onLoadMorePreviewRows}
            rows={previewRows}
          />
        </div>
      );
    }

    return <PreviewLoading />;
  }

  return (
    <div className="mt-2">
      {previewMode === "chart" && chart && (
        <ChartPreview chart={chart} chartType={chartType} />
      )}
      {previewMode === "chart" && !chart && (
        <div className="rounded border bg-background p-2 text-[11px] text-muted-foreground">
          Select chart columns to preview this visualization.
        </div>
      )}
      {previewMode === "table" && (
        <TablePreview
          assetId={assetId}
          canLoadMore={canLoadMorePreviewRows}
          columns={previewColumns}
          dense={dense}
          loading={isPreviewLoading}
          onLoadMore={onLoadMorePreviewRows}
          rows={previewRows}
        />
      )}
      {previewMode === "markdown" && markdown && (
        <MarkdownPreview markdown={markdown} />
      )}
    </div>
  );
}

interface AssetNodeMeasurementProps {
  assetId: string;
  previewMode: AssetViewMode | null;
  previewColumns: string[];
  previewRows: Record<string, unknown>[];
  markdown: string;
  dense?: boolean;
  isPreviewLoading?: boolean;
  canLoadMorePreviewRows?: boolean;
  measurementRef: React.RefObject<HTMLDivElement | null>;
}

export function AssetNodeMeasurement({
  assetId,
  previewMode,
  previewColumns,
  previewRows,
  markdown,
  dense = false,
  isPreviewLoading = false,
  canLoadMorePreviewRows = false,
  measurementRef,
}: AssetNodeMeasurementProps) {
  if (previewMode !== "table" && previewMode !== "markdown") {
    return null;
  }

  if (typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div
      className="pointer-events-none fixed left-0 top-0 -z-10 opacity-0"
      ref={measurementRef}
    >
      {previewMode === "table" ? (
        <TablePreview
          assetId={assetId}
          canLoadMore={canLoadMorePreviewRows}
          columns={previewColumns}
          dense={dense}
          loading={isPreviewLoading}
          rows={previewRows}
        />
      ) : (
        <MarkdownPreview markdown={markdown} />
      )}
    </div>,
    document.body
  );
}

function PreviewLoading() {
  return (
    <div className="mt-2 rounded border bg-background p-2">
      <div className="mb-2 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-3.5 animate-spin" />
        Loading preview…
      </div>
      <div className="space-y-2 animate-pulse">
        <div className="h-3 w-2/3 rounded bg-muted" />
        <div className="h-3 w-full rounded bg-muted" />
        <div className="h-3 w-5/6 rounded bg-muted" />
        <div className="h-16 w-full rounded bg-muted/80" />
      </div>
    </div>
  );
}

function ChartPreview({
  chart,
  chartType,
}: {
  chart: LineChartSpec;
  chartType: string;
}) {
  return (
    <div className="h-54 rounded border bg-background p-2">
      <ChartContainer className="h-full min-h-50 w-full" config={chart.config}>
        {chartType === "bar" ? (
          <BarChart accessibilityLayer data={chart.data}>
            <CartesianGrid vertical={false} />
            <XAxis
              axisLine={false}
              dataKey={chart.xKey}
              tickLine={false}
              tickMargin={8}
            />
            <YAxis axisLine={false} tickLine={false} tickMargin={8} />
            <ChartTooltip
              content={<ChartTooltipContent hideLabel />}
              cursor={false}
            />
            <ChartLegend content={<ChartLegendContent />} />
            {chart.series.map((series) => (
              <Bar
                key={series}
                dataKey={series}
                fill={`var(--color-${series})`}
                radius={6}
              />
            ))}
          </BarChart>
        ) : (
          <LineChart accessibilityLayer data={chart.data}>
            <CartesianGrid vertical={false} />
            <XAxis
              axisLine={false}
              dataKey={chart.xKey}
              tickLine={false}
              tickMargin={8}
            />
            <YAxis axisLine={false} tickLine={false} tickMargin={8} />
            <ChartTooltip content={<ChartTooltipContent />} />
            <ChartLegend content={<ChartLegendContent />} />
            {chart.series.map((series) => (
              <Line
                key={series}
                dataKey={series}
                dot={false}
                stroke={`var(--color-${series})`}
                strokeWidth={2}
                type="monotone"
              />
            ))}
          </LineChart>
        )}
      </ChartContainer>
    </div>
  );
}

function TablePreview({
  assetId,
  canLoadMore,
  columns,
  dense = false,
  loading = false,
  onLoadMore,
  rows,
}: {
  assetId: string;
  canLoadMore?: boolean;
  columns: string[];
  dense?: boolean;
  loading?: boolean;
  onLoadMore?: () => void;
  rows: Record<string, unknown>[];
}) {
  return (
    <div className="max-h-56" onWheelCapture={stopPreviewScrollPropagation}>
      <VirtualDataTable
        columns={columns}
        rows={rows}
        height={224}
        dense={dense}
        loading={loading}
        canLoadMore={canLoadMore}
        onLoadMore={onLoadMore}
        autoLoadMore
        scrollKey={`asset-preview:${assetId}`}
      />
    </div>
  );
}

function MarkdownPreview({ markdown }: { markdown: string }) {
  return (
    <div className="max-h-56 overflow-auto rounded border bg-background p-2">
      <article className="max-w-none text-xs leading-5 text-foreground">
        <Suspense fallback={<div className="text-muted-foreground">Loading preview...</div>}>
          <ReactMarkdown
            components={{
              h1: ({ children }) => (
                <h1 className="mb-2 mt-1 text-lg font-bold">{children}</h1>
              ),
              h2: ({ children }) => (
                <h2 className="mb-2 mt-2 text-base font-semibold">{children}</h2>
              ),
              p: ({ children }) => <p className="mb-1.5">{children}</p>,
              ul: ({ children }) => (
                <ul className="mb-1.5 list-disc pl-4">{children}</ul>
              ),
              li: ({ children }) => <li className="mb-0.5">{children}</li>,
            }}
          >
            {markdown}
          </ReactMarkdown>
        </Suspense>
      </article>
    </div>
  );
}

function stopPreviewScrollPropagation(event: WheelEvent<HTMLDivElement>) {
  const element = event.currentTarget;
  const canScrollVertically = element.scrollHeight > element.clientHeight + 1;
  const canScrollHorizontally = element.scrollWidth > element.clientWidth + 1;

  const wantsVerticalScroll = Math.abs(event.deltaY) >= Math.abs(event.deltaX);
  const wantsHorizontalScroll = !wantsVerticalScroll && event.deltaX !== 0;

  if (
    (wantsVerticalScroll && canScrollVertically) ||
    (wantsHorizontalScroll && canScrollHorizontally)
  ) {
    event.stopPropagation();
  }
}
