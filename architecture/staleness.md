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
freshness lookups are O(gaps). Assets without a real execution-window contract
hold a single "built" marker row (`interval_start = NULL`). SQL
`time_interval` and API requests that reference Renart's start/end variables
stamp their run window. Replay-safe API merge (with a primary key) and SQL
`time_interval` union independent windows; other windowed API writes replace
their prior coverage with the latest window so replace/append modes cannot
claim data they may no longer contain. Load's Sling max-key state is not a
Renart run window, and dormant `incremental_key` metadata never makes an asset
interval-aware. `BackfillSafe` is the narrower union-safe contract used by the
scheduler before enabling catch-up and returned with each asset's staleness
status. The editor uses that same backend fact for its explicit Backfill range
action; the execution endpoint requires a complete UTC range and revalidates the
asset before dispatch. A daily River job prunes raw facts (default
90 days); coverage is the durable summary.

A full refresh remains paired with its requested run window. For an
interval-aware asset it replaces prior interval coverage with that window; it
does not create a universal built marker for a query that may be window-filtered.
For a non-windowed table it replaces the marker. Asset-level and selected-
environment refresh restrictions run configured strategies and therefore keep
normal union/marker behavior.

Notes: `run_id` is empty for build-mode runs (no run record); scheduled runs
carry theirs. A partial unique index keeps one fact per
`(asset, environment, scheduled run)` while deliberately allowing repeated
empty build-mode IDs; recorder inserts are no-ops when crash recovery replays a
fact that already committed. The recorder fingerprints at run _completion_, so an edit saved
mid-build-mode-run records the newer fingerprint (scheduled runs are immune —
they fingerprint the executed snapshot). Pipeline execution collects terminal
asset events even when the overall run fails: completed assets record success
facts, the failing asset records a failed attempt, and assets the executor never
reached record nothing.

**Last run attempt.** Facts only capture successes, so the recorder also upserts
`renart_asset_runs` — one row per `(asset, environment)` with the target
fingerprint, `succeeded|failed|cancelled`, and timestamp (a later run overwrites
it, so a success clears a prior failure). The upsert is monotonic by timestamp:
an older or equal-time recovery event cannot replace a newer attempt.
Interactive, stale-plan, and full
pipeline materialization all emit `RunCompleted` for the assets they actually
attempted, including terminal failures.

Bruin JSON run logs under `logs/runs/` are diagnostic artifacts, not application
state. Freshness, materialization timestamps, and latest attempts are restored
from `.renart/state.db`; transient running state comes from scheduler steps and
SSE. In particular, a terminal run-log snapshot may still call an untouched
asset `pending`, and Renart never persists that value as asset state.

After an unclean server stop, scheduler startup first marks the orphaned run and
every open step failed. It then re-emits only the persisted terminal steps
through the same synchronous `RunCompleted` bus: prior successes remain
successes, the interrupted step is failed, and unreached assets remain absent.
The run row stores the requested execution modes at admission, then atomically
replaces them with the effective environment, window, full-refresh/backfill
mode, and sensor mode immediately before the first asset starts. If that write
fails, execution does not begin. Recovery therefore applies the same coverage
replacement semantics as the interrupted executor, including environment-level
full-refresh restrictions and default windows. Rows interrupted by a legacy
build before that effective-context write existed remain explicitly unresolved:
their River arguments are request diagnostics only, so startup acknowledges and
counts the skipped replay without emitting materialization facts. Those assets
remain stale rather than risking false coverage from an inferred environment,
window, or refresh mode.
New scheduler-backed admissions also persist a private versioned RunSpec. For a
spec-backed run, recovery never overwrites requested modes from empty or
conflicting River arguments; the spec remains authoritative while fact replay
continues to use only the persisted effective execution context. New manual
run/spec/job/link admission is atomic. River-argument link and mode recovery is
retained only for pre-upgrade jobs, and an unknown or structurally incompatible
spec fails closed rather than falling back to legacy semantics. The spec's
stable pipeline UUID is independently bound to the durable UUID run slot before
execution and travels through scheduler execution into snapshot resolution. A
pipeline path rename therefore cannot redirect a queued snapshot through a
newly resolved identity.
For a deployed run it materializes the run's exact pinned snapshot while the
recorder fingerprints it, then deletes the temp directory. This is derived-state
recovery only—asset code and textual logs are never replayed. The fact and
latest-attempt writes above make the path safe if the original completion event
committed immediately before the process died. A durable pending flag is cleared
only after replay returns, so another stop during startup retries safely; its
migration also queues interrupted runs reconciled by older builds for one-time
backfill.

