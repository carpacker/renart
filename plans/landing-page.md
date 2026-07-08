# Landing page overhaul — proposal for iteration

Status: awaiting feedback (questions.md #1–#3) — an implementation is already
committed at `71afe30`; this doc is the story I propose to keep/sharpen.

## The story (one sentence)

Renart is the all-in-one, git-native data pipeline IDE: write, run, schedule,
and trust your pipelines — one local binary, your files, no platform.

## Arc (as committed, proposed to keep)

1. **Hero** — headline + live canvas round-trip video
   (`hero-canvas-roundtrip.webm`). CTA: install one-liner + quickstart link.
2. **One tool, the whole lifecycle** — four scenes, each = screenshot + copy:
   - *Write* — SQL editor that knows your pipeline (asset-aware
     intellisense, jump-to-definition, type checking),
   - *Explore* — notebooks with per-notebook DuckDB sessions, promote cells
     into the pipeline,
   - *Ship* — from first run to production cron: runs page, schedules,
     env-aware execution,
   - *Trust* — staleness tracking: fingerprints know what's fresh, rebuild
     only what isn't.
3. **The parts you'd otherwise bolt on** — run history, lineage/docs by
   default, reviewable diffs (git-native), quality checks with the asset.
4. **Manifesto strip** — why local-first + git beats a SaaS control plane.
5. **Principles** — one binary, your machine, your files.
6. **Final CTA** — install → scheduled pipeline in one sitting.

## Proposed changes on top of 71afe30

- Regenerate all media with the current UI (recolored icon, dark-mode fixes,
  truncated sidebar) via `pnpm landing:media`; add a staleness-badge close-up
  shot for the *Trust* scene (currently text-only).
- Copy pass: tighten hero subline to name the three pillars explicitly
  (IDE · scheduler · freshness tracking); remove any remaining bruin
  references except a single "works with existing bruin projects" nod in the
  FAQ/footer if desired.
- OG image refresh with the new icon color.

## Blocked on

The maintainer's answers to questions.md #1–#3 (direction, media regen, "stainless").
