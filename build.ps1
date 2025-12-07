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

function Invoke-JSUnitTests {
    Write-BuildLog "Running JavaScript unit tests (jest)..." "INFO"

    if ($env:PRINTMASTER_SKIP_JS_TESTS -eq '1') {
        Write-BuildLog "Skipping JS unit tests because PRINTMASTER_SKIP_JS_TESTS=1" "WARN"
        return $true
    }

    Push-Location $ProjectRoot
    try {
        $cmd = "npm run test:js"
        Write-BuildLog "Executing: $cmd" "INFO"
        $testOutput = & npm run test:js 2>&1
        $testExit = $LASTEXITCODE
        if ($testExit -ne 0) {
            $testOutput | ForEach-Object { Write-BuildLog $_ "ERROR" }
            Write-BuildLog "JS unit tests failed" "ERROR"
            return $false
        }
        $testOutput | ForEach-Object { Write-BuildLog $_ "INFO" }
        Write-BuildLog "JS unit tests passed" "INFO"
        return $true
    }
    finally { Pop-Location }
}

function Invoke-PlaywrightTests {
    Write-BuildLog "Running Playwright smoke tests..." "INFO"

    if ($env:PRINTMASTER_SKIP_PLAYWRIGHT -eq '1') {
        Write-BuildLog "Skipping Playwright tests because PRINTMASTER_SKIP_PLAYWRIGHT=1" "WARN"
        return $true
    }

    Push-Location $ProjectRoot
    try {
        # Ensure playwright browsers are installed when running CI locally
        Write-BuildLog "Ensuring Playwright browsers are installed (npx playwright install)" "INFO"
        & npx playwright install > $null 2>&1

        $cmd = "npm run test:playwright"
        Write-BuildLog "Executing: $cmd" "INFO"
        $testOutput = & npm run test:playwright 2>&1
        $testExit = $LASTEXITCODE
        if ($testExit -ne 0) {
            $testOutput | ForEach-Object { Write-BuildLog $_ "ERROR" }
            Write-BuildLog "Playwright smoke tests failed" "ERROR"
            return $false
        }
        $testOutput | ForEach-Object { Write-BuildLog $_ "INFO" }
        Write-BuildLog "Playwright smoke tests passed" "INFO"
        return $true
    }
    finally { Pop-Location }
}

# ANSI color codes
$ColorReset = "`e[0m"
$ColorDim = "`e[2m"
$ColorRed = "`e[31m"
$ColorGreen = "`e[32m"
$ColorYellow = "`e[33m"
$ColorBlue = "`e[34m"

function Write-BuildLog {
    param([string]$Message, [string]$Level = "INFO")
    
    # Use default log if $LogFile not set yet
    if (-not $script:LogFile) {
        $script:LogFile = Join-Path $LogDir "build.log"
    }
    
    # ISO 8601 timestamp format (industry standard)
    $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
    
    # Determine color based on level
    $levelColor = switch ($Level) {
        "ERROR"   { $ColorRed }
        "WARN"    { $ColorYellow }
        default   { $ColorBlue }  # INFO uses blue
    }
    
    # Format: dim-timestamp colored-[LEVEL] message
    $consoleMessage = "${ColorDim}${timestamp}${ColorReset} ${levelColor}[${Level}]${ColorReset} ${Message}"
    $logMessage = "[$timestamp] [$Level] $Message"
    
    # Write to console
    Write-Host $consoleMessage
    
    # Append to log file (plain text)
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
    
    Write-BuildLog "Clean complete" "INFO"
}

function Test-Prerequisites {
    Write-BuildLog "Checking build prerequisites..." "INFO"
    
    # Check Go installation
    $goVersion = & go version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-BuildLog "Go is not installed or not in PATH" "ERROR"
        Write-BuildLog "Install Go from: https://go.dev/dl/" "ERROR"
        return $false
    }
    Write-BuildLog "Found: $goVersion" "INFO"
    
    # Check for staticcheck
    $staticcheckVersion = & staticcheck -version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-BuildLog "staticcheck not found - installing..." "WARN"
        Write-BuildLog "Running: go install honnef.co/go/tools/cmd/staticcheck@latest" "INFO"
        & go install honnef.co/go/tools/cmd/staticcheck@latest
        if ($LASTEXITCODE -ne 0) {
            Write-BuildLog "Failed to install staticcheck" "ERROR"
            return $false
        }
        Write-BuildLog "staticcheck installed successfully" "INFO"
    } else {
        Write-BuildLog "Found staticcheck: $staticcheckVersion" "INFO"
    }
    
    # Check git (for version injection)
    $gitVersion = & git --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-BuildLog "Git not found - version info will be limited" "WARN"
    } else {
        Write-BuildLog "Found: $gitVersion" "INFO"
    }
    
    return $true
}

