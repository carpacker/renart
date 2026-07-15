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
(`service.ScaffoldProject`: `demo:chess` — native `type: api` Chess.com
profiles and games feeding SQL performance and opening analysis,
`demo:retail` — offline SQL-only demo, `empty`, `bare` for the import
flow) — pipeline files, a `duckdb-default` connection, default .gitignore
patterns, `.renart/project.yml` identity, and `git init` + an initial commit
when the target has no repository — then opens/registers the project and
refreshes its workspace. `GET /api/projects/templates` lists the templates
for the welcome UI. The process-level `/api/projects/browse` directory picker
uses the same default-parent resolution as project creation, and
`POST /api/projects/directories` creates one visible child folder selected by
the user. `.renart/project.yml` also carries project-scoped feature
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
SQLite writes and the UI's staleness/run history updates live. DuckDB access
is additionally serialized per canonical database file as described in §4,
because one server can run multiple pipelines and child processes concurrently.
For an asset target, `--refresh-upstreams` first invokes the server-side stale
planner narrowed to that asset's transitive upstream closure; only non-fresh
upstreams run, with their configured strategies, and a failed refresh prevents
the requested asset from starting. The same planning and execution services run
directly in embedded mode. Pipeline targets reject the flag because their normal
run already covers the whole DAG.
Embedded mode boots the same
graph headless (`serverConfig.headless`: no static assets, watcher, or
fingerprint pre-warm) and **never starts the River scheduler** (two
schedulers on one state DB would duplicate runs); run facts still land in
`.renart/state.db`. The visible command surface is pinned by
`cmd/root_test.go`.

**Release parser build cache.** Release and live-E2E builds compile Bruin's
`bruinsqlparser` Rust static library before linking the Go binary. The build
script keeps Cargo's target directory outside the Go module cache at
`${XDG_CACHE_HOME:-$HOME/.cache}/renart/rustsqlparser/target`; CI caches that
directory and the containerized GoReleaser jobs mount it into the build
container. `RENART_RUSTSQLPARSER_TARGET_DIR` provides an explicit override for
local or hermetic builders. Cargo still owns dependency and source invalidation,
so unchanged parser builds reuse the release artifacts instead of recompiling
the dependency graph for every target.

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
enforce a single-SELECT boundary. The scheduler executes deployed snapshots
materialized to a temp dir, never the working tree (see staleness.md §5).

Before either a pipeline or a single asset starts, the direct runner validates
declared dependencies across the whole parsed pipeline. Non-URI dependencies
must resolve to another asset; a missing edge produces the same
`dependency-exists` diagnostic as Bruin and stops before connections or task
execution begin. The same validator feeds pipeline type-check findings. Runtime
output is one stdout/stderr stream: lifecycle messages remain run-scoped, while
task output passes through a line-aware asset writer that adds a timestamp,
deterministically colored asset label, and `>>` marker to every logical line.

Local DuckDB files use the coordinator in `internal/web/duckcoord`. Connection
paths are made absolute, symlink-resolved, deduplicated, and sorted before an
exclusive lease is acquired. A process-local keyed lock serializes goroutines
and a per-user advisory file lock serializes separate Renart processes. The
parent keeps the lease for the entire database-touching phase, including the
lifetime of Sling and ingestr children; API fetching and Python computation
stay outside that phase. Loads that read and write two DuckDB files acquire
both in sorted order. Waiting is context-cancellable, and an OS-released file
lock makes a killed process recover without stale lock cleanup. Independent
external programs do not participate in the advisory protocol, so inspect
retains bounded retry and a clear DuckDB lock error as a defensive fallback.

The scheduler is built on River with the SQLite driver: `Store` owns
persistence/migrations, `Service` owns orchestration (catch-up windows,
uniqueness via `river:"unique"`), and execution is injected as a plain
`Runner` function. One filesystem lock owns both queue consumption and schedule
registration; startup acquires it before River workers start. It then
atomically fails runs left open by a killed process, cancels the corresponding
abandoned River pipeline/housekeeping jobs, and replays persisted terminal
steps into derived freshness state without rerunning asset code. Runs are
linked to their River job IDs, including recovery from the job arguments if a
process dies during the claim/link handoff; queued jobs River never claimed are
preserved. Recovery emits one structured count summary for operational
visibility (see staleness.md §3).

HTTP API assets use a native streaming extractor followed by Sling for the
warehouse write. The target DuckDB lease is acquired after extraction and held
until Sling exits. OpenAPI inference, pagination, validation warnings, and
HTTP API extraction and execution-window behavior are documented in
[http-api-assets.md](http-api-assets.md).

Load assets use one canonical `.asset.yml`: the top-level `connection` is the
target (or omitted for the pipeline default), while `source_connection` and
`source_table` live under flat `parameters`. A database target always writes to
the asset's canonical name; file and object-storage targets instead require
`parameters.destination_object`. The asset's `materialization` is the only load
strategy source. Renart invokes Sling from those semantic fields directly; no
replication sidecar or parallel destination/mode parameter set exists. A
run-scoped full refresh temporarily selects Sling's replace mode without
rewriting the asset definition; `refresh_restricted` assets keep their
configured strategy and surface a warning instead.

