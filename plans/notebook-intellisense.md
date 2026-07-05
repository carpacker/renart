# Notebook intellisense + go-to-definition

Status: in progress (this session)

## Problem

In notebook cells:
- no completion for pipeline assets (e.g. `scratch.base`) even though cells
  can `select … from <pipeline asset>` (imports),
- no completion for sibling cell names,
- Ctrl+Click / F12 go-to-definition does nothing.

In the pipeline editor the *reverse* must hold: cell names must NOT be
suggested (notebook cells are notebook-scoped).

## Current wiring

- Pipeline editors get intellisense from `web/hooks/use-sql-intellisense.ts` +
  `web/lib/monaco-sql-providers.ts`, fed by the suggestion catalog atoms
  (`web/lib/atoms/suggestion-catalog*.ts`) and per-asset parse context
  (`use-sql-parse-context.ts`).
- Notebook cells render Monaco through
  `web/components/redesign/notebook-cell-editor.tsx`, which builds
  `schemaTables` via `buildNotebookSchemaTables(workspace, cells, cell,
  resultColumnsByCell)` (notebook-page.tsx) — so the *data* for siblings and
  workspace exists; what differs is which providers get registered for the
  `inmemory://bruin/notebook/<cellId>.<ext>` models.

## Plan

1. Trace what `notebook-cell-editor` registers vs the asset editor
   (`use-asset-monaco` / `use-sql-intellisense`): identify why asset/cell
   tables don't reach the completion provider for notebook models.
2. Ensure the notebook completion source includes:
   - sibling cells (name → columns from last run, else declared), and
   - pipeline assets (workspace suggestion catalog), marked as imports.
3. Scoping rule: suggestion catalog entries for notebook cells must be tagged
   with their notebook id and filtered out everywhere except sibling cells of
   the same notebook. Pipeline models never see cell entries.
4. Go-to-definition: register a definition provider for notebook models that
   resolves
   - cell name → scrolls to / focuses that cell's card (client-side navigation
     within the notebook page),
   - pipeline asset name → navigates to the asset in the build page (same
     route the catalog uses).
5. e2e: extend `web/tests/e2e/sql/intellisense.live.spec.ts` (or a notebook
   spec) with: completion lists sibling cell + pipeline asset in a cell;
   pipeline editor does not list cell names; F12 on a cell reference focuses
   the cell.

## Non-goals

- Python-cell intellisense changes (ty engine) beyond what already works.
- Cross-notebook references (cells stay notebook-scoped).
