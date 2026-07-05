# Docs overhaul: minimal real set

Status: proposed (see questions.md #9 for the page-count veto)

Supersedes the *content* phasing of plans/user-docs-rollout.md: the IA stays,
but instead of 46 mostly-mocked pages we ship ~15 real ones and delete the
rest (git keeps them; re-add as features stabilize).

## Keep (rewrite with real content, no bruin focus)

1. `index` (docs landing)
2. `installation`
3. `quickstart` — install → demo project → first materialize → first schedule
4. `concepts` — project/pipeline/asset/materialization/staleness/notebook,
   filesystem+git as source of truth
5. `workspace/interface-tour` — shell, explorer, canvas, editor, results
6. `workspace/pipeline-canvas`
7. `workspace/runs-and-history`
8. `editing-assets/asset-editor` (folds in columns + materialization +
   quality-checks basics; per-field reference lives in `reference/…`)
9. `asset-types/sql-assets`
10. `asset-types/python-assets`
11. `asset-types/http-api-assets`
12. `notebooks/overview` (folds working-in-a-notebook + promoting-cells)
13. `scheduling/overview` (folds creating-schedules)
14. `connections-environments/managing-connections` (folds environments)
15. `reference/asset-file-format` + `reference/cli` (real flags from
    `renart --help`)

Delete the remaining stubs; prune `astro.config.mjs` sidebar accordingly.

## Bruin policy in docs

- "asset", "pipeline", "project config" — never "bruin asset" / ".bruin.yml
  workflow" in prose. The literal filename `.bruin.yml` may appear in the
  configuration reference only, introduced as "the project config file".
- `bruin-cli-and-renart.mdx` is the one page allowed to discuss bruin
  interop; keep it short, link from concepts.

## Screenshots

`web/scripts/capture-onboarding-screenshots.mjs` + tutorial renderer already
exist; extend the capture script with a stable scratch workspace so docs
screenshots regenerate deterministically (`pnpm docs:screenshots`).

## Verification

- `docs`: astro build green; no page links to a deleted stub; grep -i bruin
  over docs content returns only the interop page + config reference.
