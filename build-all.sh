#!/usr/bin/env bash
# rohy multi-target release build — Linux / macOS host. Mirrors build-all.ps1.
#
# Produces every artefact THIS MACHINE can produce, drops them in artefacts/, and writes a
# checksum manifest. Each target is built by the Taskfile (Taskfile.yml, build/<os>/), run by
# the Wails v3 CLI:
#   go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
#
# ----------------------------------------------------------------------------------------
# Why one machine cannot produce all six
# ----------------------------------------------------------------------------------------
#
#   windows/amd64, windows/arm64  PURE GO — Wails uses the pure-Go WebView2 loader, so these
#                                 cross-build from any host.
#   linux/amd64, linux/arm64      cgo (GTK4 / WebKitGTK 6.0). Needs Linux and the target
#                                 arch's headers.
#   darwin/amd64, darwin/arm64    cgo (WKWebView). Needs macOS and Xcode, which targets both
#                                 architectures from either.
#
# So a Linux host builds the Linux target for its own architecture plus both Windows targets;
# a macOS host builds both Darwin targets plus both Windows targets. What the host cannot do
# is reported as skipped, with the reason — never silently absent. For all six in one run,
# use .github/workflows/release.yml.
#
# Cross-architecture Linux (building arm64 on amd64) would go through the Taskfile's Docker
# cross-compiler. That is deliberately out of scope here: the release workflow uses a native
# arm64 runner instead, which is simpler and less fragile.

set -euo pipefail
cd "$(dirname "$0")"

# SemVer for this build, without the leading "v". REQUIRED, and deliberately not defaulted: the
# version is stated in README.md, and build/config.yml carries it for the packaging files. A default
# here would be one more copy, and some release would eventually ship it stale.
VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: $0 <version>   e.g. $0 1.2.3   (the version README.md states)" >&2
  exit 2
fi
case "$VERSION" in
  v*) echo "error: pass the version without a leading 'v' (got '$VERSION')" >&2; exit 2 ;;
esac
SKIP_TESTS="${SKIP_TESTS:-0}"
TRY_CROSS="${TRY_CROSS:-0}"     # attempt targets this host is expected to fail on
ARTEFACTS="artefacts"

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
      [ "$HOST" = "linux" ] && echo "" || echo "cgo: needs a Linux host (GTK4 / WebKitGTK 6.0)" ;;
    linux/*)
      if [ "$HOST" = "linux" ]; then
        echo "cgo: cross-arch Linux needs the Docker cross-compiler"
      else
        echo "cgo: needs a Linux host (GTK4 / WebKitGTK 6.0)"
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

echo "rohy ${VERSION} (${COMMIT}) built ${DATE}  [host ${HOST}/${HOST_ARCH}]"

# --- Tests -------------------------------------------------------------------------------
if [ "$SKIP_TESTS" != "1" ]; then
  step "Backend tests"
  go test ./backend/...
  step "Frontend tests"
  (cd frontend && npm test)
fi

rm -rf "$ARTEFACTS"
mkdir -p "$ARTEFACTS"

BUILT=""
SKIPPED=""

for platform in $TARGETS; do
  os="${platform%/*}"
  arch="${platform#*/}"
  slug="${os}_${arch}"
  name="rohy_${VERSION}_${slug}"
  reason="$(buildable_reason "$platform")"

  if [ -n "$reason" ] && [ "$TRY_CROSS" != "1" ]; then
    SKIPPED="${SKIPPED}${platform}|${reason}\n"
    continue
  fi

  # macOS ships a .app bundle, which only the package task assembles.
  task="build"
  [ "$os" = "darwin" ] && task="package"

  step "Build $platform"
  # Every target writes the same bin/ path, so each is packaged before the next one builds.
  # dist is deleted and -f reruns the frontend's tasks too, so no target embeds a
  # frontend left over from an earlier one.
  rm -rf frontend/dist
  if ! wails3 task -f "${os}:${task}" "ARCH=${arch}" \
      "VERSION=${VERSION}" "COMMIT=${COMMIT}" "DATE=${DATE}"; then
    if [ -z "$reason" ]; then
      echo "build failed for $platform" >&2
      exit 1
    fi
    warn "$platform failed as expected on this host ($reason)"
    SKIPPED="${SKIPPED}${platform}|attempted; ${reason}\n"
    continue
  fi
  [ -f frontend/dist/index.html ] || { echo "frontend/dist missing after $platform build" >&2; exit 1; }

  case "$os" in
    darwin)
      # Zipped with ditto where available, so the bundle structure, the executable bit and the
      # ad-hoc signature survive.
      [ -d bin/rohy.app ] || { echo "bin/rohy.app missing after $platform build" >&2; exit 1; }
      if command -v ditto >/dev/null 2>&1; then
        ditto -c -k --keepParent bin/rohy.app "${ARTEFACTS}/${name}.zip"
      else
        (cd bin && zip -qr "../${ARTEFACTS}/${name}.zip" rohy.app)
      fi
      BUILT="${BUILT}${platform}|${name}.zip\n"
      continue ;;
    windows) out="bin/rohy.exe" ;;
    *)       out="bin/rohy" ;;
  esac
  [ -f "$out" ] || { echo "$out missing after $platform build" >&2; exit 1; }

  stage="${ARTEFACTS}/${slug}"
  mkdir -p "$stage"
  cp "$out" LICENSE README.md "$stage/"
  case "$os" in
    windows) (cd "$stage" && zip -qr "../${name}.zip" .)
             BUILT="${BUILT}${platform}|${name}.zip\n" ;;
    *)       tar -czf "${ARTEFACTS}/${name}.tar.gz" -C "$stage" .
             BUILT="${BUILT}${platform}|${name}.tar.gz\n" ;;
  esac
  rm -rf "$stage"
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
