#!/usr/bin/env bash
# rohy multi-target release build — Linux / macOS host. Mirrors build-all.ps1.
#
# Produces every artefact THIS MACHINE can produce, drops them in build/artefacts, and
# writes a checksum manifest.
#
# ----------------------------------------------------------------------------------------
# Why one machine cannot produce all six
# ----------------------------------------------------------------------------------------
#
# Measured by attempting a real cross-build of every target with cgo disabled:
#
#   windows/amd64, windows/arm64  PURE GO — Wails uses the pure-Go WebView2 loader, so these
#                                 cross-build from any host.
#   linux/amd64, linux/arm64      cgo (WebKitGTK). Needs Linux and the target arch's headers.
#   darwin/amd64, darwin/arm64    cgo (WKWebView). Needs macOS and Xcode, which targets both
#                                 architectures from either.
#
# So a Linux host builds the Linux target for its own architecture plus both Windows targets;
# a macOS host builds both Darwin targets plus both Windows targets. What the host cannot do
# is reported as skipped, with the reason — never silently absent. For all six in one run,
# use .github/workflows/release.yml.
#
# Cross-architecture Linux (building arm64 on amd64) needs a cross toolchain and a target
# sysroot carrying the GTK/WebKit headers. That is deliberately out of scope here: the CI
# workflow uses a native arm64 runner instead, which is simpler and less fragile.

set -euo pipefail
cd "$(dirname "$0")"

VERSION="${1:-0.1.0}"
SKIP_TESTS="${SKIP_TESTS:-0}"
TRY_CROSS="${TRY_CROSS:-0}"     # attempt targets this host is expected to fail on
ARTEFACTS="build/artefacts"

step() { printf '\n=== %s ===\n' "$1"; }
warn() { printf '  ! %s\n' "$1" >&2; }

HOST_OS="$(uname -s)"
case "$HOST_OS" in
  Linux)  HOST="linux" ;;
  Darwin) HOST="darwin" ;;
  *) echo "unsupported host $HOST_OS; use build-all.ps1 on Windows" >&2; exit 1 ;;
esac

# Normalise the host architecture to Go's naming.
case "$(uname -m)" in
  x86_64|amd64)  HOST_ARCH="amd64" ;;
  arm64|aarch64) HOST_ARCH="arm64" ;;
  *) echo "unsupported host architecture $(uname -m)" >&2; exit 1 ;;
esac

