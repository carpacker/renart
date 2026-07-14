# Python query connection policy

> **Status (2026-07-14): proposal only — not implemented.** The current
> behavior remains unchanged: a Python pipeline asset may issue read-only SDK
> queries against any named connection in its selected environment. This plan
> adds an opt-in environment guardrail without changing that default.

## 1. Decision

Add a nested `python_query` policy to each entry in
`.renart/environments.yml`:

```yaml
environments:
  prod:
    protected: true
    python_query:
      connection_scope: allowlist
      connections:
        - warehouse_readonly
        - product_analytics
```

`connection_scope` has four values:

| Value       | Connections an asset may query                                                                             |
| ----------- | ---------------------------------------------------------------------------------------------------------- |
| `all`       | Every named connection in the selected environment. This is the default when the block or field is absent. |
| `lineage`   | The asset's effective connection plus the effective connections of its declared asset dependencies.        |
| `allowlist` | Exactly the names in `connections`; the asset's own connection is not added implicitly.                    |
| `none`      | No Python SDK queries. Python code and result materialization can still run.                               |

An `allowlist` must contain at least one unique, non-empty connection name.
`connections` is invalid with the other scopes. The API and settings UI write
the explicit canonical value; hand-authored files may omit the entire block to
retain today's `all` behavior.

This is deliberately a **connection-level** policy. Once a connection is
allowed, the database credential determines which catalogs, schemas, tables,
and rows it can read. Renart should recommend read-only, least-privilege
connections for sensitive environments rather than pretending this setting is
row-level security.

## 2. Why this shape

The default honors the already accepted SDK contract and avoids breaking
existing projects. The restrictive modes cover distinct, understandable use
cases:

- `lineage` is the low-maintenance choice: declared dependencies already carry
  ordering and lineage, and their resolved connections provide a useful data
  boundary without adding Python-only metadata to every asset.
- `allowlist` covers raw/source tables that do not have a Renart asset and lets
  a production environment expose dedicated read-only connections.
- `none` gives an environment a clear kill switch without disabling Python
  transformations that do not call `query()`.

A single absent-or-present list was rejected because an absent list, an empty
list, and an automatically derived lineage set are materially different states
that would be hard to explain in the UI. Per-asset connection allowlists were
also rejected for the first version: they add authoring ceremony and duplicate
environment security configuration in every asset. They can be proposed later
as a narrower override if real projects need them.

## 3. Scope and semantics

- Policies are evaluated in the run's selected environment. `all` means all
  connections in that environment, never connections from another environment.
- The check applies equally to interactive, embedded CLI, delegated CLI,
  scheduled, and snapshot runs. Unlike `protected`, this controls data access,
  not how a run was launched.
- Both an omitted `connection=` argument and an explicit argument are checked.
  The omitted form resolves the asset's effective pipeline/default connection
  first, then authorizes it.
- `lineage` uses direct declared asset dependencies. Each dependency is
  resolved with the pipeline's normal connection rules; duplicate connection
  names collapse into one set. Missing dependencies remain a separate type
  check error.
- Connection names match the connection manager's existing exact-name
  semantics. The settings UI selects names rather than accepting arbitrary
  text.
- Notebook Python cells are unchanged. Their broker exposes only the synthetic
  `renart-notebook` live-session connection and already rejects every other
  name. Imported project assets continue through the notebook import resolver;
  a future feature that gives notebook code direct project connections must
  adopt an explicit notebook policy instead of silently reusing this one.

This remains a local guardrail: a user who owns the repository and process can
edit the policy or read local credentials. In a future hosted execution model,
the same vocabulary can become an enforced permission boundary.

## 4. Enforcement design

Enforce after the broker resolves the requested/default connection and before
SQL parsing, materialization waiting, or connection lookup:

```text
SDK request
  -> resolve connection name
  -> authorize connection for asset + environment
  -> validate one read-only SELECT
  -> wait for referenced in-flight assets
  -> execute through the Go connection manager
```

