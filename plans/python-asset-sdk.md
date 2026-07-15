# Python asset SDK: query the project without credentials, upload without ingestr

> **Status (2026-07-14): Phases 1–2 and the explicit phase-3 upstream refresh
> implemented, including notebook runner unification and startup optimization,**
> on the `redesign` branch. §10
> questions were answered by Lukas: (1) SDK named `renart`, no bruin shim;
> (2) any project connection is readable; (3) wait semantics as proposed,
> with explicit `--refresh-upstreams` now implemented in phase 3; (4) queries
> default to Arrow while
> pandas remains available in the SDK wheel; (5) the ingestr asset type stays
> as-is. Stable Renart release tags now publish the exact same versioned wheel
> to PyPI as `renart` through trusted publishing.
>
> As built: `internal/web/runstate` (in-flight task registry, fed by
> `HybridBruinExecutor.RunAsset/RunPipeline`), `internal/web/pybroker`
> (per-task loopback broker: `POST /v1/query` Arrow IPC + JSON fallback,
> `GET /v1/context`, wait/deadlock/lint semantics), `internal/pysdk`
> (embedded SDK sources + Go wheel assembler, injected via uv `--with`), and
> `internal/web/service/python_operator.go` (renart-owned operator replacing
> `bruinpython.NewLocalOperator` in the direct registry and notebook cells:
> parquet staging wrapper, native DuckDB `read_parquet` load through bruin's
> materializer, Sling load for other warehouses with env-named connections
> and `SLING_LOADED_AT_COLUMN=false`). ingestr is gone from the python path.
> Verified live: pipeline + single-asset + server-delegated runs, merge
> strategy idempotence, read-only guard, undeclared-dep lint, same-run
> deadlock guard. Phase 2 added embedded `.pyi` stubs registered with
> pyintelligence, built-in dependency recognition, type-check lint for literal
> `query()` calls missing `depends`, notebook-cell broker queries, live notebook
> E2E coverage, and user docs. Notebook cells now query their live in-process
> session (logical names are rewritten to `cell_<id>`), stage only Parquet, and
> never receive or create an input/output DuckDB staging database. Empty
> notebooks do not provision a duplicate pandas/duckdb project; uv verification
> is cached, `uv run` owns lock/sync, and pandas/polars imports are lazy for Arrow
> results. SDK queries and generated notebook cells use Arrow by default, with
> `.to_pandas()` and `format="pandas"` available explicitly. On the profiling
> machine this reduced a warm SDK-query cell from 719–918 ms to 418–515 ms. The
> as-built design is now recorded in
> `architecture/backend.md` and `architecture/notebooks.md`. Phase 3 now includes
> `renart run <asset> --refresh-upstreams`: Renart builds only stale transitive
> upstreams in the selected environment/window before the requested asset, in
> both delegated and embedded CLI modes, and aborts the target run if an
> upstream refresh fails. A cross-connection policy surface (now proposed in
> [python-cross-connection-policy.md](python-cross-connection-policy.md)) and
> Arrow Flight evaluation remain deferred.

Goal: Python assets get first-class access to the rest of the renart project —
`query()` against project data, run context, and result materialization — with
two hard constraints that distinguish this from Bruin's python-sdk:

