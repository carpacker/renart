# Notebook auto-recompute in the backend

Status: **implemented** (all three phases). This began as an evaluation; the
recommendation below was then built. Implementation notes are at the end.

## TL;DR

**It would work, and it would be meaningfully snappier.** Every building block
the backend needs already exists — the cell DAG, the per-notebook DuckDB
session (with real table schemas), the SQL parser/validator, the Runner, and a
global SSE hub the client already consumes. The current client-driven design
pays multiple HTTP round-trips per recompute *wave* (save → parse-context →
run, repeated per level of the chain). A backend owner collapses a whole chain
into one in-process loop and streams results over SSE, so the user pays roughly
one save round-trip instead of N×(parse + run).

The real cost is **ownership**: staleness and "last result" state move from the
React component to the server, which is an architectural shift (and, as a bonus,
makes multi-viewer correct). I recommend doing it **behind the existing
auto-recompute toggle**, in phases, keeping the client path as the fallback.

## What the backend already has

| Need | Already in the backend | Reference |
| --- | --- | --- |
| Cell DAG (topo order, ancestors, descendants) | yes | `internal/web/notebook/dag.go` |
| Execute a wave of cells in one session pass | yes | `notebook.Runner.RunCells` |
| Real upstream schemas for re-validation | yes — the session DuckDB holds the actual tables/views | `internal/web/notebook/session.go` |
| Single-SELECT detection + unresolved-column diagnostics | yes | `service/parse_context.go`, `sqlintelligence/polyglot.go` |
| Knows when a cell changed | yes — every save lands here | `service.NotebookService.UpdateCell` |
| Push results to the client without polling | yes — global SSE hub + `/api/events`, client filters by `type` | `internal/web/events/hub.go`, `web/hooks/use-workspace-sync.ts` |
| Cancel an in-flight run | yes (just added) — ctx → DuckDB interrupt | `notebook/run.go` `RunCells` ctx check |

The only thing the client owns today that the backend does **not** yet track is
**per-cell staleness** and **last-run success** — both trivially derivable
server-side (mark stale on save of a cell + its `Descendants`; clear on a
successful run).

## Why the client design is the bottleneck

The current loop (see `web/components/redesign/notebook-page.tsx` and
`web/lib/notebook-autorecompute.ts`) is correct and was deliberately made
wave-by-wave for safety, but each wave costs round-trips:

```
type → AUTO_COMMIT_DEBOUNCE(350ms) → PUT /cells/{id} (save)
     → mark stale → AUTO_RECOMPUTE_DEBOUNCE(300ms)
     → (per cell) POST /sql/parse-context  [re-validation gate]
     → POST /run  → apply results
     → results change schema → next wave repeats the whole thing
```

For a 3-deep clean chain that is ~3× (parse RTT + run RTT) plus two debounce
windows, all serialized. The re-validation gate (`useSQLParseContextState`'s
`isCurrent`) exists precisely because the client cannot see the new upstream
schema until a result round-trips back and a fresh parse-context round-trips
again.

On the backend none of that is remote: after a cell runs, the next cell's
inputs are *already in the session DB*. Validate-then-run for the whole chain is
a tight local loop.

## Proposed backend design (phased, behind the toggle)

### Phase 1 — server-driven recompute, client still owns the editor
- Add per-notebook in-memory state: `stale set`, `lastResult` per cell,
  `autoFailed` memory, and an `autoRecompute` flag (seeded by a query param /
  header from the client toggle).
- On `UpdateCell`: mark the cell + `Descendants` stale. If auto-recompute is on,
  enqueue a recompute pass for the notebook (debounced, one pass at a time —
  the session file lock already serializes execution).
- The pass computes eligibility against the **session's real schemas** (reuse
  `sqlintelligence` for single-SELECT + column resolution; this is strictly
  better than the client's column heuristic), runs each ready wave via
  `RunCells`, and re-validates the next wave locally.
- Publish each `CellRunResult` as a `notebook.cell.result` SSE event tagged with
  the notebook id. The client subscribes (it already has the EventSource) and
  applies results, replacing its parse-context+run orchestration for the auto
  path. Manual run, "run from here", and the editor's parse-context endpoint are
  unchanged.

### Phase 2 — make the server the source of truth for staleness
- Expose the stale set + last results in the notebook GET payload and via SSE,
  so a freshly opened tab (or a second viewer) renders correct staleness without
  replaying edits. Retire the client-side stale propagation.

