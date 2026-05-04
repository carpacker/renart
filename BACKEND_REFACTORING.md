# Backend Refactoring Notes

This document tracks backend cleanup opportunities found in the Renart Go service layer. The focus is duplication, hand-rolled logic that overlaps with Bruin packages, and patterns that make future product work riskier than necessary.

## Summary

The backend has grown around practical UI flows, but several responsibilities are now mixed together:

- Asset CRUD, inferred naming, SQL dependency inference, SQL patch scheduling, downstream asset generation, and asset rename refactoring all live in `internal/web/service/asset.go`.
- Direct execution, direct query, database import helpers, column introspection, output formatting, run formatting, and executor wiring all live in `internal/web/service/direct_executor.go`.
- Inspect/materialization, freshness, database object discovery, query output parsing, and DuckDB lock handling all live in `internal/web/service/execution.go`.
- Several services manually duplicate Bruin behavior for asset type/connection type resolution, config loading, pipeline discovery, connection manager creation, renderer setup, and file/comment mutation.

The most important refactoring direction is to make Renart thinner around Bruin primitives, especially where Bruin already has canonical asset type, connection, pipeline builder, scheduler, and query extraction behavior.

## High-Priority Findings

### 1. Connection Type And Asset Type Mapping Is Duplicated

Current locations:

- `internal/web/service/asset.go`
- `internal/web/service/direct_executor.go`
- Bruin upstream package: `github.com/bruin-data/bruin/pkg/pipeline`
- Bruin upstream package: `github.com/bruin-data/bruin/pkg/helpers`
- Bruin upstream package: `github.com/bruin-data/bruin/pkg/connection`

Examples:

- `asset.go` defines `sqlAssetTypeForConnectionType`, manually mapping connection type strings to query asset types.
- `asset.go` defines `sqlAssetTypeForIngestrDestination`, even though Bruin already exposes `pipeline.IngestrTypeConnectionMapping` and `helpers.GetIngestrDestinationType`.
- `direct_executor.go` defines `determineDirectAssetType` by inspecting Go concrete type strings and connection names.
- `direct_executor.go` defines `convertDirectSourceTypeToQueryType`, duplicating source-to-query asset type mapping that can be derived from Bruin's `pipeline.AssetTypeConnectionMapping` plus known query asset types.

Risk:

- New Bruin connection or asset types must be added in multiple Renart locations.
- Alias handling can drift from Bruin behavior. For example, `bigquery`, `google_cloud_platform`, and `gcp` aliases are currently handled ad hoc.
- `fmt.Sprintf("%T", conn)` based detection is brittle and can break when Bruin changes client structs.

Recommended direction:

- Introduce a small Renart adapter around Bruin's `pipeline.AssetTypeConnectionMapping` and `pipeline.IngestrTypeConnectionMapping`.
- Prefer `config.ConnectionAndDetailsGetter.GetConnectionType(name)` where available instead of inferring connection type from implementation names.
- Keep only UI-specific fallback/default behavior in Renart, such as defaulting to `duckdb.sql` when no pipeline context exists.

### 2. Config Loading And Environment Selection Are Repeated

Current locations:

- `execution.go`: `loadExecutionConfigOrEmpty`, DuckDB read-only config creation.
- `direct_executor.go`: `RunPipeline`, `getDirectPipelineAndAsset`, `getDirectConnectionAndQuery`, `buildDirectAssetQuery`, `directConnectionManager`.
- `onboarding.go`: `PreviewDiscovery`, quickstart config mutation.

Repeated patterns:

- Load `.bruin.yml` with `config.LoadOrCreate`.
- Default selected environment from default environment.
- Select a requested environment if provided.
- Create `connection.NewManagerFromConfigWithContext` and unwrap the first error.

Risk:

- Different flows select environments differently.
- DuckDB lock/read-only inspect can use a different selected environment than direct run/query.
- Tests need to duplicate setup for each path because there is no single helper to exercise.

