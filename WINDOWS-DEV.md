# Developing Renart on Windows

Upstream's tooling is bash + GNU make, which doesn't run natively on Windows.
`make.ps1` in the repo root is a PowerShell replacement for the targets that
matter. This document covers the setup, the fork workflow, and where things
live in the codebase.

---

## 1. One-time setup

### Prerequisites

CI pins these versions; mismatches cause confusing failures.

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26.5 | https://go.dev/dl/ |
| Node | 22.22.1 | https://nodejs.org/ |
| pnpm | 10.33.0 | via `corepack enable` — the repo pins it in `package.json` |
| mingw-w64 gcc | any recent | **required** — `renart.exe` needs cgo |
| WebView2 | any | ships with Windows 11; needed by `renart-gui.exe` |

### The C compiler is not optional

`internal/web/adbcutil/cancel.go` does `import "C"`, and the server reaches it
via `main → cmd → clientapi → service → duckdbsession → adbcutil`. So every
build of the server needs `CGO_ENABLED=1` and a working gcc. Building with
`CGO_ENABLED=0` fails with:

```
imports renart/internal/web/adbcutil: build constraints exclude all Go files
```

That message is misleading — there are no build tags in that package. The
excluding constraint is the implicit `cgo` one.

```powershell
winget install --id=MSYS2.MSYS2 -e
C:\msys64\usr\bin\pacman.exe -S --noconfirm mingw-w64-ucrt-x86_64-gcc
```

Then add `C:\msys64\ucrt64\bin` to PATH and open a new terminal.

Only `renart-gui.exe` builds without cgo — `scripts/build_standalone_helper.sh`
sets `CGO_ENABLED=0` for its Windows branch, and `make.ps1` mirrors that.

`make.ps1 go-build` also passes `-tags no_duckdb_arrow -buildmode=exe`, matching
the `renart-duckdb-windows-amd64` target in `.goreleaser.yaml` — the only
Windows build configuration upstream actually ships. If you ever hit missing
`KnownFolders.h` / `ShlObj.h` from an older mingw, the release build works
around it with `CGO_CPPFLAGS=-I<repo>/scripts/mingw-compat`.

Check everything at once:

```powershell
.\make.ps1 doctor
```

### Build

```powershell
.\make.ps1 build
```

That runs, in order: `pnpm install` → `pnpm build` (frontend → `web/dist`) →
`go build` for `renart.exe` → `go build` for `renart-gui.exe`. Then:

```powershell
.\renart.exe          # opens the native window, using the current dir as workspace
```

### The two failure modes that bite first

**"Renart dev backend / the UI is not embedded."**
Your `renart.exe` was built with `-tags webdev`. That tag deliberately skips
embedding `web/dist` so Vite can serve the frontend with hot reload
(`web/embed_dev.go` vs `web/embed.go`). A `webdev` binary serves the API only.
Rebuild with `.\make.ps1 go-build`, or use `.\make.ps1 dev` and open port 5173.

**"Renart GUI is unavailable… renart-gui helper binary was not found."**
`renart-gui.exe` isn't next to `renart.exe`. Build it with `.\make.ps1 gui`,
or point `RENART_GUI_BINARY` / `--gui-binary` at one. Without it Renart falls
back to serving the UI in your browser, which still works.

**"This project is configured to use 10.33.0 of pnpm. Your current pnpm is v11.x"**
The repo has **no `package.json` at its root** — only `web/`, `docs/`, and
`extensions/vscode/` have one. Corepack resolves the pnpm version from the
`packageManager` field of the *current directory's* package.json, so
`corepack pnpm --dir web install` run from the repo root finds nothing, falls
back to whatever pnpm corepack has globally activated, and then pnpm reads
`web/package.json`, sees the 10.33.0 pin, and refuses.

The fix is to invoke corepack from inside the package directory, which
`make.ps1` now does:

```powershell
Push-Location web; corepack pnpm install; Pop-Location
```

