#!/usr/bin/env bash
# rohy release build (P12) — Linux / macOS host. Mirrors build.ps1.
#
# The same two guarantees as the Windows script:
#   1. frontend/dist is deleted and rebuilt every time. The Go binary embeds whatever is in
#      dist, so a stale build ships an old UI behind a new backend — that exact failure cost
#      real debugging time in this project, so it is designed out rather than remembered.
#   2. Version/commit/date are injected into backend/version, the single source the About
#      dialog reads, so the reported version is always what was actually built.
#
# Cross-compiling is not attempted: Wails links the platform's native webview through cgo,
# so each OS builds on its own machine/runner. See .github/workflows/release.yml.

set -euo pipefail
cd "$(dirname "$0")"

VERSION="${1:-0.0.1}"
SKIP_TESTS="${SKIP_TESTS:-0}"

step() { printf '\n=== %s ===\n' "$1"; }

# --- Build metadata -----------------------------------------------------------------
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
if [ -n "$(git status --porcelain 2>/dev/null || true)" ]; then
  COMMIT="${COMMIT}-dirty"   # a dirty tree must never masquerade as a clean release
fi
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

PKG="rohy/backend/version"
LDFLAGS="-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Date=${DATE}"

echo "rohy ${VERSION} (${COMMIT}) built ${DATE}"

# --- Tests --------------------------------------------------------------------------
if [ "$SKIP_TESTS" != "1" ]; then
  step "Backend tests"
  go test ./backend/...

  step "Frontend tests"
  (cd frontend && npm test)
fi

# --- Clean frontend build (the hygiene gate) ----------------------------------------
step "Clean frontend build"
rm -rf frontend/dist
(cd frontend && (npm ci --silent || npm install --silent) && npm run build)
[ -f frontend/dist/index.html ] || { echo "frontend/dist missing after build" >&2; exit 1; }

# --- App build ----------------------------------------------------------------------
step "Wails build"
wails build -clean -ldflags "$LDFLAGS"

step "Artifacts"
ls -lh build/bin
echo
echo "Done."
