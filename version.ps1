# Get Current Version
# Quick helper to show the current version from VERSION file

$versionFile = Join-Path $PSScriptRoot "VERSION"

if (Test-Path $versionFile) {
    $version = (Get-Content $versionFile -Raw).Trim()
    Write-Host "Current Version: " -NoNewline -ForegroundColor Cyan
    Write-Host $version -ForegroundColor Green
} else {
    Write-Host "VERSION file not found!" -ForegroundColor Red
    exit 1
}

# Show git info if available
$gitCommit = (git rev-parse --short HEAD 2>$null) -join ""
if ($gitCommit) {
    Write-Host "Git Commit:      " -NoNewline -ForegroundColor Cyan
    Write-Host $gitCommit -ForegroundColor Yellow
}

$gitBranch = (git rev-parse --abbrev-ref HEAD 2>$null) -join ""
if ($gitBranch) {
    Write-Host "Git Branch:      " -NoNewline -ForegroundColor Cyan
    Write-Host $gitBranch -ForegroundColor Yellow
}

# Check if there's a built binary and show its version
$agentExe = Join-Path $PSScriptRoot "agent\printmaster-agent.exe"
if (Test-Path $agentExe) {
    Write-Host ""
    Write-Host "Built Binary:" -ForegroundColor Cyan
    $builtDate = (Get-Item $agentExe).LastWriteTime
    $builtSize = [math]::Round((Get-Item $agentExe).Length / 1MB, 2)
    Write-Host "  Built:         $builtDate" -ForegroundColor Gray
    Write-Host "  Size:          $builtSize MB" -ForegroundColor Gray
}
