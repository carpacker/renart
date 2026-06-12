# Implementation plan: fingerprints, staleness, snapshots, per-env schedules, protected environments

Scope: the five interlocking features discussed — fingerprint engine, materialization
log + coverage, staleness service + UI, deploy snapshots, per-environment schedules,
and environment protection. Ordered so each phase ships something usable on its own
and nothing has to be ripped out later. Estimates assume solo work and existing
familiarity with the codebase; they cover implementation + tests, not design churn.

**Dependency graph**

```
Phase 0 (identity, migrations)
   ├── Phase 1 (fingerprints) ──► Phase 2 (facts/coverage) ──► Phase 3 (staleness UI)
   ├── Phase 4 (snapshots/deploy) ──► Phase 5 (per-env schedules) ──► Phase 6 (protection)
   └── Phase 7 (Python hardening — anytime after 1)
```

Phases 1–3 and 4–6 are two independent tracks after Phase 0; interleave if you want
visible progress on both fronts. Total: roughly 3.5–4.5 weeks.

---

## Phase 0 — Identity and storage groundwork (1–2 days)

**Goal:** stable IDs and a migrations story, so every later table has sound keys.

1. **Pipeline identity.** Add `id: <uuid>` to `pipeline.yml`, generated on creation;
   on first load of a pipeline without one, generate and write it back (with a
   notice). This is the key everything durable hangs off — schedules, run history,
   snapshots.
2. **Asset identity.** Use `pipeline_id + asset_name` as `asset_id` for v1. Accepted
   limitation: renaming an asset orphans its history. Document it; add the
   content-hash rename heuristic later only if it hurts. (Notebook cells already get
   stable IDs per the notebook design — same field, different generator.)
3. **Migrations.** All new tables live in the same SQLite database River uses, under
   a `renart_` prefix, with a simple versioned-migration runner if one doesn't exist
   yet (golang-migrate or 30 lines of hand-rolled `schema_version`). Every later
   phase adds a migration file; none alter River's own tables.
4. **Event bus seam.** Confirm there's a single place that observes run completion
   (River subscription) and file saves. Phases 2 and 3 both attach here; if it's
   currently diffuse, consolidate now — it's a half-day that saves a week.

**Exit criteria:** pipelines self-assign UUIDs; `renart_schema_version` exists;
one Go interface (`Events`) emits `RunCompleted` and `AssetSaved`.

---

## Phase 1 — Fingerprint engine (3–5 days)

**Goal:** deterministic `Fingerprint(asset, env, vars)` for every asset type, with
Merkle propagation, exposed as one Go package consumed by everything later.

### Design points

- **Fingerprint inputs (SQL assets):**
  `H(fp_version ‖ canonical_sql ‖ config_hash ‖ consumed_vars_hash ‖ sorted(upstream_fps))`
  - `canonical_sql`: parse with the existing SQL machinery → strip comments →
    collapse whitespace. Nothing cleverer (no identifier case folding across
    dialects — too risky).
  - `config_hash`: materialization strategy, partitioning, target schema/name,
    incremental keys — anything that changes what lands in the warehouse.
  - `consumed_vars_hash`: hash of (name, value) pairs for **only the variables the
    render actually read**. Wrap the Jinja context in a recording map; persist the
    consumed-name set per asset so the staleness check can recompute the hash for a
    new variable configuration without re-rendering.
- **Fingerprint inputs (Python assets, v1):**
  `H(fp_version ‖ file_bytes ‖ lockfile_hash ‖ shared_dir_hash ‖ config_hash ‖ upstream_fps)`
  - `lockfile_hash`: requirements.txt / uv.lock, whichever Bruin resolves against.
  - `shared_dir_hash`: one hash over the designated shared-code directory.
  - Consumed-vars: assume **all** vars for now (over-invalidates, safe).
- **`fp_version` is mandatory.** Bake an algorithm version constant into every hash
  and store it alongside. When canonicalization improves later, bump it; everything
  goes stale once, which is correct and self-healing. Without it, algorithm changes
  silently mismatch history.
- **Escape hatches now, not later** (they're trivial and they cap every edge-case
  discussion): optional `version:` in asset config that *replaces* the content hash;
  optional `depends_on_files:` globs whose contents get hashed in.
- **Merkle + memoized incremental compute:** `Engine.FingerprintDAG(pipeline, env,
  vars)` returns the full map; `Engine.Invalidate(assetID)` marks a node dirty and
  recomputes only its downstream cone on next read. Keep the cache keyed by
  (env, vars_hash) for the *current* selection only — don't cache across selections.