`pybroker.Config` gains an `AuthorizeConnection(name) error` callback. The
broker returns a stable `connection_forbidden` error and never invokes
`RunQuery` on rejection. Keeping the callback at this boundary covers implicit
and explicit connections and preserves notebook isolation, while the policy
package stays independent of HTTP and database code.

The Python operator computes the authorization context once when it starts a
task: environment policy, effective asset connection, and effective declared
upstream connections. It does not hand credentials or the full connection
configuration to the broker or Python. Every execution registry must receive
the same policy loader/function dependency; no CLI or scheduler-specific
fallback may default to `all` when a configured policy exists.

Policy parsing gains validation for unknown scopes, invalid field
combinations, duplicate/empty names, and allowlist entries missing from the
environment. As today, the loader retains its last known-good configuration if
a hand edit becomes invalid. Connection renames through the Renart API update
allowlist references in the same filesystem transaction; external edits are
reported by type check rather than silently broadened.

## 5. Type check and editor behavior

Pipeline type check becomes environment-aware. The HTTP endpoint accepts the
selected environment, and `renart type-check` gains `--environment` with the
project's selected/default environment as its fallback.

For Python `query()` calls:

- a statically known forbidden `connection="name"` is an error at its source
  location;
- an omitted connection is checked against the asset's resolved default;
- under a restrictive policy, a dynamic connection expression produces a
  warning that runtime authorization will decide it;
- a missing allowlist connection is a policy/configuration error even when no
  current query names it.

The existing literal-query dependency lint remains separate. A declared
dependency can authorize a connection in `lineage` mode, but it does not make
an otherwise invalid SQL reference valid. Monaco may filter connection-name
completions to the allowed set, but UI filtering is a hint; backend enforcement
is authoritative.

Project settings adds a **Python query access** row to the existing environment
policy surface. The scope selector explains the current effective behavior.
`lineage` previews the resolved connection names; `allowlist` shows a
multi-select drawn from that environment. The UI never edits `.bruin.yml`
credentials as part of this operation.

## 6. API and model changes

Proposed Go model:

```go
type PythonQueryPolicy struct {
    ConnectionScope string   `yaml:"connection_scope,omitempty" json:"connection_scope"`
    Connections     []string `yaml:"connections,omitempty" json:"connections"`
}

type EnvironmentPolicy struct {
    Protected          bool              `yaml:"protected" json:"protected"`
    DeployedOnly       bool              `yaml:"deployed_only" json:"deployed_only"`
    ConfirmDestructive bool              `yaml:"confirm_destructive" json:"confirm_destructive"`
    PythonQuery        PythonQueryPolicy `yaml:"python_query,omitempty" json:"python_query"`
}
```

The zero value of `PythonQueryPolicy` means `all`, so old files, API clients,
and environments with no Renart policy preserve current behavior. Generated
frontend API types and the workspace SSE policy projection change together.

## 7. Validation plan

Backend tests:

- policy YAML load/save/default/validation for all four scopes;
- authorization tables for implicit, own, upstream, allowlisted, missing, and
  forbidden connections;
- broker rejection occurs before SQL parsing, waiting, and `RunQuery`;
- embedded CLI, delegated/server, scheduler snapshot, and direct UI runs all
  receive the same policy;
- type check covers literal, omitted, dynamic, and renamed connections;
- notebook Python remains limited to `renart-notebook`.

Live coverage should use two DuckDB connections in one environment: prove an
allowed cross-connection query materializes successfully, then switch to a
restrictive policy and prove the same asset fails with
`connection_forbidden` without opening the forbidden database.

## 8. Non-goals

- table, schema, row, or column-level authorization;
- parsing SQL to infer policy beyond the selected connection;
- exposing credentials to Python;
- changing notebook import permissions;
- per-asset allowlists in the first implementation;
- replacing database-native roles and read-only credentials.

## 9. Implementation order

1. Add and validate the policy model while preserving zero-value `all`.
2. Add the broker authorization hook and wire every Python execution context.
3. Make type check environment-aware and diagnose static Python calls.
4. Regenerate frontend types and add the environment-policy controls.
5. Add focused, execution-path, and live cross-connection coverage.
6. Fold the shipped behavior into `architecture/backend.md` and remove this
   proposal.
