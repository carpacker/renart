### Project Context: Renart

## Overview

Renart is the git-native data pipeline IDE for Bruin projects.

It is aimed at data engineers, analytics engineers, and technical data users who want a fast visual way to edit, inspect, run, and understand version-controlled data pipelines while staying inside a Git-backed project.

Renart should feel meaningfully different from a code-first workflow:

- Bruin CLI is for a more coding-oriented, terminal-first experience
- Renart is a Git-native IDE for visually editing, inspecting, and executing those same projects
- The canvas should make assets, dependencies, lineage, and data flow easier to understand at a glance
- The UI should stay comfortable for users who expect filesystem and Git history to remain the source of accountability

## Product Positioning

When making product or UX decisions, preserve this direction:

- Renart is a git-native data pipeline IDE for people building data pipelines that remain version-controllable
- Renart presents assets, dependencies, lineage, and data flow on a canvas instead of forcing users to reason only from YAML and SQL files
- Renart is a fast visual alternative to pure file editing while preserving the underlying Bruin project files
- Renart should help users move quickly, not add ceremony
- AI-enhanced and visual workflows should feel like a strength of the product, not an afterthought

## Runtime Model

### Single source of truth

The filesystem is authoritative.

Frontend state exists for responsiveness and interaction flow, but workspace state coming back from the backend wins if there is a conflict.

### How data flows

1. The Go server watches workspace files.
2. The frontend loads initial state from `/api/workspace`.
3. The frontend subscribes to `/api/events` through SSE.
4. Filesystem-changing actions go through Go endpoints under `/api/...`.
5. SSE reconciles the final state after writes, CLI usage, or outside file edits.

### Important sync rule

Do not add polling for workspace changes. Use SSE-driven updates.

### Repository requirement

Renart should be started inside a Git repository.

If a user flow depends on workspace discovery or project semantics, prefer solutions that continue to assume a Git-backed project rather than silently loosening that model.

## Current Stack

- **Backend:** Go HTTP server using Bruin packages for project parsing, config, execution, and persistence
- **Frontend:** React 19.2 + TypeScript 5.9
- **Routing:** TanStack Router
- **Build Tool:** Vite 8 via `rolldown-vite`
- **Styling:** Tailwind CSS v4 + shadcn/ui + Radix primitives + Base UI where used
- **Canvas / DAG:** React Flow
- **Editor:** Monaco via `@monaco-editor/react`
- **State:** Jotai plus SWR where hooks already use remote cache semantics
- **Forms:** React Hook Form and TanStack Form where already adopted
- **Charts:** Recharts
- **Panels:** `react-resizable-panels`
- **Tables:** `@tanstack/react-virtual`
- **Command UI:** `cmdk`
- **Icons:** `lucide-react` and `react-icons`
- **Markdown:** `react-markdown`
- **Realtime Sync:** Server-Sent Events (SSE)
- **Docs:** Astro Starlight in `docs/`

## Local Tooling

- Go is installed at `/usr/local/go`. If `go` is not on `PATH`, use `/usr/local/go/bin/go`.

## Frontend Learnings

When working on `renart/web`, preserve the component and routing patterns the app already uses.

### Cards

- Prefer the shared shadcn card primitives from `renart/web/components/ui/card.tsx` for panelized UI.
- Do not build ad hoc "card" wrappers out of plain `div`s with repeated `rounded-* border bg-card shadow-*` class stacks when the intent is a card.
- If a page needs a header/body split, use `Card`, `CardHeader`, `CardTitle`, `CardDescription`, and `CardContent` instead of hand-rolled section shells.

### TanStack Router

- For hierarchical URLs that should not visually nest parent pages, use pathful layout routes with `route.tsx` plus leaf `index.tsx` files.
- Example pattern for settings-style pages:
  - `foo/route.tsx` for the non-visual parent route that renders `<Outlet />`
  - `foo/index.tsx` for `/foo`
  - `foo/$id/route.tsx` for the non-visual branch route
  - `foo/$id/index.tsx` for `/foo/$id`
- Avoid underscore-flattened route hacks when the goal is simply to prevent a visible parent page from rendering around its children.
- Avoid putting visible page UI in a route file that also needs to host child routes unless you explicitly want nested rendering.
- After changing route files, regenerate the TanStack route tree by running the Renart web build so `src/routeTree.gen.ts` matches the filesystem routes.

## App Shape

### Entry points

- [web/src/main.tsx](web/src/main.tsx)
- [web/src/router.tsx](web/src/router.tsx)
- [web/src/providers.tsx](web/src/providers.tsx)

### Main workspace shell

The primary UI is rendered by [web/components/workspace-shell.tsx](web/components/workspace-shell.tsx).

It coordinates:

- workspace synchronization
- canvas nodes and edges
- selection state
- onboarding and help state
- create/delete flows
- inspect and materialize results
- debounced asset saving
- persisted node positions
- sidebar, canvas, editor, and results layout

### Key visual areas

- [web/components/workspace-sidebar.tsx](web/components/workspace-sidebar.tsx)
- [web/components/workspace-canvas-pane.tsx](web/components/workspace-canvas-pane.tsx)
- [web/components/workspace-editor-pane.tsx](web/components/workspace-editor-pane.tsx)
- [web/components/workspace-results-panel.tsx](web/components/workspace-results-panel.tsx)
- [web/components/workspace-dialogs.tsx](web/components/workspace-dialogs.tsx)

### Backend service shape

The Go backend is organized around small HTTP handler packages in `internal/web/httpapi` and service code in `internal/web/service`.

Important service files include:

- `workspace.go`, `workspace_coordinator.go`, and `workspace_resolver.go` for workspace loading, SSE-oriented coordination, path safety, and ID resolution
- `asset.go`, `asset_naming.go`, `asset_dependencies.go`, `asset_format.go`, and `asset_type.go` for asset CRUD, inferred naming, dependency reconciliation, SQL formatting, and Bruin asset type mapping
- `execution.go`, `run.go`, and `pipeline_execution.go` for inspect, materialize, run, freshness, and streaming execution flows
- `direct_executor.go`, `direct_run.go`, `direct_query.go`, `direct_executor_registry.go`, `direct_executor_import.go`, `direct_executor_resolution.go`, `direct_executor_patches.go`, `direct_asset_patches.go`, and `direct_run_formatting.go` for embedded/direct execution, query, import, patch, and output formatting support
- `config_runtime.go`, `query_result.go`, and `db_discovery.go` for shared config/runtime, query result envelopes, and database object discovery helpers
- `onboarding.go`, `config.go`, `sql.go`, `parse_context.go`, `jinja_render.go`, and `suggestions.go` for onboarding, config editing, SQL discovery/intellisense, Jinja rendering, and UI suggestions

## Core UX Behaviors To Preserve

- live SSE synchronization for workspace changes
- debounced asset saves instead of write-on-every-keystroke
- visual-first node creation from canvas interactions
- downstream asset creation directly from asset nodes
- inline asset and pipeline editing flows
- inspect and materialize states that replace stale output immediately
- canvas-first understanding of lineage and dependencies
- editor and visualization flows that still respect the underlying workspace files
- mobile-safe and desktop-safe interactions where supported today

## Inspect and Execution Rules

- Inspect is a safe preview path and should stay conservative
- Plain SQL inspect/query execution must not run write-capable SQL
- Inspect for plain SQL assets should stay limited to read-only single `SELECT` queries
- Materialization is the path for executing SQL that produces side effects
- When direct execution cannot confidently match Bruin behavior, prefer explicit CLI fallback

## Current API Surface

Frontend code already uses Go endpoints including:

- `GET /api/workspace`
- `GET /api/events`
- `GET /api/config`
- `POST /api/config/environments`
- `PUT /api/config/environments`
- `POST /api/config/environments/clone`
- `DELETE /api/config/environments`
- `POST /api/config/connections`
- `PUT /api/config/connections`
- `DELETE /api/config/connections`
- `POST /api/config/connections/test`
- `POST /api/pipelines`
- `PUT /api/pipelines`
- `GET /api/pipelines/:pipelineId/config`
- `PUT /api/pipelines/:pipelineId/config`
- `DELETE /api/pipelines/:pipelineId`
- `POST /api/pipelines/:pipelineId/assets`
- `PUT /api/pipelines/:pipelineId/assets/:assetId`
- `DELETE /api/pipelines/:pipelineId/assets/:assetId`
- `POST /api/assets/:assetId/format-sql`
- `GET /api/assets/:assetId/inspect`
- `POST /api/assets/:assetId/materialize/stream`
- `GET /api/pipelines/:pipelineId/materialization`
- `POST /api/pipelines/:pipelineId/materialize/stream`
- `POST /api/run`
- `GET /api/assets/freshness`
- `GET /api/assets/:assetId/columns/infer`
- `PUT /api/assets/:assetId/columns`
- `POST /api/assets/:assetId/fill-columns-from-db`
- `POST /api/assets/:assetId/render-jinja`
- `GET /api/assets/:assetId/sql-path-suggestions`
- `GET /api/ingestr/suggestions`
- `POST /api/sql/parse-context`
- `POST /api/sql/column-values`
- `GET /api/sql/databases`
- `GET /api/sql/tables`
- `GET /api/sql/table-columns`
- `GET /api/onboarding/state`
- `PUT /api/onboarding/state`
- `POST /api/onboarding/import`
- `POST /api/onboarding/quickstart`
- `POST /api/onboarding/discovery`
- `GET /api/onboarding/path-suggestions`

Do not introduce frontend assumptions that require a separate Node backend runtime.

## Practical Guidance For Agents

- Prefer updating existing hooks and components over introducing parallel state systems
- Keep layouts shrink-safe with `min-w-0`, truncation, and overflow control
- If a change affects both full inspect views and node previews, update both
- If a feature changes asset creation or execution semantics, verify both frontend behavior and backend results
- Prefer Bruin Go packages and Renart service helpers over shelling out unless the behavior intentionally falls back to CLI semantics
- Keep typed service responses/errors and shared workspace resolution patterns rather than reintroducing ad hoc maps or path decoding
- Prefer `pnpm` over `npm` when both are available
- Keep changes small and concrete when possible
- Favor user-facing clarity over internal cleverness in Renart-specific docs and UI copy

## Do

- Use Bruin and Renart backend APIs as the path for filesystem-changing operations
- Let SSE reconcile final workspace state
- Preserve the distinction between code-first CLI work and git-native visual IDE work
- Keep the product legible for data engineers and technical data users working in Git-backed projects

## Do Not

- Do not add polling for workspace refresh
- Do not bypass the Go server for filesystem writes
- Do not treat Jotai as persistent truth
- Do not add Node-only server routes as if Renart were a separate fullstack JS app
- Do not collapse the product into a generic CRUD dashboard
- Do not weaken the Git-backed project assumption to make flows appear easier

## Validation

- Validate frontend changes with `corepack pnpm build` in `renart/web`
- Validate Renart backend work with the relevant Go tests
- Validate docs changes with `corepack pnpm build` in `renart/docs` when touching the Starlight site
- When relevant, validate the local live flow with `go build .` in `renart/` and `corepack pnpm test:e2e:live` in `renart/web`
