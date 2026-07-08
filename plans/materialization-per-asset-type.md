# Materialization per asset type: making every offered mode actually work

Concept only — no implementation. Companion to
`materialization-strategies.md` (SQL-focused, partially implemented since:
the DTO/patch API now carry `materialization_strategy` + `incremental_key`, and the
workbench sheet shows a Materialization card for every asset type). This document
covers the next problem: **the UI lets users configure materialization on sql,
python, load and api assets, but only a subset of the offered modes executes
correctly — and for load/api none of it is wired to the run path at all.**

The UI currently offers (`MATERIALIZATION_OPTIONS`, `asset-guided-cards.tsx:151`):
`none`, `view` (SQL only), `table (create+replace)`, `append`, `merge`,
`incremental (time_interval)` — the same list for all four asset types.

---

## 1. Verified current behavior per asset type

### SQL — executes via bruin, two real gaps

Runs through bruin's per-warehouse `Materializer`
(`direct_executor_registry.go`, e.g. `duck.NewBasicOperator(..., HookWrapperMaterializer{Mat: duck.NewMaterializer(false)}, ...)`),
so the engine supports every bruin strategy. What breaks from the UI:

1. **`time_interval` always fails.** The materializer requires
   `time_granularity` (`bruin pkg/duckdb/materialization.go:200`:
   *"time_granularity is required for time_interval strategy"*), but renart never
   sets it: `AssetUpdateRequest` has only type/strategy/incremental_key
   (`service/asset.go:19-21`) and the card has no granularity input. Every
   UI-configured "Incremental (time interval)" asset errors at run time.
2. **`merge` is unconfigurable.** Bruin's merge needs at least one column with
   `primary_key: true`. The workbench columns card only *displays* a key badge
   (`asset-guided-cards.tsx:600`) — there is no way to set `primary_key` from the
   UI, so "Merge by key" fails at run time unless the user hand-edits the file.
3. (Known, deferred) `full_refresh` is hardcoded off — `NewMaterializer(false)`,
   `RunConfigFullRefresh=false` in `buildDirectRunAssetContext`
   (`direct_run.go:432`). Irrelevant for the currently offered modes, blocking
   only for SCD2-style strategies (not offered).

### Python — executes via bruin, with sharp edges

`bruinpython.NewLocalOperator` (used at `direct_executor_registry.go:257`) is
bruin's full uv runner: when `materialization.type == "table"` it runs the asset
through `runWithMaterialization` (`bruin pkg/python/uv.go:346`) — the script must
expose a **`materialize()`** function returning a dataframe; the result is written
to an Arrow file and uploaded with **ingestr** (`uv tool run ingestr ingest
--incremental-strategy ...`). So python materialization *does* work today, iff:

- the script defines `materialize()` (a plain script with top-level side effects
  runs, then logs "materialize() returned None, skipping materialization");
- the strategy is one of bruin's supported set for python: `create+replace`,
  `append`, `merge`, `delete+insert` (`bruin pkg/python/materialization_mapping.go`).
  **`time_interval` is NOT supported** — bruin's translation silently drops the
  strategy and ingestr falls back to `replace`, i.e. the UI offers an option that
  silently does the wrong thing;
- `merge` again needs `primary_key` columns (same UI gap as SQL);
- the destination connection resolves via
  `Pipeline.GetConnectionNameForAsset` → majority-SQL-asset-type fallback — fine
  for the common single-warehouse workspace.

