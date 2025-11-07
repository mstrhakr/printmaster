# PrintMaster Server - Service Uninstallation Script
# Requires Administrator privileges

$ErrorActionPreference = "Stop"

# Check for admin privileges
if (-NOT ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "This script must be run as Administrator"
    exit 1
}

$serviceName = "PrintMasterServer"
$exePath = Join-Path $PSScriptRoot "printmaster-server.exe"

Write-Host "PrintMaster Server Service Uninstallation" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

# Check if service exists
$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if (!$service) {
    Write-Warning "Service '$serviceName' is not installed"
    exit 0
}

# Stop service if running
if ($service.Status -eq 'Running') {
    Write-Host "Stopping service..." -ForegroundColor Yellow
    Stop-Service -Name $serviceName -Force
    Start-Sleep -Seconds 2
}

# Uninstall service
Write-Host "Uninstalling service..." -ForegroundColor Yellow
& $exePath --service uninstall

if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to uninstall service"
    exit 1
}

Write-Host ""
Write-Host "Service uninstalled successfully" -ForegroundColor Green
Write-Host ""
Write-Host "Note: Service data remains at C:\ProgramData\PrintMaster" -ForegroundColor Gray
Write-Host "Remove manually if desired." -ForegroundColor Gray