function Invoke-Linters {
    param(
        [Parameter(Mandatory=$true)]
        [string]$Component
    )
    
    Write-BuildLog "Running linters for $Component..." "INFO"
    
    $componentDir = Join-Path $ProjectRoot $Component
    Push-Location $componentDir
    
    try {
        # Run go vet (capture output so we can log it in full)
        Write-BuildLog "Running go vet..." "INFO"
        $vetOutput = & go vet ./... 2>&1
        $vetExit = $LASTEXITCODE
        if ($vetOutput) {
            # Write each line of vet output into the build log (INFO level)
            $vetOutput | ForEach-Object { Write-BuildLog $_ "INFO" }
        }
        if ($vetExit -ne 0) {
            $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
            Write-BuildLog "$Component failed go vet checks (see above output)" "ERROR"
            Add-Content -Path $script:LogFile -Value "[$timestamp] [ERROR] FAIL: $Component failed go vet checks"
            return $false
        }
        # Colorize PASS
        $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
        Write-BuildLog "$Component passed go vet checks" "INFO"
        Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] PASS: $Component passed go vet checks"

        # Run staticcheck (capture output so we can log it in full)
        Write-BuildLog "Running staticcheck..." "INFO"
        $scOutput = & staticcheck ./... 2>&1
        $scExit = $LASTEXITCODE
        if ($scOutput) {
            # Write each line of staticcheck output into the build log (INFO level)
            $scOutput | ForEach-Object { Write-BuildLog $_ "INFO" }
        }
        if ($scExit -ne 0) {
            $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
            Write-BuildLog "$Component failed staticcheck (see above output)" "ERROR"
            Add-Content -Path $script:LogFile -Value "[$timestamp] [ERROR] FAIL: $Component failed staticcheck"
            return $false
        }
        # Colorize PASS
        $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
        Write-BuildLog "$Component passed staticcheck" "INFO"
        Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] PASS: $Component passed staticcheck"

        return $true
    }
    finally {
        Pop-Location
    }
}