Note: this path ships **ingestr** (installed on the user's machine via uv at run
time). Renart deliberately avoided ingestr for load assets (license); using
bruin's stock python operator means python-materialized assets pull it in anyway.
Decision needed: accept (it's upstream bruin behavior) or, later, replace the
upload leg with the sling bridge (Arrow → CSV → `sling run`, same as api assets).

### Load — materialization is parsed, persisted, and completely ignored

`runLoadAsset` (`service/load.go:345`) builds
`sling run --src-conn/--src-stream/--tgt-conn/--tgt-object [--mode <params.mode>]`
purely from the flat `parameters`; `asset.Materialization` is never read. Two
consequences:

- The Materialization card and the load editor's own `mode` select
  (`load-parameters-editor.tsx:169`) are **two disconnected controls**; only
  `mode` does anything.
- Even `mode` is only half-wired: sling's `incremental` mode requires
  `--primary-key` and/or `--update-key`, which renart never passes
  (`loadModeArgsFromParams` emits `--mode` only), so incremental sling runs fail
  outright.

### HTTP API — always full-refresh, everything else is a no-op

`runAPIAsset` (`service/api_asset.go:298`) fetches → CSV → `sling run
--src-stream file://…csv --tgt-conn … --tgt-object …` with mode args only from the
(always-false) full-refresh context. Sling's default mode is `full-refresh`, so an
api asset always behaves like `create+replace` regardless of configuration —
"Table (replace)" works by accident; `append`, `merge`, `time_interval` configure
fine and change nothing. (The spec's `load.mode` field is parsed into
`nativeAPILoad.Mode` but never used — dead surface to remove.)

---

## 2. Design

### 2.1 One source of truth: `asset.Materialization`

For all four types, the Materialization card (and its YAML-view twin) is *the*
control. Sling's `mode` parameter stops being user-facing: it stays readable for
back-compat (used only when `materialization` is empty), the editor's mode select
is removed, and the api spec's dead `load.mode` is dropped from the parse struct.

### 2.2 Shared bruin→sling strategy mapping (fixes sling + api at once)

Both sling and api assets load through the sling CLI, so one helper in
`service/load.go` covers both run paths:

```go
// slingMaterializationArgs maps asset.Materialization (+ primary-key columns)
// to sling --mode/--primary-key/--update-key flags.
func slingMaterializationArgs(asset *pipeline.Asset) ([]string, error)
```

| bruin materialization                  | sling invocation                                             | notes |
| -------------------------------------- | ------------------------------------------------------------ | ----- |
| none / `table` + `create+replace`      | *(no flags)* — sling defaults to `full-refresh`              | current behavior, now explicit |
| `table` + `truncate+insert`            | `--mode truncate`                                            | keeps table/permissions, replaces rows |
| `table` + `append`, incremental_key set| `--mode incremental --update-key <incremental_key>`          | append only rows newer than max(key) — sling's append-new semantics |
| `table` + `append`, no incremental_key | `--mode snapshot`                                            | sling's append-everything mode; adds a `_sling_loaded_at` column — must be documented in the card's helper text |
| `table` + `merge`                      | `--mode incremental --primary-key <pk1,pk2,…>` + optional `--update-key <incremental_key>` | PKs from `asset.ColumnNamesWithPrimaryKey()`; validation error if empty. With update-key sling merges only new/changed rows; without, full scan + upsert |
| `view`, `time_interval`, `delete+insert`, `ddl`, scd2 | **rejected** (validation error at save and at run) | no sling equivalent; UI never offers them for load/api |

`runLoadAsset` replaces `loadModeArgsFromParams` with: explicit full-refresh
from run context wins → else `slingMaterializationArgs` → else legacy
`params.Mode` passthrough. `runAPIAsset` appends the same helper's output instead
of `slingRunModeArgs(ctx)` alone.

### 2.3 Per-asset-type capability matrix (backend-validated, UI-driven)

Replace the single `MATERIALIZATION_OPTIONS` list with a matrix keyed by asset
kind (sql / python / sling / api), exported from one place and enforced in
`updateAsset` + the transactions endpoint (reject unsupported combos with a 400
naming the asset kind):

| option (UI value)          | sql | python | sling | api |
| -------------------------- | --- | ------ | ----- | --- |
| none (run only)            | ✓   | ✓      | –¹    | –¹  |
| view                       | ✓   | –      | –     | –   |
| table (create+replace)     | ✓   | ✓      | ✓     | ✓   |
| truncate+insert²           | ✓   | –      | ✓     | ✓   |
| append                     | ✓   | ✓      | ✓     | ✓   |
| merge                      | ✓   | ✓      | ✓     | ✓   |
| delete+insert²             | ✓   | ✓      | –     | –   |
| incremental (time_interval)| ✓   | –³     | –     | –   |

¹ for load/api "none" *is* full-refresh (a loader always writes a table), so the
matrix shows only "table (replace)" as the default instead of a misleading "none".
² new options worth adding while we're here — both already execute via bruin's SQL
materializer, and truncate maps cleanly to sling.
³ currently offered and silently degrades to replace — must be removed.

### 2.4 Close the two cross-cutting SQL/python gaps

- **`time_granularity`**: add to `AssetUpdateRequest`, WebAsset DTO and the card —
  a Date/Timestamp select that appears only when `time_interval` is chosen
  (default it from the incremental-key column's declared type when available).
  Without this, the SQL time_interval option stays broken.
- **Primary keys**: make the key badge in the Columns card an editable toggle
  backed by a new semantic transaction (`column.primary_key.set` /
  `.unset` in `asset_transactions.go`). When `merge` is selected and no column has
  a primary key, the Materialization card shows an inline blocker linking to the
  Columns card ("Merge needs at least one key column") — same validation the
  backend enforces. This unblocks merge for **all four** asset types.

### 2.5 Run-time guardrails

Bruin's SQL materializers already produce clear errors; add equivalents where
renart owns the path:

- `runLoadAsset`/`runAPIAsset`: fail fast with actionable messages
  ("merge materialization needs at least one primary-key column") *before*
  spawning sling.
- Python: on save, reject `time_interval`/`view` for python assets; optionally
  detect a missing `materialize()` (cheap regex on the script) and surface a
  card-level warning rather than letting the run "succeed" with the
  skipping-materialization log line.

### 2.6 Coverage/staleness interplay (small, verify only)

`matlog/recorder.go:IntervalAware` keys off strategy/incremental_key, so once
load/api assets carry real strategies, their runs will start recording
interval rows stamped with the run window. Sling doesn't actually filter by that
window (its incremental state is max-of-key, not the renart window), so coverage
intervals would be cosmetic-but-wrong. Simplest rule: treat load/api assets as
non-interval-aware (single "built" marker) regardless of strategy — one guard in
`IntervalAware`.

---

## 3. Phased plan

**Phase 1 — sling + api execute their materialization (the actual bug report).**
`slingMaterializationArgs` + wiring in both run paths + fail-fast validation +
unit tests on the flag mapping (table above, incl. PK join and legacy `mode`
fallback). Remove the load editor's mode select; keep `mode` param read-only
back-compat. Deliverable check: duckdb live e2e — sling csv→duckdb and an api
asset, each run twice under `merge` (assert no duplicate rows) and `append`
(assert row count grows).

**Phase 2 — capability matrix + the two blocking editors.** Per-type options in
the card/YAML view, backend validation in `updateAsset` + transactions,
`time_granularity` field end-to-end (api-types regen), `column.primary_key.set`
transaction + editable key toggle. This makes SQL `time_interval` and merge (all
types) reachable from the UI. Live e2e: configure merge + key via UI only, run
twice, no dupes.

**Phase 3 — polish and parity.** Add `truncate+insert`/`delete+insert` options
where supported; python `materialize()` presence warning; decide the
ingestr-license question for python uploads (keep bruin stock vs. sling bridge);
staleness guard from §2.6; full-refresh action stays deferred to the older doc's
Phase 2.

---

## 4. Open questions

- **Append semantics for loaders**: is `snapshot` (with its `_sling_loaded_at`
  extra column) acceptable as "append without incremental key", or should
  keyless append be rejected for load/api? Snapshot is more useful; the extra
  column may surprise schema-sensitive downstreams.
- **Existing sling assets with `mode` set**: migrate on next save (derive
  materialization from mode, drop the param) or leave until touched? Proposal:
  leave; the fallback keeps them working.
- **Python + ingestr license**: bruin's stock python materialization shells out
  to ingestr. If that's unacceptable, the sling-bridge alternative reuses the
  api-asset CSV path but loses ingestr's `delete+insert` and type fidelity
  (CSV round-trip); Arrow→parquet→sling would preserve types better.

## 5. Key files

- Run paths: `internal/web/service/load.go` (`runLoadAsset`,
  `loadModeArgsFromParams` → replace), `internal/web/service/api_asset.go`
  (`runAPIAsset`), `internal/web/service/direct_executor_registry.go` (SQL +
  python operators; nothing to change for their engines).
- Model/validation: `internal/web/service/asset.go` (`AssetUpdateRequest`,
  `updateAsset` — add `time_granularity`, capability validation),
  `internal/web/service/asset_transactions.go` (`column.primary_key.*`),
  `internal/web/model/dto.go` + generated api-types.
- Frontend: `web/components/redesign/asset-guided-cards.tsx`
  (`MATERIALIZATION_OPTIONS` → per-type matrix, granularity select, merge-key
  blocker), `web/components/redesign/asset-yaml-editor.tsx` (same options),
  `web/components/redesign/load-parameters-editor.tsx` (drop mode select).
- Bruin references: `pkg/python/materialization_mapping.go` (python strategy
  set), `pkg/python/uv.go:346` (`runWithMaterialization`),
  `pkg/<warehouse>/materialization.go` (SQL validation),
  `pipeline.Asset.ColumnNamesWithPrimaryKey`.

