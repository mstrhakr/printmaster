# PrintMaster Build Script
# Usage: .\build.ps1 [target] [options]
# Targets: agent, all, clean, test
# Options: -Verbose

param(
    [Parameter(Position=0)]
    [ValidateSet('agent', 'server', 'both', 'all', 'clean', 'test', 'test-storage', 'test-all')]
    [string]$Target = 'agent',
    
    [Parameter()]
    [switch]$Release,
    
    [Parameter()]
    [switch]$VerboseBuild,
    
    [Parameter()]
    [switch]$IncrementVersion
)

$ErrorActionPreference = 'Continue'
$ProjectRoot = $PSScriptRoot
$LogDir = Join-Path $ProjectRoot "logs"
$LogFile = $null  # Will be set dynamically with version info
$MaxLogFiles = 10

# Ensure logs directory exists
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Path $LogDir -Force | Out-Null
}

function Write-BuildLog {
    param([string]$Message, [string]$Level = "INFO")
    
    # Use default log if $LogFile not set yet
    if (-not $script:LogFile) {
        $script:LogFile = Join-Path $LogDir "build.log"
    }
    
    $timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    $logMessage = "[$timestamp] [$Level] $Message"
    
    # Write to console
    switch ($Level) {
        "ERROR" { Write-Host $logMessage -ForegroundColor Red }
        "WARN"  { Write-Host $logMessage -ForegroundColor Yellow }
        "SUCCESS" { Write-Host $logMessage -ForegroundColor Green }
        default { Write-Host $logMessage }
    }
    
    # Append to log file
    Add-Content -Path $script:LogFile -Value $logMessage
}

function Set-BuildLogFile {
    param(
        [string]$Component,
        [string]$Version,
        [int]$BuildNumber
    )
    
    $timestamp = Get-Date -Format "yyyyMMdd_HHmmss"
    $logFileName = "build_${Component}_${Version}.${BuildNumber}_${timestamp}.log"
    $script:LogFile = Join-Path $LogDir $logFileName
    
    Write-BuildLog "=== Build Log ===" "INFO"
    Write-BuildLog "Component: $Component" "INFO"
    Write-BuildLog "Version: $Version.$BuildNumber" "INFO"
    Write-BuildLog "Log File: $logFileName" "INFO"
    
    # Clean up old log files, keep only recent ones
    Get-ChildItem $LogDir -Filter "build_*.log" | 
        Sort-Object LastWriteTime -Descending | 
        Select-Object -Skip $MaxLogFiles | 
        Remove-Item -Force -ErrorAction SilentlyContinue
}

function Remove-BuildArtifacts {
    Write-BuildLog "Cleaning build artifacts..."
    
    $patterns = @(
        "agent\*.exe",
        "agent\*.syso",
        "ui\*.exe"
    )
    
    foreach ($pattern in $patterns) {
        $path = Join-Path $ProjectRoot $pattern
        Get-ChildItem $path -ErrorAction SilentlyContinue | ForEach-Object {
            Write-BuildLog "Removing $($_.Name)" "INFO"
            Remove-Item $_.FullName -Force
        }
    }
    
    Write-BuildLog "Clean complete" "SUCCESS"
}

