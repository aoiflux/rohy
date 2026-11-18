# Rohy VI (the VS Code extension) build — Windows host. Mirrors build-extension.sh.
#
# Builds tools/rohy-vi/rohy-vi-<version>.vsix: one package that carries rohyctl for Windows,
# Linux and macOS, on x64 and arm64, so the extension needs nothing else installed. rohyctl is
# pure Go, so every platform cross-builds from this one.
#
# The work is done by tools/rohy-vi/scripts/package.js, which both this script and
# build-extension.sh run, so the two cannot drift. It checks the versions, the rule schema and
# the extension's tests; cross-builds rohyctl, stamped with rohy's version (build/config.yml) and
# the commit; runs the one built for this machine; packages with a pinned vsce; confirms every
# rohyctl is inside; and prints the SHA-256. tools/rohy-vi/README.md says more.
#
# It never publishes: publishing needs the publisher's token, which no script here handles. It
# prints the command to run.
#
# Needs Go and Node 22 or later, with npx (it fetches the pinned vsce).

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

foreach ($tool in "go", "node", "npx") {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "$tool is not on PATH; the extension build needs Go and Node 22 or later"
    }
}
$nodeMajor = [int]((node --version).TrimStart("v").Split(".")[0])
if ($nodeMajor -lt 22) { throw "Node $(node --version) is too old; the extension build needs 22 or later" }

Push-Location tools/rohy-vi
try {
    node scripts/package.js
    if ($LASTEXITCODE -ne 0) { throw "the extension build failed (exit $LASTEXITCODE)" }
}
finally {
    Pop-Location
}
