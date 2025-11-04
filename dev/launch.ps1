# Dev launch script for PrintMaster (Windows PowerShell)
# Runs tests, builds the agent, starts the server, opens browser and tries to bring browser window to the front.
# Usage: pwsh -NoProfile -ExecutionPolicy Bypass .\dev\launch.ps1

$ErrorActionPreference = 'Stop'
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Definition
# project root assumed to be one level up from dev directory
Push-Location (Join-Path $scriptRoot '..')

Write-Host "[dev] Running `go test ./...`..."
& go test ./...
if ($LASTEXITCODE -ne 0) {
    Write-Error "Tests failed (exit $LASTEXITCODE). Aborting launch."
    Pop-Location
    exit $LASTEXITCODE
}
Write-Host "[dev] Tests passed. Proceeding to build."

$binDir = Join-Path (Get-Location) 'bin'
if (-not (Test-Path $binDir)) { New-Item -ItemType Directory -Path $binDir | Out-Null }
$exePath = Join-Path $binDir 'printmaster-agent.exe'
Write-Host "[dev] Building agent to $exePath"
& go build -o $exePath ./agent
if ($LASTEXITCODE -ne 0) {
    Write-Error "Build failed (exit $LASTEXITCODE). Aborting launch."
    Pop-Location
    exit $LASTEXITCODE
}
Write-Host "[dev] Build succeeded. Starting server..."

# Start the agent in a new process
$proc = Start-Process -FilePath $exePath -WorkingDirectory (Get-Location) -PassThru
Start-Sleep -Seconds 1

# Open the default browser to the UI
$uiUrl = 'http://localhost:8080'
Write-Host "[dev] Opening browser to $uiUrl"
Start-Process $uiUrl

# Try to bring an existing browser window to the foreground (best-effort)
# Adds user32.dll P/Invoke helpers and attempts to set foreground window for common browsers
$code = @"
using System;
using System.Runtime.InteropServices;
public class Win {
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")]
    public static extern bool ShowWindowAsync(IntPtr hWnd, int nCmdShow);
}
"@
Add-Type $code -ErrorAction SilentlyContinue

$browserNames = @('chrome','msedge','firefox','librewolf','opera')
$found = $false
foreach ($name in $browserNames) {
    try {
        $p = Get-Process -Name $name -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
        if ($p) {
            Write-Host "[dev] Bringing browser process '$($p.ProcessName)' (PID $($p.Id)) to foreground"
            # SW_RESTORE = 9, SW_SHOW = 5
            [Win]::ShowWindowAsync($p.MainWindowHandle, 5) | Out-Null
            [Win]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
            $found = $true
            break
        }
    } catch {
        # ignore and continue
    }
}
if (-not $found) { Write-Host "[dev] No known browser window found to bring to foreground; browser was opened (or may open)." }

Write-Host "[dev] Agent started (PID $($proc.Id)). Use Ctrl+C in this console to keep it alive or close the console to leave the agent running." 

Pop-Location

# Exit with success
exit 0
