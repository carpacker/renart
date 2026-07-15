import {
  ArrowLeftRight,
  Activity,
  BookOpen,
  Calendar,
  CheckCircle2,
  ClipboardCheck,
  Cloud,
  Cpu,
  FileCode,
  Hammer,
  LayoutDashboard,
  LayoutGrid,
  Layers,
  Network,
  Play,
  Radar,
  Sprout,
  Table2,
  TrendingUp,
} from "lucide-react";

import type { AssetStaleness } from "@/lib/api-staleness";

export const integrations: Record<string, string> = {
  DuckDB: "#f59e0b",
  Stripe: "#635bff",
  ingestr: "#0ea5e9",
  dbt: "#ff694b",
  Python: "#3b82f6",
  BigQuery: "#4285f4",
  Sklearn: "#f59e0b",
  Load: "#14b8a6",
  Test: "#16a34a",
};

export const navItems = [
  { to: "/", label: "Build", icon: Hammer },
  { to: "/catalog", label: "Catalog", icon: Network },
  { to: "/notebooks", label: "Notebooks", icon: BookOpen },
  { to: "/runs", label: "Runs", icon: Play },
  { to: "/schedules", label: "Schedules", icon: Calendar },
] as const;

export const objectGroups = [
  {
    id: "pipeline",
    label: "Pipelines",
    icon: Layers,
    items: ["simple", "cloud_product_sync"],
  },
  {
    id: "notebook",
    label: "Notebooks",
    icon: BookOpen,
    items: ["revenue_exploration", "churn_analysis"],
  },
  {
    id: "dashboard",
    label: "Dashboards",
    icon: LayoutDashboard,
    items: ["revenue_overview", "ops_health"],
  },
];

export const kindMeta = {
  sql: { label: "SQL asset", icon: FileCode, ext: ".sql", description: "Transform with a SELECT" },
  python: { label: "Python asset", icon: Cpu, ext: ".py", description: "Custom Python transform" },
  load: {
    label: "Load source / sink",
    icon: ArrowLeftRight,
    ext: ".asset.yml",
    description: "Replicate data between connections",
  },
  seed: {
    label: "Seed asset",
    icon: Sprout,
    ext: ".asset.yml",
    description: "Load a version-controlled file into a table",
  },
  sensor: {
    label: "Sensor",
    icon: Radar,
    ext: ".asset.yml",
    description: "Wait for an external readiness condition",
  },
  source: {
    label: "Source table",
    icon: Table2,
    ext: ".source.yml",
    description: "Existing table with columns, types, and checks",
  },
  ingestr: {
    label: "External source",
    icon: Cloud,
    ext: ".asset.yml",
    description: "Stripe, GA4, S3, and other ingestr sources",
  },
  unittest: {
    label: "Unit test",
    icon: ClipboardCheck,
    ext: ".test.yml",
    description: "Mock inputs and expected output",
  },
  md: { label: "Markdown cell", icon: BookOpen, ext: ".md", description: "Narrative notes" },
  kpi: { label: "KPI tile", icon: TrendingUp, ext: "", description: "Single metric" },
  line: { label: "Line chart", icon: Activity, ext: "", description: "Time-series chart" },
  bar: { label: "Bar chart", icon: LayoutGrid, ext: "", description: "Categorical chart" },
  table: { label: "Table tile", icon: Table2, ext: "", description: "Tabular result" },
} as const;

export type AssetKind = keyof typeof kindMeta;

export function kindForAssetType(type: string): AssetKind {
  const normalized = type.toLowerCase();
  if (normalized.includes(".sensor.")) return "sensor";
  if (normalized.endsWith(".seed")) return "seed";
  if (normalized.includes("python")) return "python";
  if (normalized.includes("load")) return "load";
  if (normalized.includes("ingestr")) return "ingestr";
  if (normalized.includes("source")) return "source";
  if (normalized.includes("test")) return "unittest";
  return "sql";
}

