# Renart Docs Framework — How We Write the User Docs

Status: active. This is the authoring contract for everything under `docs/`
(Astro Starlight, served at getrenart.com/docs). The companion
`renart-user-docs-concept.md` defines *what* to build (the information
architecture and rollout); this defines *how* to write it. If you are writing or
reviewing a docs page, this is the checklist.

The locked decisions (§0) are the non-negotiable part. The rest is craft guidance.

---

## 0. Locked decisions

These were decided with the user and govern every page. Don't re-litigate them in
a PR; change them here first if they need to change.

1. **Audience: assume zero Bruin knowledge.** The reader is a data/analytics
   engineer who has never heard of Bruin. We teach **Renart's own model** in
   Renart's own words. Minimise Bruin overall: don't make the reader learn Bruin to
   use Renart. Mention Bruin only where it's load-bearing (the file-compatibility
   promise, the CLI interop, credential shapes we genuinely defer on) and link out
   instead of teaching it. "Renart is built on Bruin" is a footnote, not a prologue.
2. **Docs vs. in-app help — the line.** Docs own **conceptual and multi-step**
   content (what a thing is, why it exists, how to complete a task end to end). The
   **app** owns **field-level hints** (a tooltip on one input, an empty-state
   nudge, an inline validation message). If you're tempted to document a single
   field's meaning in prose, that's an in-app tooltip, not a docs page. If you're
   tempted to put a five-step flow in a tooltip, that's a how-to.
3. **Screenshots are hand-curated.** No auto-capture harness. Each image is
   deliberately composed against the **docs demo project** (`docs/demo-project`, the
   `acme_shop` pipeline — a purpose-built, coherent example, not anyone's personal
   playground), cropped, and (where useful) annotated. Quality over coverage — a few
   excellent shots beat many mechanical ones. See §5.
4. **Versioning: unversioned for now.** Docs track `main`. No Starlight versioning
   yet. Revisit when releases stabilise (the trigger lives in the concept doc, not
   here).
5. **Ownership: a "docs touched?" gate.** A user-facing change ships with its docs.
   PRs carry a docs checkbox (§7); reviewers enforce it.

## 1. The four modes (Diátaxis)

Every page is exactly one of these. A page that mixes modes gets split. When you
don't know where content goes, you've usually got two pages fused together.

| Mode | The reader wants to… | Shape | Renart examples |
| --- | --- | --- | --- |
| **Tutorial** | learn by doing, build confidence | one guided happy path, every step verifiable, no choices | Quickstart; "Build your first pipeline" |
| **How-to** | get a specific task done now | goal → steps → result, assumes context | "Materialize a pipeline", "Load a CSV with a Load asset" |
| **Reference** | confirm a fact fast | terse, table-first, no narrative | CLI flags, asset file keys, shortcuts |
| **Explanation** | understand why/how it fits | prose, diagrams, no steps | Concepts; "How Renart stores everything as files" |

Litmus test before writing: **"Is the reader learning, doing, looking up, or
understanding?"** That answer is the page type. Put it in the frontmatter intent
(see §6) and don't drift.

## 2. Voice & tone

- **Second person, present tense, active voice.** "Click **Materialize**." Not
  "The user should click" or "Materialization will be triggered."
- **Lead with the reader's goal, not our architecture.** Open with what they get,
  not how it's built. The "why it's interesting to us" belongs in Explanation pages,
  and even there, kept short.
- **Concise.** Cut hedging ("simply", "just", "of course"), throat-clearing intros,
  and restating the heading. If a sentence doesn't change what the reader does or
  understands, delete it.
- **Concrete over abstract.** Name the real button, the real file, the real menu.
  Use the example project's real asset names.
- **No Bruin tax.** Never require the reader to know a Bruin term to follow a
  sentence. If a Bruin word is unavoidable, define it in one clause and move on.
- **Confident, not salesy.** State what Renart does. Skip adjectives like
  "powerful", "seamless", "blazing".

## 3. Terminology — use the UI's exact words

The docs and the product must use **one vocabulary**. Drift here is the fastest way
to confuse readers.

- Use the **exact labels the UI shows**: Build, Catalog, Notebooks, Schedules,
  Runs, Settings; asset, pipeline, connection, environment, the workbench,
  Inspect, Materialize, staleness. Match capitalisation to the UI.
- One concept, one term. Don't alternate "task"/"asset" or "graph"/"canvas".
  The canonical term is whatever the UI prints; the glossary (Concepts page) is the
  source of truth and every term links back to it on first use per page.
- When the product renames something, the docs rename in the **same PR** (§7).
- Prefer the product's noun for the noun and a plain verb for the action:
  "create an asset", "open the canvas", "run the pipeline".

## 4. Page templates

Author from these skeletons so pages stay uniform and scannable. Keep one `H1`,
sentence-case headings that echo UI labels.

### How-to

```
# <Verb the task>           ← task-titled: "Add a manual dependency"
<One sentence: what you'll accomplish and when you'd want to.>

## Before you start          ← prerequisites, only if real
- …

## Steps
1. <Action with the real UI label in bold>
2. …

## Result
<What the reader can now see/verify. A screenshot of the end state if it helps.>

## Related                   ← Diátaxis siblings + the concept it rests on
- …
```

### Tutorial

```
# <Build/Do something concrete>
<What you'll have built by the end, and roughly how long.>

<Numbered narrative. One happy path. No "alternatively". Every step ends in
something the reader can see on screen. Screenshots at the moments that orient.>

## What you built
<Recap + the obvious next tutorial/how-to.>
```

### Reference

