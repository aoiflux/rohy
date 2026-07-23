# rohy release build (P12) — Windows host.
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
# Cross-compiling is deliberately NOT attempted here: Wails links the platform's native
# webview (WebView2 / WebKitGTK / WKWebView) through cgo, so each OS must be built on its
# own machine or CI runner. See .github/workflows/release.yml for the full matrix.

[CmdletBinding()]
param(
    # SemVer for this build. Keep in step with backend/version.Version's default.
    [string]$Version = "0.0.1",
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
} catch { }
$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

$pkg = "rohy/backend/version"
$ldflags = "-s -w -X $pkg.Version=$Version -X $pkg.Commit=$commit -X $pkg.Date=$date"

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
Step "Clean frontend build"
if (Test-Path frontend/dist) {
    Remove-Item -Recurse -Force frontend/dist
    Write-Host "removed stale frontend/dist"
}
Push-Location frontend
npm ci --silent 2>$null
if ($LASTEXITCODE -ne 0) { npm install --silent }
npm run build
$buildExit = $LASTEXITCODE
Pop-Location
if ($buildExit -ne 0) { throw "frontend build failed" }
if (-not (Test-Path frontend/dist/index.html)) { throw "frontend/dist is missing after build" }

# --- App build ----------------------------------------------------------------------
Step "Wails build"
# NB: not $args — that is a PowerShell automatic variable and splatting it misbehaves.
$wailsArgs = @("build", "-clean", "-ldflags", $ldflags)
if ($Installer) { $wailsArgs += "-nsis" }
wails @wailsArgs
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

# --- Report -------------------------------------------------------------------------
Step "Artifacts"
Get-ChildItem build/bin -File | ForEach-Object {
    "{0,10:N0} KB  {1}" -f ($_.Length / 1KB), $_.Name
}
Write-Host "`nDone." -ForegroundColor Green
