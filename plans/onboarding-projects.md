# First-run onboarding + create-project flows

Status: proposed (blocked on questions.md #4/#5 for the visual concept;
mechanics below proceed)

## Goal

A first-time renart user (no project yet, or empty workspace) gets a real
onboarding screen; from it — and from the project switcher — they can create
a new project three ways:

a) **Demo project** — pick one of the bundled demos; renart writes the files,
   `git init` + `.gitignore`, then materializes everything once with a
   progress/loading animation so the user lands in a *working* workspace.
b) **Import warehouse tables** — connect (duckdb/postgres/…), pick existing
   tables, generate source assets (`type: <dialect>.source` or seed-style
   declarations) into a fresh pipeline.
c) **Empty slate** — minimal `pipeline.yml` + example asset + git init.

## Existing pieces to reuse

- `internal/web/service/onboarding.go` already scaffolds the chess quickstart
  (players/games/player_stats/python) and knows connection setup states.
- Project registry / per-project runtimes (multi-project server) landed in
  `9d2c1fc` — new projects must register there; the switcher
  (`components/redesign/project-switcher.tsx`) gets a "New project…" entry.
- Old onboarding routes/components (`routes/onboarding/*`,
  `workspace-onboarding.tsx`) die with the old UI; the new flow is a redesign
  route (`/welcome` or a dialog-on-first-run in the shell).

## Backend

1. `POST /api/projects` accepts `{name, path?, template: "demo:<id>" |
   "import" | "empty"}`:
   - creates dir, writes template files, `git init`, `.gitignore`
     (`.renart/`, `duckdb-files/`, `logs/`, `__pycache__/`, `.env`),
     initial commit,
   - registers the project and returns its id.
2. Demo templates live in `internal/web/service/templates/` (embed.FS):
   at least the chess demo; a second self-contained DuckDB/CSV demo (no
   network) for offline first-runs.
3. `POST /api/projects/{id}/bootstrap-run` triggers the initial full
   materialization; progress is streamed over the existing SSE run events so
   the UI can animate per-asset progress.
4. Import flow reuses the connection-create endpoints + a `list tables`
   endpoint per connection type (duckdb: information_schema; postgres:
   pg_catalog) + an endpoint that writes source asset YAMLs for the chosen
   tables.

## Frontend

1. First-run detection: server flags `workspace.empty && !projects` in
   `/api/workspace` (or a dedicated `/api/onboarding/state`); the shell
   redirects to `/welcome`.
2. `/welcome` (redesign route, shadcn components only — Card, Button, Tabs,
   Progress, Dialog; no bespoke CSS unless the concept demands it):
   - three option cards (demo / import / empty),
   - demo path: pick demo → name/location → creating… (checklist with per-file
     ticks) → materializing… (per-asset progress bar from SSE) → "Open
     workspace",
   - import path: connection form → table multi-select → review generated
     assets → create,
   - empty path: name → create.
3. Same flows reachable later from the project switcher ("New project…").

## Verification

- e2e: create-empty and create-demo (offline demo) through the UI against a
  scratch server; assert git repo + .gitignore + initial commit + registered
  project + demo tables materialized.
