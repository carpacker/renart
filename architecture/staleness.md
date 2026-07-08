# Staleness stack — fingerprints, facts, snapshots, schedules, protection

Status: current state (built on the `redesign` branch, June 2026). Six
interlocking subsystems that together answer "what is built, what is stale,
what runs on a schedule, and what is protected".

```
identity + bus ──► fingerprints ──► facts/coverage ──► staleness service/UI
       └────────► snapshots/deploy ──► per-env schedules ──► protection
```

## 1. Identity and events

- Pipelines self-assign an `id: <uuid>` in `pipeline.yml` on first load
  (`internal/web/identity`). Asset identity is `pipeline_uuid:asset_name` —
  renaming an asset orphans its history (accepted; a content-hash rename
  heuristic can be added later if it hurts).
- One event bus (`internal/web/bus`) emits `RunCompleted` and `AssetSaved`;
  the fact recorder and staleness service attach here.
- All durable tables live in the scheduler's SQLite DB (`.renart/state.db`)
  under the `renart_` prefix, migrated by the goose runner (goose's version
  table is the schema-version ledger). WAL + `busy_timeout=5000`.

## 2. Fingerprints (`internal/web/fingerprint`, `renart fp`)

Deterministic, versioned (`v1:` prefix) content identity per asset:

```
SQL:    H(fp_version ‖ canonical_sql ‖ config_hash ‖ consumed_vars_hash ‖ sorted(upstream_fps))
Python: H(fp_version ‖ file_bytes ‖ lockfile_hash ‖ shared_dir_hash ‖ config_hash ‖ upstream_fps)
```

- **SQL canonicalization runs through the embedded wasm formatter**: comments
  stripped, statement formatted per-asset-dialect (`internal/web/sqlformat`),
  whitespace collapsed — format-on-save, keyword-case edits, and trailing
  commas never change a fingerprint; identifier case stays significant. A
  format call costs ~66 ms, so results are cached by content hash (content
  only ever formats one way; the cache cannot go stale): cold DAG on 20 assets
  ≈ 1.3 s, warm ≈ 0.4 ms. The server pre-warms all pipelines at startup.
  Statements the formatter can't parse (e.g. Jinja in identifier position)
  fall back to the stripped canonical form, deterministically.
- **Consumed variables are detected textually** (`var.NAME` references
  intersected with declared variables), not by instrumenting the Jinja
  renderer. Over-approximates → over-invalidates, the safe direction.
- **Python hashes raw bytes** + nearest lockfile (`uv.lock`,
  `requirements.txt`, `pyproject.toml`) + the shared dir, and assumes all
  variables consumed. Comment-only edits over-invalidate until the deferred
  hardening lands (§8).
- **Escape hatches live under `meta:`** (`meta.fingerprint_version` replaces
  the content hash; `meta.depends_on_files` globs get hashed in) because
  Bruin's asset schema has no free top-level keys.
- **The engine recomputes on every call** — full recompute is O(pipeline)
  cheap hashing and cannot go stale; only disk-derived inputs are cached,
  validated by stat. `Engine.Invalidate` exists as API.
- **Stability guard:** `fingerprint/golden_test.go` fingerprints the committed
  fixture project against `testdata/fixture-golden.json` on every test run.
  Intentional algorithm changes bump `fingerprint.Version` and regenerate with
  `-update-golden`; everything goes stale once — correct and self-healing.

## 3. Materialization facts and coverage (`internal/web/matlog`)

Every run writes an immutable fact row (asset, environment, fingerprint,
vars_hash, optional interval, run_id, timestamp); a compacted coverage table
merges overlapping/adjacent intervals into one row per contiguous range, so
freshness lookups are O(gaps). Full-refresh assets hold a single "built"
marker row (`interval_start = NULL`). `IntervalAware(asset)` (time_interval,
delete+insert, append, or any asset with an `incremental_key`) decides whether
runs stamp their window. A daily River job prunes raw facts (default 90 days);
coverage is the durable summary.

Notes: `run_id` is empty for build-mode runs (no run record); scheduled runs
carry theirs. The recorder fingerprints at run *completion*, so an edit saved
mid-build-mode-run records the newer fingerprint (scheduled runs are immune —
they fingerprint the executed snapshot). Partially failed pipeline runs record
no facts for the assets that did succeed; they read as stale and a rebuild
repairs it.

**Last run attempt.** Facts only capture successes, so the recorder also upserts
`renart_asset_runs` — one row per `(asset, environment)` with the target
fingerprint, `succeeded|failed`, and timestamp (a later run overwrites it, so a
success clears a prior failure). Interactive materialize emits a failed
`RunCompleted` on error (`MaterializeAssetStream`) so the failure is persisted;
the pipeline-run path still records nothing on failure (accepted, as above).

## 4. Staleness service and UI (`internal/web/staleness`)

In-memory status map per current selection (env, range, vars), exposed at
`/api/pipelines/{id}/staleness` and pushed over SSE. Recompute triggers:
selection change (batched coverage query), `AssetSaved` (invalidate + recompute
the downstream cone), `RunCompleted` (flip the touched assets).

