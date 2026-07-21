#!/usr/bin/env bash

set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="$(mktemp)"
trap 'rm -f "${output}"' EXIT

cd "${root}"

# go-licenses v1.6.0 misidentifies these three module roots even though each
# contains a LICENSE file. The deterministic notice generator independently
# requires and bundles those exact files, so ignoring them here only bypasses
# the classifier bug; it does not omit their notices.
if ! GOTOOLCHAIN="${GOTOOLCHAIN:-go1.26.5}" "${GO:-/usr/local/go/bin/go}" run github.com/google/go-licenses@v1.6.0 check . \
  --disallowed_types=forbidden,unknown \
  --ignore=github.com/DATA-DOG/go-sqlmock \
  --ignore=github.com/segmentio/asm \
  --ignore=modernc.org/mathutil >"${output}" 2>&1; then
  cat "${output}" >&2
  exit 1
fi

node scripts/generate-third-party-notices.mjs --check
printf 'Go dependency license classifications passed.\n'
