# Secret management

> **Status (2026-07-27): Phase 0 and the core local Phase 1 path are
> implemented; schedule convergence from Phase 2 has started.** Connection
> credentials are write-only, `local:` values use the OS credential store,
> `local-vault:` values use a passphrase-protected age file outside Git, and
> `env:` remains first-class for headless systems. `.renart/secrets.yml` tracks
> value-free bindings, and production connection-manager paths resolve through
> one operation-scoped service. Schedules and the local CLI administration and
> child-process bridge use the same typed resolver. Provider-backed temporary
> files, explicit Python injection, team providers, and the hosted provider
> model remain in progress and must not appear as available features in
> user-facing documentation.

## 1. Decision summary

Renart should separate three things that are currently easy to conflate:

1. **Version-controlled configuration** says which symbolic value a connection
   field needs.
2. **A provider binding** says where that symbolic value comes from in a given
   environment.
3. **A resolved value** exists only inside the server operation that needs it.

The recommended project contract is:

- keep connections and their non-sensitive fields in the existing project
  config;
- use its already-supported `${NAME}` placeholders for sensitive fields;
- track connection-field coordinates and provider bindings, but never values,
  in `.renart/secrets.yml`;
- store the default local values in the operating-system credential store;
- resolve references just in time through one provider-neutral Go service;
- return only presence, provider, and health metadata to the browser;
- reuse the same resolver for connection fields, schedule variables, and
  explicit Python secret injection;
- give a future hosted runner a run-scoped grant to resolve only the references
  required by that run.

The first user-visible milestone should be local keychain-backed connection
editing with write-only secret fields. External providers and hosted storage
should implement the same interfaces instead of creating parallel paths.

Do not store plaintext secrets in `.renart/state.db`, project files, plan
artifacts, snapshots, browser state, URLs, command-line arguments, or logs.
Redaction remains defense in depth; it is not the primary access boundary.

## 2. Why this fits Renart

The filesystem remains authoritative, but secret _values_ are deliberately not
part of the filesystem source of truth. Git should answer:

- which connection and field needs a secret;
- which environment it applies to;
- which provider/key supplies it;
- whether the reference is pinned or follows the provider's latest version.

The secret provider should answer:

- whether the caller is allowed to use the value;
- the current value and optional lease/version metadata;
- how and when the lease must be renewed or revoked.

This preserves a reviewable secret-binding contract without pretending that a
credential belongs in a Git diff. It also gives local Renart, terminal users,
CI, self-hosted runners, and a future hosted service the same project contract.

## 3. Current-state audit

### 3.1 Connection editing before Phase 0 exposed values

Before Phase 0, `ConfigService.BuildResponse` reflected each connection into
`WorkspaceConfigConnection.Values`. Sensitive fields therefore cross
`GET /api/config`, enter browser state, and are populated into ordinary form
inputs. The UI only chooses password-style rendering by inspecting field names.

The connection structs already provide a stronger source of truth:
`sensitive:"true"` and `sensitive_file:"true"` tags. The API's reflected field
definition currently drops those tags even though it retains field type,
requiredness, and defaults.

Connection create, update, and test requests also used one undifferentiated
`values` map. Updating a connection deletes and rebuilds it, so simply omitting
a secret from a request would clear it. A write-only API therefore
needed explicit keep/replace/clear semantics before responses could stop
returning values. Phase 0 now provides that boundary; plaintext persistence
remains supported only as a legacy input and moves to the OS credential store
when the user replaces it through the connection editor.

### 3.2 The config format already has a portable reference

String values in the project config can contain `${NAME}` references. The
config loader expands variables that exist in the process environment, while
unresolved references remain available as strings. Connection construction
also expands placeholders.

The current config loader deliberately adds `.bruin.yml` to `.gitignore`
because that file may contain credentials. This proposal does not silently
start tracking it. Instead, the tracked binding manifest names the
environment/connection/field consumer explicitly, while the local config keeps
the compatible placeholder. Once all plaintext has been migrated, making safe
connection topology shareable can be evaluated separately with the execution
engine; it must not be smuggled into the secret-storage implementation.