1. **Credentials never enter the Python process** (unless the user explicitly
   opts in via Bruin's `secrets:`, which stays untouched for compat).
2. **The upload leg drops ingestr** — ingestr is FSL-1.1 licensed
   ("may not be used to offer a competing ingestion/ELT/connector product"),
   which renart cannot rely on; it also puts the destination URI, credentials
   included, on the `uv tool run … ingestr --dest-uri` command line.

A third requirement: a query against another project asset must be able to
**wait for that asset's in-flight materialization** instead of reading a
half-built or stale table.

---

## 1. Pre-implementation baseline (historical)

**Execution.** Python assets run through Bruin's local operator:
`direct_executor_registry.go:257` installs
`bruinpython.NewLocalOperator(manager, directPythonEnvVariables(pl))` for
`AssetTypePython` — the same registry serves interactive runs, `renart run`
(embedded or delegated), and River scheduler snapshot runs, so **the runner is
always in-process with the connection manager**. The operator builds `BRUIN_*`
env vars (`pkg/env/variables.go`: run window, run id, pipeline, vars JSON) and
runs the module via uv.

**Data access today: none.** Bruin's model is `secrets:` mappings that inject
whole connection JSONs into env vars (`pkg/python/operator.go`); the PyPI
`bruin-sdk` package then parses those env vars client-side and opens direct
database connections. Renart never surfaces `secrets:` in its UI or asset
codec, so a renart Python asset currently has no way to read project data at
all. Whatever we build is greenfield on the renart side while staying
env-compatible with Bruin scripts.

**Materialization tail.** For `materialization.type: table`, Bruin wraps the
module in a generated script (`pkg/python/uv.go runWithMaterialization`): it
imports the module, calls the user's `materialize()`, writes the returned
dataframe(s) as an Arrow IPC file, then shells out to
`uv tool run --from ingestr@1.0.75 ingestr ingest --source-uri mmap://<file>
--dest-uri <URI with credentials> --dest-table <asset name>`. Three problems:
the FSL license, credentials on argv (visible in `ps`), and a heavyweight
per-version ingestr environment.

**Renart already has an ingestr-free write stack.** Load and HTTP API assets
run Sling (`uv tool run --from sling sling run …`), with the strategy →
Sling-mode mapping centralized in `slingMaterializationArgs`
(`internal/web/service/load.go:299`): create+replace → default, truncate →
`--mode truncate`, append+incremental_key → `--mode incremental --update-key`,
merge → `--mode incremental --primary-key …`. `loadConnectionURI` bridges any
named bruin connection to a Sling DSN via `GetIngestrURI()`.

**The staging pattern is already proven in notebooks.**
`internal/web/notebook/run_python.go`: the runner exports a Python cell's
upstream tables into a throwaway DuckDB file (path via
`RENART_NOTEBOOK_INPUTS`), the cell's `materialize()` output lands in another
throwaway DuckDB file, and the runner `ATTACH`es it and copies the table into
the session — Python touches no credentials. But the output leg still routes
through ingestr (`notebook_python.go` synthesizes a DuckDB connection named
`renart-notebook-session` and lets Bruin's operator do arrow → ingestr →
duckdb), so notebooks inherit every problem above minus the credential one.

**Waiting infrastructure.** The staleness stack (architecture/staleness.md)
already knows what is fresh/stale per (asset, env, fingerprint), and
build-stale compiles topological plans. What does *not* exist is a queryable
in-process registry of *in-flight* tasks ("is something materializing asset X
right now?") — each run path (direct run loop, `MaterializeStaleAssetsStream`,
scheduler runner) keeps its scheduler state private.

## 2. Design overview

```
┌──────────────────────────── renart process (server or embedded CLI) ───────────────┐
│                                                                                     │
│  python operator (new, renart-owned)                                                │
│    ├── starts per-task broker on 127.0.0.1:0, mints per-task token                   │
│    ├── uv run … module            (env: RENART_API_URL, RENART_API_TOKEN, BRUIN_*)  │
│    └── loads staged parquet into destination (native duckdb / sling)                 │
│                                                                                     │
│  pybroker (new)                                                                     │
│    ├── POST /v1/query  ──► run-registry wait ──► connection manager ──► Arrow IPC   │
│    └── GET  /v1/context ──► run window, vars, asset, pipeline                        │
│                                                                                     │
└─────────────────────────────────────────────────────────────────────────────────────┘
        ▲ loopback HTTP, bearer token                    │ parquet file
        │                                                ▼
   python script: `from renart import query`        materialize() → staging.parquet
```

The **run broker** is the crux: queries execute inside the Go process through
the same connection manager every other path uses, so credentials stay in Go,
DuckDB's single-writer discipline is preserved (the broker shares the
in-process locks — no second writer process), and there is one chokepoint for
read-only enforcement, environment policy, and materialization-waiting.

