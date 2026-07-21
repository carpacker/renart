# Releasing Renart

Renart releases are intentionally tag-driven. Do not publish from an unclean
checkout or bypass the release workflow for an ordinary release.

## Prepare

1. Start a `release/vX.Y.Z` branch and update release-facing versions and
   notes.
2. Run `make release-check` and the full live Playwright suite.
3. Confirm the release snapshot workflow passes. It cross-builds every archive,
   validates checksums, licenses, SBOMs, executable startup, and the Linux
   glibc 2.31 compatibility ceiling.
4. Review the generated change log and the public-alpha limitations.

## Publish

Create and push an annotated stable `vMAJOR.MINOR.PATCH` tag. The workflow:

1. builds and validates the Python SDK wheel;
2. creates a draft GitHub release with checksum, SBOM, and provenance data;
3. smoke-tests the release archives;
4. publishes the wheel through PyPI trusted publishing; and
5. makes the GitHub release public only after PyPI succeeds.

The `pypi` GitHub environment must require the intended reviewers and have a
trusted publisher configured for the `renart` project. No long-lived PyPI token
belongs in repository secrets.

The cross-builder image, Rust toolchain, Zig linker, and Syft SBOM generator are
all pinned. Update their versions and checksums deliberately, then validate a
snapshot before changing a release tag.

## Failure handling

If any post-build validation or PyPI publication fails, the GitHub release
remains a draft. Diagnose it there, delete the draft and tag if necessary, and
release a new patch version. Never replace public artifacts for a version that
users may already have downloaded.