### Phase 3 — drop the client auto-recompute module
- Once Phase 1/2 are proven, `notebook-autorecompute.ts` and the client
  debounce/eligibility machinery can be deleted; the client keeps only the
  typing→save debounce (auto-commit) and result rendering.

## Risks and things to get right

- **Validation parity.** Server-side eligibility must not be *looser* than the
  client's, or it could run a cell into a failure. Reusing `sqlintelligence`
  against the live session schema is the safe move (it already powers the
  unresolved-column diagnostics). The existing breaking-rename e2e becomes a
  backend test.
- **Concurrency.** One recompute loop per notebook; the session lock serializes
  DuckDB work already. Need a cancel/supersede signal so a new edit aborts an
  in-flight pass (ctx cancel — same handle the Stop button now uses).
- **SSE scoping.** The hub is a global broadcast; events must carry the notebook
  id and the client must filter (same pattern as `staleness.updated`). Fine at
  current scale; a per-notebook channel is a later optimization.
- **Drafts stay client-side.** The backend only ever sees *saved* content, so
  the typing→save debounce (auto-commit) remains on the client. Clean split:
  client owns "what the user is typing", server owns "what is stale and what it
  evaluates to".
- **Python cells.** Already excluded from auto-recompute and (per the
  cancellation note) not truly killable via bruin's operator — keep them manual.

## Implementation notes (as built)

All three phases shipped. What landed, and where it diverged from the plan:

- **Engine** (`internal/web/service/notebook_autorecompute.go`,
  `notebook_recompute_pass.go`): a per-notebook in-memory `notebookRuntime`
  (stale set, last results, `autoFailed` memory, the toggle, import
  environment) held in a `notebookRuntimes` map on `NotebookService`. The
  eligibility logic — `computeAutoRecomputeWave` (run only cells whose upstreams
  are already fresh) and `computeAutoRecomputeClosure` (the auto-pending set) —
  is a straight Go port of the old client functions, covered by
  `notebook_autorecompute_test.go`.
- **Trigger**: `UpdateCell` → `onCellChanged` marks the cell + `Descendants`
  stale and arms a 200 ms debounce; the pass runs wave by wave, re-validating
  between waves, and interrupts an in-flight wave (ctx cancel) when a new edit
  supersedes it. `Run` (manual) folds its results into the runtime and can kick
  the pass for newly-unblocked downstreams. `DeleteCell` forgets the cell.
- **Validation reuse**: instead of a bespoke validator we inject
  `ParseContextService.Parse` as the `ValidateSQL` dep, against sibling
  *output* columns — identical semantics to what the client used to request, so
  the breaking-rename guard holds with zero divergence.
- **Transport**: a single `notebook.runtime` SSE event (stale / auto_pending /
  running / results-delta), tagged with the notebook id, on the existing hub via
  `PublishImmediate` (not the debounced `Publish`, so results aren't coalesced).
  New endpoints: `GET …/runtime` (seed snapshot), `PUT …/settings` (toggle +
  environment), `POST …/cancel` (Stop also halts a server pass).
- **Client**: `notebook-page.tsx` now renders server state — it subscribes to
  the runtime stream and seeds from the snapshot; `auto_pending` drives the
  stale-hatch suppression. The whole client eligibility module
  (`lib/notebook-autorecompute.ts`) and the `isCurrent` parse gate were deleted;
  only the typing→save auto-commit debounce remains client-side.
- **Optimistic staleness**: on edit the server publishes all stale cells as
  auto_pending up front, so the hatch doesn't flash; the pass then demotes any
  that won't actually refresh (Python, non-SELECT, errors) within the debounce.

Known follow-ups: rename/block-reorder don't yet re-trigger recompute for the
*other* cells whose references they rewrite (a manual run or any subsequent edit
recovers); Python cells remain manual; runtime state is in-memory (lost on
server restart, same as the old client state was on reload).

## Key files

- `internal/web/service/notebook_autorecompute.go`,
  `notebook_recompute_pass.go` — the engine
- `internal/web/service/notebook_service.go` — `UpdateCell`/`DeleteCell`
  triggers, `Run` folding results
- `internal/web/notebook/run.go` — `Runner.RunCells` / `runOne`
- `internal/web/notebook/dag.go` — DAG helpers
- `internal/web/service/parse_context.go` — the reused validator
- `internal/web/events/hub.go`, `cmd/server.go` (`hub`, deps wiring) — SSE
- `web/components/redesign/notebook-page.tsx`,
  `web/lib/api-notebooks.ts` — the client consumer