### Package sketch

```go
package fingerprint

type Fingerprint string // hex, prefixed "v1:"

type Engine interface {
    DAG(ctx, pipeline, env string, vars Vars) (map[AssetID]Result, error)
    Invalidate(asset AssetID) // file save hook
}

type Result struct {
    FP           Fingerprint
    ConsumedVars []string // names only; values live in the hash
}
```

### Tests

Golden-file determinism tests (same input → same hash across runs/platforms);
property tests: comment-only edit → unchanged; whitespace edit → unchanged; upstream
edit → all downstream change; unrelated-var flip → unchanged when var not consumed.
These tests are the contract — be generous here, everything downstream trusts them.

**Exit criteria:** `renart fp <pipeline>` debug command prints the DAG's
fingerprints; tests above green.

---

## Phase 2 — Materialization facts and coverage (2–3 days)

**Goal:** every run writes immutable facts; a compacted coverage table answers
freshness lookups in O(gaps).

### Schema

```sql
CREATE TABLE renart_materializations (
  id              INTEGER PRIMARY KEY,
  asset_id        TEXT NOT NULL,
  environment     TEXT NOT NULL,
  fingerprint     TEXT NOT NULL,
  vars_hash       TEXT NOT NULL,
  interval_start  TEXT,            -- NULL for full-refresh assets
  interval_end    TEXT,
  run_id          TEXT NOT NULL,
  materialized_at TEXT NOT NULL
);
CREATE INDEX idx_mat_lookup ON renart_materializations
  (asset_id, environment, fingerprint, vars_hash);

CREATE TABLE renart_coverage (
  asset_id        TEXT NOT NULL,
  environment     TEXT NOT NULL,
  fingerprint     TEXT NOT NULL,
  vars_hash       TEXT NOT NULL,
  interval_start  TEXT,            -- NULL means "built" marker (full refresh)
  interval_end    TEXT,
  materialized_at TEXT NOT NULL,   -- latest run contributing to this row
  PRIMARY KEY (asset_id, environment, fingerprint, vars_hash, interval_start)
);
```

### Write path (in the run-completion transaction)

1. Insert the raw fact row.
2. Merge into coverage: select rows for the key where
   `existing.start <= new.end AND existing.end >= new.start` **or adjacent**
   (`existing.end == new.start` / `new.end == existing.start`); compute the union
   min/max across all matches plus the new interval; delete matches; insert one row.
   This handles the bridge case (new interval connecting two existing rows →
   three collapse to one) by construction. ~10 lines; unit-test it hard:
   contiguous hourly appends, out-of-order parallel backfill completion,
   exact-adjacency, full containment.
3. Full-refresh assets: upsert the single NULL-interval row, bumping
   `materialized_at`.

### Housekeeping

- River periodic job: prune raw `renart_materializations` older than a retention
  window (default 90 days). Coverage is the durable summary; the log is history.
- Lazy GC: coverage rows for fingerprints no longer reachable from any current or
  snapshot DAG can be deleted opportunistically — but **don't** do this in v1; keep
  them, they're tiny and they enable cross-version reuse later.

**Exit criteria:** runs (manual and scheduled) write facts + merged coverage;
a year-of-hourly-runs simulation test shows coverage stays at 1 row for a gapless
asset and lookup time is flat.

---

## Phase 3 — Staleness service and UI (3–4 days)

**Goal:** live badges in the build view, push-updated, correct across selection
switches, with zero per-render queries.

### Service

In-memory map `map[AssetID]Status` for the **current selection**
(env, time range, vars). Recompute triggers:

- **Selection change** → full recompute: one batched coverage query
  (`WHERE asset_id IN (...) AND environment = ? AND vars_hash = ?` — fetch all
  fingerprint rows for those keys, filter to current fingerprints in Go), then
  per-asset coverage check of the selected range against the (already merged)
  intervals in memory.
- **`AssetSaved` event** → `fingerprint.Invalidate`, recompute statuses for the
  downstream cone only.
- **`RunCompleted` event** → flip the specific assets the run touched.

Status enum (the UI distinctions that matter):

| Status | Meaning |
|---|---|
| `fresh` | coverage exists for current fp + vars + range |
| `stale_edited` | this asset's own definition changed since last build |
| `stale_upstream` | inherited via Merkle cascade |
| `partial` | incremental: some intervals covered (report built/total) |
| `never_built` | no row for this asset in this env at all (any fingerprint) |
| `missing` | log says fresh, async verification couldn't find the table |

