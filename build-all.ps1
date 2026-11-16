# rohy multi-target release build — Windows host.
#
# Produces every artefact THIS MACHINE can produce, drops them in artefacts/, and writes a
# checksum manifest. Its companion is build-all.sh for Linux and macOS hosts. Each target is
# built by the Taskfile (Taskfile.yml, build/<os>/), run by the Wails v3 CLI:
#   go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
#
# ----------------------------------------------------------------------------------------
# Why one machine cannot produce all six
# ----------------------------------------------------------------------------------------
#
# This was measured, not assumed — by attempting a real cross-build of every target with cgo
# disabled:
#
#   windows/amd64, windows/arm64  PURE GO. Wails uses the pure-Go WebView2 loader on
#                                 Windows, so both cross-build from any host. Verified:
#                                 an arm64 binary built here reports PE machine 0xAA64.
#   linux/amd64, linux/arm64      cgo. The frontend binds GTK4 / WebKitGTK 6.0, so it needs the
#                                 target's headers and toolchain.
#   darwin/amd64, darwin/arm64    cgo. The frontend binds WKWebView, so it needs macOS + Xcode.
#
# So this script builds the two Windows targets and reports the other four as skipped, with
# the reason. Nothing is silently missing. For a full six-target release, use the GitHub
# Actions workflow (.github/workflows/release.yml), which runs one job per host class.
#
# A trap worth knowing: `go list` reports CgoFiles=0 when cross-compiling, because cgo is
# implicitly off and those files move to IgnoredGoFiles. Every target LOOKS pure-Go until
# you actually try to link it. Only a build tells the truth.

[CmdletBinding()]
param(
    # SemVer for this build, without the leading "v". REQUIRED, and deliberately not defaulted:
    # the version lives in README.md and nowhere else, so a default here would be a second copy
    # that some release eventually ships stale.
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^\d+\.\d+\.\d+([-+].+)?$')]
    [string]$Version,
    # Skip the test suites (not recommended for anything you intend to ship).
    [switch]$SkipTests,
    # Attempt the cgo-bound targets anyway. They are expected to fail on this host; the flag
    # exists so a future toolchain change can be tested without editing the script.
    [switch]$TryCrossTargets
)

$ErrorActionPreference = "Stop"
Set-Location -Path $PSScriptRoot

function Step($msg) { Write-Host "`n=== $msg ===" -ForegroundColor Cyan }
function Warn($msg) { Write-Host "  ! $msg" -ForegroundColor Yellow }

$ArtefactDir = Join-Path $PSScriptRoot "artefacts"

# --- Targets -----------------------------------------------------------------------------
# Buildable = this host can link it. See the header for how that was determined. Every target
# writes the same bin/ path, Output names it.
$targets = @(
    @{ Platform = "windows/amd64"; Output = "bin/rohy.exe"; Buildable = $true; Reason = "" }
    @{ Platform = "windows/arm64"; Output = "bin/rohy.exe"; Buildable = $true; Reason = "" }
    @{ Platform = "linux/amd64"; Output = "bin/rohy"; Buildable = $false; Reason = "cgo: needs Linux + GTK4 / WebKitGTK 6.0 headers" }
    @{ Platform = "linux/arm64"; Output = "bin/rohy"; Buildable = $false; Reason = "cgo: needs Linux arm64 + GTK4 / WebKitGTK 6.0 headers" }
    @{ Platform = "darwin/amd64"; Output = "bin/rohy.app"; Buildable = $false; Reason = "cgo: needs macOS + Xcode (WKWebView)" }
    @{ Platform = "darwin/arm64"; Output = "bin/rohy.app"; Buildable = $false; Reason = "cgo: needs macOS + Xcode (WKWebView)" }
)

# --- Build metadata ----------------------------------------------------------------------
# Identical to build.ps1: version, revision and date are injected into the one package every
# surface reads its identity from, and a dirty tree is marked as such so a work-in-progress
# binary can never present itself as a clean release.
$commit = "unknown"
try { $commit = (git rev-parse --short HEAD).Trim() } catch { }
try {
    $porcelain = git status --porcelain
    if ($null -ne $porcelain -and $porcelain.Length -gt 0) { $commit = "$commit-dirty" }
}
catch { }
$date = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")

Write-Host "rohy $Version ($commit) built $date" -ForegroundColor Green