export type AppAsset = {
  id: string;
  name: string;
  kind: AssetKind;
  group: string;
  integration: string;
  description: string;
  dir?: string;
  imports?: string[];
  status: "ok" | "overdue" | "unknown" | "pending" | "success" | "failed";
  materializedAt: string;
  staleness?: AssetStaleness;
  // Set when the asset file failed to parse; the node renders an error state and
  // the editor shows the message so the user can fix it in place.
  parseError?: string;
  x: number;
  y: number;
};

export const assets: AppAsset[] = [
  {
    id: "stripe_orders",
    name: "stripe_orders",
    dir: "raw",
    kind: "ingestr",
    group: "SOURCES",
    integration: "Stripe",
    description: "Raw orders synced from Stripe",
    status: "overdue",
    materializedAt: "Jul 11, 3:57 PM",
    x: 40,
    y: 96,
  },
  {
    id: "ga4_events",
    name: "ga4_events",
    dir: "raw",
    kind: "ingestr",
    group: "SOURCES",
    integration: "ingestr",
    description: "Web events from GA4 export",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 40,
    y: 212,
  },
  {
    id: "locations",
    name: "locations",
    dir: "raw",
    kind: "source",
    group: "SOURCES",
    integration: "DuckDB",
    description: "Unmanaged location table",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 40,
    y: 328,
  },
  {
    id: "events_to_bq",
    name: "events_to_bq",
    dir: "raw",
    kind: "load",
    group: "SOURCES",
    integration: "Load",
    description: "Replicate GA4 to BigQuery",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 40,
    y: 444,
  },
  {
    id: "orders_cleaned",
    name: "orders_cleaned",
    dir: "staging",
    kind: "sql",
    group: "STAGING",
    integration: "dbt",
    description: "dbt model for orders_cleaned",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 330,
    y: 96,
  },
  {
    id: "events_cleaned",
    name: "events_cleaned",
    dir: "staging",
    kind: "sql",
    group: "STAGING",
    integration: "dbt",
    description: "dbt model for events_cleaned",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 330,
    y: 212,
  },
  {
    id: "locations_cleaned",
    name: "locations_cleaned",
    dir: "staging",
    kind: "sql",
    group: "STAGING",
    integration: "dbt",
    description: "dbt model for locations_cleaned",
    status: "ok",
    materializedAt: "May 10, 11:48 AM",
    x: 330,
    y: 328,
  },
  {
    id: "revenue_daily",
    name: "revenue_daily",
    dir: "marts",
    kind: "sql",
    group: "ANALYTICS",
    integration: "DuckDB",
    description: "Daily revenue rollup",
    status: "ok",
    materializedAt: "May 10, 11:49 AM",
    x: 620,
    y: 212,
  },
  {
    id: "predicted_orders",
    name: "predicted_orders",
    dir: "ml",
    kind: "python",
    group: "ML",
    integration: "Python",
    description: "Demand forecast output",
    imports: ["pandas", "numpy", "requests"],
    status: "ok",
    materializedAt: "May 10, 11:49 AM",
    x: 910,
    y: 150,
  },
  {
    id: "model_revenue",
    name: "model_revenue",
    dir: "ml",
    kind: "python",
    group: "ML",
    integration: "Sklearn",
    description: "Revenue model by month",
    imports: ["sklearn", "pandas", "scipy"],
    status: "ok",
    materializedAt: "May 10, 11:49 AM",
    x: 910,
    y: 274,
  },
];

export const pipelineDependencies = ["scikit-learn", "pandas", "pyarrow", "dlt"] as const;

const importToPackage: Record<string, string> = {
  sklearn: "scikit-learn",
  cv2: "opencv-python",
  PIL: "pillow",
};

export function packageForImport(importName: string) {
  return importToPackage[importName] ?? importName;
}

export function parsePythonImport(line: string) {
  const match =
    line.match(/^\s*import\s+([A-Za-z0-9_]+)/) ?? line.match(/^\s*from\s+([A-Za-z0-9_]+)\s+import/);
  return match?.[1] ?? null;
}

