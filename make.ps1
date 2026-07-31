<#
.SYNOPSIS
  PowerShell replacement for the repo's GNU Makefile.

.DESCRIPTION
  The upstream Makefile shells out to bash scripts, so it does not run on
  Windows without WSL or Git Bash. This script implements the targets that
  matter on Windows natively.

  Windows does not need the Bruin SQL-parser CGo stub that the Makefile builds:
  that shim only exists to satisfy an unconditional native linker flag on
  Linux/macOS.

  Windows DOES need a C compiler. internal/web/adbcutil uses cgo, and the
  server imports it transitively, so renart.exe requires CGO_ENABLED=1 and a
  mingw-w64 gcc on PATH. Only renart-gui.exe builds without cgo.

.EXAMPLE
  .\make.ps1 help
  .\make.ps1 doctor
  .\make.ps1 dev
  .\make.ps1 build
#>

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('help', 'doctor', 'deps', 'web-build', 'go-build', 'gui', 'build', 'dev', 'test', 'check', 'clean')]
    [string]$Target = 'help',

    [Parameter(Position = 1)]
    [string]$Workspace = 'example/example',

    [int]$BackendPort = 3000,
    [int]$FrontendPort = 5173
)

$ErrorActionPreference = 'Stop'
$RepoRoot = $PSScriptRoot
Set-Location $RepoRoot

# Versions this repo's CI pins. Mismatches are usually the cause of weird
# build failures, so `doctor` compares against these.
$WantGo = '1.26.5'
$WantNode = '22.22.1'
$WantPnpm = '10.33.0'

function Write-Step { param($m) Write-Host "==> $m" -ForegroundColor Cyan }
function Write-Ok { param($m) Write-Host "  OK  $m" -ForegroundColor Green }
function Write-Warn2 { param($m) Write-Host "  !!  $m" -ForegroundColor Yellow }
function Write-Bad { param($m) Write-Host "  XX  $m" -ForegroundColor Red }

function Invoke-Checked {
    # Note: the parameter is $ArgList, not $Args. $args is an automatic
    # variable in PowerShell; shadowing it in a param block is legal but
    # fragile, so avoid the name entirely.
    param([string]$Exe, [string[]]$ArgList, [hashtable]$Env = @{})
    $saved = @{}
    foreach ($k in $Env.Keys) {
        $saved[$k] = [Environment]::GetEnvironmentVariable($k)
        [Environment]::SetEnvironmentVariable($k, $Env[$k])
    }
    try {
        & $Exe @ArgList
        if ($LASTEXITCODE -ne 0) { throw "$Exe $($ArgList -join ' ') failed with exit code $LASTEXITCODE" }
    } finally {
        foreach ($k in $saved.Keys) { [Environment]::SetEnvironmentVariable($k, $saved[$k]) }
    }
}

function Get-CCompiler {
    foreach ($cc in 'gcc', 'x86_64-w64-mingw32-gcc', 'clang') {
        $c = Get-Command $cc -ErrorAction SilentlyContinue
        if ($c) { return $c.Source }
    }
    # Common MSYS2 / WinLibs locations that may not be on PATH yet.
    foreach ($p in 'C:\msys64\ucrt64\bin\gcc.exe', 'C:\msys64\mingw64\bin\gcc.exe') {
        if (Test-Path $p) { return $p }
    }
    return $null
}

function Assert-CCompiler {
    if (Get-CCompiler) { return }
    throw @"
No C compiler found, and renart.exe cannot be built without one.

internal/web/adbcutil uses cgo (import "C"), and the server imports it
transitively, so CGO_ENABLED=1 and a working gcc are mandatory on Windows.

Install MSYS2 and the UCRT64 GCC toolchain:

    winget install --id=MSYS2.MSYS2 -e
    C:\msys64\usr\bin\pacman.exe -S --noconfirm mingw-w64-ucrt-x86_64-gcc

Then add C:\msys64\ucrt64\bin to PATH and open a new terminal.
"@
}

function Get-Shell {
    # Windows still ships powershell.exe (5.1), but a PS7-only machine has pwsh.
    if (Get-Command powershell -ErrorAction SilentlyContinue) { return 'powershell' }
    if (Get-Command pwsh -ErrorAction SilentlyContinue) { return 'pwsh' }
    throw "Neither powershell nor pwsh found on PATH."
}

function Get-Pnpm {
    # The repo pins pnpm via package.json "packageManager", so corepack is the
    # supported entrypoint. Fall back to a global pnpm if corepack is absent.
    # Returns a hashtable, not a nested array: PowerShell flattens nested
    # arrays, which would collapse Exe and Prefix into one list.
    if (Get-Command corepack -ErrorAction SilentlyContinue) {
        return @{ Exe = 'corepack'; Prefix = @('pnpm') }
    }
    if (Get-Command pnpm -ErrorAction SilentlyContinue) {
        return @{ Exe = 'pnpm'; Prefix = @() }
    }
    throw "Neither corepack nor pnpm found. Install Node $WantNode, then run: corepack enable"
}

