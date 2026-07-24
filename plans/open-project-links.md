# Open-project links

> **Status (2026-07-24): investigation and concept only — not implemented.**
> This document evaluates links from documentation into local Renart and a
> future hosted product. It does not describe a currently supported URL
> handler.

## 1. Decision summary

Add a canonical HTTPS **open intent** first:

```text
https://getrenart.com/open/v1
  ?repo=https%3A%2F%2Fgithub.com%2Frenart-data%2Frenart.git
  &ref=<full-commit-sha>
  &subdir=example/product_analytics
```

That page is a safe, useful fallback everywhere. It shows exactly what will be
opened and offers:

- **Open in Renart** when a registered native handler is available;
- a copyable `renart open '<https intent>'` command;
- clone/open instructions;
- installation and GitHub links.

The eventual native scheme should be `renart:` and dispatch to a new
`renart open` launcher command, not directly to `renart-gui`:

```text
renart://open/v1?repo=...&ref=...&subdir=...
```

The launcher validates the intent, forwards it to an authenticated running
Renart process or starts `renart standalone` in bootstrap mode, and presents an
import/review screen. It never automatically executes project code, activates
schedules, installs dependencies, or materializes assets.

Do not make `navigator.registerProtocolHandler()` or a PWA protocol handler the
primary design. Browser handlers have limited support, require HTTPS, use
`web+` custom schemes, and route back to one web origin—not to an arbitrary
loopback Renart server.

## 2. Product use

Open links are most valuable when a documentation page contains a maintained,
runnable example. The button should mean “bring this exact example into the
visual IDE,” not merely “open whatever repository is nearby.”

Good placements:

- tutorial introductions with a corresponding example project;
- feature guides whose behavior is easier to understand on a canvas;
- demo/template galleries;
- release notes for a new runnable example.

Poor placements:

- every reference page;
- pages that require private credentials before anything useful appears;
- links to an entire monorepo with no workspace subdirectory;
- aspirational features that do not yet work.

The docs component should show **Open example in Renart** beside **View files on
GitHub**, with a short note when cloning is required. It should not auto-launch
a custom protocol on page load; launch only from a user click.

## 3. Current-state audit

### 3.1 The server can already host multiple local projects

The process-level project manager maintains a registry of Git-backed project
roots, lazily opens them, and mounts each runtime under
`/api/projects/{projectID}`. The welcome flow can start on a temporary bootstrap
root, browse directories, create a project, and open an existing local path.

This is a strong destination for an imported project. Missing pieces are:

- cloning or fetching a remote repository;
- matching a remote URL + subdirectory to an existing registry entry;
- a trust/review state before a newly cloned runtime starts;
- a process-level authenticated intent endpoint;
- activation/focus and single-instance coordination.

### 3.2 The desktop helper is deliberately thin

`renart standalone` starts the normal loopback HTTP server, then launches
`renart-gui --url <loopback URL>`. The Wails helper only creates a native window
and navigates it to that URL. It has no project manager, Git importer, or
cross-process activation service.

Protocol handling therefore belongs in the main `renart` launcher. Pointing an
OS association directly at `renart-gui` would force the window helper to grow a
second server/project lifecycle and duplicate application logic.

### 3.3 Current releases are portable archives, not registered apps

Release archives and the install script place `renart` and its GUI companion
beside one another in a bin directory. They do not currently install:

- a macOS `.app` bundle with `CFBundleURLTypes`;
- a Windows package/installer or per-user protocol association;
- a Linux `.desktop` entry and `x-scheme-handler/renart` association.

Native protocol support therefore includes packaging/installer work; changing
the Wails window code alone is insufficient.

## 4. Intent contract

### 4.1 Fields

```go
type OpenProjectIntent struct {
    Version   int
    RepositoryURL string
    Ref       string
    Subdir    string
    Entry     string // optional route, pipeline, asset, or notebook after open
    Source    string // optional public docs page for display/audit
}
```

Required semantics:

- `repository_url` identifies a Git remote, never a local path;
- `ref` should be a full commit SHA in maintained docs links;
- `subdir` is a slash-separated relative workspace root with no `..`,
  absolute path, drive prefix, or encoded path escape;
