# Materialization strategies: current support and a plan to reach parity with bruin

> **Status (2026-07):** Phase 1 is partially implemented — `materialization_strategy`
> and `incremental_key` are in the asset DTO (`model/dto.go`) and the patch API
> (`service/asset.go`), populated in `workspace.go`. Still missing from Phase 1:
> `partition_by`, `cluster_by`, `time_granularity`. Phases 2 (full refresh /
> backfill; run paths still hardcode `RunConfigFullRefresh = false`) and 3
> (capability-aware editor) are not started. Per-asset-type gaps (load/api
> assets ignore `Materialization`, `primary_key` is not editable, python needs
> `materialize()` + ingestr) are analyzed in the companion
> `materialization-per-asset-type.md`, which carries the active plan for them.

Exploration only — no implementation. Goal: map what bruin's materialization model
offers, what renart already supports (often more than it looks), and what we'd build
to expose incremental / time-based / merge / SCD-2 materializations through renart's
UI and API.

TL;DR: **the execution engine already supports every bruin strategy** — renart runs
SQL assets through bruin's per-warehouse `Materializer`, and the staleness stack
already tracks per-interval coverage for incremental assets. The gap is almost
entirely **surface area**: the asset DTO carries only `materialization_type`, the
patch API only writes `type`, there is no `full_refresh`/backfill control, and the UI
has no functional editor for strategy, incremental key, partition/cluster, or time
granularity. We can ship most of the value by widening the DTO + patch API and adding
an editor panel — no new execution code.

---

## 1. What bruin's materialization model is

`pipeline.Materialization` (`pkg/pipeline/pipeline.go`):

| Field             | Values / meaning |
| ----------------- | ---------------- |
| `Type`            | `""` (none / passthrough), `view`, `table` |
| `Strategy`        | `create+replace`, `delete+insert`, `truncate+insert`, `append`, `merge`, `time_interval`, `ddl`, `scd2_by_time`, `scd2_by_column` |
| `IncrementalKey`  | column used to delete/replace a window (`delete+insert`, `time_interval`, SCD-2) |
| `TimeGranularity` | `date` or `timestamp` — chooses `{{start_date}}`/`{{end_date}}` vs `{{start_timestamp}}`/`{{end_timestamp}}` for `time_interval` |
| `PartitionBy`     | warehouse partitioning (mainly BigQuery) |
| `ClusterBy`       | `[]string` clustering (BigQuery / Snowflake) |

How execution works: `Materializer.Render(asset, query)` (`pkg/pipeline/materializer.go`)
looks up `MaterializationMap[type][strategy]` — a **per-warehouse** map of functions
that rewrite the asset's SELECT into the right DML. Each warehouse package ships its
own (`pkg/duckdb/materialization.go`, `pkg/bigquery/...`, `pkg/snowflake/...`, etc.),
so the generated SQL is dialect-correct. Concrete duckdb examples:

- **create+replace / none** → `BEGIN; DROP TABLE IF EXISTS x; CREATE TABLE x AS <q>; COMMIT`
- **append** → `INSERT INTO x <q>`
- **delete+insert** (needs `incremental_key`) → temp table, `DELETE FROM x WHERE key IN (SELECT DISTINCT key FROM tmp)`, `INSERT … SELECT * FROM tmp`
- **merge** (needs `columns` + at least one `primary_key`) → temp table, `UPDATE … FROM` on PK match + `INSERT … WHERE NOT EXISTS`
- **time_interval** (needs `incremental_key` + `time_granularity`) → `DELETE FROM x WHERE key BETWEEN '{{start_*}}' AND '{{end_*}}'`, then `INSERT INTO x <q>` — the window comes from the run context
- **ddl** → `CREATE TABLE IF NOT EXISTS` from `columns` (+ PK + column comments); never dropped, even on full refresh
- **scd2_by_time / scd2_by_column** → full `_valid_from/_valid_until/_is_current` history build; needs `primary_key`(s), and time variant needs `incremental_key`

`full_refresh` (Materializer field): when set and type is `table`, bruin overrides the
strategy to `create+replace` (except `ddl`, and except assets with
`RefreshRestricted`). This is how a "rebuild from scratch" run differs from an
incremental run.

