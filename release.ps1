# PrintMaster Release Script
# Usage: .\release.ps1 [component] [bump-type]
# Components: agent, server, both
# Bump Types: patch, minor, major
# Example: .\release.ps1 agent patch

param(
    [Parameter(Position=0, Mandatory=$true)]
    [ValidateSet('agent', 'server', 'both')]
    [string]$Component,
    
    [Parameter(Position=1, Mandatory=$true)]
    [ValidateSet('patch', 'minor', 'major')]
    [string]$BumpType,
    
    [Parameter(Position=2)]
    [string]$Message = "",
    
    [Parameter()]
    [switch]$SkipTests,
    
    [Parameter()]
    [switch]$SkipPush,
    
    [Parameter()]
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$ProjectRoot = $PSScriptRoot

function Write-Status {
    param([string]$Message, [string]$Level = "INFO")
    $timestamp = Get-Date -Format "HH:mm:ss"
    switch ($Level) {
        "ERROR" { Write-Host "[$timestamp] ❌ $Message" -ForegroundColor Red }
        "WARN"  { Write-Host "[$timestamp] ⚠️  $Message" -ForegroundColor Yellow }
        "SUCCESS" { Write-Host "[$timestamp] ✅ $Message" -ForegroundColor Green }
        "STEP" { Write-Host "`n[$timestamp] 🔹 $Message" -ForegroundColor Cyan }
        default { Write-Host "[$timestamp] ℹ️  $Message" }
    }
}

function Get-GitStatus {
    $status = git status --porcelain 2>&1
    return $status
}

function Test-GitClean {
    $status = Get-GitStatus
    return ($null -eq $status -or $status.Count -eq 0)
}

function Bump-Version {
    param(
        [string]$VersionFile,
        [string]$BumpType
    )
    
    if (-not (Test-Path $VersionFile)) {
        throw "VERSION file not found: $VersionFile"
    }
    
    $currentVersion = (Get-Content $VersionFile -Raw).Trim()
    
    if ($currentVersion -notmatch '^(\d+)\.(\d+)\.(\d+)$') {
        throw "Invalid version format in $VersionFile : $currentVersion (expected x.y.z)"
    }
    
    $major = [int]$Matches[1]
    $minor = [int]$Matches[2]
    $patch = [int]$Matches[3]
    
    switch ($BumpType) {
        'major' {
            $major++
            $minor = 0
            $patch = 0
        }
        'minor' {
            $minor++
            $patch = 0
        }
        'patch' {
            $patch++
        }
    }
    
    $newVersion = "$major.$minor.$patch"
    
    if (-not $DryRun) {
        Set-Content -Path $VersionFile -Value $newVersion -NoNewline
    }
    
    return @{
        Old = $currentVersion
        New = $newVersion
    }
}

function Build-Component {
    param([string]$Component, [string]$Version)
    
    Write-Status "Building $Component..." "STEP"
    
    # Build with -Release flag for optimized, stripped binaries
    if ($VerbosePreference -eq 'Continue') {
        $buildResult = & "$ProjectRoot\build.ps1" $Component -Release -VerboseBuild
    } else {
        $buildResult = & "$ProjectRoot\build.ps1" $Component -Release
    }
    
    if ($LASTEXITCODE -ne 0) {
        throw "Build failed for $Component"
    }
    
    Write-Status "$Component built successfully" "SUCCESS"
    
    # Create versioned release binary
    $componentDir = Join-Path $ProjectRoot $Component
    $sourceBinary = Join-Path $componentDir "printmaster-$Component.exe"
    $releaseBinary = Join-Path $componentDir "printmaster-$Component-v$Version.exe"
    
    if (Test-Path $sourceBinary) {
        Copy-Item $sourceBinary $releaseBinary -Force
        Write-Status "Created release binary: printmaster-$Component-v$Version.exe" "SUCCESS"
    }
}

function Run-Tests {
    param([string]$Component)
    
    if ($SkipTests) {
        Write-Status "Skipping tests (--SkipTests flag)" "WARN"
        return
    }
    
    Write-Status "Running tests for $Component..." "STEP"
    
    Push-Location (Join-Path $ProjectRoot $Component)
    try {
        $testOutput = go test ./... -v 2>&1
        $testExitCode = $LASTEXITCODE
        
        if ($testExitCode -ne 0) {
            Write-Host $testOutput
            throw "Tests failed for $Component"
        }
        
        Write-Status "All tests passed for $Component" "SUCCESS"
    }
    finally {
        Pop-Location
    }
}

function Commit-And-Tag {
    param(
        [string]$Component,
        [string]$Version
    )
    
    Write-Status "Committing version bump..." "STEP"
    
    if ($DryRun) {
        Write-Status "[DRY RUN] Would commit VERSION files" "WARN"
        Write-Status "[DRY RUN] Would tag as v$Version" "WARN"
        return
    }
    
    # Add VERSION files
    if ($Component -eq 'both') {
        git add VERSION server/VERSION
        if ($Message) {
            $commitMsg = "$Message - v$Version"
        } else {
            $commitMsg = "chore: Release v$Version (agent + server)"
        }
    } elseif ($Component -eq 'server') {
        git add server/VERSION
        if ($Message) {
            $commitMsg = "$Message - server v$Version"
        } else {
            $commitMsg = "chore: Release server v$Version"
        }
    } else {
        git add VERSION
        if ($Message) {
            $commitMsg = "$Message - v$Version"
        } else {
            $commitMsg = "chore: Release agent v$Version"
        }
    }
    
    # Commit
    git commit -m $commitMsg
    
    if ($LASTEXITCODE -ne 0) {
        throw "Git commit failed"
    }
    
    Write-Status "Committed: $commitMsg" "SUCCESS"
    
    # Tag
    $tagName = if ($Component -eq 'server') { "server-v$Version" } else { "v$Version" }
    $tagMsg = if ($Component -eq 'both') {
        "Release v${Version} - Agent and Server"
    } elseif ($Component -eq 'server') {
        "Server Release v$Version"
    } else {
        "Agent Release v$Version"
    }
    
    git tag -a $tagName -m $tagMsg
    
    if ($LASTEXITCODE -ne 0) {
        throw "Git tag failed"
    }
    
    Write-Status "Tagged as $tagName" "SUCCESS"
}

function Push-Release {
    if ($SkipPush) {
        Write-Status "Skipping push (--SkipPush flag)" "WARN"
        return
    }
    
    Write-Status "Pushing to GitHub..." "STEP"
    
    if ($DryRun) {
        Write-Status "[DRY RUN] Would push commits and tags" "WARN"
        return
    }
    
    # Push commits
    git push
    if ($LASTEXITCODE -ne 0) {
        throw "Git push failed"
    }
    
    # Push tags
    git push --tags
    if ($LASTEXITCODE -ne 0) {
        throw "Git push tags failed"
    }
    
    Write-Status "Pushed to GitHub successfully" "SUCCESS"
}

# ============================================================================
# MAIN EXECUTION
# ============================================================================

Write-Host ""
Write-Host "╔══════════════════════════════════════════════════════╗" -ForegroundColor Cyan
Write-Host "║          PrintMaster Release Automation             ║" -ForegroundColor Cyan
Write-Host "╚══════════════════════════════════════════════════════╝" -ForegroundColor Cyan
Write-Host ""

Write-Status "Component: $Component" "INFO"
Write-Status "Bump Type: $BumpType" "INFO"
Write-Status "Dry Run: $DryRun" "INFO"
Write-Host ""

try {
    # Pre-flight checks
    Write-Status "Running pre-flight checks..." "STEP"
    
    # Check if we're in a git repository
    $isGitRepo = Test-Path (Join-Path $ProjectRoot ".git")
    if (-not $isGitRepo) {
        throw "Not in a git repository"
    }
    
    # Check for uncommitted changes
    if (-not (Test-GitClean)) {
        Write-Status "Uncommitted changes detected:" "WARN"
        Get-GitStatus | ForEach-Object { Write-Host "  $_" -ForegroundColor Yellow }
        
        $continue = Read-Host "`nContinue anyway? (y/N)"
        if ($continue -ne 'y') {
            throw "Release cancelled - commit or stash changes first"
        }
    } else {
        Write-Status "Working directory is clean" "SUCCESS"
    }
    
    # Check if on main/master branch
    $currentBranch = git rev-parse --abbrev-ref HEAD
    if ($currentBranch -ne 'main' -and $currentBranch -ne 'master') {
        Write-Status "Currently on branch: $currentBranch" "WARN"
        $continue = Read-Host "Not on main branch. Continue? (y/N)"
        if ($continue -ne 'y') {
            throw "Release cancelled"
        }
    }
    
    # Bump version(s)
    Write-Status "Bumping version ($BumpType)..." "STEP"
    
    if ($Component -eq 'both') {
        $agentVersion = Bump-Version -VersionFile (Join-Path $ProjectRoot "agent\VERSION") -BumpType $BumpType
        $serverVersion = Bump-Version -VersionFile (Join-Path $ProjectRoot "server\VERSION") -BumpType $BumpType
        
        Write-Status "Agent: $($agentVersion.Old) → $($agentVersion.New)" "SUCCESS"
        Write-Status "Server: $($serverVersion.Old) → $($serverVersion.New)" "SUCCESS"
        
        $finalVersion = $agentVersion.New  # Use agent version for tag
    } 
    elseif ($Component -eq 'server') {
        $versionInfo = Bump-Version -VersionFile (Join-Path $ProjectRoot "server\VERSION") -BumpType $BumpType
        Write-Status "Server: $($versionInfo.Old) → $($versionInfo.New)" "SUCCESS"
        $finalVersion = $versionInfo.New
    }
    else {
        $versionInfo = Bump-Version -VersionFile (Join-Path $ProjectRoot "agent\VERSION") -BumpType $BumpType
        Write-Status "Agent: $($versionInfo.Old) → $($versionInfo.New)" "SUCCESS"
        $finalVersion = $versionInfo.New
    }
    
    # Run tests
    if ($Component -eq 'both') {
        Run-Tests -Component 'agent'
        Run-Tests -Component 'server'
    } else {
        Run-Tests -Component $Component
    }
    
    # Build release binaries
    if ($Component -eq 'both') {
        $agentVersion = Get-Content (Join-Path $AgentDir 'VERSION') -Raw
        $serverVersion = Get-Content (Join-Path $ServerDir 'VERSION') -Raw
        Build-Component -Component 'agent' -Version $agentVersion.Trim()
        Build-Component -Component 'server' -Version $serverVersion.Trim()
    } else {
        Build-Component -Component $Component -Version $finalVersion
    }
    
    # Commit and tag
    Commit-And-Tag -Component $Component -Version $finalVersion
    
    # Push to GitHub
    Push-Release
    
    # Summary
    Write-Host ""
    Write-Host "╔══════════════════════════════════════════════════════╗" -ForegroundColor Green
    Write-Host "║              Release Complete! 🎉                    ║" -ForegroundColor Green
    Write-Host "╚══════════════════════════════════════════════════════╝" -ForegroundColor Green
    Write-Host ""
    Write-Status "Version: $finalVersion" "SUCCESS"
    Write-Status "Component: $Component" "SUCCESS"
    
    if (-not $SkipPush -and -not $DryRun) {
        $repoUrl = git remote get-url origin 2>$null
        if ($repoUrl) {
            Write-Status "View on GitHub: $repoUrl" "INFO"
        }
    }
    
    Write-Host ""
    
    exit 0
}
catch {
    Write-Host ""
    Write-Status "Release failed: $_" "ERROR"
    Write-Host ""
    Write-Status "To recover:" "WARN"
    Write-Status "  1. Fix the issue" "INFO"
    Write-Status "  2. Revert VERSION changes: git restore VERSION server/VERSION" "INFO"
    Write-Status "  3. Try again" "INFO"
    Write-Host ""
    exit 1
}
