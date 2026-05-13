# Renart

Renart is an IDE for git-native data pipelines.

It gives Bruin projects a fast visual editing environment for assets, dependencies, lineage, SQL inspection, and materialization while keeping the filesystem and Git as the source of truth.

## Who Renart Is For

Renart is for data engineers, analytics engineers, and technical data users who want to understand and change real pipeline structures without losing the benefits of version-controlled project files.

Use Renart when you want:

- a canvas-first view of assets, dependencies, and data flow
- editor workflows that still write normal Bruin project files
- safe inspect flows for previewing SQL results
- explicit materialization flows for running assets
- a Git-friendly alternative to a generic pipeline dashboard

## Install

```bash
curl -LsSf getrenart.com/install.sh | sh
```

Then start Renart inside a Git repository that contains, or will contain, a Bruin project:

```bash
renart web
```

Renart starts on `127.0.0.1:8080` by default. If that port is unavailable, it picks the next available fallback port and prints the URL.

## Documentation

The documentation site lives in `docs/`.

```bash
make docs-dev
```

The production docs build is fully static and can be served by Caddy:

```bash
make docs-docker
make docs-docker-run
```

## Development

Renart is a Go server plus a static React frontend embedded into the binary.

- Backend: Go HTTP server
- Frontend: React, TypeScript, Vite, TanStack Router, Tailwind CSS, Monaco, React Flow
- Docs: Astro and Starlight
- Runtime sync: filesystem watcher plus Server-Sent Events

Useful targets:

```bash
make help
make build
make check
make web-build
make docs-build
make landing-media
make docs-screenshots
```

Direct command equivalents:

```bash
/usr/local/go/bin/go build .
corepack pnpm --dir web build
corepack pnpm --dir docs build
```

## Architecture Principles

- The filesystem is authoritative.
- Git-backed project workspaces are the normal operating model.
- Frontend state exists for responsiveness, not persistence.
- File-changing actions go through the Go server APIs.
- Workspace changes reconcile through Server-Sent Events, not polling.
- Bruin compatibility matters more than clever internal shortcuts.

## Contributing

Contributions should preserve Renart's positioning as an IDE for git-native data pipelines: visual and direct, but still grounded in real files, real Bruin project semantics, and reviewable Git diffs.

Before opening a PR, run the relevant checks:

```bash
make check
```

For frontend changes, also consider live flow coverage when the behavior touches workspace sync, canvas interactions, inspect/materialize, or Monaco editor behavior:

```bash
make web-test-live
```

## License

Renart is licensed under Apache License 2.0. See `LICENSE`.
