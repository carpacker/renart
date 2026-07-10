# HTTP API assets

Status: current state.

HTTP API assets fetch JSON records and load them into the asset's warehouse
table. They are plain Bruin-compatible YAML assets (`type: api`); Renart owns
the HTTP extraction path and delegates the JSONL-to-warehouse write to Sling.

## 1. Asset contract

The nested `parameters` block can contain:

- `request`: URL, method, headers, query parameters, and an optional JSON body;
- `auth`: bearer, basic, or API-key authentication (header or query);
- `iterate`: render one request per configured item;
- `response`: the dot-path containing records and an optional output field map;
- `pagination`: next URL (body or `Link` header), cursor, page-number, or offset;
- `openapi`: a schema URL plus optional operation selectors and a validation
  mode (`warn` by default, `error`, or `off`).

The asset's top-level `connection`, `materialization`, and `columns` use the
same file-backed fields as other assets. Windowed API loads normally use
`materialization.strategy: merge` and at least one primary-key column. This
keeps retries and overlapping windows replay-safe at the destination.

The alpha contract is one output table per API asset. Nested values are written
as JSON fields; Renart does not create child tables from nested arrays.

## 2. Execution

`HybridBruinExecutor.runAPIAsset` parses the spec, resolves Jinja values and the
target connection, streams each response page into a temporary JSON Lines file,
and invokes Sling for the warehouse write. JSONL preserves source nulls and
nested JSON values without CSV coercion. Per-page response bodies are capped at
25 MiB. GET requests retry `429` and `5xx` responses up to three attempts and
honour a capped `Retry-After`; mutating methods are not retried automatically.

Materialization intent maps to Sling as follows:

| Renart strategy | Sling behavior |
| --- | --- |
| Table (replace) | default replace behavior |
| Table (truncate) | `--mode truncate` |
| Append | snapshot, or incremental with `incremental_key` |
| Merge | incremental with primary-key columns and optional update key |
| Full refresh action | `--mode full-refresh`, overriding the saved strategy |

Unsupported strategies fail before the loader starts instead of silently
falling back to replacement.

## 3. Execution windows and replay

API request URLs, headers, query parameters, and bodies use the same standard
Jinja execution-window values as other assets, including `start_timestamp`,
`end_timestamp`, `start_date`, and `end_date`. An API can therefore receive the
selected materialization or backfill interval directly:

```yaml
parameters:
  request:
    params:
      updated_since: "{{ start_timestamp }}"
      updated_before: "{{ end_timestamp }}"
```

When an API asset references one of these values, the materialization log treats
it as interval-aware. Successful runs record coverage for their selected
window; failed runs do not. Coverage already includes the environment, asset
fingerprint, and variables hash, so historical backfills and parameterized
pipeline variants do not compete over a second mutable source position.

Destination merge plus primary keys provides replay safety. Pagination cursors
remain local to one extraction run and are discarded when it ends. Renart does
not persist opaque provider sync tokens in the alpha contract; APIs that only
support such tokens require a different extraction path.

## 4. OpenAPI, inference, and diagnostics

OpenAPI 3 and Swagger 2 documents are fetched with size/time limits and cached
in memory for ten minutes. Renart resolves operation response schemas, common
`$ref`/composition shapes, record paths, column types, and response-path
suggestions.

`POST /api/assets/{assetID}/api-infer` samples the first response page and
returns the request URL, candidate record paths, record count, inferred columns,
and warnings. Applying columns is an explicit user action. The YAML editor also
offers schema-backed completions and diagnostics for unresolved response and
pagination paths. Inside `request.url`, OpenAPI operation parameters are
completed after `?`/`&`; enum-backed parameter values are completed after `=`
(including comma-separated array values), and parameters already present in
the URL are omitted from subsequent name suggestions.

Run-time validation defaults to `warn`: mismatches appear in streaming output
and in the typed completion warning list without blocking the load. `error`
fails the run; `off` skips schema fetching and validation. Invalid validation
modes fail with an actionable configuration error.

## 5. Templates

The New asset dialog contains three pattern-focused starters:

- NWS weather alerts: OpenAPI inference and warning-level validation;
- PokéAPI: public next-URL pagination;
- Pipedrive Deals: query API-key auth, cursor pagination, merge, and
  execution-window filtering through `start_timestamp`.

Templates are examples copied into the user's asset file, not special runtime
integrations. Credentials remain environment references in the YAML.

## 6. Frontend ownership

The API parameters editor owns the nested request YAML. Guided metadata cards
own connection, materialization, columns, and response testing.
Creation templates live in `web/lib/api-asset-templates.ts`; HTTP calls and
generated DTOs live in `web/lib/api-assets-columns.ts` and
`web/lib/generated/api-types.ts`.

The filesystem remains authoritative. Edits go through Go endpoints and the
workspace SSE stream reconciles the final asset state.
