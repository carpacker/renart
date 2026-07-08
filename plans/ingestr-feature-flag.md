# Hide ingestr behind a feature flag

Status: in progress (this session)

## Rule

Without the flag, a user sees **zero** ingestr traces — unless the open
pipeline already contains ingestr assets, in which case those assets keep
working and their surfaces stay visible. Users coming from bruin can flip the
flag to get everything back.

## Flag

`features.ingestr: true|false` in `.renart/project.yml` (project-scoped),
surfaced as a toggle in Project settings → General ("Show ingestr asset
types"). Server includes the resolved flag in `/api/workspace` (or config
payload) as `features: { ingestr: bool }`; effective visibility =
`flag || workspaceContainsIngestrAssets`.

## Surfaces to gate (grep `ingestr` in web/)

- new-asset dialog / asset kind pickers (`build-page.tsx` kind options,
  `asset-guided-cards.tsx`),
- connection dropdowns in settings (`settings-pages.tsx` +
  `lib/asset-types.ts` connection type lists — only non-ingestr types
  offered),
- catalog filters / integration badges (`catalog-page.tsx`),
- YAML/intellisense suggestions for `type:` values
  (`use-yaml-intellisense.ts`, suggestion catalog),
- load/ingestr parameter editors stay (they only render for existing
  assets).

## Mechanics

- `web/lib/features.ts`: `useFeature("ingestr")` reading workspace payload +
  "pipeline contains ingestr asset" derivation (asset type prefix
  `ingestr.`).
- Keep executor/back-end behavior untouched: parsing, running, type-checking
  ingestr assets works regardless of the flag.

## Verification

- e2e: flag off + clean pipeline → "ingestr" appears nowhere in the DOM of
  new-asset dialog, settings connections, catalog filters; flag off +
  pipeline with an ingestr asset → the asset renders and runs; flag on →
  options reappear.
