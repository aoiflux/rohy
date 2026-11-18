# rohy release build — Windows host.
#
# Builds the app for the CURRENT platform with version metadata stamped into the binary.
#
# Two things this script exists to guarantee:
#
#   1. A CLEAN frontend build every time. A stale frontend/dist was the root cause of the
#      "dead UI" incident earlier in this project: the Go binary embeds whatever is in dist,
#      so a skipped or partial frontend build ships a working backend behind an old UI.
#      dist is deleted before every build — never reused, never assumed fresh.
#
#   2. Version metadata that matches reality. Version/commit/date are injected via -ldflags
#      into backend/version, which is the single source the About dialog reads. An unstamped
#      build reports itself as a dev build rather than pretending to be a release.
#
# This script builds for the CURRENT platform only. Release artefacts for all six targets
# come from .github/workflows/release.yml.
#
# Cross-compiling is not attempted here, but the reason is narrower than it looks. On Linux
# and macOS the Wails frontend binds the native webview (WebKitGTK / WKWebView) through cgo,
# so those targets need their own toolchain and headers. Windows does NOT: it uses the
# pure-Go WebView2 loader, so windows/arm64 cross-builds from an amd64 Windows host — which
# is exactly what the release workflow does.
#
# The build itself is Taskfile.yml, run by the Wails v3 CLI:
#   go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0

[CmdletBinding()]
param(
    # SemVer for this build, without the leading "v". REQUIRED, and deliberately not defaulted:
    # the version lives in README.md and nowhere else, so a default here would be a second copy
    # that some release eventually ships stale.
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+([-+].+)?$')]
    [string]$Version,
    # Skip `go test ./backend/...` (not recommended for a release).
    [switch]$SkipTests,
    # Produce an NSIS installer as well as the bare executable (Windows only).
    [switch]$Installer
)

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

function Step($msg) { Write-Host "`n=== $msg ===" -ForegroundColor Cyan }

# --- Build metadata -----------------------------------------------------------------
$commit = "unknown"
try { $commit = (git rev-parse --short HEAD).Trim() } catch { }
try {
    if ((git status --porcelain) -ne $null -and (git status --porcelain).Length -gt 0) {
        $commit = "$commit-dirty"   # a dirty tree must never masquerade as a clean release
    }
}
catch { }
$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

Write-Host "rohy $Version ($commit) built $date" -ForegroundColor Green

# --- Tests --------------------------------------------------------------------------
if (-not $SkipTests) {
    Step "Backend tests"
    go test ./backend/...
    if ($LASTEXITCODE -ne 0) { throw "backend tests failed — refusing to build a release" }

    Step "Frontend tests"
    Push-Location frontend
    npm test
    $testExit = $LASTEXITCODE
    Pop-Location
    if ($testExit -ne 0) { throw "frontend tests failed — refusing to build a release" }
}

# --- Clean frontend build (the hygiene gate) ----------------------------------------
# -f reruns every task, the frontend's too, whatever the task runner remembers of an
# earlier build.
Step "Clean build"
if (Test-Path frontend/dist) {
    Remove-Item -Recurse -Force frontend/dist
    Write-Host "removed stale frontend/dist"
}
$task = if ($Installer) { "package" } else { "build" }
wails3 task -f $task "VERSION=$Version" "COMMIT=$commit" "DATE=$date"
if ($LASTEXITCODE -ne 0) { throw "wails3 task $task failed" }
if (-not (Test-Path frontend/dist/index.html)) { throw "frontend/dist is missing after build" }

# --- Report -------------------------------------------------------------------------
Step "Artifacts"
$out = @(Get-ChildItem bin -File)
if ($Installer) { $out += Get-ChildItem build/windows/nsis -Filter *-installer.exe -File }
$out | ForEach-Object {
    "{0,10:N0} KB  {1}" -f ($_.Length / 1KB), $_.Name
}
Write-Host "`nDone." -ForegroundColor Green
