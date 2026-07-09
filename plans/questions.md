# Open questions

Shared scratchpad for things that need the maintainer's input. Tasks with open
questions are skipped (per instruction), everything else proceeds.
Answer inline and delete entries as they resolve.

## Landing page (iterate before implementing)

A full overhaul is already committed at `71afe30` (earlier today): story arc is
hero → "one tool, the whole lifecycle" (editor intelligence → notebooks →
scheduling → staleness) → "the parts you'd otherwise bolt on" (runs/lineage/
diffs/quality) → manifesto → local-first principles → CTA. Questions before I
touch it further:

1. Is the committed version the direction you want, or should I restructure?
   My reading of your brief ("all-in-one data pipeline IDE, scheduler, the
   staleness tracking") matches its lifecycle section — I'd keep the structure
   and sharpen copy + regenerate media.
2. Screenshots: `web/scripts/capture-landing-media.mjs` exists and captures
   hero video + feature shots from a live app. OK to regenerate all media with
   the current UI (post icon-recolor, post dark-mode fixes) and swap them in?
3. "the stainless tracking etc." — I read this as *staleness* tracking
   (fingerprint-based freshness). Correct?

## Onboarding / new-project flows

4. "there's already a concept design available" — I could not find an
   onboarding concept in the repo (no mockup file, no plan doc; the existing
   `/onboarding` routes are the old connection-first flow). Where does the
   concept live (Figma link, HTML file, branch)?
   → Proceeding meanwhile with plans/onboarding-projects.md based on your
   three-option brief; the visual port can follow once the concept surfaces.
5. Demo projects: is the chess-based quickstart (players/games/player_stats)
   the model for "demo projects", and how many do you want (I'd do 2:
   analytics-on-API demo + a pure-DuckDB/CSV demo)?

## Ingestr feature flag

6. Where should the flag live? Options: (a) `.renart/project.yml` setting
   surfaced in Project settings (per-project, my preference — bruin imports
   are per-project), (b) env var / CLI flag, (c) localStorage-only UI toggle.
   → Proceeding with (a) + auto-enable when the pipeline already contains
   ingestr assets, per your "unless your pipeline already contains ingestr
   assets".

## Docs

9. The docs IA from plans/user-docs-rollout.md has 46 pages; you asked for a
   "minimal set spanning the main features". I'll collapse to ~15 real pages
   (see plans/docs-overhaul.md) and delete the rest rather than leave mocks.
   Veto if you want the full IA kept as stubs.