Alternatives considered and rejected:

- **Bruin's model (credentials in env).** Rejected — it is the thing we're
  fixing. Kept working for users who hand-write `secrets:`, unchanged.
- **Staged-inputs only** (generalize the notebook pattern: export declared
  upstreams to a local DuckDB file before the run). No ad-hoc queries, full
  upstream export can dwarf the query the script actually wanted, and data is
  a stale copy by construction. Possible later *offline* extension, not v1.
- **Hand Python a read-only DuckDB handle.** DuckDB-only, fights the
  single-writer lock while a pipeline is running, and leaks the whole file.
- **Arrow Flight SQL.** The right long-term wire protocol, but gRPC +
  flight adds a big dependency for a loopback hop. Plain HTTP + Arrow IPC
  stream is ~50 lines on each side (verified, §3) and can be swapped later
  without touching the SDK surface.

## 3. Verified by experiment (2026-07-12)

Spikes live only in the session scratchpad; results:

1. **Sling loads local parquet into DuckDB with renart's strategy flags.**
   `sling run --src-stream file://…/data.parquet --tgt-conn <env-named conn>
   --tgt-object main.py_result` — full-refresh worked; a second file with
   `--mode incremental --primary-key id --update-key updated_at` correctly
   merged (updated row replaced, new row appended). Sling appends a
   `_sling_loaded_at` column by default; `SLING_LOADED_AT_COLUMN=false`
   removes it. Connection URI passed via **env var name** (`--tgt-conn
   SLING_TGT` + `SLING_TGT=duckdb://…` in env), so credentials stay off argv —
   note: `runLoadAsset` today passes URIs on argv; same fix applies there.
2. **Go → Python Arrow round-trip.** A Go server using
   `github.com/apache/arrow-go/v18` streaming two record batches as an Arrow
   IPC stream over loopback HTTP, read by Python with **stdlib urllib +
   pyarrow only** (`ipc.open_stream(resp).read_all().to_pandas()`): typed
   DataFrame (int64/string preserved), bearer-token auth, multi-batch
   streaming all worked first try.

## 4. The broker (`internal/web/pybroker`)

**Lifecycle.** Started by the python operator per task instance on
`127.0.0.1:0` (TCP, not unix socket — Windows), closed when the task ends.
Token: 256-bit random, constant-time compared (same pattern as
`SameOriginGuardWithToken`, middleware.go). Loopback bind + token; the token
authorizes exactly one task's scope (asset, pipeline, environment, run window
baked into the broker instance at construction).

**Endpoints (v1).**

| Endpoint | Behavior |
| --- | --- |
| `POST /v1/query` `{sql, connection?}` | wait on referenced in-flight assets (§6) → execute read-only on the asset's connection (or `connection` if allowed, §10 Q2) → stream Arrow IPC (`application/vnd.apache.arrow.stream`); `Accept: application/json` returns the existing `{columns, rows}` envelope for debugging |
| `GET /v1/context` | JSON: start/end, execution date, run id, pipeline, asset, vars, full_refresh — the same values the `BRUIN_*` env vars carry, as one typed document |

**Read-only enforcement** mirrors inspect: single-statement SELECT check, and
`access_mode=read_only` on DuckDB paths. Writes deliberately have no endpoint
— transformations that write belong in SQL assets or the asset's own
materialization, otherwise lineage/staleness go blind.

**Execution + encoding.** Queries go through the same path as
`ExecutionService.RunConnectionQueryForEnvironment` (execution.go:1332), but
we need schema-carrying results rather than parsed JSON: call the
connection's `SelectWithSchema` and encode to Arrow with a Go→Arrow type
mapping (new direct dependency `github.com/apache/arrow-go/v18` — Apache-2.0,
build verified). Batches stream as they are read, so large pulls don't buffer
in Go. Row-set JSON stays as fallback encoding only.

