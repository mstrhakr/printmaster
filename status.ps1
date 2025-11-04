# PrintMaster Status Check
# Quick overview of project state

$ErrorActionPreference = 'Continue'

function Write-Section {
    param([string]$Title)
    Write-Host "`n╔══════════════════════════════════════════════════════╗" -ForegroundColor Cyan
    Write-Host "║  $($Title.PadRight(50))  ║" -ForegroundColor Cyan
    Write-Host "╚══════════════════════════════════════════════════════╝" -ForegroundColor Cyan
}

function Write-Item {
    param([string]$Label, [string]$Value, [string]$Color = "White")
    Write-Host "  $($Label.PadRight(20)): " -NoNewline
    Write-Host $Value -ForegroundColor $Color
}

# ============================================================================
# VERSION INFO
# ============================================================================
Write-Section "Version Information"

$agentVersion = if (Test-Path "VERSION") { (Get-Content "VERSION" -Raw).Trim() } else { "NOT FOUND" }
$serverVersion = if (Test-Path "server\VERSION") { (Get-Content "server\VERSION" -Raw).Trim() } else { "NOT FOUND" }

Write-Item "Agent Version" $agentVersion "Green"
Write-Item "Server Version" $serverVersion "Green"

# ============================================================================
# GIT STATUS
# ============================================================================
Write-Section "Git Status"

$gitBranch = git rev-parse --abbrev-ref HEAD 2>$null
$gitCommit = git rev-parse --short HEAD 2>$null
$gitRemote = git remote get-url origin 2>$null
$gitStatus = git status --porcelain 2>$null

if ($gitBranch) {
    Write-Item "Branch" $gitBranch "Yellow"
    Write-Item "Commit" $gitCommit "Gray"
    Write-Item "Remote" $gitRemote "Gray"
    
    if ($gitStatus) {
        Write-Item "Working Directory" "UNCOMMITTED CHANGES" "Red"
        Write-Host ""
        $gitStatus | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow }
    } else {
        Write-Item "Working Directory" "Clean ✓" "Green"
    }
} else {
    Write-Host "  Not a git repository" -ForegroundColor Red
}

# Check for unpushed commits
$unpushed = git log origin/$gitBranch..$gitBranch --oneline 2>$null
if ($unpushed) {
    Write-Host ""
    Write-Host "  ⚠️  Unpushed commits:" -ForegroundColor Yellow
    $unpushed | ForEach-Object { Write-Host "    $_" -ForegroundColor Yellow }
}

# ============================================================================
# TAGS
# ============================================================================
Write-Section "Recent Tags"

$recentTags = git tag -l --sort=-version:refname 2>$null | Select-Object -First 5
if ($recentTags) {
    $recentTags | ForEach-Object { Write-Host "  $_" -ForegroundColor Green }
} else {
    Write-Host "  No tags found" -ForegroundColor Gray
}

# ============================================================================
# BUILD STATUS
# ============================================================================
Write-Section "Build Artifacts"

$agentExe = "agent\printmaster-agent.exe"
$serverExe = "server\printmaster-server.exe"

if (Test-Path $agentExe) {
    $agentSize = [math]::Round((Get-Item $agentExe).Length / 1MB, 2)
    $agentTime = (Get-Item $agentExe).LastWriteTime.ToString("yyyy-MM-dd HH:mm:ss")
    Write-Item "Agent Binary" "$agentSize MB (built $agentTime)" "Green"
} else {
    Write-Item "Agent Binary" "NOT BUILT" "Red"
}

if (Test-Path $serverExe) {
    $serverSize = [math]::Round((Get-Item $serverExe).Length / 1MB, 2)
    $serverTime = (Get-Item $serverExe).LastWriteTime.ToString("yyyy-MM-dd HH:mm:ss")
    Write-Item "Server Binary" "$serverSize MB (built $serverTime)" "Green"
} else {
    Write-Item "Server Binary" "NOT BUILT" "Red"
}

# ============================================================================
# RUNNING PROCESSES
# ============================================================================
Write-Section "Running Processes"

$processes = Get-Process | Where-Object { 
    $_.ProcessName -like '*printmaster*' -or 
    $_.Path -like '*printmaster*' -or 
    $_.ProcessName -like '*debug_bin*' 
}

if ($processes) {
    $processes | ForEach-Object {
        $port = ""
        try {
            $connections = Get-NetTCPConnection -OwningProcess $_.Id -ErrorAction SilentlyContinue | 
                          Where-Object { $_.State -eq 'Listen' }
            if ($connections) {
                $port = " (Port: $($connections[0].LocalPort))"
            }
        } catch {}
        
        Write-Host "  $($_.ProcessName)$port" -ForegroundColor Yellow
        
        $startTime = if ($_.StartTime) { $_.StartTime.ToString('HH:mm:ss') } else { "unknown" }
        Write-Host "    PID: $($_.Id) | Started: $startTime" -ForegroundColor Gray
    }
} else {
    Write-Host "  No PrintMaster processes running" -ForegroundColor Gray
}

# ============================================================================
# QUICK ACTIONS
# ============================================================================
Write-Section "Quick Actions"

Write-Host "  Build:   " -NoNewline -ForegroundColor White
Write-Host ".\build.ps1 agent" -ForegroundColor Cyan

Write-Host "  Test:    " -NoNewline -ForegroundColor White
Write-Host ".\build.ps1 test-all" -ForegroundColor Cyan

Write-Host "  Release: " -NoNewline -ForegroundColor White
Write-Host ".\release.ps1 agent patch" -ForegroundColor Cyan

Write-Host "  Debug:   " -NoNewline -ForegroundColor White
Write-Host "Press F5 in VS Code" -ForegroundColor Cyan

Write-Host ""
