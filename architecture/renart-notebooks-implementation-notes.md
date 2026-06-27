# Notebook implementation notes

Status of `renart-notebooks-implementation-plan.md` as built on the
`redesign` branch (June 2026). Phases N1–N5 and N7 are implemented; N6 is
partially done (telemetry shipped, auto-run deferred). One commit per phase.

## What landed where

| Phase | Code | Commit theme |
|---|---|---|
| N1 — file format & loader | `internal/web/notebook` (notebook.go, manifest.go, identity.go, validate.go, jinja.go) | folder format, durable IDs, dependency derivation, validation |
| N1/N2 — namespace & isolation | `internal/web/service/notebooks.go`, `workspace.go` | `ComputeState` loads notebooks; every asset carries `class`; pipeline→notebook deps are a state error |
| N3 — sessions & execution | `notebook/session.go`, `run.go`, `dag.go`; `service/notebook_service.go`; `httpapi/notebooks.go` | per-notebook DuckDB, cell runner, import cache, CRUD + run API |
| N4 — rename engine | `notebook/rename.go`, `fingerprint.go` | span-splice references, invariant-1 guard |
| N5 — viz language | `notebook/viz.go`; `components/redesign/notebook-viz*.tsx` | `@viz(...)` grammar + diagnostics + Recharts renderer |
| N7 — promotion | `notebook/promote.go`; service `PromoteCell` | cell → pipeline asset, reference rewrite |

Frontend: `components/redesign/notebook-page.tsx` (live page + index),
`lib/api-notebooks.ts`, route `src/routes/redesign/notebooks/*`, Notebooks
nav entry. The mock `RedesignNotebookPage` in `object-pages.tsx` was removed.

## Core invariants — how each is enforced

1. **No logical name in a fingerprint.** `CellFingerprint` resolves sibling
   names → `cell_<id>` via the identifier splice (never the parser's
   `RenameTables`, which *fails* on Jinja and would leave the logical name
   in), then `fingerprint.CanonicalSQL`. The cell's own name never appears.
   `TestRenameChangesNoFingerprint` (5 referencing cells, incl. Jinja, a
   string literal `'base'`, a self-join, a comment mention) is the permanent
   guard.
2. **Class is first-class; direction policed.** `model.Asset.Class` is
   `pipeline`/`notebook`; `validateAssetClassDirection` makes a pipeline
   asset depending on a notebook cell a state-level error. Catalog/lineage
   read `state.Pipelines` only, so cells never leak in.
3. **Presentation in comments.** `@viz` lines are comments; `CanonicalSQL`
   strips them. `TestVizIsOutsideFingerprint` proves changing the directive
   (kind, stacked, …) moves no fingerprint.
4. **Machine-named objects.** Cells materialize as `cell_<id>`, imports as
   `src_<sanitized ref>`, inside `.renart/notebooks/<uuid>.duckdb`.

## Deliberate deviations from the plan

- **Rename uses a hand-written identifier-splice tokenizer, not the SQL
  parser's `RenameTables`.** Bruin's `RenameTables` re-prints the statement
  (uppercases keywords, expands aliases) — it would destroy user formatting,
  which the plan forbids ("positions only — never AST re-print"). The
  tokenizer (`spliceIdentifiers`) walks code/string/comment/quoted-ident/
  Jinja states and replaces only bare or double-quoted identifier tokens, so
  `'base'`, `-- base`, and `{{ base }}` are left alone while `from base` and
  `base.id` are rewritten. A name preceded by `.` (`schema.base`) is skipped.
  The same splice resolves names for the fingerprint (it never fails on
  templated SQL).
- **Cell identity lives in frontmatter `id:`, the manifest references cells
  by that id.** So rename = rewrite sibling files + move the cell file; the
  manifest needs no edit. `ExecutableFile.Content` is body-only, so the
  rename engine operates on `Cell.Raw` (full file) and the @bruin block (a
  block comment) is skipped automatically.
- **Import fast path is zero-copy `ATTACH`** for DuckDB-backed pipeline
  assets (one batched `attach; ctas; detach` — ATTACH visibility is
  per-connection and the client may use a different connection per call),
  falling back to a row-capped generic `Fetch` through the connection for
  everything else. `SourceFetcher` is the swappable seam for the future
  cloud gateway. Unknown refs (`ErrUnknownSource`) are left untouched so the
  session yields a clear missing-table error rather than a fake import.
- **No facts/coverage integration; staleness is client-side.** Cell runs do
  *not* emit `RunCompleted` into the bus/matlog. The reactive UX (edit marks
  self + descendant cone stale) is implemented in the React page over the
  dependency edges. Honest for the per-session DuckDB model where results
  are ephemeral and re-queried; wiring cells into the staleness service is a
  later option if cross-session notebook staleness is wanted.
- **Promotion warns on dialect mismatch instead of blocking with flagged
  expressions.** Bruin's `sqlparser` exposes no transpiler, so DuckDB→other
  promotion proceeds with an explicit "review the SQL" warning rather than
  silently emitting possibly-wrong SQL. Same-dialect (the common
  DuckDB→DuckDB case) is clean.
- **SQL-only cells (`isCellFile` accepts `.sql`).** Python cells are parked
  behind Phase-7 Python fingerprinting, per the plan.

## Known limitations / not built

- **N6 auto-run** (marimo-style execute-stale-on-save with cancel/timeout/
  suspend-after-failure) is not built; lazy-reactive staleness + run
  telemetry (rows · ms on the cell header) are. 
- **Promote-whole-notebook** (subgraph → new pipeline) is not built; single-
  cell promotion is.
- **Monaco squiggles / completion for `@viz`** are not wired — the parser
  produces spans and the cell card surfaces diagnostics as text, but the
  notebook cell editor is a plain textarea, not Monaco. (The viz settings
  popover is a chart-type toggle row that rewrites the directive line.)
- **Warehouse-backed `notebook_target`** (sandbox schemas + manifest/TTL
  janitor) is not built; the DuckDB-file default with delete-on-close +
  startup sweep is. Protected-env handling falls out for free: a notebook
  reads prod via the import resolver and writes only the local file.
- **Notebook cell editor is a textarea**, not the shared Monaco asset
  editor — no SQL intellisense inside cells yet. Reusing `use-asset-monaco`
  here is the obvious follow-up.

## Sessions & cleanup

`SessionStore` keeps one `.duckdb` file per notebook UUID under
`.renart/notebooks/`, serialized by a per-UUID in-process mutex. Close-
notebook (`DELETE /session`) and delete-notebook remove the file; startup
`SweepSessions` removes files whose notebook no longer exists (covers
kill -9). Import provenance is tracked in a `__renart_imports` table inside
each session DB (ref → object, imported_at, row_count, complete).

## Test surface

`internal/web/notebook` covers the loader, DAG, runner (real DuckDB +
real SQL parser), import cache + attach fast path, rename invariance, viz
parser, and promotion planning. `internal/web/service` covers ComputeState
loading + direction rule, the full CRUD/run lifecycle, and promotion.
`web/tests/e2e/redesign/notebooks.live.spec.ts` drives the real server:
create/edit/run, chart-type write + rename rewrite, promotion, and catalog
isolation.
