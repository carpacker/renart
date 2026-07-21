# Connection-driven asset creation

Status: proposed — current creation, connection, capability, and runtime paths
have been investigated; not yet implemented

## Goal

Make the New asset flow ask users for concepts they actually choose:

1. **What are you building?** SQL, Python, HTTP API, Load, Seed, or Sensor.
2. **Where does it read/write/check?** A configured connection (two roles for
   Load).

The concrete Bruin asset type and SQL dialect then follow from the connection.
A user choosing `warehouse-prod` (Postgres) should see a read-only summary such
as `PostgreSQL · pg.sql`; they should never also have to choose PostgreSQL from
a second dialect list.

The result must remain a normal, explicit Bruin file. Renart derives the type at
creation time and persists `type: pg.sql`; it does not introduce a Renart-only
dynamic asset type.

## Recommendation in one sentence

Keep the six equal-size intent choices, make connection the primary second
step, derive the concrete Bruin type/dialect server-side, and reuse one nested
New connection dialog whose available connection types are filtered by the
role's backend-owned compatibility profile.

## Current state and concrete problems

### Creation UI

`NewAssetDialog` currently presents six equal-size choices and then assembles
kind-specific fields:

- SQL, HTTP API, and Load have a target/destination connection select;
- Load also has a source connection and source object;
- Python creation does not currently have a target connection select, although
  existing Python assets expose the shared target editor later;
- Seed and Sensor use the backend's semantic capability list;
- `Auto (pipeline default)` is shown even when the selected kind cannot resolve
  a compatible unambiguous default.

The connection lists come from two different client models: the workspace
summary (`name -> type`) and a client-side Load category helper. There is no one
creation compatibility contract.

### SQL correctness bug

`buildCreateAssetInput` defaults every SQL asset to `duckdb.sql`. Selecting a
Postgres/Snowflake/etc. connection only sets `connection`; it does not derive or
validate the corresponding SQL asset type. This can leave the parser,
materializer, typecheck dialect, and actual connection disagreeing.

The backend already has most of the necessary primitives:

- Bruin's `pipeline.AssetTypeConnectionMapping`;
- `queryAssetTypeForConnectionType` and `sqlAssetTypeForConnectionType`;
- `AssetTypeToDialect` for Renart's supported SQL intelligence;
- explicit seed/sensor authoring capabilities;
- curated Load database/storage/file connection categories;
- canonical effective target resolution for SQL, Python, API, and Load assets.

They are not yet composed into a general authoring profile, and `Create` trusts
the client-supplied concrete `type` too much.

### New connection UI

Project settings already has the right building blocks:

- `useWorkspaceConnectionForm` owns draft/reset/save behavior;
- `WorkspaceConnectionFormFields` renders environment, name, type, dynamic
  values, and validation;
- `useWorkspaceSettingsData` owns config mutation and cache refresh.

The composition is hard-wired into `ConnectionSheet`. The asset dialog should
reuse the form and mutation contract, not embed settings or navigate the user
away from an unfinished asset.

## UX model

### Axis 1: authoring intent

Keep these as the top-level choices:

| Intent | User mental model | Persisted Bruin type |
| --- | --- | --- |
| SQL | Transform with a query | derived, e.g. `duckdb.sql`, `pg.sql`, `sf.sql` |
| Python | Custom Python transform | `python` |
| HTTP API | Fetch records and load them | Renart `api` |
| Load | Replicate between connections | Renart `load` |
| Seed | Load a versioned file | derived, e.g. `duckdb.seed`, `pg.seed` |
| Sensor | Wait for a query/table/key condition | derived by connection + condition variant |

Seed and Sensor should stay top-level. Although they can be platform-specific,
they have different source/condition forms and execution semantics; hiding them
under SQL would make the first choice less truthful.

Ingestr remains a feature-gated legacy/source option outside the default six.
Source assets such as `pg.source` are discovered/imported relations, not a
seventh transform choice.

### Axis 2: connection role

After intent and name, put the relevant connection field before advanced
details:

- SQL, Python, HTTP API, Seed: **Target connection**;
- query/table Sensor: **Connection to check**;
- S3 key Sensor: **AWS/S3 connection**;
- Load: **Source connection** and **Destination connection**.

The connection row shows:

- configured name;
- friendly engine/type badge;
- `Pipeline default` marker when applicable;
- a short incompatibility reason only when preserving an invalid current value
  during repair.

Below it, show a static derived summary rather than another selector:

```text
SQL dialect       PostgreSQL
Bruin asset type  pg.sql
```

For generic Python/API/Load types, the summary names the operator and target
family instead (`Python -> PostgreSQL table via Sling`).

### Connection picker footer

Every connection picker ends with a separator and:

```text
+ New connection…
Manage connections…
```

`New connection…` opens a nested, wide shadcn Dialog. It keeps the New asset
dialog and all entered fields mounted underneath. The dialog:

- is locked to the currently selected environment;
- only lists connection types compatible with the role that opened it;
- reuses `WorkspaceConnectionFormFields` and the existing Verify action;
- saves through the Go config API;
- closes and selects the new connection after a successful save;
- clearly says that credentials are written to the project configuration for
  this environment.

`Manage connections…` may navigate to Project settings only after an explicit
choice because that abandons or suspends the creation flow.

Do not put a complex form directly inside Select content. Use a command-style
connection picker or treat the footer item as an action that closes the popover
before opening the dialog; this avoids nested focus/portal bugs.

### Auto / pipeline default

Replace vague `Auto` with a resolved option whenever possible:

```text
Pipeline default — warehouse-dev (DuckDB)
```

The backend returns one of:

- `resolved`: selectable, with concrete connection, type, and dialect;
- `ambiguous`: disabled, explain which defaults conflict;
- `missing`: disabled, offer New connection;
- `incompatible`: disabled, explain why the pipeline default cannot serve this
  intent.

Persist an empty asset-level `connection` only for a resolved default. Persist
the concrete derived asset type regardless, because Bruin needs it for parsing,
default-connection lookup, and execution.

If a pipeline default later changes to another engine family, the existing
asset must not silently change dialect. Typecheck should report the mismatch and
offer an explicit type/connection migration.

### Downstream and ad-hoc defaults

- New downstream SQL/Python defaults to the source asset's effective target
  connection when compatible, then falls back to the pipeline default.
- A Load downstream uses the source asset's target as its source connection and
  asks for the destination.
- Convert ad-hoc query to asset preselects the ad-hoc editor's current
  connection. Its SQL dialect and persisted type must match the environment in
  which the query was authored.

## Backend-owned compatibility contract

### Why the backend must own it

Compatibility is not merely a frontend display rule. It is the intersection of:

1. a Bruin type/connection mapping;
2. a Renart direct execution operator;
3. a Renart render/typecheck/intellisense dialect;
4. kind-specific target semantics (relation versus object/file);
5. the connection's availability in the selected environment.

For example, Bruin exposes more `.sql` asset mappings than Renart's current
`AssetTypeToDialect` map. Those engines should not appear as fully supported SQL
creation choices until the editor and typecheck paths can honor them. Deriving
the list in TypeScript from a name suffix would over-promise.

### Proposed endpoint/profile

Add a pipeline- and environment-scoped read endpoint, for example:

```text
GET /api/pipelines/{pipelineID}/asset-creation-profile?environment=default
```

The response is value-only and secret-free:

```json
{
  "environment": "default",
  "kinds": [
    {
      "kind": "sql",
      "roles": [{
        "role": "target",
        "allow_default": true,
        "connections": [{
          "name": "warehouse-dev",
          "connection_type": "postgres",
          "compatible": true,
          "asset_type": "pg.sql",
          "dialect": "postgres"
        }],
        "default": {
          "status": "resolved",
          "connection": "warehouse-dev",
          "connection_type": "postgres",
          "asset_type": "pg.sql",
          "dialect": "postgres"
        }
      }]
    }
  ],
  "creatable_connection_types": []
}
```

`creatable_connection_types` reuses the current reflected config field
definitions but annotates compatible roles. No credential values enter this
endpoint.

Generalize the existing seed/sensor `AssetAuthoringCapability` rather than
creating a second permanent capability system. During migration, keep the old
fields in the workspace DTO until all semantic editor consumers use the new
profile.

### Server-derived create request

Introduce a v2 semantic request:

```json
{
  "name": "analytics.orders",
  "kind": "sql",
  "connection": "warehouse-dev",
  "use_pipeline_default": false,
  "executable_content": "select ..."
}
```

The server resolves the selected environment, verifies compatibility, derives
the concrete type/dialect, and renders the canonical file. Load additionally
carries source/destination roles; Seed/Sensor carry their existing variant
fields.

Keep the current `type` request for backward-compatible internal/CLI callers,
but validate that it matches the effective connection. A client must not be
able to submit `duckdb.sql` plus a Postgres connection and create an internally
inconsistent asset.

