# Renart Asset Editing Concept

## 1. Product thesis

Renart should behave like a **Bruin-native asset workbench**, not like a visual wrapper around YAML.

The committed artifact remains a normal Bruin asset file. For SQL assets, that means the Bruin definition stays embedded inside the same `.sql` file between `/* @bruin` and `@bruin */`; Bruin explicitly treats a SQL file plus a sibling YAML definition as two unrelated assets, so Renart should not split canonical SQL asset definitions across files. Bruin’s `meta` field is intended for custom asset metadata, and `depends` determines execution order while supporting both string dependencies and object dependencies with `asset`, `uri`, and `mode`. ([Bruin][1])

Renart’s job is to provide better editing surfaces, inference, reconciliation, and guardrails **without creating a second source of truth**.

```text
Bruin file = canonical artifact
Renart UI = editing and guidance layer
meta.renart = compact user intent + provenance
```

---

## 2. Design goals

Renart should optimize for five goals:

1. **Bruin compatibility**
   A repository edited through Renart should remain usable by Bruin CLI, normal Git workflows, code review, and external editors.

2. **High developer control**
   Users can always inspect and edit the underlying Bruin YAML, but Renart should make dangerous or confusing edits explicit.

3. **Guided onboarding**
   Beginners should not need to understand every Bruin field before creating a working asset.

4. **Safe generation**
   SQL-derived dependencies, columns, and types should be generated automatically, but user-authored metadata must not be silently destroyed.

5. **Clean Git diffs**
   Renart metadata should be compact, deterministic, and reviewable.

---

## 3. Core model

Renart should separate three concepts internally:

```text
1. Final Bruin definition
   The YAML that Bruin sees.

2. Generated projection
   Things Renart can infer from SQL, file path, asset type, parser output, or warehouse introspection.

3. User intent
   Manual additions, suppressions, overrides, ownership decisions, and reconciliation choices.
```

Physically, only this is stored:

```text
final Bruin definition + compact meta.renart provenance
```

The generated base and user patch are **runtime concepts**, not separate committed files.

```text
SQL body
  ↓
SQL AST inference
  ↓
generated projection
  + compact user intent
  + existing Bruin annotations
  ↓
Renart reconciler
  ↓
final Bruin YAML
  ↓
asset file
```

---

## 4. Ownership model

Renart should use **field-level ownership**, not line-level ownership.

Each field belongs to one of these categories:

| Category       | Meaning                                                     | Example                             |
| -------------- | ----------------------------------------------------------- | ----------------------------------- |
| Hard-generated | User edits the source of truth, not the YAML field directly | column name inferred from SQL alias |
| Soft-generated | Renart suggests or maintains it, but user can override      | column type, inferred dependency    |
| User-owned     | Renart preserves it unless explicitly asked to transform it | descriptions, checks, tags, owner   |
| Detached       | Renart no longer manages this field/path                    | manually detached dependency list   |

Suggested defaults:

| Bruin area               | Default owner                        |
| ------------------------ | ------------------------------------ |
| `name`                   | file path / explicit user override   |
| `type`                   | asset kind selector                  |
| `depends`                | SQL AST inference + manual additions |
| `columns[*].name`        | SQL AST inference                    |
| `columns[*].type`        | inference, user-overridable          |
| `columns[*].description` | user                                 |
| `columns[*].checks`      | user                                 |
| `materialization`        | user / guided wizard                 |
| `custom_checks`          | user                                 |
| `hooks`                  | user                                 |
| `meta.renart`            | Renart                               |

This ownership model should be enforced by the asset reconciler, not only by Monaco decorations.

---

## 5. Storage shape

Renart should continue storing everything in the Bruin asset file.

For SQL assets:

```sql
/* @bruin
type: bq.sql

depends:
  - raw.orders
  - raw.customers
  - asset: analytics.date_spine
    mode: symbolic

materialization:
  type: table
  strategy: merge

columns:
  - name: order_id
    type: integer
    primary_key: true
    checks:
      - name: not_null
      - name: unique

  - name: customer_id
    type: integer
    checks:
      - name: not_null

  - name: order_total
    type: numeric
    description: Total order amount after discounts.

  - name: loaded_at
    type: timestamp
    description: Technical load timestamp.

meta:
  renart:
    v: 1
    g: 12
    sig:
      d: "uV4z8T1qN7p9K2aB"
      c: "oQx7B3sm8Ep1L0vR"
    d:
      add: ["a:analytics.date_spine#symbolic"]
      drop: []
    c:
      add: [loaded_at]
      drop: []
      own:
        order_total: [type]
@bruin */

select
  o.id as order_id,
  o.customer_id,
  o.total_after_discounts as order_total
from raw.orders o
join raw.customers c on c.id = o.customer_id
```

