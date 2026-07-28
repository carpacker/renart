# Pipeline execution parallelism

> **Status (2026-07-27): core implementation complete; warehouse rollout in
> progress.** Durable v3 contracts, the shared unit graph, full/reviewed/Needed
> convergence, bounded local resources, cancellation/failure semantics, and the
> run-review controls are implemented. Native PostgreSQL, Trino, ClickHouse,
> and StarRocks SQL relations are the first audited remote families. Remaining
> operator-family audits, wait-time telemetry, and optional DuckDB relation
> concurrency stay in this plan.

## 1. Decision summary

Renart should parallelize **execution units**: one asset over one exact time
window. Every mutating pipeline run should be normalized into a dependency DAG
of those units before physical work begins, regardless of whether the request
came from:

- Run all;
- a reviewed asset, selector, or needed plan;
- Build needed;
- an inline run; or
- a scheduled run.

The recommended implementation is a Renart-owned, bounded unit scheduler around
the existing per-asset executor. It should:

- use the existing `pipeline.yml` `max_active_steps` value as the maximum
  number of active steps in one run;
- treat an omitted value as `1` during the initial Renart rollout and preserve
  `1` as the compatibility mode;
- order each asset's checks and metadata work inside that asset's unit;
- never start a unit until its selected upstream units completed successfully;
- continue independent branches after a failure, while skipping only affected
  downstream units;
- serialize units that may write the same local file or DuckDB database;
- treat arbitrary Python, hooks, remote objects, and other unproven targets as
  pipeline-exclusive at first;
- additionally enforce per-connection `max_concurrent_assets` limits;
- share a workspace-wide execution budget so scheduled, inline, and interactive
  runs cannot multiply into an unbounded number of workers;
- persist running state and physical target claims before invoking an operator;
  and
- stop admitting work, cancel in-flight work, and durably close queued units
  when a run is cancelled.

This should not be implemented by independently adding goroutines to the
current execution loops. Those loops already differ in failure, ledger, and
completion behavior; parallelizing each one would make those differences
harder to reconcile.

## 2. Why execution units are the right boundary

An execution unit already exists in the durable run model and is the smallest
reviewable materialization decision:

```text
asset + [start, end) + render position + reason
```

It is also the useful unit for safety and user expectations:

- an asset can have multiple catch-up windows, but those windows must not race
  each other against the same mutable target;
- an asset's main task, blocking checks, non-blocking checks, and metadata push
  belong to one understandable lifecycle;
- the run timeline can show meaningful overlap between assets;
- a per-connection concurrency limit naturally counts active assets rather
  than incidental check goroutines; and
- freshness should be recorded after each successful unit, before a dependent
  unit becomes runnable.

The first version should keep the tasks *inside* one unit sequential. Parallel
checks are a later optimization and are unlikely to be the main source of
pipeline speedup.

## 3. Current-state audit

### 3.1 Execution is sequential in three separate paths

`internal/web/service/direct_run.go` currently has two distinct schedulers:

1. `RunPipeline` repeatedly scans the execution engine's pending task
   instances, runs the first dependency-ready instance through
   `Sequential.RunSingleTask`, and returns on the first failure.
2. `runPlannedPipeline` loops over reviewed execution units and runs each unit
   synchronously. Each unit has another sequential loop over main, check, and
   metadata tasks.

`internal/web/service/execution_stale.go` has a third loop. Build needed orders
assets, then runs every selected window for an asset before moving to the next
asset. It already has the more useful failure rule: a failed branch skips its
downstreams while independent branches continue.

Adding concurrency separately to these paths would leave three schedulers with
different cancellation, failure, logging, and freshness rules.

### 3.2 The durable model is already close

The scheduler persists:

- a typed, redacted run plan;
- ordered execution units with queued/running/terminal states;
- step events;
- target-write claims;
- completion ordinals and evidence; and
- a recomputed execution-target snapshot.

`pipelineRunObservation`, the run-state registry, and the stream/log writers
already protect their mutable state with mutexes. They still need focused race
tests, but a concurrent execution path does not require replacing the ledger or
the event model.