Validation lives inside each materializer func (e.g. "merge requires primary_key",
"incremental_key must be TIMESTAMP or DATE", "time_granularity required for
time_interval"). Strategy support is **not uniform across warehouses** — duckdb views
reject `append`/`delete+insert`; some warehouses won't have every table strategy.

---

## 2. What renart already supports today

More than the UI implies. Evidence by file:

**Execution — all strategies, already wired.**
`internal/web/service/direct_executor_registry.go` registers, per warehouse, bruin's
`NewBasicOperator(manager, extractor, HookWrapperMaterializer{Mat: <wh>.NewMaterializer(false)}, parser)`.
So when renart runs a SQL asset, it goes through the same materializer bruin's CLI
uses. **If an asset file already declares `materialization: { strategy: merge, … }`,
renart executes it correctly today.** No renart-side strategy code exists or is needed.

**Run window — passed through.** `execution.go` resolves a time window and calls
`Executor.RunAsset(…, StartDate, EndDate)` (`execution.go:567`, `:929`). Combined with
the Jinja path (renart renders `start_date`/`end_date`/`start_timestamp`/… from the
run context, same as the notebook Jinja work), `time_interval` and `scd2_by_time`
get their window. (Worth an explicit end-to-end test — see Open questions.)

**Staleness / coverage — incremental-aware already.** The matlog stack models
per-interval coverage: `matlog/recorder.go:IntervalAware(asset)` returns true for
`time_interval`, `delete+insert`, `append`, or any asset with an `incremental_key`;
`recorder.go` stamps `IntervalStart/IntervalEnd` from the run window; `matlog/store.go`
merges contiguous intervals into `CoverageRow`s (full-refresh assets get a single
"built" marker, `IntervalStart == nil`). The staleness service already turns this into
partial-coverage (`covered_seconds`/`total_seconds`) shown in the UI. So the data
model for "how much of an incremental table is built" is in place.

**Asset patch — preserves existing strategy.** `asset.go` patches a parsed asset and
calls `asset.Persist(...)`, which round-trips the whole `materialization` block. So
hand-written strategy/incremental fields survive edits — they're just not editable
through renart.

### The gaps

1. **DTO is type-only.** `model.Asset` (`dto.go`) exposes `MaterializationType`,
   `IsMaterialized`, `MaterializedAs` — but **not** `strategy`, `incremental_key`,
   `partition_by`, `cluster_by`, `time_granularity`. The frontend can't even *read*
   an asset's strategy. (`workspace.go:249` only populates `MaterializationType`.)
2. **Patch API is type-only.** `PatchAssetRequest` (`asset.go:19`) has only
   `MaterializationType`. No way to set strategy/incremental/partition/cluster.
3. **No `full_refresh`.** Materializers are constructed with `NewMaterializer(false)`
   and no run path threads a full-refresh flag. There's no "rebuild this incremental
   table from scratch" action, and no backfill-a-window action.
4. **No functional UI.** The redesign build-page *mock*
   (`build-page.tsx:1794`) shows a rich Materialization form (Type, Strategy,
   Object name, Partition by, Incremental key, Cluster by) — but it only feeds a YAML
   **preview tab** (`build-page.tsx:1836`) with placeholder defaults
   (`partitionBy="day"`, `incrementalKey="created_at"`); nothing persists. The real
   editor (`asset-editor-header.tsx` / `api-assets-crud.ts`) sends only
   `materialization_type`.
5. **No per-warehouse capability surfacing.** Strategy availability differs by
   warehouse and column metadata (merge needs PKs, SCD-2 needs reserved columns). The
   UI has nothing to validate or guide this, so a user could pick an unsupported combo
   and only learn at run time.

---

## 3. Proposed implementation (phased, no code yet)

Ordered so each phase is independently useful.

### Phase 1 — Read + write the whole materialization block (highest value, lowest risk)

- **DTO**: add `materialization_strategy`, `incremental_key`, `partition_by`,
  `cluster_by []string`, `time_granularity` to `model.Asset` (and the materialization
  state DTO), populated from `asset.Materialization.*` in `workspace.go`.
- **Patch API**: extend `PatchAssetRequest` with the same optional fields; in
  `asset.go`, set `asset.Materialization.Strategy/IncrementalKey/PartitionBy/ClusterBy/TimeGranularity`
  before `Persist`. Normalize/validate enum values server-side (reject unknown
  strategy / granularity), but defer *semantic* validation (PK presence, key type) to
  run time — bruin already returns clear errors.
- **Frontend**: wire the existing build-page form (and the real asset editor) to send
  these fields via the patch API; drop the placeholder defaults; render the asset's
  actual strategy/key in read-only views (catalog/build "Materialization" field).
- **Tests**: api-types regen (the generator coupling noted in backend-refactor); a Go
  patch test round-tripping a `merge`/`time_interval` block; a live e2e that sets a
  strategy via the UI and asserts the asset file's `materialization:` block.

After Phase 1, every bruin strategy is fully usable through renart (declare it,
execute it) — because execution already works.

### Phase 2 — Full refresh & backfill controls

- **Run option**: add `full_refresh bool` (and optionally an explicit
  `start`/`end` backfill window) to the asset/pipeline run request
  (`execution.go` / `httpapi/execution.go`).
- **Executor**: the registry builds materializers eagerly with `false`. Either build
  them per-run from a `fullRefresh` flag, or register both and pick. Cleanest is to
  parametrize operator construction by run options.
- **UI**: a "Rebuild from scratch (full refresh)" action on table assets, and a
  "Backfill range…" action for interval-aware assets (date/timestamp range picker,
  reusing the execution-time-window component). Guard with a confirm — full refresh on
  a large incremental table is expensive.
- **Coverage interplay**: a full refresh should reset coverage to the "built" marker;
  a backfill should record its interval. `matlog/recorder.go` already keys off the run
  window, so this mostly falls out — verify the full-refresh path writes the
  single-marker coverage, not an interval.

### Phase 3 — Capability-aware, guided editor

- **Capability map**: expose, per asset type / connection, which strategies are valid
  (mirror each warehouse's `matMap` keys) and which fields each strategy requires
  (incremental_key, primary_key columns, columns for merge/ddl). Could be a static
  table generated from bruin's maps, or a small backend endpoint.
- **UI affordances**: only show valid strategies for the asset's warehouse; when
  `merge`/SCD-2 is chosen, surface the primary-key requirement inline with the
  Columns editor; when `time_interval` is chosen, require `incremental_key` +
  `time_granularity`; preview the generated DML (bruin's `Materializer.Render` could
  back a dry-run endpoint, akin to the existing Jinja-render preview).
- **Staleness surfacing**: for interval-aware assets, show the coverage timeline
  (the data already exists) so users see what's built vs missing and can backfill the
  gaps from the same view.

---

## 4. Notebook angle (optional, smaller)

Notebook cells already have a tiny materialization model (`@materialize(table)` pins a
table vs a view; Python cells always materialize a table). Notebooks deliberately run
in a local DuckDB session, not a warehouse, so incremental strategies are largely out
of scope there. If wanted, the cell run path (`internal/web/notebook/run.go`) could
grow `@materialize(strategy=…)` directives that map onto the duckdb materializer — but
this is low priority vs. pipeline assets and risks complicating the notebook model.

---

## 5. Open questions / risks

- **End-to-end window rendering**: confirm `time_interval` actually renders
  `{{start_timestamp}}`/`{{end_timestamp}}` correctly through renart's run path (bruin
  injects these literals during materialization; they must then be Jinja-rendered with
  the run window). Likely fine since renart reuses bruin's operator, but it deserves a
  dedicated test before we advertise the feature.
- **Per-warehouse divergence**: the strategy × type matrix and required fields differ
  by warehouse; a generic UI must be data-driven off each `matMap`, not hardcoded, or
  it will offer invalid combos.
- **Column metadata dependency**: merge/ddl/SCD-2 need accurate `columns` +
  `primary_key`. renart's column editor must be solid first, or these strategies fail
  at run time with confusing errors.
- **Full-refresh blast radius**: needs a clear confirm + ideally a cost hint; an
  accidental full refresh on a large warehouse table is a real footgun.
- **`RefreshRestricted` / `ddl`**: respect bruin's "never drop" semantics in the
  full-refresh UI (don't imply a ddl/restricted asset will be rebuilt).

---

## 6. Key files (for whoever implements)

- bruin model: `pkg/pipeline/pipeline.go` (`Materialization`, strategy/ granularity
  consts), `pkg/pipeline/materializer.go` (`Render`, full-refresh override),
  `pkg/<warehouse>/materialization.go` (per-strategy SQL + validation)
- renart execution: `internal/web/service/direct_executor_registry.go` (materializer
  wiring), `internal/web/service/execution.go` (run window, RunAsset)
- renart model/API: `internal/web/model/dto.go` (`Asset`), `internal/web/service/asset.go`
  (`PatchAssetRequest`, persist), `internal/web/service/workspace.go` (DTO population)
- renart coverage: `internal/web/matlog/recorder.go` (`IntervalAware`),
  `internal/web/matlog/store.go` (`CoverageRow` merge), `internal/web/staleness/`
- renart frontend: `web/components/redesign/build-page.tsx` (mock editor +
  `buildDefinitionYaml`), `web/components/asset-editor-header.tsx`,
  `web/lib/api-assets-crud.ts` (`PatchAssetRequest` type)
