# Plans

Ephemeral design documents: proposals, evaluations, and implementation plans
for work that has **not shipped** (or only partially). The current state of
what *is* built lives in [`../architecture/`](../architecture/).

When a plan is implemented, fold the as-built reality (including deviations)
into the relevant `architecture/` doc and delete the plan — git history keeps
the original.

| Doc | Status |
| --- | --- |
| [cli-v1.md](cli-v1.md) | proposed — renart CLI usable standalone and inside a web-UI terminal |
| [project-settings-and-workspaces.md](project-settings-and-workspaces.md) | proposed — real project settings + multi-project switching |
| [dbt-assets.md](dbt-assets.md) | evaluation — enabling renart intelligence on existing dbt projects |
| [materialization-strategies.md](materialization-strategies.md) | in progress — Phase 1 partially landed |
| [materialization-per-asset-type.md](materialization-per-asset-type.md) | proposed — make every offered materialization mode execute on sql/python/sling/api assets |
| [user-docs-rollout.md](user-docs-rollout.md) | active — docs IA + phased rollout (contract: `architecture/docs-framework.md`) |
| [notebook-intellisense.md](notebook-intellisense.md) | in progress — asset/cell completion + go-to-definition in notebooks |
| [redesign-to-root.md](redesign-to-root.md) | in progress — promote /redesign to / and delete the old UI |
| [ingestr-feature-flag.md](ingestr-feature-flag.md) | in progress — hide ingestr surfaces behind a project flag |
| [onboarding-projects.md](onboarding-projects.md) | proposed — first-run onboarding + demo/import/empty project creation |
| [docs-overhaul.md](docs-overhaul.md) | proposed — collapse mocked docs to ~15 real pages |
| [http-api-assets.md](http-api-assets.md) | proposed — OpenAPI infer/validate, records_path intellisense, dlt-parity sources |
| [landing-page.md](landing-page.md) | awaiting feedback — landing story + media regen proposal |
| [questions.md](questions.md) | open questions for Lukas |