The workspace scheduler lock is acquired before any River worker starts, so a
River job still marked `running` at that point belongs to the stopped process.
Recovery cancels admitted pipeline and housekeeping jobs in the same SQLite
transaction that closes Renart's run records. A queued run remains queued only
when it still has an available, pending, retryable, or scheduled River job;
terminal-linked and truly jobless queued rows fail and release their active
slot. Pre-upgrade runnable jobs are relinked from their run ID. A claimed
scheduled compatibility signal with no admitted run is instead returned to
River with its exact arguments and interval intact. Startup writes a structured
summary with reconciled-run, cancelled-job, requeued-signal, replay, and
replay-failure counts; cancelled queue rows retain the interruption as an
attempt error.

Before the unique run-slot migration is applied, a legacy database can contain
multiple queued/running rows for one pipeline path because old admission was
not atomic. Migration keeps one deterministic queued-first survivor, marks the
other rows failed, closes their open steps, and writes the recovery reason to
their logs. The survivor then enters normal startup recovery and receives the
legacy path-only slot; old rows did not retain the stable UUID needed to
reconstruct a UUID alias.

## 4. Staleness service and UI (`internal/web/staleness`)

In-memory status map per current selection (env, range, vars), exposed at
`/api/pipelines/{id}/staleness` and pushed over SSE. The frontend tracks loading
and failures per pipeline for the exact selection. A matching SSE snapshot is
authoritative for that pipeline: it resolves that request/error and prevents an
older in-flight HTTP response from replacing the pushed state, without hiding
unresolved sibling pipelines. Recompute triggers: selection change (batched
coverage query), `AssetSaved` (invalidate + recompute the downstream cone),
`RunCompleted` (flip the touched assets).

