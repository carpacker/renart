# Renart CLI v1 — a clean surface + run pipelines from a real terminal

Status: proposed (revised 2026-07-11 after maintainer decisions: bare
`renart` prints polished help; debug commands move under a hidden
`renart debug` group; the web-UI terminal view is **out of scope** for now —
the earlier PTY design lives in this file's git history; `renart init` is in).

Goal: make `renart` a first-class CLI — a clean, self-explanatory command
surface plus the verb users actually want, `renart run`, usable in a shell or
CI **including while a renart web server is running against the same
workspace**.

## 0. The surface contract (what "clean" means)

Today `renart --help` says "renart - Standalone Renart server" and lists
debug plumbing (`fp`, `sql-lsp`) beside user commands; `type-check`, `deploy`
and `fp` take a `<pipeline directory>` argument while the future `run` wants
asset/pipeline names; `deploy` needs `--workspace`/`--scheduler-state` flags
the others don't have. The contract that fixes this:

- **Root help.** Name `renart`, one-liner: *"Renart — the data pipeline IDE.
  Build, run, and schedule pipelines from one binary."* Bare `renart` prints
  this help (script-safe; the docs keep advertising `renart web`).
- **Visible commands are only what a pipeline author needs**, categorized:
  - *Start*: `web` (the IDE server), `standalone` (desktop app)
  - *Pipeline*: `run`, `ls`, `type-check`, `deploy`
  - *Project*: `init`
- **Everything else hides under `renart debug`** (the group itself is
  `Hidden: true`): `debug fp`, `debug sql-lsp`, `debug warm-cache`. The
  top-level `fp`/`sql-lsp`/`warm-cache` names are removed — pre-release, no
  compat burden. If the stdio LSP ever becomes an advertised editor
  integration, `sql-lsp` gets re-promoted to a visible top-level command;
  until then it's plumbing.
- **One target grammar everywhere.** Pipeline-scoped commands accept a
  pipeline name, an asset name, or a path — never *require* a directory
  argument. The workspace is resolved by walk-up (§2.2), so
  `cd pipelines/marts && renart type-check` just works. `deploy` loses its
  bespoke `--scheduler-state` flag (derived from the resolved workspace).
- **Consistent flags & exit codes.** `--json`, `--env`, `--workspace`,
  `--quiet` mean the same thing on every command. Exit codes: 0 success,
  1 run/validation failure, 2 usage error. `ArgsUsage` and an example on
  every command's help.

This section alone (rename + categories + debug group + help polish) is
shippable before any of the run machinery below.

## 1. Starting point

The binary already is a CLI (`urfave/cli/v3`): `web`, `standalone`, `fp`,
`deploy`, `type-check`, `sql-lsp`, plus hidden `warm-cache`. Missing:
`run`, `ls`, `init`, and a coherent story for coexisting with a running
server.

Two facts drive the whole design:

- **DuckDB is single-writer per file, cross-process.** In-process, access is
  serialized with a per-path map mutex; across processes there is no
  coordination — a second process hits DuckDB's file lock and fails
  ("database is busy", already mapped to a friendly message in
  `execution.go`). A CLI process materializing into the same DuckDB file the
  server is writing **cannot be made safe**, only avoided.
- **SQLite is already multi-process-safe here.** `.renart/state.db` runs WAL +
  `busy_timeout=5000`, transactions are short, and `renart deploy` already
  writes snapshots into it from a separate process today. The only hard
  constraint: **never run two River schedulers** against the same DB
  (duplicate scheduled runs); the CLI must never start the scheduler service.

## 2. Core design: delegate when a server is running, embed otherwise

```
renart run marts.orders
   │
   ├─ discover server for this workspace (.renart/server.json + health check)
   │
   ├─ server alive ──► CLIENT MODE: call the server's HTTP API,
   │                   stream output. One process owns DuckDB/SQLite writes;
   │                   staleness, SSE, run history update for free.
   │
   └─ no server ────► EMBEDDED MODE: run in-process through the same
                       service layer the server uses (ExecutionService,
                       direct executor, policy.Check, matlog recorder).
                       No watcher, no SSE hub, no River scheduler.
```

Same pattern as docker/dagger: the daemon owns shared mutable state whenever
it exists; the CLI is a thin client. It solves every concurrency question at
once instead of sprinkling locks:

- DuckDB: all writes flow through one process → the in-process mutex is
  sufficient again.
- SQLite: facts/coverage/run records written by one process while a server
  runs; embedded mode only writes when no server exists.
- Staleness: client-mode runs go through the server's bus, so badges and SSE
  update live. Embedded runs happen when no server is watching.
- Policy: `policy.Check` sits in the shared execution dispatch, so protected
  environments behave identically in UI, client mode, and embedded mode.

### 2.1 Server discovery (project-aware)

The server is multi-project (`architecture/backend.md` §2: one process, a
`ProjectRuntime` per open project mounted at `/api/projects/{id}/*`), so
discovery is per **project root**, not per process:

- When a server opens a project, it writes `.renart/server.json` in that
  project's root: `{pid, host, port, project_id, version, started_at,
  token}`; removed when the runtime closes or the server shuts down. A stale
  file (dead PID / failed health check) is ignored and overwritten.
- New `GET /api/health` returns `{version}`; the CLI confirms the project is
  actually open via `GET /api/projects` (paths are in the registry payload)
  and — if the workspace is registered but not open — asks
  `POST /api/projects/open` to mount it, then talks to
  `/api/projects/{project_id}/*`.
- `RENART_SERVER=<url>` (+ `RENART_TOKEN`) overrides discovery — the seam a
  future embedded terminal or the cloud plugs into.
- `--local` / `RENART_NO_SERVER=1` forces embedded mode. If a live server is
  detected, `--local` **warns** (DuckDB conflicts possible, server staleness
  won't update) and, on conflict, surfaces the busy error with a retry hint
  rather than retry-looping.
- Version skew: warn when CLI and server versions differ (normally it's the
  same binary).

### 2.2 Workspace resolution

Like git: walk up from cwd to the nearest directory containing the project
config (`.bruin.yml`; fallback: a `pipeline.yml` ancestor). Explicit
`--workspace` overrides. This is what lets every command drop its
`<pipeline directory>` argument.

### 2.3 Client mode transport

Reuse the existing streaming endpoints —
`POST /api/pipelines/{id}/materialize/stream` and
`POST /api/assets/{assetID}/materialize/stream` — plus `GET /api/workspace`
for target resolution, all under the project mount. Put the HTTP client in a
small `internal/clientapi` package shared by all CLI commands (and later by
anything cloud-shaped). Requests carry the token from `server.json`; the
server accepts it as an alternative to the Origin check (CLI requests have no
Origin header — today they pass because non-browser requests without Origin
are allowed; the token makes this explicit and future-proof).

### 2.4 Embedded mode

Construct the same dependency graph `cmd/server.go`/`cmd/projects.go` build,
minus watcher, SSE hub, HTTP, and the River scheduler *service* (the store is
still opened to write run facts/snapshots — same as `renart deploy` today).
One initial workspace parse instead of a watcher. `policy.Check` runs as in
the server; `confirm_destructive` prompts on TTY. Facts + coverage land in
`.renart/state.db` so the next server start sees correct staleness.

## 3. Command surface (v1)

| Command | Behavior |
| --- | --- |
| `renart web` | exists — the IDE server; help copy stops calling it "standalone server". |
| `renart standalone` | exists — desktop app. |
| `renart run <target>` | Run a pipeline (`marts`), a single asset (`marts.orders` or a path), `--downstream` for the cone. Flags: `--env`, `--start-date/--end-date`, `--json`, `--local`, `--quiet`. |
| `renart ls [pipelines\|assets]` | List what's in the workspace (cheap; `--json` for scripting). |
| `renart type-check [target]` | exists — gains walk-up + the target grammar; stays local-capable (read-only). |
| `renart deploy [pipeline]` | exists — gains walk-up + target grammar; loses `--scheduler-state`. |
| `renart init [dir]` | Scaffold a project from the shipped templates (`service.ScaffoldProject`): `--template chess\|retail\|empty` (default `empty`). Completes the terminal story: init → run → web. |
| `renart debug …` | hidden group: `fp`, `sql-lsp`, `warm-cache`. |

Deliberately **not** in v1: the web-UI terminal view (dropped by decision —
revisit when the standalone CLI has proven itself; PTY design in git
history), `renart schedule` management, backfill windows (waits for the
full-refresh/backfill plan in `materialization-strategies.md`), connection
management.

Output: human-readable by default — one status line per asset (name, status,
duration, rows where available), colored via the existing `fatih/color`
setup, streamed as the run progresses; `--json` emits JSON-lines events for
scripting.

## 4. Edge cases & risks

- **CLI run + scheduled run collide** (client mode): both flow through the
  server's execution service — same serialization as UI-triggered runs
  today; no new behavior.
- **Embedded CLI running, server starts up:** the server boots normally; its
  startup staleness read happens after the CLI's short SQLite transactions.
  A DuckDB materialization mid-flight can make a server-triggered run hit
  the busy error — acceptable, visible, retryable. Optional hardening
  (post-v1): advisory flock at `.renart/exec.lock` shared by both.
- **Stale `server.json` after kill -9:** PID + health check handles it;
  discovery must never block more than ~250 ms before falling back to
  embedded.
- **Two servers, one workspace** (port fallback makes this possible): last
  writer of `server.json` wins; the loser still works via its own UI. Not
  worth solving in v1.
- **A concurrent non-renart process** holding the DuckDB lock: out of our
  control; the busy-error message already tells the user what happened.

## 5. Implementation order

1. **Surface cleanup** (§0): root name/usage, categories, `debug` group,
   help polish, exit-code + flag conventions on the existing commands.
   Small, independently shippable, fixes the visible uncleanliness now.
2. **Workspace walk-up + `internal/clientapi` + `server.json`/health +
   `renart run`** (client + embedded modes) — the bulk of the value.
3. **Polish:** `renart ls`, `renart init`, `--json` event stream,
   version-skew warning, docs (update `docs/.../reference/cli.mdx` per
   `architecture/docs.md`).

Verification: e2e that starts a server, runs `renart run` in client mode and
asserts the run appears in `/api/runs` + staleness flips; an embedded-mode
test against a workspace with no server; a snapshot-style test on `--help`
output so the surface contract stays enforced.
