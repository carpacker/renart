import { cloneElement, isValidElement, ReactElement, ReactNode } from "react";
import { Database, Boxes, Globe } from "lucide-react";

import {
  SiClickhouse,
  SiDatabricks,
  SiDuckdb,
  SiGooglebigquery,
  SiMysql,
  SiPostgresql,
  SiSnowflake,
  SiTrino,
  SiGoogledataproc,
  SiPython,
  SiDbt,
  SiAirbyte,
  SiPrometheus,
  SiGrafana,
  SiSqlite,
  SiR,
} from "react-icons/si";
import { GiBearFace } from "react-icons/gi";

import { cn } from "@/lib/utils";

type AssetTypeIconProps = {
  assetType?: string;
  connection?: string;
  meta?: Record<string, string>;
  className?: string;
  size?: number;
};

export function AssetTypeIcon({
  assetType,
  connection,
  meta,
  className,
  size = 16,
}: AssetTypeIconProps) {
  const resolved = resolveAssetIcon(assetType, connection, meta, size);

  return <span className={cn(className)}>{resolved.icon}</span>;
}

export function resolveAssetIcon(
  assetType?: string,
  connection?: string,
  meta?: Record<string, string>,
  size = 16
): { icon: ReactNode; badge?: { background: string } } {
  const type = normalize(assetType);
  const provider = providerFromAssetType(type);
  const fallback = normalize(
    [
      connection,
      meta?.connection,
      meta?.platform,
      meta?.engine,
      meta?.destination,
    ]
      .filter(Boolean)
      .join(" ")
  );
  const value = provider || fallback;

  if (isPythonType(type)) {
    return iconWithColor(SiPython({ size }), "#3b82f6", "#dbeafe");
  }
  if (isRType(type)) {
    return iconWithColor(SiR({ size }), "#0284c7", "#cffafe");
  }
  if (isIngestrType(type)) {
    return iconWithColor(GiBearFace({ size }), "#d97706", "#fef3c7");
  }
  if (isSlingType(type)) {
    return iconWithColor(<Boxes size={size} />, "#7c3aed", "#ede9fe");
  }
  if (isAPIType(type)) {
    return iconWithColor(<Globe size={size} />, "#0891b2", "#cffafe");
  }
  if (isSensorType(type)) {
    return iconWithColor(SiPrometheus({ size }), "#f97316", "#ffedd5");
  }
  if (isSeedType(type)) {
    return iconWithColor(SiDbt({ size }), "#ea580c", "#ffedd5");
  }
  if (isDashboardType(type)) {
    return iconWithColor(SiGrafana({ size }), "#f97316", "#ffedd5");
  }

  if (has(value, "athena")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "clickhouse")) {
    return iconWithColor(SiClickhouse({ size }), "#ca8a04", "#fef3c7");
  }
  if (has(value, "databricks")) {
    return iconWithColor(SiDatabricks({ size }), "#ef4444", "#fee2e2");
  }
  if (has(value, "motherduck")) {
    return { icon: duckdbIcon(size), badge: { background: "#fef3c7" } };
  }
  if (has(value, "duckdb")) {
    return { icon: duckdbIcon(size), badge: { background: "#fef3c7" } };
  }
  if (has(value, "oracle")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "bigquery")) {
    return iconWithColor(SiGooglebigquery({ size }), "#3b82f6", "#dbeafe");
  }
  if (has(value, "microsoft sql server", "sqlserver", "mssql")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "fabric")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "mysql")) {
    return iconWithColor(SiMysql({ size }), "#0369a1", "#e0f2fe");
  }
  if (has(value, "postgres", "postgresql")) {
    return iconWithColor(SiPostgresql({ size }), "#2563eb", "#dbeafe");
  }
  if (has(value, "redshift")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "snowflake")) {
    return iconWithColor(SiSnowflake({ size }), "#0891b2", "#cffafe");
  }
  if (has(value, "synapse")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "amazons3", "s3")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "trino")) {
    return iconWithColor(SiTrino({ size }), "#6366f1", "#e0e7ff");
  }
  if (has(value, "emr")) {
    return placeholderIcon(size, value || type);
  }
  if (has(value, "dataproc")) {
    return placeholderIcon(size, value || type);
  }

  if (has(type, ".sql") || has(value, "sql")) {
    return iconWithColor(SiSqlite({ size }), "#8b5cf6", "#f3e8ff");
  }

  return placeholderIcon(size, value || type);
}