function Invoke-Pnpm {
    # IMPORTANT: runs *inside* $Dir rather than using pnpm's `--dir` flag.
    #
    # This repo has no package.json at its root - only web/, docs/, and
    # extensions/vscode/ have one. Corepack resolves the pnpm version from the
    # "packageManager" field of the CWD's package.json, so invoking
    # `corepack pnpm --dir web ...` from the repo root finds nothing, falls back
    # to whatever pnpm version corepack has globally activated, and then pnpm
    # reads web/package.json, sees the 10.33.0 pin, and refuses to run.
    # Running from inside web/ lets corepack resolve 10.33.0 correctly.
    param([string]$Dir, [string[]]$ArgList, [hashtable]$Env = @{})
    $p = Get-Pnpm
    Push-Location (Join-Path $RepoRoot $Dir)
    try {
        Invoke-Checked -Exe $p.Exe -ArgList (@($p.Prefix) + $ArgList) -Env $Env
    } finally { Pop-Location }
}

# ---------------------------------------------------------------- targets ---

function Target-Help {
    Write-Host ""
    Write-Host "  Renart on Windows" -ForegroundColor White
    Write-Host "  -----------------"
    Write-Host "  .\make.ps1 doctor       Check Go / Node / pnpm / WebView2 / git line endings"
    Write-Host "  .\make.ps1 deps         Install web dependencies (pnpm install)"
    Write-Host "  .\make.ps1 web-build    Build the React frontend into web/dist"
    Write-Host "  .\make.ps1 go-build     Build renart.exe (embeds web/dist)"
    Write-Host "  .\make.ps1 gui          Build renart-gui.exe (the native window helper)"
    Write-Host "  .\make.ps1 build        deps + web-build + go-build + gui"
    Write-Host "  .\make.ps1 dev [ws]     Hot-reload dev servers (air + vite)"
    Write-Host "  .\make.ps1 test         go test ./... + vitest"
    Write-Host "  .\make.ps1 check        Full frontend check + go vet/test"
    Write-Host "  .\make.ps1 clean        Remove build output"
    Write-Host ""
    Write-Host "  After 'build', run .\renart.exe inside any git repo." -ForegroundColor DarkGray
    Write-Host "  In 'dev', open http://127.0.0.1:$FrontendPort (NOT the backend port)." -ForegroundColor DarkGray
    Write-Host ""
}