export function missingPythonDependencies(
  asset: Pick<AppAsset, "imports">,
  declared: readonly string[] = pipelineDependencies,
) {
  return (asset.imports ?? [])
    .map(packageForImport)
    .filter((pkg, index, packages) => !declared.includes(pkg) && packages.indexOf(pkg) === index);
}

export const pipelineVariables = [
  ["client", "string", "client_a"],
  ["region", "string", "us"],
  ["schedule", "string", "@daily"],
] as const;

export const pipelineVariants = [
  { id: "default", overrides: {} },
  { id: "client_alpha", overrides: { client: "alpha", region: "us", schedule: "@hourly" } },
  { id: "client_beta", overrides: { client: "beta", region: "eu", schedule: "0 6 * * *" } },
  { id: "client_gamma", overrides: { client: "gamma", region: "ap", schedule: "@weekly" } },
] as const;

export type PipelineVariantId = (typeof pipelineVariants)[number]["id"];

export function pipelineVariantValue(variantId: string, variableName: string) {
  const variant = pipelineVariants.find((item) => item.id === variantId);
  const base = pipelineVariables.find(([name]) => name === variableName)?.[2] ?? "";
  return variant?.overrides[variableName as keyof typeof variant.overrides] ?? base;
}

export function renderedPipelineName(variantId: string) {
  return `${pipelineVariantValue(variantId, "client")}_pipe`;
}

export function renderedPipelineSchedule(variantId: string) {
  return pipelineVariantValue(variantId, "schedule");
}

export const impactPlan = {
  changes: [
    {
      name: "marts.revenue_daily",
      type: "breaking",
      note: "revenue retyped DECIMAL(18,2) to DOUBLE; invalidates existing data",
    },
    { name: "staging.orders_cleaned", type: "nonbreaking", note: "added column customer_id" },
    { name: "raw.events_to_bq", type: "added", note: "new load asset" },
    { name: "ml.predicted_orders", type: "indirect", note: "selects from revenue_daily" },
    { name: "ml.model_revenue", type: "indirect", note: "selects from revenue_daily" },
    { name: "raw.locations", type: "metadata", note: "description updated; no recompute" },
  ],
  backfill: [
    { name: "marts.revenue_daily", rows: "~120k", reason: "breaking change" },
    { name: "ml.predicted_orders", rows: "~4.2k", reason: "downstream" },
    { name: "ml.model_revenue", rows: "~360", reason: "downstream" },
  ],
} as const;

export const changeTypeMeta: Record<string, { label: string; className: string }> = {
  added: { label: "Added", className: "bg-emerald-100 text-emerald-700" },
  breaking: { label: "Breaking", className: "bg-red-100 text-red-700" },
  nonbreaking: { label: "Non-breaking", className: "bg-amber-100 text-amber-700" },
  indirect: { label: "Indirect", className: "bg-orange-100 text-orange-700" },
  metadata: { label: "Metadata", className: "bg-zinc-200 text-zinc-600" },
};

export const edges = [
  ["stripe_orders", "orders_cleaned"],
  ["ga4_events", "events_cleaned"],
  ["locations", "locations_cleaned"],
  ["orders_cleaned", "revenue_daily"],
  ["events_cleaned", "revenue_daily"],
  ["locations_cleaned", "revenue_daily"],
  ["revenue_daily", "predicted_orders"],
  ["revenue_daily", "model_revenue"],
] as const;

export const tests = [
  {
    id: "t1",
    name: "revenue_sums_amounts",
    status: "pass",
    given: "orders_cleaned (3 rows)",
    expect: "revenue_daily (1 row)",
  },
  {
    id: "t2",
    name: "nulls_count_as_zero",
    status: "fail",
    given: "amount = null",
    expect: "revenue = 0",
    got: "revenue = null",
  },
  {
    id: "t3",
    name: "groups_by_day",
    status: "pass",
    given: "orders_cleaned (2 days)",
    expect: "revenue_daily (2 rows)",
  },
] as const;