**Policy.** Broker queries funnel through the same `policy.Check` chokepoint
as other execution; reads in protected environments are allowed (guardrails
target execution/destructive ops), but the chokepoint placement means a
future "no interactive reads in prod" flag costs one line.

## 5. Python SDK (`renart` package, repo dir `sdk/python/`)

Mirrors the ergonomic surface of Bruin's python-sdk, minus everything that
touches credentials:

```python
"""@bruin
name: analytics.player_stats
type: python
materialization:
  type: table
depends:
  - chess_games
@bruin"""

from renart import query, context

def materialize():
    games = query("select * from chess_games where end_time >= '2026-01-01'")
    if context.is_full_refresh:
        ...
    return games.to_pandas().groupby("winner").size().reset_index(name="wins")
```

- `query(sql, connection=None, format="arrow")` → `pyarrow.Table` (the
  zero-copy default, and feeds polars via `pl.from_arrow`);
  `format="pandas"` or `.to_pandas()` → `pandas.DataFrame`.
- `context` — typed accessors backed by `GET /v1/context`, falling back to the
  `BRUIN_*` env vars (which we keep setting), so scripts that already read
  `BRUIN_START_DATE` etc. keep working unchanged.
- Transport: stdlib `urllib` against `RENART_API_URL` + `RENART_API_TOKEN`.
  The embedded wheel includes PyArrow and pandas; Arrow stays the default so
  pandas conversion is paid only when requested (see §10 Q4).
- `materialize()` protocol stays byte-compatible with Bruin (return a
  DataFrame / pyarrow Table / generator of Tables).

**Distribution (implemented).** The SDK sources are embedded in the Renart
binary and one deterministic Go assembler writes the wheel on demand, then the
runner injects it through `--with <wheel-path>` (works in both uv script mode
and pyproject project mode). Stable release tags inject the Renart version into
the binary and export the same wheel bytes for PyPI trusted publishing under
the distribution name `renart`. PyPI exists mainly so external editors and
CI can resolve `import renart`; the binary-assembled wheel remains the runtime
source of truth and requires no network access.

**Intellisense (phase 2).** Ship `.pyi` stubs in the wheel and register them
with pyintelligence (ty wasm) so `from renart import query` resolves in the
editor; extend the existing python-deps surface so the import isn't flagged
as undeclared.

## 6. Waiting for materialization

New tiny package `internal/web/runstate`: an in-process registry
`(pipeline uuid, asset name, environment) → in-flight task handle (done chan +
outcome)`. All three run paths (direct run loops in `direct_run.go`,
`MaterializeStaleAssetsStream`, the scheduler `Runner`) register task
start/finish — a few lines each since they all already emit per-asset events
at exactly those points.

`POST /v1/query` then:

1. Extracts referenced tables from the SQL (the sqlintelligence wasm parser
   already does used-table extraction for lineage/fingerprints) and maps them
   to workspace assets by name — same resolution the LSP uses.