Recommended direction:

- Add one internal helper that loads config and selects an environment consistently.
- Add one helper that creates a connection manager from a selected config and returns a normal `error`.
- Use the helper in execution, direct executor, onboarding discovery, and Jinja rendering where appropriate.

### 3. Pipeline And Asset Resolution Is Reimplemented In Several Places

Current locations:

- `workspace.go` and workspace coordinator resolution callbacks.
- `direct_executor.go`: `getDirectPipelineAndAsset`, `RunPipeline`, direct target resolution.
- `execution.go`: materialization state rebuilds pipeline from ID.
- `asset.go`: manually validates pipeline roots and derives pipeline-relative asset paths.
- Tests contain repeated local `ResolveAssetByID` functions.

Risk:

- Inferred asset names and nested asset paths are easy to break because every resolver must agree on path semantics.
- Tests use simplified resolvers that can diverge from production behavior.

Recommended direction:

- Introduce a shared `WorkspaceResolver` or `PipelineResolver` service with methods for:
  - decode pipeline/asset IDs,
  - safe-join paths,
  - build mutated pipelines,
  - find an asset by file path,
  - return relative workspace paths.
- Move tests to the same resolver where practical.

### 4. Asset Name Inference And File-Path Logic Are Embedded In Asset CRUD

Current locations:

- `asset.go`: `assetPathForInferredName`, `assetNameLeafPath`, `inferredAssetNameFromPath`, `pipelineRelPathForAsset`, explicit-name stripping.
- Frontend mirrors some path construction in `web/lib/workspace-shell-helpers.ts`.

Risk:

- Backend and frontend can disagree on where a new asset should be created.
- Rename is especially fragile because inferred names require file moves, while explicit names require metadata edits.
- SQL dependency reconciliation currently persists through Bruin and then removes generated `name:` fields when the source asset relied on inference.

Recommended direction:

- Extract inferred-name path functions into a focused Go file with tests.
- Make asset rename operate on a small explicit model: `explicit-name asset` versus `inferred-name asset`.
- Consider returning backend-generated path suggestions to the frontend instead of duplicating path construction in TypeScript.

### 5. Direct Executor Recreates A Large Part Of Bruin Executor Wiring

Current location:

- `internal/web/service/direct_executor.go`

Examples:

- `buildDirectMainExecutors` manually clones `bruinexecutor.DefaultExecutorsV2` and overrides many operators.
- Direct run loops over scheduler task instances and lifecycle output manually.
- Direct query uses custom extraction/render setup.

Risk:

- Renart can drift from Bruin CLI execution semantics.
- Adding a Bruin asset type or changing default executor behavior requires Renart changes.
- The file is too large and tightly coupled to all database packages.

Recommended direction:

- Prefer a Bruin-provided execution service or a smaller exported hook in Bruin for embedded/direct execution.
- Until Bruin exposes that, split Renart code into narrower files:
  - direct run orchestration,
  - direct query,
  - executor registry overrides,
  - import/discovery helpers,
  - formatting/output.
- Keep explicit CLI fallback boundaries documented in code.

### 6. SQL Query Result JSON Formatting Is Duplicated

Current locations:

- `direct_executor.go`: `QueryAsset`, `QueryConnection`, `formatQueryRowsForJSON`, `formatQueryJSONValue`.
- `execution.go`: `ParseQueryJSONOutput`, `extractColumnNames`, `castRows`, `castRowsByColumns`, `inferColumns`.

Risk:

- Direct query and inspect parsing can disagree about output envelope formats.
- JSON shape changes require edits in multiple files.

Recommended direction:

- Create one `QueryResultDTO` and conversion helper that maps Bruin `query.QueryResult` to the Renart API JSON envelope.
- Keep parsing of legacy/envelope outputs in one file.

### 7. SQL Dependency Reconciliation Is Intertwined With Asset Persistence

Current location:

- `asset.go`

Examples:

- `reconcileSQLAssetDependencies` mutates upstreams, persists, and then removes `name:` if the file used name inference.
- Manual/inferred upstream merge logic is local and hard to reuse.
- Rename refactoring updates SQL text, upstream metadata, persists changed assets, then reruns reconciliation.

Risk:

- Persistence side effects are hard to reason about.
- Name inference introduced a post-persist cleanup path because Bruin persistence writes explicit names.

Recommended direction:

- Split dependency inference/merge from persistence.
- Introduce tests around the pure merge behavior separately from filesystem persistence.
- Longer term, coordinate with Bruin so `Asset.Persist` can preserve inferred-name files without writing `name:`.

### 8. Error Response Types Are Per-Service And Map-Based

Current locations:

- `AssetAPIError` in `asset.go`.
- `OnboardingImportResult` with `HTTPCode`.
- Various `map[string]string{"status": "ok"}` responses.

Risk:

- API handlers need custom translation per service.
- Response shape is hard to evolve and hard to statically test.

Recommended direction:

- Introduce a shared service error type with status/code/message.
- Replace common success maps with typed response structs.

## Medium-Priority Findings

### 9. SQL Object Discovery Is Generic But Located In Execution Service

Current location:

- `execution.go`: `fetchObjectsForConnection`, `fetchRowCountsForObjects`, `DBObjectInfo`.

Risk:

- Discovery behavior is tied to materialization state, but onboarding/import also needs discovery.
- Queries use generic `information_schema` and `SHOW TABLES`, while Bruin connections may expose better database-specific methods.

Recommended direction:

- Move DB object discovery to a separate helper.
- Prefer Bruin connection discovery interfaces when available.

### 10. Filesystem Usage Is Mostly `afero`, But Services Still Instantiate OS FS Directly

Current locations:

- `asset.go`, `execution.go`, `onboarding.go`, `direct_executor.go`.

Risk:

- Unit tests need temp dirs and real filesystem effects.
- Browser/WASM or virtual filesystem work becomes harder.

Recommended direction:

- Inject `afero.Fs` into service dependencies for services that write files.
- Keep `afero.NewOsFs()` only in composition/root wiring.

### 11. Onboarding Quickstart Is Hard-Coded Inside Service Logic

Current location:

- `onboarding.go`: quickstart YAML/SQL/Python content and file layout.

Risk:

- Quickstart changes require editing service code and test fixtures independently.
- Existing static `onboarding/quickstart` files can drift from generated files.

Recommended direction:

- Either use embedded template files as the source of truth or remove the separate static copy.
- Add a test that compares generated quickstart contents with the fixture/template source.

## Suggested Refactoring Sequence

1. Extract asset type/connection type helpers and replace hand-written mappings with Bruin mappings.
2. Extract inferred asset name/path helpers and add focused tests.
3. Introduce shared config/environment/connection-manager helpers and migrate execution/direct/onboarding code.
4. Split query result JSON formatting/parsing into a focused file.
5. Split `asset.go` into asset CRUD, asset naming, dependency reconciliation, and SQL formatting files.
6. Split `direct_executor.go` into direct run, direct query, direct import/discovery, direct executor registry, and output formatting files.
7. Replace `map[string]string` API results with typed response structs.
8. Coordinate with Bruin upstream for direct execution embedding and inferred-name-preserving persistence.

## First Safe Refactoring Target

The safest initial code change is asset/connection type mapping because it is small, covered by existing tests, and directly addresses duplicated Bruin behavior. The target shape is:

- Use `pipeline.IngestrTypeConnectionMapping` for ingestr destinations.
- Use `pipeline.AssetTypeConnectionMapping` to derive query asset types by connection type instead of maintaining a local switch.
- Use `config.ConnectionAndDetailsGetter.GetConnectionType(name)` in direct import paths where a manager is available instead of inspecting concrete connection type names.
