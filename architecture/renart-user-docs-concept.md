# Renart User-Facing Documentation — Concept & Plan

Status: proposal. This document defines how we structure, write, and grow the
user-facing docs at `docs/` (Astro Starlight, served at getrenart.com/docs).

---

## 1. Purpose & scope

The docs must let a data/analytics engineer go from "never heard of Renart" to
"running, editing, and trusting a real pipeline" without reading the source. They
cover the **product** (the web app + CLI), not the codebase (that lives in
`architecture/*`). They sit *next to* Bruin's own docs: Renart docs explain the
Renart experience and link out to Bruin for the underlying asset/connection model
rather than re-documenting it.

Non-goals: API/internal architecture reference (kept in `architecture/`), and
re-explaining Bruin concepts Bruin already documents well.

## 2. Audience & jobs-to-be-done

Primary persona (from the README): data engineers, analytics engineers, and
technical data users who want a visual, Git-native way to work on Bruin pipelines.

Three reader modes the IA must serve simultaneously:

- **Evaluator** — "Is this for me, and how is it different from the Bruin CLI / a
  dashboard?" Needs orientation, concepts, and a fast win.
- **New user** — "Get me running on my own repo." Needs install + guided tutorials.
- **Working user** — "How do I do X right now?" Needs short, findable, task-shaped
  how-tos and reference, not prose.

## 3. Current state & gaps

Today `docs/` has two sidebar groups — **Introduction** (Overview, Concepts, Who
Renart Is For, Renart for Bruin Users) and **Getting Started** (Installation,
Quickstart, Running Renart). Tone is excellent: clear, concept-first, concise.

Gap: it stops at "you can start Renart." Nothing documents the actual product
surface — the canvas, the asset editor/workbench, inspect, materialize, asset
types (SQL/Python/API/Sling/ingestr/seed), connections & environments, notebooks,
scheduling, type-checking, runs/staleness, or the CLI beyond `renart web`. That is
the bulk of what users need and exactly what's grown most on the redesign branch.

## 4. Documentation philosophy

Adopt **Diátaxis** (the four-mode model) so every page has one job and authors
know where new content goes. The four modes, mapped to Renart:

| Mode | Reader is… | Renart content | Example |
| --- | --- | --- | --- |
| **Tutorial** | learning by doing | Getting Started, "build your first pipeline" | Quickstart |
| **How-to** | accomplishing a task | Workflow guides | "Add a manual dependency", "Load a CSV with Sling" |
| **Reference** | confirming a fact | CLI flags, asset file format, shortcuts | `renart type-check` flags |
| **Explanation** | understanding *why* | Concepts, the Git-native model | "How Renart relates to Bruin" |

Rules that fall out of this:
- A page never mixes modes. A how-to that starts explaining theory splits into a
  how-to + a linked explanation.
- **Terminology is fixed to the product's own words** — Build, Catalog, Inspect,
  Materialize, asset, connection, environment, workbench. The docs must use the
  exact labels the UI uses; a glossary entry per term anchors them.
- Every how-to is **task-titled** ("Materialize a pipeline", not "Materialization")
  and ends in a verifiable outcome.
- **Don't duplicate Bruin.** When a topic is really a Bruin concept (asset YAML
  fields, connection credential shapes), state the Renart-specific part and link to
  Bruin docs for the canonical reference.

## 5. Proposed information architecture

Eleven top-level groups. Priority tags: **P0** = launch-blocking (a user can't
succeed without it), **P1** = core workflows, **P2** = depth/polish.

### Introduction *(Explanation)*
- Overview — what Renart is, the one-screenshot pitch **[P0, exists]**
- Concepts / glossary — pipeline, asset, canvas, inspect, materialize, connection,
  environment, workbench, staleness **[P0, expand existing]**
- Who Renart is for **[P0, exists]**
- Renart & Bruin — relationship, when to use the CLI vs the UI, file-compat
  promise **[P0, exists as "Renart for Bruin Users"]**
- How Renart works — the Git-native file model, "every edit is a file change" **[P1]**

### Getting Started *(Tutorial)*
- Installation **[P0, exists]**
- Quickstart — open the bundled example, read the canvas, inspect, materialize, in
  ~5 min **[P0, exists — tighten to a single happy path]**
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
  decision table **[P1]** (links to `renart-materialization-strategies.md`)
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
- Load (Sling) assets — source/target connections, the `local` file option, path
  & stream autocomplete (`sling conns discover`), modes, automatic upstream &
  column inference **[P1, new feature — author alongside the code]**
