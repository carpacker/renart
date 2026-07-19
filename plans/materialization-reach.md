# Advanced materialization UX and semantics

Status: proposed reach work. The guided SQL/Python/Load/API materialization
contract, complete metadata round-trip, full refresh, explicit replay-safe
backfill, and truthful coverage behavior have shipped. Their current design
lives in `architecture/backend.md`, `architecture/asset-editing.md`, and
`architecture/staleness.md`; this file contains only work that has not shipped.

## 1. Guided advanced SQL strategies

DDL, SCD2, and Data Vault strategies can already be hand-authored. Renart keeps
them visible as a custom value, validates the strategy against the concrete
Bruin warehouse materializer, and executes supported definitions. They are not
selectable in the guided editor because their field contracts are larger and
less uniform than the base table strategies.

Before making one selectable:

1. Extend the backend capability contract with its exact requirements, derived
   from the active Bruin materializer and covered by one test per warehouse
   family. Do not infer requirements from frontend asset-type strings.
2. Add the required column, key, and strategy-specific metadata operations to
   both guided and YAML-shaped editors. Preserve inactive metadata so changing
   modes remains reversible.
3. Add type-check diagnostics for every requirement currently enforced only
   during rendering. An incomplete multi-step edit may be persisted, but it
   must be visible before execution.
4. Add direct-render regression tests for the generated statements and the
   execution window. DDL must remain excluded from the Full refresh action;
   other advanced modes should not gain that action until their reset semantics
   are explicitly modeled.

The first candidate should be one SCD2 strategy on DuckDB/Postgres, where the
renderer and local integration tests are easiest to exercise. Generalize only
after that vertical slice is complete.

## 2. Coverage timeline and gap selection

The staleness API already returns covered seconds and exact gaps. Build stale
automatically compiles those gaps into a topological execution plan, while the
asset action can backfill one explicit UTC range when `backfill_safe` is true.

The remaining UX is a compact coverage timeline that:

- distinguishes covered ranges from gaps for the selected environment and
  fingerprint;
- lets a user select one or more reported gaps and delegates execution to the
  existing server-side planner or explicit backfill endpoint;
- never offers accumulation for `interval_aware` assets that are not also
  `backfill_safe`;
- refreshes through the existing SSE reconciliation path, without polling or a
  client-owned coverage model.

This is a visualization layer over the existing facts, not a new scheduler or
coverage store.

## 3. Static Python `materialize()` diagnostic

Python table execution already fails clearly when the module has no
`materialize()` function. Add the same feedback to pipeline type check so the
error appears before a run. Use the embedded Python parser/intelligence path;
do not use a regex that mistakes comments, strings, nested functions, or methods
for a module-level definition.

Acceptance cases: a module-level sync or async definition is accepted; a
comment/string/nested definition is rejected; run-only Python assets do not get
the diagnostic.

## Completion

When these items ship, fold the field contracts and coverage UI into the three
architecture documents above and delete this plan.