# --- Targets -----------------------------------------------------------------------------
# buildable_reason echoes an empty string when this host can link the target, or the reason
# it cannot. Keeping the rule in one function stops the two scripts drifting apart.
buildable_reason() {
  case "$1" in
    windows/*) echo "" ;;                       # pure Go, cross-builds anywhere
    linux/"$HOST_ARCH")
      [ "$HOST" = "linux" ] && echo "" || echo "cgo: needs a Linux host (WebKitGTK)" ;;
    linux/*)
      if [ "$HOST" = "linux" ]; then
        echo "cgo: cross-arch Linux needs a cross toolchain and target sysroot"
      else
        echo "cgo: needs a Linux host (WebKitGTK)"
      fi ;;
    darwin/*)
      [ "$HOST" = "darwin" ] && echo "" || echo "cgo: needs macOS + Xcode (WKWebView)" ;;
    *) echo "unknown target" ;;
  esac
}

TARGETS="windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

# --- Build metadata ----------------------------------------------------------------------
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
if [ -n "$(git status --porcelain 2>/dev/null || true)" ]; then
  COMMIT="${COMMIT}-dirty"   # a dirty tree must never masquerade as a clean release
fi
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PKG="rohy/backend/version"
LDFLAGS="-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Date=${DATE}"

echo "rohy ${VERSION} (${COMMIT}) built ${DATE}  [host ${HOST}/${HOST_ARCH}]"

# --- Tests -------------------------------------------------------------------------------
if [ "$SKIP_TESTS" != "1" ]; then
  step "Backend tests"
  go test ./backend/...
  step "Frontend tests"
  (cd frontend && npm test)
fi

# --- Clean frontend build ----------------------------------------------------------------
# Once for the whole run: the embedded frontend is identical for every target. Deleted rather
# than reused, because the binary embeds whatever is in dist and a stale dist ships an old UI
# behind a new backend.
step "Clean frontend build"
rm -rf frontend/dist
(cd frontend && (npm ci --silent || npm install --silent) && npm run build)
[ -f frontend/dist/index.html ] || { echo "frontend/dist missing after build" >&2; exit 1; }

rm -rf "$ARTEFACTS"
mkdir -p "$ARTEFACTS"

BUILT=""
SKIPPED=""

for platform in $TARGETS; do
  slug="${platform//\//_}"
  name="rohy_${VERSION}_${slug}"
  reason="$(buildable_reason "$platform")"

  if [ -n "$reason" ] && [ "$TRY_CROSS" != "1" ]; then
    SKIPPED="${SKIPPED}${platform}|${reason}\n"
    continue
  fi

  step "Build $platform"
  case "$platform" in
    windows/*) out="rohy.exe" ;;
    *)         out="rohy" ;;
  esac

  # -skipbindings: the generated TypeScript bindings are identical for every target, so
  # regenerating them per target only costs time.
  if ! wails build -clean -skipbindings -platform "$platform" -ldflags "$LDFLAGS" -o "$out"; then
    if [ -z "$reason" ]; then
      echo "build failed for $platform" >&2
      exit 1
    fi
    warn "$platform failed as expected on this host ($reason)"
    SKIPPED="${SKIPPED}${platform}|attempted; ${reason}\n"
    continue
  fi

  stage="${ARTEFACTS}/${slug}"
  mkdir -p "$stage"

  case "$platform" in
    darwin/*)
      # macOS ships a .app bundle. It is archived rather than tarred so the bundle structure
      # and the executable bit survive; ditto is used where available for the same reason.
      if [ -d "build/bin/rohy.app" ]; then
        if command -v ditto >/dev/null 2>&1; then
          ditto -c -k --keepParent "build/bin/rohy.app" "${ARTEFACTS}/${name}.zip"
        else
          (cd build/bin && zip -qr "../../${ARTEFACTS}/${name}.zip" "rohy.app")
        fi
        rm -rf "$stage"
        BUILT="${BUILT}${platform}|${name}.zip\n"
        continue
      fi
      cp "build/bin/$out" "$stage/" ;;
    *)
      cp "build/bin/$out" "$stage/" ;;
  esac

  cp LICENSE README.md "$stage/"
  case "$platform" in
    windows/*) (cd "$stage" && zip -qr "../${name}.zip" .) ;;
    *)         tar -czf "${ARTEFACTS}/${name}.tar.gz" -C "$stage" . ;;
  esac
  rm -rf "$stage"

  case "$platform" in
    windows/*) BUILT="${BUILT}${platform}|${name}.zip\n" ;;
    *)         BUILT="${BUILT}${platform}|${name}.tar.gz\n" ;;
  esac
done

# --- Checksums ---------------------------------------------------------------------------
# Bare file names so `sha256sum -c SHA256SUMS.txt` works from inside the directory.
step "Checksums"
(
  cd "$ARTEFACTS"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum ./* > SHA256SUMS.txt 2>/dev/null || true
  else
    shasum -a 256 ./* > SHA256SUMS.txt 2>/dev/null || true
  fi
  # Drop the manifest's own line and normalise the "./" prefix.
  grep -v "SHA256SUMS.txt" SHA256SUMS.txt | sed 's| \./| |' > SHA256SUMS.tmp || true
  mv SHA256SUMS.tmp SHA256SUMS.txt
  cat SHA256SUMS.txt
)

# --- Report ------------------------------------------------------------------------------
step "Built"
if [ -z "$BUILT" ]; then warn "nothing was built"; else printf "%b" "$BUILT" | column -t -s'|'; fi

if [ -n "$SKIPPED" ]; then
  step "Skipped on this host"
  printf "%b" "$SKIPPED" | column -t -s'|'
  echo
  echo "These need their own host. Run build-all.ps1 on Windows, build-all.sh on the other"
  echo "platform, or use .github/workflows/release.yml, which covers all six in one run."
fi

echo
echo "Artefacts: $ARTEFACTS"