- `entry` is an optional typed in-app destination, not an arbitrary frontend
  URL;
- unknown versions or fields that affect behavior fail closed.

Do not put access tokens, embedded URL credentials, environment values, local
paths, provider secret references, or serialized project files in the intent.
Reject repository URLs with userinfo.

The HTTPS and native representations must share one Go parser, canonicalizer,
size limit, and test corpus. Query parameters are preferable to a base64 JSON
blob for public examples because users can inspect them. A future hosted
private intent should use a short-lived opaque ID rather than adding private
metadata to the URL.

### 4.2 Reproducibility

Docs examples should pin a commit rather than a moving branch. The interstitial
shows the friendly release/tutorial name, repository host, and short commit,
while preserving the full SHA internally.

A docs validation script should maintain a manifest:

```yaml
product-analytics:
  repo: https://github.com/renart-data/renart.git
  ref: 0123456789abcdef...
  subdir: example/product_analytics
```

For examples in this repository, the docs build can validate the subdirectory
locally. A release/update script advances the commit deliberately. Remote
network validation belongs in CI, not every local docs build.

## 5. Safe import flow

A clicked repository is untrusted input even when the link came from a docs
page.

1. Parse and validate the intent before network access.
2. Show repository host, ref, subdirectory, and destination.
3. Let the user select a new or existing empty parent directory.
4. Clone/fetch into a staging directory with dangerous Git protocols disabled.
5. Resolve the requested ref and verify the resulting commit.
6. Validate that the subdirectory remains within the checkout and appears to
   be a project.
7. Show a trust screen before constructing a project runtime.
8. Move the checkout into its final destination and register it.
9. Open the requested in-app entry only after normal workspace loading.

Trust confirmation is not ceremony: loading a tracked schedule into a fully
wired runtime must not make untrusted repository content eligible to run.
Linked imports need a local registry state such as `reviewed`/`trusted`.
Until trusted:

- do not start or reconcile schedules;
- do not run Python or shell-backed assets;
- do not install dependencies;
- do not materialize, inspect through unsafe fallbacks, or run custom checks;
- do not invoke repository-provided Git hooks or helpers.

Passive file parsing, tree display, and Git diff inspection may be available in
review mode if their implementations are confirmed non-executing.

Official example links can receive clearer provenance styling, but should not
create a hidden bypass that any repository can imitate.

### Existing clones

Extend registry metadata with normalized remote URL and project subdirectory.
When an intent matches:

- if the working tree has changes, open it without fetching or switching;
- if its HEAD differs from the requested ref, explain the mismatch and offer
  an explicit fetch/open-new-copy flow;
- never reset, clean, or overwrite a working tree;
- never assume two local directories with the same remote are interchangeable.

## 6. Launcher and process routing

### 6.1 `renart open`

The native association invokes:

```text
renart open <intent-url>
```

This command:

- accepts canonical HTTPS or `renart:` intents;
- performs syntax/security validation;
- discovers a suitable running Renart instance;
- POSTs the intent with that instance's session token;
- otherwise starts `renart standalone --open-intent <validated-file-or-url>`.

Pass the URL as one argv value and enforce a bounded length. Never interpolate
it into a shell command.

### 6.2 Authenticated forwarding

An ordinary web page must not be able to trigger a clone by requesting an
unauthenticated loopback URL. Keep the existing token and same-origin boundary:

- use a process-level `POST /api/open-intents`;
- require the per-process session token;
- store pending intent state in memory;
- publish progress through the existing authenticated application/SSE surface;
- reject GET activation and CORS access.

Per-project `.renart/server.json` discovery is sufficient once an existing
project has been matched. New-project intents need a user-level instance
registry or local activation socket under the Renart state directory. Instance
records must be user-private, carry PID/start time and a random token, expire
on shutdown, and be checked for stale PID reuse.

The first implementation may open a second standalone process. A polished
single-instance/focus path is platform-specific and can follow without changing
the intent contract.

### 6.3 Why not put this in `renart-gui`