function Build-Agent {
    param(
        [bool]$IsRelease = $false,
        [switch]$IncrementVersion = $false
    )
    
    $buildType = if ($IsRelease) { "RELEASE" } else { "DEV" }
    Write-BuildLog "Building PrintMaster Agent ($buildType)..."
    Write-BuildLog "Working directory: $(Get-Location)"
    
    Push-Location (Join-Path $ProjectRoot "agent")
    
    try {
        # Read version from VERSION file
        $versionFile = Join-Path $ProjectRoot "agent\VERSION"
        if (Test-Path $versionFile) {
            $version = (Get-Content $versionFile -Raw).Trim()
        } else {
            $version = "0.0.0"
            Write-BuildLog "VERSION file not found, using default: $version" "WARN"
        }
        
        # Auto-increment version for release builds if requested
        if ($IsRelease -and $IncrementVersion) {
            # Parse semantic version (major.minor.patch)
            if ($version -match '^(\d+)\.(\d+)\.(\d+)$') {
                $major = [int]$Matches[1]
                $minor = [int]$Matches[2]
                $patch = [int]$Matches[3]
                
                # Increment patch version
                $patch++
                $version = "$major.$minor.$patch"
                
                # Save new version
                Set-Content -Path $versionFile -Value $version -NoNewline
                Write-BuildLog "Version incremented to: $version" "SUCCESS"
            } else {
                Write-BuildLog "Invalid version format in VERSION file, expected x.y.z" "WARN"
            }
        }
        
        # Get or increment build number (reset on version change)
        $buildNumberFile = Join-Path $ProjectRoot "agent\.buildnumber"
        $lastVersionFile = Join-Path $ProjectRoot "agent\.lastversion"
        
        # Check if version changed
        $lastVersion = ""
        if (Test-Path $lastVersionFile) {
            $lastVersion = (Get-Content $lastVersionFile -Raw).Trim()
        }
        
        if ($lastVersion -ne $version) {
            # Version changed, reset build number
            $buildNumber = 1
            Write-BuildLog "Version changed from $lastVersion to $version, resetting build number" "INFO"
        } else {
            # Same version, increment build number
            if (Test-Path $buildNumberFile) {
                $buildNumber = [int](Get-Content $buildNumberFile -Raw).Trim()
                $buildNumber++
            } else {
                $buildNumber = 1
            }
        }
        
        # Save build number and version
        Set-Content -Path $buildNumberFile -Value $buildNumber -NoNewline
        Set-Content -Path $lastVersionFile -Value $version -NoNewline
        
        # Create version string
        # Release: x.y.z (clean semantic version)
        # Dev: x.y.z.build-dev (includes build number for tracking)
        if ($IsRelease) {
            $versionString = "$version"
        } else {
            $versionString = "$version.$buildNumber-dev"
        }
        
        # Set versioned log file
        Set-BuildLogFile -Component "agent" -Version $version -BuildNumber $buildNumber
        
        # Get build metadata
        $buildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        $gitCommit = (git rev-parse --short HEAD 2>$null) -join ""
        if (-not $gitCommit) { $gitCommit = "unknown" }
        
        # Build ldflags for version injection
        $buildTypeString = if ($IsRelease) { "release" } else { "dev" }
        $ldflags = "-X 'main.Version=$versionString' -X 'main.BuildTime=$buildTime' -X 'main.GitCommit=$gitCommit' -X 'main.BuildType=$buildTypeString'"
        
        # Generate Windows version resource (only on Windows)
        # Must happen AFTER $versionString is created so we can embed build number
        if ($IsWindows -or $env:OS -eq "Windows_NT") {
            Write-BuildLog "Generating Windows version resource..."
            $winverScript = Join-Path $ProjectRoot "tools\generate-winver.ps1"
            if (Test-Path $winverScript) {
                try {
                    & $winverScript -Component "agent" -Version $versionString -GitCommit $gitCommit -BuildTime $buildTime 2>&1 | ForEach-Object {
                        Write-BuildLog $_.ToString() "INFO"
                    }
                }
                catch {
                    Write-BuildLog "Warning: Failed to generate version resource: $_" "WARN"
                }
            }
        }
        
        # Build arguments
        $buildArgs = @("build")
        
        if ($IsRelease) {
            # Release build: optimized, stripped, no debug info
            Write-BuildLog "Building optimized release binary..."
            $ldflags += " -s -w"  # Strip debug info and symbol table
            $buildArgs += "-trimpath"  # Remove local file paths for security
        } else {
            # Dev build: keep debug info for troubleshooting
            Write-BuildLog "Building development binary (with debug info)..."
        }
        
        $buildArgs += "-ldflags", $ldflags
        
        # Add verbose flag if requested
        if ($VerboseBuild) {
            $buildArgs += "-v"
        }
        
        # Output file
        $buildArgs += "-o", "printmaster-agent.exe"
        $buildArgs += "."
        
        Write-BuildLog "Version: $versionString (build #$buildNumber)"
        Write-BuildLog "Command: go $($buildArgs -join ' ')"
        Write-BuildLog "Build Time: $buildTime"
        Write-BuildLog "Git Commit: $gitCommit"
        
        # Execute build with full output capture
        $buildOutput = & go @buildArgs 2>&1
        $buildExitCode = $LASTEXITCODE
        
        # Log all build output
        if ($buildOutput) {
            $buildOutput | ForEach-Object { 
                $line = $_.ToString()
                Write-BuildLog $line
            }
        }
        
        if ($buildExitCode -eq 0) {
            # Get file size
            if (Test-Path "printmaster-agent.exe") {
                $fileSize = (Get-Item "printmaster-agent.exe").Length
                $fileSizeMB = [math]::Round($fileSize / 1MB, 2)
                Write-BuildLog "Build successful: printmaster-agent.exe ($fileSizeMB MB)" "SUCCESS"
                Write-BuildLog "Binary location: $(Join-Path (Get-Location) 'printmaster-agent.exe')" "SUCCESS"
            } else {
                Write-BuildLog "Build reported success but executable not found!" "ERROR"
                return $false
            }
            
            return $true
        } else {
            Write-BuildLog "Build failed with exit code $buildExitCode" "ERROR"
            return $false
        }
    }
    finally {
        Pop-Location
    }
}

