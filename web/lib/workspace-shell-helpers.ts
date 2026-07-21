import { NewAssetKind } from "@/components/new-asset-node";

export function buildSuggestedAssetName(
  kind: NewAssetKind,
  existingNames: Set<string>,
  pipelineName?: string | null,
): string {
  const pipelinePrefix = slugifyPipelinePrefix(pipelineName);
  const prefixByKind: Record<NewAssetKind, string> = {
    sql: `${pipelinePrefix}.my_sql_asset_`,
    python: `${pipelinePrefix}.my_python_asset_`,
    ingestr: `${pipelinePrefix}.my_ingestr_asset_`,
    load: `${pipelinePrefix}.my_load_asset_`,
    api: `${pipelinePrefix}.my_api_asset_`,
    seed: `${pipelinePrefix}.my_seed_asset_`,
    sensor: `${pipelinePrefix}.my_sensor_asset_`,
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

export function buildOnboardingSQLStarterQuery(pythonAssetName: string): string {
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
