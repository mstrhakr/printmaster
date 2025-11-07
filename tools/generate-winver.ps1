# Generate Windows version resource for Go executables
# This script creates a .syso file with version info embedded in the Windows executable
param(
    [Parameter(Mandatory=$true)]
    [ValidateSet("agent", "server")]
    [string]$Component,
    
    [Parameter(Mandatory=$true)]
    [string]$Version,
    
    [string]$GitCommit = "unknown",
    [string]$BuildTime = (Get-Date -Format "yyyy-MM-dd HH:mm:ss")
)

$ErrorActionPreference = "Stop"

# Parse semantic version
# Input can be "0.3.4" or "0.3.4.9-dev" (dev builds include build number)
# Strip any suffix after dash for parsing
$versionNumeric = ($Version -split '-')[0]
$versionParts = $versionNumeric -split '\.' | ForEach-Object { $_ -replace '[^\d]', '' }

# Ensure we have 4 parts for Windows FILEVERSION
while ($versionParts.Count -lt 4) {
    $versionParts += "0"
}
$versionParts = $versionParts[0..3]
$fileVersion = $versionParts -join ','

# Determine component details
switch ($Component) {
    "agent" {
        $componentName = "PrintMaster Agent"
        $description = "Network printer discovery and monitoring agent"
        $internalName = "printmaster-agent"
        $filename = "printmaster-agent.exe"
    }
    "server" {
        $componentName = "PrintMaster Server"
        $description = "Central management server for PrintMaster agents"
        $internalName = "printmaster-server"
        $filename = "printmaster-server.exe"
    }
}

# Output to current directory (caller should be in component directory)
$outputPath = "."

# Create versioninfo.json for goversioninfo
$versionInfoJson = @{
    "FixedFileInfo" = @{
        "FileVersion" = @{
            "Major" = [int]$versionParts[0]
            "Minor" = [int]$versionParts[1]
            "Patch" = [int]$versionParts[2]
            "Build" = [int]$versionParts[3]
        }
        "ProductVersion" = @{
            "Major" = [int]$versionParts[0]
            "Minor" = [int]$versionParts[1]
            "Patch" = [int]$versionParts[2]
            "Build" = [int]$versionParts[3]
        }
        "FileFlagsMask" = "3f"
        "FileFlags " = "00"
        "FileOS" = "040004"
        "FileType" = "01"
        "FileSubType" = "00"
    }
    "StringFileInfo" = @{
        "CompanyName" = "PrintMaster"
        "FileDescription" = $description
        "FileVersion" = $Version
        "InternalName" = $internalName
        "LegalCopyright" = "Copyright © 2025"
        "OriginalFilename" = $filename
        "ProductName" = $componentName
        "ProductVersion" = $Version
        "Comments" = "Build: $BuildTime, Commit: $GitCommit"
    }
    "VarFileInfo" = @{
        "Translation" = @{
            "LangID" = "0409"
            "CharsetID" = "04B0"
        }
    }
}

# Write versioninfo.json
$jsonPath = Join-Path $outputPath "versioninfo.json"
$versionInfoJson | ConvertTo-Json -Depth 10 | Set-Content -Path $jsonPath -Encoding UTF8

Write-Host "[INFO] Generated version info: $jsonPath" -ForegroundColor Cyan
Write-Host "[INFO] Version: $Version ($fileVersion)" -ForegroundColor Cyan

# Check if goversioninfo is installed
$goversioninfo = Get-Command goversioninfo -ErrorAction SilentlyContinue

if (-not $goversioninfo) {
    Write-Host "[WARN] goversioninfo not found, attempting to install..." -ForegroundColor Yellow
    try {
        go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
        $goversioninfo = Get-Command goversioninfo -ErrorAction SilentlyContinue
    }
    catch {
        Write-Host "[ERROR] Failed to install goversioninfo: $_" -ForegroundColor Red
        Write-Host "[WARN] Continuing without version resource embedding" -ForegroundColor Yellow
        return
    }
}

if ($goversioninfo) {
    Write-Host "[INFO] Generating Windows resource file..." -ForegroundColor Cyan
    Push-Location $outputPath
    try {
        & goversioninfo -o resource.syso
        if ($LASTEXITCODE -eq 0) {
            Write-Host "[SUCCESS] Generated resource.syso" -ForegroundColor Green
        }
        else {
            Write-Host "[ERROR] goversioninfo failed with exit code $LASTEXITCODE" -ForegroundColor Red
        }
    }
    finally {
        Pop-Location
    }
}