function Build-Server {
    param(
        [bool]$IsRelease = $false,
        [switch]$IncrementVersion = $false
    )
    
    $buildType = if ($IsRelease) { "RELEASE" } else { "DEV" }
    Write-BuildLog "Building PrintMaster Server ($buildType)..."
    Write-BuildLog "Working directory: $(Get-Location)"
    
    Push-Location (Join-Path $ProjectRoot "server")
    
    try {
        # Read version from server/VERSION file
        $versionFile = Join-Path $ProjectRoot "server\VERSION"
        if (Test-Path $versionFile) {
            $version = (Get-Content $versionFile -Raw).Trim()
        } else {
            $version = "0.0.0"
            Write-BuildLog "VERSION file not found, using default: $version" "WARN"
        }
        
        # Auto-increment version for release builds if requested
        if ($IsRelease -and $IncrementVersion) {
            # Parse semantic version (major.minor.patch)
            if ($version -match '^(\d+)\.(\d+)\.(\d+)$') {
                $major = [int]$Matches[1]
                $minor = [int]$Matches[2]
                $patch = [int]$Matches[3]
                
                # Increment patch version
                $patch++
                $version = "$major.$minor.$patch"
                
                # Save new version
                Set-Content -Path $versionFile -Value $version -NoNewline
                Write-BuildLog "Server version incremented to: $version" "SUCCESS"
            } else {
                Write-BuildLog "Invalid version format in VERSION file, expected x.y.z" "WARN"
            }
        }
        
        # Get or increment build number (reset on version change)
        $buildNumberFile = Join-Path $ProjectRoot "server\.buildnumber"
        $lastVersionFile = Join-Path $ProjectRoot "server\.lastversion"
        
        # Check if version changed
        $lastVersion = ""
        if (Test-Path $lastVersionFile) {
            $lastVersion = (Get-Content $lastVersionFile -Raw).Trim()
        }
        
        if ($lastVersion -ne $version) {
            # Version changed, reset build number
            $buildNumber = 1
            Write-BuildLog "Version changed from $lastVersion to $version, resetting build number" "INFO"
        } else {
            # Same version, increment build number
            if (Test-Path $buildNumberFile) {
                $buildNumber = [int](Get-Content $buildNumberFile -Raw).Trim()
                $buildNumber++
            } else {
                $buildNumber = 1
            }
        }
        
        # Save build number and version
        Set-Content -Path $buildNumberFile -Value $buildNumber -NoNewline
        Set-Content -Path $lastVersionFile -Value $version -NoNewline
        
        # Create version string
        # Release: x.y.z (clean semantic version)
        # Dev: x.y.z.build-dev (includes build number for tracking)
        if ($IsRelease) {
            $versionString = "$version"
        } else {
            $versionString = "$version.$buildNumber-dev"
        }
        
        # Set versioned log file
        Set-BuildLogFile -Component "server" -Version $version -BuildNumber $buildNumber
        
        # Get build metadata
        $buildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        $gitCommit = (git rev-parse --short HEAD 2>$null) -join ""
        if (-not $gitCommit) { $gitCommit = "unknown" }
        
        # Build ldflags for version injection
        $buildTypeString = if ($IsRelease) { "release" } else { "dev" }
        $ldflags = "-X 'main.Version=$versionString' -X 'main.BuildTime=$buildTime' -X 'main.GitCommit=$gitCommit' -X 'main.BuildType=$buildTypeString'"
        
        # Generate Windows version resource (only on Windows)
        # Must happen AFTER $versionString is created so we can embed build number
        if ($IsWindows -or $env:OS -eq "Windows_NT") {
            Write-BuildLog "Generating Windows version resource..."
            $winverScript = Join-Path $ProjectRoot "tools\generate-winver.ps1"
            if (Test-Path $winverScript) {
                try {
                    & $winverScript -Component "server" -Version $versionString -GitCommit $gitCommit -BuildTime $buildTime 2>&1 | ForEach-Object {
                        Write-BuildLog $_.ToString() "INFO"
                    }
                }
                catch {
                    Write-BuildLog "Warning: Failed to generate version resource: $_" "WARN"
                }
            }
        }
        
        # Build arguments
        $buildArgs = @("build")
        
        if ($IsRelease) {
            # Release build: optimized, stripped, no debug info
            Write-BuildLog "Building optimized release binary..."
            $ldflags += " -s -w"  # Strip debug info and symbol table
            $buildArgs += "-trimpath"  # Remove local file paths for security
        } else {
            # Dev build: keep debug info for troubleshooting
            Write-BuildLog "Building development binary (with debug info)..."
        }
        
        $buildArgs += "-ldflags", $ldflags
        
        # Add verbose flag if requested
        if ($VerboseBuild) {
            $buildArgs += "-v"
        }
        
        # Output file
        $buildArgs += "-o", "printmaster-server.exe"
        $buildArgs += "."
        
        Write-BuildLog "Version: $versionString"
        Write-BuildLog "Command: go $($buildArgs -join ' ')"
        Write-BuildLog "Build Time: $buildTime"
        Write-BuildLog "Git Commit: $gitCommit"
        
        # Execute build with full output capture
        $buildOutput = & go @buildArgs 2>&1
        $buildExitCode = $LASTEXITCODE
        
        # Log all build output
        if ($buildOutput) {
            $buildOutput | ForEach-Object { 
                $line = $_.ToString()
                Write-BuildLog $line
            }
        }
        
        if ($buildExitCode -eq 0) {
            # Get file size
            if (Test-Path "printmaster-server.exe") {
                $fileSize = (Get-Item "printmaster-server.exe").Length
                $fileSizeMB = [math]::Round($fileSize / 1MB, 2)
                Write-BuildLog "Build successful: printmaster-server.exe ($fileSizeMB MB)" "SUCCESS"
                Write-BuildLog "Binary location: $(Join-Path (Get-Location) 'printmaster-server.exe')" "SUCCESS"
            } else {
                Write-BuildLog "Build reported success but executable not found!" "ERROR"
                return $false
            }
            
            return $true
        } else {
            Write-BuildLog "Build failed with exit code $buildExitCode" "ERROR"
            return $false
        }
    }
    finally {
        Pop-Location
    }
}

