# Renart Go Backend — Architecture Review & Refactoring Plan

_Reviewed 2026-06-09 at commit `508b139` (~25k LOC of Go, excluding vendored code)._

## 1. Current architecture

```
main.go → cmd/web.go (CLI + wiring + webServer)
            ├── internal/web/httpapi    HTTP handlers, one file per domain (~1.8k LOC)
            ├── internal/web/service    domain logic, 54 files (~16.3k LOC)
            ├── internal/web/scheduler  River + SQLite scheduler (~1.2k LOC)
            ├── internal/web/events     SSE pub/sub hub with debounce
            ├── internal/web/watch      fsnotify/poll filesystem watcher
            ├── internal/web/{api, model, freshness, static,
            │                  sqlintelligence, pyintelligence, sqlformat}
            └── Bruin packages          parsing, config, connections, execution
```

Intended layering: transport (`httpapi`) → domain (`service`) → Bruin. The
filesystem is the source of truth; a watcher triggers workspace re-parses and
the result is pushed to the frontend over SSE.

## 2. What is good

- **Runtime model.** Filesystem-as-truth + watcher + debounced SSE hub is a
  simple, coherent sync architecture. `events.Hub` is small and correct:
  buffered per-client channels, non-blocking sends that drop instead of
  blocking on slow clients, debounce-with-coalescing for watcher noise and
  `PublishImmediate` for handler-triggered events. Self-write suppression
  (3s window in `WorkspaceCoordinator`) pragmatically solves the echo problem.
- **Scheduler.** Built on River with the SQLite driver: durable, transactional
  job queue instead of a hand-rolled cron loop. Clean split between `Store`
  (persistence, River migrations) and `Service` (orchestration, catchup
  windows, uniqueness via `river:"unique"`). Execution is injected as a plain
  `Runner` function, so the scheduler knows nothing about how pipelines run.
- **Narrow handler interfaces.** Each `httpapi` file declares its consumer-side
  interface (`SchedulerHandlers`, `AssetHandlers`, `WorkspaceReader`, …);
  handlers are mechanical decode → delegate → encode with no business logic.
- **Centralized path safety.** `WorkspaceResolver` funnels all asset/pipeline
  ID decoding through `SafeJoin`, keeping path-traversal protection in one
  place.
- **Executor abstraction.** `BruinCommandExecutor` with the hybrid
  direct/CLI-fallback implementation directly encodes the product rule
  “match Bruin behavior or fall back to CLI”, and makes execution mockable.
- **Deployment.** Single binary: embedded frontend, embedded Python, pure-Go
  SQLite. Port fallback, browser auto-open, graceful HTTP shutdown.
- **Tests where they matter.** 20 test files concentrated on service and
  scheduler logic rather than trivial handler tests.

## 3. What needs improvement

### 3.1 `cmd/web.go` is a god object (highest priority)

~2.2k lines. `webServer` implements every handler interface and is passed as
`Service: s` to nearly all route registrations. The file mixes CLI parsing,
wiring, ~800 lines of hand-written struct conversion, pass-through adapter
methods, and real business logic that belongs in services (e.g.
`FillColumnsFromDB` with its try-both-path-variants retry strategy,
`UpdatePipelineSchedule` orchestration, asset-type derivation helpers).
`cmd/web_compat.go` adds leftover helpers from a past refactor. None of it is
unit-tested — because it is not testable in this shape.

**Fix:** point handlers at the services directly, push residual logic down
into services, shrink `cmd` to flags + wiring + lifecycle.

### 3.2 Four parallel copies of the same data model

`webmodel.Asset`, `service.WorkspaceAsset`, `cmd.webAsset`, and the `httpapi`
DTOs are field-for-field identical (same for Column/Pipeline/State and the
Python-LSP type family, which exists in triplicate). Every new field must be
threaded through 3–4 structs and 6+ conversion functions.

**Fix:** one canonical DTO set; delete the conversion layers. Distinct types
only where representations actually diverge.

### 3.3 Fragmented error handling

Six structurally identical `{Status, Code, Message}` error types
(`ServiceAPIError`, `SQLAPIError`, `SuggestionAPIError`,
`ParseContextAPIError`, `JinjaRenderAPIError`, `httpapi.APIError`).
Inconsistent envelopes: `api.Response` vs ad-hoc `map[string]any` vs typed
responses with embedded `Status` fields. Error classification by
`strings.Contains` on messages (e.g. `GetPipelineMaterialization`), which
breaks silently when a message is reworded. Several wrong status classes
(404-ish failures returned as 400).

**Fix:** one shared API error type; sentinel errors with `errors.Is/As` at
service boundaries; one envelope.

### 3.4 No observability layer

Logging is `fmt.Printf`; zap is in `go.mod` but used in two files. No chi
middleware: no request logging, no panic recovery, no request IDs.

**Fix:** zap logger initialized in `cmd` and injected; request-log + recovery
middleware.

### 3.5 Local-server hardening gaps

No Origin/Host validation and no CSRF protection. Binding to `127.0.0.1` does
not prevent cross-site request forgery: any web page can fire simple
no-preflight `POST`s at `localhost:8080` — against a server that writes files
and executes SQL/Python. Only `ReadHeaderTimeout` is set.

**Fix:** reject state-changing requests whose `Origin`/`Host` don’t match the
bound address; add read/idle timeouts (write timeout must stay off for SSE).

### 3.6 Lifecycle and swallowed errors

- `schedulerSvc.Stop()` is never called on shutdown — the River client is not
  drained; in-flight runs are killed mid-write.
- Errors discarded in load-bearing places: `_ = c.Refresh(ctx)` in
  `PushUpdate` (a failed re-parse silently broadcasts stale state),
  fire-and-forget `Reconcile` in `WorkspaceChanged`, `webui.DistFS()` failure
  degrading to `nil` without a log line.

### 3.7 `service` package is becoming a monolith

54 files / 16.3k lines in one flat namespace covering asset CRUD, execution,
the direct-executor family, Python intelligence, onboarding, suggestions, SQL
discovery. Function-field `Dependencies` structs make services testable but
hide the call graph. Sub-packages would make boundaries legible. Not urgent.

### 3.8 Smaller items

- Duplicated helpers: `buildDownstreamIndex`, `pathContains`,
  `stripAssetContent(+KeepingIDs)`, `resolvePipelineRunTarget` exist in both
  `cmd/web_compat.go` and `service`.
- `WorkspaceCoordinator.CurrentState()` returns aliased slices/maps;
  `StripAssetContent` copies slice spines but not nested maps. Works because
  consumers are read-only today, but nothing enforces it.
- Full-workspace re-parse + full-state broadcast on every file event. Fine at
  current scale (150ms debounce); `Revision` is already in place if
  incremental diffs are ever needed.

## 4. Refactoring plan (order of attack)

1. **Delete dead duplicated helpers** in `cmd/web_compat.go`.
2. **Unify the model types** and delete conversion layers — unlocks everything
   else.
3. **Consolidate error types**, replace string-matched classification.
4. **Slim down `cmd/web.go`**: re-point `httpapi` interfaces at services, move
   adapter logic into services.
5. **Middleware + lifecycle**: zap logging, recovery, Origin check, scheduler
   `Stop()` on shutdown, server timeouts.
6. **Split `service` into sub-packages** opportunistically (later).

Verification at each step: `go build ./...`, `go vet ./...`,
`go test ./...`, and the live e2e suite (`corepack pnpm test:e2e:live` in
`web/`) at major checkpoints.