Return the effective `asset_type`, `connection`, and `dialect` in the mutation
response so the UI and tests can verify what was persisted without waiting for
SSE. Filesystem state still wins when the workspace update arrives.

## Compatibility matrix

The profile should be generated from backend capability definitions. This
matrix describes the intended policy, not a second hard-coded implementation:

| Intent / role | Compatible connection family | Derived behavior | Explicit exclusions |
| --- | --- | --- | --- |
| SQL target | Warehouse connection with a query asset mapping **and** supported Renart dialect/operator | concrete `<engine>.sql` + matching Monaco/LSP dialect | local file, S3/GCS/SFTP, SaaS/source-only connections; mapped engines missing Renart intelligence stay unavailable with a reason |
| Python target | Load-capable database destination | generic `python`, returned table loaded to the asset relation (DuckDB direct; others Sling) | local/file/object storage because Python has no destination-object contract; SaaS sources |
| HTTP API target | Load-capable database destination | generic `api`, JSONL staged then Sling-loaded to the asset relation | local/file/object storage until API assets gain an explicit destination-object contract; SaaS sources |
| Load source | Load database, storage, file, or synthetic `local` | Sling connection/stream; source object required | SaaS connections without a Sling-compatible data-store contract |
| Load destination | Load database, storage, file, or synthetic `local` | relation name for databases; destination object/path for storage/file/local | SaaS destinations |
| Seed target | Connection type represented by a directly executable Seed capability | concrete `<engine>.seed` | engines absent from the seed capability list; local is a source path, never the target connection |
| Query Sensor | Connection type represented by a query-sensor capability | concrete `<engine>.sensor.query` and matching SQL dialect | storage/file/SaaS; engines without Renart query-sensor support |
| Table Sensor | Connection type represented by a table-sensor capability | concrete `<engine>.sensor.table` | engines with query-only sensors |
| Key Sensor | AWS/S3-compatible capability | `s3.sensor.key_sensor` | all unrelated types |

The profile should expose capability levels, not pretend every Bruin mapping is
equal:

- `supported`: create, edit, typecheck/render, and execute are covered;
- `read_only`: an existing hand-authored type can be displayed/repaired but not
  newly offered;
- `unsupported`: hidden from ordinary creation, with a backend reason available
  for diagnostics.

This gives Dremio/Sail/StarRocks/Doris and future Bruin additions a deliberate
path into the selector instead of silently inheriting partial support.

## Connection identity and environments

The SQL type is committed once and cannot vary by environment. Therefore:

- editing credentials, host, database, or schema for a connection is safe;
- changing a connection's **type** in place is an identity-changing operation
  that can invalidate every referencing asset;
- the ordinary connection edit dialog should make Type read-only. Users create
  a new connection (or enter a dedicated migration flow with impact review) to
  change it;
- when the same connection name exists in several environments, its canonical
  type must be consistent anywhere a typed asset can run;
- a connection missing from another environment is allowed during authoring,
  but deployment/typecheck for that environment blocks with a clear missing
  connection finding.

Creating a connection from New asset adds it only to the selected environment.
The success message should name that environment; automatic credential cloning
would be surprising and unsafe.

## Existing-asset editing

Creation and editing must share the same invariant:

- changing a SQL/Seed/Sensor connection within the same connection type is an
  ordinary metadata edit;
- selecting a different engine is an explicit type migration that updates
  `type` and `connection` atomically after confirmation and reruns
  format/typecheck/materialization capability resolution;
- Python/API/Load can switch among compatible target connections without a
  concrete type change, but their materialization capability profile is
  recomputed;
- an unknown or now-incompatible hand-authored value remains visible for repair
  and is never silently cleared.

The guided editor should consume the same creation profile resolver with the
current asset included as a preserved option. Avoid a separate editor-only
connection filter.

## Component design

Recommended frontend pieces:

- `AssetKindPicker`: the existing equal-size cards and compact selected summary;
- `AssetConnectionField`: command/select trigger, compatibility badges, resolved
  default, New connection footer;
- `WorkspaceConnectionDialog`: reusable Dialog composition around
  `useWorkspaceConnectionForm` + `WorkspaceConnectionFormFields`;
- `DerivedAssetTypeSummary`: static dialect/concrete type/operator copy;
- kind-specific detail components for API source, Load mapping, Seed, and Sensor.

