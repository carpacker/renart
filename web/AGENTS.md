### Project Context: Bruin Web

## Overview

Bruin Web is the local-first browser UI for the Bruin CLI. It is not a standalone SaaS app and it is not backed by a Node.js server. The frontend is a static React application embedded into the Go Bruin binary and served by the Go HTTP server.

The app is centered around an interactive lineage canvas, a resizable editor pane, live workspace updates from the filesystem, and direct manipulation of Bruin pipelines and assets.

## Current Stack

- **Backend:** Go HTTP server in the main Bruin repo
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

## Runtime Model

### Single source of truth

The **filesystem is authoritative**. Frontend state exists only for immediate UX responsiveness. If a conflict exists between local UI state and what comes back from the backend, backend/workspace state wins.

### How data flows

1. The Go server watches Bruin workspace files.
2. The frontend loads initial state from `/api/workspace`.
3. The frontend subscribes to `/api/events` via SSE.
4. All create/update/delete/materialize/inspect actions call Go endpoints under `/api/...`.
5. SSE updates reconcile the UI after writes, CLI usage, or external file edits.

### Important sync rule

Do **not** add polling for workspace changes. Use SSE-driven updates.

## Dev Server Behavior

- Local frontend dev server runs on **5173**.
- Vite proxies `/api` to the Go server on **http://127.0.0.1:3000**.
- Production output is static and must remain compatible with Go embedding.

See [vite.config.ts](vite.config.ts).

## Current App Shape

### Entry points

- [src/main.tsx](src/main.tsx) mounts the app.
- [src/router.tsx](src/router.tsx) defines the TanStack Router root and the `/` route.
- [src/providers.tsx](src/providers.tsx) wires app-level providers.

### Main screen

The primary UI is rendered by [components/workspace-shell.tsx](components/workspace-shell.tsx).

It coordinates:

- workspace sync
- canvas nodes and edges
- selection state
- onboarding/help state
- create/delete dialogs
- inspect/materialize results
- debounced asset saving
- persisted node positions
- sidebar + canvas + editor layout

### Key visual areas

- [components/workspace-sidebar.tsx](components/workspace-sidebar.tsx): pipeline list, selection, collapse state, pipeline actions
- [components/workspace-canvas-pane.tsx](components/workspace-canvas-pane.tsx): React Flow canvas
- [components/workspace-editor-pane.tsx](components/workspace-editor-pane.tsx): Monaco editor + configuration + visualization settings
- [components/workspace-results-panel.tsx](components/workspace-results-panel.tsx): inspect/materialize output
- [components/workspace-dialogs.tsx](components/workspace-dialogs.tsx): confirmation and creation dialogs

## Important Hooks

- [hooks/use-workspace-sync.ts](hooks/use-workspace-sync.ts)
  - fetches `/api/workspace`
  - subscribes to `/api/events`
  - preserves asset `content` on lite SSE updates when appropriate

- [hooks/use-asset-actions.ts](hooks/use-asset-actions.ts)
  - create/update/delete asset
  - create/delete pipeline

- [hooks/use-asset-canvas-interactions.ts](hooks/use-asset-canvas-interactions.ts)
  - click/right-click asset creation
  - downstream child asset creation from nodes
  - draft-node lifecycle and outside-click dismissal

- [hooks/use-debounced-asset-save.ts](hooks/use-debounced-asset-save.ts)
  - debounced writes back to the backend

- [hooks/use-asset-results.ts](hooks/use-asset-results.ts)
  - inspect and materialize flows

- [hooks/use-asset-previews.ts](hooks/use-asset-previews.ts)
  - node preview inspection data and pagination

- [hooks/use-pipeline-materialization-state.ts](hooks/use-pipeline-materialization-state.ts)
  - freshness/materialization enrichment

- [hooks/use-persisted-node-positions.ts](hooks/use-persisted-node-positions.ts)
  - stores custom graph positions on the client side

## Important Libraries / Helpers