function Test-Storage {
    Write-BuildLog "Running storage tests..."
    Write-BuildLog "Working directory: $(Get-Location)"
    
    Push-Location (Join-Path $ProjectRoot "agent")
    
    try {
        $testArgs = @("test", "./storage", "-v")
        if ($VerbosePreference -eq 'Continue') {
            $testArgs += "-count=1"  # Disable cache for verbose
        }
        
        Write-BuildLog "Command: go $($testArgs -join ' ')"
        
        $testOutput = & go @testArgs 2>&1
        $testExitCode = $LASTEXITCODE
        
        # Log all output
        $testOutput | ForEach-Object { 
            $line = $_.ToString()
            if ($line -match "FAIL|ERROR") {
                Write-BuildLog $line "ERROR"
            } elseif ($line -match "PASS|ok") {
                Write-BuildLog $line "SUCCESS"
            } else {
                Write-BuildLog $line
            }
        }
        
        if ($testExitCode -eq 0) {
            Write-BuildLog "Storage tests passed" "SUCCESS"
            return $true
        } else {
            Write-BuildLog "Storage tests failed with exit code $testExitCode" "ERROR"
            return $false
        }
    }
    finally {
        Pop-Location
    }
}

function Test-All {
    Write-BuildLog "Running all agent tests..."
    Write-BuildLog "Working directory: $(Get-Location)"
    
    Push-Location (Join-Path $ProjectRoot "agent")
    
    try {
        $testArgs = @("test", "./...", "-v")
        if ($VerbosePreference -eq 'Continue') {
            $testArgs += "-count=1"
        }
        
        Write-BuildLog "Command: go $($testArgs -join ' ')"
        
        $testOutput = & go @testArgs 2>&1
        $testExitCode = $LASTEXITCODE
        
        # Log all output
        $testOutput | ForEach-Object { 
            $line = $_.ToString()
            if ($line -match "FAIL|ERROR") {
                Write-BuildLog $line "ERROR"
            } elseif ($line -match "PASS|ok") {
                Write-BuildLog $line "SUCCESS"
            } else {
                Write-BuildLog $line
            }
        }
        
        if ($testExitCode -eq 0) {
            Write-BuildLog "All tests passed" "SUCCESS"
            return $true
        } else {
            Write-BuildLog "Tests failed with exit code $testExitCode" "ERROR"
            return $false
        }
    }
    finally {
        Pop-Location
    }
}

