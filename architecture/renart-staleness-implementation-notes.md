# Staleness implementation notes

Status of `renart-staleness-implementation-plan.md` as built on the
`redesign` branch (June 2026). One commit per phase; no feature flags (the
whole stack landed on the feature branch).

## What landed where

| Phase | Code | Commit theme |
|---|---|---|
| 0 — identity & events | `internal/web/identity`, `internal/web/bus` | pipelines self-assign `id:` UUIDs; one bus emits `RunCompleted`/`AssetSaved` |
| 1 — fingerprints | `internal/web/fingerprint`, `renart fp` | canonical SQL + config + consumed-vars + Merkle upstreams, `v1:` prefixed |
| 2 — facts & coverage | `internal/web/matlog`, migration 00002/00003 | immutable fact log + merged-interval coverage, daily River pruning |
| 3 — staleness | `internal/web/staleness`, `/api/pipelines/{id}/staleness` | per-selection status map, SSE push, badges + Build-stale UI |
| 4 — snapshots | `internal/web/snapshot`, `renart deploy`, `/api/pipelines/{id}/deploy*` | content-addressed blobs, scheduled runs execute snapshots from a temp dir |
| 5 — env schedules | `renart_schedules` (migration 00005), `/api/env-schedules` | identity = (pipeline UUID, environment); tombstones; catch-up policies |
| 6 — protection | `internal/web/policy`, `.renart/environments.yml` | `policy.Check` at the execution-service dispatch chokepoint |

All durable tables live in the scheduler's SQLite database
(`.renart/state.db`) under the `renart_` prefix, migrated by the existing
goose runner (goose's version table is the schema-version ledger the plan
called `renart_schema_version`). WAL mode and `busy_timeout=5000` were
already set; the coverage merge transaction is a handful of point queries.

## Deliberate deviations from the plan

- **SQL canonicalization runs through the embedded wasm formatter (v2).**
  Comments are stripped, the statement is formatted
  (`internal/web/sqlformat`, per-asset dialect), and whitespace collapsed —
  so format-on-save, keyword-case edits, and trailing commas never change a
  fingerprint, while identifier case stays significant. A single format
  call costs ~66 ms, so results are cached by content hash (a content only
  ever formats one way; the cache cannot go stale): cold DAG on 20 assets
  ≈ 1.3 s, warm ≈ 0.4 ms. The server pre-warms the cache for all pipelines
  at startup so the first staleness fetch does not pay the cold cost; a
  save re-formats only the edited asset. Statements the formatter cannot
  parse (e.g. Jinja in identifier position) fall back to the stripped
  canonical form, deterministically. Python stays raw-byte hashed: safe
  normalization needs a parser (CRLF/whitespace inside string literals is
  semantic), which is exactly the Phase 7 ruff-wasm work.
- **Consumed variables are detected textually**, not by instrumenting the
  Jinja renderer: `var.NAME` references in the asset content, intersected
  with declared variables. Over-approximates (a name in a dead template
  branch still counts), which over-invalidates — the safe direction.
- **Escape hatches live under `meta:`** (`meta.fingerprint_version`,
  `meta.depends_on_files`) because Bruin's asset schema has no free
  top-level keys.
- **The fingerprint engine recomputes on every call** instead of caching
  DAG results per selection; only disk-derived inputs (lockfiles, shared
  dir, pinned files) are cached, validated by stat. Full recompute is
  O(pipeline) cheap hashing and cannot go stale; `Engine.Invalidate` keeps
  the plan's API.
- **Snapshots materialize to a temp dir outside the workspace** with a
  `ConfigPath` override on the executor, rather than under the workspace
  (where pipeline discovery would pick them up as workspace pipelines).
- **`run_id` on facts is empty for build-mode runs** (they have no run
  record); scheduler runs carry their run ID through
  `MaterializePipelineRun`.
- **User-archived schedules are not auto-restored.** The plan's
  "reappears → reactivate" applies to reconciler tombstones
  (`archived_reason = missing`, e.g. branch switches); explicit deletions
  (`archived_reason = user`) stay archived until restored in the UI.
- **`stale_upstream` also covers variable-value changes**: own-content
  matches but the full fingerprint (which includes the consumed-vars hash)
  does not. A distinct `stale_vars` state can be split out later if the
  conflation hurts.
- **Environment policies live in `.renart/environments.yml`**, not inside
  `.bruin.yml`'s `environments:` map, so Bruin's own config parsing is
  never at risk. Same flag names/semantics as the plan.
- **`confirm_destructive` is enforced by `policy.Check` but not yet
  surfaced**: renart currently has no full-refresh/backfill/drop execution
  path that takes a confirmation. The hook exists for when one appears.

## Known-accepted limitations (per plan, plus discoveries)

- Renaming an asset orphans its history (`asset_id = pipeline_uuid:name`).
- SQL canonicalization is formatting-insensitive only — no identifier case
  folding across dialects.
- Python fingerprints hash raw file bytes: comment-only edits
  over-invalidate until Phase 7.
- Coverage rows for abandoned fingerprints accumulate slowly; harmless,
  GC later (they enable cross-version reuse).
- Partially failed pipeline runs record no materialization facts for the
  assets that did succeed; they read as stale and a rebuild repairs it.
- The materialization recorder fingerprints at run *completion*: an edit
  saved mid-build-mode-run records the newer fingerprint. Scheduled runs
  are immune (fingerprinted from the executed snapshot's content).
- Local protection is a guardrail, not a boundary — the user owns the
  credentials. The cloud permission model (prod credentials decrypt only
  for the scheduler identity) enforces the same flags harder.

## Fingerprint stability guard

`internal/web/fingerprint/golden_test.go` fingerprints the committed
fixture project (`testdata/fixture`) and compares against
`testdata/fixture-golden.json` on every `go test` run (so in CI). If the
algorithm changes intentionally: bump `fingerprint.Version`, regenerate
with `go test ./internal/web/fingerprint/ -run TestFixtureProjectGolden
-update-golden`, and everything goes stale once — correct and self-healing.

## Phase 7 — deferred ("schedule when it itches")

Python fingerprint hardening (the `renart-pyfp` Rust→wasm module on
`ruff_python_parser`, one-level import resolution, runtime-observed
variables) is not built. Until then Python assets hash raw bytes + nearest
lockfile + `shared/` directory and assume all variables are consumed.
When it lands, the wasm binary's own hash feeds into `fp_version` for
Python assets so upgrading the module invalidates correctly.

## Set up for later (not built, by design)

Cross-environment physical reuse à la SQLMesh (fingerprints already
identify materializations), breaking vs non-breaking change analysis via
column lineage, notebook-cell caching on the same engine, and cloud
schedules (the `delegated` schedule status is reserved).
