# Contributing to the Renart docs

These docs follow a fixed authoring contract. The full version lives in
[`architecture/docs.md`](../architecture/docs.md);
the companion [`plans/user-docs-rollout.md`](../plans/user-docs-rollout.md)
holds the information architecture and rollout. This page is the short version.

## The rules

1. **Assume zero Bruin knowledge.** Teach Renart's own model in Renart's own words.
   Mention Bruin only where it's load-bearing, and link out instead of teaching it.
2. **One page, one mode** (Diátaxis): tutorial / how-to / reference / explanation.
   Never mix them — split instead.
3. **Use the UI's exact words.** Match the product's labels and capitalisation
   (Build, Catalog, Inspect, Materialize, asset, connection, environment,
   workbench). First use of a term links to the glossary (Concepts).
4. **How-tos are task-titled** ("Add a manual dependency") and end in a verifiable
   result.
5. **Docs vs. app:** docs own conceptual + multi-step content; the app owns
   field-level hints (a single tooltip is not a docs page).

## Page templates

Use the skeleton for your mode from the framework doc (§4): how-to, tutorial,
reference, explanation. Every page has `title` + `description` frontmatter and a
single H1.

## Screenshots

Hand-curated, against the docs demo project (`docs/demo-project`, the `acme_shop`
pipeline), in the default light theme, cropped to the relevant surface, with alt
text. Annotate only when it adds clarity. Keep a note of which pipeline/view
produced the shot so it's reproducible. Store under `src/assets/docs/<area>/`. See
framework doc §5.

## The docs gate

A user-facing change ships with its docs. Tick the PR's "docs touched?" box (or mark
it N/A with a reason). Reviewers treat a missing doc like a missing test.