`stale_edited` vs `stale_upstream`: when a fingerprint mismatches, compare the
asset's *own-content* sub-hash (store it alongside the full fp in the engine's
Result) — if own-content matches but full fp doesn't, it's upstream.

### Verification (trust-but-verify)

When a selection is first opened in a session, fire one async
`information_schema` query per connection listing the relevant schemas; diff
against assets currently marked fresh; downgrade misses to `missing`. Throttle to
once per (connection, session); never block the UI on it.

### UI

Badges on canvas nodes + asset list; `partial` shows "27/30 days"; the
**Build stale** action compiles the stale set into a plan — for incrementals,
exactly the uncovered gap intervals (this is the planning-mode integration; reuse
its preview UI). Unsaved editor buffers get a separate, purely frontend "modified"
dot — the service only ever sees saved state.

**Exit criteria:** switching env/range/vars updates badges in <50 ms with no
spinner; editing an asset flips it and its cone instantly; toggling back to a
previously built env shows fresh (the bug the old reset-flags idea would have had).

---

## Phase 4 — Snapshot store and deploy (3–4 days)

**Goal:** scheduled runs execute immutable deployed versions; build mode keeps
running the working tree; every run is attributable to exact code.

### Schema

```sql
CREATE TABLE renart_blobs (
  hash    TEXT PRIMARY KEY,   -- content hash of file bytes
  content BLOB NOT NULL
);
CREATE TABLE renart_snapshots (
  version_id  TEXT PRIMARY KEY,  -- uuid
  pipeline_id TEXT NOT NULL,
  merkle_root TEXT NOT NULL,
  manifest    TEXT NOT NULL,     -- JSON: relpath -> blob hash, + pipeline config
  git_sha     TEXT,
  git_dirty   INTEGER,
  created_at  TEXT NOT NULL,
  created_by  TEXT
);
```

Snapshot **source files**, not rendered SQL — rendering depends on per-run
env/vars/interval, so the executor renders at run time from snapshot content
exactly as it does from the working tree.

### Deploy action

"Deploy" button (and `renart deploy <pipeline>`): walk pipeline files → insert
missing blobs → write snapshot row (dedupe: if merkle_root equals the latest
snapshot's, no-op with a message). Record git SHA + dirty flag when in a repo.

### Executor resolution

One seam: the executor takes a `fs.FS`. Build mode passes the working tree;
scheduled runs pass a snapshot FS. Simplest correct v1: materialize the snapshot
to a temp dir per run and pass that (files are KB-scale; extraction is
microseconds); a virtual `fs.FS` over the blob table is a clean later swap if
Bruin's loaders accept it. Run records gain `snapshot_version_id` (NULL for
working-tree builds).

### Drift UI

Compare working-tree merkle root vs latest deployed per pipeline; badge
("differs from deployed v12 — 3 assets"), per-asset diff view (blob content vs
file), one-click redeploy.

**Exit criteria:** edit a scheduled pipeline mid-interval → the scheduled run
still executes the deployed version; run detail view shows the exact deployed
code; redeploy picks up changes.

---

## Phase 5 — Per-environment schedules (2–3 days)

**Goal:** schedule identity is `(pipeline, environment)`; no implicit default env;
reconciler and River wiring updated.

### Schema

```sql
CREATE TABLE renart_schedules (
  pipeline_id         TEXT NOT NULL,
  environment         TEXT NOT NULL,
  snapshot_version_id TEXT NOT NULL REFERENCES renart_snapshots(version_id),
  cron                TEXT NOT NULL,
  vars                TEXT,                    -- JSON, env-specific overrides
  catchup_policy      TEXT NOT NULL DEFAULT 'skip',  -- skip | run_once | backfill
  status              TEXT NOT NULL DEFAULT 'active',-- active|paused|archived|delegated
  next_run_at         TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  PRIMARY KEY (pipeline_id, environment)
);
```

### Work items

1. **Migration:** existing schedules get an explicit environment (their current
   effective one); the implicit "default" path is removed. Enabling a schedule in
   the UI/CLI now *requires* picking an env — and requires a deployed snapshot
   (offer "deploy now" inline if none exists).
2. **Reconciler:** diff over the compound key. File deleted / branch switched →
   `archived` tombstone + River handle removed; file reappears (same pipeline_id)
   → reactivate. Run history is keyed by pipeline_id + environment and never
   touched.
3. **River wiring:** on startup, register dynamic periodic jobs for all `active`
   rows; Add/Remove handles on reconcile. Job args carry
   (pipeline_id, environment, snapshot_version_id, scheduled interval); unique-job
   key = (pipeline_id, environment, interval) so restarts, leader churn, and
   catch-up can never double-enqueue a logical run.
