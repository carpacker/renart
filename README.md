# Renart

Standalone extraction target for the Renart backend and React frontend.

This directory is intended to become its own repository.

Current shape:

- Go web server code copied from Renart-specific sources
- React frontend copied from `web/`
- Core Bruin functionality expected to come from the published Go module `github.com/bruin-data/bruin`

Current entrypoint:

```bash
go run . web
```

Documentation site scaffold:

```bash
cd docs
corepack pnpm install
corepack pnpm dev
```

Installer script:

```bash
curl -LsSf getrenart.com/install.sh | sh
```

The checked-in installer is generated from:

```bash
PATH="$HOME/go/bin:$PATH" binst init --source=goreleaser --file=.goreleaser.yaml -o .config/binstaller.yml
PATH="$HOME/go/bin:$PATH" binst gen --config=.config/binstaller.yml -o install.sh
```

The docs are intended to be short user-facing product documentation for people using Renart in Bruin projects, not just implementation notes for developers working on Renart itself.

CLI-backed operations are executed through the real `bruin` binary by default.

Direct execution is preferred when Renart can reuse shared Bruin Go package logic while preserving compatibility with the real CLI. When parity is uncertain, Renart explicitly falls back to the real `bruin` binary instead of guessing.

If needed, you can override that path when starting Renart:

```bash
go run . web --bruin-binary /path/to/bruin
```

## Current Executor Status

Direct executor paths currently cover:

- `QueryAsset`
- `QueryConnection`
- `ImportDatabase`
- `ApplyPatch`
  - `fill-asset-dependencies`
  - `fill-columns-from-db`
- `FormatAsset` for non-`sqlfluff`
- `RunAsset` for conservative simple SQL backends:
  - `duckdb.sql`
  - `motherduck.sql`
  - `pg.sql`
  - `bq.sql`
  - `athena.sql`
  - `databricks.sql`
  - `fabric.sql`
  - `fw.sql`
  - `my.sql`
  - `sf.sql`
  - `ms.sql`
  - `clickhouse.sql`
  - `trino.sql`
  - `vertica.sql`
- `RunPipeline` for conservative whole-pipeline runs across that same SQL backend set
- direct column checks for the same conservative SQL backend set
- direct custom checks for the same conservative SQL backend set
- direct metadata push for the subset of supported backends with a shared Bruin metadata operator:
  - `pg.sql`
  - `bq.sql`
  - `sf.sql`

CLI-backed paths remain in place for:

- `FormatAsset` with `sqlfluff`
- unsupported or higher-risk run paths
- metadata push outside the explicitly supported direct subset
- Oracle run paths

## Compatibility Rules

- The real `bruin` CLI is the compatibility oracle.
- Direct paths should reuse shared Bruin package logic instead of reimplementing CLI behavior from scratch.
- If parity cannot be shown confidently, Renart should fall back to the real CLI.
- New direct paths should add focused compatibility tests and keep Renart live E2E green.
- CLI fallbacks launched from Renart disable telemetry with `TELEMETRY_OPTOUT=1`.

Likely follow-up after splitting into a fresh repo:

1. Reduce the copied Go surface further if desired.
2. Run `go mod tidy` in the new repo.
3. Decide whether to keep or drop copied tests.
4. Continue expanding direct execution only where compatibility can be demonstrated.
5. Update frontend package naming and live test wiring.