# --- Tests -------------------------------------------------------------------------------
if (-not $SkipTests) {
    Step "Backend tests"
    go test ./backend/...
    if ($LASTEXITCODE -ne 0) { throw "backend tests failed - refusing to build a release" }

    Step "Frontend tests"
    Push-Location frontend
    npm test
    $testExit = $LASTEXITCODE
    Pop-Location
    if ($testExit -ne 0) { throw "frontend tests failed - refusing to build a release" }
}

# --- Artefact directory ------------------------------------------------------------------
if (Test-Path $ArtefactDir) { Remove-Item -Recurse -Force $ArtefactDir }
New-Item -ItemType Directory -Force -Path $ArtefactDir | Out-Null

# --- Build -------------------------------------------------------------------------------
$built = @()
$skipped = @()

foreach ($t in $targets) {
    $platform = $t.Platform
    $os, $arch = $platform -split "/"
    $slug = "${os}_${arch}"
    $name = "rohy_${Version}_${slug}"

    if (-not $t.Buildable -and -not $TryCrossTargets) {
        $skipped += [pscustomobject]@{ Platform = $platform; Reason = $t.Reason }
        continue
    }

    # macOS ships a .app bundle, which only the package task assembles.
    $task = if ($os -eq "darwin") { "package" } else { "build" }

    Step "Build $platform"
    # Every target writes the same bin/ path, so each is packaged before the next one builds.
    # dist is deleted and -f reruns the frontend's tasks too, so no target embeds a
    # frontend left over from an earlier one: the Go binary embeds whatever is in dist, and a
    # stale dist ships an old UI behind a new backend, a failure this project has paid for once.
    if (Test-Path frontend/dist) { Remove-Item -Recurse -Force frontend/dist }
    wails3 task -f "${os}:${task}" "ARCH=$arch" "VERSION=$Version" "COMMIT=$commit" "DATE=$date"
    if ($LASTEXITCODE -ne 0) {
        if ($t.Buildable) { throw "build failed for $platform" }
        Warn "$platform failed as expected on this host ($($t.Reason))"
        $skipped += [pscustomobject]@{ Platform = $platform; Reason = "attempted; $($t.Reason)" }
        continue
    }
    if (-not (Test-Path frontend/dist/index.html)) { throw "frontend/dist is missing after the $platform build" }

    $produced = $t.Output
    if (-not (Test-Path $produced)) { throw "wails3 reported success but $produced is missing" }

    # Package. The licence and readme travel with the binary so an extracted folder is
    # self-describing; a .app bundle is zipped as it stands.
    $zip = Join-Path $ArtefactDir "$name.zip"
    if ($os -eq "darwin") {
        Compress-Archive -Path $produced -DestinationPath $zip -Force
    }
    else {
        $stage = Join-Path $ArtefactDir $slug
        New-Item -ItemType Directory -Force -Path $stage | Out-Null
        Copy-Item $produced $stage
        Copy-Item "LICENSE" $stage
        Copy-Item "README.md" $stage
        Compress-Archive -Path (Join-Path $stage "*") -DestinationPath $zip -Force
        Remove-Item -Recurse -Force $stage
    }

    $built += [pscustomobject]@{ Platform = $platform; Artefact = (Split-Path $zip -Leaf) }
}

# --- Checksums ---------------------------------------------------------------------------
# The manifest the README instructs users to verify against. Written with the bare file name
# so `sha256sum -c` resolves it from inside the artefact directory.
Step "Checksums"
$sumFile = Join-Path $ArtefactDir "SHA256SUMS.txt"
Get-ChildItem $ArtefactDir -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" } | ForEach-Object {
    $h = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$h  $($_.Name)"
} | Set-Content -Path $sumFile -Encoding ascii
Get-Content $sumFile

# --- Report ------------------------------------------------------------------------------
Step "Built"
if ($built.Count -eq 0) { Warn "nothing was built" } else { $built | Format-Table -AutoSize }

if ($skipped.Count -gt 0) {
    Step "Skipped on this host"
    $skipped | Format-Table -AutoSize
    Write-Host "These need their own host. Run build-all.sh on Linux and macOS, or use" -ForegroundColor Yellow
    Write-Host ".github/workflows/release.yml, which covers all six in one run." -ForegroundColor Yellow
}

Write-Host "`nArtefacts: $ArtefactDir" -ForegroundColor Green
