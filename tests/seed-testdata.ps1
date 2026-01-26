# Seed E2E test databases (PowerShell version for Windows)
# Creates fresh SQLite databases with test data for E2E testing.
#
# Usage: .\seed-testdata.ps1
#
# Requirements: sqlite3 must be in PATH

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$TestDataDir = Join-Path $ScriptDir "testdata"

Write-Host "=== PrintMaster E2E Test Data Seeder ===" -ForegroundColor Cyan
Write-Host ""

# Check for sqlite3
$sqlite3 = Get-Command sqlite3 -ErrorAction SilentlyContinue
if (-not $sqlite3) {
    Write-Host "ERROR: sqlite3 is required but not installed." -ForegroundColor Red
    Write-Host "Download from: https://sqlite.org/download.html"
    Write-Host "Add sqlite3.exe to your PATH"
    exit 1
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
