# Plans

Ephemeral design documents: proposals, evaluations, and implementation plans
for work that has **not shipped** (or only partially). The current state of
what *is* built lives in [`../architecture/`](../architecture/).

When a plan is implemented, fold the as-built reality (including deviations)
into the relevant `architecture/` doc and delete the plan — git history keeps
the original.

| Doc | Status |
| --- | --- |
| [cli-v1.md](cli-v1.md) | proposed — a clean CLI surface + `renart run`, standalone and inside a web-UI terminal |
| [dbt-assets.md](dbt-assets.md) | evaluation — enabling renart intelligence on existing dbt projects |
| [materialization-strategies.md](materialization-strategies.md) | in progress — Phase 1 partially landed |
| [materialization-per-asset-type.md](materialization-per-asset-type.md) | proposed — make every offered materialization mode execute on sql/python/load/api assets |
| [remote-table-intellisense.md](remote-table-intellisense.md) | proposal — surface warehouse tables with no backing asset via the LSP |
| [questions.md](questions.md) | open questions for the maintainer |

Recently folded away (git history keeps them): `docs-alpha.md` and
`landing-page.md` → `architecture/docs.md`; `notebook-intellisense.md` →
`architecture/sql-lsp.md`; `ingestr-feature-flag.md` and
`project-settings-and-workspaces.md` → `architecture/backend.md` +
`architecture/frontend.md`.
