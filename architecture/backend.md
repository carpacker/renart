# Renart Go backend — current architecture

Status: current state. Originated as an architecture review (2026-06); the
refactor items from that review are done except where noted in §6.

## 1. Shape

```
main.go → urfave/cli commands (cmd/)
  web         run the HTTP server against a workspace root
  standalone  same server + native window via the renart-gui helper binary
  fp          print a pipeline's fingerprint DAG (debug)
  deploy      snapshot a pipeline for scheduled execution
  type-check  render + type-check a pipeline's assets
  warm        pre-warm the wasm compile / format caches

cmd/server.go  flags → serverConfig → wiring (services, watcher, scheduler)
cmd/web.go     route registration + a thin webServer adapter
  ├── internal/web/httpapi        HTTP handlers, one file per domain
  ├── internal/web/service        domain logic (asset CRUD, execution,
  │                               intelligence, onboarding, config, …)
  ├── internal/web/scheduler      River + SQLite scheduler
  ├── internal/web/events         SSE pub/sub hub with debounce
  ├── internal/web/watch          fsnotify/poll filesystem watcher
  ├── internal/web/{model, api}   canonical DTOs, response envelope
  ├── internal/web/{bus, identity, fingerprint, matlog, staleness,
  │                snapshot, policy}          → see staleness.md
  ├── internal/web/notebook                   → see notebooks.md
  ├── internal/web/service/assetmeta          → see asset-editing.md
  ├── internal/web/{sqlintelligence, pyintelligence, sqlformat,
  │                freshness, profiling, static}
  └── Bruin packages (github.com/bruin-data/bruin) — parsing, config,
      connections, per-warehouse materializers, execution operators
```

Layering: transport (`httpapi`) → domain (`service`) → Bruin. Handlers are
mechanical decode → delegate → encode; each `httpapi` file declares the narrow
consumer-side interface it needs (`AssetHandlers`, `SchedulerHandlers`, …) and
is pointed directly at the owning service.

## 2. Runtime model

The **filesystem is the source of truth**. One server process serves one
workspace root (positional CLI argument). A watcher (`internal/web/watch`)
triggers full workspace re-parses through the `WorkspaceCoordinator`; the
resulting state is pushed to all clients over a single SSE endpoint
(`/api/events`). The hub (`internal/web/events`) uses buffered per-client
channels with non-blocking drop-on-slow sends, debounce-with-coalescing for
watcher noise, and `PublishImmediate` for handler-triggered events.
Self-write suppression (a short window in `WorkspaceCoordinator`) prevents
the server's own file writes from echoing back as change events.

Concurrent file writes are serialized by a per-file lock in the asset write
path (fast successive edits used to race read-modify-write cycles and drop
content).

## 3. Persistence

All durable state lives in SQLite at `.renart/state.db` inside the workspace
(WAL mode, `busy_timeout=5000`), shared between River's job tables and
renart's own `renart_*` tables, migrated by a goose runner. Renart-specific
per-environment policy lives in `.renart/environments.yml`; everything else
users author is plain Bruin files (`.bruin.yml`, `pipeline.yml`, asset files).

## 4. Execution

`BruinCommandExecutor` is a hybrid: a **direct** in-process path that drives
Bruin's operator/materializer packages (registered per warehouse in
`service/direct_executor_registry.go`), with a **CLI fallback** so behavior
matches the `bruin` binary where the direct path can't. Inspect-style queries
force read-only semantics (single-SELECT check + `access_mode=read_only` for
DuckDB paths). The scheduler executes deployed snapshots materialized to a
temp dir, never the working tree (see staleness.md §5).

The scheduler is built on River with the SQLite driver: `Store` owns
persistence/migrations, `Service` owns orchestration (catch-up windows,
uniqueness via `river:"unique"`), and execution is injected as a plain
`Runner` function.

HTTP API assets use a native streaming extractor followed by Sling for the
warehouse write. OpenAPI inference, pagination, validation warnings, and
HTTP API extraction and execution-window behavior are documented in
[http-api-assets.md](http-api-assets.md).

## 5. Conventions

- **One DTO set.** `internal/web/model` owns workspace DTOs, `service` owns
  request/response DTOs, `httpapi` re-exports aliases. When a Go DTO changes,
  `web/scripts/generate-api-types.mjs` must regenerate
  `web/lib/generated/api-types.ts` — it parses the Go structs in
  `internal/web/model/dto.go`.
- **One error type.** `service.APIError` (`{Status, Code, Message}`) with
  sentinel errors + `errors.Is/As` at service boundaries; one `api.Response`
  envelope.
- **Middleware** (`httpapi/middleware.go`): zap request logging, panic
  recovery, and an Origin/Host guard on state-changing requests (loopback
  origins are trusted so the Vite dev proxy works). SSE keeps the write
  timeout off; read/idle timeouts are set.
- **Path safety.** All asset/pipeline ID decoding funnels through
  `WorkspaceResolver.SafeJoin`.
- **Deployment.** Single binary: embedded frontend, embedded Python (uv),
  pure-Go SQLite. Port fallback, browser auto-open, graceful shutdown
  (scheduler `Stop()` drains River).

## 6. Embedded engines & memory

SQL intelligence (parse/lineage/validation) runs on an embedded wasm build of
sqlglot (`sqlintelligence`, "polyglot" engine); Python intelligence runs ty as
wasm (`pyintelligence`); SQL formatting is a wasm formatter (`sqlformat`).
All run under wazero with an on-disk compilation cache (`renart warm`
pre-warms it). RSS is dominated by these engines; retiring the interpreter
fallback + the disk cache brought idle memory to roughly 360 MB.

## 7. Open items

- `internal/web/service` is a large flat package (asset CRUD, execution,
  intelligence, onboarding, suggestions in one namespace). Sub-packages would
  make boundaries legible (`assetmeta` and `notebook` already split out).
  Opportunistic, not urgent.
- Every file event triggers a full workspace re-parse + full-state broadcast.
  Fine at current scale behind the debounce; `Revision` exists if incremental
  diffs are ever needed.
- `WorkspaceCoordinator.CurrentState()` returns aliased slices/maps; consumers
  are read-only today but nothing enforces it.

Verification for backend changes: `go build ./...`, `go vet ./...`,
`go test ./...`, and the live e2e suite (`corepack pnpm test:e2e:live` in
`web/`) at major checkpoints.
