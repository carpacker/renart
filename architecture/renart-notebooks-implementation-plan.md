# Implementation plan: Renart notebooks

Builds directly on the shipped foundation: fingerprint engine (fp_version,
consumed-vars, Merkle), materialization facts + coverage, staleness service,
snapshot CAS, per-env schedules, protected environments. Cells are ordinary Bruin
assets in a notebook namespace; everything below is about making that true without
leaking scratch work into the production-facing surfaces.

**Reference prototype:** `sqlx-notebook.jsx` — the interaction model (reactive DAG,
status dots, run-from-here, `@viz` directives, inline rename, DAG strip with
external sources) is the target UX. Its implementation shortcuts that must NOT be
carried over: regex-based dependency scanning (use the real SQL parser; the
prototype's word-boundary match hits names inside identifiers-in-strings it didn't
strip, and can't see through quoting/Jinja), rename without reference rewriting,
and `CREATE TABLE` per run (views by default per the cleanup design).

---

## Core invariants (everything else is consequences)

1. **No logical name ever enters a fingerprint.** Cell fingerprints hash the
   ID-resolved physical form: canonicalize after rewriting referenced logical names
   to stable cell IDs, and exclude the cell's own display name from config_hash
   (its physical target is named by ID). Consequence: rename is free — zero
   fingerprints change, the staleness service sees nothing, no recompute can occur.
2. **Asset class is a first-class dimension, and dependency direction is policed.**
   Every asset carries `class: pipeline | notebook`. Catalog, global lineage,
   search, and pipeline-side completion filter to `pipeline` by default. A pipeline
   asset depending on a notebook asset is a validation *error*, not a warning.
3. **Presentation lives in comments and is therefore outside the fingerprint.**
   `@viz` directives are comment lines; canonicalization strips them; changing a
   chart type can never trigger staleness or a rerun. Text is the single source of
   truth — any viz-settings UI writes the directive back into the cell source.
4. **Physical objects are machine-named and notebook-scoped.** Everything a
   notebook creates lives at `nb_<notebook_id>.cell_<cell_id>` (or prefixed-table
   fallback). Logical names exist only in the editor layer.

---

## Phase N1 — File format and loader (2–3 days)

Notebook-as-folder:

```
notebooks/
  revenue-exploration/
    notebook.yml          # id, title, cell order, prose blocks, settings
    clean_sales.sql       # ordinary Bruin asset file, frontmatter: id, class
    by_month.sql
    growth.sql
```

- **Filename = cell name; `id:` in frontmatter is authoritative.** Git rename
  detection works on the file move; the ID survives it. New unnamed cells get
  `cell_<n>.sql` autonames (prototype behavior).
- `notebook.yml` holds: notebook `id`, display title, ordered list of blocks —
  each either `cell: <cell_id>` or `markdown: |` prose. Prose blocks are not
  assets; they have no IDs in the DAG and no fingerprints.
- Loader changes: the existing asset loader gains the `class` field; a notebook
  loader wraps it, assembling the folder into a Notebook struct. Validation reuses
  the pipeline validator (cycles → reuse existing detection; the prototype's
  cycle UI treatment is the target).
- Every existing code path (parsing, lineage extraction, fingerprinting, rendering)
  operates on cell assets unchanged. That's the point of the design — if any code
  path needs a notebook special case here, stop and ask why.

**Exit:** a notebook folder loads into a DAG of class-tagged assets; round-trips
through save without reordering noise in git diffs.

## Phase N2 — Namespace, catalog and lineage isolation (1–2 days)

- Catalog indexer: `WHERE class = 'pipeline'`. Global lineage graph: same filter on
  nodes; edges into notebook-class nodes simply don't exist there.
- **Direction rule:** validator rejects `pipeline asset → notebook asset` deps with
  a clear message ("pipeline assets cannot depend on notebook cells; promote the
  cell first"). Notebook → pipeline deps resolve normally (cells read prod assets).
- Completion scoping: inside a notebook, the completion index = pipeline assets ∪
  own notebook's cells. Inside pipeline assets, notebook names are absent.
- Warehouse-side: if/when the catalog reflects live warehouse schemas, ignore
  `nb_*` schemas in the crawler.
- Per-asset lineage page gets an off-by-default "show notebook consumers" toggle
  (deferred; just leave the query hook).

**Exit:** a notebook referencing `marts.fct_orders` runs fine; `fct_orders`'s
lineage page and the catalog show no trace of it; a pipeline asset referencing a
cell fails validation.

## Phase N3 — Sessions, physical naming, cleanup (3–4 days)

Implements the earlier cleanup design, now concrete around the DuckDB default:

- **Session = (notebook, environment, target).** Target resolves per the
  precedence above; the default is the built-in DuckDB connection, where each
  notebook gets its own database file (`.renart/notebooks/<notebook_id>.duckdb`).
  Cells materialize as views/tables named `cell_<id>` inside it.
- **Upstream import resolver — the one genuinely new piece.** A cell referencing a
  pipeline asset that lives on another connection (Snowflake, Postgres, …) can't
  just be name-rewritten; the data must be brought into DuckDB. Resolution for a
  pipeline-asset reference under a DuckDB target: look up the asset's physical
  location → fetch via the connection as an Arrow stream → register/CTAS into the
  session DB as `src_<asset_id>` → rewrite the reference to that name. Cache keyed
  by the upstream asset's **fingerprint + coverage watermark** (both already
  queryable), so re-opening a notebook re-fetches only when upstream actually
  changed; show freshness ("imported 2h ago · upstream unchanged") on the cell's
  source chip. Guardrails: default row cap / `LIMIT`-pushdown with an explicit
  "import full table" action, and per-import size telemetry. This resolver is
  deliberately the same shape as the future cloud gateway read path (asset →
  Arrow stream → local DuckDB) — build the interface so the fetch backend is
  swappable (direct connection now, gateway later).
- **Views by default** within the session DB; per-cell pin (`@materialize(table)`
  directive or pin button writing it) for expensive cells. Imported sources are
  always tables (they're the cache).
- **Cleanup, default path: delete the file.** Closing a notebook (or GC'ing a
  deleted one) removes its `.duckdb` file — no manifest reconciliation, no
  orphaned warehouse objects, no janitor edge cases. The manifest +
  startup-sweep + River TTL janitor machinery from the earlier design applies
  **only to warehouse-backed `notebook_target`s** (sandbox schemas), where it
  works exactly as designed; BigQuery targets additionally set dataset
  expiration.
- **Protected environments:** notebook runs are classified interactive at the
  Phase-6 policy chokepoint. With the DuckDB default, a notebook in a protected
  env *reads* prod assets (via the import resolver, subject to read grants) and
  *writes* only the local file — the failure mode the protection exists for is
  structurally impossible. Warehouse-backed targets in protected envs require the
  forced `notebook_target` pin.
- **Facts integration:** cell runs write materialization facts + coverage exactly
  like pipeline runs (asset_ids are notebook-scoped, environment is the session's
  env, so pipeline staleness is untouched). Notebook staleness = the same
  staleness service pointed at the notebook's DAG; import-cache freshness uses the
  same coverage lookups. Facts/coverage rows for a deleted notebook are GC'd with
  it.
- Run mechanics from the prototype to keep: run-from-here = stale-set ∩
  descendants; blocked status when an upstream errored; status dots.

**Dialect note (cuts across N3/N5/N7):** with DuckDB as the default engine, cells
are written in DuckDB SQL while pipeline assets may target another dialect. Inside
the notebook this is fine — imported sources are plain tables. It surfaces at
**promotion** (N7): transpile the cell through the SQL parser's dialect conversion
where clean, and where constructs don't translate, block promotion with the
specific expressions flagged rather than emitting silently-different SQL. Cells
whose target env is the same warehouse dialect skip all of this.

**Exit:** open → edit → run → close leaves zero objects (file deleted); kill -9
mid-session leaves only a stale file that the next startup removes; a cell reading
a Snowflake pipeline asset works against the local DuckDB session with visible
import freshness; coverage answers "which cells are stale" with the existing
service.

## Phase N4 — Rename engine (2–3 days)

- Trigger: rename the file via UI (F2 on cell header, like the prototype's inline
  input, but committing on blur/enter with validation rather than per-keystroke).
- Validation before applying: identifier charset (prototype's `\w` coercion is
  fine), collision against sibling cells AND pipeline asset names AND warehouse
  reserved words; reject with inline error.
- Apply as one atomic WorkspaceEdit: for each referencing cell (from the dependency
  index), get parser token spans for the old name (positions only — never AST
  re-print; preserve user formatting, casing, comments), splice new name; rename
  the file; update notebook.yml if needed. Single undo step in the editor.
- Edge cases to test: quoted identifiers, name inside string literal (must NOT be
  rewritten — parser spans handle this; the prototype's regex would corrupt it),
  name inside Jinja expression, self-reference in a comment (don't touch), rename
  to a name that differs only by case on a case-insensitive dialect.
- **Assertion test (the invariant):** snapshot all fingerprints, rename a cell with
  five referencing cells, recompute — byte-identical map. Make this a permanent
  regression test; it guards invariant 1 against future canonicalization changes.

**Exit:** rename a mid-DAG cell → all references update, zero cells go stale, the
warehouse is untouched, git shows a file rename + small diffs.

## Phase N5 — Viz language v1 (4–5 days)

Formalize the prototype's directive into a small, versioned grammar:

```
directive   := "--" ws "@viz(" kind ( "," pair )* ")"
kind        := "table" | "bar" | "line" | "area" | "pie" | "kpi"
pair        := key ":" value
value       := string | number | bool | "[" string ("," string)* "]"
```

Per-kind keys (v1, deliberately small):

| kind | required | optional |
|---|---|---|
| table | — | limit, columns[] |
| bar / line / area | x, y (string or array) | stacked, format, sort |
| pie | x, y | limit |
| kpi | value | format (currency/percent/number), icon, compare (column for delta) |

- **Parser:** replace the regex with a real tiny parser (Go side, shared with CLI)
  producing either a typed config or a diagnostic with a span → Monaco squiggle on
  malformed directives, completion for kinds/keys (the IntelliSense infra exists).
  First directive in the cell wins; warn on duplicates.
- **Renderer:** frontend component keyed on the typed config — the prototype's
  Recharts components are essentially production-ready; port them with two
  hardenings: row caps per kind (charts slice head N with a "showing N of M"
  notice, as the table preview already does) and graceful column-missing states
  ("column 'revenue' not in result" instead of an empty chart).
- **Settings popover ⇄ text:** chart settings UI parses the current directive,
  edits write the directive line back into the source (text remains the source of
  truth, invariant 3). This is the same philosophy as the form-based ingestr
  editor — forms are views over text.
- **Validation against result schema:** after a run, check directive columns
  against actual result columns; surface mismatches as cell-level warnings.
- Explicitly out of v1 (write this in the docs): custom Vega/spec escape hatch,
  multi-query dashboards, interactions/cross-filtering. The directive language is
  the seed of the SQLX dashboarding concept — promotion of a notebook to a
  dashboard layout is a later feature that reads the same directives; don't let
  v1 grow toward it prematurely.

**Exit:** all six kinds render from directives; malformed directive shows a
squiggle with a useful message; editing only the directive reruns nothing.

## Phase N6 — Reactivity polish (2 days)

- Default: **lazy reactive** (prototype behavior) — edits mark self + descendant
  cone stale via the existing staleness events; nothing auto-runs.
- Per-notebook toggle `auto-run: true` (marimo-style): on save (debounced), execute
  the stale set in topo order, with hard guards — cancel button, per-cell timeout,
  and auto-run suspends itself after a failure until a manual run succeeds.
- Run telemetry on the cell header (rows · ms, as in the prototype) from the run
  record.

## Phase N7 — Promotion to pipeline (2–3 days)

- "Promote cell" action: pick target pipeline + schema/table name → **dialect
  check first** (transpile DuckDB SQL to the target connection's dialect via the
  parser; clean translation proceeds, untranslatable constructs block with the
  offending expressions flagged) → move the file into the pipeline dir, set
  `class: pipeline`, assign real target name in config, rewrite references in
  remaining notebook cells from the cell name to the new asset reference (the
  rename engine does this — it's the same WorkspaceEdit machinery with a
  different replacement), keep the asset `id` stable.
- The promoted asset's fingerprint changes (target config changed) → it's
  `never_built` in pipeline envs, which is the correct prompt to build/deploy it.
- Promote-whole-notebook (subgraph → new pipeline) reuses the same per-cell step in
  topo order. Defer the fancy version (auto-deriving schedules etc.).

---

## Parked (decide later, with current leanings)

- **Python cells:** the folder format and class tagging already admit `.py` assets;
  the blocker is result preview (materialized output table → same viz path) and the
  Phase-7 fingerprint hardening. Lean: ship SQL-only first, Python cells ride in
  after pipeline Python fingerprinting lands.
- **Parameters/widgets:** notebook-level vars reusing pipeline vars + consumed-vars
  hashing (staleness correctness is already guaranteed by the engine). Input
  widgets are a UI feature over that. Lean: vars yes (cheap), widgets later.
- **Cross-notebook references:** forbidden in v1 (namespace is per-notebook).
  Revisit only with a concrete use case; the workaround is promotion.
- **Result persistence:** don't store result blobs; on reopen, re-query head N from
  the still-existing nb_ objects (or show stale/missing states). Cheap and honest.
- **Notebook sharing/cloud:** notebooks are folders of files — they travel through
  git and the snapshot CAS for free; a "published notebook" (read-only, gateway-
  backed) is a cloud feature that needs no format changes.

## Resolved decisions

1. **Reference syntax: bare names.** The SQL parser provides span-based resolution
   and rewrite; completion and rename error messages are built around plain
   identifiers. `{{ ref() }}` is not supported in notebook cells.
2. **Notebook target: local DuckDB by default.** Resolution order for where a
   session materializes:

   ```yaml
   # project settings (default for all notebooks)
   notebooks:
     target: duckdb            # the built-in default connection

   # per-environment override (environments config)
   environments:
     staging:
       notebook_target: staging_sandbox   # a warehouse connection + nb_* schemas
     prod:
       notebook_target: duckdb            # protected envs may pin this

   # per-notebook override (notebook.yml)
   target: duckdb
   ```

   Precedence: notebook.yml → environment → project default. Protected
   environments may declare `notebook_target` as *forced* (overrides ignored), so
   "exploring prod data" can never mean "writing into prod's warehouse."

3. **Directive name: `@viz`,** as the first member of a general comment-directive
   family. The `-- @word(args)` grammar is a single reserved namespace: one parser,
   one Monaco integration (squiggles + completion), many directives. Known future
   members: `@materialize(table)` (cell pinning, N3) and `@param` (notebook
   variables, parked). All directives are comments and therefore outside
   fingerprints by construction — any future directive that *should* affect
   execution semantics (and thus staleness) must instead go into asset config, not
   a directive; write that rule into the directive parser's doc comment so it
   survives.

## Open decisions

None — all resolved.

**Sequencing:** N1 → N2 → N3 form the usable core (~1.5 weeks); N4 and N5 are
independent of each other after N3 (~1.5 weeks combined); N6/N7 are polish weeks.
The prototype remains the UX reference throughout — the implementation should feel
identical to it while standing on the parser, fingerprint engine, and staleness
service instead of regexes and in-memory state.
