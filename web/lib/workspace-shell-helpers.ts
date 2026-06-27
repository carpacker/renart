import { NewAssetKind } from "@/components/new-asset-node";

export function buildSuggestedAssetName(
  kind: NewAssetKind,
  existingNames: Set<string>,
  pipelineName?: string | null
): string {
  const pipelinePrefix = slugifyPipelinePrefix(pipelineName);
  const prefixByKind: Record<NewAssetKind, string> = {
    sql: `${pipelinePrefix}.my_sql_asset_`,
    python: `${pipelinePrefix}.my_python_asset_`,
    ingestr: `${pipelinePrefix}.my_ingestr_asset_`,
    sling: `${pipelinePrefix}.my_sling_asset_`,
    api: `${pipelinePrefix}.my_api_asset_`,
  };

  const prefix = prefixByKind[kind];
  let index = 1;
  while (existingNames.has(`${prefix}${index}`)) {
    index += 1;
  }

  return `${prefix}${index}`;
}

export function normalizeAssetName(input: string): string {
  return input.trim().toLowerCase();
}

export function buildCreateAssetInput(
  name: string,
  kind: NewAssetKind,
  preferredSqlAssetType = "duckdb.sql"
): {
  name: string;
  type: string;
  path?: string;
  content?: string;
} {
  const path = buildAssetPathFromName(name, kind);
  if (kind === "python") {
    return {
      name,
      type: "python",
      path,
    };
  }

  if (kind === "ingestr") {
    return {
      name,
      type: "ingestr",
      path,
      content: `type: ingestr

parameters:
  source_connection: your-source-connection
  source_table: your_source_table
  destination: duckdb
`,
    };
  }

  if (kind === "sling") {
    const leaf = name.split(".").pop() ?? "asset";
    return {
      name,
      type: "sling",
      path,
      content: `source: your_source_connection
target: your_target_connection

defaults:
  mode: full-refresh
  object: public.${leaf}

streams:
  your_source_stream:
    object: public.${leaf}
`,
    };
  }

  if (kind === "api") {
    return {
      name,
      type: "api",
      path,
      // Defaults to a free, no-auth sample API that returns a JSON array, so the
      // asset materializes out of the box; swap in your own endpoint.
      content: `type: api

parameters:
  request:
    url: https://jsonplaceholder.typicode.com/users
    method: GET
    headers:
      Accept: application/json

  # The response is a JSON array, so records_path is the root ("").
  response:
    records_path: ""
`,
    };
  }

  return {
    name,
    type: preferredSqlAssetType,
    path,
  };
}

function buildAssetPathFromName(name: string, kind: NewAssetKind): string {
  const parts = name.split(".").map((part) => slugifyPipelinePrefix(part));
  const extensionByKind: Record<NewAssetKind, string> = {
    sql: ".sql",
    python: ".py",
    ingestr: ".asset.yml",
    sling: ".asset.yml",
    api: ".asset.yml",
  };
  const leaf = parts.pop() ?? "asset";
  return ["assets", ...parts, `${leaf}${extensionByKind[kind]}`].join("/");
}

export function buildOnboardingPythonStarterQuery(): string {
  return `import pandas as pd


def materialize():
    return pd.DataFrame(
        [
            {"customer_id": 1, "customer_name": "Ada Lovelace"},
            {"customer_id": 2, "customer_name": "Grace Hopper"},
            {"customer_id": 3, "customer_name": "Katherine Johnson"},
        ]
    )
`;
}

export function buildOnboardingSQLStarterQuery(
  pythonAssetName: string
): string {
  const pythonRef = tableReferenceForAssetName(pythonAssetName);

  return `with segment_map as (
    select *
    from (
        values
            (1, 'Enterprise'),
            (2, 'Startup'),
            (3, 'Research')
    ) as t(customer_id, segment)
)
select
    customers.customer_id,
    customers.customer_name,
    coalesce(segment_map.segment, 'General') as segment
from ${pythonRef} as customers
left join segment_map
    on customers.customer_id = segment_map.customer_id
order by customers.customer_id
`;
}

function slugifyPipelinePrefix(input?: string | null): string {
  const normalized = (input ?? "").trim().toLowerCase();
  if (!normalized) {
    return "default";
  }

  const slug = normalized
    .replace(/[^a-z0-9\s_-]/g, "")
    .replace(/[\s_]+/g, "_")
    .replace(/-+/g, "_")
    .replace(/^_+|_+$/g, "");

  return slug || "default";
}

function quoteSQLIdentifier(identifier: string): string {
  if (/^[a-z_][a-z0-9_]*$/i.test(identifier)) {
    return identifier;
  }

  return `"${identifier.replace(/"/g, '""')}"`;
}

function tableReferenceForAssetName(assetName: string): string {
  return assetName
    .split(".")
    .filter(Boolean)
    .map((part) => quoteSQLIdentifier(part))
    .join(".");
}
