# Run E2E Docker tests locally
# This script sets up the Docker environment, runs tests, and cleans up.
#
# Usage: .\run-e2e.ps1 [options]
#   -Build     Force rebuild containers
#   -KeepUp    Don't stop containers after tests
#   -Verbose   Show verbose output
#
# Example:
#   .\run-e2e.ps1
#   .\run-e2e.ps1 -Build -Verbose

param(
    [switch]$Build,
    [switch]$KeepUp,
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

Write-Host "=== PrintMaster E2E Docker Tests ===" -ForegroundColor Cyan
Write-Host ""

# Change to tests directory
Push-Location $ScriptDir
try {
    # Seed test databases
    Write-Host "Seeding test databases..." -ForegroundColor Yellow
    & "$ScriptDir\seed-testdata.ps1"
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to seed test databases"
    }
    Write-Host ""

    # Build and start containers
    Write-Host "Starting Docker containers..." -ForegroundColor Yellow
    $composeArgs = @("-f", "docker-compose.e2e.yml", "up", "-d")
    if ($Build) {
        $composeArgs += "--build"
    }
    $composeArgs += @("server", "agent")
    
    docker compose @composeArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to start containers"
    }

    # Wait for services
    Write-Host ""
    Write-Host "Waiting for services to be healthy..." -ForegroundColor Yellow
    
    $serverReady = $false
    $agentReady = $false
    
    for ($i = 1; $i -le 30; $i++) {
        if (-not $serverReady) {
            try {
                $resp = Invoke-WebRequest -Uri "http://localhost:8443/api/health" -UseBasicParsing -TimeoutSec 2 -ErrorAction SilentlyContinue
                if ($resp.StatusCode -eq 200) {
                    Write-Host "  Server is ready!" -ForegroundColor Green
                    $serverReady = $true
                }
            } catch {
                if ($Verbose) { Write-Host "  Attempt $i`: Server not ready yet..." }
            }
        }
        
        if (-not $agentReady) {
            try {
                $resp = Invoke-WebRequest -Uri "http://localhost:8080/api/health" -UseBasicParsing -TimeoutSec 2 -ErrorAction SilentlyContinue
                if ($resp.StatusCode -eq 200) {
                    Write-Host "  Agent is ready!" -ForegroundColor Green
                    $agentReady = $true
                }
            } catch {
                if ($Verbose) { Write-Host "  Attempt $i`: Agent not ready yet..." }
            }
        }
        
        if ($serverReady -and $agentReady) { break }
        Start-Sleep -Seconds 2
    }
    
    if (-not $serverReady -or -not $agentReady) {
        Write-Host "Services did not become healthy in time!" -ForegroundColor Red
        docker compose -f docker-compose.e2e.yml logs
        throw "Services not healthy"
    }

    # Run E2E tests
    Write-Host ""
    Write-Host "Running E2E tests..." -ForegroundColor Yellow
    Write-Host ""
    
    $env:E2E_SERVER_URL = "http://localhost:8443"
    $env:E2E_AGENT_URL = "http://localhost:8080"
    $env:E2E_ADMIN_PASSWORD = "e2e-test-password"
    
    $testArgs = @("-tags=e2e", "-v", "-count=1", "./...")
    go test @testArgs
    $testExitCode = $LASTEXITCODE

    # Show container status
    if ($Verbose) {
        Write-Host ""
        Write-Host "Container status:" -ForegroundColor Yellow
        docker compose -f docker-compose.e2e.yml ps
    }

    # Cleanup
    if (-not $KeepUp) {
        Write-Host ""
        Write-Host "Stopping containers..." -ForegroundColor Yellow
        docker compose -f docker-compose.e2e.yml down -v
    } else {
        Write-Host ""
        Write-Host "Containers left running. Stop with:" -ForegroundColor Yellow
        Write-Host "  docker compose -f tests/docker-compose.e2e.yml down -v"
    }

    # Final status
    Write-Host ""
    if ($testExitCode -eq 0) {
        Write-Host "=== E2E Tests PASSED ===" -ForegroundColor Green
    } else {
        Write-Host "=== E2E Tests FAILED ===" -ForegroundColor Red
    }
    
    exit $testExitCode

} finally {
    Pop-Location
}