That syntax is valuable because it works in terminal-first execution when the
variable is exported. Renart can add keychain and remote-provider convenience
without introducing a project format that only Renart understands.

### 3.3 Scheduled variables already model references separately

Tracked schedules store `secret_refs` as `env:NAME`. Public schedule DTOs return
only variable and reference names. The scheduler resolves values during
validation, planning, and execution, while the private run specification keeps
the reference and omits the resolved value.

This is the closest existing precedent for the proposed boundary. It should be
generalized to the shared resolver rather than replaced.

### 3.4 Execution already has useful defenses

Plans, render output, execution-target snapshots, and pipeline configuration
identities are designed to be secret-free. Render redaction uses sensitive
connection values, and recent direct-execution paths pass credentials through
named environment payloads rather than command-line arguments.

The gaps are consistency and timing:

- every connection-manager construction path must resolve through one service;
- the redactor must be seeded before any resolved value can enter an error or
  child-process output;
- resolved values must not be serialized into durable scheduler state;
- legacy Python `secrets:` injection still places selected values in the child
  process environment.

## 4. Project format

### 4.1 Connections keep `${NAME}` placeholders

```yaml
environments:
  production:
    connections:
      postgres:
        - name: warehouse
          host: db.example.com
          port: 5432
          database: analytics
          username: renart_runner
          password: ${WAREHOUSE_PASSWORD}
```

The variable name is a stable interface between the connection and secret
binding. It is not itself confidential.

Only fields marked `sensitive:"true"` may be managed as inline secret values.
A `sensitive_file:"true"` field needs a separate file-secret mode that writes a
private temporary file for the duration of an operation; it must not silently
turn arbitrary secret text into a persistent path.

### 4.2 Track bindings, not values

```yaml
version: 1

environments:
  default:
    connections:
      warehouse:
        password:
          symbol: WAREHOUSE_PASSWORD
          ref: local:warehouse/password

  production:
    connections:
      warehouse:
        password:
          symbol: WAREHOUSE_PASSWORD
          ref: vault:analytics/data/warehouse#password
```

`.renart/secrets.yml` is reviewable and safe to commit. The exact grammar
should be versioned and parsed into a typed `SecretRef`; code should not pass
provider-specific strings beyond the parser. Explicit consumer coordinates
also let a fresh clone report “warehouse.password needs a local value” even
before that machine's ignored connection config is complete.

Schedule-variable consumers remain in `.renart/schedules.yml`; their existing
`secret_refs` values adopt the same reference parser instead of being duplicated
in this manifest.

Initial reference forms:

| Form                    | Meaning                                                                               |
| ----------------------- | ------------------------------------------------------------------------------------- |
| `env:NAME`              | Read an existing process environment variable.                                        |
| `local:alias`           | Read the value under project UUID + environment + alias from the OS credential store. |
| `local-vault:alias`     | Read the value from Renart's passphrase-protected per-project local vault.             |
| `sops:path#key`         | Decrypt one key from a tracked SOPS document. Optional later provider.                |
| `vault:path#key`        | Resolve a Vault value using an external provider profile.                             |
| `aws-sm:identifier#key` | Resolve an AWS Secrets Manager value or JSON member.                                  |
| `gcp-sm:resource`       | Resolve a Secret Manager resource/version.                                            |
| `azure-kv:identifier`   | Resolve an Azure Key Vault secret/version.                                            |

Provider authentication, endpoints, and tenant defaults belong in user- or
runner-level Renart profiles, not in the project binding file. A reference may
include a non-secret fully qualified identifier when portability requires it.

For `local:alias`, every collaborator stores their own value under the same
portable alias. The actual credential-store key is derived from the stable
project UUID, environment, and alias so two cloned projects do not collide.

### 4.3 Alternatives considered

#### Plain environment variables only

