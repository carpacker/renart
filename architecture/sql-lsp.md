# SQL language server — current architecture

Status: current state (built on the `sql-language-server` branch, July 2026).
A Go implementation in `internal/sqllsp` serving two frontends: a stdio
JSON-RPC LSP server (`renart debug sql-lsp`) for external editors, and HTTP
endpoints (`/api/sql/lsp/*`) consumed by the web UI's Monaco editors —
including notebook cells.

> The original design exploration (`renart-sql-lsp-plan.md`, deleted — see git
> history) sketched a Rust core. The as-built engine is pure Go with the same
> provider-neutral graph boundary; the Rust split never became necessary
> because the analysis layer is regex-based rather than a full parser.

## 1. Layering

```
Graph sources
  web WorkspaceState (pipelines + notebooks)   LoadGraphFromDir (bruin + dbt folders)
        ↓                                            ↓
Canonical graph  (assets, relations, schemas, provenance — provider-neutral)
        ↓
Engine  (internal/sqllsp/analyzer.go: regex scope analysis, per-request)
        ↓                          ↓
stdio JSON-RPC server        web service (internal/web/service/sql_lsp.go)
(cmd/sqllsp.go)              → httpapi /api/sql/lsp/* → web/hooks/use-sql-lsp.ts
```

- **Canonical graph** (`types.go`): `CanonicalGraph{Assets, Relations,
  Schemas, …}` with per-node `Provenance`. Renart/Bruin/dbt are provenance,
  not core concepts — the engine never branches on provider.
- **Engine** (`analyzer.go`): stateless over an immutable graph plus an
  in-memory index (`NewEngine` builds name→asset/relation/columns maps; cheap,
  ~1 ms for 500 assets). Each request analyzes the open document's SQL with
  regexes — no persisted AST, no incremental state. Features: diagnostics,
  completion, definition, hover, references, rename, quick-fix code actions,
  semantic tokens, document symbols, signature help.
- **Syntax validation** is delegated to the optional polyglot FFI library
  (`polyglot_ffi.go`, downloaded to a version-keyed cache or pointed at via
  `POLYGLOT_SQL_FFI_PATH`). When absent, diagnostics degrade to the regex
  checks (unresolved relation/alias/column); nothing else changes.
- **Templates**: `{{ ref(...) }}` / `{{ source(...) }}` calls are rendered
  length-aware with a source map (`render.go`); diagnostics and tokens map
  back to template ranges. Rename refuses templated documents with
  `ErrRenameTemplated` — edits against rendered SQL cannot be mapped back
  safely — and both frontends surface the reason (LSP `RequestFailed`,
  Monaco `rejectReason`).

## 2. Web service: state → graph, caching

`SQLLSPService` (`internal/web/service/sql_lsp.go`) builds the graph from the
coordinator's `WorkspaceState` rather than the filesystem:

- Every pipeline asset becomes an asset node + relation; declared
  `model.Column`s become a `declared` schema layer.
- The graph is **cached by `WorkspaceState.Revision`** (monotonic, bumped on
  every mutation). Editing issues LSP requests per keystroke against the same
  saved state, so rebuilding per request was wasted work. `Revision == 0`
  (unmanaged/initial state) is never cached.
- The polyglot client is shared and loaded lazily in the background
  (`NewLazyPolyglotClient`); requests never block on it and pick it up as
  soon as it is ready.

## 3. Notebook cells

Notebook cells (`state.Notebooks[].Cells`) are LSP targets like pipeline
assets, but their visibility mirrors the per-notebook DuckDB session
(notebooks.md §2):

- `selectedAsset` resolves cell IDs and returns the containing notebook.
- `graphForRequest` extends the cached pipeline graph **per request** with
  that notebook's cells (fresh slices — the cached graph is never mutated):
  sibling cells become relations with declared or inferred columns, and each
  cell's `ExternalRefs` (warehouse tables that are neither cells nor pipeline
  assets) become *bare* relations — they resolve without claiming columns, so
  reading a raw table is never an `unresolved-relation` error and its columns
  are never validated.
- Scoping is strict both ways: cells of other notebooks stay unresolved, and
  pipeline-asset requests never see notebook cells.
- References from a cell also search sibling cell documents.

In the editor (`notebook-cell-editor.tsx`), SQL cells use `useSQLLSP` with the
same provider split as the asset editor: the language server owns diagnostics,
decorations, hover, rename, etc.; `useSQLIntellisense` keeps schema-aware
completion (which knows notebook run columns the backend cannot see).

Python assets and Python notebook cells project static SQL passed as the first
argument to `query("...")` or `renart.query("...")` through
`use-python-query-intellisense.ts`. The host document stays Python. A small
literal scanner decodes ordinary, raw, and triple-quoted strings and keeps a
UTF-16 source map, then the adapter translates completion, diagnostics, hover,
definition/navigation, signature-help, and semantic-token positions to and
from the existing SQL LSP. Interpolated strings, bytes, variables,
concatenation, and other runtime expressions remain Python-only because they do
not represent one stable SQL document. This projection uses the Python asset's
existing graph identity, so a notebook query sees only its sibling cells and
the same pipeline relations as an SQL cell.