export const runs = [
  {
    id: "3875a208",
    status: "success",
    pipeline: "cloud_product_sync",
    assets: 13,
    launched: "Jul 16, 1:15:19 PM",
    duration: "0:01:57",
  },
  {
    id: "9fd00436",
    status: "success",
    pipeline: "simple",
    assets: 2,
    launched: "Jul 16, 9:02:11 AM",
    duration: "0:00:00.1",
  },
  {
    id: "c41ea0b2",
    status: "running",
    pipeline: "revenue_daily",
    assets: 1,
    launched: "now",
    duration: "0:00:12",
  },
  {
    id: "771bc0de",
    status: "failed",
    pipeline: "stripe_orders",
    assets: 1,
    launched: "Jul 15, 6:40:11 PM",
    duration: "0:00:03",
  },
  {
    id: "5a2f91c7",
    status: "success",
    pipeline: "cloud_product_sync",
    assets: 13,
    launched: "Jul 15, 1:15:02 PM",
    duration: "0:01:51",
  },
  {
    id: "e0c33b14",
    status: "success",
    pipeline: "ga4_events",
    assets: 4,
    launched: "Jul 15, 11:30:00 AM",
    duration: "0:00:44",
  },
  {
    id: "b7719d52",
    status: "success",
    pipeline: "simple",
    assets: 2,
    launched: "Jul 15, 9:02:08 AM",
    duration: "0:00:00.1",
  },
  {
    id: "1d8e4af0",
    status: "failed",
    pipeline: "predicted_orders",
    assets: 2,
    launched: "Jul 14, 4:18:55 PM",
    duration: "0:00:09",
  },
  {
    id: "44c6b2a9",
    status: "success",
    pipeline: "revenue_daily",
    assets: 1,
    launched: "Jul 14, 1:15:10 PM",
    duration: "0:00:31",
  },
  {
    id: "9aa10f7b",
    status: "success",
    pipeline: "cloud_product_sync",
    assets: 13,
    launched: "Jul 14, 1:14:58 PM",
    duration: "0:02:03",
  },
  {
    id: "2f5c8e61",
    status: "success",
    pipeline: "ga4_events",
    assets: 4,
    launched: "Jul 14, 11:30:02 AM",
    duration: "0:00:39",
  },
  {
    id: "8b3d7c40",
    status: "cancelled",
    pipeline: "model_revenue",
    assets: 1,
    launched: "Jul 13, 8:21:44 PM",
    duration: "0:00:05",
  },
  {
    id: "c92a1e33",
    status: "success",
    pipeline: "simple",
    assets: 2,
    launched: "Jul 13, 9:02:05 AM",
    duration: "0:00:00.1",
  },
  {
    id: "70de55a8",
    status: "success",
    pipeline: "events_to_bq",
    assets: 1,
    launched: "Jul 13, 7:00:00 AM",
    duration: "0:01:12",
  },
] as const;

export const diagnostics = [
  {
    severity: "error",
    asset: "stripe_orders",
    message: "Asset is overdue; freshness check failed.",
    action: "Run asset",
  },
  {
    severity: "error",
    asset: "revenue_daily",
    message: "Unit test nulls_count_as_zero failed: expected 0, got null.",
    action: "View test",
  },
  {
    severity: "warn",
    asset: "revenue_daily",
    message: "Schema drift: revenue declared DECIMAL but table is DOUBLE.",
    action: "View metadata",
  },
  {
    severity: "warn",
    asset: "predicted_orders",
    message: "Imports numpy and requests are not in the pipeline's Python dependencies.",
    action: "Add deps",
  },
  {
    severity: "warn",
    asset: "orders_cleaned",
    message: "Column customer_id has no description.",
    action: "Edit",
  },
] as const;

export const schemaRows = [
  { name: "day", declared: "DATE", actual: "DATE", status: "match", description: "Calendar day" },
  {
    name: "revenue",
    declared: "DECIMAL(18,2)",
    actual: "DOUBLE",
    status: "drift",
    description: "Sum of order amounts",
  },
  {
    name: "order_count",
    declared: "BIGINT",
    actual: "-",
    status: "missing",
    description: "Number of orders",
  },
  { name: "_loaded_at", declared: "-", actual: "TIMESTAMP", status: "extra", description: "" },
] as const;

