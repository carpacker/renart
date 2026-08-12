# FIXES.md - Renart fork fresh-checkout sweep (2026-08-09)

Sweep pattern: fresh `git clone` -> walk the documented core path exactly as a
stranger would -> write every break or gap here. This is Carson's divergent fork
on `carson/windows-dev`, five Windows commits over upstream `renart-data/renart`.

## Done-line (core path)

Clone -> `go test ./...` -> `go vet ./...` -> `make dev` (Go backend + pnpm web).

## Verdict

Builds and the focused source-state tests are green on this fork. The only
deterministic Windows failure from the fresh-checkout sweep was a POSIX-only
file-identity assumption in a test; it is now documented and asserted honestly.

## Resolved core-path finding

### 1. Atomic replacement with preserved metadata follows platform file identity

- Root cause: `os.SameFile(before, after)` returns `true` on Windows after an
  `os.Rename(repl, asset)` replacement that preserves size and modification time.
  Go does use Windows volume and file-index identity, but resolves it lazily by
  reopening the `FileInfo` path; after rename, both captures resolve the
  replacement path rather than preserving the old file identity.
- Resolution: the source-state test asserts the platform difference explicitly.
  Windows expects the metadata-only state to remain equal; non-Windows retains
  the replacement-detection assertion. The `Equal` comment now qualifies its
  replacement detection by platform capability.
- Boundary: `SourceState` remains a cheap, content-free, non-adversarial
  time-of-check guard. The content-addressed manifest remains canonical source
  identity; adding a second content hash is intentionally out of scope.
- Evidence: `go test ./internal/web/snapshot -run SourceState -count=1` passes
  on Windows with the observed `SameFile` behavior.

## Other hygiene

### 2. Windows dev launcher documentation is complete

- The fork added `make.ps1` (commit `8952c51`), but CONTRIBUTING and AGENTS said
  `make dev`. The contributor and agent guidance now name the PowerShell
  `./make.ps1 dev [workspace]` equivalent and its port parameters.

### 3. EOL

- Fork commit `65721aa` pins LF via `.gitattributes`; this clone verified no
  CRLF warning on Go sources.

## Notes

- Fork tip was `a72a727` during the original sweep. `go vet` was clean; all
  other focused packages passed (`staleness`, `watch`, `static`).
