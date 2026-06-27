"use client";

import { useMemo } from "react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart,
  Pie,
  PieChart,
  XAxis,
  YAxis,
} from "recharts";

import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { NotebookCellRunResult, VizDirective } from "@/lib/api-notebooks";

const CHART_ROW_CAP = 200;
const PIE_COLORS = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)", "var(--chart-5)"];

function asArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.map(String);
  }
  if (typeof value === "string" && value) {
    return [value];
  }
  return [];
}

function rowsToObjects(result: NotebookCellRunResult): Record<string, unknown>[] {
  return result.rows.map((row) => {
    const object: Record<string, unknown> = {};
    result.columns.forEach((column, index) => {
      object[column] = row[index];
    });
    return object;
  });
}

function MissingColumnNotice({ columns }: { columns: string[] }) {
  return (
    <div className="flex h-full min-h-32 items-center justify-center rounded-lg border border-dashed bg-muted/30 px-4 text-center text-xs text-muted-foreground">
      {columns.length === 1
        ? `Column '${columns[0]}' is not in the result.`
        : `Columns ${columns.map((c) => `'${c}'`).join(", ")} are not in the result.`}
    </div>
  );
}

function missingColumns(needed: string[], present: string[]): string[] {
  const lower = new Set(present.map((c) => c.toLowerCase()));
  return needed.filter((column) => !lower.has(column.toLowerCase()));
}

function formatNumber(value: unknown, format?: string): string {
  const numeric = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(numeric)) {
    return String(value ?? "");
  }
  if (format === "currency") {
    return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(numeric);
  }
  if (format === "percent") {
    return new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 1 }).format(numeric);
  }
  return new Intl.NumberFormat().format(numeric);
}

/**
 * Renders a cell's result according to its @viz directive. The directive is
 * the single source of truth (it lives in the cell text); this component is
 * a pure view over the typed config plus the run result, with graceful
 * row-cap and missing-column states.
 */
export function NotebookVizRenderer({ result }: { result: NotebookCellRunResult }) {
  const viz = result.viz ?? null;
  const data = useMemo(() => rowsToObjects(result), [result]);

  if (!viz || viz.kind === "table") {
    return null; // table is the default; the cell card renders it directly.
  }

  if (viz.kind === "kpi") {
    return <KpiCard viz={viz} result={result} />;
  }

  const xKey = String(viz.options.x ?? "");
  const yKeys = asArray(viz.options.y);
  const needed = [xKey, ...yKeys].filter(Boolean);
  const missing = missingColumns(needed, result.columns);
  if (missing.length > 0) {
    return <MissingColumnNotice columns={missing} />;
  }

  const capped = data.slice(0, CHART_ROW_CAP);
  const config: ChartConfig = {};
  yKeys.forEach((key, index) => {
    config[key] = { label: key, color: PIE_COLORS[index % PIE_COLORS.length] };
  });

  const truncated = data.length > capped.length;
  const stacked = viz.options.stacked === true;

  return (
    <div>
      <ChartContainer config={config} className="h-56 w-full">
        {viz.kind === "bar" ? (
          <BarChart data={capped} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis dataKey={xKey} tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tickLine={false} axisLine={false} tickMargin={8} />
            <ChartTooltip content={(props) => <ChartTooltipContent {...props} />} />
            <ChartLegend content={<ChartLegendContent />} />
            {yKeys.map((key) => (
              <Bar key={key} dataKey={key} fill={`var(--color-${key})`} stackId={stacked ? "stack" : undefined} radius={stacked ? 0 : 4} />
            ))}
          </BarChart>
        ) : viz.kind === "area" ? (
          <AreaChart data={capped} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis dataKey={xKey} tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tickLine={false} axisLine={false} tickMargin={8} />
            <ChartTooltip content={(props) => <ChartTooltipContent {...props} />} />
            <ChartLegend content={<ChartLegendContent />} />
            {yKeys.map((key) => (
              <Area key={key} dataKey={key} type="monotone" fill={`var(--color-${key})`} stroke={`var(--color-${key})`} stackId={stacked ? "stack" : undefined} fillOpacity={0.2} />
            ))}
          </AreaChart>
        ) : viz.kind === "pie" ? (
          <PieChart>
            <ChartTooltip content={(props) => <ChartTooltipContent {...props} />} />
            <Pie data={capped} dataKey={yKeys[0]} nameKey={xKey} outerRadius={88}>
              {capped.map((_, index) => (
                <Cell key={index} fill={PIE_COLORS[index % PIE_COLORS.length]} />
              ))}
            </Pie>
          </PieChart>
        ) : (
          <LineChart data={capped} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis dataKey={xKey} tickLine={false} axisLine={false} tickMargin={8} />
            <YAxis tickLine={false} axisLine={false} tickMargin={8} />
            <ChartTooltip content={(props) => <ChartTooltipContent {...props} />} />
            <ChartLegend content={<ChartLegendContent />} />
            {yKeys.map((key) => (
              <Line key={key} dataKey={key} type="monotone" stroke={`var(--color-${key})`} strokeWidth={2} dot={false} />
            ))}
          </LineChart>
        )}
      </ChartContainer>
      {truncated ? (
        <div className="mt-1 text-[11px] text-muted-foreground">showing {capped.length} of {result.total_rows} rows</div>
      ) : null}
    </div>
  );
}

function KpiCard({ viz, result }: { viz: VizDirective; result: NotebookCellRunResult }) {
  const valueKey = String(viz.options.value ?? "");
  const compareKey = viz.options.compare ? String(viz.options.compare) : "";
  const missing = missingColumns([valueKey, compareKey].filter(Boolean), result.columns);
  if (missing.length > 0) {
    return <MissingColumnNotice columns={missing} />;
  }

  const valueIndex = result.columns.findIndex((c) => c.toLowerCase() === valueKey.toLowerCase());
  const compareIndex = compareKey ? result.columns.findIndex((c) => c.toLowerCase() === compareKey.toLowerCase()) : -1;
  const firstRow = result.rows[0] ?? [];
  const value = valueIndex >= 0 ? firstRow[valueIndex] : undefined;
  const compare = compareIndex >= 0 ? firstRow[compareIndex] : undefined;
  const format = typeof viz.options.format === "string" ? viz.options.format : undefined;

  const delta = typeof value === "number" && typeof compare === "number" && compare !== 0
    ? (value - compare) / Math.abs(compare)
    : null;

  return (
    <div className="rounded-lg border p-4">
      <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{valueKey}</div>
      <div className="mt-1 text-3xl font-semibold tracking-tight">{formatNumber(value, format)}</div>
      {delta !== null ? (
        <div className={`mt-1 text-xs ${delta >= 0 ? "text-emerald-600" : "text-red-500"}`}>
          {delta >= 0 ? "▲" : "▼"} {formatNumber(Math.abs(delta), "percent")} vs {compareKey}
        </div>
      ) : null}
    </div>
  );
}