- Ingestr assets **[P2]**
- Seeds **[P2]**

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

Author every page from a fixed skeleton so pages stay scannable and uniform.

- **How-to:** one-line goal → prerequisites → numbered steps → "Result" → "Related".
- **Reference:** terse, table-first, no narrative.
- **Tutorial:** narrative, one happy path, every step verifiable, no branching.
- **Explanation:** prose, diagrams allowed, links to the how-tos it motivates.

Conventions:
- **Frontmatter** `title` + `description` on every page (already the norm); the
  `description` is the SEO/serp line.
- **Screenshots** are first-class but costly to maintain. Standardize on the
  bundled example project for every screenshot so they're reproducible, and script
  capture where possible (a `make docs-screenshots` target already exists — extend
  it). Annotate UI shots; keep them few and high-value. Prefer short GIFs/video
  only for genuinely motion-based flows (canvas, inspect).
- **Code/CLI blocks** copy-paste runnable against the example project.
- **Callouts** (Starlight asides) for the file-compat promise and destructive
  actions (materialize side effects, full-refresh).
- **Cross-links** every page to its Diátaxis siblings ("Related") and to Bruin
  where relevant.
- **One H1 per page**, sentence-case headings matching UI labels.

## 7. Cross-cutting decisions

- **Bruin boundary.** Maintain a single "Renart & Bruin" explanation page as the
  canonical statement of the split, and link to it rather than re-litigating it
  per page. Every asset-type page names the Bruin doc it defers to.
- **Versioning.** Renart is pre-1.0 and fast-moving. Start *unversioned* (docs
  track `main`); add Starlight versioning only once releases stabilize. Record a
  "docs reflect vX" note in the footer.
- **Search & nav.** Starlight ships Pagefind search; ensure it's enabled. Keep the
  sidebar ≤ ~9 visible groups; collapse deep groups.
- **Feedback loop.** Add a per-page "Edit on GitHub" + a lightweight "Was this
  helpful?" so gaps surface from real readers.
- **Accessibility & SEO.** Alt text on every image (the canvas shots especially),
  descriptive `description` frontmatter, keep the existing structured-data/OG setup.
- **Contribution guide.** A short `docs/CONTRIBUTING`-style page: Diátaxis rules,
  the page skeletons, the screenshot convention, and "use the UI's exact words."

## 8. Phased rollout

**Phase 0 — Skeleton (1 pass).** Stand up all eleven sidebar groups with stub
pages + the four page templates and the contribution guide. Lets us fill in
parallel and exposes the shape immediately. No content debt hidden behind a flat
nav.

**Phase 1 — The P0/P1 spine.** The path a user actually walks: expand Concepts;
"Build your first pipeline" tutorial; canvas; asset editor + workbench;
materialization/dependencies/columns how-tos; SQL + Load(Sling) asset pages;
connections & environments; type-checking; CLI reference; troubleshooting. This is
the launchable doc set.

**Phase 2 — Depth.** Remaining asset types, notebooks, scheduling, provenance
explanation, shortcuts, config reference, deploy/standalone, richer media.

Sequence within a phase by **reader frequency**, not by how interesting the
feature is to us: install → quickstart → editing → connections → run → validate.

## 9. Open questions (need a decision)

1. **Audience breadth.** Do we assume the reader already knows Bruin, or support a
   Bruin-newcomer path? This changes how much Bruin we restate. *(Recommendation:
   assume light Bruin familiarity; one "Renart & Bruin" page bridges newcomers.)*
2. **Docs vs. in-app help.** How much lives in docs vs. inline tooltips/empty
   states in the app? Propose: docs own conceptual + multi-step; the app owns
   field-level hints.
3. **Screenshot maintenance budget.** Auto-captured against the example, or
   hand-curated? Auto is cheaper long-term but needs the capture harness extended.
4. **Versioning trigger.** Which release turns on doc versioning?
5. **Ownership.** Who owns docs freshness as features land (a "docs touched?" PR
   checkbox)?

## 10. Success criteria

- A new user reaches a materialized asset on their own repo from the docs alone.
- Every shipped UI surface has a findable how-to within one search/scan.
- No page mixes Diátaxis modes; terminology matches the UI 1:1.
- New features land with their P0/P1 docs in the same or adjacent PR.