`NewAssetDialog` remains the coordinator but stops calculating engine/type
compatibility itself. Keep one form state; do not introduce a second persistent
frontend source of truth. After config mutation, update the existing settings
atom and let workspace SSE reconcile the authoritative file state.

On mobile, the kind cards stay two columns and the connection dialog becomes a
full-width, height-bounded dialog with its own `ScrollArea`. Preserve the
existing focus-ring padding around ScrollArea content.

## Delivery phases

### Phase 1 — capability and correctness boundary

1. Add one backend compatibility registry generated from Bruin mappings plus
   Renart operator/intelligence support.
2. Add the pipeline/environment creation-profile endpoint and tests.
3. Add semantic create resolution and mismatch rejection for the legacy
   `type` request.
4. Add connection-type consistency diagnostics across environments.

This phase should land before the UI changes so no new flow can create a type /
connection mismatch.

### Phase 2 — reusable connection UI

1. Extract `WorkspaceConnectionDialog` from the settings-only Sheet
   composition without duplicating form logic.
2. Add `AssetConnectionField` with compatible results, resolved default, and
   footer actions.
3. Save a new connection in the active environment, verify optionally, refresh
   config state, and select it while preserving the asset draft.
4. Make connection type immutable in ordinary edit mode; add impact diagnostics
   for existing cross-type configurations.

### Phase 3 — new creation flow

1. Replace client-derived lists with the server profile for all six intents.
2. Add Python target selection and static derived summaries.
3. Make SQL/Seed/Sensor type derivation connection-driven.
4. Resolve and label Pipeline default accurately.
5. Carry the ad-hoc editor connection into Convert to asset and prefer the
   source connection for downstream creation.
6. Remove the `preferredSqlAssetType = "duckdb.sql"` fallback from ordinary UI
   creation.

### Phase 4 — editing and cleanup

1. Use the same profile in guided existing-asset connection editors.
2. Add the explicit cross-engine migration transaction.
3. Remove duplicated client Load/semantic filtering after every consumer uses
   the backend profile.
4. Fold the as-built UI/backend contract into architecture docs and delete this
   plan.

## Test plan

- backend table tests for every supported kind/connection pair and every
  exclusion family;
- a guard that every offered SQL type has a Renart dialect, formatter,
  materializer/render path, and compatible Bruin connection mapping;
- property/table test that server-derived type maps back to the selected
  canonical connection type;
- default resolution: one default, multiple ambiguous defaults, missing
  default, incompatible default;
- selected environment and same-name cross-environment type mismatch;
- spoofed `duckdb.sql` + Postgres legacy request is rejected;
- component tests for filtering, derived summary, current-invalid preservation,
  and nested-dialog focus restoration;
- live E2E: create and run DuckDB and Postgres SQL assets, create a Python/API
  target, local-to-database and database-to-local Loads, create/select a new
  connection without losing the draft, and convert an ad-hoc query while
  retaining its connection;
- mobile live E2E for scrolling/focus and the New connection dialog.

## Open questions

1. **Four versus six top-level choices:** should Seed/Sensor move into a later
   specialized menu? Recommendation: keep six; they are distinct user intents,
   while dialect is the axis to remove.
2. **Connection type mutability:** disable Type for existing connections or
   build an impact-reviewed migration immediately? Recommendation: disable in
   ordinary edit mode and add migration later.
3. **Partially supported Bruin engines:** show disabled rows or hide them?
   Recommendation: hide in ordinary creation, preserve and explain them when an
   existing asset already uses one.
4. **Python/API object targets:** should they gain `destination_object` and use
   storage/local destinations? Recommendation: not in this selector change;
   add the runtime/physical-target contract first.
5. **Auto across environments:** validate only the selected environment at
   creation or require every environment to be ready? Recommendation: create in
   the selected environment, show portability warnings, and enforce the chosen
   environment at deploy/run review.
6. **New connection breadth:** should a role-filtered dialog offer an `All
   connection types` escape hatch? Recommendation: no in-context escape hatch;
   Manage connections opens the full settings surface.
7. **Ad-hoc conversion with an unsupported connection:** block conversion or
   fall back to Pipeline default? Recommendation: keep the selected connection,
   show the incompatibility, and require an explicit user choice—never change
   dialect silently.

## Completion

When implemented, document the backend capability/validation contract in
`architecture/backend.md`, the creation/editing behavior in
`architecture/asset-editing.md`, and the reusable dialog/state flow in
`architecture/frontend.md`; then delete this plan.