2. Any referenced asset with an **in-flight task in the same environment**:
   wait for completion (bounded by a timeout, default ~30 min, and the run
   context's cancellation), logging `waiting for chess_games …` into the
   asset's captured output. If the awaited task fails, the query fails with
   that upstream error rather than silently reading stale data.
3. **Deadlock guard:** if the referenced asset is part of the *same run* but
   scheduled at-or-after the querying asset (not started and not upstream of
   us), fail fast: `"asset X is scheduled later in this run — declare it in
   depends to order it before this asset"`. The bruin scheduler instances
   expose enough state to decide this.
4. **Undeclared-dependency lint:** referencing an asset that is not in the
   python asset's declared `depends` produces a warning in the run log
   (ordering is only guaranteed for declared upstreams). Phase 2 surfaces the
   same check in pipeline type-check by statically scanning `query()` string
   literals.

Deliberately **not** v1: the broker triggering a build of *stale-but-idle*
upstreams (ad-hoc `renart run one_asset.py` with stale upstreams). The
mechanics exist (`build-stale` cone planning), but auto-building on read is a
policy landmine (protected envs, surprise cost). Proposed phase 3 as explicit
opt-in: `renart run --refresh-upstreams`, which pre-runs the build-stale plan
for the target's upstream cone. (§10 Q3.)

## 7. Replacing ingestr: the upload leg

New renart-owned operator (working name `internal/web/pyexec`) replaces
`bruinpython.NewLocalOperator` in `direct_executor_registry.go` and
`notebook_python.go`. Bruin exports the uv plumbing (`UvChecker`,
`CommandRunner`, `CommandInstance`, `env.SetupVariables`, module/repo
finders) but not the operator's `executionContext`, so the operator body
(~200 lines: repo/module resolution, dependency-config detection, env
assembly, uv invocation) is reimplemented — it also *gains* broker startup
and loses the `secrets:`-independent parts we don't want. Bruin's operator
remains the fallback for hand-written `secrets:` compat if we choose (§10 Q1).

**Collect.** Our wrapper template (analog of Bruin's `PythonArrowTemplate`)
imports the module, calls `materialize()`, and writes **Parquet** via
`pyarrow.parquet.ParquetWriter` (chunked, so generator-of-Tables streaming
still works). Parquet over Arrow IPC because both Sling and DuckDB consume it
natively and it self-describes types well; pyarrow arrives via `--with`
exactly as Bruin does today.

**Load, per destination:**

- **DuckDB (the renart default):** no subprocess at all. Synthesize the query
  `SELECT * FROM read_parquet('<staged>')` and run it through Bruin's
  existing DuckDB `Materializer` strategy map — create+replace, append,
  delete+insert, merge, time_interval all come for free with dialect-correct
  SQL, executed on the in-process connection (no second-writer lock dance,
  which is why ingestr needed `duck.LockDatabase`). This also *improves*
  strategy parity for python assets over ingestr's mode translation.
- **Everything else (postgres, bigquery, snowflake, …):** Sling, reusing
  `loadConnectionURI` + `slingMaterializationArgs` verbatim, with
  `SLING_LOADED_AT_COLUMN=false` and the connection URI passed through an
  env-named connection (verified §3) — credentials off argv. Strategies not
  representable in Sling modes (`time_interval`, `scd2_*`) stay rejected for
  Python assets, consistent with the capability contract in
  `../architecture/backend.md` §4.

**Net effect:** ingestr disappears from the python path entirely (first
python run gets faster — no ingestr env install), credentials never appear on
argv, and the notebook cell path sheds its synthetic-connection workaround
(the runner can load the staged parquet straight into the throwaway session
file, replacing `PythonMaterializer`'s ingestr leg).

The `ingestr` *asset type* (hidden behind `features.ingestr`) is out of scope
here, but note the FSL question applies to it identically — long-term it
should either stay clearly flagged as bring-your-own-license or gain a Sling
translation. (§10 Q5.)

## 8. Wiring per execution context

| Context | Where the operator runs | Broker |
| --- | --- | --- |
| UI / API interactive run | server process, `buildDirectMainExecutors` | per-task listener in-process |
| Scheduler snapshot run | server process, same registry, snapshot temp dir | same; `ConfigPath` override already flows through the executor |
| `renart run` delegated | server process (SSE delegation) | same — the CLI never runs python itself when a server is live |
| `renart run` embedded | headless in-process graph | same code path; no HTTP server needed — the broker is its own listener |
| Notebook python cell | server, `materializePythonCell` | same operator in collection-only mode; broker queries the already-open live session and the runner loads staged Parquet directly, with no database path exposed to Python |

The query executor, runstate registry, table extraction, and connection policy
dependencies are threaded from the service graph through the direct executor
construction site, so all execution contexts inherit the same broker behavior.

## 9. Phasing

**Phase 1 — core (ships the user-visible feature):**
`pybroker` (query + context, Arrow encoding, token auth) · `runstate`
registry + wait/deadlock/lint semantics · renart-owned python operator with
parquet staging and native-duckdb/Sling load (ingestr removed from python
assets) · SDK wheel (query/context), embed + `--with` injection · notebook
cell path switched to the new operator (upload leg only).
Tests: broker auth/read-only/arrow-encode units; strategy SQL goldens for the
duckdb read_parquet path; live e2e — python asset queries an upstream duckdb
table and materializes each strategy; a pipeline run where the python asset
waits on a slower upstream; scheduler snapshot run.

**Phase 2 — DX (implemented):** `.pyi` stubs + pyintelligence registration ·
type-check lint for undeclared literal `query()` references · JSON fallback
coverage · notebook `query()` against the live notebook session · direct
Parquet-to-session output load · startup/import optimization · docs for Python
assets and notebook cells.

**Phase 3 — reach (partially implemented):** `renart run
--refresh-upstreams` builds the stale transitive-upstream cone before an asset
run, using normal upstream materialization strategies and stopping before the
target if refresh fails. Stable release tags publish `renart` to PyPI from
the same deterministic wheel assembler after the binary release succeeds.
Remaining: implement the proposed cross-connection read-policy surface ·
consider Arrow Flight if profiles ever show the loopback hop mattering.

## 10. Open questions for Lukas

1. **SDK identity & Bruin compat.** Proposal: import name `renart`, no
   `bruin`-import shim (the trust models differ; a shim would silently change
   semantics of existing bruin scripts). Bruin scripts using only `BRUIN_*`
   env vars keep working; scripts using `bruin-sdk`/`secrets:` keep working
   via Bruin's own mechanism if the user opts into `secrets:` — acceptable?
   Answer: yes import name should be renart
2. **Read scope.** May a python asset `query(connection=…)` any connection
   defined in the project, or only its own connection (+ declared upstream
   assets' connections)? Proposal: any project connection, reads only — the
   broker never reveals credentials and the same user authored both configs;
   protected-env policy can tighten later.
   Answer: yes it may read all connections in the project as proposed
3. **Wait semantics.** v1 = wait on in-flight same-env tasks, hard error on
   same-run ordering violations, warn on undeclared refs, no auto-build of
   stale-idle upstreams. Is the phase-3 `--refresh-upstreams` opt-in the
   right shape, or do you want read-triggered freshness (broker auto-builds
   the stale cone) earlier / at all?
   Answer: for now, do not rebuild the stale cone as a side effect of a broker read.
   Implementation update (2026-07-14): the explicit CLI opt-in is implemented;
   broker reads still never trigger builds.
4. **pandas dependency.** Should `query()` return pandas or PyArrow by default,
   and should pandas be included in the injected wheel?
   Answer: we should check if we can't just give the user pyarrow dataframes instead
   of pandas dataframes
   Implementation decision (updated 2026-07-13): return a PyArrow Table by
   default because it avoids the conversion cost. Keep pandas installed so
   callers can use `.to_pandas()` or request `format="pandas"` explicitly.
5. **ingestr asset type.** Leave the flag-hidden ingestr asset type as-is for
   now (bruin-compat, user-invoked), or schedule its Sling migration into
   this effort's phase 3?
   Answer: For now leave as is

## 11. Risks

- **Type fidelity** parquet → Sling → warehouse (decimals, tz-aware
  timestamps, nested types). Mitigation: per-warehouse e2e matrix; the
  DuckDB-native path sidesteps Sling entirely for the default case.
- **Table-ref extraction** for wait/lint is dialect-imperfect. Acceptable:
  waiting is best-effort correctness *improvement*, lint is warn-only, and
  declared `depends` remains the contract.
- **arrow-go dependency** adds ~10 MB of modules to the build; runtime RSS
  impact negligible (no wasm, no persistent state). Verified it builds.
- **uv `--with` wheel injection** interacts with three dependency modes
  (none / requirements.txt / pyproject). `uv run` performs project lock/sync in
  the same invocation; the SDK wheel itself supplies PyArrow and pandas.
- **Broker lifetime edge cases** (script outlives task timeout, orphaned
  listeners). Listener is owned by the operator's context — cancellation
  closes it; tokens die with the listener.