| Status           | Meaning                                                                                                      |
| ---------------- | ------------------------------------------------------------------------------------------------------------ |
| `fresh`          | coverage exists for current fp + vars + range                                                                |
| `stale_edited`   | own definition changed since last build (own-content sub-hash mismatch)                                      |
| `stale_upstream` | inherited via the Merkle cascade — also covers variable-value changes (own-content matches, full fp doesn't) |
| `partial`        | incremental: some intervals covered (built/total surfaced as covered/total seconds)                          |
| `never_built`    | no row for this asset in this env at any fingerprint                                                         |
| `missing`        | materialization history says fresh, async verification couldn't find the table                               |
| `volatile`       | sensor check has no durable output coverage and must run again in every stale plan                           |

The `missing` downgrade only applies to assets whose output is a warehouse
object named after the asset (`verifiableByName`: SQL, seed, and database-backed
Load). Local-, file-, and object-storage-backed Load assets use an explicit
`destination_object`, while Python assets may return nothing or write elsewhere;
those outputs cannot be verified by the asset name and therefore rest on the run
fact alone.

Sensors are deliberately classified as `volatile` before and after a successful
check. Their last attempt is still recorded and displayed, but they are excluded
from warehouse-object verification and never become fresh from a run fact. The
Build-stale planner therefore includes them on every requested build; interactive
execution performs one check, while scheduled execution waits according to the
sensor's configured interval and timeout.

Unsaved editor buffers get a purely-frontend "modified" dot; the service only
sees saved state.

Each `AssetStatus` also carries the last run attempt (`last_run_status`,
`last_run_at`, `last_run_on_current_content` — the latter true when the run's
fingerprint matches the asset's current one) from `renart_asset_runs`,
orthogonal to the base `status`. The frontend renders both dimensions instead
of allowing one to replace the other: for example, unchanged built content can
show **Fresh** + **Last run failed**, while edited or never-built content whose
current version failed shows its base **Edited**/**Never built** badge + **Build
failed**. A cancelled attempt is likewise separate. This preserves the answer
to "can I use the existing data?" while also answering "what happened when I
last tried to build it?" and distinguishes an untested edit from one that was
run and failed.

Running state is transient and asset-scoped. The UI derives it only from
scheduler steps (initial active-run hydration plus `run.step` SSE events); a
queued or started pipeline does not mark every asset pending. `run.finished`
clears all transient entries for that run, while the canonical terminal attempt
arrives through staleness. Assets skipped after an upstream failure therefore
retain their previous freshness and attempt state. Build remembers a terminal
event even when a very fast run finishes before the trigger response supplies
its run ID, then associates the result and reloads the canonical stored log.
Late queued/running events or active-run hydration cannot resurrect that
finished run.

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
The endpoint's optional `upstream_of` selector narrows that same plan to one
asset's transitive upstream closure. `renart run <asset> --refresh-upstreams`
uses this selector in delegated mode (and the same planner directly in embedded
mode), then starts the requested asset only if the upstream plan succeeds.

## 5. Snapshots and deploy (`internal/web/snapshot`, `renart deploy`)

Content-addressed store in SQLite: `renart_blobs` (hash → file bytes) +
`renart_snapshots` (version, pipeline, merkle root, manifest JSON, git
SHA/dirty). Snapshots hold **source files, not rendered SQL** — rendering
depends on per-run env/vars/interval, so the executor renders at run time from
snapshot content exactly as from the working tree. Every selected snapshot is
an exact version ID owned by the target pipeline. Admission and execution
validate canonical manifest paths, blob presence, and content hashes; a
missing, wrong-pipeline, or corrupt deployment fails closed. Snapshot runs
materialize into a fresh temp directory **outside the workspace** (so pipeline
discovery doesn't pick it up) with a `ConfigPath` override on the executor.
Ordinary Build runs explicitly stay on the saved working tree even after a
deployment exists, while a `deployed_only` environment resolves the latest
deployment to an exact ID before enqueue. Deploy dedupes on identical merkle
root. Drift between working tree and the latest deployed version is surfaced
per pipeline (`/api/pipelines/{id}/deploy/status`, per-file view via
`/api/snapshots/{versionId}/file`); the status also reports whether the latest
snapshot is executable so identical-but-corrupt content can be repaired by a
new Deploy instead of dead-ending the UI.

## 6. Per-environment schedules (`renart_schedules`, `/api/env-schedules`)

Schedule identity is `(pipeline UUID, environment)` — no implicit default env.
Each row carries the pinned snapshot version, cron, env-specific vars, a
catch-up policy (`skip | run_once | backfill`), and a status
(`active | paused | archived | delegated` — `delegated` is reserved for cloud).
The reconciler diffs over the compound key: file deleted / branch switched →
`archived` tombstone (reason `missing`) with the River handle removed; the
pipeline reappearing reactivates reconciler tombstones but **not** explicit
user deletions (reason `user`), which stay archived until restored in the UI.
River `ByArgs` uniqueness suppresses a duplicate `(pipeline UUID, environment,
interval)` signal while the first job is active; it is not a durable completed-
occurrence ledger. New admissions claim pipeline-global SQLite path and
stable-UUID slot aliases, preventing concurrent scheduler-backed executions
across a rename. Migrated active rows have only a path alias because their UUID
was not persisted, so rename safety cannot be reconstructed for those rows. A
scheduled signal blocked by a slot is snoozed and retried with its original
arguments; no run row or visible deferred occurrence exists until it acquires
the slot.

Only the process holding `.renart/scheduler.lock` may change rows or enqueue
runs. `GET /api/env-schedules` reports `owner`, `follower`, or `unavailable`;
mutations through a follower return `409 scheduler_not_owner` before any
deployment, `pipeline.yml`, or schedule-store write. Followers remain
read-only—automatic takeover and cross-process handoff still belong to the
separate scheduler-coordination workstream.

The schedules UI compares each row's pinned snapshot with the pipeline's latest
deployed version. A differing pin is shown as **Older deployment**, independently
of data freshness and last-run status, with an action that deploys the current
pipeline (deduping identical content) and atomically advances only that
environment's schedule to the resulting snapshot. The row-level manual action
submits that displayed exact pin and remains a manual run, so it cannot advance
the schedule watermark. Rows without a pin show **Needs deployment** instead of
silently running the working tree.
For an actual scheduled tick, the successful run status and its environment-
scoped watermark advance commit in one SQLite transaction. A crash or write
failure therefore leaves the interval retryable instead of recording success
while silently re-enqueueing the same catch-up window later. Watermark
capability and identity come from the server-derived stored RunSpec, never a
client trigger or the mere presence of a run ID.

Existing database rows from the former pinless contract are migrated once:
each non-archived row is pinned to that pipeline's then-latest deployment, or
paused when no deployment exists. Legacy `pipeline.yml` schedules follow the
same rule when first imported. Active rows with an invalid pin or stored
variable overrides are paused during reconciliation; admission rejects new
active rows until both are executable.

## 7. Protected environments (`internal/web/policy`)

Per-environment flags in `.renart/environments.yml` (kept out of `.bruin.yml`
so Bruin's own config parsing is never at risk):

```yaml
environments:
  prod:
    protected: true # no interactive build-mode execution
    deployed_only: true # only snapshot versions may execute
    confirm_destructive: true # full refresh / backfill / drop need typed confirm
```

Enforced by `policy.Check` at execution dispatch; the legacy `/api/run` path and
manual scheduler trigger apply the same check instead of bypassing it. Scheduled
snapshot runs pass as non-interactive. UI-side disabling is a hint, not
enforcement. Full refresh has a destructive confirmation dialog and sends the
typed environment through to the server; the CLI uses
`--confirm-environment`. Explicit backfill requests use the same contract, while
an ordinary selected execution window is not automatically mislabeled as
destructive. Locally these are guardrails, not a boundary (the user owns the
credentials); the cloud permission model enforces the same flags harder, under
the same names.

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