The actual Bruin fields remain readable. Renart metadata only stores compact exceptions and provenance.

---

## 6. Compact `meta.renart` schema

Recommended schema:

```yaml
meta:
  renart:
    v: 1
    g: 12

    sig:
      d: "dependencyProjectionHash"
      c: "columnProjectionHash"

    d:
      add: ["a:analytics.date_spine#symbolic"]
      drop: ["a:scratch.tmp_dates#full"]

    c:
      add: [loaded_at]
      drop: [debug_rank]
      own:
        order_total: [type]
      map:
        "e:9cc83f4a": order_total
```

Field meanings:

| Field    | Meaning                                                        |
| -------- | -------------------------------------------------------------- |
| `v`      | Renart metadata schema version                                 |
| `g`      | Renart inference/generator version                             |
| `sig.d`  | checksum of the Renart-managed dependency projection           |
| `sig.c`  | checksum of the Renart-managed column projection               |
| `d.add`  | manual dependencies added by the user                          |
| `d.drop` | inferred dependencies intentionally suppressed by the user     |
| `c.add`  | manual columns preserved even when not inferred from SQL       |
| `c.drop` | inferred columns intentionally omitted from metadata           |
| `c.own`  | generated fields now owned by the user                         |
| `c.map`  | optional rename memory, usually expression-hash to column name |

The default assumption is:

```text
Inferred things do not need to be listed.
Only exceptions are stored.
```

That is what keeps the metadata small.

---

## 7. Dependency reconciliation

Dependencies are inferred from the SQL AST by default.

```text
final dependencies =
  inferred SQL dependencies
  - ignored inferred dependencies
  + manual dependencies
```

In code terms:

```ts
finalDepends = unionByDependencyKey(
  inferredDepends.filter(dep => !meta.renart.d.drop.includes(key(dep))),
  meta.renart.d.add.map(parseDependencyKey),
)
```

Use normalized dependency keys in Renart metadata:

```text
a:<asset>#<mode>
u:<uri>#<mode>
```

Examples:

```text
a:raw.orders#full
a:analytics.date_spine#symbolic
u:bruin://other-pipeline/raw.events#full
```

UI behavior:

```text
Inferred dependency
raw.orders
Source: SQL AST
Action: Ignore

Manual dependency
analytics.date_spine
Source: User
Action: Remove

Ignored inferred dependency
scratch.tmp_dates
Source: SQL AST
Action: Restore
```

This avoids forcing users to hand-maintain dependencies while still allowing explicit manual additions.

---

## 8. Column reconciliation

Columns are also inferred from SQL by default.

```text
final columns =
  inferred columns
  - intentionally dropped inferred columns
  + manually added columns
  + preserved user annotations
```

Renart should preserve user-authored metadata by column name:

```yaml
columns:
  - name: order_id
    type: integer
    primary_key: true
    checks:
      - name: not_null
      - name: unique
```

Here, `name` and maybe `type` can be generated, while `primary_key` and `checks` are user-authored annotations.

When SQL changes, Renart should avoid destructive behavior:

```text
Column no longer inferred, but has user metadata
→ mark as stale
→ ask whether to remove, keep manually, or map to a new column

Column expression renamed but expression hash matches
→ suggest metadata move

Column type changes but user owns type
→ preserve user type
```

---

## 9. Checksums and external edits

The compact schema is robust because the Bruin YAML itself acts as the visible snapshot, while `sig` tells Renart whether the managed projection still matches what Renart last wrote.

On every canonical write:

```ts
meta.renart.sig.d = hash(canonicalize(managedDependencyProjection))
meta.renart.sig.c = hash(canonicalize(managedColumnProjection))
```

On load:

```ts
const currentDependencySig = hash(canonicalize(currentManagedDependencies))
const currentColumnSig = hash(canonicalize(currentManagedColumns))

if (currentDependencySig === meta.renart.sig.d) {
  // Safe to update inferred dependencies automatically.
} else {
  // External/manual edit detected.
  // Preserve changes and adopt unknown entries as manual.
}
```

Recommended behavior on checksum mismatch:

```text
Unknown dependency in file
→ adopt as d.add

Missing inferred dependency
→ adopt as d.drop

Generated column type changed externally
→ mark c.own[column].type

Unknown column in file
→ adopt as c.add
```

