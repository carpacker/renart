# Landing page overhaul

Status: **implemented** (July 2026, `redesign` branch) — full visual redesign of
`docs/src/pages/index.astro` plus all-new media. This doc records the story the
page tells and how the media was produced, for the next regeneration.

## The story (one sentence)

Renart is the all-in-one, git-native data pipeline IDE: write, run, schedule,
and trust your pipelines — one local binary, your files, no platform.

## Arc (as shipped)

1. **Hero** — serif headline ("The all-in-one data pipeline IDE") + subline
   naming the pillars (editor · scheduler · freshness monitor, notebooks and
   lineage built in). CTAs: Get started, docs, install one-liner.
   Media: `hero-workspace.webp` (editor + canvas + workbench, staging.orders).
2. **Marquee** — "runs on the stack you already have" logo strip.
3. **The lifecycle** — four alternating screenshot+copy rows:
   - *01 Build* — SQL editor grounded in the DAG (intellisense over upstream
     columns), `lifecycle-build.webp`;
   - *02 Explore* — notebook with table + charts, cell-actions menu open on
     "Promote to pipeline", `lifecycle-notebook.webp`;
   - *03 Run* — schedules page with the New-schedule dialog open (pipeline,
     environment, cron, timezone, catch-up, deploy toggle),
     `lifecycle-schedules.webp`;
   - *04 Trust* — canvas with all four staleness badges (Fresh / Edited /
     Upstream changed / Never built), `lifecycle-staleness.webp`.
4. **Bento** — runs & history (failed-run detail with the SQL error in the
   event log, `feature-runs.webp`), catalog with lineage highlight
   (`feature-catalog.webp`), git-native diffs, quality checks.
5. **Manifesto strip** — "your data stack should not be five tools duct-taped
   together."
6. **Principles** — one binary, your machine, your files (checklist grid).
7. **Final CTA** — install → scheduled pipeline in one sitting. Footer.

## How the media was made

- `make landing-media` regenerates everything. It runs
  `web/scripts/capture-landing-media.mjs` (workspace content in
  `web/scripts/landing-media-workspace.mjs`), which builds the demo state
  end-to-end and writes the webp files + og-image into
  `docs/public/landing/`.
- The demo state: a 10-asset `acme` pipeline (raw → staging → mart), an
  hourly `marketing` pipeline, seeded DuckDB data, recorded runs (including
  one failed scheduled run with a binder error), env schedules on default +
  production, a run notebook with @viz charts, and a staged staleness mix
  applied last (edited staging asset → stale marts + one never-built asset).
- Captured with Playwright at deviceScaleFactor 2, dark theme, viewport
  1400×900 (14:9); hero at 1920×1080, bento shots at 1200×675, the schedules
  dialog shot at 1176×756 so the dialog fills the frame. Converted to webp
  q92 via sharp (from docs/). OG image is a 1200×675 PNG.
- If a capture changes dimensions, update the matching `<img>`
  width/height in `docs/src/pages/index.astro`.

## Deliberately left out (for now)

- Onboarding/welcome flow — still being built on
  `worktree-onboarding-projects`; fold it in once it lands.