The helper should continue receiving only an authenticated loopback app URL.
Keeping URI parsing, Git, server bootstrap, and trust decisions in the main
binary:

- preserves one project/runtime implementation;
- makes `renart open` usable from terminals and OS handlers;
- keeps browser fallback behavior;
- avoids duplicating APIs in Wails bindings;
- lets future hosted and local links share the parser.

## 7. Platform registration

| Platform | Registration                                                                                                                                    | Current gap                                                                                    |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| macOS    | Package an application bundle whose `Info.plist` declares `CFBundleURLTypes`/`CFBundleURLSchemes`, and route URL-open events to `renart open`.  | Releases contain a bare helper, not a signed/notarized `.app` bundle.                          |
| Windows  | Register the scheme through a packaged app manifest or an explicit per-user installer association that invokes `renart.exe open "%1"`.          | The zip/install script does not create protocol associations or package identity.              |
| Linux    | Install a `.desktop` file with `Exec=renart open %u` and `MimeType=x-scheme-handler/renart;`, then register it through the desktop MIME system. | The installer copies binaries only; headless installs should not register desktop integration. |

Registration should happen through a real installer or an explicit
`renart integrations install-protocol` action with clear consent. Starting
`renart web` must not silently change OS defaults.

The freedesktop specification defines URI handlers as
`x-scheme-handler/<scheme>` MIME types, and `%u` is the desktop-entry field code
for one URL. Apple exposes schemes through `CFBundleURLTypes`. Windows supports
custom protocol activation for desktop applications, with packaging-specific
registration options.

## 8. Browser-only options

### 8.1 `navigator.registerProtocolHandler`

This API is not a substitute for the native scheme:

- it requires a secure HTTPS context;
- a custom protocol must begin with `web+`;
- the handler URL must be same-origin and include `%s`;
- browsers may prompt the user;
- support is not Baseline across major browsers.

It could register `web+renart:` to send a user back to
`https://getrenart.com/open/handler?...`. That helps a future hosted experience,
but cannot reliably locate a random-port local server and does not register the
desired `renart:` native scheme.

### 8.2 PWA manifest `protocol_handlers`

The manifest member has similar limited/experimental support and opens the
installed PWA at an HTTPS route. It is potentially useful for a hosted Renart
PWA, not the primary local IDE bridge.

### 8.3 HTTPS app/universal links

Associating `https://getrenart.com/open/...` directly with installed native
apps would produce the cleanest click behavior and retain a browser fallback.
It requires signed platform packages, domain association files, and
platform/browser-specific verification. It is a later packaging milestone,
not a prerequisite for the canonical HTTPS intent.

## 9. Documentation integration

Create a page-local Astro component backed by a checked manifest rather than
handwritten URL strings:

```mdx
<OpenInRenart example="product-analytics" entry="pipeline/product_analytics" />
```

The component renders:

- example name and a one-line outcome;
- **Open example in Renart**;
- **View files on GitHub**;
- an accessible copy-command fallback;
- a note that the first open clones a local Git repository.

The public `/open/v1` interstitial:

- never auto-launches on load;
- repeats the exact repository/ref/subdirectory;
- lets the user click the native link;
- explains installation when no handler responds;
- offers the terminal command and GitHub source;
- does not claim the project is trusted merely because the page is hosted by
  Renart.

Keep links selective. A docs page should only include one when its maintained
example exercises the documented feature and runs without private
infrastructure. The examples become release-tested product surfaces.

## 10. Future hosted integration

The canonical HTTPS intent can become the chooser:

- **Open locally** launches or instructs the native app;
- **Open in Renart Cloud** signs the user in, requests repository permission,
  and creates/imports a workspace;
- **View on GitHub** remains the transparent fallback.

The hosted import service repeats ref/subdirectory validation and uses strict
network egress and Git-protocol policy. Private repository credentials stay in
the authenticated provider integration, not the URL.

For a private invitation, use:

```text
https://app.getrenart.com/open/<short-lived-opaque-id>
```