This makes external VS Code edits safe by default.

---

## 10. Autosave model

Autosave should have two layers:

```text
1. Draft persistence
   Captures every user edit immediately.

2. Canonical file persistence
   Writes coherent Bruin-compatible asset states.
```

This solves the browser-refresh problem without forcing every keystroke to trigger full metadata regeneration.

### 10.1 Draft persistence

Every edit is persisted immediately to a draft journal.

Possible storage:

```text
- Browser IndexedDB
- Server-side workspace draft store
- Local .renart/drafts ignored by Git
```

Drafts contain volatile editing state:

```json
{
  "assetPath": "assets/marts/orders.sql",
  "sqlBuffer": "select ...",
  "pendingTransactions": [
    {
      "type": "column.check.add",
      "column": "order_id",
      "check": "not_null"
    }
  ],
  "updatedAt": "2026-06-23T10:00:00Z"
}
```

On reload:

```text
If draft is newer than file
→ restore draft
→ show “Recovered unsaved browser state”
→ continue canonical reconciliation
```

### 10.2 Canonical file persistence

The Bruin file is written automatically when Renart can produce a coherent asset file.

Coherent means:

```text
- Bruin block can be parsed/rendered
- SQL body can be safely preserved
- metadata transaction is semantically valid enough to serialize
- no user-authored metadata is being silently destroyed
```

SQL can still be temporarily invalid while typing. In that case:

```text
Persist SQL body.
Keep last known good generated metadata.
Pause dependency/column regeneration until SQL parses again.
```

### 10.3 Autosave behavior by edit type

| Edit type                            | Draft write |                                  Canonical write |
| ------------------------------------ | ----------: | -----------------------------------------------: |
| SQL keystroke                        |   immediate | update SQL body, defer inference if parser fails |
| Add column check                     |   immediate |              immediate semantic YAML transaction |
| Change materialization               |   immediate |              immediate semantic YAML transaction |
| Parser finds new dependency          |         n/a |                    auto-apply if non-destructive |
| Parser loses dependency              |         n/a |                remove only if checksum says safe |
| Column disappears with user metadata |         n/a |                       create reconciliation item |
| Expert YAML edit                     |   immediate |         write if parseable, otherwise draft-only |

---

## 11. Transaction-based persistence

UI components should never directly write YAML.

Everything emits semantic transactions:

```ts
type AssetTransaction =
  | { type: "sql.changed"; sql: string }
  | { type: "dependency.manual.add"; dependency: BruinDependency }
  | { type: "dependency.manual.remove"; key: string }
  | { type: "dependency.inferred.ignore"; key: string }
  | { type: "dependency.inferred.restore"; key: string }
  | { type: "column.manual.add"; column: ColumnDefinition }
  | { type: "column.inferred.drop"; column: string }
  | { type: "column.field.own"; column: string; field: string }
  | { type: "column.check.add"; column: string; check: BruinCheck }
  | { type: "column.description.set"; column: string; description: string }
  | { type: "materialization.set"; materialization: BruinMaterialization }
  | { type: "raw_yaml.patch"; patch: YamlPatch }
```

Canonical write flow:

```ts
async function persistAssetTransaction(tx: AssetTransaction) {
  const file = await readAssetFile(tx.path)

  const parsed = parseAssetFile(file)
  const currentBruin = parseBruinDefinition(parsed.header)
  const currentRenart = currentBruin.meta?.renart ?? {}

  const editedBruin = applyTransaction(currentBruin, tx)
  const inference = tryInferFromSql(parsed.sql)

  const reconciled = reconcile({
    bruin: editedBruin,
    renart: currentRenart,
    inference,
  })

  const nextHeader = renderBruinHeaderStable(reconciled)
  const nextFile = replaceBruinHeader(file, nextHeader)

  await atomicWriteFile(tx.path, nextFile)
}
```

This gives you a single enforcement layer for ownership, checksums, formatting, validation, and autosave.

---

## 12. UI concept

Renart should expose three editing modes:

```text
Guided mode
Expert YAML mode
Raw / detached mode
```

Most users should start in Guided mode. Power users should never feel trapped.

---

## 13. Guided mode

Guided mode should not show the full `@bruin` block by default.

Recommended layout:

```text
┌──────────────────────────────────────────────────────────────┐
│ marts.orders_enriched  bq.sql  table: merge  3 deps  4 cols │
└──────────────────────────────────────────────────────────────┘

┌──────────────────────── SQL editor ──────────────────────────┐
│ select                                                       │
│   o.id as order_id,                                          │
│   o.customer_id,                                             │
│   o.total_after_discounts as order_total                     │
│ from raw.orders o                                            │
│ join raw.customers c on c.id = o.customer_id                 │
└──────────────────────────────────────────────────────────────┘

┌ Identity ┐ ┌ Materialization ┐ ┌ Dependencies ┐ ┌ Columns ┐
│ bq.sql   │ │ table / merge   │ │ 2 inferred   │ │ 3 inf.  │
│ inferred │ │ configured      │ │ 1 manual     │ │ 1 manual│
└──────────┘ └─────────────────┘ └──────────────┘ └─────────┘
```

The header chips act as navigation:

```text
bq.sql           → Identity card
table: merge    → Materialization card
3 deps          → Dependencies card
4 cols          → Column grid
```

---

## 14. Focused cards

Renart should use focused metadata cards instead of one giant form.

### 14.1 Identity card

Purpose:

```text
Asset name, type, connection, owner, tags, domains.
```

Suggested UI:

```text
Asset type
[bq.sql ▼]

Asset name
marts.orders_enriched
Source: inferred from file path
[Set explicit name]

Owner
[analytics-team@company.com]

Tags
[finance] [daily] [+]
```

If Bruin name inference is used, Renart can avoid writing `name` unless an explicit override is needed.

### 14.2 Materialization card

Purpose:

```text
Choose write behavior by intent, not by raw YAML fields.
```

Suggested UI:

```text
How should this asset write data?

( ) View
( ) Recreate table
( ) Append rows
(●) Merge by primary key
( ) Incremental by time interval
( ) SCD2 history
( ) DDL-only
```

For merge:

```text
Primary key
[order_id ✓]

Update on merge
[customer_id ✓] [order_total ✓]
```

Generated Bruin YAML remains visible through preview or Expert mode.

### 14.3 Dependencies card

Purpose:

```text
Show inferred, manual, ignored, and stale dependencies separately.
```

Suggested UI:

```text
Inferred from SQL
✓ raw.orders       [Ignore]
✓ raw.customers    [Ignore]

Manual dependencies
◇ analytics.date_spine  symbolic  [Remove]

Ignored inferred dependencies
⊘ scratch.tmp_dates     [Restore]
```

Actions:

```text
Add manual dependency
Ignore inferred dependency
Restore ignored dependency
Switch full/symbolic mode
Open lineage view
```

### 14.4 Columns card

Purpose:

```text
A spreadsheet-like column workbench.
```

Suggested UI:

```text
name          type       pk   checks             description
order_id      integer    ✓    not_null, unique   —
customer_id   integer         not_null           —
order_total   numeric         —                  Total after discounts
loaded_at     timestamp       —                  Technical load timestamp
```

Column status markers:

```text
Inferred
Manual
Stale
Type overridden
Needs reconciliation
```

Bulk actions:

```text
Add not_null to selected
Mark as primary key
Generate descriptions
Detach type ownership
Map stale column to new column
Remove stale columns
```

### 14.5 Quality checks card

Purpose:

```text
Column checks and custom checks.
```

Suggested UI:

```text
Column checks
order_id      not_null, unique
order_total   positive

Custom checks
[+] Add custom SQL check
```

Custom checks should use a small SQL editor, not a long text input.

### 14.6 Hooks card

Purpose:

```text
Pre/post SQL snippets.
```

Suggested UI:

```text
Pre-hooks
[SQL editor card]

Post-hooks
[SQL editor card]
```

### 14.7 Advanced/meta card

Purpose:

```text
Expose advanced Bruin fields without cluttering the beginner flow.
```

Contains:

```text
retries
rerun_cooldown
interval modifiers
routing
custom meta except meta.renart
```

---

## 15. Expert YAML mode

Expert mode shows the actual Bruin YAML block.

It should be AST-aware, not just a raw textarea.

Features:

```text
- YAML schema validation
- autocomplete for Bruin fields
- inline diagnostics
- field ownership indicators
- generated-field decorations
- semantic diff before destructive changes
- actions such as “Take ownership”, “Ignore inferred”, “Restore generated”
```

Example presentation:

```yaml
depends:
  - raw.orders
  - raw.customers
  - asset: analytics.date_spine
    mode: symbolic
```

Hover on `raw.orders`:

```text
Inferred from SQL AST.
Change the SQL or choose “Ignore inferred dependency”.
```

