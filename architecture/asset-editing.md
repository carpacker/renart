# Asset editing — workbench, provenance, reconciliation

Status: current state (built on the `asset-editing-workbench` branch,
June 2026), with the not-yet-built pieces called out in §7.

## 1. Product thesis

Renart behaves like a **Bruin-native asset workbench**, not a visual wrapper
around YAML. The committed artifact remains a normal Bruin asset file — for
SQL assets the definition stays embedded between `/* @bruin` and `@bruin */`
in the same `.sql` file (Bruin treats a SQL file plus a sibling YAML as two
unrelated assets, so definitions are never split across files). Renart adds
editing surfaces, inference, reconciliation, and guardrails **without
creating a second source of truth**:

```
Bruin file        = canonical artifact
Renart UI         = editing and guidance layer
renart_* meta keys = compact user intent + provenance (exceptions only)
```

Design goals: Bruin compatibility (a repo edited through renart stays usable
by the `bruin` CLI, git, and external editors), high developer control, guided
onboarding, safe generation (inferred deps/columns never silently destroy
user-authored metadata), and clean git diffs.

## 2. Core model

Three concepts, of which only the first is committed:

1. **Final Bruin definition** — the YAML Bruin sees.
2. **Generated projection** — what renart infers from SQL AST, file path,
   asset type, or warehouse introspection.
3. **User intent** — manual additions, suppressions, overrides, ownership.

Physically: `final Bruin definition + compact provenance keys`. Ownership is
**field-level**: column names are hard-generated from SQL, column types are
soft-generated (user-overridable, recorded as owned), descriptions/checks/
materialization are user-owned, `depends` is inference + manual additions.
Ownership is enforced by the server-side reconciler, not by editor
decorations.

## 3. Provenance storage (`internal/web/service/assetmeta`)

The concept originally called for a nested `meta.renart` block; that does
**not** survive Bruin, which parses `meta` as `map[string]string`
(`pkg/pipeline/yaml.go`). Provenance is therefore stored as **flat string
keys** under `meta:`, keeping every committed file loadable by plain bruin:

```
renart_v         schema version
renart_g         generator version
renart_sig_deps  checksum of the renart-managed dependency projection
renart_sig_cols  checksum of the renart-managed column projection
renart_dep_add   manual dependencies (keys: a:<asset>#<mode> / u:<uri>#<mode>)
renart_dep_drop  inferred dependencies the user suppressed
renart_col_add   manual columns (names)
renart_col_drop  inferred columns the user omitted
renart_col_own   generated columns whose fields the user owns (col:field|field;…)
renart_col_map   rename memory (e:<exprhash>:col); optional
```

Only _exceptions_ are stored — inferred things are never listed; the file's
real `depends:`/`columns:` plus these keys reconstruct intent on the next
reconcile. The key strings are stable; changing one is a file-format
migration. All `assetmeta` functions are pure and unit-tested.

## 4. Reconciliation

**Dependencies** (`service/asset_dependencies.go`): inferred from the SQL AST
(Rust sqlparser with fallback), then

```
final depends = (inferred − renart_dep_drop) + renart_dep_add
```

**Columns** (`service/asset_columns_reconcile.go`): inferred columns are
merged into the asset's declared columns preserving user-authored metadata by
column name; `ReconcileAssetColumns` returns the merged set plus
`ReconcileItem`s for situations it cannot resolve automatically (a column with
user metadata no longer inferred → the UI asks map / keep manually / remove).

**Checksums / external edits**: on every canonical write the managed
dependency and column projections are hashed into `renart_sig_deps` /
`renart_sig_cols`. On load, a signature mismatch means an external edit:
unknown dependencies are adopted as manual (`dep_add`), missing inferred ones
as suppressed (`dep_drop`), externally changed generated fields become
user-owned, unknown columns become manual. External VS Code edits are safe by
default.

## 5. Transactions (`service/asset_transactions.go`)

