import { cloneElement, isValidElement, ReactElement, ReactNode } from "react";

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

  if (!resolved) {
    return null;
  }

  return <span className={cn(className)}>{resolved.icon}</span>;
}

export function resolveAssetIcon(
  assetType?: string,
  connection?: string,
  meta?: Record<string, string>,
  size = 16
): { icon: ReactNode } | null {
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
    return iconWithColor(SiPython({ size }), "#3b82f6");
  }
  if (isRType(type)) {
    return iconWithColor(SiR({ size }), "#0284c7");
  }
  if (isIngestrType(type)) {
    return iconWithColor(GiBearFace({ size }), "#d97706");
  }
  if (isSensorType(type)) {
    return iconWithColor(SiPrometheus({ size }), "#f97316");
  }
  if (isSeedType(type)) {
    return iconWithColor(SiDbt({ size }), "#ea580c");
  }
  if (isDashboardType(type)) {
    return iconWithColor(SiGrafana({ size }), "#f97316");
  }

  if (has(value, "athena")) {
    return null;
  }
  if (has(value, "clickhouse")) {
    return iconWithColor(SiClickhouse({ size }), "#eab308");
  }
  if (has(value, "databricks")) {
    return iconWithColor(SiDatabricks({ size }), "#ef4444");
  }
  if (has(value, "motherduck")) {
    return { icon: duckdbIcon(size) };
  }
  if (has(value, "duckdb")) {
    return { icon: duckdbIcon(size) };
  }
  if (has(value, "oracle")) {
    return null;
  }
  if (has(value, "bigquery")) {
    return iconWithColor(SiGooglebigquery({ size }), "#3b82f6");
  }
  if (has(value, "microsoft sql server", "sqlserver", "mssql")) {
    return null;
  }
  if (has(value, "fabric")) {
    return null;
  }
  if (has(value, "mysql")) {
    return iconWithColor(SiMysql({ size }), "#0369a1");
  }
  if (has(value, "postgres", "postgresql")) {
    return iconWithColor(SiPostgresql({ size }), "#2563eb");
  }
  if (has(value, "redshift")) {
    return null;
  }
  if (has(value, "snowflake")) {
    return iconWithColor(SiSnowflake({ size }), "#06b6d4");
  }
  if (has(value, "synapse")) {
    return null;
  }
  if (has(value, "amazons3", "s3")) {
    return null;
  }
  if (has(value, "trino")) {
    return iconWithColor(SiTrino({ size }), "#6366f1");
  }
  if (has(value, "emr")) {
    return null;
  }
  if (has(value, "dataproc")) {
    return null;
  }

  if (has(type, ".sql") || has(value, "sql")) {
    return iconWithColor(SiSqlite({ size }), "#8b5cf6");
  }

  return null;
}

function iconWithColor(icon: ReactNode, color: string) {
  if (!isValidElement(icon)) {
    return { icon };
  }

  return {
    icon: cloneElement(icon as ReactElement<{ color?: string }>, {
      color,
    }),
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
