# Contributing to Renart

Thanks for helping improve Renart. The project is in public alpha, so focused
bug reports and small, well-tested changes are especially useful.

## Before opening a change

1. Search the existing issues and plans for overlapping work.
2. Open an issue before a large behavior or architecture change.
3. Keep changes scoped and preserve the filesystem-as-source-of-truth model.
4. Do not include credentials, workspace state databases, or user data.

## Development

Use `make dev` to run the Go backend and Vite frontend together. On Windows,
use `./make.ps1 dev` from PowerShell instead. See
[`AGENTS.md`](AGENTS.md), [`architecture/`](architecture/), and the relevant
document under [`plans/`](plans/) before changing a subsystem.

Run the checks appropriate to your change:

```bash
go test ./...
go vet ./...
(cd web && corepack pnpm check)
(cd docs && corepack pnpm build)
```

Changes to workspace sync, the canvas, Monaco, inspect, or materialization
should also run the live Playwright suite documented in `AGENTS.md`.

## Pull requests

- Explain the user-visible outcome and any important trade-offs.
- Add or update tests for changed behavior.
- Update user docs for shipped user-facing behavior and architecture docs for
  as-built subsystem changes.
- Keep generated files and lockfiles in the same commit as their source change.
- Confirm that you have the right to submit the contribution under Apache-2.0.

Security reports belong in the private channel described in
[`SECURITY.md`](SECURITY.md), not a public issue.
