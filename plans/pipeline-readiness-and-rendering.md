# Pipeline readiness, execution planning, and rendered SQL

Status: implementation in progress. Shipped slices are recorded in
`architecture/`; target behavior below remains proposed unless it appears
there.

## 1. Recommendation

Renart should not turn the current controls into a mandatory linear wizard.
There are two independent kinds of state:

1. **Definition readiness**: are the saved files valid, renderable, deployed,
   and pinned to the intended schedules?
2. **Data freshness**: for one environment and time range, which assets are
   missing, stale, partially covered, volatile, running, or fresh?

`Build stale` changes data freshness. It is not inherently a prerequisite for
deploying a source snapshot. Conversely, a fresh warehouse does not mean the
current source is valid or deployed. Putting the actions into a single required
sequence would hide this distinction and become awkward for normal local work.

The recommended direction is therefore:

- replace the flat row of peer controls with a persistent **Readiness** summary
  that has separate Definition and Data sections;
- make **Run…** open a real, read-only execution plan with explicit source,
  environment, interval, scope, validation, and rendered operations;
- keep **Deploy** separate from running data, but reuse the same plan and
  rendering contract before creating a deployment snapshot;
- make every run say which source it will execute: saved working tree, a named
  deployment, or a schedule's pinned deployment;
- make every mutating execution create the same durable run/provenance record,
  whether River dispatches it or it streams synchronously;
- put planning in front of Renart's patched `HybridBruinExecutor` construction,
  using Bruin's render/materializer primitives underneath instead of inventing
  another SQL templating or materialization layer;
- add the smallest coherent version first. Do not adopt SQLMesh's warehouse
  environment/snapshot model unless Renart later needs isolated data
  environments and promotion as a first-class product capability.

The target shape is:

```text
                         definition readiness
saved working tree ──> checks + render plan ──> deployed source ──> schedule pin
        │                       │
        └────────────── run selected work ───────────────┐
                                                         v
data state (per env + range)      missing/stale/partial ──> running ──> fresh
```

The horizontal lines are not one required pipeline. A user may plan or run a
working tree without deploying it, and may deploy without rebuilding existing
data. The UI should show both dimensions without pretending otherwise.

This proposal does not add ceremony to the asset-editing hot path. The existing
one-click asset Materialize action and stale-upstream prompt remain. The
pipeline-scope Run action opens the reviewable plan; every action can use the
same planner internally without forcing the panel to open.

## 2. Audit baseline before implementation (2026-07-16)

This section preserves the pre-Phase-0 audit that motivated the proposal. It
is not the current-state contract; `architecture/` and the dated implementation
checkpoints below take precedence.

### 2.1 Operation matrix

| Operation            | Source used                                                          | Context                                                            | What it checks                                                                                        | Side effects                                                                     | Durable Renart run   |
| -------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------- |
| Pipeline type-check  | Saved working tree                                                   | Pipeline variables and a derived/selected interval; no environment | Dependencies, materialization declarations, rendered logical SQL, inferred/declared columns and types | None                                                                             | No                   |
| Pipeline dry-run     | Saved working tree                                                   | Selected environment, but a separate Bruin lint/dry-run renderer   | Bruin lint, supported warehouse query validation, custom checks, and hook queries                     | Intended to be validation-only; may contact the warehouse                        | No                   |
| Build stale          | Saved working tree                                                   | Environment and interval                                           | Recomputes staleness, applies run policy, then normal runtime dependency validation                   | Builds every non-fresh selected asset/gap in topological order and records facts | No pipeline-run row  |
| CLI `renart run`     | Saved working tree, even when delegated to an open server            | Environment, interval, selection, refresh/full-refresh flags       | Runtime dependency validation; not Renart type-check                                                  | Materializes assets and records facts                                            | No scheduler run row |
| Build-toolbar `Run`  | Latest deployed snapshot if one exists; otherwise saved working tree | Environment and interval                                           | Normal runtime validation; not Renart type-check or dry-run                                           | Runs the full DAG and records facts                                              | Yes                  |
| Deploy               | Saved working tree under the pipeline directory                      | No environment or interval                                         | Nothing beyond reading and hashing files                                                              | Creates an immutable source snapshot                                             | No                   |
| Environment schedule | The schedule row's pinned deployment                                 | Environment and scheduled interval                                 | Normal runtime validation only                                                                        | Runs the full DAG, records facts/logs/steps, advances the schedule watermark     | Yes                  |
| Schedule-row `Run`   | Latest deployment, not necessarily the row's displayed pin           | Environment only                                                   | Normal runtime validation only                                                                        | Runs the full DAG                                                                | Yes                  |

This is the most important current-state finding: **`Run` does not have one
source-version meaning**. The Build-page button changes from working-tree
execution to latest-deployment execution after the first deploy, while CLI
`renart run`, asset materialization, and Build stale continue to execute the
working tree. A schedule row's manual Run also does not use that schedule's
pinned version.

The relevant implementation paths are:

- type-check: `cmd/typecheck.go`, `internal/web/service/typecheck.go`, and
  `internal/web/httpapi/pipeline.go`;
- Build stale: `internal/web/httpapi/build_stale.go` and
  `internal/web/service/execution_stale.go`;
- direct/CLI execution: `cmd/run.go`,
  `internal/web/httpapi/pipeline_execution.go`, and
  `internal/web/service/direct_run.go`;
- queued runs and source resolution: `internal/web/scheduler/service.go`,
  `cmd/server.go`, and `cmd/web.go`;
- deployment snapshots: `internal/web/snapshot/store.go` and
  `internal/web/httpapi/deploy.go`.

### 2.2 Type-check is useful but isolated

Type-check reloads the saved pipeline, derives an execution window, renders the
logical SQL with Jinja/macros/pipeline variables, and checks it against schema
information from other assets. It also reports dependency errors,
materialization configuration errors, Python query dependency findings, and
missing output-column declarations for non-SQL table producers.

It currently has several limits that matter to the workflow design:

- it has no environment input and does not use an environment connection or
  schedule-specific variables;
- SQL positions refer to rendered SQL and are not mapped back to source spans;
- it is an ephemeral response with no source fingerprint or checked
  revision;
- the Build-page effect is not keyed to an explicit content fingerprint. It can
  rerun when pipeline/workspace reconciliation changes component inputs, but
  neither the UI nor the response can prove that a report still matches the
  latest saved files; a manual bell action reruns it;
- transport failures are reduced to an empty/null state in the UI;
- it does not gate asset materialization, Build stale, Run, Deploy, schedule
  creation, or scheduled execution;
- CLI warnings fail only with `--strict`.

Type-check should remain best-effort during authoring. Incomplete SQL, Python,
or YAML must remain saveable. Stronger gates belong to an explicit Run or
Deploy action, and only deterministic findings should become blockers.

### 2.3 Dry-run is a second, mostly hidden validation system

The pipeline materialization endpoint accepts `dry_run`. That path uses Bruin
lint rules plus supported warehouse validators and check/hook dry-runs. The
frontend hook contains support for it, but there is no current Build-page
control that invokes it. The current implementation selects the requested
environment but creates its Jinja renderer with Bruin's `yesterday` defaults,
so it does not share the interval context already selected for type-check or
the prospective run.

It is not the same as type-check:

- type-check is Renart's local, DAG/schema-aware static analysis;
- dry-run is Bruin lint plus connection-aware validation and can require
  existing upstream relations;
- neither reuses or gates the other;
- dry-run does not return a statement plan or rendered materialization SQL.

This distinction should be explicit in the product. The recommended names are
**Code checks** and **Warehouse validation**. Warehouse validation should stay
optional because new upstream tables/columns can cause legitimate false
negatives, a limitation also documented by Bruin.

### 2.4 Staleness and Build stale

Staleness is already the strongest part of this lifecycle. It combines the
current working-tree fingerprint with achieved fingerprints and interval
coverage for one environment and time range. It distinguishes fresh, edited,
upstream-changed, partial, never-built, missing, and volatile assets; the last
run attempt is orthogonal to freshness.

At click time, Build stale recomputes statuses server-side, converts every
non-fresh asset into a topologically ordered plan, uses exact uncovered gaps for
partial incrementals, executes upstreams first, and skips downstream work after
an upstream failure. This is correct behavior to preserve.

Variable-aware primitives already exist: asset fingerprints hash consumed
variables and coverage rows carry the canonical full-variable-map hash. The
HTTP staleness/Build-needed paths and materialization recorder do not yet carry
the same overrides end to end, however. In addition, historical coverage for
an older code/variable variant can be classified as fresh even after a later
run overwrote the same mutable physical target. Schedule variables must not be
enabled in execution without resolving both gaps.

The existing “latest” helpers are not a complete last-writer identity:
`LatestFingerprint` omits variables/target and reads raw facts that retention
prunes, `LatestOwnContent` reads durable coverage but returns only own-content,
and the latest-run row may describe a failure. No current fact or coverage row
identifies the resolved physical destination.

The UX does not expose enough of that plan:

- the action disappears when everything is fresh rather than showing a
  positive state;
- environment and interval live remotely in the global header;
- gap counts are shown, but not the actual ranges;
- volatile sensors can keep the action permanently present without explaining
  their special semantics;
- there is no validation or rendered-operation summary;
- the operation has no durable pipeline-run record or canonical run-details
  link.

### 2.5 Run execution is split across two paths

Direct pipeline/asset endpoints execute the saved working tree. The CLI uses
those endpoints when a Renart server is open and the same service path in
embedded mode. Build stale also uses direct asset execution.

The Build toolbar instead queues a River scheduler run. The scheduler's runner
always attempts snapshot resolution. With no explicit version it chooses the
latest deployment and only uses the working tree when no deployment exists.
That makes the Build-page label `Run` materially ambiguous.

There are related context losses:

- schedule-row manual Run sends only pipeline and environment, so it does not
  identify the row's pinned snapshot;
- run-details `Re-execute` does not preserve the original environment, window,
  source version, or trigger context;
- schedule variables are accepted and persisted but are not passed through job
  arguments to the renderer/executor;
