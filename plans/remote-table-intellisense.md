# Remote-table intellisense in the SQL LSP

Status: proposal (investigation done; not yet implemented)

## Goal

Surface **warehouse tables that have no backing Bruin asset** (e.g. a raw
`public.events` table that exists in the connected database but isn't a
pipeline asset) in the SQL editor's FROM-clause completions — through the LSP,
consistent with how asset relations already complete.

Hard requirements (from the maintainer):
- **Defined assets always take precedence over purely remote tables.** On a name
  collision the asset wins; in ranking, assets sort above remote tables.
- **Modular and decoupled.** Remote-table discovery must be an optional,
  isolated component. If it is not configured, not yet loaded, errors, or
  hangs, the rest of the LSP (asset completions, diagnostics, hover, …) must
  keep working exactly as today. Discovery must never block a completion
  request.

## Current state

- **Discovery is client-side and connection-live.** The client fetches
  `/api/sql/tables` (`SQLService.Tables` → `conn.GetTablesWithSchemas`, a live
  query, *no cache*) and memoizes it in `sqlDiscoveryTablesAtom`.
- **The client already merges remote tables with asset precedence**, but only in
  a bare `from `/`join ` position (`isTableCompletionContext` in
  `web/hooks/use-sql-lsp.ts`, deduped against local asset names). It does not
  fire for partial prefixes (`from ord…`) or the `from schema.` case, and it is
  separate from the LSP's relation completions.
- **The LSP is a pure, static-graph engine.** `internal/sqllsp` takes a
  `CanonicalGraph` (built only from workspace assets + inferred columns, cached
  by workspace revision in `SQLLSPService.graphForState`) and returns
  completions/diagnostics. It has **no connection access** — `SQLLSPService`
  deps are `WorkspaceRoot`, `CurrentState`, `PolyglotClient`.
- `relationCompletions` / `relationCompletionsInSchema` (added for asset
  relations) and unresolved-relation resolution all read `graph.Relations`.

## Proposed design

Inject remote tables as **extra relation nodes** into the graph the LSP service
hands the engine, sourced from a decoupled, cached provider. This reuses the
existing relation-completion and relation-resolution machinery instead of adding
a parallel path, and the precedence/dedup falls out of graph construction.

### 1. `RemoteTableProvider` — isolated, cached, non-blocking

New component (own file, e.g. `internal/web/service/remote_table_cache.go`),
injected into `SQLLSPService` as an **optional** dependency:

```
type RemoteTable struct { Schema, Name, QualifiedName string }

type RemoteTableProvider interface {
    // Returns the last-known tables for (connection, environment) immediately,
    // never blocking. Triggers a background refresh when the entry is missing
    // or stale. Returns nil when discovery is disabled/unavailable.
    Tables(connection, environment string) []RemoteTable
}
```

- Backing cache keyed by `(connection, environment)`, each entry `{tables,
  fetchedAt, refreshing}`, guarded by a mutex/`sync.Map`.
- `Tables()` returns the cached slice synchronously (possibly empty/stale) and,
  if missing or older than a TTL (say 60s), spawns **one** background refresh
  (dedup via the `refreshing` flag).
- Refresh calls the existing discovery (`SQLService.Tables` /
  `fetchObjectsForConnection`) under a short `context.WithTimeout` (say 3–5s).
  Failures/timeouts are logged and leave the previous entry intact (or empty);
  they never surface to the caller.
- `SQLLSPService` holds `RemoteTables RemoteTableProvider` (nil = feature off).
  Nil / empty results ⇒ asset-only behavior, unchanged. This is the whole
  "decoupled, degrades gracefully" contract.

### 2. Wire discovery into the graph per request

In `SQLLSPService.graphForRequest` (or a thin wrapper around it), after building
the revision-cached asset graph:

1. Resolve the target asset's **connection + environment** (server-side
   `pipeline.GetConnectionNameForAsset` + the selected environment).
2. `remote := deps.RemoteTables?.Tables(conn, env)` — non-blocking.
3. Append a `RelationNode` per remote table **whose qualified name is not
   already an asset relation** (asset precedence on collision). Tag them so they
   can be ranked below assets — e.g. a `Kind: "remote"` marker on `RelationNode`
   or a separate `graph.RemoteRelations` slice.

The asset graph stays revision-cached; only the (small) remote overlay is added
per request, so we don't pollute or churn the shared cache.

### 3. Ranking + dedup (asset precedence)

- **Collision:** skip a remote table whose lowercased qualified name equals an
  existing asset relation → the asset relation is the only completion, and
  `resolveRelation` keeps resolving to the asset.
- **Ranking:** give remote-relation completions a SortText that sorts *after*
  asset/relation completions (extend `relationCompletions` /
  `relationCompletionsInSchema` to emit a lower rank for remote-tagged nodes).
  Result: `analytics.customers` (asset) always appears above `public.events`
  (remote) in the popup.

### 4. Side effect: fewer false unresolved-relations (optional, gated)

If remote relations are in the graph, `resolveRelation` also resolves them, so
`select * from public.events` stops being flagged "Unresolved relation" when the
table really exists remotely. This is desirable but couples diagnostics to
discovery. Recommend gating it: only treat remote relations as
resolution-satisfying, never as a source of *new* diagnostics, and keep the
current behavior when discovery is unavailable. (Decision below.)

### 5. Client cleanup

Once the LSP owns remote-table completion, remove the client-side remote-table
merge in `use-sql-lsp.ts` (the `isTableCompletionContext` + `loadRemoteTables`
block) so there is a single source of truth. The client keeps `/api/sql/tables`
for the settings/catalog UIs.

## Resilience checklist

- Provider nil / not configured → asset-only, no errors. (tests, misconfigured
  projects, connections without discovery support.)
- Discovery query errors / times out / connection down → cached-or-empty, logged
  once, completions unaffected.
- Cold cache (first keystroke) → returns empty immediately, refreshes in the
  background; remote tables appear on a later keystroke. Never blocks.
- Large warehouses → cap the number of remote relations injected (e.g. top N by
  name match against the typed prefix, computed in the provider or the overlay
  step) so we don't push thousands of items per request.

## Open questions / decisions

1. **Diagnostics coupling (§4):** let remote tables satisfy unresolved-relation
   checks, or keep completion-only for now? (Lean: completion-only first, add
   resolution behind the same provider once it's proven.)
2. **Schema/db scoping:** discovery is per database; `SQLService.Tables` takes a
   `databaseName`. Which database(s) do we enumerate for an asset's connection —
   the default, or all? Affects payload size and the `schema.` completion set.
3. **TTL / limits:** 60s TTL and 3–5s refresh timeout are guesses; confirm.
4. **Where the provider lives:** `service` package (reuses `SQLService`/
   `ExecutionService`) vs a new small package. Reuse is simpler; a package makes
   the "decoupled" boundary explicit.

## Rollout steps

1. `RemoteTableProvider` + cache (own file) with unit tests (TTL, dedup,
   error/timeout → empty, single-flight refresh).
2. Add the optional dep to `SQLLSPService`; overlay remote relations in
   `graphForRequest`; tag + rank them below assets. Engine unit tests:
   asset-precedence collision, remote-below-asset ranking, empty provider ⇒
   unchanged output.
3. Live e2e: a fixture with a materialized table that has no asset →
   `from <schema>.` and partial prefixes complete it, ranked under assets.
4. Remove the client-side remote-table merge; keep `/api/sql/tables` for UIs.
5. Fold the as-built summary into `architecture/` (SQL intellisense doc) and
   delete this plan.