4. **Catch-up:** on startup/wake, compute missed intervals per active row and apply
   its policy. `backfill` only legal for pipelines whose assets are interval-aware;
   validate at schedule creation.
5. **UI:** schedules panel grouped by pipeline with one row per env (cron, last
   run, next run, version, toggle); archived section showing tombstones + history.

**Exit criteria:** same pipeline scheduled hourly in prod and daily in staging,
independently toggleable; branch switch archives and restores schedules without
losing history; laptop-closed-overnight behaves per policy.

---

## Phase 6 — Protected environments (1–2 days)

**Goal:** policy flags per environment, enforced at one chokepoint, mirrored in UI.

### Config

```yaml
environments:
  prod:
    protected: true            # no interactive build-mode execution
    deployed_only: true        # only snapshot versions may execute
    confirm_destructive: true  # full refresh / backfill / drop need typed confirm
```

### Work items

1. **Single enforcement point:** a `policy.Check(envPolicy, runRequest) error` in
   the run-dispatch layer that *every* execution path goes through — UI build, CLI,
   scheduler. Scheduler runs pass trivially (they're snapshot-based, non-interactive).
   Scattered UI-side checks are not enforcement; they're hints.
2. **UI mirrors policy:** build/run controls disabled with explanatory tooltips in
   protected envs; destructive actions open a type-the-env-name confirm dialog;
   env selector renders protected envs with a distinct (red) treatment so "you are
   looking at prod" is never ambiguous.
3. **CLI parity:** same errors, `--i-know-what-i-am-doing` deliberately absent for
   `protected` (an accident-prevention flag with an override isn't one); destructive
   confirm prompts on TTY.
4. **Name the limits in docs:** locally these are guardrails (the user owns the
   credentials); the enforced version is the cloud permission model where prod
   credentials only decrypt for the scheduler identity. Keep flag names/semantics
   identical so the cloud later enforces the same config harder rather than
   introducing a second vocabulary.

**Exit criteria:** with prod protected, no interactive path (UI or CLI) can execute
against prod; scheduled prod runs unaffected; staging unaffected.

---

## Phase 7 — Python fingerprint hardening (3–5 days, schedule when it itches)

1. **Rust→wasm32 module** (`renart-pyfp`): depends on `ruff_python_parser` +
   `ruff_python_trivia`. Exports one function:
   `analyze(source) -> { normalized_hash, imports: [module paths] }`
   (strip comments/docstrings via CommentRanges, normalize trailing whitespace,
   hash; collect `import`/`from` targets from the AST). Run under wazero with the
   existing disk compilation cache. Pin the crate versions; the wasm binary's own
   hash feeds into `fp_version` for Python assets so upgrading the module
   invalidates correctly.
2. **Import resolution, one level:** map import targets to project-local files;
   hash those files into the fingerprint. Anything under the shared directory stays
   covered by the coarse `shared_dir_hash` (keep it — it's the safety net).
3. **Runtime-observed variables:** Bruin's Python runner records which injected
   vars/env vars the asset read; store the consumed set keyed by code fingerprint;
   staleness uses the recorded set (safe: a code change changes the fingerprint and
   invalidates regardless, so a stale observation can never under-invalidate).
   First run of a new fingerprint assumes all vars.

---

## Cross-cutting

- **Feature flags per phase** (env var or settings toggle) so half-built phases can
  merge to main without blocking releases — you're solo, but you're also your own
  prod user.
- **Fingerprint stability is the load-bearing wall.** Any nondeterminism (map
  iteration order, locale-dependent formatting, parser version drift) corrupts
  everything above it. Sort all inputs before hashing; add a CI job that
  fingerprints a fixture project and compares against committed goldens on every
  build.
- **SQLite contention:** run-completion writes (facts + coverage merge) and
  staleness reads share the DB with River. WAL mode + a single writer goroutine for
  renart tables avoids `SQLITE_BUSY` surprises; keep the coverage merge transaction
  short.
- **Known-accepted limitations to write down:** asset rename orphans history
  (Phase 0); SQL canonicalization is conservative (formatting-insensitive only);
  Python v1 over-invalidates on comment edits until Phase 7; coverage rows for
  abandoned fingerprints accumulate slowly (harmless, GC later).
- **What this sets up for later (do not build now):** cross-environment physical
  reuse à la SQLMesh (fingerprints already identify materializations), breaking vs
  non-breaking change analysis via column lineage, notebook-cell caching (same
  fingerprint engine), and cloud schedules (`delegated` status already reserved).