function placeholderIcon(size: number, seed: string) {
  const palette = pickPlaceholderPalette(seed);
  const iconSize = Math.max(12, size - 4);

  return {
    icon: (
      <span
        className="inline-flex items-center justify-center rounded-[4px]"
        style={{
          width: size,
          height: size,
          backgroundColor: palette.background,
        }}
      >
        <Database size={iconSize} color={palette.foreground} />
      </span>
    ),
    badge: { background: palette.background },
  };
}

function pickPlaceholderPalette(seed: string) {
  if (has(seed, "athena")) {
    return { background: "#fef3c7", foreground: "#a16207" };
  }
  if (has(seed, "oracle")) {
    return { background: "#fee2e2", foreground: "#dc2626" };
  }
  if (has(seed, "microsoft sql server", "sqlserver", "mssql", "fabric", "synapse")) {
    return { background: "#dbeafe", foreground: "#2563eb" };
  }
  if (has(seed, "redshift")) {
    return { background: "#f3e8ff", foreground: "#7c3aed" };
  }
  if (has(seed, "amazons3", "s3", "emr")) {
    return { background: "#ffedd5", foreground: "#ea580c" };
  }
  if (has(seed, "dataproc")) {
    return { background: "#dcfce7", foreground: "#16a34a" };
  }
  return { background: "#e5e7eb", foreground: "#6b7280" };
}

function iconWithColor(icon: ReactNode, color: string, background?: string) {
  if (!isValidElement(icon)) {
    return { icon, badge: background ? { background } : undefined };
  }

  return {
    icon: cloneElement(icon as ReactElement<{ color?: string }>, {
      color,
    }),
    badge: background ? { background } : undefined,
  };
}

function duckdbIcon(size: number) {
  return (
  <span  style={{ height: "1.5em" }} className="inline-flex items-center">
    <span
      style={{
        display: "inline-flex",
        position: "relative",
        width: size,
        height: size,
      }}
    >
      {iconWithColor(SiDuckdb({ size }), "#000000").icon}
      <svg
        aria-hidden="true"
        focusable="false"
        viewBox="0 0 24 24"
        style={{
          position: "absolute",
          inset: 0,
          pointerEvents: "none",
        }}
      >
        <circle cx="9.5" cy="12" r="4.97" fill="#ffff00" />
        <ellipse cx="17.1" cy="11.99" rx="2.07" ry="1.78" fill="#ffff00" />
      </svg>
    </span>
  </span>
  );
}

function providerFromAssetType(assetType: string): string {
  if (!assetType) {
    return "";
  }

  const [prefix] = assetType.split(".");

  const providersByPrefix: Record<string, string> = {
    athena: "athena",
    bq: "bigquery",
    clickhouse: "clickhouse",
    databricks: "databricks",
    dataproc_serverless: "dataproc",
    duckdb: "duckdb",
    emr_serverless: "emr",
    fabric: "fabric",
    fw: "fabric",
    motherduck: "motherduck",
    ms: "mssql",
    my: "mysql",
    oracle: "oracle",
    pg: "postgres",
    rs: "redshift",
    sf: "snowflake",
    synapse: "synapse",
    trino: "trino",
    s3: "s3",
  };

  return providersByPrefix[prefix] ?? assetType;
}

function isSeedType(assetType: string) {
  return assetType.endsWith(".seed");
}

function isSensorType(assetType: string) {
  return assetType.includes(".sensor.");
}

function isIngestrType(assetType: string) {
  return assetType === "ingestr";
}

function isSlingType(assetType: string) {
  return assetType === "sling";
}

function isAPIType(assetType: string) {
  return assetType === "api";
}

function isPythonType(assetType: string) {
  return assetType === "python" || assetType.includes("python_sdk");
}

function isRType(assetType: string) {
  return assetType === "r";
}

function isDashboardType(assetType: string) {
  return assetType.includes("dashboard") || assetType === "grafana";
}

function normalize(value?: string) {
  return (value ?? "").trim().toLowerCase();
}

function has(value: string, ...tokens: string[]) {
  return tokens.some((token) => value.includes(token));
}