Completion has two inputs, matching native notebook SQL cells: canonical graph
suggestions from the LSP and the editor's `schemaTables` context. The latter
adds schemas discovered by the client and a sibling cell's last successful run
columns, which are intentionally ephemeral and therefore absent from
`WorkspaceState`. LSP suggestions win when both sources describe the same
item. Arbitrary Python output consequently gains column completion after that
cell has run, without moving runtime state into the canonical graph.

The adapter also projects Monaco's SQL lexical tokens into decorations inside
the Python string, then overlays LSP semantic relation tokens when they arrive.
This keeps SQL syntax highlighting immediate for both closed strings and the
unfinished plain literals produced while a user is typing.

## 4. Column inference: fixpoint

SQL assets that declare no columns get an `inferred` schema layer so
completion and column validation work against them. Inference reads the
asset's SQL (`InferOutputColumns`): named projections directly, `select *` /
`alias.*` by copying the referenced relation's columns *from the engine's
index*. That makes inference self-referential — a `select *` asset needs its
upstream's columns, which may themselves be inferred.

`withInferredColumns` therefore runs a **topologically ordered fixpoint**
(capped at `maxInferenceRounds = 5`):

1. Order the undeclared assets upstream-first (`topoOrderInferenceTargets`,
   Kahn's algorithm over the declared upstreams; cyclic leftovers keep their
   original order).
2. Infer each asset against the current graph, applying every result to the
   engine's index **immediately** (`Engine.SetRelationColumns`), so later
   assets in the same round see it.
3. If any asset's inferred column set changed, rebuild the schema layers
   (base + one `inferred` layer per asset) and repeat.
4. Stop when a round changes nothing or the cap is hit.

With truthful edges, step 2 walks a DAG in one round regardless of chain
depth; round two confirms stability. The fixpoint loop stays as the
correctness net for when the edges lie.

Design notes, in decreasing order of importance:

- **The edges are cheap and reliable**: pipeline `depends:` is auto-reconciled
  on every asset save (`reconcileSQLAssetDependencies` parses the SQL with the
  bruin sqlparser and persists the result; async retry while mid-edit SQL
  doesn't parse), and notebook cell upstreams are derived at load time by the
  same used-tables scan (in memory only — cell files are never rewritten).
  Both arrive in `model.Asset.Upstreams`, name-keyed like the graph's
  relations, so ordering needs no extra SQL parsing. This is why topo ordering
  is worth having *inside* the fixpoint but not as a replacement: on its own
  it would inherit every staleness window of those edges.
- **Every round re-infers all undeclared assets**, not just the still-empty
  ones: partial results can grow (`select a.x, * from …` yields `x` before
  the `*` resolves).
- **Determinism**: the result is the least fixpoint, independent of asset
  iteration order — ordering affects how fast it converges, never what it
  converges to. (The original code rebuilt the engine per asset inside the
  loop with no outer loop, so chaining depended on iteration order and the
  whole thing was O(N²).)
- **Cycles** converge naturally — a cycle simply stops producing new columns —
  so no cycle detection is needed; Kahn's leftovers just run last.
- **Failure mode is benign**: only when edges are missing/stale *and* the
  chain is deeper than the cap do columns go missing. An empty column set
  suppresses unknown-column diagnostics, so the worst case is "no
  completions", never false errors.
- **Cost placement**: for pipeline assets the fixpoint runs inside the
  revision-cached graph build (once per workspace save; ~50 ms per round per
  500 assets, and DAGs now take two rounds). For notebooks it runs in the
  per-request augmentation, over that notebook's cells only (a few ms).

## 5. stdio server

`renart debug sql-lsp` (`cmd/sqllsp.go`) loads the graph from the workspace folder
(`LoadGraphFromDir` understands bruin pipelines and dbt projects incl.
`schema.yml`), serves LSP over stdio, and reloads the graph on
`workspace/didChangeWatchedFiles`. A missing graph degrades to syntax-only
analysis. Message size is capped at 64 MiB (`maxMessageBytes`).

## 6. Completion & diagnostic surface (web editor)

The app's Monaco asset editor (`web/components/app/asset-editor.tsx`) drives SQL
intellisense **entirely through the LSP** (`web/hooks/use-sql-lsp.ts`); the older
client-side parse-context providers are deliberately disabled, so the LSP is the
single source of truth.

- **Completions** by context: column fields (after an alias `.`), relations —
  workspace assets and, in a `from schema.` position,
  `relationCompletionsInSchema` returns schema-stripped inserts — and clause
  **keywords** (`keywordCompletions`, sorted last via a `z` SortText so
  schema-aware items win). The client keeps only kinds it renders (columns,
  relations, keywords). Purely-remote warehouse tables (no backing asset) are
  not yet completed from the LSP — see `plans/remote-table-intellisense.md`.
- **Diagnostics**: unresolved relation / alias / column (column checks only fire
  when the relation's columns are known from asset SQL or declared metadata),
  **circular self-reference** (a used relation that resolves to the current
  asset), rendered-template diagnostics, and polyglot syntax errors. Column-not-
  materialized warnings and inspect-error markers are intentionally not surfaced
  in this editor.
- **Monaco gotcha**: the completion registry is shared across languages, so the
  python/yaml completion providers must register once (keyed on `asset?.id`,
  live state via a ref) — re-registering them on a workspace/SSE update
  re-triggers any open SQL suggestion widget and drops its selection.