- [lib/api.ts](lib/api.ts): frontend API surface for all Go endpoints
- [lib/types.ts](lib/types.ts): shared web-side data types
- [lib/graph.ts](lib/graph.ts): React Flow node/edge generation and layout helpers
- [lib/asset-visualization.ts](lib/asset-visualization.ts): visualization metadata parsing
- [lib/atoms.ts](lib/atoms.ts): Jotai atoms for workspace, selection, editor, and tabs
- [lib/sql-schema.ts](lib/sql-schema.ts): schema context for SQL intellisense

## Supported UX Patterns

These behaviors already exist and should be preserved when changing the UI:

- **Live SSE synchronization** for workspace changes
- **Debounced asset saves** instead of write-on-every-keystroke
- **Visual-first node creation** from canvas interactions
- **Downstream asset creation** directly from asset nodes
- **Pipeline deletion** with confirmation dialog
- **Asset renaming** through the editor form
- **Split-pane editing** with Monaco and metadata tabs
- **Inspect/materialize loading states** that replace old content immediately
- **Table, chart, and markdown visualization modes**
- **Bar chart support** in inspect views and node previews
- **Dense table mode** via visualization metadata
- **Fresh/stale materialization indicators**
- **Asset type/provider icons** based on real asset type semantics
- **Initial loading screen** before workspace state is available
- **Collapsible active pipeline** while keeping it selected

## Visualization Metadata

Visualization settings are driven by asset metadata. Common keys include:

- `web_view`
- `web_chart_type`
- `web_chart_x`
- `web_chart_series`
- `web_chart_title`
- `web_table_columns`
- `web_table_limit`
- `web_table_dense`
- `web_markdown_column`
- `web_markdown_template`

When updating visualization behavior, keep both the full inspect view and the asset-node preview in sync.

## Current API Surface

Frontend code already calls these Go endpoints through [lib/api.ts](lib/api.ts):

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

Do not introduce frontend assumptions that require a separate server runtime.

## Constraints for Future Changes

### Do

- Prefer updating existing hooks/components over introducing parallel state systems.
- Prefer composition over view-switch components when distinct concerns grow apart.
- Use TanStack Router layout routes to share settings/page structure instead of folding multiple editors into one component.
- Move shared stateful behavior into custom hooks and keep form panes focused on a single responsibility.
- Keep writes debounced when editing asset content.
- Let SSE reconcile the final workspace state.
- Preserve current React Flow interactions and selection behavior.
- Use Bruin/Go APIs as the source for filesystem-changing operations.
- Keep layouts shrink-safe with `min-w-0`, overflow control, and truncation where needed.

### Do not

- Do not add Node.js-only API routes.
- Do not add polling for workspace refresh.
- Do not bypass the Go server for filesystem writes.
- Do not treat Jotai as persistent truth.
- Do not infer asset semantics from UI labels when canonical metadata is available.

## Layout Notes

The right editor pane is sensitive to flexbox overflow bugs. When touching editor-pane layout, tabs, or visualization settings:

- ensure flex children that must shrink use `min-w-0`
- avoid width rules that preserve expanded sizes after resize
- prefer truncation over overflow for tab labels and compact controls
- validate both expansion and shrinking of the resizable pane

Relevant files:

- [components/workspace-editor-pane.tsx](components/workspace-editor-pane.tsx)
- [components/ui/tabs.tsx](components/ui/tabs.tsx)

## Practical Guidance For Agents

- Read existing hooks before adding new workspace behavior.
- If a feature touches both inspect views and node previews, update both.
- If a feature changes asset creation semantics, verify both frontend input building and backend-generated asset results.
- Prefer `pnpm` over `npm` for web dependency installation, audits, and script execution when both are available.
- Prefer small, surgical UI changes that preserve current interaction patterns.
- Validate with a frontend build from [package.json](package.json): `pnpm build`.

## Summary

Bruin Web is currently a Vite-built, TanStack Router-based, SSE-driven React app embedded into the Go Bruin server. It is a visual pipeline editor first, not a form-over-CRUD dashboard. Any change should respect the filesystem-first model, Go-backed APIs, real-time SSE sync, and the established canvas/editor/results workflow.