Don't work around this with `--pm-on-fail=ignore`. pnpm 11 ignores the
`pnpm.overrides` block in `web/package.json` (those are pinned security
overrides) and may rewrite `pnpm-lock.yaml` to a newer lockfile format, which
would put a large unnecessary diff between you and upstream.

Also: PowerShell doesn't search the current directory, so it's `.\renart.exe`,
not `renart`.

---

## 2. Daily loop

```powershell
.\make.ps1 dev                       # workspace defaults to example/example
.\make.ps1 dev path\to\your\repo     # or point it at a real project
```

`example/` is gitignored upstream — it's a scratch workspace each developer
creates locally, not something the clone ships. On first run `make.ps1 dev`
bootstraps it as an empty git repo; use Renart's welcome screen to seed it with
the demo project. Renart always needs a git repo as its workspace.

This opens two windows:

- **Backend** — `air` rebuilds and restarts the Go server on any `.go` change,
  on `127.0.0.1:3000`, built with `-tags webdev`.
- **Frontend** — Vite with real HMR on `127.0.0.1:5173`, proxying `/api` to the
  backend.

**Open http://127.0.0.1:5173.** Port 3000 is the API only and will show you the
dev-backend placeholder page. Edit `.go` → backend restarts; edit anything under
`web/` → the page hot-updates without losing state.

Before committing anything non-trivial:

```powershell
.\make.ps1 check     # go vet + go test + frontend format/lint/typecheck/build
```

### A Windows detail worth knowing

The Makefile builds a small C shim (`scripts/build_bruin_sqlparser_stub.sh`)
because Bruin's `sqlparser` package carries an unconditional native linker flag
on Linux and macOS. Windows builds run with `CGO_ENABLED=0`, so the shim is
irrelevant here — `make.ps1` skips it. If you ever build under WSL, you'll need
it.

---

## 3. Fork workflow

```
origin    https://github.com/carpacker/renart.git   (yours)
upstream  https://github.com/renart-data/renart.git (fetch only; push disabled)
```

Keep `main` as a clean mirror of upstream and do your work on branches. That way
pulling in his improvements stays a fast-forward instead of a merge fight.

```powershell
# refresh main from upstream
git checkout main
git fetch upstream
git merge --ff-only upstream/main
git push origin main

# start a feature
git checkout -b carson/my-feature

# pull upstream into your feature branch later
git fetch upstream
git rebase upstream/main
```

If `git merge --ff-only` refuses, you've committed directly to `main` — move
those commits to a branch and reset `main` back to `upstream/main`.

### Line endings — don't undo this

The repo is developed on Linux/macOS. A Windows checkout had rewritten all 1,258
tracked files to CRLF, which made every commit a whole-file diff and would have
broken the 260 Go golden-file tests that compare exact output.

`.gitattributes` now forces `eol=lf` in the working tree on every platform,
regardless of your global `core.autocrlf`. **Leave it in place.** If you ever see
`git status` report hundreds of modified files with no real changes:

```powershell
git diff --ignore-cr-at-eol --stat    # confirm it's ONLY line endings (empty output = safe)
git reset --hard HEAD                 # rewrite the tree with correct endings
```

---

## 4. Codebase map

### Backend — `internal/web/`

Layered `httpapi` (transport) → `service` (domain) → Bruin (execution).

| Path | Role |
|---|---|
| `internal/web/httpapi/` | One file per domain (`assets.go`, `pipeline.go`, `run.go`, …). Decode → delegate → encode. No logic. |
| `internal/web/service/` | The actual domain logic. Large flat package by design. Tests co-located. |
| `internal/web/model/dto.go` | Canonical DTOs — **the frontend contract** |
| `internal/web/scheduler/` | River + SQLite job scheduler |
| `internal/web/events/` | SSE pub/sub hub → `/api/events` |
| `internal/web/watch/` | fsnotify watcher → workspace re-parse → SSE broadcast |
| `internal/web/sqlintelligence/` | Dependency extraction via embedded Polyglot WASM |
| `internal/sqllsp/` | SQL language-server core |
| `cmd/` | urfave/cli entrypoint and route wiring (`cmd/web.go`) |

