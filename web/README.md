# Bruin Web

Small guide for running and using the Bruin Web UI locally.

## Prerequisites

- `bruin` CLI built and available from the repo root
- Node.js + npm/pnpm installed

## Quick start

From the repository root:

1. Build frontend assets:

```bash
cd web
pnpm install
pnpm build
cd ..
```

2. Start the Bruin Web server:

```bash
./bruin web --port 3000 --host 127.0.0.1
```

3. Open:

`http://127.0.0.1:3000`

## Run against a different workspace

Pass a workspace path as the final argument:

```bash
./bruin web --port 3000 --host 127.0.0.1 /path/to/your/workspace
```

## Common workflow in UI

- Select a pipeline and asset from the sidebar.
- Edit SQL/Python in the editor pane.
- Use **Materialize** to run an asset and **Inspect Data** to preview results.
- Add or modify visualization settings in the **Visualization** tab.
- The UI auto-refreshes as files change (watch mode).

## Useful flags

- `--static-dir web/dist` to serve frontend files from a custom path
- `--host 0.0.0.0` to access from other machines

## Frontend-only development (optional)

If you only work on the UI shell itself:

```bash
cd web
pnpm dev
```

This runs Vite dev server for frontend development.