export function editorLinesFor(asset: Pick<AppAsset, "kind" | "name"> & { model?: string }) {
  switch (asset.kind) {
    case "python":
      return asset.name === "predicted_orders"
        ? [
            "import pandas as pd",
            "import numpy as np",
            "import requests",
            "",
            "def main(orders):",
            "    return (orders",
            "        .groupby('day')['amount']",
            "        .sum())",
          ]
        : [
            "import pandas as pd",
            "from scipy import stats",
            "",
            "def main(revenue):",
            "    return stats.linregress(revenue.day, revenue.revenue)",
          ];
    case "load":
      return [
        "source:",
        "  connection: ga4",
        "  stream: public.events",
        "",
        "target:",
        "  connection: bq-prod",
        "  object: raw.events",
        "  mode: incremental",
        "  primary_key: [event_id]",
      ];
    case "source":
      return [
        "type: source",
        "# existing table, not managed by renart",
        "columns:",
        "  - name: id",
        "    type: BIGINT",
        "    checks: [not_null, unique]",
        "  - name: name",
        "    type: VARCHAR",
      ];
    case "ingestr":
      return [
        "type: ingestr",
        "source: stripe",
        "source_table: charges",
        "destination: duckdb-default",
      ];
    case "unittest":
      return [
        "unit_test:",
        `  model: ${asset.model ?? "revenue_daily"}`,
        "  given:",
        "    - input: orders_cleaned",
        "      rows:",
        "        - { day: 2026-06-01, amount: 100 }",
        "  expect:",
        "    rows:",
        "      - { day: 2026-06-01, revenue: 100 }",
      ];
    case "md":
      return [
        "## Revenue exploration",
        "",
        "Quick look at daily revenue vs. forecast.",
        "",
        "- source: `revenue_daily`",
        "- window: last 7 days",
      ];
    case "kpi":
      return [
        "title: Revenue (30d)",
        "type: kpi",
        "query: |",
        "  SELECT sum(revenue) FROM revenue_daily",
        "  WHERE day >= today() - 30",
        "format: currency",
      ];
    case "line":
      return [
        "title: Daily revenue",
        "type: line",
        "query: |",
        "  SELECT day, sum(revenue) AS revenue",
        "  FROM revenue_daily GROUP BY 1 ORDER BY 1",
      ];
    case "bar":
      return [
        "title: Revenue by region",
        "type: bar",
        "query: |",
        "  SELECT region, sum(revenue) AS revenue",
        "  FROM revenue_daily GROUP BY 1",
      ];
    case "table":
      return [
        "title: Top assets",
        "type: table",
        "query: |",
        "  SELECT name, rows",
        "  FROM asset_stats ORDER BY rows DESC LIMIT 20",
      ];
    default:
      return [
        "SELECT",
        "  date_trunc('day', created_at) AS day,",
        "  coalesce(sum(amount), 0) AS revenue",
        "FROM orders_cleaned",
        "GROUP BY 1",
      ];
  }
}

export const queryRows = [
  ["1", "2026-06-01", "$4,210"],
  ["2", "2026-06-02", "$5,118"],
  ["3", "2026-06-03", "$4,902"],
] as const;

export const eventRows = [
  [
    "1:17:14.940 PM",
    "freshness_check",
    "ASSET_CHECK",
    "Check freshness_check succeeded for materialization of stripe_orders.",
  ],
  [
    "1:17:15.020 PM",
    "freshness_check",
    "STEP_OUTPUT",
    "Yielded output orders_cleaned of type Any.",
  ],
  ["1:17:15.080 PM", "freshness_check", "STEP_SUCCESS", "Finished execution of step in 944ms."],
  ["1:17:16.894 PM", "-", "RUN_SUCCESS", "Finished execution of run for cloud_product_sync."],
] as const;

export const statusIcon = CheckCircle2;

export function getAsset(id: string) {
  return assets.find((asset) => asset.id === id) ?? assets[6];
}
