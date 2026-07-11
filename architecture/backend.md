# Renart Go backend — current architecture

Status: current state. Originated as an architecture review (2026-06); the
refactor items from that review are done except where noted in §6.

## 1. Shape

```
main.go → cmd.Root() → urfave/cli commands (cmd/)
  web         run the HTTP server against a workspace root       (IDE)
  standalone  same server + native window via renart-gui helper  (IDE)
  run         run a pipeline or asset; delegates to a live
              server, else executes in-process                   (Pipeline)
  ls          list pipelines/assets                              (Pipeline)
  deploy      snapshot a pipeline for scheduled execution        (Pipeline)
  type-check  render + type-check a pipeline's assets            (Pipeline)
  init        scaffold a project from the welcome templates      (Project)
  debug       hidden group: fp (fingerprint DAG), sql-lsp
              (stdio LSP), warm-cache (wasm compile caches)

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

**Projects.** One process hosts many projects: a global registry
(`~/.config/renart/projects.json`; `RENART_PROJECTS_REGISTRY` overrides for
tests) plus one lazily-opened per-project runtime each, mounted at
`/api/projects/{id}/*` (`cmd/projects.go`); the argv root stays aliased at the
unprefixed `/api/*`. `POST /api/projects` scaffolds a project from a template
(`service.ScaffoldProject`: `demo:chess` — native `type: api` chess.com
assets, `demo:retail` — offline SQL-only demo, `empty`, `bare` for the import
flow) — pipeline files, a `duckdb-default` connection, default .gitignore
patterns, `.renart/project.yml` identity, and `git init` + an initial commit
when the target has no repository — then opens/registers the project and
refreshes its workspace. `GET /api/projects/templates` lists the templates
for the welcome UI. `.renart/project.yml` also carries project-scoped feature
flags (`internal/web/identity`): `features.ingestr` re-enables the ingestr
surfaces the UI hides by default — `/api/config` filters ingestr source
connection types out unless the flag is set, and the frontend
(`web/lib/features.ts`) additionally shows them when the workspace already
contains ingestr assets.

**CLI ↔ server (delegate-or-embed).** Pipeline commands resolve their
workspace git-style (walk up to `.bruin.yml` → `.renart` → repo root;
`cmd/workspace.go`) and their target as a pipeline name, asset name, or
path. `renart run` then delegates to a live server when one has the
workspace open: servers write `.renart/server.json` (pid, project-mount API
base, session token) into every open project root — removed on graceful
shutdown; `web`/`standalone` trap SIGINT/SIGTERM for exactly this — and
expose `GET /api/health`. `internal/clientapi` reads the file, health-checks
it fast (a stale file falls back to embedded mode in under a second,
comparing symlink-resolved roots), and streams the same materialize SSE
endpoints the UI uses, authenticating with the token
(`SameOriginGuardWithToken`; `RENART_SERVER`/`RENART_TOKEN` pin a server,
`--local` forces embedded). Delegation means one process owns all
DuckDB/SQLite writes — DuckDB is single-writer per file across processes —
and the UI's staleness/run history update live. Embedded mode boots the same
graph headless (`serverConfig.headless`: no static assets, watcher, or
fingerprint pre-warm) and **never starts the River scheduler** (two
schedulers on one state DB would duplicate runs); run facts still land in
`.renart/state.db`. The visible command surface is pinned by
`cmd/root_test.go`.

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

Pipeline type checks also validate materialization configuration: supported
loader strategies, required merge primary keys, declared incremental/update
keys, time-interval prerequisites, and merge-only column metadata. Editing may
temporarily persist an incomplete merge so multi-step form changes are possible;
type check and execution surface the incomplete state until it is resolved.

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

SQL intelligence (parse/lineage/validation) and formatting run on the embedded
Polyglot SQL wasm engine (`sqlintelligence`, `sqlformat`); Python intelligence
runs ty as wasm (`pyintelligence`).
All run under wazero with an on-disk compilation cache (`renart debug warm-cache`
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
- Project runtimes are opened lazily but never evicted: each open project
  keeps its watcher, SQLite pool, and scheduler alive for the life of the
  process. Idle eviction (close after N hours unused, keep the registry
  entry) is the planned cap if footprint becomes a problem.

Verification for backend changes: `go build ./...`, `go vet ./...`,
`go test ./...`, and the live e2e suite (`corepack pnpm test:e2e:live` in
`web/`) at major checkpoints.
