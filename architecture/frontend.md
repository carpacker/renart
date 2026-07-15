# Renart web frontend — current architecture

Status: current state. The frontend is a static React app built by Vite and
embedded into the Go binary; the Go server serves it and owns every
filesystem-changing action. There is no Node.js runtime in production.

## 1. Stack

- **React 19.2 + TypeScript 7**
- **Routing:** TanStack Router (file-based, generated route tree)
- **Build:** Vite 8 via `rolldown-vite`
- **Styling:** Tailwind CSS v4 + shadcn/ui + Radix primitives + Base UI where used
- **Canvas / DAG:** React Flow
- **Editor:** Monaco via `@monaco-editor/react`
- **State:** Jotai, plus SWR where hooks already use remote-cache semantics
- **Forms:** React Hook Form and TanStack Form where already adopted
- **Charts:** Recharts
- **Panels:** `react-resizable-panels`
- **Tables:** `@tanstack/react-virtual`
- **Command UI:** `cmdk`
- **Icons:** `lucide-react`, `react-icons`
- **Markdown:** `react-markdown`
- **Quality tooling:** Oxlint for linting and Oxfmt for formatting
- **Realtime sync:** Server-Sent Events (see [backend.md](backend.md) §2)

## 2. Dev server

- Frontend dev server runs on **5173**; Vite proxies `/api` to the Go server on
  **http://127.0.0.1:3000**. See [`../web/vite.config.ts`](../web/vite.config.ts).
- Production output is static and must stay compatible with Go embedding.

## 3. App shape

Paths below are relative to `web/`.

### Entry points

- [src/main.tsx](../web/src/main.tsx) mounts the app.
- [src/providers.tsx](../web/src/providers.tsx) wires app-level providers (`AppProviders`).
- [src/router.tsx](../web/src/router.tsx) builds the TanStack Router from the
  generated route tree (`AppRouter`).

### Routing

File-based routes under [src/routes](../web/src/routes):

- [__root.tsx](../web/src/routes/__root.tsx) → [_shell.tsx](../web/src/routes/_shell.tsx),
  a pathless layout route that renders the app shell.
- Pages live under [src/routes/_shell](../web/src/routes/_shell): the build IDE at
  `/pipelines/$pipelineId/...`, plus `catalog`, `notebooks`, `runs`, `schedules`,
  `project` (settings), `account`, `dashboards`. `/` waits for the workspace,
  then redirects to the first pipeline's canvas — or to `/welcome` when the
  workspace has no pipelines.
