# HTTP API assets: schema, intellisense, dlt-parity sources

Status: proposed — blocked on questions.md #6/#7 (truncated brief + parity
bar); depends on primary-key column editing (landed separately).

## Current state (internal/web/service/api_asset.go)

Native `http.api` assets: YAML spec with `url`, auth, `response.records_path`,
flat field map → CSV → warehouse load. Executor: `runAPIAsset` +
`writeAPIAssetCSV`; `recordsAtPath` walks a dot-path; columns come from
`apiResponseFieldColumns`.

## Workstreams

### a) OpenAPI infer + validate
- `POST /api/assets/{id}/api-infer`: fetch sample (or accept a pasted
  OpenAPI doc URL), derive JSON schema of records at `records_path`
  (types, nullability, formats) and cache it next to the asset
  (`<asset>.openapi.yml` or embedded `response.schema`).
- Validation mode: during runs, check records against the stored schema;
  mismatches surface as run warnings/diagnostics (same channel as SQL
  type-check findings).

### b) records_path intellisense
- Backend endpoint returns the JSON structure of a sample response (paths +
  cardinalities); the YAML editor (use-yaml-intellisense.ts) completes
  `records_path` values from it, and flags paths that don't resolve.

### c) dlt-source parity (bounded)
- Adopt dlt's REST-source primitives, not the engine: pagination strategies
  (cursor, offset, page-number, next-url header/body), incremental cursors
  (`updated_at`-style with persisted state in .renart/state), auth presets
  (bearer, basic, api-key header/query), per-endpoint `primary_key` +
  `write_disposition` (append/replace/merge — merge needs the primary-key
  columns feature).
- Re-implement 2–3 verified-sources as bundled templates (questions.md #6
  for which; personio-style paginated + token auth is the reference shape).
  Templates appear in the new-asset dialog under "API sources".

### d) Column/type inference
- After (a), map JSON-schema types → warehouse column types and write the
  `columns:` block (with `primary_key: true` where the template declares it)
  into the asset YAML on user confirmation ("Infer columns" button in the
  asset editor, same UX as SQL column inference if/where that exists).

## Sequencing

1. records_path sampling endpoint + intellisense (small, immediately useful)
2. schema infer + column generation (a+d share the sampler)
3. pagination/incremental/auth engine extensions
4. bundled source templates + docs page update

## Testing

- Unit: recordsAtPath/pagination/cursor state machines against fixture JSON.
- Live e2e: httptest server with paginated fixtures; run asset → table
  materialized with inferred types; schema drift → warning diagnostic.