```
# <Thing>
<One line of scope.>

<Tables first. Flags, keys, defaults, values. Minimal prose — only what a table
can't carry. No tutorials, no motivation.>
```

### Explanation

```
# <Concept or "How X works">
<Prose. Diagrams welcome. Answer "why does this exist / how do the pieces fit".
No step lists — link to the how-tos this motivates instead.>
```

## 5. Screenshots (hand-curated)

Screenshots are first-class and deliberately made. The bar is "would this look good
in a product tour".

- **Always the docs demo project** (`docs/demo-project`, the `acme_shop`
  pipeline): a small, coherent e-commerce analytics pipeline, organised into folders
  under `assets/` — `raw/` (four CSV **seeds**) → `staging/` (four cleaning views) →
  `marts/` (`order_items_enriched`, `customer_revenue`, `daily_revenue`). Renart uses
  the folder as the layer, so assets are named `raw.*`, `staging.*`, `marts.*` (no
  `raw_`/`stg_` prefixes), and the canvas groups them into labelled columns. It runs
  entirely on local DuckDB, so it's reproducible and readers can follow along on
  identical data.
  Never screenshot a personal or throwaway workspace. To get real data into Inspect
  shots, open the project and materialize the pipeline first.
- **Clean, consistent capture.** Same theme (pick one — default light unless the
  feature is dark-specific), same viewport width, no stray dev overlays, no
  half-loaded states. Crop to the relevant surface; don't dump the whole 1440px
  window when the point is one panel.
- **Annotate when it adds clarity** — a single accent-colour box or arrow on the
  thing the step refers to. Keep annotations sparse and consistent (one colour, one
  weight). Don't annotate decoratively.
- **Few and high-value.** A screenshot earns its place by orienting the reader or
  proving a result. Don't illustrate every step; illustrate the ones where "am I in
  the right place?" is the question.
- **Motion only when motion is the point.** The canvas, Inspect, a reconcile
  prompt — a short muted GIF/MP4 can beat three stills. Otherwise prefer a still.
- **Storage & naming.** Live with the page (e.g. `docs/src/assets/<area>/<page>-<n>.png`),
  kebab-case, descriptive. Keep source crops out of the bundle.
- **Every image needs alt text** describing what it shows (not "screenshot"). The
  canvas shots especially — describe the lineage being shown.
- **Maintenance.** When the UI changes a surface, recapture in the same PR. Because
  capture is manual, keep a short note per page of *what* state produced the shot
  (which pipeline/asset, which view) so it's reproducible by hand later.

## 6. Conventions

- **Frontmatter** on every page: `title` and `description`. The `description` is the
  search/SERP line — write it for a human deciding whether to click, in one
  sentence, no period-padding. (Optionally record the Diátaxis mode in a comment or
  sidebar group so intent stays explicit.)
- **One H1**, sentence-case headings matching UI labels.
- **Code/CLI blocks are copy-paste runnable** against the example project. Show the
  command and, where it clarifies, the expected output. Use real paths
  (`example/example`), not `<your-project>` unless the value is genuinely the
  reader's.
- **Callouts (Starlight asides)** for two things especially: the
  file-compatibility promise ("every edit is a plain file change you can commit")
  and destructive/side-effecting actions (materialize writes to the warehouse;
  full-refresh). Use `:::caution`/`:::note`/`:::tip` sparingly and meaningfully.
- **Cross-link to siblings.** Every page links to its Diátaxis neighbours under
  "Related" and to the one concept it rests on. How-tos link up to the explanation;
  explanations link down to the how-tos.
- **Link out to Bruin rarely and precisely.** Only for the canonical reference of
  something we deliberately don't restate (a specific connection's credential
  fields). Link the exact page, name what the reader will find there, and keep the
  Renart-specific part in our docs.
- **Lists over paragraphs** for anything enumerable. **Tables** for anything with
  parallel structure (flags, options, comparisons).

## 7. Ownership — the "docs touched?" gate

A user-facing change is not done until its docs are. Operationally:

- **PR checkbox.** Every PR template carries: *"User-facing change? → docs updated
  (or N/A because …)."* Author ticks it; reviewer verifies it's honest.
- **Same-PR or adjacent-PR.** P0/P1 docs land with the feature. P2 polish may
  follow in a tracked adjacent PR, not "someday".
- **Rename discipline.** A UI label change includes the docs + glossary edit in the
  same PR (§3).
- **Reviewer's job.** Treat a missing/limp doc the way you'd treat a missing test:
  request changes. "Was this helpful?" feedback and real reader gaps feed back into
  the backlog.

## 8. Review checklist

Before approving a docs PR, confirm:

- [ ] **One mode.** The page is purely tutorial / how-to / reference / explanation.
- [ ] **No Bruin tax.** A Bruin-newcomer can follow it; Bruin appears only where
      load-bearing and is linked, not taught.
- [ ] **UI words.** Terminology and capitalisation match the product 1:1; first use
      of each term links to the glossary.
- [ ] **Task-titled** (how-tos) and ends in a verifiable result.
- [ ] **Frontmatter** `title` + `description` present and reader-useful.
- [ ] **Runnable** code/CLI against the example project.
- [ ] **Screenshots** (if any) are example-project, clean, cropped, alt-texted, and
      reproducible per the page's capture note.
- [ ] **Cross-links** to Diátaxis siblings + the resting concept.
- [ ] **Right place / right scope** — not a tooltip masquerading as a page, nor a
      flow crammed into reference.
- [ ] **The gate** is satisfied: the feature this documents actually matches what
      ships.

---

*Companion: `renart-user-docs-concept.md` (information architecture, page
inventory, and phased rollout).*