- [welcome.tsx](../web/src/routes/welcome.tsx) (`/welcome`, outside the shell)
  is the first-run onboarding and new-project wizard
  ([welcome-page.tsx](../web/components/app/welcome-page.tsx)): demo / import /
  empty flows against `POST /api/projects`, with `?new=1` (the project
  switcher's "New project...") forcing creation of a fresh directory instead of
  scaffolding into the current empty workspace. New-project location uses the
  server-backed directory picker shared with the project switcher; it starts at
  the effective suggested parent and can create child folders. Demo creation
  bootstraps the workspace with the `build-stale/stream` run (fresh assets are
  all `never_built`) and renders its per-asset SSE progress and ANSI-colored
  output.
- `redesign.$.tsx` / `redesign.index.tsx` redirect legacy `/redesign/*` bookmarks
  to the root paths — the only place the old "redesign" name survives.
- The route tree is generated into
  [src/routeTree.gen.ts](../web/src/routeTree.gen.ts) **by the build** — never edit
  it by hand. After changing route files, rerun the web build so it matches the
  filesystem routes.

For hierarchical URLs that should not visually nest parent pages, use pathful
layout routes (`route.tsx` renders `<Outlet />`) with leaf `index.tsx` files —
not underscore-flattened route hacks.

### App shell + primary views

- [components/app/app-shell.tsx](../web/components/app/app-shell.tsx) (`AppShell`):
  top nav (Build / Catalog / Notebooks / Runs / Schedules, from
  [app-data.ts](../web/components/app/app-data.ts)), the
  [project switcher](../web/components/app/project-switcher.tsx), the
  [command palette](../web/components/app/app-command-palette.tsx), and the routed
  `<Outlet />`.
- [components/app/build-page.tsx](../web/components/app/build-page.tsx): the primary
  IDE — the interactive lineage canvas
  ([lineage-canvas.tsx](../web/components/app/lineage-canvas.tsx), React Flow)
  beside the asset editor. Bare asset URLs default to this split view; ad-hoc
  queries preserve code/split layout and add the editor beside a canvas-only
  view. The first asset selection from a pipeline-only canvas opens the split
  view; after an asset is present in the route, later selections preserve the
  explicit code/split/canvas layout. A DAG that fits at the default zoom is
  horizontally centered on initial render, while a wider DAG keeps its layout
  origin so it remains predictable to pan. The explorer's asset filter searches
  names, groups, paths, types, and connections.
- [components/app/asset-editor.tsx](../web/components/app/asset-editor.tsx): the
  Monaco editor plus guided metadata cards
  ([asset-guided-cards.tsx](../web/components/app/asset-guided-cards.tsx)) and YAML
  view; wires intellisense through
  [use-asset-monaco.ts](../web/hooks/use-asset-monaco.ts). Load, seed, and sensor
  assets replace Monaco with compact YAML-like parameter editors in the same
  main pane. Seed editors add a drag-and-drop file replacement target directly
  below their parameters; the browser uploads through the Go server and then
  refreshes columns from the new local file. Generic identity and dependency
  metadata remains in the inspector, along with columns and checks for
  relation-producing assets; sensors omit columns and checks because they do not
  materialize a relation.
- The Build view's New asset dialog uses the workspace's backend-provided
  `asset_capabilities` contract for seed and sensor authoring
  ([semantic-asset-create-fields.tsx](../web/components/app/semantic-asset-create-fields.tsx)).
  Its six top-level asset-kind choices use one fixed tile size, while the
  creation fields use the plain `Field` variant instead of nesting a bordered
  card around each input. It filters types and connections together, offers
  upload/workspace-file/URL seed sources, renders the parameters required by
  each sensor variant, and sends uploaded bytes through the multipart asset API.
  The workspace-file source uses the shared path combobox, restricted to
  supported files below the workspace root. The resulting seed and sensor assets
  have distinct canvas/catalog classifications while remaining ordinary Bruin
  files in the workspace.
- Other pages: [catalog-page.tsx](../web/components/app/catalog-page.tsx),
  [notebook-page.tsx](../web/components/app/notebook-page.tsx),
  [runs-page.tsx](../web/components/app/runs-page.tsx),
  [schedules-page.tsx](../web/components/app/schedules-page.tsx),
  [settings-pages.tsx](../web/components/app/settings-pages.tsx),
  [object-pages.tsx](../web/components/app/object-pages.tsx),
  [welcome-page.tsx](../web/components/app/welcome-page.tsx).
  Run details use semantic event badges, link current-workspace asset events
  back to the split Build view, and render timeline asset names in a dedicated
  wrapping column with tooltips so short duration bars never truncate identity.

Project connection routes accept an environment/connection search target so
pipeline default-connection links can open the exact editable connection sheet.

All feature UI lives under `components/app/`; shared primitives under
`components/ui/`. Prefer the shared shadcn card primitives
([components/ui/card.tsx](../web/components/ui/card.tsx)) for panelized UI rather
than hand-rolled `div` shells.

## 4. Key hooks

- [use-workspace-sync.ts](../web/hooks/use-workspace-sync.ts): fetches
  `/api/workspace`, subscribes to `/api/events` (SSE), reconciles workspace state,
  preserves asset `content` on lite SSE updates when appropriate.
- [use-asset-content-editing.ts](../web/hooks/use-asset-content-editing.ts): editor
  draft state, display-value sync, and the Ctrl/Cmd+S save path.
- [use-debounced-asset-save.ts](../web/hooks/use-debounced-asset-save.ts): debounced
  writes back to the backend.
- [use-asset-monaco.ts](../web/hooks/use-asset-monaco.ts): mounts Monaco for the
  asset editor and wires the SQL / Python / YAML / Jinja intellisense hooks.
- [use-sql-lsp.ts](../web/hooks/use-sql-lsp.ts): SQL intellisense via the Go LSP
  (`/api/sql/lsp/*`) — completions, diagnostics, definition, hover, rename. See
  [sql-lsp.md](sql-lsp.md).
- [use-python-query-intellisense.ts](../web/hooks/use-python-query-intellisense.ts):
  maps static SQL string literals in Python `query(...)` calls onto the same Go
  LSP, merges schema-aware client completions (including notebook run columns),
  and translates SQL completions, diagnostics, navigation, and highlighting
  back into the Python Monaco model.
- [use-asset-results.ts](../web/hooks/use-asset-results.ts): inspect and materialize
  flows, including API-asset full refresh.
- [use-app-asset-materialization-status.ts](../web/hooks/use-app-asset-materialization-status.ts):
  freshness / materialization enrichment.
- [use-pipeline-staleness.ts](../web/hooks/use-pipeline-staleness.ts),
  [use-pipeline-scheduler.ts](../web/hooks/use-pipeline-scheduler.ts),
  [use-pipeline-deploy.ts](../web/hooks/use-pipeline-deploy.ts),
  [use-source-control.ts](../web/hooks/use-source-control.ts): run / schedule /
  deploy / VCS surfaces.

## 5. Libraries / helpers

- [lib/api.ts](../web/lib/api.ts): barrel re-exporting the per-domain `api-*.ts`
  modules (`api-assets`, `api-pipelines`, `api-config`, `api-sql`, `api-sql-lsp`,
  `api-scheduler`, `api-source-control`, …) — the frontend surface for every Go
  endpoint. These modules are authoritative for the live API surface.
- [lib/types.ts](../web/lib/types.ts): shared web-side types; re-exports the
  generated API types. The generated types come from the Go DTOs via
  `web/scripts/generate-api-types.mjs` (see [backend.md](backend.md) §5) — don't
  hand-edit `web/lib/generated/api-types.ts`.
- [lib/atoms/](../web/lib/atoms): Jotai atoms split by domain (`workspace`,
  `selection`, `editor`, `results`, `materialization`, `sql-discovery`, suggestion
  catalog).
- [lib/app-lineage-layout.ts](../web/lib/app-lineage-layout.ts): lineage canvas
  layout engine.
- [lib/asset-visualization.ts](../web/lib/asset-visualization.ts): visualization
  metadata parsing.
- [lib/project-context.ts](../web/lib/project-context.ts): per-tab project pin
  (sessionStorage) — `projectApiPath` rewrites `/api/...` onto the pinned
  project's `/api/projects/{id}/...` mount in one place; no pin means the
  server's default project via the unprefixed alias. Two tabs can work on
  different projects against one server.
- [lib/features.ts](../web/lib/features.ts): project-scoped feature flags.
  Ingestr surfaces (source connection types, asset kinds) render only when
  `.renart/project.yml` sets `features.ingestr` or the workspace already
  contains ingestr assets (see [backend.md](backend.md) §2).
- [lib/sql-schema.ts](../web/lib/sql-schema.ts): schema context for SQL
  intellisense.
- [lib/api-asset-templates.ts](../web/lib/api-asset-templates.ts): the three
  pattern-focused HTTP API starters used by the New asset dialog. API assets
  also have sampled response inference, OpenAPI/path diagnostics, and persisted
  cursor controls in the guided editor; see
  [http-api-assets.md](http-api-assets.md).

## 6. Visualization metadata

Inspect/preview rendering is driven by asset metadata keys. Common ones:
`web_view`, `web_chart_type`, `web_chart_x`, `web_chart_series`,
`web_chart_title`, `web_table_columns`, `web_table_limit`, `web_table_dense`,
`web_markdown_column`, `web_markdown_template`. When changing visualization
behavior, keep the full inspect view and the asset-node preview in sync.

## 7. Layout notes

The right editor pane is sensitive to flexbox overflow bugs. When touching
editor-pane layout, tabs, or visualization settings:

- flex children that must shrink use `min-w-0`;
- avoid width rules that preserve expanded sizes after resize;
- prefer truncation over overflow for tab labels and compact controls;
- validate both expansion and shrinking of the resizable pane.

Relevant files: [asset-editor.tsx](../web/components/app/asset-editor.tsx),
[build-page.tsx](../web/components/app/build-page.tsx),
[components/ui/tabs.tsx](../web/components/ui/tabs.tsx).

## 8. Validation

Run `pnpm check` from `web/` to verify Oxfmt, Oxlint, TypeScript, and the Vite
production build (prefer `pnpm` over `npm`). For behavior that touches workspace
sync, canvas interactions, inspect/materialize, or Monaco, run the live e2e
suite: `corepack pnpm test:e2e:live` in `web/`.