This is the smallest implementation and remains a supported provider. It is
excellent for CI and terminal workflows, but it gives the IDE no safe
persistence, presence metadata, or rotation workflow.

#### One encrypted secrets file

[SOPS](https://github.com/getsops/sops) can keep encrypted YAML in Git and
supports age and cloud KMS recipients. It is a useful optional provider for
teams that want Git-distributed ciphertext. It should not be the only model:
recipient management and re-encryption create operational and merge overhead,
and headless decryption still needs an identity.

#### Whole-connection external backends

The execution engine can source complete connections from Vault, Doppler, AWS,
or Azure. Renart should remain compatible with that mode, but it is too coarse
as the primary IDE model: non-sensitive topology becomes invisible to Git,
mixed providers are difficult, and editing one credential means replacing an
opaque connection object.

#### Built-in passphrase-locked local vault

This is the implemented fallback for systems without a usable native credential
store. Renart keeps one small encrypted
vault in the operating system's per-user application-data directory, keyed by
stable project UUID and environment. The tracked manifest continues to
contain references only. A passphrase entered in the web/standalone UI or
`renart secrets vault unlock` derives the decryption key; only the running
local process retains that key in memory. A later local unlock agent could
share the session across processes.

Using age's passphrase recipient is preferable to inventing a file format: it
provides authenticated file encryption and a deliberately expensive scrypt
derivation. This mode is well suited to an interactive local or SSH session
and does not require exporting each connection value. It does have one honest
operational limit: after a process or machine restart, scheduled runs remain
blocked until the vault is unlocked. Unattended startup requires a
non-interactive root of trust such as a native unlocked store, an age identity
file or hardware plugin, environment injection, workload identity, or an
external secret manager; an encrypted file cannot remove that requirement.

The first version keeps the unlocked document and passphrase inside the Renart
server and serializes whole-vault rewrites with a cross-process lock and atomic
replacement. It detects out-of-process ciphertext changes before updates. A
cross-process local agent over a
Unix socket or Windows named pipe would make CLI and web unlock state shared,
but adds lifecycle, peer-authentication, and stale-socket work and should be a
separate phase. Restart-blocked schedules are an explicit limit of this
interactive fallback; unattended systems should use environment or workload
identity backed providers.

#### Plaintext in SQLite encrypted by an application key

This is not recommended. It creates a second durable secret store, shifts the
problem to protecting and distributing its master key, complicates backups,
and makes a future hosted migration harder. The OS credential store is a better
local default; a managed cloud service should use a dedicated envelope-
encrypted store.

## 5. Service architecture

### 5.1 Core types

```go
type SecretRef struct {
    Provider string
    Key      string
    Field    string
    Version  string
}

type ResolveRequest struct {
    ProjectID  string
    Environment string
    Reference  SecretRef
    Purpose    SecretPurpose
    RunID      string
}

type SecretLease interface {
    Bytes() []byte
    VersionID() string
    ExpiresAt() time.Time
    Close(context.Context) error
}

type SecretProvider interface {
    Stat(context.Context, ResolveRequest) (SecretStatus, error)
    Resolve(context.Context, ResolveRequest) (SecretLease, error)
    Put(context.Context, PutSecretRequest) (SecretStatus, error)
    Delete(context.Context, DeleteSecretRequest) error
}
```

Not every provider supports `Put` or `Delete`; capability metadata must make
that explicit. `SecretLease` must not implement JSON or text marshaling. Its
byte slice should be short-lived, copied as little as practical, and cleared on
close where Go permits.

`Purpose` differentiates connection validation, inspect, materialization,
schedule validation, scheduled execution, and explicit Python injection. It is
part of authorization and audit, not a free-form log label.

### 5.2 One resolved-config factory

Introduce a `ResolvedConnectionFactory` used by every path that currently
constructs a connection manager:

1. load the project config without mutating it;
2. inspect only fields marked `sensitive:"true"` or `sensitive_file:"true"`;
3. find `${NAME}` placeholders and the selected environment's binding;
4. resolve all required values as one operation-scoped bundle;
5. clone the config and replace placeholders in memory;
6. seed a value redactor before driver or subprocess construction;
7. create the connection manager;
8. close/revoke leases when the operation ends.

Do not temporarily mutate global process environment variables. Concurrent
inspects and runs can select different environments, and a process-global
overlay would create credential races. A map-aware upstream config loader would
be ideal; an in-memory clone-and-overlay layer is a viable no-fork first step.

Resolution should be batched by provider. Caches may live for one operation or
for the provider's explicit lease, never as an unbounded process-global map.

### 5.3 Schedules use the same resolver

Generalize schedule `secret_refs` from env-only validation to typed
`SecretRef`. Existing `env:NAME` declarations remain valid without migration.
During planning, keep the reference in `RunSpec` and use the value only for
validation. During execution, resolve again so rotation takes effect and
short-lived credentials are fresh.

The durable run record may keep non-secret provider, key identity, and version
metadata for audit. It must not contain the value.

### 5.4 Python and agents

Connection access from Python should continue moving toward the token-scoped
broker, where credentials remain in Go. Legacy `secrets:` injection is an
explicit escape hatch: resolve only the named references, put them only in that
child's environment, seed redaction first, and document that arbitrary Python
can read them.

Future notebook or coding agents receive connection names, schemas, and secret
status only. They may invoke brokered query tools under user policy; they never
receive a generic `get secret` tool.

## 6. Browser and API contract

### 6.1 Sensitive fields become authoritative metadata

Add `is_sensitive` and `is_sensitive_file` to
`WorkspaceConfigFieldDef`, derived from struct tags rather than name
heuristics.

For a configured connection:

- `values` contains only non-sensitive fields;
- `secret_fields` contains descriptors such as configured/missing,
  provider, writable, rotatable, and a safe display reference;
- no endpoint returns the current value, including immediately after creation.

The settings UI displays `Configured`, `Missing`, `Unavailable`, or
`Permission required`. A password box starts empty. Editing uses a tri-state
operation:

```json
{
  "secret_changes": {
    "password": { "action": "keep" },
    "api_token": {
      "action": "replace",
      "binding": { "ref": "local:warehouse/api-token" },
      "value": "write-only"
    },
    "old_key": { "action": "clear" }
  }
}
```

There is no magic `"********"` sentinel. `keep`, `replace`, and `clear` are
validated server-side. Required sensitive fields may be absent from `values`
when a healthy binding satisfies them.

Connection testing assembles the draft non-secret values and resolves
write-only changes on the server. Errors are redacted before crossing the API.

### 6.2 Secret administration

The initial UI can live in the connection editor. A later project settings
page may show:

- aliases and environments;
- provider and presence/health;
- last rotation/use metadata where the provider exposes it;
- consumers (connection fields and schedule variables);
- replace, rebind, or remove actions;
- a preflight that checks availability without reading values into the browser.

Do not add a reveal/copy-current-value action. Replacement is safer and maps to
hosted permissions.

## 7. Identity, plans, and freshness

Credential bytes must remain excluded from:

- source and configuration fingerprints;
- physical target identity;
- deploy snapshots;
- confirmed plan artifacts;
- freshness and materialization facts.

The tracked binding document and reference identity _do_ affect the
configuration digest. Rebinding `WAREHOUSE_PASSWORD` from one provider/key to
another therefore invalidates a confirmed plan. Rotating the value behind an
unchanged unpinned reference does not make assets stale or change a physical
target.

A pinned provider version is part of the binding identity. An unpinned
`latest` reference resolves at execution time; its observed version can be
recorded as secret-free audit metadata without changing the reviewed plan.

Some credentials contain routing information, such as a service-account
document that implicitly selects a cloud project. Renart must not infer a
reviewed physical target only from hidden bytes. Require target-routing fields
to be explicit non-sensitive configuration, or produce a stable secret-free
target descriptor during server-side preflight and show it in the plan.

## 8. Local provider

Use the native user credential store:

- macOS Keychain;
- Windows Credential Manager/Credential Locker;
- the freedesktop Secret Service API on Linux.

The Linux provider must fail clearly when no Secret Service implementation or
unlocked collection is available. Headless Linux is common for scheduled
runs; environment variables and external providers remain first-class rather
than silently falling back to plaintext.

The current `local:` provider uses
[`zalando/go-keyring`](https://github.com/zalando/go-keyring) behind Renart's
own provider interface. The interface keeps the adapter replaceable and lets
tests use an in-memory credential-store implementation without adding a
production plaintext fallback.

Local schedules need a startup preflight. A locked keychain should mark the
schedule blocked with an actionable status; it must not repeatedly prompt or
fail as an unexplained pipeline error.

## 9. Hosted-cloud model

The same project bindings should work in a hosted product, but the trust
boundary changes.

### 9.1 Prefer bring-your-own vault and workload identity

For production, the preferred path is for the execution runner to authenticate
to the customer's provider using workload identity and fetch a short-lived
value directly. Cloud providers document temporary workload credentials, and
Vault can issue leased dynamic credentials that are revocable.

The control plane stores reference metadata and policy. It should not receive a
long-lived cloud access key merely so a runner can read another secret store.
Where possible:

- exchange the runner's signed identity for short-lived provider credentials;
- scope the identity to the project, environment, and allowed secret paths;
- resolve only after a run is admitted;
- revoke or let the lease expire when the run ends.

### 9.2 Managed hosted secrets

Some users will need a Renart-managed provider. Store each value as
authenticated ciphertext with envelope encryption:

- a per-organization or per-project data-encryption key;
- that key wrapped by a cloud KMS key;
- tenant/project/environment/reference identity bound as encryption context;
- ciphertext and version metadata in the secret store;
- KMS and ciphertext access separated by service role.

The browser sends a replacement over authenticated TLS to a write-only
endpoint. The control plane encrypts immediately and never returns it. A
run-scoped grant authorizes an execution-plane worker to decrypt only the
references declared by the admitted run. Do not place plaintext in queues,
job specifications, traces, analytics, or support tooling.

### 9.3 Permissions and audit

Separate:

- `secret.metadata.read`;
- `secret.use` through an approved run/query;
- `secret.manage` for bind/replace/remove;
- provider-profile administration.

`secret.manage` still need not imply reveal. Audit binding changes, failed and
successful resolution, run/purpose, version identity, actor, and runner—but
never the value. Apply retention and export policy to this audit stream.

Hosted private links or notifications must use opaque identifiers, not secret
references or credentials in URLs.

## 10. Threat model and invariants

The design protects against accidental Git commits, ordinary API inspection,
browser extensions reading connection state, durable run/snapshot leakage, and
cross-environment concurrency mistakes.

It does not protect a secret after giving it to arbitrary user-authored Python,
a compromised database driver, or a process running as the same local OS user
with access to that user's unlocked credential store. Those are explicit
execution trust boundaries.

Required invariants:

1. A config GET, SSE event, error envelope, plan, snapshot, or run record never
   contains a resolved value.
2. Every provider resolution names project, environment, reference, purpose,
   and optional run.
3. No resolved value enters argv.
4. No project import or docs link carries a secret binding value.
5. Provider/network errors are masked before leaving the service boundary.
6. A binding change is reviewable and invalidates stale confirmations.
7. A value rotation alone does not change asset freshness.
8. Secret access by an agent is only indirect through a bounded operation.

## 11. Delivery plan

### Phase 0 — stop round-tripping credentials

Implemented on 2026-07-26:

- sensitive field tags are exposed in generated API types;
- connection mutations and draft tests use keep/replace/clear semantics;
- config responses and SSE-derived reloads omit sensitive values;
- reflection and HTTP canary tests cover tagged connection fields and response
  boundaries.

Estimated effort: 4–6 engineering days. This is independently valuable and
should ship first.

### Phase 1 — local bindings and keychain

Implemented on 2026-07-26:

- the strict, versioned `.renart/secrets.yml` parser and typed references;
- `env:` plus native OS credential-store-backed `local:` providers, with
  operation-scoped leases and explicit configured/missing/unavailable status;
- one resolved-config factory for production connection construction across
  connection test, inspect/query, materialization, schema/discovery,
  onboarding, notebook imports, and ad-hoc queries;
- compensating provider/manifest/config updates for connection and environment
  create, rename, clone, replace, clear, and delete flows;
- a connection-editor source choice between the credential store and
  environment-variable references, including actionable headless errors;
- CLI status/set/remove commands plus an operation-scoped child-process
  environment bridge that never puts values in argv or the parent environment;
- secret-free binding identity in plans and freshness: rebinding changes the
  digest, while rotating a value behind the same reference does not.

Still open in this phase:

- an explicit migration preview and stronger crash-injection coverage across
  the provider plus two filesystem writes;
- decide and implement the passphrase-locked user-local vault described above
  for machines without a usable native credential service;
- provider-backed `sensitive_file` leases that materialize private temporary
  files for one operation.

Estimated effort: 2–3 weeks. The migration must be crash-safe: write the
keychain value, verify it, update project files atomically, then optionally
remove the old literal.

### Phase 2 — schedules and Python convergence

Implemented so far:

- schedule references use the shared typed parser and resolver, preserve only
  refs in durable state, and resolve separately for validation and execution;
- connection settings expose provider health and an environment-reference path
  when a desktop credential service is unavailable;
- notebook imports that need a named project connection use the
  `notebook_query` purpose through the Go connection factory.

Still open:

- move remaining Python connection use behind the broker where possible;
- make all redaction and subprocess boundaries consume the resolved bundle.

Estimated effort: 1–2 weeks.

### Phase 3 — team providers

- add SOPS/age and selected Vault/cloud-secret adapters;
- add provider profiles, capability discovery, version pinning, leases, and
  rotation workflows;
- preserve compatibility with whole-connection external backends.

Estimated effort: 3–5 weeks per well-tested provider wave, depending on auth
methods and live-test infrastructure.

### Phase 4 — hosted execution

- add tenant-scoped metadata, RBAC, audit, and run grants;
- implement workload-identity provider access;
- add the managed envelope-encrypted provider;
- ensure the same frontend DTOs operate against local and hosted providers.

This depends on the hosted runner/control-plane architecture and should be
estimated with that work.

## 12. Validation

The implementation needs more than unit tests:

- reflection tests prove every `sensitive`/`sensitive_file` field is classified;
- API canary tests cover GET, create/update/test responses, SSE, errors, plans,
  runs, and snapshots;
- concurrent tests resolve two environments with different values and prove no
  cross-talk;
- crash tests cover keychain-write/config-write migration ordering;
- live tests cover keychain available, locked, missing, and headless Linux;
- provider contract tests cover versioning, expiry, renewal, revocation, and
  permission denial;
- subprocess tests inspect argv, environment scope, stdout/stderr, and cleanup;
- hosted tests prove one tenant/run grant cannot resolve another tenant's key.

## 13. Research references

- [SOPS project and provider/age model](https://github.com/getsops/sops)
- [Vault secrets engines](https://developer.hashicorp.com/vault/docs/secrets)
- [Vault lease, renewal, and revocation](https://developer.hashicorp.com/vault/docs/concepts/lease)
- [Google Cloud workload identities](https://docs.cloud.google.com/iam/docs/workload-identities)
- [AWS IAM temporary-credential guidance](https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html)
- [Windows Credential Locker](https://learn.microsoft.com/en-us/windows/apps/develop/security/credential-locker)
- [freedesktop Secret Service API](https://specifications.freedesktop.org/secret-service/latest/)
