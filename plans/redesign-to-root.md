# Promote /redesign to / and delete the old UI

Status: in progress (this session)

## Shape

The app currently ships two UIs:
- old: `web/src/routes/_workspace*` + `web/components/workspace-*.tsx` and
  friends (canvas-pane, editor-pane, settings pages, onboarding routes),
- new: `web/src/routes/redesign/**` + `web/components/redesign/**`, mounted at
  `/redesign`.

## Steps

1. Move `web/src/routes/redesign/*` up to `web/src/routes/` (route files keep
   their relative structure: `pipelines/`, `runs/`, `notebooks/`, `catalog`,
   `schedules`, `project/`, `account/`, `dashboards/`). `redesign/route.tsx`
   becomes the root layout route (pathless `_shell` or the root `__root`
   augmentation).
2. Delete old routes: `_workspace*`, `onboarding/*` (old flow; new onboarding
   comes from plans/onboarding-projects.md), `-onboarding-shared.ts`,
   `onboarding.success.tsx`.
3. Replace all `to="/redesign..."` links/`Link` params (~40 call sites) with
   root paths; grep `"/redesign` in web/.
4. Add a `/redesign/*` → `/*` redirect route so old bookmarks keep working.
5. Delete now-unreferenced old components (`workspace-*.tsx`,
   `asset-editor-*`, `column-editor`, …) — verify with `tsc --noEmit` +
   `eslint` unused-import errors + a script that greps for orphan imports.
   Keep shared primitives that redesign imports (check `components/ui/**`,
   `ansi-output`, `asset-type-icon`, `virtual-data-table`, …).
6. Backend: grep Go for hardcoded `/redesign` paths (open-browser URL,
   onboarding redirects) and update; check `internal/web/static` fallback
   routing and `--no-open` default URL.
7. Update playwright specs (`web/tests/e2e/**`) that navigate `/redesign` or
   the old routes; run `pnpm build` to regenerate `routeTree.gen.ts`;
   run live e2e smoke.
8. Rename dirs/files in a later cleanup pass (`components/redesign/` →
   keep name for now to limit the diff; the "redesign" naming cleanup joins
   the repo-cleanup task).

## Risks

- routeTree regeneration must happen through the build (AGENTS.md).
- Old-UI-only hooks/atoms may still be imported by redesign code — delete
  bottom-up, compiler-guided.