| Status | Meaning |
|---|---|
| `fresh` | coverage exists for current fp + vars + range |
| `stale_edited` | own definition changed since last build (own-content sub-hash mismatch) |
| `stale_upstream` | inherited via the Merkle cascade — also covers variable-value changes (own-content matches, full fp doesn't) |
| `partial` | incremental: some intervals covered (built/total surfaced as covered/total seconds) |
| `never_built` | no row for this asset in this env at any fingerprint |
| `missing` | log says fresh, async verification couldn't find the table |

The `missing` downgrade only applies to assets whose output is a warehouse
object named after the asset (`verifiableByName`: SQL, seed). Load (sling) and
python assets write to arbitrary destinations — a local file, a
`destination_table` that doesn't match the asset name, or nothing — so the
name-based lookup would always report them missing; they are skipped and rest on
the run fact alone.

Unsaved editor buffers get a purely-frontend "modified" dot; the service only
sees saved state.

Each `AssetStatus` also carries the last run attempt (`last_run_status`,
`last_run_at`, `last_run_on_current_content` — the latter true when the run's
fingerprint matches the asset's current one) from `renart_asset_runs`,
orthogonal to the base `status`. The frontend composes them
(`resolveFreshnessDisplay`): base ∈ {`stale_edited`,`never_built`} + a failed run
on the current content → **Build failed**; `fresh` + a failed run on the current
content → **Run failed** (unchanged code, latest run failed); otherwise the base
label. This distinguishes an untested edit from an edit that was run and failed.

The Build-stale action is server-side: `POST
/api/pipelines/{id}/build-stale/stream` (`httpapi/build_stale.go`) recomputes
the stale set for the selection, compiles it into a plan (every non-fresh
asset; for partials exactly the uncovered gap intervals), and
`ExecutionService.MaterializeStaleAssetsStream` builds it in topological order
as one SSE-streamed run — one combined log, per-asset `asset` progress events,
one `RunCompleted` bus emit per built window (so coverage and achieved
fingerprints reflect exactly what ran, and downstreams built later in the same
plan already see the fresh upstream fingerprints). Assets downstream of a
failed plan member are skipped rather than built stale.

## 5. Snapshots and deploy (`internal/web/snapshot`, `renart deploy`)

Content-addressed store in SQLite: `renart_blobs` (hash → file bytes) +
`renart_snapshots` (version, pipeline, merkle root, manifest JSON, git
SHA/dirty). Snapshots hold **source files, not rendered SQL** — rendering
depends on per-run env/vars/interval, so the executor renders at run time from
snapshot content exactly as from the working tree. Scheduled runs materialize
the snapshot **to a temp dir outside the workspace** (so pipeline discovery
doesn't pick it up) with a `ConfigPath` override on the executor. Deploy
dedupes on identical merkle root. Drift between working tree and the latest
deployed version is surfaced per pipeline (`/api/pipelines/{id}/deploy/status`,
per-file view via `/api/snapshots/{versionId}/file`).

## 6. Per-environment schedules (`renart_schedules`, `/api/env-schedules`)

Schedule identity is `(pipeline UUID, environment)` — no implicit default env.
Each row carries the pinned snapshot version, cron, env-specific vars, a
catch-up policy (`skip | run_once | backfill`), and a status
(`active | paused | archived | delegated` — `delegated` is reserved for cloud).
The reconciler diffs over the compound key: file deleted / branch switched →
`archived` tombstone (reason `missing`) with the River handle removed; the
pipeline reappearing reactivates reconciler tombstones but **not** explicit
user deletions (reason `user`), which stay archived until restored in the UI.
River job uniqueness is keyed on (pipeline, environment, interval) so restarts
and catch-up can never double-enqueue a logical run.

## 7. Protected environments (`internal/web/policy`)

Per-environment flags in `.renart/environments.yml` (kept out of `.bruin.yml`
so Bruin's own config parsing is never at risk):

```yaml
environments:
  prod:
    protected: true            # no interactive build-mode execution
    deployed_only: true        # only snapshot versions may execute
    confirm_destructive: true  # full refresh / backfill / drop need typed confirm
```

Enforced by `policy.Check` at the single execution-service dispatch chokepoint
that every path (UI build, CLI, scheduler) goes through; scheduler runs pass
trivially (snapshot-based, non-interactive). UI-side disabling is a hint, not
enforcement. `confirm_destructive` is enforced but not yet exercised — renart
has no full-refresh/backfill execution path yet (see
`plans/materialization-strategies.md`). Locally these are guardrails, not a
boundary (the user owns the credentials); the cloud permission model enforces
the same flags harder, under the same names.

## 8. Deferred and known-accepted

- **Python fingerprint hardening** (Rust→wasm `renart-pyfp` on
  `ruff_python_parser`: comment/docstring-stripped hash, one-level import
  resolution, runtime-observed variables; the wasm binary's own hash feeds
  `fp_version`) — deferred until raw-byte over-invalidation itches.
- Coverage rows for abandoned fingerprints accumulate slowly; harmless, GC
  later (they enable cross-version reuse).
- Set up for later, deliberately not built: cross-environment physical reuse à
  la SQLMesh, breaking vs non-breaking change analysis via column lineage,
  notebook-cell caching on the same engine, cloud schedules (`delegated`).

## Key files

`internal/web/identity`, `internal/web/bus`, `internal/web/fingerprint`
(+ `golden_test.go`), `internal/web/matlog/{recorder,store}.go`,
`internal/web/staleness`, `internal/web/snapshot`, `internal/web/policy`,
`internal/web/scheduler`, migrations under the scheduler store.
