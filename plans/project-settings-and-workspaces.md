# Project settings & multi-project workspaces

Status: proposed. Two intertwined pieces: (1) replace the mocked project
settings (environments, connections) in the redesign UI with the real thing;
(2) make renart treat directories as **projects** in the IDE sense, switchable
from the top-nav dropdown that today statically says `data_platform`. The
cloud platform is kept in mind throughout but deliberately not designed here.

## 1. What exists today

More than the mock suggests — the gap is mostly frontend:

- **Backend config CRUD is real and complete.** `service.ConfigService` reads
  and writes `.bruin.yml` (node-preserving via
  `LoadOrCreateWithoutPathAbsolutization`); `/api/config` returns
  environments, connections, the default environment, **and reflection-derived
  field definitions per connection type** (`connection_types`), which is
  exactly what a schema-driven form needs. Create/update/delete/clone for
  environments, create/update/delete/**test** for connections all exist
  (`httpapi/config.go`).
- **The old (pre-redesign) UI already uses it**: `workspace-environment-pane`,
  `workspace-connection-form(-fields)` render forms from `connection_types`
  and call the test endpoint. This machinery is portable.
- **Renart-side environment policy lives elsewhere**: protection flags in
  `.renart/environments.yml` (`protected`, `deployed_only`,
  `confirm_destructive` — see `architecture/staleness.md` §7), currently
  hand-edited, no API.
- **The redesign settings pages are static mocks** (`settings-pages.tsx`), and
  the project dropdown in `redesign-shell.tsx` is hardcoded.
- **One server process = one workspace root** (positional argument). The
  workspace has no identity of its own; pipelines have UUIDs, the project
  doesn't.

## 2. Terminology

A **project** is a workspace directory: the root that holds `.bruin.yml`,
pipelines, notebooks, and `.renart/` state. One project = one git repo in the
common case. "Project" is the user-facing word (top-nav dropdown, settings);
"workspace root" remains the internal name for the path. The mocked
"Account/Workspaces" page (cloud workspaces) is a different, cloud-era concept
and stays mocked.

## 3. Project settings redo (Phase 1 — mostly frontend)

Wire the redesign settings pages to the existing API and add the one missing
backend surface (policy):

- **General**: project display name + project id (see §4.1), default
  environment (already in `/api/config`), workspace path (read-only).
- **Environments**: list from `/api/config`; create/clone/delete/rename via
  the existing endpoints; per-environment `schema_prefix`. **Merge the two
  config surfaces visually, not physically**: the environment detail view
  shows connections (from `.bruin.yml`) *and* the renart policy toggles
  (protected / deployed-only / confirm-destructive, plus `notebook_target`
  when notebooks gain warehouse targets) — written to
  `.renart/environments.yml` through new endpoints
  (`GET/PUT /api/config/environment-policies/{env}`) backed by
  `policy.Loader`'s file. Keeping the files separate preserves the invariant
  that Bruin's own config parsing is never at risk.
- **Connections**: port the schema-driven form + test-connection flow from the
  old UI components into the redesign look; group by environment; show the
  `sling_category` badge where relevant. Delete the old `_workspace` settings
  panes once parity lands (they're the last consumers).
- **Protected-environment affordance**: environments with `protected: true`
  render with the distinct treatment the staleness design promised (red env
  chip), here and in the env selector.
- **Secrets caveat** (record, don't solve): `/api/config` returns connection
  values raw. Fine for a loopback origin-guarded server whose user owns the
  files; for the cloud this becomes a secret-store integration — keep
  `ConfigService`'s request/response shapes stable so a cloud resolver can
  back the same fields without a second vocabulary.

This phase is a quick win: no new architecture, real forms replace the mock,
and one new small API for policy.

## 4. Multi-project (Phase 2)

### 4.1 Project identity

Same pattern as pipeline identity: self-assign `.renart/project.yml` with
`id: <uuid>` and optional `name:` on first open (directory basename as the
default name). The UUID is what the registry, per-project UI state, and —
later — cloud workspace mapping key on; it survives moves and renames.

### 4.2 Project registry

A **global** (per-user, outside any workspace) registry:
`~/.config/renart/projects.json` (XDG paths; `%AppData%` on Windows):

```json
{ "projects": [
    { "id": "…", "name": "data_platform", "path": "/home/…/data-platform",
      "type": "local", "lastOpenedAt": "…" }
] }
```

Appended whenever a server opens a workspace; entries with dead paths render
greyed-out with a remove action. `type` exists so cloud projects can join the
same list later without a format change.

### 4.3 Server architecture: one server, many project runtimes

Options considered:

- **(A) One server process per project + supervisor** — simple isolation, but
  port sprawl, N schedulers, and the dropdown becomes a cross-origin
  navigation. Rejected.
- **(C) Single active project, switch in-process** (teardown + re-init of
  coordinator/watcher/scheduler) — cheapest, but the switch is *server-global*:
  two browser tabs on different projects fight each other. That breaks the IDE
  mental model the feature exists for. Rejected.
- **(B) Multi-project server** — one process, a `ProjectRuntime` per open
  project, project id in the URL. **Recommended.**

`ProjectRuntime` is a container for exactly what `cmd/server.go` wires per
root today: workspace coordinator + watcher, the service graph, scheduler
store/service on that project's `.renart/state.db`, policy loader, SSE hub
(or a project-tagged shared hub). A `ProjectManager` holds
`map[projectID]*ProjectRuntime` with lazy open and idle eviction (close
watcher/scheduler after N hours unused, keep registry entry).

Routing: mount the existing API under `/api/projects/{projectID}/…`. During
migration, the unprefixed `/api/*` routes alias to the **default project**
(the argv root), so the current frontend and e2e suite keep working while
pages move over. The SSE endpoint becomes per-project
(`/api/projects/{id}/events`).

Frontend: the route tree gains a project segment (`/p/{projectID}/…`); each
tab pins its project. The top-nav dropdown lists the registry (open runtimes
first), switching = navigation, not server mutation. "New project" reuses the
onboarding/scaffold flow with a directory picker; "Open project" needs a
small server-side directory-browse endpoint (loopback + origin-guarded, same
trust model as everything else).

### 4.4 Scheduling semantics across projects

Each project keeps its own `.renart/state.db` — projects stay self-contained
and portable. Schedules tick only while their project's runtime is open in
some server; idle eviction pauses them (catch-up policies already handle the
gap on reopen, same as laptop-closed-overnight). Always-on scheduling is
explicitly the cloud platform's job; don't build a local daemon for it.

### 4.5 Interaction with the CLI (see `plans/cli-v1.md`)

`server.json` discovery becomes project-aware: the server records the roots
it has open; `renart run` in workspace X delegates only if the server has X
open (or asks it to open X — a one-line call once `ProjectManager` exists).

## 5. Cloud platform — kept in mind, not built

- Stable project UUIDs (§4.1) are the join key for "connect this local
  project to a cloud workspace".
- The registry's `type: local | cloud` field reserves the shape.
- Config/connection API shapes stay backend-agnostic so a secret store can
  replace raw values (§3).
- The Account section (profile/members/billing) and "Connect cloud workspace"
  stay as mocks; per-environment credentials-decrypt-only-for-scheduler is
  the cloud enforcement of the local policy flags (`architecture/staleness.md`
  §7).

## 6. Phasing & effort

1. **Settings for real** (§3): wire + port forms, policy endpoints, delete old
   panes. Small backend, medium frontend. Independently shippable.
2. **Project identity + registry + ProjectRuntime + routed switching** (§4):
   the big one — mostly a disciplined refactor of `cmd/server.go` wiring into
   a constructor, then routing/frontend. Ship behind the default-project alias
   so it can land incrementally.
3. **Niceties**: idle eviction tuning, per-project recent state, project
   scaffolding polish, CLI project-awareness.

## 7. Risks / open questions

- **Resource footprint per open runtime**: each project costs a watcher, a
  SQLite pool, staleness cache, and (shared) wasm engines. The wasm engines
  are process-wide singletons already — keep them shared; idle eviction caps
  the rest.
- **api-types generator churn**: new DTOs (registry, policies) must flow
  through `web/scripts/generate-api-types.mjs`.
- **e2e**: the live suite assumes unprefixed routes; the alias keeps it green
  until specs migrate.
- **Directory-browse endpoint** is the only genuinely new security surface;
  it must stay loopback-only and origin-guarded like the rest.
- Open: should switching projects be allowed to *open arbitrary paths* the
  server was never started on? Proposed: yes, that's the point of the
  registry — but only paths the OS user can read, and only via the guarded
  endpoints.
