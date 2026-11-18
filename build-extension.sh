#!/usr/bin/env bash
# Rohy VI (the VS Code extension) build — Linux / macOS host. Mirrors build-extension.ps1.
#
# Builds tools/rohy-vi/rohy-vi-<version>.vsix: one package that carries rohyctl for Windows,
# Linux and macOS, on x64 and arm64, so the extension needs nothing else installed. rohyctl is
# pure Go, so every platform cross-builds from this one.
#
# The work is done by tools/rohy-vi/scripts/package.js, which both this script and
# build-extension.ps1 run, so the two cannot drift. It checks the versions, the rule schema and
# the extension's tests; cross-builds rohyctl, stamped with rohy's version (build/config.yml) and
# the commit; runs the one built for this machine; packages with a pinned vsce; confirms every
# rohyctl is inside; and prints the SHA-256. tools/rohy-vi/README.md says more.
#
# It never publishes: publishing needs the publisher's token, which no script here handles. It
# prints the command to run.
#
# Needs Go and Node 22 or later, with npx (it fetches the pinned vsce).

set -euo pipefail
cd "$(dirname "$0")"

for tool in go node npx; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is not on PATH; the extension build needs Go and Node 22 or later" >&2
    exit 1
  fi
done
node_major="$(node --version | sed -e 's/^v//' -e 's/\..*//')"
if [ "$node_major" -lt 22 ]; then
  echo "error: Node $(node --version) is too old; the extension build needs 22 or later" >&2
  exit 1
fi

cd tools/rohy-vi
node scripts/package.js