Plan v2 currently stores aggregate write-resource claims for run admission.
Those claims answer whether *two runs* may overlap, but not which unit inside a
run owns which claim. Parallel execution therefore needs a typed per-unit
resource contract rather than inferring behavior from the presentation
artifact.

### 3.3 Cross-run resource admission already exists

Reviewed plans and execution-target snapshots distinguish:

- no write;
- exact local file;
- exact DuckDB database; and
- conservative pipeline isolation.

The run store uses the aggregate claims to serialize conflicting runs while
allowing proven-distinct targets to overlap. Parallel units should reuse the
same canonical identities and the same fail-closed rules inside one run.

The aggregate run claim remains necessary: an in-process unit scheduler cannot
protect a target from another scheduled or inline run.

### 3.4 DuckDB has a useful coordination layer

`internal/web/duckcoord` already:

- canonicalizes database paths;
- acquires multiple paths in sorted order;
- provides context-cancellable process-local locks; and
- uses advisory file locks to coordinate Renart processes for the same user.

Direct SQL tasks acquire this lease around the operator. API and Python assets
perform network or compute work first, then acquire the target lease around the
final load. Load assets acquire their relevant DuckDB paths around the Sling
transfer.

That coordinator remains the last line of defense. It currently coordinates
DuckDB access for reads and checks as well as writes. The unit scheduler should
avoid dispatching work that will immediately block on the same lease because
that wastes a worker slot and obscures the wait reason.

DuckDB supports multiple writer threads in one process, but concurrent writes
can conflict, and several Renart paths use child processes or separate database
handles. The initial contract should therefore remain deliberately stronger:
**one Renart writer per canonical DuckDB database file**, even when relations
are distinct.

This requires distinguishing two related contracts:

- **mutation claims** describe durable side effects and drive cross-run
  admission; and
- **runtime coordination claims** describe exclusive operator leases, including
  DuckDB reads/checks that must not overlap a writer in the current
  implementation.

Both use canonical, secret-free identities. A no-write DuckDB sensor has no
mutation claim, but it does have a runtime coordination claim for the database
file. The existing DuckDB coordinator still protects the cross-run case.

