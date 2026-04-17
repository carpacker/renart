### Project Context: Renart

## Overview

Renart is the fast visual workspace for Bruin projects.

It is aimed at data engineers, analytics engineers, and technical data users who want a quicker visual way to edit, inspect, and run version-controlled data pipelines while still working in Git-backed projects.

Renart should feel meaningfully different from a code-first workflow:

- Bruin CLI is for a more coding-oriented, terminal-first experience
- Renart is for fast visual editing, inspection, and execution of those same projects
- The canvas should make assets, dependencies, lineage, and data flow easier to understand at a glance
- The UI should stay comfortable for users working in Git-backed repositories

## Product Positioning

When making product or UX decisions, preserve this direction:

- Renart is a visual editor for people building data pipelines that remain version-controllable
- Renart presents assets, dependencies, lineage, and data flow on a canvas instead of forcing users to reason only from YAML and SQL files
- Renart is a fast visual alternative to pure file editing inside Bruin projects
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

- **Backend:** Go HTTP server in the Bruin repo / Renart extraction target
- **Frontend:** React 19 + TypeScript
- **Routing:** TanStack Router
- **Build Tool:** Vite via `rolldown-vite`
- **Styling:** Tailwind CSS v4 + shadcn/ui + Radix primitives
- **Canvas / DAG:** React Flow
- **Editor:** Monaco via `@monaco-editor/react`
- **State:** Jotai
- **Forms:** React Hook Form
- **Charts:** Recharts
- **Panels:** `react-resizable-panels`
- **Tables:** `@tanstack/react-virtual`
- **Realtime Sync:** Server-Sent Events (SSE)

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
- `POST /api/pipelines`
- `DELETE /api/pipelines/:pipelineId`
- `POST /api/pipelines/:pipelineId/assets`
- `PUT /api/pipelines/:pipelineId/assets/:assetId`
- `DELETE /api/pipelines/:pipelineId/assets/:assetId`
- `GET /api/assets/:assetId/inspect`
- `POST /api/assets/:assetId/materialize/stream`
- `GET /api/pipelines/:pipelineId/materialization`
- `GET /api/assets/freshness`
- `GET /api/assets/:assetId/columns/infer`
- `PUT /api/assets/:assetId/columns`
- `POST /api/assets/:assetId/fill-columns-from-db`

Do not introduce frontend assumptions that require a separate Node backend runtime.

## Practical Guidance For Agents

- Prefer updating existing hooks and components over introducing parallel state systems
- Keep layouts shrink-safe with `min-w-0`, truncation, and overflow control
- If a change affects both full inspect views and node previews, update both
- If a feature changes asset creation or execution semantics, verify both frontend behavior and backend results
- Prefer `pnpm` over `npm` when both are available
- Keep changes small and concrete when possible
- Favor user-facing clarity over internal cleverness in Renart-specific docs and UI copy

## Do

- Use Bruin and Renart backend APIs as the path for filesystem-changing operations
- Let SSE reconcile final workspace state
- Preserve the distinction between code-first CLI work and fast visual UI work
- Keep the product legible for data engineers and technical data users working in Git-backed projects

## Do Not

- Do not add polling for workspace refresh
- Do not bypass the Go server for filesystem writes
- Do not treat Jotai as persistent truth
- Do not add Node-only server routes as if Renart were a separate fullstack JS app
- Do not collapse the product into a generic CRUD dashboard

## Validation

- Validate frontend changes with `corepack pnpm build` in `renart/web`
- Validate Renart backend work with the relevant Go tests
- Validate docs changes with `corepack pnpm build` in `renart/docs` when touching the Starlight site
- When relevant, validate the local live flow with `go build .` in `renart/` and `corepack pnpm test:e2e:live` in `renart/web`
