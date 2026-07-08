# Renart User-Facing Documentation — Concept & Plan

Status: active (decisions in §9 locked with the user). This document defines what
we build and in what order for the user-facing docs at `docs/` (Astro Starlight,
served at getrenart.com/docs). The companion `../architecture/docs-framework.md` defines the
authoring contract (voice, templates, conventions, the docs gate).

---

## 1. Purpose & scope

The docs must let a data/analytics engineer go from "never heard of Renart" to
"running, editing, and trusting a real pipeline" without reading the source. They
cover the **product** (the web app + CLI), not the codebase (that lives in
`architecture/*`). They teach **Renart's own model in Renart's own words** and do
not require the reader to know Bruin: Bruin is mentioned only where it is
load-bearing (file-compatibility, CLI interop, a few credential shapes we defer on)
and linked rather than taught.

Non-goals: API/internal architecture reference (kept in `architecture/`), and
teaching Bruin. We reduce Bruin's presence overall — see §9.1.

## 2. Audience & jobs-to-be-done

Primary persona: data engineers, analytics engineers, and technical data users who
want a visual, Git-native way to build and run data pipelines. **We assume no prior
Bruin knowledge** — many readers will meet the underlying engine for the first time
through Renart, and the docs must carry them without that background.

Three reader modes the IA must serve simultaneously:

- **Evaluator** — "Is this for me, and how is it different from a CLI tool or a
  dashboard?" Needs orientation, concepts, and a fast win.
- **New user** — "Get me running on my own repo." Needs install + guided tutorials.
- **Working user** — "How do I do X right now?" Needs short, findable, task-shaped
  how-tos and reference, not prose.

## 3. Current state & gaps

Today `docs/` has two sidebar groups — **Introduction** (Overview, Concepts, Who
Renart Is For, Renart for Bruin Users) and **Getting Started** (Installation,
Quickstart, Running Renart). Tone is excellent: clear, concept-first, concise.

The current Bruin-User framing (a dedicated "Renart for Bruin Users" page, Bruin
assumed) is **out of step with the locked audience decision** (§9.1): we now assume
no Bruin knowledge. That page becomes a short, optional "Renart & the Bruin CLI"
interop note rather than a load-bearing bridge, and Bruin references thin out
across the set.

Gap: it stops at "you can start Renart." Nothing documents the actual product
surface — the canvas, the asset editor/workbench, inspect, materialize, asset
types (SQL/Python/API/Load/seed), connections & environments, notebooks,
scheduling, type-checking, runs/staleness, or the CLI beyond `renart web`. That is
the bulk of what users need and exactly what's grown most on the redesign branch.

## 4. Documentation philosophy

We adopt **Diátaxis** (the four-mode model) so every page has one job. The full
authoring contract — the four modes, voice & tone, page templates, terminology,
screenshots, and the review checklist — lives in `../architecture/docs-framework.md`. The
load-bearing points for the IA below:

- A page never mixes modes (tutorial / how-to / reference / explanation).
- **Terminology is fixed to the product's own words** (Build, Catalog, Inspect,
  Materialize, asset, connection, environment, workbench), with a glossary anchor.
- Every how-to is **task-titled** and ends in a verifiable outcome.
- **No Bruin tax.** Teach Renart's model directly; mention Bruin only where
  load-bearing and link out rather than teaching it (§9.1).

## 5. Proposed information architecture

Eleven top-level groups. Priority tags: **P0** = launch-blocking (a user can't
succeed without it), **P1** = core workflows, **P2** = depth/polish.

### Introduction *(Explanation)*
- Overview — what Renart is, the one-screenshot pitch **[P0, exists]**
- Concepts / glossary — pipeline, asset, canvas, inspect, materialize, connection,
  environment, workbench, staleness **[P0, expand existing]**
- Who Renart is for **[P0, exists]**
- How Renart works — the Git-native file model, "every edit is a file change",
  the file-compat promise **[P1]**
- Renart & the Bruin CLI — *optional* short interop note: edits are plain files the
  `bruin` CLI can also run; when you'd reach for the CLI. Demoted from the old
  "Renart for Bruin Users" bridge to keep with the no-Bruin-assumption audience
  **[P2, rework existing]**

