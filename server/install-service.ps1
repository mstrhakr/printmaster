# PrintMaster Server - Service Installation Script
# Requires Administrator privileges

$ErrorActionPreference = "Stop"

# Check for admin privileges
if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "This script must be run as Administrator"
    exit 1
}

$serviceName = "PrintMasterServer"
$exePath = Join-Path $PSScriptRoot "printmaster-server.exe"

Write-Host "PrintMaster Server Service Installation" -ForegroundColor Cyan
Write-Host "=======================================" -ForegroundColor Cyan
Write-Host ""

# Check if executable exists
if (!(Test-Path $exePath)) {
    Write-Error "printmaster-server.exe not found at: $exePath"
    exit 1
}

Write-Host "Executable: $exePath" -ForegroundColor Green

# Check if service already exists
$existingService = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existingService) {
    Write-Host "Service already exists. Stopping and removing..." -ForegroundColor Yellow
    
    # Stop service if running
    if ($existingService.Status -eq 'Running') {
        Write-Host "Stopping service..." -ForegroundColor Yellow
        Stop-Service -Name $serviceName -Force
        Start-Sleep -Seconds 2
    }
    
    # Uninstall existing service
    Write-Host "Uninstalling existing service..." -ForegroundColor Yellow
    & $exePath --service uninstall
    Start-Sleep -Seconds 2
}

# Install service
Write-Host "Installing service..." -ForegroundColor Green
& $exePath --service install

if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to install service"
    exit 1
}

Write-Host "Service installed successfully" -ForegroundColor Green

# Start service
Write-Host "Starting service..." -ForegroundColor Green
& $exePath --service start

if ($LASTEXITCODE -ne 0) {
    Write-Warning "Failed to start service automatically. You can start it manually."
} else {
    Write-Host "Service started successfully" -ForegroundColor Green
}

# Show service status
Write-Host ""
Write-Host "Service Status:" -ForegroundColor Cyan
Get-Service -Name $serviceName | Select-Object Name, Status, StartType | Format-Table

Write-Host ""
Write-Host "Installation complete!" -ForegroundColor Green
Write-Host "Data directory: C:\ProgramData\PrintMaster" -ForegroundColor Gray
Write-Host "Log directory: C:\ProgramData\PrintMaster\logs" -ForegroundColor Gray
Write-Host ""
Write-Host "To manage the service:" -ForegroundColor Cyan
Write-Host "  Start:   .\printmaster-server.exe --service start" -ForegroundColor Gray
Write-Host "  Stop:    .\printmaster-server.exe --service stop" -ForegroundColor Gray
Write-Host "  Restart: .\printmaster-server.exe --service restart" -ForegroundColor Gray