Seed and sensor authoring is likewise semantic rather than raw-file templating.
The workspace DTO's backend-owned `asset_capabilities` list is the runtime
contract for the concrete Bruin types Renart can create, their compatible
connection types, required/default parameters, and seed file support. Seeds can
reference a workspace file selected from the workspace-root picker or an
HTTP(S) URL, or accept a binary-safe multipart upload. A picker selection is a
request-only workspace-relative path; the canonical YAML stores it relative to
the new `.asset.yml` definition. Uploaded files are staged beside that
definition and marked with `meta.renart_seed_file` so deleting the asset removes
only the file Renart owns. Sensor definitions use flat typed parameters for
query, table, or S3-key conditions plus `poke_interval` and `timeout`.

Every advertised seed main task runs through Renart's Sling operator, separately
from generic ingestr assets. The operator resolves local sources relative to the
asset definition (or accepts HTTP(S)), supplies an explicit source format, and
full-refreshes the canonical asset name through the resolved target connection.
With `enforce_schema` and declared columns, it also passes the source selection,
renames, and supported type casts to Sling. The normal per-warehouse column and
custom checks still run around that main task. Sensor main tasks use Bruin's
native sensor operators: interactive runs default to one bounded check, while
scheduled runs retain dependency-gate semantics and wait until success or the
configured timeout. HTTP and CLI execution can explicitly select
`sensor_mode=once`, `wait`, or `skip`; the backend normalizes and enforces the
effective mode rather than relying on UI behavior.

Python assets run through Renart's in-process operator
(`service/python_operator.go`). Each task receives an embedded, version-locked
`renart` SDK wheel and a token-scoped loopback broker (`internal/web/pybroker`).
The same deterministic wheel is published to PyPI as `renart-sdk` on stable
Renart release tags for external editors and CI. Release builds inject one
version into both artifacts; runtime execution still uses the wheel assembled
inside the Renart binary, so a network lookup or separately installed SDK can
never introduce runner skew.
SDK queries stay read-only and execute through the Go connection manager, so
credentials never enter Python. `internal/web/runstate` lets queries wait for
in-flight same-environment materializations and rejects same-run ordering
deadlocks. `materialize()` results stage as Parquet, then load natively through
the DuckDB materializer or through Sling for other warehouses; the Python path
does not use ingestr. `query()` returns a PyArrow Table by default; callers can
convert explicitly with `.to_pandas()` or request `format="pandas"`. The SDK's
`.pyi` files are also mounted into the embedded Python language server, and
pipeline type-check warns when a literal `query()` reads a project asset missing
from `depends`. Before a pyproject-backed run, the operator compares the project
environment and uv cache filesystems. If they differ and the user has not set a
cache or link policy, that invocation selects uv's copy mode up front; same-
filesystem runs retain uv's faster default linking behavior.
Notebook Python cells use the same operator in collection-only mode: broker
queries run against the notebook's already-open live session and the resulting
Parquet file is loaded directly into that session, without input or output
DuckDB staging databases.

Workspace asset DTOs carry a backend-owned materialization capability profile
derived from the concrete asset type and destination. It is the contract used by
both metadata editors: warehouse-specific exclusions and field requirements are
not duplicated as frontend asset-family heuristics. Dedicated runtimes without
the generic SQL/Python/loader contract expose no generic materialization editor;
hand-authored advanced SQL strategies remain visible as custom values instead of
being mislabeled as replace. The DTO and patch API round-trip the complete guided
materialization block (`strategy`, incremental key, time granularity, partition,
and cluster expressions). `time_interval` requires both a key and `date` or
`timestamp`; partition/cluster controls appear only for warehouse materializers
that consume them. Hand-authored DDL/SCD/Data Vault strategies are checked against
the concrete Bruin warehouse support matrix even though their larger contracts do
not yet have guided controls.

Full refresh is a run-scoped execution option shared by SQL, Python, Load, and
API table assets. Direct SQL materializers are constructed with that option for
the individual run; Python and loader-backed assets apply the equivalent
replace behavior at their write boundary. Athena, Databricks, and ClickHouse's
multi-statement materializers select the full-refresh or configured renderer per
asset so a restricted sibling in the same run keeps its saved strategy. DDL,
other hand-authored advanced modes, query-only modes, and
asset-level or selected-environment full-refresh restrictions do not advertise
the action in the workspace DTO. The direct runner applies Bruin's environment
restriction to every parsed asset before dispatch, and backend policy checks
remain authoritative even when a client supplies the request directly.

Explicit asset backfill is a separate run-scoped option. It requires a complete
start/end range, a single-asset scope, and `matlog.BackfillSafe` (currently SQL
`time_interval` and window-aware API merge with a primary key). Full refresh and
backfill are mutually exclusive. Both are destructive policy operations; the
backend revalidates capability, range, scope, and typed environment confirmation
rather than trusting the UI.

Pipeline type checks also validate declared dependency existence and
materialization configuration: supported strategies, required merge primary
keys, active incremental/update keys, and time-interval prerequisites.
Warehouse-inactive partition/cluster metadata is preserved and reported as a
warning. Editing may temporarily persist an incomplete merge so multi-step form
changes are possible; type check and execution surface the incomplete state
until it is resolved. Metadata belonging to another strategy is preserved as
dormant state and reported as a warning rather than blocking an otherwise valid
asset.

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
  (scheduler `Stop()` drains River, then escalates to context cancellation if
  workers do not stop within the grace period).

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