### Getting Started *(Tutorial)*
- Installation **[P0, exists]**
- Quickstart — open the docs demo project (`docs/demo-project`, `acme_shop`), read
  the canvas, inspect, materialize, in ~5 min **[P0, exists — retarget onto the demo
  project and tighten to a single happy path]**
- Build your first pipeline — longer guided tutorial on the reader's own repo:
  add a connection, create a SQL asset, add a downstream, run it **[P1]**
- Running Renart — ports, fallback, flags, opening a project **[P0, exists]**

### The Workspace *(How-to + Explanation)*
- Tour of the interface — Build / Catalog / Notebooks / Schedules / Runs /
  Settings **[P1]**
- The pipeline canvas — navigate, read lineage, layout, open assets **[P1]**
- The asset catalog — browse, search, filter **[P2]**
- Runs & history — what a run records, re-running **[P2]**

### Editing Assets *(How-to + Reference — the workbench)*
- The asset editor — Monaco, IntelliSense, SQL validation, formatting **[P1]**
- The asset workbench — guided cards vs the YAML view, when to use each **[P1]**
- Identity, owners & tags **[P2]**
- Materialization strategies — none/view/table/append/merge/incremental, with a
  decision table **[P1]** (links to `materialization-strategies.md`)
- Dependencies — inferred vs manual, ignoring, reconciliation prompts **[P1]**
- Columns — inferred vs manual, types, descriptions, refresh-from-definition **[P1]**
- Quality checks — adding/removing Bruin column checks **[P2]**
- How provenance works — the `renart_*` meta keys, why edits survive re-inference
  *(Explanation)* **[P2]**

### Asset Types *(Reference + How-to per kind)*
- Choosing an asset type — overview table **[P1]**
- SQL assets **[P1]**
- Python assets — the Bruin Python SDK, pyproject **[P2]**
- HTTP API assets **[P2]**
- Load assets — source/target connections, the `local` file option, path
  & stream autocomplete (`sling conns discover`), modes, automatic upstream &
  column inference **[P1, new feature — author alongside the code]**
- Seeds **[P2]**

*Ingestr assets are intentionally out of scope for the docs for now (decision in
§9), so they are not listed as an asset type to document.*

### Connections & Environments *(How-to)*
- Managing connections — add, edit, test **[P1]**
- Environments — what they are, switching, schema prefixes **[P1]**
- Supported connection types — short list + link to Bruin's catalog **[P2]**

### Notebooks *(How-to + Explanation)*
- Notebooks overview — what they are, per-notebook DuckDB sessions **[P2]**
- Working in a notebook — cells, `@viz`, rename/promote **[P2]**
- Promoting cells to assets **[P2]**

### Scheduling *(How-to)*
- Scheduling overview **[P2]**
- Creating & editing schedules; environment schedules **[P2]**

### Validation & Quality *(How-to)*
- Type checking in the UI **[P1]**
- SQL validation & IntelliSense **[P2]**
- Quality checks (cross-link to Editing Assets) **[P2]**

### Automation & Deployment *(How-to + Reference)*
- `renart type-check` in CI **[P1]**
- Deploying with `renart deploy` **[P2]**
- Standalone mode **[P2]**

### Reference
- CLI reference — `web`, `type-check`, `deploy`, `standalone`; global flags & env
  vars (e.g. `RENART_SLING_BINARY`, ports) **[P1]**
- Asset file format — the Renart-managed keys + `renart_*` meta, link to Bruin
  **[P2]**
- Keyboard shortcuts **[P2]**
- Configuration & settings **[P2]**
- Troubleshooting & FAQ — port in use, "not a git repo", connection won't
  validate, sling/uv first-run, etc. **[P1]**

## 6. Page templates & conventions

The page skeletons (how-to / reference / tutorial / explanation), frontmatter,
callouts, cross-linking, and the full screenshot guidance live in
`../architecture/docs-framework.md`. The IA-relevant headlines:

- **Four fixed skeletons**, one per Diátaxis mode; pages never mix them.
- **Screenshots are hand-curated** (decision §9.3): deliberately composed against
  the **docs demo project** (`docs/demo-project`, the `acme_shop` pipeline — a
  purpose-built coherent example), cropped, alt-texted, annotated only when it adds
  clarity. No auto-capture harness. Few and high-value; motion (GIF/MP4) only for
  genuinely motion-based flows (canvas, Inspect).