- trigger-time backfill intent and protected-environment confirmation are used
  by an initial policy check and then dropped from the queued run contract;
  full-refresh is not represented there at all. The eventual runner therefore
  cannot record or reapply the requested destructive context and executes the
  normal incremental path for the selected interval;
- scheduler-backed manual runs use scheduled sensor behavior, while direct/CLI
  runs use interactive sensor behavior;
- the HTTP caller supplies the `trigger` string. Labeling a request as
  `schedule` can bypass the interactive policy branch and makes a successful
  run eligible to advance the schedule watermark;
- active-run admission checks for an existing row and inserts the new row in
  separate operations. Concurrent requests can both pass, while direct and
  Build-needed work are not represented in that check at all.

Scheduled job insertion does have a narrower duplicate defense that must be
preserved: River `ByArgs` hashes pipeline UUID, environment, and normalized
start/end and suppresses a second job while the first is active. It is not a
permanent occurrence ledger, and scheduled run rows are still created later in
the worker after a separate active-run check.

These are correctness issues to resolve before making a polished plan UI.
They share one architectural cause: run identity/provenance and River dispatch
are coupled. A synchronous execution should be able to stream immediately and
still create the same durable run record as a queued execution; River should be
only one dispatch mechanism.

### 2.6 Deploy and schedules

Deploy recursively snapshots source files below the pipeline directory,
excluding repository internals, caches, local Renart state, logs, virtual
environments, and database artifacts. Blobs are content-addressed and snapshots
record a Merkle root plus Git SHA/dirty metadata. Identical content is a no-op.

Deploy intentionally stores source rather than rendered SQL because scheduled
rendering depends on the actual environment, variables, execution window, and
full-refresh mode. That is the right base model and should remain.

What deploy does **not** do today:

- type-check, render, dry-run, or execute the source;
- bind the snapshot to an environment;
- update any existing schedule pin;
- reject dirty, incomplete, or invalid definitions.

New environment schedules require an explicit snapshot, `deploy_now`, or an
existing pin. `deploy_now` atomically snapshots the current working tree and
pins that one schedule. A standalone Deploy creates a new latest snapshot but
leaves every schedule on its previous pin. The Schedules page correctly marks
such rows as using an older deployment, but the Build-page deploy tooltip and
manual Run semantics do not consistently explain this.

Two current safety gaps weaken the pin contract:

- schedule upsert does not verify that a caller-supplied snapshot exists or
  belongs to the selected pipeline;
- if runtime snapshot reconstruction fails, the runner logs a warning and can
  fall back to the working tree (a deployed-only policy may reject the later
  run). A requested pinned version should instead fail closed.

There is an intentional legacy exception: migration from the older
`pipeline.yml` scheduler creates environment-schedule rows with an empty pin.
Those rows retain the historical `latest deployment, otherwise working tree`
behavior. A blanket fail-closed rule would therefore stop migrated schedules;
their source must be pinned by a data migration before empty pins are forbidden.

The snapshot intentionally excludes workspace-level `.bruin.yml`; a scheduled
snapshot run uses the live workspace connection/environment configuration.
That keeps credentials out of deployments, but it means a source deployment
alone is not a complete immutable execution environment. Plan/run provenance
should therefore identify the environment/configuration digest without storing
credentials.

There are also two schedule concepts:

- `pipeline.yml` contains Bruin's version-controlled schedule and supplies a
  default execution interval;
- Renart's `(pipeline UUID, environment)` rows in `state.db` contain the actual
  local operational trigger, source pin, catch-up policy, and lifecycle state.

The UI should call the former the pipeline's **default interval/cadence** and
the latter an **environment schedule**. Editing one must not imply that the
other has changed.

Scheduler ownership is another hidden part of current schedule state. Only one
process owns the scheduler lock. A follower attempts that lock once, does not
take over automatically if the owner exits, and can accept durable schedule
changes that do not reach the owner's in-memory registrations. It may still
display calculated next-run times even though it dispatches nothing. This is a
reliability issue, not merely missing copy: ownership, heartbeat, handoff, and
cross-process reconciliation need a real contract as well as visible status.

### 2.7 Rendering checkpoint

At the start of this investigation Renart had pieces of rendering but no
pre-execution render artifact. Phase 1 now provides a saved-source asset render
response, a Build `Render` view, and `renart render`; the exact shipped boundary
is recorded in `architecture/backend.md` and `architecture/frontend.md`.
Important gaps from the original picture remain:

- `/api/assets/{id}/render-jinja` still serves editor content for inline Monaco
  values/tooltips; it is not the execution-plan interface;
- type-check renders logical queries internally but returns diagnostics only;
- Inspect and ad-hoc results can display the rendered SELECT after the action;
- dry-run validates but does not expose statements;
- run output is a combined text log, not a structured execution plan;
- Renart has no pipeline `plan` endpoint or `renart plan` command.

The visible `Impact plan` is currently hard-coded fixture data, and its `Run
plan` button only closes the dialog. The variant selector is also fixture-backed
and does not affect execution. Those surfaces must be removed, clearly marked
as a prototype, or replaced by real behavior before a genuine plan feature is
introduced.

## 3. Prior art and what to borrow

### 3.1 Bruin: canonical execution rendering