# Main execution
Write-BuildLog "=== PrintMaster Build Script ===" "INFO"
Write-BuildLog "Target: $Target" "INFO"
Write-BuildLog "Project Root: $ProjectRoot" "INFO"

$success = $false

switch ($Target) {
    'clean' {
        Remove-BuildArtifacts
        $success = $true
    }
    'agent' {
        $success = Build-Agent -IsRelease:$Release -IncrementVersion:$IncrementVersion
    }
    'server' {
        $success = Build-Server -IsRelease:$Release -IncrementVersion:$IncrementVersion
    }
    'both' {
        # Build both agent and server
        $success = Build-Agent -IsRelease:$Release -IncrementVersion:$IncrementVersion
        if ($success) {
            $success = Build-Server -IsRelease:$Release -IncrementVersion:$IncrementVersion
        }
    }
    'test' {
        # Alias for test-storage
        $success = Test-Storage
    }
    'test-storage' {
        $success = Test-Storage
    }
    'test-all' {
        $success = Test-All
    }
    'all' {
        # Build agent and run tests
        $success = Build-Agent -IsRelease:$Release -IncrementVersion:$IncrementVersion
        if ($success) {
            $success = Test-Storage
        }
    }
}

Write-BuildLog "=== Build Complete ===" "INFO"
Write-BuildLog "Log file: $LogFile" "INFO"

if ($success) {
    Write-BuildLog "Result: SUCCESS" "SUCCESS"
    exit 0
} else {
    Write-BuildLog "Result: FAILED" "ERROR"
    exit 1
}
