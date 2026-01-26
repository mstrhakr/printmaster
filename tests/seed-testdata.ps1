# Seed E2E test databases (PowerShell version for Windows)
# Creates fresh SQLite databases with test data for E2E testing.
#
# Usage: .\seed-testdata.ps1
#
# Requirements: sqlite3 must be in PATH (or will download automatically)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$TestDataDir = Join-Path $ScriptDir "testdata"

Write-Host "=== PrintMaster E2E Test Data Seeder ===" -ForegroundColor Cyan
Write-Host ""

# Check for sqlite3
$sqlite3 = Get-Command sqlite3 -ErrorAction SilentlyContinue
if (-not $sqlite3) {
    Write-Host "WARNING: sqlite3 not found. Attempting to download..." -ForegroundColor Yellow
    
    # Download sqlite3 for Windows
    $sqliteUrl = "https://www.sqlite.org/2024/sqlite-tools-win-x64-3470200.zip"
    $sqliteZip = Join-Path $env:TEMP "sqlite-tools.zip"
    $sqliteDir = Join-Path $ScriptDir "sqlite-tools"
    
    try {
        Invoke-WebRequest -Uri $sqliteUrl -OutFile $sqliteZip
        Expand-Archive -Path $sqliteZip -DestinationPath $sqliteDir -Force
        $sqlite3Path = Get-ChildItem -Path $sqliteDir -Filter "sqlite3.exe" -Recurse | Select-Object -First 1 -ExpandProperty FullName
        $env:PATH = "$($sqlite3Path | Split-Path -Parent);$env:PATH"
        Write-Host "Downloaded sqlite3 successfully" -ForegroundColor Green
    } catch {
        Write-Host "ERROR: Failed to download sqlite3. Please install manually." -ForegroundColor Red
        Write-Host "Download from: https://sqlite.org/download.html"
        Write-Host "Add sqlite3.exe to your PATH"
        exit 1
    }
}

# Create directories if needed
$serverDir = Join-Path $TestDataDir "server"
$agentDir = Join-Path $TestDataDir "agent"
$seedDir = Join-Path $TestDataDir "seed"

New-Item -ItemType Directory -Force -Path $serverDir | Out-Null
New-Item -ItemType Directory -Force -Path $agentDir | Out-Null

# Remove old databases
Write-Host "Removing old test databases..."
$serverDb = Join-Path $serverDir "server.db"
$agentDb = Join-Path $agentDir "agent.db"

if (Test-Path $serverDb) { Remove-Item $serverDb -Force }
if (Test-Path $agentDb) { Remove-Item $agentDb -Force }

# Create server database
Write-Host "Creating server test database..."
$serverSeed = Join-Path $seedDir "server_seed.sql"
Get-Content $serverSeed -Raw | sqlite3 $serverDb

# Create agent database
Write-Host "Creating agent test database..."
$agentSeed = Join-Path $seedDir "agent_seed.sql"
Get-Content $agentSeed -Raw | sqlite3 $agentDb

# Verify databases
Write-Host ""
Write-Host "Verifying databases..."

$serverDevices = (sqlite3 $serverDb "SELECT COUNT(*) FROM devices;") | Out-String
$serverAgents = (sqlite3 $serverDb "SELECT COUNT(*) FROM agents;") | Out-String
$agentDevices = (sqlite3 $agentDb "SELECT COUNT(*) FROM devices;") | Out-String

Write-Host "  Server: $($serverDevices.Trim()) devices, $($serverAgents.Trim()) agents"
Write-Host "  Agent:  $($agentDevices.Trim()) devices"

Write-Host ""
Write-Host "=== Seed complete! ===" -ForegroundColor Green
Write-Host ""
Write-Host "Test databases created at:"
Write-Host "  $serverDb"
Write-Host "  $agentDb"
Write-Host ""
Write-Host "Run E2E tests with:"
Write-Host "  docker compose -f tests/docker-compose.e2e.yml up --build"
