# Renart CLI v1 — run pipelines from a real terminal

Status: proposed. Goal: make `renart` a first-class CLI for running pipelines,
usable (a) inside a new terminal view in the web UI and (b) standalone in a
shell/CI — **including while a renart web server is running against the same
workspace**.

## 1. Starting point

The binary already is a CLI (`urfave/cli/v3`): `web`, `standalone`, `fp`,
`deploy`, `type-check`, `warm`. What's missing is the verb users actually
want — `renart run` — plus a coherent story for coexisting with a running
server, and the terminal surface in the UI.

Two facts drive the whole design:

- **DuckDB is single-writer per file, cross-process.** In-process, bruin
  serializes access with a per-path map mutex (`pkg/duckdb/lock.go`); across
  processes there is no coordination — a second process hits DuckDB's file
  lock and fails ("duckdb database is busy (lock held by another process)",
  already mapped to a friendly message in `execution.go`). A CLI process
  materializing into the same DuckDB file the server is writing **cannot be
  made safe**, only avoided.
- **SQLite is already multi-process-safe here.** `.renart/state.db` runs WAL +
  `busy_timeout=5000`, transactions are short, and `renart deploy` already
  writes snapshots into it from a separate process today. The only hard
  constraint is: **never run two River schedulers** against the same DB
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

This is the same pattern as docker/dagger CLIs: the daemon owns shared
mutable state whenever it exists; the CLI is a thin client. It solves every
concurrency question at once instead of sprinkling locks:

- DuckDB: all writes flow through one process → the in-process mutex is
  sufficient again.
- SQLite: facts/coverage/run records written by one process while a server
  runs; embedded mode only writes when no server exists.
- Staleness: client-mode runs go through the server's bus, so badges and SSE
  update live. Embedded runs happen when no server is watching.
- Policy: `policy.Check` sits in the shared execution dispatch, so protected
  environments behave identically in UI, client mode, and embedded mode.

### 2.1 Server discovery

- On startup the server writes `.renart/server.json`:
  `{pid, host, port, workspace_root, version, started_at, token}`; removed on
  graceful shutdown. A stale file (dead PID / failed health check) is ignored
  and overwritten.
- New `GET /api/health` returns `{version, workspace_root}` — the CLI
  verifies it's talking to a server for *this* workspace (resolve symlinks
  before comparing).
- `RENART_SERVER=<url>` (+ `RENART_TOKEN`) overrides discovery — this is how
  the web-UI terminal pins the CLI to its own server, and later the seam for
  the cloud.
