# Architecture docs

This directory describes the **current state** of renart's architecture — what
is built and how it works, not how we got there. When a design lands, its plan
gets folded into (or replaced by) a current-state doc here.

Ephemeral design documents — proposals, evaluations, implementation plans for
work that has not shipped — live in [`../plans/`](../plans/). A plan graduates
into this directory (usually merged into an existing doc) once it is
implemented; deviations discovered during implementation are recorded here, not
in the plan.

## Contents

| Doc | Covers |
| --- | --- |
| [backend.md](backend.md) | Go backend: layering, runtime model, execution, conventions |
| [frontend.md](frontend.md) | Web app: stack, routing, app shell, hooks, libraries, layout rules |
| [staleness.md](staleness.md) | Fingerprints, materialization facts/coverage, staleness service, deploy snapshots, per-env schedules, protected environments |
| [notebooks.md](notebooks.md) | Notebook folder format, sessions, rename engine, `@viz`, server-driven auto-recompute, promotion |
| [asset-editing.md](asset-editing.md) | Asset workbench: ownership model, `assetmeta` provenance keys, reconciliation, transaction API |
| [sql-lsp.md](sql-lsp.md) | SQL language server: canonical graph, engine, web service caching, notebook cell scoping, column-inference fixpoint |
| [docs-framework.md](docs-framework.md) | Authoring contract for the user docs under `docs/` |

Agent orientation and repo-wide rules live in the top-level [`AGENTS.md`](../AGENTS.md);
the user-facing product docs live under `docs/` (see docs-framework.md for how
those are written).
