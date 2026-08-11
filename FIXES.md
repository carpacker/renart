# FIXES.md — renart fork fresh-checkout sweep (2026-08-09)

Sweep pattern: fresh `git clone` -> walk the documented core path exactly as a
stranger would -> write every break/gap here, above the line = blocks the core
path, below = hygiene. **Read-only sweep: this is Carson's divergent fork
(`carson/windows-dev`, 5 Windows commits over upstream `renart-data/renart`).
No changes committed — recommendations only.**

## Done-line (core path)

Clone -> `go test ./...` -> `go vet ./...` -> `make dev` (Go backend + pnpm web).

## Verdict

Builds and mostly tests green on this fork. **One test FAILS deterministically
on Windows** — and it is a real platform gap, not a flake.

---

## ABOVE THE LINE — blocks a stranger's walk

### 1. `TestSourceStateDetectsAtomicReplacementWithPreservedMetadata` fails on Windows
- Symptom: `go test ./...` → `FAIL renart/internal/web/snapshot` —
  `source_state_test.go:52` "Should be false".
- Root cause (probed empirically):
  `os.SameFile(before, after)` returns **true** on Windows after
  `os.Rename(repl, asset)` where the replacement preserves size + mtime.
  POSIX inode semantics do not hold on NTFS as Go implements them here — an
  atomic replace with preserved metadata is **not detected**.
- Security relevance: `source_state.go`'s `Equal` relies on `os.SameFile` as
  the "catches atomic replacements that preserve size and modification time"
  guard. On Windows that guard is ineffective — a same-size, same-mtime content
  swap passes as unchanged. The doc comment already concedes this is a
  non-adversarial check, but on Windows it is weaker still than on POSIX.
- Recommended fix (fork, not done — read-only sweep):
  - Test: skip the inode assertion on Windows (`runtime.GOOS == "windows"`) with
    a comment citing this probe, or assert the platform difference explicitly.
  - Optional hardening: when size+mode+mtime all match, fall back to a cheap
    content hash (e.g. first/last N bytes) so the guard is platform-independent.
  - Upstream: file an issue at `renart-data/renart` describing the Windows
    SameFile behavior; the test as written cannot pass on Windows CI.
- Evidence: `go run` probe → `SameFile after atomic replace: true (want false)`.

## BELOW THE LINE — hygiene

### 2. `make dev` requires `make` — not present on every Windows box
- The fork added `make.ps1` (commit `8952c51`), but README/AGENTS still say
  `make dev`. On a box without `make`, the documented dev command fails;
  `.\make.ps1 dev` works. Document both.

### 3. EOL
- Fork commit `65721aa` pins LF via `.gitattributes` — verified clean in this
  clone (no `w/crlf` on Go sources). Good; nothing to do.

## Notes

- Fork tip `a72a727`; upstream origin/main visible at `fe1f44d` (docs) — fork
  tracks upstream but carries Windows fixes. `go vet` clean. All other packages
  pass (`staleness`, `watch`, `static`).