Bruin's [`render` command](https://getbruin.com/docs/bruin/commands/render.html)
already renders a SQL asset into its materialized execution output. It supports
raw-query output, start/end dates, variables, full refresh, interval modifiers,
config selection, and JSON. Bruin's
[`validate` command](https://getbruin.com/docs/bruin/commands/validate.html)
supports environment-aware validation and warehouse dry-runs, while explicitly
warning that new relations or columns can produce false negatives. Bruin
[`run`](https://getbruin.com/docs/bruin/commands/run.html) validates by default
and then executes main tasks and checks in DAG order.

Lesson: Bruin owns materialization semantics. Renart should expose or factor
that behavior into a reusable in-process planning contract, not recreate DDL
with templates or regex. Bruin's current command implementation is
CLI-oriented, SQL/query-sensor focused, and returns an undifferentiated SQL
blob, so Renart still needs a structured service boundary around the same
runtime factories.

### 3.2 SQLMesh: review changes before applying them

An SQLMesh [plan](https://sqlmesh.readthedocs.io/en/stable/concepts/plans/)
compares local state to a target environment and presents added, removed,
directly changed, indirectly affected models, missing intervals, diffs, and an
affected date range before it applies warehouse changes. SQLMesh can categorize
breaking versus non-breaking changes and uses
[isolated environments](https://sqlmesh.readthedocs.io/en/stable/concepts/environments/)
to test changed model versions. Its
[`render` command](https://sqlmesh.readthedocs.io/en/stable/reference/cli/#render)
accepts interval/execution-time context, dialect selection, and dependency
expansion.

Lesson: make impact and missing intervals reviewable before execution, and
separate applying source changes from processing data. Do not copy SQLMesh's
physical snapshot/environment machinery yet: Renart and Bruin currently write
the user's named targets directly, so pretending to offer isolated environments
would be misleading.

### 3.3 dbt and dbt Cloud: compile automatically, keep artifacts inspectable

[`dbt compile`](https://docs.getdbt.com/reference/commands/compile) makes
executable SQL inspectable but is explicitly not a prerequisite for build
commands; those compile automatically. [`dbt build`](https://docs.getdbt.com/reference/commands/build)
composes models, tests, snapshots, and seeds in DAG order, records combined
artifacts, and skips affected downstream nodes after blocking upstream test
failures. dbt Cloud's
[deployment environments](https://docs.getdbt.com/docs/deploy/deploy-environments)
bundle deployment context, while its
[job scheduler](https://docs.getdbt.com/docs/deploy/job-scheduler) treats a job
as steps, settings, and a trigger, and retains logs/artifacts for runs.

Lesson: rendering should be automatic inside planning/execution, not another
manual ceremony. Validation and checks should be visible as steps of one run,
and the resulting context and artifacts should be durable.

### 3.4 Dataform: compilation results are first-class

Dataform compiles workspace code in real time, lets users inspect compiled
queries and the compiled graph, and creates a context-specific compilation
result. Release configurations produce compilation results, and workflow
configurations schedule those results. Compilation adds the actual `CREATE`,
`REPLACE`, or `INSERT` boilerplate around source queries. See the
[Dataform overview](https://cloud.google.com/dataform/docs/overview) and
[compilation configuration](https://cloud.google.com/dataform/docs/configure-compilation).

Lesson: rendered execution output is more useful as a first-class,
context-bound artifact than as a post-run log detail. Renart can get this
benefit without storing rendered SQL as its deployment format.

## 4. Product directions

### Option A: clarify the existing controls

Keep the current backend operations and make the UI truthful:

- replace the bell with `Problems` or `Checks` and show a positive checked
  state;
- rename `Build stale` to `Build needed` and always show `Fresh` when there is
  no work;
- rename the pipeline-wide button to `Run pipeline`;
- label every run with `Working tree`, `Deployment <id>`, or `Pinned <id>`;
- add `Compiled query` and `Execution SQL` tabs to SQL assets;
- surface environment/window next to each action;
- remove the fixture Impact plan and variant controls.

Advantages: small, low risk, and immediately less misleading.

Disadvantages: users still have to mentally compose checks, scope, render,
execution, and deployment. It does not create one backend truth for what will
run.

### Option B: Readiness plus a shared execution plan (recommended)

Introduce a read-only plan service and make it the common input to Run,
Build-needed, deployment review, and scheduled-run artifacts.

The Build page gets:

- a persistent Readiness summary with separate Definition, Data, and Deployment
  state;
- a primary `Run…` action that opens a plan for `Needed`, `Entire pipeline`, or
  `Selected asset/closure` scope;
- a `Deploy…` action that plans the saved working tree, creates an immutable
  deployment, and then explicitly offers to update selected schedule pins;
- an asset-level rendered view backed by the same plan service.

Advantages: one truthful model, close to Renart's current architecture, useful
for local work, and extensible to richer deployment artifacts later.

Disadvantages: requires a shared renderer/executor boundary, source-version
cleanup, structured run inputs, and a universal run/operation schema.

### Option C: full plan/apply environments and promotion

Adopt a SQLMesh/Dataform-like lifecycle:

```text
workspace draft -> immutable compilation -> development apply -> approval ->
production promotion -> scheduled processing
```

Every environment would name a promoted model version, and Renart would manage
isolated or shared physical targets.

Advantages: strongest safety, reproducibility, impact analysis, and audit
story.

Disadvantages: substantial warehouse state/version management, target naming,
garbage collection, migration, and user ceremony. It also risks diverging from
the Bruin CLI's direct-target mental model.

Recommendation: implement Option B, include enough provenance that an immutable
compilation artifact could be added later, and avoid Option C until isolated
data environments are a concrete product requirement.

## 5. Recommended UX

### 5.1 Readiness is a summary, not a gate button

Replace the separate bell/vanishing stale action/opaque Deploy state with one
compact summary such as:

```text
Readiness   2 code issues · 4 assets need work · 3 undeployed files
```

Opening it shows:

**Definition**

- saved pipeline source identity and whether editor changes are still unsaved;
- code-check result, checked context, and whether it is current;
- renderability by asset;
- deployment drift and latest deployment identity;
- configured environment-policy warnings.

**Data for `prod`, 2026-07-15 -> 2026-07-16**

- fresh/stale/partial/never-built/volatile counts;
- exact uncovered intervals;
- latest failed/cancelled attempts separately from freshness;
- `Build needed…` action.

**Schedules**

- schedules on the latest deployment;
- schedules pinned to older deployments;
- paused/active state and next run;
- scheduler ownership/liveness (`active here`, `managed by another process`, or
  `unavailable`) plus last heartbeat/reconciliation;
- an active pipeline execution, with a link to its run details.

The summary must show positive states (`Checks pass`, `All data fresh`,
`Deployment current`) instead of removing the affordance when there is nothing
to do. Status and action are separate concepts: status is persistent,
non-mutating, and may open details; actions remain in stable positions and
disable with an explanation when unavailable. For example, `Build needed…`
remains visible but disabled when all data is fresh, and `Run…` links to the
active run when the pipeline's run slot is occupied.

### 5.2 `Run…` is scoped and explicit

The selected environment resolves a default source once, rather than asking the
user on every click:

- normal interactive environments default to the saved working tree;
- `deployed_only` environments resolve the latest deployment to one immutable
  version at plan time, do not allow a working-tree override, and block with a
  Deploy action when no deployment exists;
- a schedule-row action always defaults to that row's exact pin.

A later explicit `default_run_source` environment policy can generalize this,
but Phase 0 can derive the rule from `deployed_only`. The source is never chosen
merely because some deployment happens to exist. Normal environments may offer
a per-run deployment override.

Variables follow the same explicitness rule. A generic Build-page run starts
from the selected source's pipeline defaults plus visible ad-hoc overrides; it
does not inherit an environment schedule's variables. `Run pinned` starts from
that row's pin and schedule overrides, which the panel labels separately.

The global environment/window controls remain useful as **view context** for
the DAG and freshness. The Run surface owns **execution context** and repeats it
at the action point, so a distant header change cannot silently alter a click.
The plan panel starts with a compact context header:

```text
Source       Saved working tree @ 8f61c2…
Environment  dev
Window       2026-07-15 00:00 -> 2026-07-16 00:00 UTC
Variables    defaults + 2 overrides
Mode         incremental · sensor once
```

`Mode` displays the effective values returned by the same context resolver the
runner will use after policy normalization; it must not guess `sensor once`
while the eventual path silently changes it to `wait`.

Scope presets:

- **Needed**: the current Build-stale selection and exact gap intervals;
- **Entire pipeline**: all assets regardless of freshness;
- **Selected asset**: selected asset, optionally with upstream/downstream
  closure;
- **Custom selection**: a later extension, ideally compatible with Bruin's
  selector vocabulary.

Tabs or sections:

1. **Summary**: asset count, estimated intervals, destructive operations,
   blockers/warnings, and source provenance.
2. **Assets**: topological list with inclusion reasons (`explicit`, `stale
edited`, `uncovered interval`, `required upstream`, `volatile sensor`).
3. **Checks**: code findings, warehouse-validation status, and runtime quality
   checks that will execute.
4. **Execution**: compiled query and rendered operations per asset.

The final button is concrete: `Run 7 assets from working tree`. It never says
only `Run` when the selected asset makes pipeline scope ambiguous.

The panel should open immediately with context, selection, and cached checks.
The stage graph and deterministic renderability checks must complete before a
mutation is authorized; only expensive statement content serialization,
formatting, and delivery should load lazily as an asset is expanded. Rendered
content is cached by source and execution-context identity. Confirmation
performs the complete server-side verification even if the user never expands
an asset. A pipeline's active execution is shown before confirmation; the
action is disabled or offers navigation rather than exposing the current raw
`pipeline already has a queued or running run` error. The backend's atomic
admission check remains authoritative if another request wins the race.

Asset-scoped Materialize remains a one-click action with its existing
stale-upstream prompt. It records the same provenance internally, but opening
the pipeline plan is not required for the normal edit/materialize loop.

### 5.3 Rendering is available both per asset and per plan

For a SQL/query-sensor asset, use a read-only Monaco surface with:

- **Compiled query**: after macros, Jinja, variables, and interval context but
  before materialization;
- **Execution SQL**: the materializer output plus hooks, with all actual
  `CREATE`, `ALTER`, `MERGE`, `INSERT`, `TRUNCATE`, or other statements;
- **Checks**: rendered custom/column-check statements when available.

The header repeats source, environment, window, variables, full-refresh mode,
dialect, and connection. It says `Preview — not executed` and offers copy for
one operation or all operations.

When a deployed snapshot exists, add a valuable comparison:

```text
Working tree | Active schedule deployment | Diff
```

This directly explains why the current editor and a future scheduled run may
produce different SQL.

For non-SQL assets, show truthful semantic operations rather than fabricated
SQL:

- Seed: source file, Sling destination, mode, and table;
- Load: source connection/object, destination, mode, and selected columns;
- API: method/host/path, pagination and destination, with credentials redacted;
- Python: module/function, connection/target, and materialization mode; dynamic
  runtime SQL is explicitly `not statically renderable`;
- Sensor: rendered condition/query and sensor policy;
- Checks/metadata: separate planned stages.

### 5.4 Deploy is separate from data execution

`Deploy…` reviews the **saved** working tree, never an unsaved Monaco buffer.
It should:

1. generate the same code-check/render plan for a chosen environment and a
   representative interval;
2. show exact added/changed/removed files and source fingerprint;
3. create an immutable source deployment;
4. show schedules currently pinned to older deployments;
5. let the user explicitly update selected schedule pins.

The deployed artifact remains source, not rendered SQL. Scheduled runs create
their actual plan at runtime using the scheduled interval, environment, and
variables. The deploy preview must say that the representative rendered SQL
can differ for later schedule intervals.

Keep the word **Deploy** consistently in UI, API documentation, and help text;
`Release` must not appear as a second apparent lifecycle operation. Give each
deployment a persisted human-readable per-pipeline ordinal while retaining its
UUID as the internal identity, for example
`Deployment #7 · 8f61c2 · 2 hours ago` (plus Git SHA or dirty state where
useful). Backfill existing ordinals deterministically by creation time.
Identical no-op deploys retain the existing deployment identity and ordinal.

Schedule creation should replace the ambiguous `Deploy now` boolean with an
explicit source choice:

- `Use deployment <id>`;
- `Deploy current saved workspace after review`.

Schedule-row manual execution should say `Run pinned <id>` and send that exact
pin. A separate `Run current workspace` action may exist, but it must be
visually and semantically distinct. A manual `Run pinned` is still a manual run
and must not advance the schedule watermark; only the scheduler's actual
interval trigger does that.

`Re-execute` from run details is offered as exact only when the original source
and every context input, including effective variables, are still resolvable.
Otherwise the UI prompts for missing overrides and verifies their digest, or
labels the action `Run again with current settings`.

Deploy and pin assignment verify manifest ownership, safe relative paths,
referenced blobs, and content hashes. Runtime still verifies and fails closed,
then materializes each run into an isolated directory under Renart state. A
later performance optimization may cache an immutable verified base and
copy/reflink/overlay it into each run sandbox, but execution must never use a
shared writable cache directory directly.

## 6. Canonical plan contract

### 6.1 Universal run and execution context

Every mutating execution uses one versioned `Run`/`RunSpec` contract: one-click
asset materialization, Build needed, a full pipeline Run, direct/CLI execution,
a manual pinned run, and an actual scheduled tick. A run record is created
before dispatch and stores the resolved source and execution context. River is
only the `queued` dispatch option; `inline_streaming` actions stay fast and
stream normally while producing the same provenance.

Execution jobs should carry only a run ID. The runner reconstructs behavior
from the persisted, server-normalized context instead of lossy parallel job
arguments. The context includes source kind and immutable version, environment,
effective variables or a replayable reference, interval, selection,
full-refresh/backfill intent, sensor policy, schedule identity, and policy
authorization. Policy is checked at admission and again immediately before
side effects against that same context.

That simplification must not remove scheduled-tick idempotency. Today River's
`ByArgs` uniqueness suppresses a second due/catch-up job for the same
`(pipeline UUID, environment, normalized start, normalized end)` while the
first is available, pending, scheduled, or running. A unique run ID would
defeat that active-job defense.

The universal model moves the authoritative identity into Renart storage.
Every actual due/catch-up interval receives a server-derived
`schedule_occurrence_key` from stable schedule identity and its normalized
half-open interval. A durable uniqueness constraint ensures that duplicate
admission returns the existing occurrence/run and never creates a second
concurrent or already-successful execution. Claiming the run slot, persisting
its normalized `RunSpec`, and inserting its River execution job happen in one
SQLite transaction via `InsertTx` (or an equally durable outbox). Retries reuse
the occurrence/run identity and retain attempt history. Keep the existing
`ByArgs` guard until this replacement passes restart, leader-churn, and catch-up
concurrency tests.

River requires periodic constructors not to block, so they must not write run
rows. Periodic and catch-up firing instead share one idempotent due-occurrence
admission path: either a Renart-owned timer performs transactional admission,
or River queues a lightweight due-occurrence signal whose worker does so. Such
a signal may carry only immutable schedule identity/revision and normalized
interval; it is not an execution contract. The resulting execution job carries
only the run ID. A versioned worker must continue decoding already-persisted
legacy scheduled arguments during upgrades because available River jobs survive
process restarts.

Invocation origin (`manual`, `scheduled`, `api`, or `cli`) and the capability
to advance a schedule watermark are server-owned provenance. They are not
accepted from a client and are not inferred from `RunID`, River dispatch, or a
snapshot directory. Only a successful run admitted for an actual due/catch-up
schedule interval has `advances_watermark`; manual pinned runs and
re-executions never inherit it.

Each run contains execution units rather than assuming one step per asset:

```text
ExecutionUnit
  asset identity
  optional interval start/end
  inclusion reason
  operation/stage references
  status, timestamps, error, output
```

This represents Build-needed's asset-by-gap-window work without forcing it
into one Bruin pipeline invocation; the run owns their topological order and
downstream-of-failure skip semantics. Pipeline-scope mutating operations
compete for one atomic durable run slot initially, preserving today's
conservative cross-environment limit. Admission returns typed
`409 pipeline_run_active` with the active run ID. Scheduled work is visibly
deferred/retried rather than silently consumed. Asset-scoped concurrency can
retain a narrower policy, but it still uses the universal run record.

The initial pipeline-global slot is intentional, not a claim that it is the
ideal final policy. Environment names do not prove physical isolation: two
environments may target the same connection/table, and DuckDB or local outputs
can impose single-writer constraints. A blocked scheduled occurrence retains
its original interval/idempotency key, displays the blocking run, and retries.
After every path uses atomic admission, evolve toward physical-target/write-
resource locks, permitting `(pipeline, environment)` concurrency only when the
destinations are explicitly isolated. This avoids making a long dev run delay
prod forever without assuming that different environment labels are safe to
run concurrently.

### 6.2 Inputs

One backend planning service should accept all context that can change
behavior:

```text
pipeline identity
source = working_tree | snapshot(version_id)
expected source Merkle root
environment/configuration digest
environment
effective variables + canonical digest + value provenance
execution start/end/execution time
full refresh / backfill intent
sensor policy
selection = needed | all | asset closure | selector
expected data-state token (needed selection only)
warehouse validation = off | supported
```

Variable precedence is explicit and source-scoped:

```text
executed source's pipeline defaults
  < schedule overrides (only an actual schedule or Run-pinned action)
  < explicit ad-hoc run overrides
```

A generic Build-page run does not inherit variables merely because its
environment has a schedule. Overrides are validated against the exact source
being executed because a snapshot's declarations/defaults may differ from the
working tree. Unknown keys and values that violate declared types are rejected.

The shared context builder applies a strict `pipeline.PipelineMutator` before
asset construction, then computes digest/provenance from the resulting
effective values. Passing overrides only to a base Jinja renderer is
insufficient: Bruin's asset renderer and Renart's Python runtime both read
values from the parsed pipeline.

Draft overlays can be added later for editor-only previews, but execution and
deployment must continue to use saved filesystem content. If the editor has
unsaved content, the plan says it is excluded.

Source must be required in the resolved context rather than inferred from
whether a snapshot happens to exist. The environment policy supplies a visible
default, and the final request names the exact source. `Latest` may be a UI
choice, but it resolves to one immutable version before confirmation and is
never stored in a run. This fixes the current Build-page/CLI discrepancy
without imposing a recurring choice.

All behavior-changing input is parsed strictly. Invalid JSON, timestamps,
source IDs, variables, sensor modes, or confirmation values return structured
errors; they never silently become an empty/default request. One
`ResolveExecutionContext` function produces the normalized context consumed by
type-check, planning, policy checks, fingerprints, and execution.

### 6.3 Output

The response is structured and read-only:

```text
Plan
  provenance
    source kind, version/source Merkle root, config digest, Bruin/Renart versions
    pipeline, environment, effective-variables digest/provenance, interval, modes
  readiness
    blockers, warnings, code-check status, warehouse-validation status
  selection
    requested scope, data-state token, preview assets, dependency order, gap windows
  assets[]
    identity, type, dialect, connection, secret-free target/write-resource identity
    inclusion reasons and staleness state
    stages[]
      pre-hook | query | materialization | check | post-hook | metadata
      language, content, render fidelity, destructive metadata, redactions
  summary
    asset/window/stage counts and destructive-operation count
```

Do not classify destructive SQL, split statements, or infer phases with regex.
Those fields must come from materializer/executor semantics. If Bruin initially
returns one canonical SQL blob, show it as one exact operation until a
structured upstream contract exists.

Each stage declares render fidelity:

- `exact`: produced by the same renderer/materializer path execution uses;
- `semantic`: a truthful non-SQL operation description;
- `runtime_only`: cannot be known without executing user code;
- `unsupported`: execution itself is unsupported in this path.

This is more honest than presenting every preview as exact SQL.

For `needed` scope, distinguish the preview selection from the final execution
units recorded at confirmation. Each execution unit is an asset plus an
optional gap window and inclusion reason; it is not merely one asset-level
step.

### 6.4 One renderer, shared with execution

The implementation must not add a third materializer registry.

The planner seam is Renart's executor construction, not Bruin's raw default
registry. Python runs through Renart's SDK/broker path, seeds through Sling,
and API/Load are Renart-owned types; a preview built directly from Bruin's
default factories would be falsely labeled exact for those assets.

The desired dependency direction is:

```text
Bruin primitives + Renart operators/patches
                    │
 HybridBruinExecutor construction + shared planner
             /                         \
    read-only Plan                  actual Run
```

Practical approach:

1. extract the operator/renderer/materializer construction currently associated
   with `HybridBruinExecutor` and `direct_executor_registry.go` into a reusable
   Renart service;
2. preserve Renart's Python, Seed/Sling, API, Load, sensor, and executor patches
   in both planning and running;
3. beneath that seam, reuse Bruin's query extraction, Jinja, and materializer
   implementations and match `bruin render` behavior for SQL assets;
4. upstream a stable structured render API to Bruin if possible, since its
   existing `cmd/render.go` is CLI-layer code with global command concerns;
5. use a CLI fallback only when it is also the execution fallback, and mark
   fidelity/limitations explicitly;
6. add hooks, checks, and metadata stages from the same task graph used by the
   runner.

The renderer must not connect to the warehouse unless `Warehouse validation`
is explicitly enabled. Planning alone has no warehouse side effects.

### 6.5 Fingerprints and time-of-check safety

Name the identities precisely rather than overloading `fingerprint`:

- **Source Merkle root**: the pipeline version identity, computed with the same
  `CollectManifest` walk/exclusions used by Deploy. A deployed snapshot already
  has this root; a saved working-tree plan computes it without persisting a
  deployment.
- **Configuration digest**: a secret-free identity for the selected
  environment/configuration outside the snapshot, including `.bruin.yml`
  semantics that can affect execution.
- **Effective-variables digest**: a canonical digest of pipeline defaults plus
  environment, schedule, and run overrides.
- **Asset/DAG fingerprint**: the existing semantic identity used for staleness,
  which already hashes the values of variables each asset consumes.
- **Coverage variables hash**: the existing canonical full-variable-map hash
  that separates coverage contexts even when a particular asset consumes only
  a subset.
- **Target identity**: a secret-free canonical identity for the physical
  connection/object chosen by the same resolved operator that will execute it.
- **Latest physical output identity**: the last successful fingerprint,
  full-variable-map hash, and run that actually wrote that target.
- **Data-state token**: a digest/revision of the materialization facts and
  coverage used to resolve one `needed` preview.

One shared `ResolveEffectiveVariables` function must feed Jinja/type-check,
planning, staleness selection, execution, completion/recovery, and run/fact
recording. Reuse the existing asset-fingerprint and coverage canonicalization,
so enabling overrides alone requires plumbing rather than a fact-schema
migration and existing default-context facts remain valid.

The latest-physical-output rule is separate storage work and requires a
migration. Current facts/coverage have fingerprint and variables hash but no
target identity; `LatestFingerprint` omits the variables hash and reads raw
facts that retention later prunes, while the latest-attempt row may describe a
failure. The durable latest-successful-writer record must be keyed globally by
`target identity`, not by asset or environment: those fields describe the
writer, while two assets or environments that route to the same mutable object
compete for the same physical output. Facts and coverage remain scoped by
`(asset, environment, target identity)` so intervals from different
destinations cannot be merged or reused.

Target scoping alone is insufficient because coverage for variant A could
otherwise become reusable again after variant B overwrites the target and a
later selection returns to A. Give each accepted physical writer a monotonic
target generation. Facts and coverage store that generation, and freshness
consults only the generation on the current global writer row. A change to the
writer scope `(asset, environment)` or its `(asset fingerprint, full variables
hash)` advances the generation; the scope is part of the boundary because
coverage remains asset/environment-scoped. Returning to an older variant or
writer therefore starts a new generation instead of resurrecting its
historical coverage. Update the writer row, immutable fact, and
current-generation coverage atomically. Raw-fact retention must never remove
this writer row. Scheduled replay keys must compare their persisted target,
source, window, timestamp, and completion evidence, plus validate the stored
generation class, before any writer mutation; only an exact match is an
idempotent no-op.

Resolve and capture the exact target before execution from the same selected
source/configuration context as the operator, persist it for queued recovery,
and carry it on per-asset completion events. The recorder must not reconstruct
an executed target from a possibly changed `.bruin.yml` after completion.
Legacy facts migrate with an empty target and generation zero and remain
untrusted evidence until rebuilt; do not infer historical targets from current
configuration. Dynamic Python/runtime-only outputs remain unknown unless their
operator can report a reliable target.

Historical coverage is evidence, not automatically reusable freshness. For an
ordinary mutable target, freshness requires selected-context coverage and an
exact match with the latest writer's `(asset fingerprint, full variables
hash)`. An older variable or reverted-code variant must not become fresh after
another variant overwrote the table. Fail unknown/ambiguous legacy target or
writer identity stale until rebuilt. Multiple variants may coexist only when
their canonical target identities prove physical isolation or the
materialization contract proves additive coexistence. Downstream achieved
fingerprints must use this durable writer identity too, rather than losing the
physically present upstream when raw facts age out.

The client passively marks an open plan changed only when its pipeline source
root, configuration digest, or relevant data-state token changes. A global
workspace revision is too broad in an autosaving IDE. The panel shows a
`Source changed — refresh` banner instead of closing or repeatedly nagging; the
server-side check at confirmation is authoritative.

Run/Deploy submit the expected identities and plan inputs, not a client-owned
list of SQL statements. The server always executes from authoritative source,
never SQL sent back by the browser. If source/configuration changed, it returns
`409 plan_stale` with the available manifest/context diff.

`needed` also changes when data state changes without a source edit. At
confirmation the server re-resolves it:

- assets/windows that are no longer needed are omitted, execution continues,
  and run details record `N preview units no longer needed`;
- newly required or expanded/destructive work is not added silently; return
  `409 plan_data_changed` and ask for review;
- the final asset-by-window units and inclusion reasons are stored in run
  provenance.

Plans and rendered stages can be cached by the identities above, but the cache
is derived state. Deployment and scheduled-run correctness must not depend on
cache retention.

### 6.6 Diagnostics and incomplete authoring

Planning/rendering failures must be asset-scoped and partial:

- one incomplete asset should not erase successfully rendered siblings;
- save/autosave remains allowed;
- background checks are advisory;
- an explicit Run or Deploy can distinguish blockers from warnings;
- diagnostic positions should eventually map rendered SQL back to source;
- parser fallback for incomplete documents must preserve the current
  best-effort behavior rather than requiring a fully valid AST everywhere.

Initial deployment policy should be conservative about new blockers. Block
only deterministic parse/render/dependency/materialization errors that would
also make execution fail. Keep uncertain inference findings and warehouse
dry-run results as warnings until parity tests establish sufficient confidence.

### 6.7 Security, replay, and retention

Rendered SQL can contain sensitive literals. Therefore:

- never return connection credentials;
- redact known secret/config values on the backend before returning or storing
  a plan;
- store variable names, provenance, and a canonical digest by default, not
  resolved secrets in plan responses, SSE, or general run-list DTOs;
- persist the exact normalized values needed by a queued/recoverable `RunSpec`
  privately in local state when they are ordinary pipeline parameters. Persist
  references rather than resolved values for Bruin secrets, and require a
  durable resolvable reference for any secret-bearing override;
- do not claim a digest is replayable. Exact re-execution is available only
  when every value is still retained in the private `RunSpec` or can be
  re-resolved from a stable schedule/config/secret reference and the resulting
  digest still matches;
- when exact inputs are unavailable, prompt for them and verify the digest, or
  offer `Run again with current settings` as a distinct action;
- make actual SQL retention for run details an explicit local policy with a
  bounded retention period;
- preserve Bruin's credential masking behavior in logs;
- do not treat local-only operation as a reason to put secrets into SSE events
  or durable state.

## 7. Execution and schedule semantics to fix first

Before a new UI relies on the plan contract:

1. **Persist server-owned execution context.** Create the canonical run before
   dispatch and execute only its normalized `RunSpec`. The server derives
   invocation origin and watermark capability; a client cannot claim to be a
   schedule. River execution jobs carry the run ID rather than a second lossy
   behavior contract; due-occurrence signals retain only immutable scheduling
   identity.
2. **Make run admission atomic and observable.** Initially allow one active
   pipeline-scope mutating run per pipeline across environments in durable
   storage. Return `409 pipeline_run_active` with the active run ID for a race;
   give scheduled intervals a durable unique occurrence key and defer/retry
   them visibly instead of silently consuming them.
3. **Resolve an explicit source from environment policy.** Environments that
   permit working-tree execution default to the saved working tree.
   `deployed_only` resolves an exact deployment at plan time and blocks with a
   Deploy action if none exists. Schedule actions always resolve the row's
   exact pin. Merely creating a deployment never changes a normal environment's
   default.
4. **Migrate legacy pinless schedules before forbidding them.** Before scheduler
   reconciliation, atomically pin each legacy row to the then-latest deployment
   when one exists. If none exists, pause it in a visible `needs deployment`
   state for one-time review; do not silently deploy the checked-out branch.
   New or edited schedules may not persist an empty pin afterward.
5. **Validate, verify, and fail closed for deployments.** A pin must exist and
   belong to its pipeline. Verify manifest paths, blobs, and hashes at Deploy,
   pin assignment, and runtime. A missing/corrupt deployment fails rather than
   executing the working tree. Use a fresh per-run sandbox; never execute a
   shared writable cache.
6. **Apply complete context.** Effective variables, interval,
   backfill/full-refresh intent, protected-environment authorization, sensor
   policy, selection, source, and schedule identity survive enqueueing and
   restart. Recheck policy immediately before side effects. A confirmed
   backfill must not become an ordinary incremental run in the worker.
7. **Make variable-aware freshness truthful.** Use one effective-variable
   resolver in planning, execution, recording, and staleness. Add a canonical
   target identity and durable last-successful-writer contract; do not allow an
   older variable variant to report fresh after a later run has overwritten the
   same physical target. Coexistence requires explicit isolated/additive target
   semantics.
8. **Keep schedule watermarks server-owned.** `Run pinned` uses the displayed
   row's exact version but remains ad-hoc and never consumes a scheduled
   interval. Only a successful due/catch-up tick advances the
   `(pipeline UUID, environment)` watermark; re-execution does not inherit that
   capability.
9. **Reject invalid execution context.** Malformed JSON, timestamps, source
   IDs, variables, modes, and confirmations return structured errors rather
   than silently becoming omitted/default inputs.
10. **Make re-execution honest.** Exact re-execution requires the original
    immutable source and a resolvable matching context. Otherwise prompt for
    missing inputs or say `Run again with current settings`.
11. **Make scheduler ownership fail closed, then design coordination.** Expose
    owner state now and reject mutations on a follower that cannot deliver
    them. Specify heartbeat/fencing, handoff, durable revision delivery,
    cross-process reconciliation, and SSE behavior in the separate ownership
    workstream before implementing takeover.
12. **Give deployments stable human identity.** Persist per-pipeline ordinals,
    deterministically backfill existing snapshots, and use the same label in
    pins, plans, comparisons, and run provenance.
13. **Remove fixture controls.** Replace or remove the fake Impact plan and
    variant selector before exposing the real plan surface.

## 8. Suggested implementation phases

### Phase 0a: truthful operations and immediate safety

This slice works against the current execution paths and lands independently:

- make origin server-owned: HTTP/API/CLI requests cannot claim `schedule` or
  watermark capability;
- strictly parse every behavior-changing input;
- resolve source through environment policy and display the exact result at the
  action point, including a Deploy blocker for `deployed_only` with no
  deployment;
- make schedule-row `Run pinned` use its exact row and remain manual/no-
  watermark;
- migrate legacy pinless schedules, reject new empty/wrong-pipeline pins, and
  fail closed rather than falling back from a missing/corrupt deployment;
- thread context the existing queue can represent (interval,
  backfill/full-refresh, authorization, and sensor policy) through its current
  arguments as a temporary compatibility fix. Reject any mode that still
  cannot be preserved instead of silently downgrading it;
- do not partially enable variable overrides. Until Phase 0b carries them
  through execution, recovery, recording, and latest-writer freshness together,
  a schedule with stored overrides is visibly blocked/unsupported rather than
  silently run with defaults, and a non-default direct run must not record
  default-context freshness;
- preserve only genuinely exact re-execution; otherwise use the honest label;
- expose owner/follower/unavailable scheduler state and reject schedule
  mutations on a follower that cannot deliver them;
- remove fixture plan/variant behavior, fix labels and transport-error handling,
  retain one-click asset Materialize, and show source/window/environment at the
  action.

Keep the current scheduled job arguments and River `ByArgs` uniqueness during
this slice. These fixes reduce immediate surprise and security risk without
waiting for the execution-ledger or scheduler-coordination designs.

Implementation checkpoint (2026-07-16): the existing queued scheduler path now
persists normalized request context at admission and atomically replaces it
with the effective environment, window, full-refresh/backfill intent, and
sensor mode before the first asset starts. Execution fails closed if that write
does not complete. Default windows for pinned runs come from the pinned
pipeline, fractional timestamps remain exact, and validation-only dry-runs no
longer report or pass an execution window they do not consume. Legacy runs
whose effective context was never recorded are explicitly unresolved and are
acknowledged without replaying materialization facts; their rerun UI omits
request-only environment/window values and visibly uses current defaults. The
public run DTO exposes only the resolution bit for now, while ambiguous mode
fields remain private. A successful scheduled tick and its environment-scoped
watermark now commit in the same SQLite transaction, so a failed progress write
cannot leave a duplicate catch-up interval behind. This is still the Phase 0a
compatibility path: universal `RunSpec` adoption, effective variables and
selection provenance, authorization beyond the persisted protected-environment
confirmation, durable schedule occurrences, and non-scheduler run records
remain Phase 0b work. The scheduler-backed foundation recorded below has since
replaced the older admission contract.

### Phase 0b: universal run ledger and dispatch

Implementation checkpoint (2026-07-16): new scheduler-backed manual/API and
scheduled runs persist a private, validated v1 `RunSpec`. Manual/API admission
transactionally creates the run, spec, run-ID-only River job, run/job link, and
namespaced path plus stable-UUID slot aliases. The pipeline-global slot returns
a typed active-run conflict for manual races; scheduled compatibility signals
snooze with their exact interval while it is occupied. Stored specs override
job arguments; unknown versions, unknown fields, and row/spec mismatches fail
closed, while pre-upgrade jobs use one strict upgrade decoder. Startup relinks
runnable legacy jobs, fails terminal or jobless queued runs, and requeues a
claimed scheduled signal that has not admitted a run. Follow-up hardening binds
the private RunSpec UUID independently to the durable UUID slot and carries it
through scheduler execution into snapshot resolution, so a path rename cannot
retarget a queued deployment. A pre-slot database with duplicate active rows
now keeps one deterministic queued-first survivor and records every duplicate
as a failed, auditable recovery row instead of blocking startup.

Long-lived runtimes also now take an authoritative canonical-workspace lease
outside the worktree while retaining `.renart/server.lock` for compatibility.
Runtime database/discovery/lock files are added to `.git/info/exclude` without
editing tracked ignore rules. This closes the hole where worktree cleanup could
unlink the only lock while a server still owned the workspace; it remains
separate from the scheduler ownership/handoff workstream below.

This began as only the scheduler-backed foundation. Asset/scoped and
Build-needed execution plus exact re-execution remain open; effective variables,
full-pipeline inline execution, asset/window execution units, run-ID-only
scheduled dispatch, target/latest-writer identity, and durable schedule
occurrences/attempts have since landed in the checkpoints below.

Durable occurrence checkpoint (2026-07-18): actual due/catch-up intervals now
receive a stable server-derived occurrence key over pipeline UUID, environment,
and normalized half-open interval before planning. SQLite retains one
occurrence plus numbered run attempts. Occurrence claim, run, RunSpec, retained
plan/units, and pipeline slot are atomic; concurrent or later duplicate signals
reuse an active/successful occurrence, while failure/cancellation permits a
new numbered attempt. Slot contention rolls back the attempted run but leaves
the interval visibly pending in the schedules API/UI, refreshed by SSE. Run
terminalization and scheduled-success watermark advancement update the
occurrence in the same transaction. River `ByArgs` remains a defense in depth.

Run-ID-only scheduled dispatch checkpoint (2026-07-18): new periodic and
catch-up work uses a separate v2 due-signal kind containing the captured
schedule revision and normalized interval. Its worker plans and atomically
commits the occurrence attempt, run, RunSpec, retained plan/units, slot,
run-ID-only River execution job, and run/job link, then returns without physical
execution. The ordinary run worker reconstructs all behavior from the stored
RunSpec. Startup requeues a claimed signal unchanged; if admission committed,
the occurrence makes the duplicate signal a no-op while the linked execution
job is recovered normally. Pre-v2 combined jobs keep their strict legacy
decoder until they drain.

Inline full-pipeline checkpoint (2026-07-18): synchronous pipeline execution
now uses the same v1 RunSpec with `inline_streaming` dispatch. After resolving
policy, defaults, and the exact window, the execution service atomically admits
the run/spec and path plus available stable-UUID slots without a River job,
marks it running before Bruin, and retains streamed logs, targets, and steps before
detached terminalization. Existing SSE output remains inline. Discovery-token
requests receive server-authenticated `cli` provenance; ordinary HTTP calls are
`api`. Pipeline runs through direct HTTP, delegated/embedded CLI, legacy
`/api/run`, and onboarding therefore appear in canonical history and compete
with queued work. Startup fails interrupted jobless inline rows without
physical replay.

Inline asset-selection checkpoint (2026-07-18): RunSpec v2 adds a strict exact
selection representation for one-asset and upstream/downstream/neighborhood
materialization. It retains the anchor, scope, canonical workspace-relative
asset paths, ordered common-window units, and inclusion reasons. Admission,
the path plus available UUID slots, target snapshot, step/log persistence,
policy recheck, and detached finalization share the full-pipeline inline
lifecycle without a River job. Units transition independently around each
direct asset call, so sparse executor events cannot erase the selected work.
Build-needed remains open for the follow-up that maps each exact staleness gap
to one v2 unit.

This is the architectural prerequisite for the pipeline plan:

- introduce the versioned durable `RunSpec` and universal run records for every
  mutating path, including inline streaming;
- persist complete effective variables/context, asset-by-window execution
  units, source, authorization, modes, and provenance through recovery;
- add target identity plus durable latest-successful-writer storage/coverage
  semantics, and enable variable overrides only with that freshness contract;
- make run-slot admission atomic and return typed active-run conflicts;
- retain the shipped durable schedule-occurrence identity, transactional
  attempt/run/slot/job admission, and visible deferred/retry state;
- extend the common RunSpec/dispatch seam from shipped manual, periodic,
  catch-up, and inline full-pipeline work to asset/scoped and Build-needed paths;
- keep a versioned legacy worker/decoder until pre-upgrade River jobs have
  drained;
- verify snapshot integrity, execute each deployment in an isolated per-run
  sandbox, and make recovery use the same exact source/context;
- move exact re-execution and canonical run history onto this ledger, then
  remove Phase 0a's temporary duplicated queue fields.

Phase 1 rendering can proceed in parallel once Phase 0a establishes truthful
source/context semantics. Phase 2 requires both Phase 1 and Phase 0b.

### Workstream 0c: scheduler ownership and cross-process reconciliation

Phase 0a closes the immediate hole by exposing follower state and refusing
mutations it cannot deliver. Full coordination deserves a separate design
before implementation, covering:

- lease/heartbeat and fencing semantics;
- takeover, lock loss, stale-owner cleanup, and split-brain behavior;
- durable schedule revision/outbox representation;
- follower-to-owner delivery versus owner-only mutation;
- reconciliation after restart/ownership change and SSE publication;
- interaction with River periodic registration and due-occurrence admission.

This workstream does not block single-owner local planning/rendering. It does
block any claim that schedule mutations are reliable through an arbitrary
Renart process.

### Phase 1: shared asset render service

Implementation checkpoint (2026-07-16): a read-only backend service now exposes
`POST /api/assets/{assetID}/render` for a saved working-tree asset. It shares the
direct executor's materializer construction for every hook-capable direct SQL
family, including the string, ordered-list, and Athena location-aware forms and
their Rust `DECLARE` hoisters. It distinguishes requested, environment-level
operator, and per-asset effective full refresh, uses a server-owned asset path
and preview run ID, returns source/effective-variable identities plus value-free
variable provenance, and returns stage-level fidelity, issues, and redaction
metadata.

SQL assets expose the exact compiled query. Deterministic direct paths also
expose the final submitted execution SQL: string materializers retain their
hook-aware blob, Databricks/ClickHouse/Synapse retain atomic ordered elements,
and Athena retains its location-aware ordered elements. BigQuery and Snowflake
add secret-free semantic/runtime-only stages for their live conditional
preflights. Oracle exposes the exact query its no-materialization direct runtime
submits. Query sensors compile only `parameters.query` and show the exact
submitted condition while leaving polling behavior as runtime controls. The
same request-local, non-mutating hook-template resolver serves rendering and
direct execution. Ordered hooks become structured stages only when their
provenance survives hoisting byte-for-byte; otherwise exact elements are
reported neutrally without regex or SQL-text inference.

Non-SQL rendering is now semantic rather than fabricated: seeds describe the
Sling load and enforced casts, Load assets describe the Sling copy, API assets
separate HTTP extraction shape from the Sling JSONL write, table/S3 sensors
describe their condition and controls, and ingestr describes its copy. Python
is explicitly `runtime_only` because user code and SDK calls decide what runs.
Named connections are retained without resolved credentials; URLs, API auth,
and header values are redacted or omitted.

The Build editor exposes one generic read-only `Render` action for every
supported SQL, Python, seed, Load, API, ingestr, and sensor asset. It keeps the
save barrier and shows stages, provenance, fidelity, redactions, and
`Preview — not executed`. `renart render <asset>` uses the same result: it
delegates to an open workspace server when available and otherwise invokes the
read-only service directly without booting the scheduler or state database.

Review hardening keeps the content manifest as the sole canonical source
identity while avoiding a second full read of large pipeline files: render
captures a cheap size/mtime/inode guard before and after the one manifest hash
and after rendering. This is explicitly a non-adversarial TOCTOU guard, not a
replacement fingerprint; immutable snapshot rendering remains Phase 3 work.
The render/drift manifest collector streams file hashes without retaining
source blobs; only Deploy uses the content-retaining collector. Invalid
environment, execution-time, and window inputs now retain structured
sanitized `400` reasons, source-identity I/O failures return a sanitized `500`,
and concurrent source drift remains `409`. Save-time CRLF-to-LF normalization
no longer discards a successful preview, while genuine saved-source changes
still invalidate it.

Quality-check checkpoint (2026-07-17): the renderer now enumerates Bruin's
authoritative scheduler column/custom-check instances and runs the same
destination-specific direct-executor check operators against a capture-only
connection. This returns one named, structured `check` stage per runtime task,
including exact rendered SQL, column/custom identity, and blocking semantics,
without contacting a warehouse. Invalid checks remain asset-scoped error
stages and do not erase usable query/materialization stages; unsupported direct
check paths are explicit. The shared registry also restores ingestr's
destination-aware column/custom check delegates during direct execution. Build
and CLI output use distinct check labels, and the existing backend redactor
covers check SQL and diagnostics.

Destination-check parity checkpoint (2026-07-17): Oracle now uses Bruin's
Oracle column-check operator. Python, API, and Load resolve their effective
target connection and delegate column/custom checks to that destination's
actual SQL operator; targets without a mapped SQL check operator become typed
unsupported warnings without exposing connection names. API and Load retain
their custom HTTP/Sling path only for main tasks, while checks use the shared
sequential registry and metadata tasks are explicit no-ops. This also closes a
duplicate-execution gap where scheduler-created check or metadata tasks could
rerun the side-effecting main loader. Direct execution and render parity tests
cover dialect selection, captured SQL, unsupported targets, DuckDB
coordination, and the no-rerun boundary. The shared API/Load Auto-target
resolver also honors a lone configured default when no SQL/ingestr majority
exists, instead of silently selecting a synthesized DuckDB connection name.

PostgreSQL/Redshift parity checkpoint (2026-07-17): rendering and direct
execution now construct their hook-aware string materializer through one shared
factory. Parity tests compare the execution stage byte-for-byte with the SQL a
fake direct runtime connection receives for both destinations. Exact fidelity
is limited to those deterministic paths: developer-environment schema rewrites
that depend on live warehouse state and materializations with generated
temporary identifiers are explicitly `runtime_only`; schema preparation
remains semantic and materialization errors retain earlier usable stages.

MSSQL/Vertica/Fabric parity checkpoint (2026-07-17): rendering and direct
execution now share the hook-aware string-materializer factory and Rust
`DECLARE` hoister for MSSQL, Vertica, Fabric, and the legacy Fabric query alias.
End-to-end tests compare each saved-source execution stage byte-for-byte with
the SQL captured from direct `RunAsset`, including pre/post hooks. Schema
preparation appears only for the Fabric aliases because MSSQL and Vertica do
not perform that separate runtime step. MSSQL and Vertica `delete+insert` are
`runtime_only` because their materializers generate fresh temporary names;
Fabric's names remain exact because they are deterministic. MSSQL metadata-only
DDL also matches direct execution without requiring placeholder query text.

Selected-configuration checkpoint (2026-07-17): render provenance keeps the
existing `configuration_digest` field but now derives it from a shared
run-context canonicalizer over selected environment controls and only the
execution-relevant named connections. Bruin `sensitive` and `sensitive_file`
fields contribute presence without their values or file paths; custom
marshalers are never invoked. Connection ordering and unrelated environment
connections do not change the digest, while behavior-relevant public fields
do. Maps, interfaces, raw URL/DSN/endpoint/options strings, unresolved
connections, and unknown shapes fail closed to `runtime_only` with an empty
digest. Variable provenance exposes only sorted names and their winning source;
the current render path truthfully reports pipeline defaults because schedule
and ad-hoc overrides are not executable yet. This selected-configuration
identity is explicitly not the physical-target identity from section 6.5.

Direct-hook parity checkpoint (2026-07-17): every hook-capable direct SQL path
now constructs its materializer through one of the shared factories: the
string-returning families use the Rust `DECLARE` hoister, Databricks,
ClickHouse, and Synapse use the ordered-statement wrapper, and Athena uses the
location-aware ordered-statement wrapper. Hook wrapping remains outside the
configured/full-refresh selector so pre/post hooks survive both variants.
Runtime parity tests cover ordered pre/main/post submission and the shared
hoister. Synapse direct execution now uses Bruin's Synapse operator and
materializer instead of the MSSQL implementation.

MySQL/Trino/Oracle render checkpoint (2026-07-17): MySQL and Trino now share
their direct executor's hook-aware materializer and query-extraction paths with
the renderer, with byte-for-byte parity tests for deterministic cases. MySQL
schema creation remains a semantic preparation stage, while materializations
that generate temporary names (`delete+insert`, `merge`, and both SCD2
strategies) are honestly `runtime_only`. Trino preserves its split-statement
runtime semantics, including complete time-interval batches and metadata-only
DDL derived from columns; only developer schema-prefix rewrites that require
live warehouse state remain `runtime_only`. Oracle's direct path supports only
no-materialization queries, so that submitted query is rendered exactly;
declared materialization produces the matching execution error, and declared
pre/post hooks produce a partial warning because the direct Oracle runtime does
not execute them.

Canonical asset/target identity checkpoint (2026-07-17): render now reuses the
staleness fingerprint engine to return the selected asset's full DAG
fingerprint and the existing full-variable-map coverage hash. A missing legacy
pipeline ID is represented only on a shallow in-memory copy, so the read-only
path never self-assigns or persists an ID; fingerprint failures retain usable
stages as sanitized partial results. The same identity fields are visible in
Build and `renart render`.

Render also exposes a secret-free physical-target descriptor resolved without
opening a warehouse. Exact identities exclude connection aliases,
environment/principal names, and credentials while including only proven
endpoint/routing coordinates plus the resolved relation or canonical local
file. Bruin's table-name capabilities and Renart's DuckDB path canonicalizer
are the shared parsing/normalization seams. Ambiguous session defaults,
schema-prefix rewrites, pre-hooks with unqualified targets, raw routing
options, credential-derived tenancy, non-materialized/dynamic Python outputs,
non-materialized SQL, and unsupported families fail closed to `runtime_only`;
declared Python tables can use an exact intended target only when completion
coverage requires the operator's durable write evidence. Sensors report an
exact no-output target. Response display values never expose warehouse hosts,
database paths, or credentials. This is the read-only resolution seam needed
by target capture and latest-writer persistence; the execution-evidence and
target-aware state checkpoints below now use it without widening its
credential-free contract.

Remaining-direct-SQL render checkpoint (2026-07-17): BigQuery and Snowflake
now share their exact hook-aware string materializers with rendering. BigQuery
applies the same annotation comment before submission; its live cost guard,
dataset preparation, and target-compatibility work, plus Snowflake's warehouse
selection, container preparation, target compatibility, and SCD2 migration,
are represented at semantic or runtime-only fidelity without opening a
warehouse or exposing connection values. Operator-level and asset-effective
full refresh remain distinct so environment and asset restrictions match
direct execution.

Databricks, ClickHouse, and Synapse now render the complete ordered batches
submitted by their direct operators. The renderer preserves batch elements and
uses structured pre/main/post kinds only when the final list matches the
unhoisted origin byte-for-byte; a successful `DECLARE` reorder falls back to
neutral execution stages instead of guessing provenance. Databricks three-part
targets also expose the runtime's uppercase catalog-then-schema preparation.
Athena uses the same
location-aware materializer and selected typed query-results path as direct
execution, exposes neutral ordered stages, supports metadata-only DDL, and
matches per-asset hook refresh context. Time-interval post-extraction and
generated temporary-name fidelity are covered by direct parity tests.

Post-task/operator closure checkpoint (2026-07-17): rendering now asks Bruin's
scheduler graph whether metadata push exists and appends it after quality checks
from the same structural result finalizer. PostgreSQL-compatible, BigQuery, and
Snowflake metadata stages use the exact explicit asset-type mapping installed by
the direct executor registry; mutations remain secret-free `runtime_only`, and
backend no-op/error behavior is represented semantically. BigQuery query/table
sensors now show the configured live dry-run cost guard before their condition.
PostgreSQL/Redshift and MySQL string-SCD2 assets show the direct operator's live
timestamp-column migration before execution SQL, with the same full-refresh
gate. Focused parity tests cover task order, executor mapping, sensor limits,
migration ordering, and credential redaction.

Target-generation storage checkpoint (2026-07-17): materialization facts and
coverage now carry secret-free target identity and generation, while a global
latest-successful-writer table survives raw-fact pruning. One transaction
orders a completion, advances or reuses its generation, writes the immutable
fact, updates only current-generation coverage, and moves the writer. Writer
scope changes advance the generation as well as fingerprint/full-variable
changes, preventing cross-asset or cross-environment A -> B -> A resurrection.
Stable completion IDs/ordinals handle same-run ordering; independent equal-time
writes become explicitly ambiguous, and non-exact fact-key replays fail before
writer mutation. Legacy targetless rows remain generation zero. The schema is
now active across pre-execution capture, recovery transport, recorder, and
staleness selection.

Execution-evidence checkpoint (2026-07-17): immediately before execution the
direct runner captures a version-two, secret-free snapshot of the full parsed
graph, including stable identity, exact/runtime-only target fidelity,
fingerprints, authored upstream edges, coverage mode, variable hash, and refresh
restriction. Scheduler runs persist it before their first step; all completion-
aware interactive paths carry it directly. At each main-task start Renart
captures the visible latest writers for exact in-pipeline upstream targets and
claims ordinary outputs before physical work. Evidence-required Python tables
instead claim immediately before their Go-side load; the recorder grants
coverage only when that durable claim (or an already committed matching fact)
exists. Failed/cancelled claims become dirty, successful facts clear matching
claims in the same writer/fact/coverage transaction, and active/dirty claims
suppress the prior writer. Recovery uses self-contained v2 evidence; v1 replay
fails closed where a successful consumer lacks a successful in-pipeline
upstream.

Completion/recovery checkpoint (2026-07-17): a durable SQLite outbox is the
single post-execution hand-off for recorder and staleness subscribers. Enqueue
failure still fails the request, while a later subscriber failure leaves
retryable derived-state work without misreporting the finished physical run.
Every non-dry mutating service path holds a shared per-workspace OS lease through
that hand-off; startup takes it exclusively before marking orphaned active
claims dirty, and headless invocations skip reconciliation behind a live
executor. Legacy workspace runs and quickstart materialization now route through
the same completion-aware service, and Build-stale holds the lease across its
whole ordered plan. Cancelled execution remains cancelled through the direct
event, service result, and scheduler status.

Target-aware state checkpoint (2026-07-17): staleness now selects only current-
generation coverage for the resolved exact target, exposes per-asset target
fidelity/identity and `latest_output`, and publishes a deterministic
`data_state_token` through HTTP and SSE. Equivalent rerun metadata does not churn
the token; generation, coverage, ambiguity, or active/dirty claim changes do.
Target-claim transitions trigger an asynchronous fail-closed staleness snapshot
even when no completion can be produced.

Phase 1 is complete. The shared saved-source asset renderer covers the direct
task graph at exact, semantic, runtime-only, or unsupported fidelity; target
capture and generation-aware data state now provide the source/config/data
identity seam required by Phase 2. Safe selected-configuration coverage can
continue to expand only when an opaque connection field gains an explicit
secret-free schema, and live conditional parity tests remain an ongoing
hardening concern rather than a Phase 1 blocker.

### Phase 2: pipeline execution plan

Implementation checkpoint (2026-07-18): `POST /api/pipelines/{id}/plan` and
`renart plan` share one read-only planner for saved working-tree and exact
deployment sources. It resolves `all`, `needed`, and asset-closure selection,
combines target-aware staleness gaps, topological execution units, code checks,
render stages, policy/active-run blockers, and identity-bound context, and
omits only large stage content until requested. The plan ID binds source,
selected configuration, variables, data state, context, selection, and the
operation graph.

`POST /api/pipelines/{id}/plan/confirm` regenerates the server-owned plan at its
reviewed execution timestamp and admits all three selection modes. It never
trusts browser-supplied rendered content. Source/configuration/operation changes
return the replacement plan as `plan_stale`; Needed plans may shrink only by
omitting exact units that became fresh, while new, widened, or changed work
returns `plan_data_changed`. Destructive environment confirmation is checked
before admission and again before side effects.

Accepted runs atomically persist the redacted plan artifact, selection and
data-state provenance, final ordered asset/window units, inclusion reasons,
RunSpec, run/job link, River job, and pipeline run slot. The worker executes
only those units through the shared hybrid executor, preserves repeated windows
in order, gives every unit a replay-safe completion identity, and durably tracks
queued/running/terminal unit state. Working-tree execution uses a verified
isolated copy; snapshot execution verifies its expected Merkle; selected
configuration is rechecked before the first unit.

The Build toolbar has one Definition/Data Readiness control and a primary
Review-run sheet with Summary, Assets, Checks, and lazy Execution content.
Needed plans run directly from that review. Canonical run details add a live
Plan tab with final units, safe preview omissions, identities, reasons, windows,
and redacted stage metadata; `run.unit` SSE updates it independently from legacy
step consumers. Planning stays asset-scoped for incomplete authoring: malformed
asset YAML is a blocker without erasing renderable siblings, incomplete SQL
retains sibling previews, and Python remains explicitly runtime-only where a
safe static claim is unavailable. Backend coverage includes source modes,
selection/closure, data-state shrink and expansion, policy, repeated windows,
durable unit/completion behavior, and partial sources; live coverage exercises
Needed execution/history, no-deployment `deployed_only`, incomplete source,
destructive confirmation, and confirmed Python-table freshness on desktop and
mobile where applicable.

Readiness-diagnostic follow-up (2026-07-18): the reserved Load `local` endpoint
is excluded from named-connection identity while its real source connection and
exact file target remain bound. Conditional semantic schema preparation stays
visible in render stages without making an otherwise exact plan partial. The
planner now emits its generic partial warning only for an actual failed,
unsupported, runtime-only, or unresolved-target stage; this removes repeated
non-actionable SQL warnings while retaining Python's honest runtime warning.

Phase 2 is complete. The remaining universal-ledger gap is broader than this
phase: Build-needed still uses the common completion seam without a RunSpec;
direct one-asset/scoped, full-pipeline onboarding, HTTP, and embedded execution
now use the inline ledger. Durable schedule occurrences/attempts, variables,
deploy/schedule plan integration, and separate signal-to-execution jobs are
implemented; exact gap-window selection continues in Phase 0b.

### Phase 3: Deploy and schedule integration

- reuse planning for saved-workspace deployment review;
- add working-tree/deployment/pinned-version render and file diffs;
- add/backfill stable deployment ordinals;
- make schedule source selection explicit;
- allow explicit multi-schedule promotion after deployment;
- generate and retain a redacted plan artifact for each scheduled run using its
  actual interval and variables.

Implementation checkpoint (2026-07-18): deployments now have deterministic,
per-pipeline ordinals while retaining UUIDs as their immutable execution
identity. Build, schedules, plans, runs, and CLI output use the shared human
label; identical deploys retain the existing ordinal. The web Deploy action and
schedule repair/create paths now open an explicit saved-working-tree review.
The planner's `deployment` purpose renders the entire pipeline but deliberately
does not inherit execution-only freshness, active-run, protected-environment,
or `deployed_only` gates, and run confirmation rejects that purpose.

The review shows exact added/changed/removed paths plus source-bound deployed
and saved text previews, with binary and files over 2 MiB withheld. Deployment
submits the reviewed Merkle root and returns a typed conflict without writing a
snapshot if saved source changed. Existing schedule pins never move as a side
effect: after deployment the user selects an initially unchecked subset, and
the server validates the target then compare-and-swaps the complete selection
in one transaction. Schedule creation likewise reviews the workspace and pins
the exact returned version; production UI no longer relies on `deploy_now`.

Scheduled-run integration checkpoint (2026-07-18): schedule overrides now
validate against the exact pinned deployment on write, resume, promotion, and
reconciliation. One strict Bruin pipeline mutator normalizes and applies those
values before asset construction in planning, rendering, and execution, so the
variables digest, fingerprints, targets, and recorder evidence share the same
effective context. Raw values remain private schedule/RunSpec inputs; public
schedule responses, plan provenance, and retained artifacts expose only sorted
names and digests.

Each actual due/catch-up signal plans the immutable pin at its normalized
interval and admission timestamp with scheduled sensor semantics, then
atomically persists the run, private RunSpec, redacted artifact, ordered units,
and pipeline slot. A blocked plan becomes a failed auditable run without
physical execution, and its durable blocked decision survives a crash before
failure finalization. Row-level `Run pinned` now loads the pin and overrides on
the server while retaining manual/no-watermark provenance. Phase 3 is complete;
the Build-needed ledger plus exact re-execution remain in the earlier Phase 0b
workstream. Durable occurrence identity, numbered attempts,
run-ID-only scheduled dispatch, and inline full-pipeline history are covered by
the later Phase 0b checkpoints above.

### Phase 4: optional policy and automation

- configurable deployment gates for deterministic errors and acknowledged
  warnings;
- optional warehouse validation per environment;
- opt-in `run when needed` automation alongside cron, with explainable
  staleness reasons;
- resource/destination-aware concurrency that can safely relax the initial
  pipeline-global run slot;
- change categorization/impact analysis only when Renart can infer it reliably;
- consider immutable compiled artifacts or isolated environments only if a
  concrete need justifies Option C.

## 9. Validation requirements

### Backend parity

- golden tests for compiled query and execution output across every supported
  SQL warehouse family/materialization mode;
- parity between plan statements and the statements handed to the runtime,
  including full-refresh restrictions, interval modifiers, hooks, checks, and
  query sensors;
- exact working-tree versus snapshot source tests;
- normal versus `deployed_only` default-source tests, including no-deployment
  blocking;
- schedule variables affecting SQL, Python, and API execution; generic runs do
  not inherit them; overrides validate against snapshot-specific declarations;
- unknown/type-invalid overrides, plan/executor/recorder digest parity,
  effective-variable fingerprints/coverage, and physical-target replacement
  across two variable variants;
- reverted code/variables remain stale after a newer writer; distinct target
  identities retain independent coverage; failed/cancelled or out-of-order
  recovery never replaces the latest successful writer;
- raw-fact pruning preserves durable physical identity, ambiguous legacy
  targets fail stale until rebuilt, and dynamic/no-target assets use their
  explicit fallback contract;
- queued/recovered runs retain their original effective context after schedule
  edits, and use the actual scheduled interval;
- complete backfill/full-refresh/confirmation context survives queue/restart;
- pin ownership/not-found/fail-closed tests;
- legacy pinless migration with and without an existing deployment;
- deployment manifest/blob verification and isolated-run-sandbox tests;
- client trigger spoofing rejection and server-owned watermark tests;
- manual pinned/re-executed runs do not advance the schedule watermark;
- atomic concurrent run-admission conflict and scheduled retry tests;
- the same periodic/catch-up occurrence admitted concurrently creates one run;
  a completed occurrence does not re-execute, while retry stays under its
  occurrence identity;
- transactional rollback leaves neither run nor River job; available/claimed
  jobs survive recovery correctly; legacy scheduled arguments still decode
  across the upgrade;
- a manual dev run blocking a production occurrence leaves it visibly deferred
  with its original interval, then executes it exactly once;
- scheduler owner loss, takeover, and cross-process reconciliation tests;
- malformed body/time/source/variables/mode rejection tests;
- selection/topological/gap tests that reuse current staleness fixtures;
- Needed confirmation tests for data-state shrink versus expansion/blockers;
- execution-unit window and inclusion-reason persistence tests;
- partial-plan tests for incomplete SQL/Python/YAML;
- non-SQL semantic/runtime-only tests;
- secret-redaction and plan-retention tests;
- source/config stale-plan rejection tests;
- deterministic deployment-ordinal backfill and no-op deploy identity tests.

Avoid assertions based on regex-splitting rendered SQL. Compare structured
operations or exact canonical blobs from the renderer.

### Frontend and live behavior

- E2E: edit/save -> readiness becomes stale -> plan regenerates through SSE;
- E2E: plan Needed -> exact assets/gaps -> run -> run details -> freshness;
- E2E: working-tree Run remains working-tree after a deployment exists;
- E2E: a `deployed_only` Run displays/uses its exact deployment or blocks with
  Deploy;
- E2E: schedule-row Run executes its displayed pin;
- E2E: one-click asset Materialize and its stale-upstream prompt remain intact;
- E2E: occupied run slot disables/links before confirmation and a race returns
  the typed active-run result;
- E2E: scheduler ownership/handoff state updates through SSE;
- E2E: deploy -> choose schedules -> only selected pins advance;
- E2E: working-tree versus pinned rendered SQL diff;
- E2E: incomplete source remains editable while Run shows a blocker;
- E2E: protected-environment and destructive-operation confirmation;
- accessibility and shrink-safe rendering for large plans/long SQL.

## 10. Product decisions still needed

1. Final UI term: `Build needed`, `Run needed`, or `Update data`. `Build stale`
   is technically accurate but less approachable; `Build changes` is
   inaccurate for missing/partial/volatile work.
2. Whether deterministic errors block Deploy immediately or begin as an
   advisory policy until render/type-check parity is proven.
3. Whether redacted rendered SQL is retained by default in run history and for
   how long.
4. Whether environment schedules remain operational local state only, or later
   gain a version-controlled declaration. In either case, keep Bruin's
   `pipeline.yml` cadence interoperable and clearly distinct.
5. Whether `run when needed` belongs in the initial implementation.
   Recommendation: no; first make manual plans and cron execution truthful and
   observable.
6. The local retention/garbage-collection limits for per-run sandboxes and any
   optional immutable verified snapshot cache. Correctness must not depend on
   that cache.
7. Which operators can report stable write-resource/target identities strongly
   enough to allow concurrency or reusable isolated coverage. Environment names
   alone are not sufficient evidence.

## 11. Completion

When this work ships, fold the as-built source/run/deploy/schedule contract into
`architecture/backend.md`, the plan/readiness/render UI into
`architecture/frontend.md`, and selection/freshness behavior into
`architecture/staleness.md`. Delete this plan afterward; Git history retains
the proposal and deviations.