function Invoke-Tests {
    param(
        [Parameter(Mandatory=$true)]
        [string]$Component
    )
    
    Write-BuildLog "Running tests for $Component..." "INFO"

    # Support skipping tests via environment variable for update/CI convenience.
    if ($env:PRINTMASTER_SKIP_TESTS -eq '1') {
        Write-BuildLog "Skipping tests for $Component because PRINTMASTER_SKIP_TESTS=1" "WARN"
        Add-Content -Path $script:LogFile -Value "[$(Get-Date -Format 'yyyy-MM-ddTHH:mm:sszzz')] [WARN] Skipping tests for $Component due to PRINTMASTER_SKIP_TESTS=1"
        return $true
    }
    
    $componentDir = Join-Path $ProjectRoot $Component
    Push-Location $componentDir
    
    try {
        # Run tests with verbose output
        $testOutput = & go test ./... -v 2>&1
        $testExitCode = $LASTEXITCODE
        
        if ($testExitCode -ne 0) {
            # Show test output on failure
            Write-Host ""
            $testOutput | ForEach-Object { Write-Host $_ -ForegroundColor Red }
            Write-Host ""
            # Colorize FAIL
            $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
            Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorRed}[ERROR]${ColorReset} ${ColorRed}FAIL:${ColorReset} $Component failed tests"
            Add-Content -Path $script:LogFile -Value "[$timestamp] [ERROR] FAIL: $Component failed tests"
            return $false
        }
        
        # Colorize PASS
        $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
        Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorBlue}[INFO]${ColorReset} ${ColorGreen}PASS:${ColorReset} $Component passed tests"
        Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] PASS: $Component passed tests"
        
        return $true
    }
    finally {
        Pop-Location
    }
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
                Write-BuildLog "Version incremented to: $version" "INFO"
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
            # Append branch suffix (e.g. -feature-x) when building non-main branches
            if ($script:BranchSuffix) { $versionString = "$versionString$script:BranchSuffix" }
        }
        
        # Set versioned log file
        Set-BuildLogFile -Component "agent" -Version $version -BuildNumber $buildNumber

        # Run JavaScript tests (unit + playwright smoke) before compiling Go binary
        if (-not (Invoke-JSUnitTests)) {
            Write-BuildLog "Aborting agent build due to JS unit test failures" "ERROR"
            return $false
        }
        if (-not (Invoke-PlaywrightTests)) {
            Write-BuildLog "Aborting agent build due to Playwright smoke test failures" "ERROR"
            return $false
        }
        
        # Get build metadata
        $buildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        $gitCommit = (git rev-parse --short HEAD 2>$null) -join ""
        if (-not $gitCommit) { $gitCommit = "unknown" }
        
        # Build ldflags for version injection
        $buildTypeString = if ($IsRelease) { "release" } else { "dev" }
    $ldflags = "-X 'main.Version=$versionString' -X 'main.BuildTime=$buildTime' -X 'main.GitCommit=$gitCommit' -X 'main.BuildType=$buildTypeString' -X 'main.GitBranch=$script:GitBranch'"
        
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
        
        # Execute build with CGO_ENABLED=0 (pure Go build for consistent cross-platform support)
        # This is required because we use pure-Go dependencies: modernc.org/sqlite and gosnmp
        $env:CGO_ENABLED = 0
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
                $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
                Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorBlue}[INFO]${ColorReset} ${ColorGreen}SUCCESS:${ColorReset} printmaster-agent.exe ($fileSizeMB MB)"
                Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] SUCCESS: printmaster-agent.exe ($fileSizeMB MB)"
                Write-BuildLog "Binary location: $(Join-Path (Get-Location) 'printmaster-agent.exe')" "INFO"
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
                Write-BuildLog "Server version incremented to: $version" "INFO"
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
            # Append branch suffix (e.g. -feature-x) when building non-main branches
            if ($script:BranchSuffix) { $versionString = "$versionString$script:BranchSuffix" }
        }
        
        # Set versioned log file
        Set-BuildLogFile -Component "server" -Version $version -BuildNumber $buildNumber

        # Run JavaScript tests (unit + playwright smoke) before compiling Go binary for server
        if (-not (Invoke-JSUnitTests)) {
            Write-BuildLog "Aborting server build due to JS unit test failures" "ERROR"
            return $false
        }
        if (-not (Invoke-PlaywrightTests)) {
            Write-BuildLog "Aborting server build due to Playwright smoke test failures" "ERROR"
            return $false
        }
        
        # Get build metadata
        $buildTime = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        $gitCommit = (git rev-parse --short HEAD 2>$null) -join ""
        if (-not $gitCommit) { $gitCommit = "unknown" }
        
        # Build ldflags for version injection
        $buildTypeString = if ($IsRelease) { "release" } else { "dev" }
    $ldflags = "-X 'main.Version=$versionString' -X 'main.BuildTime=$buildTime' -X 'main.GitCommit=$gitCommit' -X 'main.BuildType=$buildTypeString' -X 'main.GitBranch=$script:GitBranch'"
        
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
                $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
                Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorBlue}[INFO]${ColorReset} ${ColorGreen}SUCCESS:${ColorReset} printmaster-server.exe ($fileSizeMB MB)"
                Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] SUCCESS: printmaster-server.exe ($fileSizeMB MB)"
                Write-BuildLog "Binary location: $(Join-Path (Get-Location) 'printmaster-server.exe')" "INFO"
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
                Write-BuildLog $line "INFO"
            } else {
                Write-BuildLog $line
            }
        }
        
        if ($testExitCode -eq 0) {
            Write-BuildLog "Storage tests passed" "INFO"
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
                Write-BuildLog $line "INFO"
            } else {
                Write-BuildLog $line
            }
        }
        
        if ($testExitCode -eq 0) {
            Write-BuildLog "All tests passed" "INFO"
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

# Check prerequisites (Go, staticcheck, git)
if (-not (Test-Prerequisites)) {
    Write-BuildLog "Prerequisites check failed - cannot continue" "ERROR"
    exit 1
}

# Determine current git branch and create a branch suffix for dev builds when not main/master
try {
    $gitBranchRaw = (git rev-parse --abbrev-ref HEAD 2>$null) -join ""
} catch {
    $gitBranchRaw = ""
}
if (-not $gitBranchRaw) { $gitBranchRaw = "unknown" }

# Sanitize branch for use in version strings: allow alphanumerics, dot, underscore and dash
$gitBranchSanitized = $gitBranchRaw -replace '[^A-Za-z0-9_.-]', '-'

# If branch is a mainline branch, don't add suffix; otherwise create a suffix like -feature-x
if ($gitBranchSanitized -in @('main','master','trunk')) {
    $script:BranchSuffix = ''
} else {
    $script:BranchSuffix = "-" + $gitBranchSanitized
}

# Expose GitBranch to ldflags as well
$script:GitBranch = $gitBranchSanitized

$success = $false

switch ($Target) {
    'clean' {
        Remove-BuildArtifacts
        $success = $true
    }
    'agent' {
        # Run linters for common (shared dependency) first
        if (-not (Invoke-Linters -Component 'common')) {
            Write-BuildLog "Linter checks failed for common" "ERROR"
            exit 1
        }
        # Run tests for common
        if (-not (Invoke-Tests -Component 'common')) {
            Write-BuildLog "Tests failed for common" "ERROR"
            exit 1
        }
        # Run linters before build
        if (-not (Invoke-Linters -Component 'agent')) {
            Write-BuildLog "Linter checks failed for agent" "ERROR"
            exit 1
        }
        # Run tests before build
        if (-not (Invoke-Tests -Component 'agent')) {
            Write-BuildLog "Tests failed for agent" "ERROR"
            exit 1
        }
        $success = Build-Agent -IsRelease:$Release -IncrementVersion:$IncrementVersion
    }
    'server' {
        # Run linters for common (shared dependency) first
        if (-not (Invoke-Linters -Component 'common')) {
            Write-BuildLog "Linter checks failed for common" "ERROR"
            exit 1
        }
        # Run tests for common
        if (-not (Invoke-Tests -Component 'common')) {
            Write-BuildLog "Tests failed for common" "ERROR"
            exit 1
        }
        # Run linters before build
        if (-not (Invoke-Linters -Component 'server')) {
            Write-BuildLog "Linter checks failed for server" "ERROR"
            exit 1
        }
        # Run tests before build
        if (-not (Invoke-Tests -Component 'server')) {
            Write-BuildLog "Tests failed for server" "ERROR"
            exit 1
        }
        $success = Build-Server -IsRelease:$Release -IncrementVersion:$IncrementVersion
    }
    'both' {
        # Run linters for common (shared dependency) first
        if (-not (Invoke-Linters -Component 'common')) {
            Write-BuildLog "Linter checks failed for common" "ERROR"
            exit 1
        }
        # Run tests for common
        if (-not (Invoke-Tests -Component 'common')) {
            Write-BuildLog "Tests failed for common" "ERROR"
            exit 1
        }
        # Run linters for both components
        if (-not (Invoke-Linters -Component 'agent')) {
            Write-BuildLog "Linter checks failed for agent" "ERROR"
            exit 1
        }
        if (-not (Invoke-Linters -Component 'server')) {
            Write-BuildLog "Linter checks failed for server" "ERROR"
            exit 1
        }
        # Run tests for both components
        if (-not (Invoke-Tests -Component 'agent')) {
            Write-BuildLog "Tests failed for agent" "ERROR"
            exit 1
        }
        if (-not (Invoke-Tests -Component 'server')) {
            Write-BuildLog "Tests failed for server" "ERROR"
            exit 1
        }
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
    $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
    Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorBlue}[INFO]${ColorReset} ${ColorGreen}SUCCESS:${ColorReset} Build script completed with exit code 0"
    Add-Content -Path $script:LogFile -Value "[$timestamp] [INFO] SUCCESS: Build script completed with exit code 0"
    exit 0
} else {
    $timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:sszzz"
    Write-Host "${ColorDim}${timestamp}${ColorReset} ${ColorRed}[ERROR]${ColorReset} ${ColorRed}FAIL:${ColorReset} Build script failed with exit code 1"
    Add-Content -Path $script:LogFile -Value "[$timestamp] [ERROR] FAIL: Build script failed with exit code 1"
    exit 1
}