- **Code/CLI blocks** copy-paste runnable against the docs demo project.
- **Callouts** for the file-compat promise and destructive/side-effecting actions.
- **One H1 per page**, sentence-case headings matching UI labels.

## 7. Cross-cutting decisions

- **Bruin boundary.** We teach Renart's model directly and assume no Bruin
  knowledge (§9.1). Bruin appears only where load-bearing; a single optional
  "Renart & the Bruin CLI" interop note carries the file-compat/CLI story, and the
  rare page that genuinely defers (a specific credential shape) links the exact
  Bruin doc rather than restating it.
- **Versioning.** Renart is pre-1.0 and fast-moving. Start *unversioned* (docs
  track `main`); add Starlight versioning only once releases stabilize. Record a
  "docs reflect vX" note in the footer.
- **Search & nav.** Starlight ships Pagefind search; ensure it's enabled. Keep the
  sidebar ≤ ~9 visible groups; collapse deep groups.
- **Feedback loop.** Add a per-page "Edit on GitHub" + a lightweight "Was this
  helpful?" so gaps surface from real readers.
- **Accessibility & SEO.** Alt text on every image (the canvas shots especially),
  descriptive `description` frontmatter, keep the existing structured-data/OG setup.
- **Contribution guide.** The authoring contract is `../architecture/docs-framework.md`
  (Diátaxis rules, page skeletons, screenshot convention, "use the UI's exact
  words", the docs gate). A short `docs/`-internal contributor page can summarise it
  and link there rather than restating it.
- **Ownership / docs gate.** A user-facing change ships with its docs, enforced by a
  "docs touched?" PR checkbox (§9.5, detailed in the framework doc §7).

## 8. Phased rollout

**Phase 0 — Skeleton (1 pass).** Stand up all eleven sidebar groups with stub
pages + the four page templates and the contribution guide. Lets us fill in
parallel and exposes the shape immediately. No content debt hidden behind a flat
nav.

**Phase 1 — The P0/P1 spine.** The path a user actually walks: expand Concepts;
"Build your first pipeline" tutorial; canvas; asset editor + workbench;
materialization/dependencies/columns how-tos; SQL + Load asset pages;
connections & environments; type-checking; CLI reference; troubleshooting. This is
the launchable doc set.

**Phase 2 — Depth.** Remaining asset types, notebooks, scheduling, provenance
explanation, shortcuts, config reference, deploy/standalone, richer media.

Sequence within a phase by **reader frequency**, not by how interesting the
feature is to us: install → quickstart → editing → connections → run → validate.

## 9. Decisions (locked with the user)

1. **Audience breadth → assume zero Bruin knowledge.** We teach Renart's own model
   in Renart's own words and reduce Bruin's presence across the docs. Bruin appears
   only where load-bearing (file-compat, CLI interop, a few credential shapes) and
   is linked, not taught. The old "Renart for Bruin Users" bridge is demoted to an
   optional interop note (§3, §5).
2. **Docs vs. in-app help → docs own conceptual + multi-step; the app owns
   field-level hints.** A single field's meaning is a tooltip; a multi-step flow or
   a "what/why" is a docs page. (Framework doc §0.2.)
3. **Screenshots → hand-curated.** Deliberately composed against the docs demo
   project (`docs/demo-project`, the `acme_shop` pipeline — a purpose-built coherent
   example, not a personal playground), cropped, alt-texted, annotated only when
   useful. No auto-capture harness. (Framework doc §5.)
4. **Versioning → unversioned for now.** Docs track `main`; turn on Starlight
   versioning later once releases stabilise (trigger TBD at that point).
5. **Ownership → a "docs touched?" gate.** A PR checkbox marks user-facing changes
   as docs-updated (or N/A with a reason); reviewers enforce it. (Framework doc §7.)

## 10. Success criteria

- A new user reaches a materialized asset on their own repo from the docs alone.
- Every shipped UI surface has a findable how-to within one search/scan.
- No page mixes Diátaxis modes; terminology matches the UI 1:1.
- New features land with their P0/P1 docs in the same or adjacent PR.
