# Uninstall PrintMaster Agent Windows Service
# This script must be run as Administrator

# Check if running as Administrator
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$isAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $isAdmin) {
    Write-Host "ERROR: This script must be run as Administrator" -ForegroundColor Red
    Write-Host ""
    Write-Host "Right-click PowerShell and select 'Run as Administrator', then run this script again."
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host "PrintMaster Agent - Service Uninstallation" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

# Get the directory where this script is located
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$agentExe = Join-Path $scriptDir "printmaster-agent.exe"

# Check if executable exists
if (-not (Test-Path $agentExe)) {
    Write-Host "WARNING: printmaster-agent.exe not found in $scriptDir" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Attempting to uninstall using Windows service manager directly..."
    
    $service = Get-Service -Name "PrintMasterAgent" -ErrorAction SilentlyContinue
    if ($service) {
        Write-Host "Stopping service..." -ForegroundColor Yellow
        Stop-Service -Name "PrintMasterAgent" -Force -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 2
        
        Write-Host "Removing service..." -ForegroundColor Yellow
        sc.exe delete PrintMasterAgent
        
        if ($LASTEXITCODE -eq 0) {
            Write-Host ""
            Write-Host "Service removed successfully!" -ForegroundColor Green
        } else {
            Write-Host ""
            Write-Host "ERROR: Failed to remove service" -ForegroundColor Red
        }
    } else {
        Write-Host ""
        Write-Host "Service not found. Nothing to uninstall." -ForegroundColor Yellow
    }
    
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit 0
}

# Check if service exists
$service = Get-Service -Name "PrintMasterAgent" -ErrorAction SilentlyContinue

if (-not $service) {
    Write-Host "PrintMaster Agent service is not installed." -ForegroundColor Yellow
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit 0
}

Write-Host "Current service status: $($service.Status)" -ForegroundColor Cyan
Write-Host ""

# Confirm uninstallation
$response = Read-Host "Are you sure you want to uninstall the PrintMaster Agent service? (Y/N)"

if ($response -ne "Y" -and $response -ne "y") {
    Write-Host "Uninstallation cancelled." -ForegroundColor Yellow
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit 0
}

Write-Host ""

# Stop the service if running
if ($service.Status -eq "Running") {
    Write-Host "Stopping service..." -ForegroundColor Cyan
    & $agentExe --service stop
    Start-Sleep -Seconds 2
}

# Uninstall the service
Write-Host "Uninstalling PrintMaster Agent service..." -ForegroundColor Cyan
& $agentExe --service uninstall

if ($LASTEXITCODE -ne 0) {
    Write-Host ""
    Write-Host "ERROR: Service uninstallation failed with exit code $LASTEXITCODE" -ForegroundColor Red
    Write-Host ""
    Write-Host "You can try removing it manually with: sc.exe delete PrintMasterAgent"
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit 1
}

Write-Host ""
Write-Host "SUCCESS: Service uninstalled successfully!" -ForegroundColor Green
Write-Host ""

# Ask about data cleanup
Write-Host "Service data is still present in: C:\ProgramData\PrintMaster\" -ForegroundColor Yellow
Write-Host ""
$cleanup = Read-Host "Do you want to delete all service data (databases, logs, config)? (Y/N)"

if ($cleanup -eq "Y" -or $cleanup -eq "y") {
    $dataDir = "C:\ProgramData\PrintMaster"
    
    if (Test-Path $dataDir) {
        Write-Host ""
        Write-Host "Removing data directory..." -ForegroundColor Cyan
        
        try {
            Remove-Item -Path $dataDir -Recurse -Force -ErrorAction Stop
            Write-Host "Data directory removed successfully." -ForegroundColor Green
        } catch {
            Write-Host "ERROR: Failed to remove data directory: $_" -ForegroundColor Red
            Write-Host "You may need to manually delete: $dataDir"
        }
    } else {
        Write-Host "Data directory not found, nothing to clean up." -ForegroundColor Yellow
    }
} else {
    Write-Host ""
    Write-Host "Data directory preserved at: C:\ProgramData\PrintMaster\" -ForegroundColor Yellow
    Write-Host "You can manually delete it later if needed."
}

Write-Host ""
Read-Host "Press Enter to exit"