- `--local` / `RENART_NO_SERVER=1` forces embedded mode. If a live server is
  detected, `--local` **warns** (DuckDB conflicts possible, server staleness
  won't update) and, on conflict, surfaces the busy error with a retry hint
  rather than retry-looping forever.
- Version skew: warn when CLI and server versions differ (normally it's the
  same binary).

### 2.2 Workspace resolution

Like git: walk up from cwd to the nearest directory containing `.bruin.yml`
(fallback: a `pipeline.yml` ancestor). Explicit `--workspace` overrides. This
makes the CLI work naturally when the terminal is cd'd into
`pipelines/marts/`.

### 2.3 Client mode transport

Reuse the existing streaming endpoints —
`POST /api/pipelines/{id}/materialize/stream` and
`POST /api/assets/{assetID}/materialize/stream` — plus `GET /api/workspace`
for target resolution. Put the HTTP client in a small `internal/clientapi`
package shared by all CLI commands (and later by anything cloud-shaped).
Requests carry the token from `server.json`; the server accepts it as an
alternative to the Origin check (CLI requests have no Origin header — today
they pass because non-browser requests without Origin are allowed; the token
makes this explicit and future-proof).

### 2.4 Embedded mode

Construct the same dependency graph `cmd/server.go` builds, minus watcher,
SSE hub, HTTP, and the River scheduler *service* (the store is still opened
to write run facts/snapshots — same as `renart deploy` today). One initial
workspace parse instead of a watcher. `policy.Check` runs as in the server;
`confirm_destructive` prompts on TTY. Facts + coverage land in
`.renart/state.db` so the next server start sees correct staleness.

## 3. Command surface (v1)

| Command | Behavior |
| --- | --- |
| `renart run <target>` | Run a pipeline (`marts`), a single asset (`marts/orders.sql` or asset name), with `--downstream` for the cone. Flags: `--env`, `--start-date/--end-date`, `--json`, `--local`. |
| `renart ls [pipelines\|assets]` | List what's in the workspace (cheap, makes the terminal view immediately useful). |
| `renart type-check` | exists — gains workspace walk-up + delegation-aware defaults (stays local-capable; it's read-only). |
| `renart deploy` | exists — gains the same workspace resolution. |
| `renart fp` | exists (debug). |

Deliberately **not** in v1: `renart schedule` management, backfill windows
(waits for the full-refresh/backfill plan in
`materialization-strategies.md`), `renart init`/scaffolding, connection
management (see `project-settings-and-workspaces.md`).

Output: human-readable by default — one status line per asset (name, status,
duration, rows where available), colored via the existing `fatih/color`
setup, streamed as the run progresses; `--json` emits JSON-lines events for
scripting. Exit codes: 0 success, 1 run/validation failure, 2 usage error.
`--quiet` for CI.

## 4. Terminal view in the web UI

A real PTY, not a restricted command runner. The server already executes
SQL/Python and writes files on request, so a shell adds little new risk
surface *for a loopback-bound, origin-guarded server* — and users will
immediately want `git`, `ls`, `dbt`, etc. alongside `renart`.

- **Backend:** `creack/pty` spawning `$SHELL` (fallback `bash`), cwd =
  workspace root. Injected env: `RENART_SERVER=http://127.0.0.1:<port>`,
  `RENART_TOKEN`, and `PATH` prepended with the directory of
  `os.Executable()` so `renart` resolves to the running binary. Windows:
  out of scope for v1 (`standalone` users on Windows keep the CLI without the
  embedded terminal; revisit with ConPTY).
- **Transport:** WebSocket at `/api/terminal` — Origin-checked on upgrade
  (same guard as state-changing requests) plus the session token. Messages:
  binary I/O frames, `resize` control frames, ping/pong.
- **Session model:** sessions live server-side with a small scrollback ring
  buffer, so a browser reload reattaches instead of killing the shell.
  Explicit close or server shutdown terminates the PTY. v1: one session at a
  time (a tab strip is a frontend-only extension later).
- **Frontend:** xterm.js in a bottom dock panel (toggle in the shell, like
  the runs drawer), theme-synced. Because the CLI delegates to the same
  server, a `renart run` typed in the terminal lights up the same run UI,
  staleness badges, and SSE events the Build button does — that's the payoff
  of §2.
- Flag `--no-terminal` on `renart web` disables the endpoint for people who
  don't want a shell reachable from the browser at all.

## 5. Edge cases & risks

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
- **`bruin` CLI running concurrently:** out of our control either way; the
  busy-error message already tells the user what happened.
- **Security:** the terminal endpoint is the most sensitive thing the server
  exposes. It must never bind non-loopback while the feature exists in this
  form; the Origin guard + token are load-bearing, and the `--no-terminal`
  opt-out ships in the same release.

## 6. Implementation order

1. **Workspace walk-up + `internal/clientapi` + `server.json`/health +
   `renart run`** (client + embedded modes). This is the bulk of the value
   and independently shippable.
2. **Terminal view** (PTY, WebSocket, xterm.js dock, env injection).
3. **Polish:** `renart ls`, `--json` event stream, version-skew warning,
   docs (new CLI reference section per `architecture/docs.md`).

Verification: e2e that starts a server, runs `renart run` in client mode and
asserts the run appears in `/api/runs` + staleness flips; an embedded-mode
test against a workspace with no server; a live e2e driving the terminal
(type `renart run`, assert the run UI updates).
