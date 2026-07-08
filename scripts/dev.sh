#!/usr/bin/env bash
#
# Hot-reload development environment.
#
#   Frontend: Vite dev server (real HMR — instant, no full rebuild) on :5173,
#             proxying /api to the backend.
#   Backend:  air rebuilds and restarts the Go server on any .go change on :3000.
#
# Open http://127.0.0.1:5173 in the browser. Edit .go files → backend restarts;
# edit anything under web/ → the page hot-updates.
#
# Usage:
#   scripts/dev.sh [workspace-root]        # default workspace: example/example
#   BACKEND_PORT=3000 FRONTEND_PORT=5173 scripts/dev.sh path/to/project
#
set -euo pipefail
# Job control: each background job becomes its own process-group leader, so
# cleanup can signal the whole tree (pnpm → node → vite/esbuild children).
set -m

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

WORKSPACE="${1:-example/example}"
BACKEND_PORT="${BACKEND_PORT:-3000}"
FRONTEND_PORT="${FRONTEND_PORT:-5173}"
HOST="${HOST:-127.0.0.1}"
GO="${GO:-go}"
PNPM="${PNPM:-corepack pnpm}"

# air's build command (in .air.toml) invokes `go` directly, so make sure it is
# resolvable even when only /usr/local/go/bin/go exists.
if ! command -v go >/dev/null 2>&1; then
  if command -v "$GO" >/dev/null 2>&1; then
    PATH="$(cd "$(dirname "$(command -v "$GO")")" && pwd):$PATH"
  elif [ -x /usr/local/go/bin/go ]; then
    PATH="/usr/local/go/bin:$PATH"
  fi
  export PATH
fi

if [ ! -e "$WORKSPACE" ]; then
  echo "error: workspace '$WORKSPACE' does not exist" >&2
  exit 1
fi

# Locate air, installing it on first use if necessary.
locate_air() {
  if command -v air >/dev/null 2>&1; then
    command -v air
    return
  fi
  local gobin
  gobin="$("$GO" env GOPATH)/bin"
  if [ -x "$gobin/air" ]; then
    echo "$gobin/air"
    return
  fi
  echo "air not found — installing github.com/air-verse/air@latest ..." >&2
  "$GO" install github.com/air-verse/air@latest >&2
  echo "$gobin/air"
}
AIR_BIN="$(locate_air)"

if [ ! -d web/node_modules ]; then
  echo "Installing web dependencies ..." >&2
  $PNPM --dir web install
fi

# The backend stays on loopback — only the Vite frontend is exposed on $HOST
# (set HOST=0.0.0.0 to reach it from another machine). Vite proxies /api to the
# backend locally, so the executing server is never directly on the network.
BACKEND_HOST="127.0.0.1"

printf '\n  Renart dev servers\n  ------------------\n'
printf '  Frontend (open this):  http://%s:%s\n' "$HOST" "$FRONTEND_PORT"
printf '  Backend API (local):   http://%s:%s\n' "$BACKEND_HOST" "$BACKEND_PORT"
printf '  Workspace:             %s\n\n' "$WORKSPACE"

pids=()
cleanup() {
  trap - INT TERM EXIT
  printf '\nShutting down dev servers ...\n' >&2
  # Signal each job's whole process group (negative PID) so grandchildren —
  # notably the Vite/esbuild processes pnpm spawns — go down too.
  for pid in "${pids[@]}"; do
    kill -- "-$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

# Backend: air appends everything after `--` as the binary's args.
"$AIR_BIN" -- web --no-open --host "$BACKEND_HOST" --port "$BACKEND_PORT" "$WORKSPACE" &
pids+=($!)

# Frontend: Vite HMR, proxying /api to the backend over loopback.
PROXY_TARGET="http://${BACKEND_HOST}:${BACKEND_PORT}" $PNPM --dir web dev --port "$FRONTEND_PORT" --host "$HOST" &
pids+=($!)

# If either server exits, tear the whole thing down (EXIT trap runs cleanup).
wait -n