The server resolves that ID after authentication and authorization. It should
not encode repository tokens, cloud workspace IDs with access semantics, or
secret bindings in query parameters.

Local and hosted destinations share `OpenProjectIntent`; they differ in
destination selection, trust policy, and Git credentials.

## 11. Options and feasibility

| Option                                              | Value                                                     | Effort                                  | Feasibility                                            | Recommendation                    |
| --------------------------------------------------- | --------------------------------------------------------- | --------------------------------------- | ------------------------------------------------------ | --------------------------------- |
| HTTPS interstitial + copyable `renart open` command | Works in every browser; establishes stable docs contract. | 3–5 days, excluding clone service.      | High.                                                  | Build first.                      |
| `renart open` + reviewed local clone/import         | One command from docs to a safe local project.            | 1–2 weeks.                              | High; project manager is reusable, trust state is new. | Core implementation.              |
| Native `renart:` registration                       | Best installed-app UX.                                    | 1–3 weeks plus signing/installer work.  | High technically, packaging-dependent.                 | Build after launcher.             |
| Browser `web+renart` handler/PWA                    | Opens one HTTPS origin, inconsistent browser support.     | 2–4 days.                               | Medium/low product reliability.                        | Optional hosted enhancement only. |
| HTTPS universal/app links                           | Seamless native-or-web behavior.                          | 2–4 weeks plus distribution operations. | Medium until signed packages exist.                    | Later distribution milestone.     |

## 12. Delivery sequence

### Phase 0 — intent and docs contract

- implement the shared parser/canonicalizer and malicious-input tests;
- add `renart open` in dry/manual mode;
- add the `/open/v1` interstitial and docs manifest/component;
- link one maintained, release-tested demo;
- provide clone/manual instructions without claiming protocol registration.

### Phase 1 — safe local import

- add staging clone/fetch and exact-ref validation;
- add trust state and ensure schedules/execution cannot start before approval;
- extend the project registry with remote/subdirectory metadata;
- add bootstrap import UI, progress, cancellation, and cleanup;
- add end-to-end tests with a local Git remote (no network dependency).

### Phase 2 — process activation and native handlers

- add authenticated instance discovery/forwarding;
- package and register each platform handler;
- add install/uninstall/upgrade tests;
- handle second-instance focus where the platform supports it;
- keep the terminal and browser fallbacks.

### Phase 3 — hosted chooser

- route canonical HTTPS intents to local or hosted destinations;
- add authenticated private-repository import;
- add opaque private invitation intents and audit;
- evaluate universal/app links after signed packages are established.

## 13. Validation

- table-driven parser tests cover encoding, duplicate keys, URL userinfo,
  control characters, path traversal, Windows paths, oversized input, and
  unknown versions;
- Git tests use local bare remotes and prove exact commit/subdirectory
  selection;
- a malicious repository fixture proves no schedule, Python, hook, dependency,
  or materialization runs before trust;
- dirty existing clones are never fetched/reset without confirmation;
- loopback activation rejects unauthenticated browser requests;
- installer tests verify registration and removal on all packaged platforms;
- docs tests validate every example manifest entry and accessible fallbacks;
- live E2E covers no app installed, app already running, cold launch, clone
  cancellation, existing clone mismatch, and invalid intent.

## 14. Research references

- [MDN: `navigator.registerProtocolHandler()`](https://developer.mozilla.org/en-US/docs/Web/API/Navigator/registerProtocolHandler)
- [MDN: PWA `protocol_handlers`](https://developer.mozilla.org/en-US/docs/Web/Progressive_web_apps/Manifest/Reference/protocol_handlers)
- [Apple: `CFBundleURLTypes`](https://developer.apple.com/documentation/bundleresources/information-property-list/cfbundleurltypes)
- [Microsoft: handle URI activation](https://learn.microsoft.com/en-us/windows/apps/develop/launch/handle-uri-activation)
- [freedesktop shared MIME info: URI scheme handlers](https://specifications.freedesktop.org/shared-mime-info/latest/ar01s02.html)
- [freedesktop desktop entry `Exec` field codes](https://specifications.freedesktop.org/desktop-entry/latest-single/)