function Target-Doctor {
    Write-Step "Toolchain"
    $ok = $true

    if (Get-Command go -ErrorAction SilentlyContinue) {
        $v = (& go version) -replace '.*go(\d+\.\d+(\.\d+)?).*', '$1'
        if ($v -eq $WantGo) { Write-Ok "go $v" } else { Write-Warn2 "go $v (CI pins $WantGo)" }
    } else { Write-Bad "go not found - https://go.dev/dl/"; $ok = $false }

    if (Get-Command node -ErrorAction SilentlyContinue) {
        $v = (& node --version).TrimStart('v')
        if ($v -eq $WantNode) { Write-Ok "node $v" } else { Write-Warn2 "node $v (CI pins $WantNode)" }
    } else { Write-Bad "node not found - https://nodejs.org/"; $ok = $false }

    if (Get-Command corepack -ErrorAction SilentlyContinue) {
        Write-Ok "corepack present"
        # Resolve from inside web/ - the repo root has no package.json, so
        # corepack would otherwise report its global default, not the pin.
        Push-Location (Join-Path $RepoRoot 'web')
        try {
            $v = (& corepack pnpm --version 2>&1 | Select-Object -Last 1).ToString().Trim()
            if ($v -eq $WantPnpm) {
                Write-Ok "pnpm $v (resolved in web/)"
            } else {
                Write-Bad "pnpm resolves to $v in web/, but the repo pins $WantPnpm"
                Write-Host "        Fix: corepack install --global pnpm@$WantPnpm" -ForegroundColor DarkGray
                $ok = $false
            }
        } catch {
            Write-Warn2 "corepack present but 'corepack pnpm' failed; run: corepack enable"
        } finally { Pop-Location }
    } else { Write-Bad "corepack not found (ships with Node >=16.9); run: corepack enable"; $ok = $false }

    $cc = Get-CCompiler
    if ($cc) { Write-Ok "C compiler: $cc" }
    else {
        Write-Bad "no C compiler - renart.exe needs cgo (internal/web/adbcutil)"
        Write-Host "        winget install --id=MSYS2.MSYS2 -e" -ForegroundColor DarkGray
        Write-Host "        C:\msys64\usr\bin\pacman.exe -S --noconfirm mingw-w64-ucrt-x86_64-gcc" -ForegroundColor DarkGray
        Write-Host "        then add C:\msys64\ucrt64\bin to PATH" -ForegroundColor DarkGray
        $ok = $false
    }

    Write-Step "Native window helper (WebView2)"
    # Check both the per-machine and per-user keys: a non-admin install only
    # writes HKCU, and checking HKLM alone gives a false negative.
    $wv = Get-ItemProperty 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' -ErrorAction SilentlyContinue
    if (-not ($wv -and $wv.pv)) {
        $wv = Get-ItemProperty 'HKCU:\Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}' -ErrorAction SilentlyContinue
    }
    if ($wv -and $wv.pv) { Write-Ok "WebView2 runtime $($wv.pv)" }
    else { Write-Warn2 "WebView2 runtime not detected - renart-gui.exe needs it (ships with Win11)" }

    Write-Step "Git line endings"
    $autocrlf = (& git config --get core.autocrlf)
    if (-not $autocrlf) { $autocrlf = '(unset)' }
    Write-Host "     core.autocrlf = $autocrlf"
    if (Test-Path (Join-Path $RepoRoot '.gitattributes')) { Write-Ok ".gitattributes present (forces LF)" }
    else { Write-Bad ".gitattributes missing - the tree will drift to CRLF" }
    $dirty = @(& git status --porcelain)
    if ($dirty.Count -gt 200) { Write-Bad "$($dirty.Count) files dirty - likely a line-ending rewrite. Run: git reset --hard HEAD" }
    else { Write-Ok "$($dirty.Count) file(s) dirty" }

    Write-Step "Build artifacts"
    foreach ($f in 'renart.exe', 'renart-gui.exe') {
        if (Test-Path (Join-Path $RepoRoot $f)) { Write-Ok "$f present" } else { Write-Warn2 "$f missing - run .\make.ps1 build" }
    }
    if (Test-Path (Join-Path $RepoRoot 'web\dist\index.html')) { Write-Ok "web/dist built" }
    else { Write-Warn2 "web/dist missing - renart.exe would have no UI to embed" }

    Write-Host ""
    if ($ok) { Write-Host "Toolchain looks usable." -ForegroundColor Green }
    else { Write-Host "Install the missing tools above before building." -ForegroundColor Red }
}

function Target-Deps {
    Write-Step "pnpm install (web)"
    Invoke-Pnpm -Dir 'web' -ArgList @('install')
}

function Target-WebBuild {
    if (-not (Test-Path (Join-Path $RepoRoot 'web\node_modules'))) { Target-Deps }
    Write-Step "Building frontend -> web/dist"
    Invoke-Pnpm -Dir 'web' -ArgList @('build')
}

function Target-GoBuild {
    if (-not (Test-Path (Join-Path $RepoRoot 'web\dist\index.html'))) {
        throw "web/dist is missing. Run '.\make.ps1 web-build' first, or the binary will have no embedded UI."
    }
    Write-Step "Building renart.exe (UI embedded)"
    Assert-CCompiler
    # No -tags webdev: that tag deliberately skips embedding web/dist.
    #
    # CGO is REQUIRED. internal/web/adbcutil/cancel.go does `import "C"`, and
    # main -> cmd -> clientapi -> service -> duckdbsession -> adbcutil, so every
    # build of the server needs a C compiler. Building with CGO_ENABLED=0 fails
    # with "build constraints exclude all Go files in .../adbcutil".
    #
    # Flags mirror .goreleaser.yaml's renart-duckdb-windows-amd64 target, which
    # is the only Windows configuration upstream actually ships.
    Invoke-Checked -Exe 'go' -ArgList @(
        'build', '-trimpath',
        '-tags', 'no_duckdb_arrow',
        '-buildmode=exe',
        '-o', 'renart.exe', '.'
    ) -Env @{ CGO_ENABLED = '1' }
    Write-Ok "renart.exe"
}

function Target-Gui {
    Write-Step "Building renart-gui.exe (native window helper)"
    # Mirrors scripts/build_standalone_helper.sh's windows branch exactly.
    Invoke-Checked -Exe 'go' -ArgList @(
        'build', '-trimpath',
        '-tags', 'standalone,desktop,production',
        '-ldflags', '-s -w -H windowsgui',
        '-o', 'renart-gui.exe', './cmd/renart-gui'
    ) -Env @{ CGO_ENABLED = '0' }
    Write-Ok "renart-gui.exe"
}

function Target-Build {
    Target-WebBuild
    Target-GoBuild
    Target-Gui
    Write-Host ""
    Write-Host "Done. Run .\renart.exe inside a git repo." -ForegroundColor Green
}