**The filesystem is authoritative.** Nothing bypasses the Go server; all writes
go through `WorkspaceResolver.SafeJoin`. Local state (SQLite, locks) lives in
`.renart/` in the workspace, which is gitignored.

### Frontend — `web/`

| Path | Role |
|---|---|
| `web/src/routes/_shell/` | File-based TanStack routes: `pipelines/$pipelineId`, `catalog`, `notebooks`, `runs`, `schedules` |
| `web/components/app/build-page.tsx` | The primary IDE surface |
| `web/components/app/lineage-canvas.tsx` | React Flow DAG canvas |
| `web/components/app/asset-editor.tsx` | Monaco editor + guided metadata cards |
| `web/lib/atoms/` | Jotai state, split by domain |
| `web/hooks/use-workspace-sync.ts` | The single SSE connection + reconciliation |
| `web/lib/api-*.ts` | One API client per backend domain |

Jotai is never persistent truth — backend reconciliation over SSE always wins.

### The backend↔frontend contract

TypeScript types are **generated from Go structs**, not from a schema.
`web/scripts/generate-api-types.mjs` parses named structs out of
`internal/web/model/dto.go` (and a few others listed in the script) into
`web/lib/generated/api-types.ts`.

When you change a Go DTO, run `pnpm --dir web generate:api-types`. When you add
a *new* struct the frontend needs, add its name to the `sources[].types` array
in that script first.

**Never hand-edit** `web/lib/generated/api-types.ts` or
`web/src/routeTree.gen.ts` — both are build outputs.

### Adding a feature: the path through the stack

Adding an endpoint plus UI touches, in order:

1. `internal/web/model/dto.go` — the DTO
2. `internal/web/service/<domain>.go` (+ `_test.go`) — the logic
3. `internal/web/httpapi/<domain>.go` — the handler; wire the route in `cmd/web.go`
4. `web/scripts/generate-api-types.mjs` — register new types, then regenerate
5. `web/lib/api-<domain>.ts` — the client function
6. `web/components/app/` — the panel, wired into `build-page.tsx`
7. `web/lib/atoms/` — shared state, if needed

A new **page** is a file under `web/src/routes/_shell/` plus a nav entry in
`web/components/app/app-data.ts`.

A new **asset type** is deeper, because asset kinds are largely Bruin concepts.
The Renart-side surface is `service/asset_type.go`, `asset_capabilities.go`,
`asset_creation_profile.go`, a `direct_*_operator.go` registered in
`direct_executor_registry.go`, and the creation dialog in
`web/components/app/build-create-dialogs.tsx`. Read
`architecture/http-api-assets.md` first — it documents the most recent example
of adding one.

### Constraints from `AGENTS.md`

These are stated non-negotiables upstream; worth keeping unless you're
deliberately diverging.

- Workspace sync is **SSE only** — no polling.
- **No Node.js production runtime.** All filesystem writes go through Go.
- **Inspect stays read-only** (a single `SELECT`). Materialization is the only
  side-effecting path.
- Don't wire anything to Bruin's Rust SQL parser — Renart uses embedded Polyglot
  WASM instead, and the CGo stub is deliberately fail-closed.
- `third_party/` is vendored/embedded artifacts, not hand-editable code.
- Secrets in `.renart/secrets.yml` are typed references, never plaintext.
  Sensitivity is reflected from Go struct tags — don't add frontend heuristics.

### Where the docs are

- `architecture/` — current state. Read the relevant file before non-trivial changes.
- `plans/` — in-flight proposals, explicitly not user-facing yet.
- `docs/` — the Astro/Starlight user docs. Only describes what already ships.

### Tests

```powershell
go test -p=1 ./...              # Go, co-located *_test.go
pnpm --dir web test:unit        # vitest, co-located *.test.ts under web/lib/
pnpm --dir web test:e2e         # Playwright, web/tests/e2e/, no live backend
pnpm --dir web test:e2e:live    # Playwright against a real server (*.live.spec.ts)
```

Run the live e2e suite when you touch workspace sync, the canvas, inspect,
materialize, or Monaco — those flows aren't covered by the offline tier.
