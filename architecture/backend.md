# Renart Go backend — current architecture

Status: current state. Originated as an architecture review (2026-06); the
refactor items from that review are done except where noted in §7.

## 1. Shape

```
main.go → cmd.Root() → urfave/cli commands (cmd/)
  web         run the HTTP server against a workspace root       (IDE)
  standalone  same server + native window via renart-gui helper  (IDE)
  run         run a pipeline or asset; delegates to a live
              server, else executes in-process                   (Pipeline)
  plan        preview a pipeline execution plan; delegates to a
              live server, else plans read-only in-process       (Pipeline)
  render      preview one saved asset without execution; delegates
              to a live server, else renders read-only in-process (Pipeline)
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

The **filesystem is the source of truth**. Each project runtime serves one
workspace root; one process can host several such runtimes. A watcher
(`internal/web/watch`) triggers full workspace re-parses through the
`WorkspaceCoordinator`; the resulting state is pushed to all clients over a
single SSE endpoint (`/api/events`). The hub (`internal/web/events`) uses
buffered per-client channels with non-blocking drop-on-slow sends,
debounce-with-coalescing for watcher noise, and `PublishImmediate` for
handler-triggered events. Self-write suppression (a short window in
`WorkspaceCoordinator`) prevents the server's own file writes from echoing
back as change events.

Concurrent file writes are serialized by a per-file lock in the asset write
path (fast successive edits used to race read-modify-write cycles and drop
content).

**Projects.** One process hosts many projects: a global registry
(`~/.config/renart/projects.json`; `RENART_PROJECTS_REGISTRY` overrides for
tests) plus one lazily-opened per-project runtime each, mounted at
`/api/projects/{id}/*` (`cmd/projects.go`); the argv root stays aliased at the
unprefixed `/api/*`. Before opening a workspace state database, every
non-headless runtime acquires an authoritative per-user lease outside the Git
worktree, keyed by the canonical (absolute, symlink-resolved) workspace root.
The default location is under `XDG_RUNTIME_DIR`, falling back to the user cache;
`RENART_WORKSPACE_LOCK_DIR` overrides it. Current processes also acquire the
legacy `.renart/server.lock` as a compatibility lock for older versions and
users with different runtime directories. One process may hold many workspace
leases, but another `web` or `standalone` process cannot open a workspace
already mounted by the first. Keeping the primary lease outside the worktree
means `git clean` cannot unlink the authoritative lock while its process is
running. A bounded health check of `.renart/server.json` supplies the owning
PID/URL and detects live Renart versions from before either lock existed. The OS
releases both leases on graceful close or process exit; persistent unlocked
files are inert. Embedded CLI execution does not take this long-lived-server
lease.

This exclusivity is the supported product topology, not a precursor to leader
election. A workspace has one long-lived authority for its state database,
watcher, SSE hub, River client, and scheduler registrations. The scheduler's
`follower` state remains a fail-closed compatibility safeguard for old or
misconfigured processes; Renart does not provide heartbeat takeover,
cross-process schedule mutation, or hot standby for one workspace.

At startup, Renart adds its state database, discovery file, and compatibility
locks to the repository-local `.git/info/exclude` rather than modifying the
user's tracked `.gitignore`. Source-control status also hides only untracked
runtime artifacts, so an accidentally tracked runtime file remains visible.
Failure to update Git metadata is a warning and does not prevent the workspace
from opening; the out-of-worktree lease remains authoritative.
`POST /api/projects` scaffolds a project from a template
(`service.ScaffoldProject`: `demo:chess` — native `type: api` Chess.com
profiles and games feeding SQL performance and opening analysis,
`demo:retail` — bundled CSV seed assets feeding an offline SQL demo, `empty`,
`bare` for the import
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
The same tracked project file carries the local history-retention policy.
`/api/config` always returns its effective values, using conservative defaults
when the block is absent, and Project settings writes the complete validated
policy back as a reviewable Git diff. Invalid explicit retention values stop
startup instead of allowing a destructive housekeeping guess.

**CLI ↔ server (delegate-or-embed).** Pipeline commands resolve their
workspace git-style (walk up to `.bruin.yml` → `.renart` → repo root;
`cmd/workspace.go`) and their target as a pipeline name, asset name, or
path. `renart run`, `renart plan`, and `renart render` delegate to a live server
when one has the workspace open: servers write `.renart/server.json` (pid,
project-mount API
base, session token) into every open project root — removed on graceful
shutdown; `web`/`standalone` trap SIGINT/SIGTERM for exactly this — and
expose `GET /api/health`. `internal/clientapi` reads the file, health-checks
it fast (a stale file falls back to embedded mode in under a second,
comparing symlink-resolved roots), and authenticates with the token
(`SameOriginGuardWithToken`; `RENART_SERVER`/`RENART_TOKEN` pin a server,
`--local` forces in-process behavior). A delegated run streams the same
materialize SSE endpoints the UI uses, so one process owns all SQLite writes
and the UI's staleness/run history updates live. A delegated render calls the
same read-only pipeline-owned asset-render endpoint as the Build editor when a
deployment source is requested; ordinary working-tree rendering retains the
asset-ID convenience endpoint so incomplete saved definitions can still be
addressed by path. A delegated plan calls the same pipeline-plan endpoint as
the review sheet. Without a live server, working-tree `renart render` invokes
the shared read-only service directly and does not open scheduler state;
deployment rendering and `renart plan` boot the same headless service graph
needed for local snapshot/staleness state, but never start River or execute an
asset. DuckDB access
is additionally serialized per canonical database file as described in §4,
because one server can run multiple pipelines and child processes concurrently.

`renart ls` also prefers the server's parsed workspace. The handoff is not yet
universal: `deploy` opens the state store directly, `type-check` and some debug
commands still run locally, stateful embedded commands skip the long-lived
workspace lease, and `--local` can bypass discovery. A second `web` or
`standalone` launch currently exits with owner details rather than opening the
existing server. These are bounded single-authority handoff gaps, tracked in
`plans/pipeline-readiness-and-rendering.md`; they are not a supported
multi-server mode.

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

**Binary composition.** Release builds are deliberately self-contained: the Go
link includes Bruin's native Rust SQL parser and the complete connector/runtime
graph, while `go:embed` carries the web application, Monaco workers, the
Polyglot SQL WASM engine, and the `ty` Python-intelligence WASM engine. GoReleaser
uses `-s -w`, which removes Go debug sections but not those runtime components.
Consequently the executable remains large even when stripped; connector
dependencies and generated lookup/type data are a larger share than the visible
web bundle alone. The DuckDB ADBC driver itself is installed into the runtime
cache rather than embedded in the executable.

## 3. Persistence

Runtime state lives in SQLite at `.renart/state.db` inside the workspace (WAL
mode, `busy_timeout=5000`), shared between River's job tables and Renart's own
`renart_*` tables, migrated by a goose runner. The SQLite connection encodes
bound Go timestamps as canonical UTC SQLite values; this is required because
River orders due jobs with its exact space-separated timestamp representation.
Renart-specific project files
include per-environment policy in `.renart/environments.yml` and desired
per-environment schedules in `.renart/schedules.yml`; ordinary pipeline source
remains plain Bruin files (`.bruin.yml`, `pipeline.yml`, asset files). Schedule
deployment pins, watermarks, occurrence/run history, and derived next-run times
are machine-local SQLite state and never enter the version-controlled schedule
declaration.
Runtime lock/discovery files, including `.renart/execution.lock`, are excluded
from Git by the source-control service. Its diff endpoint resolves the same
HEAD/index/worktree pairs used for staging and returns both the unified patch
and original/modified text for the frontend's inline diff editor; binary
contents are detected before serialization and represented only by a binary
marker.

## 4. Execution

Definition readiness and data freshness are intentionally independent. A
deployment snapshots reviewed source but does not build data; a Build-needed or
ordinary working-tree run can update data without changing any deployment or
schedule pin. The common execution lifecycle is:

```text
saved source ──► render / pipeline plan ──► confirm ──────────────┐
      │                                                           │
      └──► deployment review ──► snapshot ──► pin ──► runtime plan┤
                                                                  v
                                                       run + RunSpec + units
                                                          /             \
                                             inline stream               River job
                                                          \             /
                                                           v           v
                                                      Bruin/direct execution
                                                                  │
                                                                  v
                                                 completion outbox ──► facts/staleness
```

Every user-facing non-dry mutating execution is admitted to the same durable
run ledger before physical work starts. Dispatch may be inline (to preserve
interactive SSE output) or queued through River; it does not change the source,
context, provenance, or execution-unit contract.

`BruinCommandExecutor` is a hybrid: a **direct** in-process path that drives
Bruin's operator/materializer packages (registered per warehouse in
`service/direct_executor_registry.go`), with a **CLI fallback** so behavior
matches the `bruin` binary where the direct path can't. Inspect-style queries
enforce a single-SELECT boundary. Pipeline-wide queued runs carry a server-owned
manual/scheduled origin and an explicit source: ordinary Build runs use the
saved working tree, `deployed_only` Build runs resolve one exact deployment at
admission, and scheduled runs use their exact row pin. Snapshot execution
validates ownership and content before materializing a fresh temp directory;
resolution failures fail the run rather than falling back to the working tree
(see staleness.md §5).

Recognized imported `*.source` assets keep Bruin's registered no-op main/check
semantics on the direct path. They are dependency anchors for externally owned
relations, not transformations to execute. An unknown custom `.source` type is
not inferred to be safe and still takes the unsupported/CLI-fallback path.

Pipeline planning is read-only at `POST /api/pipelines/{id}/plan` and through
`renart plan`. It resolves an explicit saved-working-tree or deployment source,
environment, interval, execution timestamp, sensor/full-refresh/backfill mode,
and one of `all`, `needed`, asset-closure, all selector matches, or needed
selector matches. Custom expressions are resolved by Bruin's selector engine;
Renart intersects the result with its own Needed state only for the explicit
needed-matches mode. The planner combines the
current code-check report, target-aware staleness and exact uncovered gaps,
topological order, one asset-by-window execution-unit list, render stages, and
structured blockers/warnings. Stage metadata is always returned; large
redacted content blobs are omitted by default and can be requested lazily
without changing the plan identity. The plan ID binds its source Merkle root,
selected-configuration and variable identities, data-state token, context,
selection, structured write-resource claims, and operation graph. Local files
and whole DuckDB database files can be exact; unproven operators remain
pipeline-conservative. Incomplete source remains visible through structured
findings and honest fidelity rather than fabricated SQL.

The same endpoint accepts a distinct `deployment` purpose for reviewing the
entire saved working tree before Deploy. That purpose is definition-only: it
does not consult data freshness, reserve a run slot, or apply protected/
`deployed_only` execution policy. Deterministic code/render failures still
block the review, while an unavailable secret-free configuration identity is
advisory. Deployment-purpose requests require `working_tree` plus `all`, and
the run-confirm endpoint rejects them; they cannot be reused as an execution
policy bypass.

`POST /api/pipelines/{id}/plan/confirm` admits all supported execution
selections, including custom selector matches. The normalized selector and its
final explicit units are both retained in the durable plan. The server
regenerates the plan from the submitted canonical request at
the reviewed execution timestamp; rendered content supplied by the browser is
never trusted. A changed source, configuration, context, or operation returns
`409 plan_stale` with the replacement plan. Needed selection has one narrower
data-state rule: units that became fresh may be omitted, and the durable preview
records what disappeared, but a new, widened, or otherwise changed unit returns
`409 plan_data_changed` for another review. Blocked, empty, non-exact-source, or
non-exact-configuration plans fail closed. Destructive policy confirmation is
checked before admission and again at execution.

Accepted confirmation atomically persists the immutable redacted plan in
`pipeline_run_plans`, its final ordered asset/window units in
`pipeline_run_units`, the private RunSpec, run row, River job, job link, and run
slot. Snapshot runs verify the requested snapshot and Merkle root; working-tree
runs copy the reviewed pipeline into a fresh isolated run directory and verify
that copy against the same Merkle root before execution. The worker rechecks
selected configuration before the first unit, uses the reviewed execution
timestamp for runtime Jinja rendering, and runs only the admitted assets and
windows through the same hybrid executor used by direct execution. Unit state
is durable (`queued`, `running`, and terminal statuses), emitted as `run.unit`,
and each completed unit has its own replay-safe completion identity while
retaining the parent pipeline run ID. Run details return the retained plan and
unit ledger beside steps, events, and output.

Planning is partial while execution remains strict. If one asset definition is
temporarily invalid, the read-only planner uses the same marked placeholder as
workspace loading, records the parse failure as an asset-scoped code-check
blocker, omits an execution unit for that asset, and still renders valid
siblings. Pipeline-level YAML that cannot establish a pipeline remains a
request error. Incomplete SQL findings likewise do not erase sibling renders;
Python stays explicitly runtime-only where no safe static claim is available.

Asset rendering is a separate read-only path. The working-tree convenience
route is `POST /api/assets/{assetID}/render`; it reads the saved asset identified
by the route and is retained for path-addressable/incomplete definitions. The
source-aware route is `POST /api/pipelines/{id}/assets/render`: it accepts an
asset name plus only a server-owned source selector (`working_tree` or a
snapshot version) and resolves both the pipeline root and asset path itself.
It never accepts a request-supplied filesystem path or editor buffer. Snapshot
source integrity and ownership are checked exactly as for planning. Both routes
use a server-owned preview run ID, strictly parse the environment/window/
execution-time/full-refresh context, and never open a warehouse connection.
The response includes the same source Merkle identity Deploy computes, the
canonical asset/DAG fingerprint, the full-variable coverage hash, the
effective-variable digest with value-free, sorted variable provenance, and a
secret-free canonical identity for the selected environment controls plus only
the named connections the asset can execute against. The fingerprint service
uses the existing staleness engine and a request-local pipeline-ID sentinel for
legacy pipelines, so rendering never writes an ID back to disk. Fingerprint
failure leaves usable render stages intact and adds a sanitized partial
warning. The configuration classifier reads Bruin's public `mapstructure`
schema directly, represents
`sensitive` and `sensitive_file` values only by presence, and never invokes
custom marshalers. Opaque maps/interfaces and non-empty URL, DSN, endpoint, or
raw options fields fail closed as `runtime_only` with an empty digest instead
of risking credential exposure or silently omitting behavior. This
configuration identity is separate from the asset's physical-target identity.
For Load assets the reserved Sling `local` endpoint is not treated as a named
Bruin connection: authored source/destination paths remain in source identity,
and an exact local destination has its own canonical physical-target identity.

The target resolver is also connection-free. It hashes only resolved physical
routing coordinates and the relation/file object; connection aliases,
environment names, principals, and credentials are excluded, and endpoints or
database paths never appear in the response. Local DuckDB/Load files share the
same canonical path and symlink rules as runtime locking. Supported database
families resolve through Bruin's table-name capabilities and require complete,
unambiguous routing context. Schema-prefix rewrites, pre-hooks with an
unqualified target, in-memory/MotherDuck/lakehouse DuckDB, raw routing options,
credential-derived tenancy, non-materialized Python/SQL, and unproven target
families fail closed as `runtime_only` with an empty identity. A Python asset
with declared table materialization resolves the same intended relation as its
Go-side Parquet loader, but its snapshot requires operator write evidence:
coverage is recorded only when the operator durably claims that target just
before loading a returned result. A successful `None` return records the run
attempt without physical coverage. Sensors are exact no-output targets. The
same resolver supplies the versioned pre-execution target snapshot used by
durable latest-writer coverage; exact identities are claimed before physical
writes, while runtime-only targets stay explicitly targetless and cannot claim
cross-run physical freshness.

SQL assets expose the exact compiled query. DuckDB/MotherDuck,
PostgreSQL/Redshift, BigQuery, MySQL, Snowflake, MSSQL, Trino, Vertica, and both
Fabric query aliases additionally expose hook-aware materializer SQL built by
the same factory as direct execution. Databricks, ClickHouse, Synapse, and
Athena expose the complete ordered statement list submitted by their direct
operators without splitting SQL text. BigQuery also applies the same annotation
comment helper before claiming exact SQL. Oracle assets without materialization
expose the exact query submitted by direct execution. Oracle materialization
remains unsupported by that direct path, and declared Oracle hooks produce an
explicit partial-result warning because the runtime does not execute them.
Every stage declares exact, semantic, runtime-only, or unsupported fidelity.
An honest semantic conditional such as connection-local schema preparation is
an `ok` render detail, not a partial-plan warning; only missing, failed,
unsupported, or genuinely runtime-only execution/target information downgrades
readiness.
Runtime column and custom quality checks are separate, named `check` stages.
They come from Bruin's scheduler task instances and the same destination-aware
check-operator registry used by direct execution. Rendering runs those
operators against a query-capturing in-memory connection that returns the
expected synthetic result, so dialect-specific SQL and custom-check Jinja/count
wrapping are exact without opening a warehouse. A malformed check becomes its
own error stage and issue without erasing successfully compiled query or
materialization stages. Asset types whose direct runtime does not expose a real
check operator are reported as unsupported rather than fabricating SQL;
ingestr, Python, API, and Load delegate checks through their resolved target
destination in both paths. Oracle column checks use Bruin's Oracle operator.
Targets without a SQL check operator remain explicitly unsupported. API and
Load use their dedicated HTTP/Sling path only for the main task; check tasks
use the shared sequential registry, and destination-resolved metadata tasks
are explicit no-ops so neither path can repeat the side-effecting main work.
Scheduler-created metadata-push post-tasks are rendered after checks and use the
same explicit PostgreSQL-compatible, BigQuery, and Snowflake asset-type mapping
as direct execution. Warehouse mutations remain `runtime_only`, while
backend-specific skips and validation failures are represented semantically;
unsupported publishers are shown as the no-op the direct registry executes.
Query sensors compile `parameters.query`, never their surrounding YAML, and
show that exact submitted condition query as execution SQL; polling mode,
interval, and timeout remain described runtime controls rather than fabricated
statements. A missing or blank query is an asset-scoped structured error.
Non-SQL assets expose structured JSON operations at their honest fidelity:
seeds describe the Sling load (including enforced casts), Load assets describe
the Sling copy, API assets separate HTTP extraction shape from the Sling JSONL
write, table/S3 sensors describe their condition and runtime controls, and
ingestr assets describe their copy. Python exposes only the saved entrypoint and
declared materialization as `runtime_only`, because user code and SDK calls
determine its actual operations. These semantic stages contain named
connections but never resolved connection URIs or credentials.
MotherDuck uses the DuckDB dialect and exact DuckDB/MotherDuck materializer
path. PostgreSQL/Redshift, MySQL, MSSQL, Trino, Vertica, and Fabric use their
corresponding Bruin materializers through the same shared factory; the current
and legacy Fabric query aliases intentionally share one construction path.
Direct execution and rendering share hook-aware factories for BigQuery and
Snowflake string materializers, Databricks/ClickHouse/Synapse ordered-statement
materializers, and Athena's location-aware ordered statements. The hook wrapper
sits outside refresh-strategy selection, so configured and full-refresh paths
retain the same pre/post hooks. Synapse specifically uses Bruin's Synapse
operator and materializer rather than borrowing MSSQL execution semantics.
Athena rendering reads the selected typed connection's public query-results
path without constructing a live client; missing configuration is an explicit
partial result rather than a fabricated location.

Hook templates are resolved with the selected asset context before
materializer construction by the same request-local helper used for direct
asset and pipeline execution, including each asset's effective full-refresh
restriction. String materializers retain hooks in one execution-SQL blob.
Databricks, ClickHouse, and Synapse expose separate pre/main/post stages only
when the final list is byte-for-byte equal to the unhoisted wrapper order; if
`DECLARE` hoisting moves statements, the exact elements remain available as
generic execution-SQL stages. Athena likewise uses neutral ordered stages
because its hoisted runtime list does not preserve trustworthy provenance.
Schema and destination preparation are emitted only where the direct operator
performs them, with semantic or runtime-only fidelity for live conditional
steps. BigQuery cost/target checks and Snowflake warehouse/target/SCD2 checks
are represented without contacting either warehouse or exposing credentials.
The BigQuery cost guard also precedes query- and table-sensor conditions.
PostgreSQL/Redshift and MySQL string-SCD2 paths expose their live conditional
timestamp-column migration before execution SQL, matching the direct
operator's full-refresh gate.
Databricks three-part targets expose the runtime's ordered catalog-then-schema
preparation, including the same identifier casing.
MSSQL, Trino, and Athena metadata-only DDL are rendered from declared columns
without requiring placeholder SQL. Trino and Athena time-interval
materializations retain the complete post-extraction statement list.
Execution stages whose live developer-environment rewrite needs warehouse
state, or whose materializer generates temporary identifiers, are labelled
`runtime_only` instead of claiming byte-for-byte parity. This includes MSSQL
and Vertica `delete+insert`, plus MySQL `delete+insert`, `merge`, and both SCD2
strategies; the corresponding generated-name paths for BigQuery, Snowflake,
Databricks, ClickHouse, Synapse, and Athena are classified the same way.
Fabric's temporary names are deterministic. Full-refresh classification uses
the environment-level operator mode and the per-asset effective mode at the
same boundaries as direct execution.
Known inline credentials tagged by Bruin are masked before any stage or issue
crosses the HTTP boundary, with explicit redaction metadata. Semantic URL
previews also redact userinfo and credential-like query parameters, and API
previews expose header names/auth shape without values. The preview uses the
read-only config loader, so rendering cannot create `.bruin.yml`, edit
`.gitignore`, or otherwise mutate the workspace. The visible `renart render`
command and Build action both use this same service contract. Render computes
the canonical source manifest by streaming each file through the content hash;
unlike Deploy, it never retains source blobs in memory, which keeps pipelines
with large seed files bounded.

`POST /api/pipelines/{id}/assets/render/compare` renders the saved working tree
and an exact (or server-resolved latest) deployment with one identical resolved
context. It aligns operations by semantic stage identity and occurrence rather
than list position, classifies them as added/removed/changed/unchanged, and
returns both redacted render results. The comparison therefore covers generated
materialization/check/hook operations, not just the authored query text.

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

DuckDB SQL file references have a separate workspace-scoping rule implemented
by `internal/web/duckdbworkspace`. Every DuckDB client returned by the web
server's connection manager sets DuckDB's `file_search_path` in the same
connection/query as the requested SQL, so relative reads such as
`"./data.parquet"` resolve from the active workspace rather than Renart's process
working directory. Notebook session clients use the same wrapper. This avoids a
process-wide `chdir`, which would couple unrelated server filesystem operations
to query execution; non-DuckDB connections are passed through unchanged.

Physical-target recovery uses a separate per-workspace execution coordinator.
Every non-dry `ExecutionService` path holds shared primary and compatibility
OS locks from before target capture through the durable completion hand-off;
startup takes them exclusively before converting orphaned active write claims
to dirty. The primary lock lives in the per-user runtime directory so a
worktree cleanup cannot split the lock domain, while `.renart/execution.lock`
coordinates processes whose runtime-directory settings differ. Pipeline,
asset/scope, Build-needed, delegated/embedded legacy `/api/run`, and onboarding
quickstart materialization all enter through this boundary. Dry-run/render/
inspect do not take the lease.

Successful physical work is handed to a durable SQLite completion outbox before
the request returns. The outbox carries the version-three target/fingerprint and
write-resource snapshot, per-task upstream-writer read sets, terminal
coordinates, and run context needed by materialization facts and staleness.
Version-two envelopes remain readable for rolling compatibility. Subscriber
failure leaves the envelope pending for idempotent startup/housekeeping replay
instead of turning a completed warehouse write into a false execution failure.
Cancelled operators retain a cancelled terminal status through asset events,
the materialization result, and scheduler finalization.

The scheduler is built on River with the SQLite driver: `Store` owns
persistence/migrations, `Service` owns orchestration (catch-up windows,
uniqueness via `river:"unique"`), and execution is injected as a plain
`Runner` function. One filesystem lock owns both queue consumption and schedule
registration; startup acquires it before River workers start. The service
exposes `owner`, `follower`, or `unavailable` ownership through the environment
schedule API. Only an active owner may mutate schedules or queue runs; follower
requests fail with `409 scheduler_not_owner` before deployment, workspace, or
SQLite writes. Under the supported topology, the earlier workspace lease means
this runtime is the only current server able to reach the scheduler. `follower`
is defense in depth, not an automatically promoted standby. Queued runs and
inline mutations persist a private,
validated, versioned `pipeline_run_specs` record outside public run DTOs and
SSE. The spec is the immutable behavior contract; scheduler-backed rows replace
their diagnostic context with effective values immediately before execution,
while inline admission stores the already server-normalized effective context.
Version 1 represents an entire pipeline. Version 2 adds a strict ordered
asset/window selection with canonical workspace-relative paths, inclusion
reasons, and asset-scope provenance; its units are inserted atomically beside
the run and spec.
Plan-confirmed runs additionally retain the reviewed source/config/time
identities, redacted plan artifact, and final unit ledger described above.
Manual/API admission inserts the run, v1 spec, optional confirmed plan and
units, run-ID-only River job, run/job link, and namespaced path plus stable-UUID
slot aliases in one SQLite transaction via `InsertTx`. Stored specs override
parallel legacy job arguments; unknown versions, unknown fields, and row/spec
mismatches fail the run closed. The stable UUID in the private spec is
independently checked against the durable UUID slot before execution, then
threaded through scheduler execution and snapshot resolution; deployment
lookup never has to rediscover that identity from a mutable pipeline path.
Pre-upgrade jobs without a spec retain one strict upgrade decoder.

Synchronous full-pipeline execution uses the same v1 RunSpec with
`dispatch=inline_streaming`. After policy/default/window normalization and
before acquiring the physical executor, Renart atomically creates the run,
private spec, and path plus available stable-UUID slot aliases without a River
job. It then marks the run running before calling Bruin, persists target
identity, steps, and streamed logs through the same scheduler service, and
finalizes through a context-detached write that releases the slot. A second
policy check uses the same normalized context immediately before side effects.
Authenticated discovery-token requests are server-classified as `cli`; other
HTTP execution is `api`, so clients cannot submit their own origin. A crash
fails the jobless inline row and replays only durable terminal provenance—it
never re-executes asset code.

One-click asset and upstream/downstream/neighborhood materialization uses the
v2 selection contract with `dispatch=inline_streaming`. Admission retains the
anchor, scope, ordered asset paths, exact common window, and inclusion reasons,
then creates no River job. Each selected asset has a durable unit transitioned
around its direct executor call independently from the executor's step events;
targets, steps, output, completion, detached terminalization, the pipeline slot,
and the pre-side-effect policy recheck otherwise use the same inline lifecycle.

Actual due/catch-up signals generate the same redacted plan against the row's
exact deployment, environment, normalized interval, and admission-time
execution timestamp. Schedule overrides are validated against declarations in
that immutable deployment, normalized through the same pre-asset Bruin
pipeline mutator used by rendering and execution, and contribute to variable
digests/fingerprints without entering the plan artifact. Their values remain in
private schedule state, the compatibility signal, and the RunSpec; public
schedule responses expose sorted names only. Scheduled sensor semantics are
planned and executed as `wait`.

The worker atomically admits the run, RunSpec, pipeline slot, redacted plan, and
ordered units before physical execution. A deterministic blocker still creates
an auditable failed run with its retained plan but never invokes the executor;
the durable blocked bit and blocker messages preserve that decision across a
crash between admission and failure finalization. A plan-stage persistence
failure leaves no partially admitted run. The row-level Run-pinned endpoint
loads the pin and private overrides server-side, queues a manual run, and never
inherits schedule-watermark capability.

Every actual due/catch-up interval is first recorded in
`schedule_occurrences` under a server-derived SHA-256 key over the stable
pipeline UUID, environment, and normalized half-open interval. Duplicate
signals return the active or successful occurrence without creating a second
run. Failed/cancelled occurrences become pending only when another signal
explicitly retries them; each admitted run is retained as a numbered
`schedule_occurrence_attempts` row. A v2 periodic/catch-up River job is only a
lightweight due signal: it carries the immutable schedule revision and interval,
performs planning/admission, and never invokes the physical runner. Occurrence
claim, attempt number, run, RunSpec, plan/units, pipeline slot, run-ID-only River
execution job, and run/job link commit in one SQLite transaction. A slot
conflict rolls the attempt back while leaving a durable pending occurrence for
the schedules API and UI. Run terminalization updates the occurrence through a
database trigger in the same transaction, including scheduled success plus its
watermark. The private RunSpec binds new scheduled runs back to the derived key;
legacy specs without one retain strict upgrade compatibility.

For new scheduler-backed admissions, the durable slot permits one
queued/running pipeline-scope run per logical pipeline across environments and
claims both path and stable-UUID aliases, preserving exclusion across a rename.
Manual races return `409 pipeline_run_active` with the active run ID.
New periodic/catch-up jobs use the distinct `renart-schedule-signal-v2` kind and
snooze for 30 seconds while another run holds the slot. River jobs persisted
under the older combined kind still decode and execute through the legacy path
until they drain. Startup requeues a claimed v2 or legacy signal with its exact
revision/interval; if a crash happens after v2 admission, its linked execution
job and occurrence let recovery distinguish queued, running, and retryable
state without reconstructing behavior from the signal. Migration reconciliation
deterministically keeps one queued-first legacy active row per pipeline path,
marks any duplicates failed, closes their open steps, and records the recovery
reason before creating the unique slot table. This keeps every conflicting row
auditable instead of preventing startup. The surviving legacy row receives only
a path alias because old run rows did not persist a pipeline UUID; it cannot gain
rename safety retroactively.

Reconciliation propagates persistence and catch-up enqueue failures instead of
leaving an apparently active row unapplied. Startup fails before River workers
start if core recovery state cannot be read or written. It atomically fails
running rows and queued rows whose queue job is terminal or missing, relinks
runnable legacy jobs, returns a claimed-but-not-yet-admitted schedule signal to
River, cancels admitted abandoned jobs, and replays persisted terminal steps
without rerunning asset code. It also normalizes otherwise-live River retry or
snooze timestamps written by older processes in an unorderable Go or RFC3339
form, preserving their arguments and attempt count while making them eligible
for pickup again. New manual admissions need no claim/link repair;
River-argument link recovery is legacy-only. Recovery emits one structured
count summary, including requeued signals and legacy replays skipped because
their effective execution context was never persisted (see staleness.md §3).

Plan-confirmed and synchronous full-pipeline execution now share the durable run
ledger. This includes HTTP pipeline streaming, delegated or embedded CLI
pipeline runs, legacy full-pipeline `/api/run`, and onboarding quickstart
materialization while preserving their inline stream. One-asset/scoped
Materialize now uses the v2 selection/unit ledger while preserving its one-click
stream. Build-needed flattens its server-recomputed topological selection into
v2 units—one per exact gap/default window—and records unit success, failure,
same-asset remainder skips, and downstream-of-failure skips while retaining one
asset-level progress step. Every window keeps a run/unit-derived completion ID.

Terminal plan-backed runs expose a server-derived re-execution capability on
run details. Renart offers exact re-execution only while the private RunSpec and
unblocked immutable plan are retained, their complete window and execution time
are present, current policy still permits the operation, and the original
source Merkle plus secret-free selected-configuration digest still resolve.
The run-owned `POST /api/runs/{id}/reexecute` accepts only an empty object and
rechecks those conditions at admission, so the browser cannot replace private
variables, modes, authorization, source, or units. It clones the retained
RunSpec and plan into a new manual, run-ID-only River job under the ordinary
pipeline slot, retaining source, environment, window, execution time,
variables, modes, authorization, selection, and units while removing schedule
identity and watermark authority. Stable pipeline UUID lookup lets an
unchanged working-tree plan survive a pipeline-directory rename. Legacy,
blocked, incomplete, or drifted runs instead advertise the distinct
current-settings action; a stale exact request fails with
`409 exact_reexecution_unavailable` rather than silently changing behavior.

Before applying either Renart or River migrations, `Store` runs SQLite's
`quick_check` against the shared state database. A failed check aborts startup
with the exact database path and instructions to preserve the database, WAL,
and shared-memory files for recovery. Renart never treats corruption as an
empty database: the file also contains schedules, deployments, run history,
and freshness state that must not be silently discarded.

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
connectionless API or Load asset follows the SQL/ingestr warehouse majority;
when there is no such majority and the pipeline configures exactly one default,
that explicit connection wins before Bruin's synthesized DuckDB fallback.
`create+replace` uses Sling's staging-table replacement, while
`truncate+insert` maps to Sling's in-place truncate mode so relation identity,
dependent views, and grants survive a reload. The latter keeps the existing
table schema and is not an atomic swap. A run-scoped full refresh temporarily
selects Sling's replace mode without rewriting the asset definition, except
that an already-full `truncate+insert` load remains in truncate mode to preserve
that explicit dependency-safe contract. `refresh_restricted` assets keep their
configured strategy and surface a warning instead. The shared Sling connection
bridge emits Sling-native DSNs where Bruin's ingestr URI convention differs:
Trino carries `catalog` and `schema` as query properties, while ClickHouse uses
the database path and exposes only the TLS flag as a query property. Because
Sling's PostgreSQL driver does not accept libpq's opportunistic `sslmode=allow`,
the bridge changes that mode to `verify-ca` and emits a run-log warning asking
the user to configure a supported mode explicitly. This compatibility rewrite
is scoped to Sling; the authored connection and direct Bruin execution keep the
configured mode. Load, API, Seed, and non-DuckDB Python materialization all use
this same bridge.
The shared Sling environment disables `_sling_loaded_at`, keeping these writes
schema-preserving instead of adding a loader-owned output column.

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
The guided editor reads existing seed content through
`GET /api/assets/{assetID}/seed-file`; the server derives the path from the
asset definition and returns only local UTF-8 CSV/JSON/JSONL/NDJSON files up to
1 MiB. Remote, runtime-rendered, binary, non-UTF-8, and larger sources return a
reason without content and remain replaceable through the multipart `POST` on
the same route. Seed bytes are never added to the workspace DTO.

Every advertised seed main task runs through Renart's Sling operator, separately
from generic ingestr assets. The operator resolves local sources relative to the
asset definition (or accepts HTTP(S)), supplies an explicit source format, and
full-refreshes the canonical asset name through the resolved target connection.
With `enforce_schema` and declared columns, it also passes the source selection,
renames, and supported type casts to Sling. The normal per-warehouse column and
custom checks still run around that main task. Renart additionally owns the
`trino.seed` type and maps it to `default_connections.trino`; the pinned Bruin
version has no equivalent type, so this asset remains intentionally Renart-only
until Bruin exposes the same contract. Sensor main tasks use Bruin's
native sensor operators: interactive runs default to one bounded check, while
scheduled runs retain dependency-gate semantics and wait until success or the
configured timeout. HTTP and CLI execution can explicitly select
`sensor_mode=once`, `wait`, or `skip`; the backend normalizes and enforces the
effective mode rather than relying on UI behavior. The default is derived from
the server-owned scheduled origin, not from the presence of a durable run ID,
because queued manual runs also have IDs.

Python assets run through Renart's in-process operator
(`service/python_operator.go`). Each task receives an embedded, version-locked
`renart` SDK wheel and a token-scoped loopback broker (`internal/web/pybroker`).
The same deterministic wheel is published to PyPI as `renart` on stable
Renart release tags for external editors and CI. Release builds inject one
version into both artifacts; runtime execution still uses the wheel assembled
inside the Renart binary, so a network lookup or separately installed SDK can
never introduce runner skew. The assembler precompresses wheel members and
writes their CRC and sizes into descriptor-free local ZIP headers, preserving
deterministic output while satisfying PyPI Warehouse's archive validation.
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

The DTO also advertises `column_inference_sources` per asset. Sources are
backend capabilities rather than frontend asset-kind branches: definition
sources include SQL output inference, API fields/OpenAPI, Load upstreams, and
local seed files; observed sources include sampled API responses and the current
materialized relation. `POST /api/assets/{assetID}/columns/sync` automatically
uses the definition and adds only the observed sources selected by the caller.
It immediately reconciles column additions and unknown-to-known type refinements;
saved-column removals, known-type changes, and source disagreements return a
non-mutating merge model. `POST /columns/sync/apply` persists the user's row-level
choices through the same provenance model. Sampled sources are marked as partial,
so absence in one API sample is not deletion evidence. The older one-source
preview and direct reconcile routes remain compatibility APIs. Materialized
Load/API observations ignore legacy `_sling_loaded_at` columns. DuckDB table
observations use `DESCRIBE` so logical catalog types such as `JSON` survive the
ADBC/Arrow result boundary. Sensors advertise no schema source.

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
For SQL queries they reuse the SQL LSP's cross-connection diagnostic and warn
when a referenced project asset resolves to a different effective target
connection; unknown connections do not produce a speculative warning.
Assets with dedicated runtime semantics and no generic capability profile are
excluded from this generic materialization validation; in particular, a seed's
successful Sling write is not reinterpreted as an unsupported `none` mode.
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