function Target-Dev {
    if (-not (Test-Path (Join-Path $RepoRoot 'web\node_modules'))) { Target-Deps }

    # air gives the Go backend hot restart. It is a go install, not a repo dep.
    $gopath = (& go env GOPATH)
    $air = Join-Path $gopath 'bin\air.exe'
    if (-not (Test-Path $air)) {
        Write-Step "Installing air (Go hot-reload)"
        Invoke-Checked -Exe 'go' -ArgList @('install', 'github.com/air-verse/air@latest')
    }

    # `example/` is gitignored upstream — it's a local scratch workspace each
    # dev creates for themselves, not something the clone ships. Bootstrap it as
    # an empty git repo; Renart's welcome screen can then seed it with a demo
    # project, an import, or nothing.
    $wsPath = Join-Path $RepoRoot $Workspace
    if (-not (Test-Path $wsPath)) {
        if ($Workspace -ne 'example/example') {
            throw "Workspace '$Workspace' does not exist."
        }
        Write-Step "Creating scratch workspace at $Workspace (gitignored)"
        New-Item -ItemType Directory -Force -Path $wsPath | Out-Null
        Push-Location $wsPath
        try {
            & git init --quiet
            # Renart requires a git repo and wants at least one commit to diff against.
            Set-Content -Path 'README.md' -Value "# Renart scratch workspace`n`nLocal dev sandbox. Gitignored by the parent repo.`n" -NoNewline
            & git add -A
            & git -c user.name='Renart Dev' -c user.email='dev@localhost' commit -q -m 'init scratch workspace'
        } finally { Pop-Location }
        Write-Ok "scratch workspace ready - use the welcome screen to seed it"
    }

    Write-Host ""
    Write-Host "  Renart dev servers" -ForegroundColor White
    Write-Host "  ------------------"
    Write-Host "  Frontend (open this):  http://127.0.0.1:$FrontendPort" -ForegroundColor Green
    Write-Host "  Backend API (local):   http://127.0.0.1:$BackendPort"
    Write-Host "  Workspace:             $Workspace"
    Write-Host ""
    Write-Host "  Two windows will open. Close them (or Ctrl-C each) to stop." -ForegroundColor DarkGray
    Write-Host ""

    $shell = Get-Shell

    # PowerShell has no job-control equivalent of dev.sh's process-group kill,
    # so each server gets its own window rather than a backgrounded job that
    # would orphan Vite's esbuild children on exit.
    $backendCmd = "`$env:CGO_ENABLED='1'; & '$air' -- web --no-open --host 127.0.0.1 --port $BackendPort '$Workspace'"
    Start-Process $shell -ArgumentList '-NoExit', '-Command', "Set-Location '$RepoRoot'; $backendCmd"

    $p = Get-Pnpm
    $pnpmInvocation = (@($p.Exe) + $p.Prefix) -join ' '
    # Run from web/, not the repo root - see the note on Invoke-Pnpm.
    $frontendCmd = "`$env:PROXY_TARGET='http://127.0.0.1:$BackendPort'; $pnpmInvocation dev --port $FrontendPort"
    Start-Process $shell -ArgumentList '-NoExit', '-Command', "Set-Location '$(Join-Path $RepoRoot 'web')'; $frontendCmd"

    Start-Sleep -Seconds 3
    Start-Process "http://127.0.0.1:$FrontendPort"
}

function Target-Test {
    Write-Step "go test ./..."
    Invoke-Checked -Exe 'go' -ArgList @('test', '-p=1', './...') -Env @{ CGO_ENABLED = '1' }
    Write-Step "vitest"
    Invoke-Pnpm -Dir 'web' -ArgList @('test:unit')
}

function Target-Check {
    Write-Step "go vet ./..."
    Invoke-Checked -Exe 'go' -ArgList @('vet', './...') -Env @{ CGO_ENABLED = '1' }
    Target-Test
    Write-Step "frontend check (format, lint, typecheck, build)"
    Invoke-Pnpm -Dir 'web' -ArgList @('check')
}

function Target-Clean {
    Write-Step "Cleaning"
    foreach ($p in 'renart.exe', 'renart-gui.exe', 'dist', 'web\dist', 'docs\dist', 'docs\.astro', 'tmp') {
        $full = Join-Path $RepoRoot $p
        if (Test-Path $full) { Remove-Item -Recurse -Force $full; Write-Ok "removed $p" }
    }
}

switch ($Target) {
    'help' { Target-Help }
    'doctor' { Target-Doctor }
    'deps' { Target-Deps }
    'web-build' { Target-WebBuild }
    'go-build' { Target-GoBuild }
    'gui' { Target-Gui }
    'build' { Target-Build }
    'dev' { Target-Dev }
    'test' { Target-Test }
    'check' { Target-Check }
    'clean' { Target-Clean }
}