See DuckDB's
[official concurrency documentation](https://duckdb.org/docs/current/connect/concurrency)
for the underlying in-process/multi-process and write-conflict constraints.

### 3.5 Existing parallelism settings are not wired into this path

Pipelines already have:

- `concurrency`, which controls how many runs of the same pipeline may overlap
  in the hosted execution model; and
- optional `max_active_steps`, which controls parallel work inside one run in
  that model.

Connection metadata already supports `max_concurrent_assets`, and the upstream
execution engine has connection-limit helpers. Renart's direct pipeline loops
do not currently enforce those values.

The implementation should use:

- `max_active_steps` for active steps per run;
- `max_concurrent_assets` as a stricter per-connection bound; and
- an internal workspace-wide bound across all runs.

It should **not** reinterpret `concurrency` as asset parallelism. Doing so would
make the same `pipeline.yml` mean something different in Renart and the CLI or
hosted runtime.

In Phase 1, one execution unit runs at most one physical task at a time. The
number of active units therefore equals the number of active steps. If tasks
inside one unit are parallelized later, they must share the same
`max_active_steps` semaphore rather than multiplying beyond it.

## 4. Goals and non-goals

### 4.1 Goals

- Speed up fan-out pipelines when independent assets have safe targets.
- Give every execution entry point the same dependency and failure semantics.
- Keep plans, events, output, cancellation, freshness, and checks correct under
  concurrency.
- Fail closed when a target or operator's write behavior is not proven.
- Reuse the existing per-run step and per-connection concurrency settings.
- Keep scheduling deterministic enough to test and explain.
- Make effective parallelism observable without turning the run dialog into a
  scheduler dashboard.

### 4.2 Non-goals for the first implementation

- Concurrent windows for the same asset.
- Concurrent writes to distinct relations in the same DuckDB file.
- Parallel task/check execution inside one asset unit.
- Distributed scheduling across hosts.
- Coordinating arbitrary programs that write the same local file without using
  Renart's locks.
- Guessing exact resources for arbitrary Python or hooks.
- Changing the upstream project format.
- Making parallel execution the default for existing pipelines.

## 5. Options considered

### 5.1 Replace the full-run sequential executor with the upstream concurrent executor

This is the smallest code change. The upstream scheduler already has a DAG,
workers, cancellation, failure propagation, and connection limits.

It is not sufficient as the final design:

- it only fixes the full-pipeline loop, not reviewed or needed execution;
- its worker callbacks cannot return persistence errors;
- a Renart step must be durably marked running and its target-write claim must
  be bound before side effects start;
- output injection and per-asset log capture are awkward;
- it has no knowledge of Renart's durable execution units or local resource
  claims; and
- it would leave Renart's durable execution-unit lifecycle outside the worker
  boundary.

The upstream scheduler remains useful as a reference and for building the task
sequence inside a unit, but Renart should not drop its concurrent executor into
the ledger boundary.

### 5.2 Add goroutine pools to each current loop

This appears incremental, but creates three subtly different schedulers. It
would duplicate dependency propagation, resource gating, cancellation, and
error handling, and would preserve the current semantic difference between
full and needed runs.

Reject this option.

### 5.3 Run one CLI subprocess per asset

This gives process isolation and existing CLI behavior, but at a high cost:

- repeated startup, parse, render, and dependency resolution;
- fragmented logs, cancellation, secrets, and target snapshots;
- cross-process DuckDB contention;
- harder completion/freshness handoff; and
- no natural shared connection or workspace budget.

Keep the existing CLI fallback for asset types that require it. Do not make it
the parallel execution architecture.

### 5.4 Recommended: one Renart execution-unit DAG runner

Normalize all new runs into typed units, build their dependency edges once, and
execute them through a bounded scheduler. Each worker invokes the existing
single-unit task runner through a stricter lifecycle callback that may fail
before physical work begins.

This is more work than switching executors, but it consolidates rather than
adding another execution path.

## 6. Scheduling contract

### 6.1 Build the unit DAG

For every selected execution unit:

1. Resolve its asset identity against the parsed pipeline.
2. Add an edge from the final selected unit of every selected upstream asset.
3. For multiple units of the same asset, add a chain in reviewed order:

   ```text
   asset/window 1 -> asset/window 2 -> asset/window 3
   ```

4. If an upstream has no selected unit because it is already fresh or outside
   the requested scope, do not invent an execution edge. Its reviewed data
   state remains the precondition.
5. Reject cycles or missing identities before starting any unit.

For a full run, produce one unit per asset for the resolved default window.
For Build needed and catch-up plans, reuse the already reviewed windows.

Waiting for an upstream asset's final selected unit is intentionally
conservative when both assets have several windows. A later planner revision
may emit interval-aware edges so a downstream window can start as soon as its
required upstream coverage is complete. That mapping must come from reviewed
coverage semantics; the runtime scheduler must not infer it from coincidentally
similar timestamps.

The execution plan should contain stable dependency positions or unit keys so a
recovered scheduled run never has to reconstruct behavior from current UI
state.

### 6.2 Ready queue and deterministic ordering

A unit is ready when:

- every execution dependency succeeded;
- its asset's preceding window succeeded;
- the run has an available unit slot;
- the workspace has an available slot;
- all connection limits have capacity; and
- its runtime coordination and write-resource gates are available.

When several units are ready, choose the lowest durable plan position first.
This does not make completion order deterministic—nor should it—but it makes
admission reproducible. Human summaries should use plan order; the timeline and
completion ordinals should reflect actual time.

### 6.3 Concurrency limits

Use the minimum of:

```text
pipeline max_active_steps
workspace available units
every connection's max_concurrent_assets
resource availability
number of dependency-ready units
```

`pipeline.yml` `max_active_steps` remains a step limit. In the initial
unit-runner design, an active unit owns one step slot while its current main,
check, or metadata task runs; it does not hold extra slots for tasks that have
not started. Because tasks inside a unit are sequential, the number of active
units and steps is the same.

`pipeline.yml` `concurrency` is not consulted by the unit scheduler. It retains
its separate meaning of overlapping runs of the same pipeline. Whether local
scheduled runs should also enforce that field is an admission-policy question,
not part of asset parallelism.

The workspace needs one fair, process-wide budget shared by scheduled and
inline runs. Otherwise River's scheduled worker count, interactive runs, and
each pipeline's own step limit multiply without a bound. Start with an
internal cap of `8`, allow an environment override for tests and advanced
operations, and revisit the value after representative benchmarks. Do not add
another project setting in the first slice.

Acquire budgets in this order:

1. choose a ready unit without mutating durable state;
2. reserve the workspace and per-run unit slots;
3. atomically reserve all sorted connection and resource keys;
4. persist the unit/step running transition and target-write binding;
5. invoke the operator.

If any reservation or persistence step fails, release everything and do not
start physical work. A worker waiting for capacity must remain queued, not
running.

The budget implementation should be context-cancellable and fair across runs.
A round-robin run queue is preferable to one global semaphore because a large
run should not permanently occupy every workspace slot while a small
interactive run waits.

### 6.4 Failure semantics

Adopt dependency-scoped failure semantics everywhere:

- a main task or blocking check failure fails its unit;
- later windows of the same asset are skipped;
- downstream units are skipped with the failed upstream named as the reason;
- independent branches continue;
- a non-blocking check failure remains visible in quality/run status but does
  not block downstream execution;
- a metadata-push failure follows its current blocking policy and must be made
  explicit in the unit result; and
- the overall run fails if any selected unit or blocking check fails.

This intentionally changes full runs from “return on first task failure” to the
behavior Build needed already approximates. Do not add a user-facing
`fail_fast` option in the first version.

### 6.5 Cancellation

Cancellation must:

1. stop admitting new units;
2. cancel the shared run context;
3. let in-flight operators honor cancellation and release their leases;
4. wait for workers to report terminal results;
5. mark never-started units cancelled (or skipped with an explicit cancelled
   dependency reason);
6. close active steps and target claims; and
7. emit exactly one terminal run state.

No worker may be left trying to send a result after the scheduler has returned.
The existing scheduled-run finalizer should remain the durable backstop.

## 7. Resource safety policy

The scheduler should use the same canonical resource vocabulary for cross-run
admission and within-run dispatch.

| Resource / behavior | Initial policy | Later optimization |
| --- | --- | --- |
| Sensor or proven no-write unit | May overlap when dependencies and connection limits allow; an accessed DuckDB file still carries an exclusive runtime coordination claim | Add read/write modes only if the operator layer can honor them consistently |
| Same canonical local file | Serialize | Consider atomic temp-write + rename where the operator owns the complete write |
| Different canonical local files | May overlap | Add live coverage for each file materialization family |
| Same DuckDB database file | Serialize the whole file | Consider shared in-process handles only after operator-family benchmarks and conflict tests |
| Different DuckDB files | May overlap | None required |
| Exact network warehouse relation | Pipeline-exclusive initially | Add a secret-free `warehouse_relation` key per proven operator family; serialize the same relation and allow distinct relations |
| Arbitrary Python | Pipeline-exclusive | Permit finer claims only through an explicit, reviewable output contract |
| Pre/post hooks | Pipeline-exclusive | Permit finer claims only if hooks gain declared resources |
| Dynamic or credential-derived routing | Pipeline-exclusive | Improve target resolution, never guess |
| Remote object target | Pipeline-exclusive | Add canonical bucket/object identities after overwrite semantics are audited |
| CLI fallback with unproven side effects | Pipeline-exclusive | Promote individual asset families after parity tests |

“Pipeline-exclusive” means the unit runs alone within its run and retains the
existing conservative cross-run admission. It must not overlap even a no-write
unit because arbitrary code may read partially written state or mutate an
undeclared resource.

Units may need multiple DuckDB paths, such as a transfer from one database to
another. Canonical keys must be deduplicated and acquired in sorted order to
avoid deadlocks.

### 7.1 Why network warehouses start conservatively

External warehouses are where parallelism will often add the most value, but
the current aggregate resource calculation deliberately treats them as
pipeline-isolated. Before adding exact relation claims, audit each direct and
fallback operator family for:

- hidden shared staging tables or files;
- overwrite/merge behavior;
- relation-name normalization and case folding;
- connection-manager thread safety;
- child-process behavior;
- metadata writes; and
- whether two assets with distinct final relations still mutate a common
  schema-level object.

Promote families separately—Postgres, BigQuery, Snowflake, and so on—rather
than weakening the policy for every warehouse at once.

## 8. Durable plan and event changes

### 8.1 Plan v3

Add a v3 typed asset contract plus explicit unit dependencies. Store resource
and connection information once per asset so catch-up plans do not repeat it
for every window. One possible shape is:

```go
type PipelineRunAssetContract struct {
    AssetID            string
    ConnectionNames    []string
    MutationIsolation  string
    MutationClaims     []PipelineRunResourceClaim
    CoordinationClaims []PipelineRunResourceClaim
}

type PipelineRunUnitEdges struct {
    Position            int
    DependencyPositions []int
}
```

The exact Go name may differ, but the persisted information must be:

- typed and validated;
- canonical and deterministically ordered;
- secret-free;
- covered by the plan identity;
- independent of presentation JSON; and
- recomputed and compared with the execution-target snapshot before the first
  side effect.

Keep aggregate `PipelineRunPlanResources` for cross-run admission. It is the
union of exact unit **mutation** claims unless any unit requires pipeline
isolation, in which case the whole plan retains pipeline isolation. Runtime
coordination claims are consumed by the in-run scheduler and operator
coordinators; they do not incorrectly turn a read-only unit into a durable
writer.

Legacy v1/v2 plans should remain readable and execute sequentially. Do not
retrofit parallel behavior by guessing per-unit claims during recovery.

### 8.2 Unit and step persistence

The durable lifecycle remains:

```text
unit queued
  -> resource/connection reservations
  -> step and unit running persisted
  -> physical operator
  -> checks and metadata
  -> completion/freshness persisted
  -> unit terminal
  -> reservations released
  -> downstream may become ready
```

Downstream admission must happen after successful completion/freshness
recording, not merely after the physical query returns. This preserves the
current synchronous freshness handoff used by Build needed.

`pipelineRunObservation` should allocate terminal ordinals under its existing
mutex. Never derive ordinal order from plan position or goroutine completion
without the durable lock.

### 8.3 Optional wait observability

Do not persist a high-volume event for every queue scan. A unit can remain
`queued` with an optional current wait class:

- dependencies;
- workspace capacity;
- run step capacity;
- connection limit; or
- write resource.

Persist or publish a wait reason only when it changes and lasts beyond a small
threshold. Resource identities shown to the UI must remain redacted and
human-readable (“DuckDB database is in use”), not expose paths or connection
secrets.

## 9. Implementation plan

### Phase 0 — lock down semantics and test seams

1. Document `max_active_steps` as the within-run step limit and keep
   `concurrency` described as overlapping pipeline runs.
2. Make the pipeline settings copy explain that Renart initially treats an
   omitted `max_active_steps` as `1`; an explicit value above `1` opts into
   local parallel execution.
3. Add v3 unit contracts and validation to the durable plan.
4. Extract connection names, mutation claims, and runtime coordination claims
   during planning.
5. Recompute and compare the contract in the execution-target snapshot.
6. Add an artificial, cancellable test operator so overlap can be measured
   without sleeping real warehouse jobs.
7. Add structured timings for dependency wait, budget wait, connection wait,
   resource wait, and physical execution.

Likely files:

- `internal/web/service/pipeline_plan.go`
- `internal/web/service/asset_physical_target.go`
- `internal/web/service/execution_target_snapshot.go`
- `internal/web/scheduler/run_plan.go`
- `internal/web/scheduler/execution_target_snapshot.go`
- `internal/web/scheduler/store.go`

### Phase 1 — shared unit DAG runner, safe local resource classes

1. Introduce a small internal execution graph package with:
   - stable node positions;
   - dependency counts and reverse edges;
   - a deterministic ready heap;
   - bounded, context-cancellable workers;
   - dependency-scoped failure propagation; and
   - guaranteed result draining.
2. Add a workspace-scoped fair budget owned by the service/runtime, not by an
   HTTP handler.
3. Add per-run, per-connection, and per-resource reservations.
4. Wrap the existing single-unit executor in a worker whose “before physical
   work” callback can return an error.
5. Start with no-write, local-file, DuckDB-file, and pipeline-exclusive
   resource policies.
6. Keep the DuckDB coordinator lease inside the operator as defense in depth.
7. Route reviewed/scoped execution through the graph runner.

Possible package boundary:

```text
internal/web/executiongraph/   generic graph and fair budget
internal/web/service/          plan/unit adapter and physical runner
internal/web/scheduler/        durable plans, events, and cross-run admission
```

The graph package should not import HTTP DTOs, presentation artifacts, or
warehouse operators.

### Phase 2 — converge full and Build-needed execution

1. Generate full-run units before physical execution.
2. Route full runs through the same graph and unit worker.
3. Replace the bespoke Build-needed asset/window loop with the same reviewed
   unit graph.
4. Remove duplicate dependency/failure loops from `direct_run.go` and
   `execution_stale.go`.
5. Make scheduled, inline, and interactive runs use the same cancellation and
   finalization path.
6. Verify `max_active_steps: 1` preserves the old lifecycle and output ordering
   except for the deliberate independent-branch failure behavior.

This phase is complete only when there is one implementation of:

- unit readiness;
- failure propagation;
- window chaining;
- connection/resource reservation;
- cancellation; and
- downstream freshness handoff.

### Phase 3 — exact network warehouse resources

For each supported materialization family:

1. audit direct and fallback operators;
2. define a canonical, secret-free `warehouse_relation` identity;
3. add same-relation exclusion and distinct-relation parallelism;
4. enforce `max_concurrent_assets`;
5. run the live warehouse parity matrix across append, replace, merge, and
   incremental windows where supported; and
6. promote the family from pipeline-exclusive only after the matrix passes.

Arbitrary Python, hooks, dynamic routing, and remote object stores remain
conservative until they have explicit contracts.

### Phase 4 — UX and rollout

1. Keep the existing `max_active_steps` field; clarify it as “Maximum active
   steps” with help text that independent assets can run together.
2. Keep `concurrency` separate and label it “Overlapping pipeline runs” wherever
   Renart exposes it.
3. In run review, state “Up to N assets may run concurrently” and note when
   conservative targets force serialization.
4. Let the run timeline show natural overlap; it already uses real timestamps.
5. Add a restrained queued reason only for a material wait.
6. Treat omitted `max_active_steps` as `1` for the first local release.
7. Consider setting new demo templates to `2`–`4` only after the live parity
   suite proves their targets are safe.
8. Keep an internal kill switch that forces effective parallelism to `1`
   during the initial release; do not make it a permanent user-facing mode.

### Phase 5 — optional DuckDB optimization

Only if profiling shows worthwhile headroom:

1. classify which operators execute through a shared in-process DuckDB owner
   and which invoke child processes;
2. benchmark concurrent distinct-table writes on representative assets;
3. test transaction conflicts, DDL, schema creation, checks, and cancellation;
4. allow relation-level concurrency only for a proven in-process subset; and
5. keep whole-file exclusion whenever any participating writer is external or
   unclassified.

This phase is independent of the main parallelism value. Different DuckDB files
can already run concurrently under Phase 1.

## 10. Validation strategy

### 10.1 Unit tests

For the graph runner:

- fan-out, fan-in, and diamond DAGs;
- stable ready ordering;
- exact `max_active_steps` enforcement;
- same-resource serialization and different-resource overlap;
- a pipeline-exclusive unit running alone;
- atomic reservation of multiple sorted resources;
- per-connection limits below the run's step limit;
- fair workspace sharing between two runs;
- cancellation before admission and during execution;
- worker panic containment;
- a failing durable-start callback preventing operator invocation;
- dependency-scoped skips; and
- result draining without goroutine leaks.

For plan v3:

- canonical ordering and identity hashing;
- invalid dependency positions and cycles;
- missing or extra unit contracts;
- aggregate claims matching the unit union;
- secret-free serialization;
- execution-target drift rejection; and
- v1/v2 sequential compatibility.

Run focused packages under `go test -race`, especially the graph runner,
`pipelineRunObservation`, the run-state registry, stream/log writers, and
inline ledger callbacks.

### 10.2 Service integration tests

Add deterministic fixtures proving:

- four independent units overlap and respect the requested limit;
- a downstream never overlaps its upstream;
- independent work continues after an unrelated failure;
- a blocking check failure skips downstream;
- a non-blocking check failure does not skip downstream;
- multiple windows of one asset remain serial;
- completion/freshness is committed before downstream admission;
- the same local file serializes while different files overlap;
- the same DuckDB file serializes while different database files overlap;
- a lower connection limit wins over `max_active_steps`;
- cancellation stops admission and closes every durable unit;
- `max_active_steps: 1` retains sequential output; and
- concurrent callbacks preserve valid ordinals and logs.

Avoid wall-clock-only assertions where a channel/barrier can prove overlap.
Keep one coarse elapsed-time assertion as a user-visible performance guard with
generous CI tolerance.

### 10.3 Live E2E

Add one small fan-out pipeline to the live suite and verify:

- overlapping bars in run details;
- deterministic unit statuses and summary order;
- aborting a run with queued and running units;
- SSE convergence without polling; and
- refresh state after parallel completion.

Extend DuckDB coordination coverage to concurrent units in one run as well as
conflicting runs/processes.

For network warehouses, extend the existing live matrix rather than adding a
single-engine smoke test. Cover multiple materialization strategies and
incremental windows where the engine supports them.

Repeat the narrow live paths with Playwright `--workers=1 --retries=0` to prove
the synchronization is deterministic before relying on the full suite.

### 10.4 Performance acceptance

Use reproducible benchmarks with four independent equal-duration units:

- at `max_active_steps: 1`, duration should remain approximately the sum;
- at `max_active_steps: 4`, duration should approach the longest branch plus
  bounded scheduler overhead;
- four writers to one DuckDB file should remain serial and error-free;
- four writers to four DuckDB files should overlap;
- event/log persistence should not dominate short units; and
- cancellation latency and goroutine count should return to baseline.

Record the configured step limit, effective peak parallelism, and wait-time
breakdown in test output so a regression can distinguish scheduling,
connection, resource, and operator bottlenecks.

## 11. Rollout and compatibility

- New code reads v1/v2 plans but executes them sequentially.
- New plans use v3 unit contracts.
- Existing pipelines with omitted `max_active_steps` remain sequential in the
  first local release.
- `max_active_steps: 1` is the explicit compatibility escape hatch.
- The initial release retains an internal force-sequential kill switch.
- Do not raise demo defaults until safe-resource live coverage passes.
- Do not promote a resource family based only on unit tests.
- Update `architecture/backend.md` and `architecture/staleness.md` with the
  as-built model, then delete this plan when all core phases ship.

## 12. Principal risks and mitigations

### Durable state says “queued” after physical work started

Make the running event and target-write binding a fallible precondition in the
worker. No callback that protects durability may be fire-and-forget.

### A large scheduled run starves interactive work

Use a fair workspace arbiter across runs, not just a global buffered channel.

### DuckDB appears safe in unit tests but fails through a child process

Keep whole-file exclusion and the existing advisory coordinator until each
operator path is explicitly classified.

### Parallel logs become unreadable

Keep per-asset capture, prefix streamed lines with the asset name, and render
the final summary in plan order. Do not concatenate unsynchronized shared
buffers.

### Completion events race with downstream readiness

Treat durable completion/freshness recording as part of the unit. Release its
dependency edge only afterward.

### Connection objects are not thread-safe

Audit manager and driver lifetimes. A connection limit of `1` must remain
possible even when resource targets differ. Operators that cannot safely share
the manager stay exclusive until fixed.

### Concurrency exhausts local memory or child processes

Use both the per-run and workspace caps. Measure process count and memory in the
performance fixture before increasing defaults.

## 13. Recommended delivery slices

Keep commits independently reviewable:

1. plan v3 and per-unit resource/connection contracts;
2. graph runner, fair workspace budget, and race tests;
3. reviewed/scoped execution behind a force-sequential kill switch;
4. full-run and Build-needed convergence;
5. the first audited native warehouse families and their live parity matrix;
6. run-review/timeline observability and live E2E;
7. one commit per additional promoted warehouse or file family; and
8. architecture documentation plus plan removal after the core path is stable.

The first user-visible performance release should include slices 1–6. That
ensures the shared execution path is complete and at least the primary
warehouse use cases actually benefit. Shipping only the full-run concurrent
executor would create a fast path that disagrees with the reviewed and needed
paths and should be avoided.
