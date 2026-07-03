# Notebooks — current architecture

Status: current state (built on the `redesign` branch, June 2026). Cells are
ordinary Bruin assets in a notebook namespace; results live in an ephemeral
per-notebook DuckDB session; recompute is server-driven.

## 1. File format and identity

Notebook-as-folder; everything travels through git:

```
notebooks/
  revenue-exploration/
    notebook.yml          # notebook id, title, ordered blocks (cell | markdown)
    clean_sales.sql       # ordinary Bruin asset file; frontmatter id: + class
    by_month.sql
```

- Filename = cell name; the frontmatter `id:` is authoritative and survives
  renames (the manifest references cells by id, so rename = rewrite sibling
  files + move the cell file, no manifest edit).
- Prose blocks live in `notebook.yml`; they are not assets and have no
  fingerprints.
- SQL-only for now (`isCellFile` accepts `.sql`); Python cells wait on Python
  fingerprint hardening (staleness.md §8). Python *support inside sessions*
  exists for execution (`notebook/run_python.go`) but cells are excluded from
  auto-recompute.
- Loading: `service/notebooks.go` folds notebooks into `ComputeState`; every
  asset carries `class: pipeline | notebook`.

## 2. Core invariants (and how each is enforced)

1. **No logical name ever enters a fingerprint.** `CellFingerprint` resolves
   sibling names → `cell_<id>` via the identifier splice (never the SQL
   parser's `RenameTables`, which fails on Jinja and re-prints the statement),
   then canonicalizes. Rename is therefore free: zero fingerprints change.
   Guarded permanently by `TestRenameChangesNoFingerprint` (5 referencing
   cells incl. Jinja, a string literal, a self-join, a comment mention).
2. **Class is first-class; dependency direction is policed.**
   `validateAssetClassDirection` makes a pipeline asset depending on a
   notebook cell a state-level *error*. Catalog/lineage read
   `state.Pipelines` only, so cells never leak into production surfaces.
3. **Presentation lives in comments, outside the fingerprint.** `@viz`
   directives are comment lines; canonicalization strips them
   (`TestVizIsOutsideFingerprint`). Any directive that *should* affect
   execution semantics must go into asset config instead — that rule lives in
   the directive parser's doc comment.
4. **Physical objects are machine-named.** Cells materialize as `cell_<id>`,
   imports as `src_<sanitized ref>`, inside
   `.renart/notebooks/<uuid>.duckdb`. Logical names exist only in the editor.

## 3. Sessions, imports, cleanup (`notebook/session.go`, `run.go`)

- One `.duckdb` file per notebook UUID, serialized by a per-UUID in-process
  mutex. Cells materialize as views by default; `@materialize(table)` pins a
  table (Python cells always materialize tables).
- **Import resolver:** a cell referencing a pipeline asset gets the data
  brought into the session. Fast path for DuckDB-backed assets is a zero-copy
  batched `ATTACH; CTAS; DETACH` (ATTACH visibility is per-connection, so it's
  one batch); everything else falls back to a row-capped generic `Fetch`
  through the connection. `SourceFetcher` is the swappable seam for a future
  cloud gateway. Unknown refs (`ErrUnknownSource`) are left untouched so the
  session yields a clear missing-table error. Provenance is tracked in a
  `__renart_imports` table inside each session DB.
- **Cleanup = delete the file.** Close-notebook and delete-notebook remove the
  session file; startup `SweepSessions` removes files whose notebook no longer
  exists (covers kill -9). No warehouse objects, no janitor edge cases.
  Protected environments fall out for free: a notebook reads prod via the
  import resolver and writes only the local file.
- Cell runs do **not** emit facts into matlog; staleness/results are runtime
  state (see §6), honest for the ephemeral per-session model.

## 4. Rename engine (`notebook/rename.go`)

A hand-written identifier-splice tokenizer (not the parser's `RenameTables`,
which would uppercase keywords and destroy user formatting) walks
code/string/comment/quoted-identifier/Jinja states and replaces only bare or
double-quoted identifier tokens: `'base'`, `-- base`, and `{{ base }}` are
left alone while `from base` and `base.id` are rewritten; a name preceded by
`.` (`schema.base`) is skipped. The same splice resolves names for the
fingerprint (it never fails on templated SQL). Validation before applying:
identifier charset, collisions against sibling cells, pipeline asset names,
and reserved words.

## 5. Viz directives (`notebook/viz.go`, `notebook-viz*.tsx`)

`-- @viz(kind, key: value, …)` with kinds `table | bar | line | area | pie |
kpi`, parsed by a real tiny parser producing a typed config or a
span-carrying diagnostic. First directive wins; duplicates warn. The Recharts
renderer row-caps per kind and degrades gracefully on missing columns. The
chart settings popover parses and rewrites the directive line — text is the
single source of truth. `@viz` is the first member of a general
`-- @word(args)` comment-directive family (`@materialize` is another); all
directives are comments and therefore outside fingerprints by construction.

## 6. Server-driven auto-recompute (`service/notebook_autorecompute.go`)

The server owns staleness and recompute; the client owns only "what the user
is typing" (a typing→save debounce) and rendering.

- Per-notebook in-memory `notebookRuntime` (stale set, last results,
  `autoFailed` memory, the auto-recompute toggle, import environment) held on
  `NotebookService`. Lost on restart, by design.
- `UpdateCell` marks the cell + descendants stale and arms a 200 ms debounce;
  the pass runs wave by wave against the session's *real* schemas,
  re-validating between waves (validation is `ParseContextService.Parse`
  injected as the `ValidateSQL` dep — identical semantics to what the client
  used to request). A new edit ctx-cancels an in-flight wave; Stop
  (`POST …/cancel`) halts a server pass. Manual `Run` folds results into the
  runtime and can unblock downstreams.
- Transport: a single `notebook.runtime` SSE event
  (stale / auto_pending / running / results-delta) tagged with the notebook
  id, via `PublishImmediate`. Endpoints: `GET …/runtime` (seed snapshot),
  `PUT …/settings` (toggle + environment), `POST …/cancel`.
- Optimistic staleness: on edit the server publishes stale cells as
  auto_pending up front so the hatch doesn't flash, then demotes any that
  won't actually refresh (Python, non-SELECT, errors).
- Eligibility logic (`computeAutoRecomputeWave` / `…Closure`) is a Go port of
  the deleted client module, covered by `notebook_autorecompute_test.go`.

## 7. Promotion (`notebook/promote.go`)

Single-cell promotion: pick target pipeline + name → dialect check → move the
file into the pipeline dir, set `class: pipeline`, assign the real target
name, rewrite references in remaining cells (same splice machinery), keep the
asset id stable. Dialect mismatch **warns** ("review the SQL") instead of
blocking with flagged expressions — Bruin's `sqlparser` exposes no transpiler;
same-dialect promotion (the common DuckDB→DuckDB case) is clean. The promoted
asset's fingerprint changes → `never_built` in pipeline envs, the correct
prompt to build it.

## 8. Not built / parked

- Rename/block-reorder don't re-trigger recompute for the *other* cells whose
  references they rewrite (a manual run or any subsequent edit recovers).
- Promote-whole-notebook (subgraph → new pipeline); Monaco for cell editors
  (cells are textareas — no SQL intellisense inside cells; reusing
  `use-asset-monaco` is the obvious follow-up); Monaco squiggles/completion
  for `@viz`.
- Warehouse-backed `notebook_target` (sandbox schemas + manifest/TTL janitor);
  the DuckDB-file default with delete-on-close + startup sweep is what exists.
- Parked by decision: Python cells in the DAG, parameters/widgets,
  cross-notebook references (workaround: promotion), result persistence
  (reopen re-queries head N), notebook sharing/cloud (folders of files travel
  through git and the snapshot CAS for free).
- Reference syntax is bare names by decision; `{{ ref() }}` is not supported
  in cells.

## Test surface

`internal/web/notebook` covers loader, DAG, runner (real DuckDB + real SQL
parser), import cache + attach fast path, rename invariance, viz parser,
promotion planning. `internal/web/service` covers ComputeState loading +
direction rule, CRUD/run lifecycle, autorecompute eligibility, promotion.
`web/tests/e2e/redesign/notebooks.live.spec.ts` drives the real server.