Hover on `analytics.date_spine`:

```text
Manual dependency.
Stored in meta.renart.d.add.
```

Generated fields should not be hard-blocked only through Monaco ranges. Monaco can guide the user visually, but the reconciler must enforce ownership and prevent accidental destructive writes.

---

## 16. Raw / detached mode

Raw mode is the escape hatch.

User intent:

```text
I know what I am doing. Let me edit the asset directly.
```

Recommended behavior:

```text
- Show full file
- Preserve syntax highlighting
- Still parse and validate when possible
- Stop overwriting detached paths
- Continue displaying lineage and diagnostics where possible
```

Detachment should be granular:

```text
Detach this field
Detach columns.type
Detach dependencies
Detach entire asset from Renart management
```

Detached assets can still be read, visualized, and run. Renart simply stops claiming ownership.

---

## 17. Reconciliation UI

Reconciliation should be shown only when Renart cannot safely infer user intent.

Example: column disappeared but had metadata.

```text
Column no longer found in SQL

order_total
- description: Total order amount after discounts.
- type: numeric

Possible actions:
[Map to total_amount]
[Keep as manual column]
[Remove column metadata]
```

Example: external YAML edit detected.

```text
External metadata edit detected

raw.fraud_rules exists in depends but was not inferred from SQL
and was not tracked as a manual dependency.

Default action:
Adopt as manual dependency
```

This preserves user work and keeps autosave non-hostile.

---

## 18. Semantic diff

Before applying non-trivial generated changes, Renart should show a semantic diff rather than a raw YAML diff.

Example:

```text
SQL analysis changed dependencies

Added
+ raw.payments

Removed
- raw.customers

Reason
raw.payments is now referenced in the query.
raw.customers is no longer referenced.

Actions
[Apply] [Ignore raw.payments] [Keep raw.customers manually]
```

For most safe additions, Renart can apply automatically and merely show a small “updated metadata” notification.

---

## 19. Git-friendly rendering rules

Renart should render Bruin YAML deterministically.

Recommended order:

```yaml
name
type
uri
connection
owner
tags
domains
enabled
depends
materialization
columns
custom_checks
hooks
retries
rerun_cooldown
interval_modifiers
routing
meta
```

Dependency order:

```text
1. inferred dependencies in SQL appearance order
2. manual dependencies in user-defined order
```

Column order:

```text
SELECT-list order
then manual columns
then stale/manual preserved columns
```

Metadata rules:

```text
- no timestamps in committed meta.renart
- no cursor positions
- no panel sizes
- no UI tab state
- no full generated snapshots unless absolutely needed
- stable hash inputs
- stable scalar formatting
```

UI-only layout state can live outside the asset file, for example in workspace state or an ignored Renart draft directory. Core semantic state belongs in the Bruin asset.

---

## 20. Suggested internal architecture

```text
┌──────────────────────┐
│ Monaco SQL editor     │
└──────────┬───────────┘
           │ sql.changed
           ▼
┌──────────────────────┐
│ SQL parser / analyzer │
└──────────┬───────────┘
           │ inference result
           ▼
┌──────────────────────┐
│ Asset reconciler      │
└──────────┬───────────┘
           │ final Bruin model
           ▼
┌──────────────────────┐
│ Stable YAML renderer  │
└──────────┬───────────┘
           │ atomic write
           ▼
┌──────────────────────┐
│ Bruin asset file      │
└──────────────────────┘
```

All UI surfaces emit transactions into the same reconciler:

```text
SQL editor
Focused cards
Column grid
Dependency card
Materialization wizard
Expert YAML mode
Command palette
```

No surface owns the file directly.

---

## 21. Command palette

For developers, add command-driven metadata editing.

Examples:

```text
Add manual dependency
Ignore inferred dependency
Mark column as primary key
Add not_null check
Convert to merge materialization
Detach column type
Open Expert YAML
Show generated changes
Run Bruin validation
```

This gives power users speed without forcing everyone into raw YAML.

---

## 22. Product behavior summary

Renart should feel like this:

```text
Write SQL.
Renart infers dependencies and columns.
Metadata appears as focused, editable cards.
Generated fields are visible but not noisy.
Manual intent is preserved.
Every edit is autosaved.
The file remains Bruin-native.
Git diffs stay clean.
Expert users can drop into YAML whenever needed.
```

The core design decision:

```text
Renart does not store a huge generated state.
Renart stores the final Bruin definition plus compact exceptions.
```