UI surfaces never write YAML. Every edit is a semantic `AssetTransaction`
(dependency.manual.add/remove, dependency.inferred.ignore/restore,
column.check.add, column.description.set, column ownership, …) POSTed to
`/api/assets/{assetID}/transactions`. The handler read-locks the file (a
per-file lock serializes concurrent read-modify-write — fast editing used to
race and drop content), parses the current definition, applies the
transaction, reconciles against fresh inference, renders the header
deterministically, and writes atomically. One enforcement layer for
ownership, checksums, formatting, and validation.

Deterministic rendering keeps git diffs clean: stable field order, inferred
dependencies in SQL appearance order then manual ones, columns in SELECT-list
order then manual/preserved ones, no timestamps or UI state in committed
metadata (the node-preserving YAML codec in `service/asset_yaml_codec.go`
round-trips unknown fields).

## 6. UI (`web/components/app/`)

- **Guided cards** (`asset-guided-cards.tsx`), rendered in the inspector
  sidebar next to the SQL editor: identity, materialization, dependencies
  (inferred / manual / ignored, with ignore/restore/remove actions), a column
  workbench (status markers for inferred/manual/stale/type-overridden,
  checks, descriptions), and reconcile prompts. Merge editing includes
  column-scoped primary keys, `update_on_merge`, custom `merge_sql`, and a
  column-backed update-key combobox where the active execution path supports
  one. The backend-provided per-asset capability profile drives the available
  modes and their prerequisites, including warehouse-specific SQL differences,
  native versus Sling-backed Python writes, and Load/API's replace, truncate,
  append, and merge subset. Unsupported hand-authored strategies are shown as
  custom values without being reinterpreted; assets with dedicated non-generic
  runtime configuration omit this section. API, Python, and Load assets share
  the same top-level target
  connection control, including an explicit Auto state. The Load editor keeps
  only source fields in `parameters`, derives database destinations from the
  asset name, shows `destination_object` for file/storage targets, and offers a
  go-to-source action when the source resolves to an upstream asset. Every edit
  flows through the transaction/API write paths; the workspace SSE stream
  refreshes the asset. Asset-type selectors in both guided and YAML views group
  SQL asset kinds separately from non-SQL kinds while preserving unknown current
  values for repair.
- **Run-scoped full refresh:** supported table assets expose a Full refresh
  action without mutating their saved strategy. The destructive dialog names
  the selected environment and current execution window; environments with
  `confirm_destructive` require typing the exact environment name. The same
  confirmation is enforced again in the backend for HTTP and CLI callers.
- **Provenance classification client-side** (`lib/asset-provenance.ts`)
  mirrors the flat-key schema for display (source chips: "inferred from SQL" /
  "manual").
- **Expert YAML mode** (`asset-yaml-editor.tsx`): edit the real definition
  with the same materialization/key controls plus pickers/completion for
  connections and columns; parsed and validated before write.
- **Pipeline connection context**: a canvas asset's connection badge opens the
  pipeline settings Connections section. The config response keeps explicit
  `pipeline.yml` overrides separate from read-only defaults Bruin infers from
  asset types, and each resolved connection links to that exact environment
  connection under project settings.
- Column refresh actions: re-infer from SQL
  (`/columns/refresh-from-definition`), fill from warehouse
  (`/fill-columns-from-db`), reconcile (`/columns/reconcile`).

## 7. Not built (still intent, from the original concept)

- **Draft persistence layer** (browser/IndexedDB journal recovering unsaved
  typing across reloads). Canonical autosave exists; the volatile draft
  journal does not.
- **Raw / detached mode** — granular "renart stops managing this field/asset"
  detachment. `renart_col_own` covers field-level type ownership; whole-path
  detachment is not implemented.
- **Semantic diff prompts** before applying non-trivial generated changes
  (today safe additions auto-apply; conflicts surface as reconcile items).
- **Command palette metadata actions** beyond what the cards expose.
- Expression-hash rename memory (`renart_col_map`) is defined in the schema
  but rename suggestions are not yet surfaced in the UI.

The full original design exploration (UI sketches, autosave matrix) lives in
git history: `architecture/renart-asset-editing-concept.md` before this file
replaced it.
