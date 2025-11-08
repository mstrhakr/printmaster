# Quiet Mode

## Overview

PrintMaster agent and server support **quiet mode** (`-q` / `--quiet`) to suppress informational output during operations. This is particularly useful for:

- **Automated installations/deployments** via scripts or orchestration tools
- **Silent updates** through MSI packages or deployment systems
- **CI/CD pipelines** where verbose output clutters logs
- **Background service installations** without user interaction

## Usage

### Basic Syntax

```bash
# Agent
printmaster-agent.exe --quiet --service install
printmaster-agent.exe -q --service uninstall

# Server
printmaster-server.exe --quiet --service install
printmaster-server.exe -q --service start
```

### Both Flags Work

- `--quiet` - Full flag name (POSIX-style)
- `-q` - Short form (common Unix convention)

Both forms are equivalent and can be used interchangeably.

## What Gets Suppressed

In quiet mode, the following are **NOT** displayed:

- ✗ ASCII art banner
- ✗ Progress bars
- ✗ Spinners
- ✗ "Press Enter to continue" prompts
- ✗ Fancy completion screens

## What Still Shows

These are **ALWAYS** displayed (even in quiet mode):

- ✓ **Errors** - Critical for troubleshooting
- ✓ **Warnings** - Safety-critical messages
- ✓ **Info messages** - Important status updates
- ✓ **Success messages** - Operation confirmations
- ✓ Requested output (like `--version`)

**Format in Quiet Mode**: All output uses standardized log format with timestamps and colorized log levels:
- Format: `timestamp [LEVEL] message`
- Timestamp: ISO 8601 format with timezone (e.g., `2025-11-08T13:45:30-05:00`) - de-emphasized (dim/gray)
- Level colors are **consistent based on severity**:
  - **[INFO]** → Blue - informational messages
  - **[WARN]** → Yellow - warnings and non-critical issues
  - **[ERROR]** → Red - errors and failures
- Message: Plain text, no formatting
- No emojis, clean machine-parseable output

## Examples

### Normal Installation (Verbose)

```powershell
PS> .\printmaster-agent.exe --service install

  ┌────────────────────────────────────────────────────────────────────────────┐
  │                                                                            │
  │   ____________ _____ _   _ ________  ___  ___   _____ _____ ___________    │
  │   | ___ \ ___ \_   _| \ | |_   _|  \/  | / _ \ /  ___|_   _|  ___| ___ \   │
  │   | |_/ / |_/ / | | |  \| | | | | .  . |/ /_\ \\ `--.  | | | |__ | |_/ /   │
  │   |  __/|    /  | | | . ` | | | | |\/| ||  _  | `--. \ | | |  __||    /    │
  │   | |   | |\ \ _| |_| |\  | | | | |  | || | | |/\__/ / | | | |___| |\ \    │
  │   \_|   \_| \_|\___/\_| \_/ \_/ \_|  |_/\_| |_/\____/  \_/ \____/\_| \_|   │
  │                                                                            │
  └────────────────────────────────────────────────────────────────────────────┘

                        Fleet Management Agent
           Version 0.6.4 | Build ca73f7a | 2025-11-08

  • Setting up directories...
  ✓ Directories configured
  • Installing service...
  ✓ Service installed successfully

  ╔══════════════════════════════════════════╗
  ║             Installation Complete        ║
  ║  ✓  PrintMaster Agent Service Installed  ║
  ╚══════════════════════════════════════════╝

  Press Enter to continue...
```

### Quiet Installation (Silent)

```powershell
PS> .\printmaster-agent.exe --service install --quiet
2025-11-08T14:23:45-05:00 [INFO] PrintMaster Agent Service Installed
```

**That's it!** No banner, no progress bars, just a clean log entry with ISO 8601 timestamp.

## Error Handling in Quiet Mode

Errors are **always visible**, ensuring you never miss critical issues:

```powershell
PS> .\printmaster-agent.exe --service install -q
2025-11-08T14:23:45-05:00 [ERROR] Failed to install service: access denied
```

Even in quiet mode, errors use the standard log format with red coloring for visibility.

## Use Cases

### 1. Automated Deployment Script

```powershell
# deploy.ps1
$ErrorActionPreference = "Stop"

Write-Host "Deploying PrintMaster Agent..."
.\printmaster-agent.exe --service install --quiet

if ($LASTEXITCODE -eq 0) {
    Write-Host "Agent installed successfully"
} else {
    Write-Host "Agent installation failed"
    exit 1
}
```

### 2. Silent MSI Installation

```batch
REM install.bat (called by MSI installer)
printmaster-agent.exe --service install --quiet
exit /b %ERRORLEVEL%
```

### 3. Docker Container Initialization

```dockerfile
RUN ./printmaster-server --service run --quiet &
```

### 4. Ansible/Chef/Puppet Automation

```yaml
# Ansible example
- name: Install PrintMaster Agent
  command: /opt/printmaster/printmaster-agent --service install --quiet
  become: true
```

## Implementation Details

### Global Flag

Both agent and server use a global `quietMode` variable in `util/terminal_ui.go`:

```go
var quietMode bool

func SetQuietMode(quiet bool) {
    quietMode = quiet
}
```

### Function-Level Checks

Each `Show*` function checks the flag and outputs log-formatted messages:

```go
func ShowBanner(version, gitCommit, buildDate string) {
    if quietMode {
        return  // Skip banner entirely
    }
    ClearScreen()
    // ... render banner ...
}

func ShowSuccess(message string) {
    if quietMode {
        // In quiet mode, output as a log entry
        timestamp := time.Now().Format("2006-01-02 15:04:05")
        fmt.Printf("%s%s [INFO] %s%s\n", ColorGreen, timestamp, message, ColorReset)
        return
    }
    ClearLine()
    fmt.Printf("  %s✓%s %s\n", ColorGreen, ColorReset, message)
}

func ShowError(message string) {
    // Errors ALWAYS shown (even in quiet mode)
    if quietMode {
        // In quiet mode, output as a log entry
        timestamp := time.Now().Format("2006-01-02 15:04:05")
        fmt.Printf("%s%s [ERROR] %s%s\n", ColorRed, timestamp, message, ColorReset)
        return
    }
    ClearLine()
    fmt.Printf("  %s✗%s %s\n", ColorRed, ColorReset, message)
}

func ShowWarning(message string) {
    // Warnings ALWAYS shown (even in quiet mode)
    if quietMode {
        // In quiet mode, output as a log entry
        timestamp := time.Now().Format("2006-01-02 15:04:05")
        fmt.Printf("%s%s [WARN] %s%s\n", ColorYellow, timestamp, message, ColorReset)
        return
    }
    ClearLine()
    fmt.Printf("  %s⚠%s %s\n", ColorYellow, ColorReset, message)
}
```

### Completion Screen Simplification

In quiet mode, the fancy box is replaced with a log-formatted line:

```go
func ShowCompletionScreen(success bool, message string) {
    if quietMode {
        // In quiet mode, output as a log entry
        timestamp := time.Now().Format("2006-01-02 15:04:05")
        if success {
            fmt.Printf("%s%s [INFO] %s%s\n", ColorGreen, timestamp, message, ColorReset)
        } else {
            fmt.Printf("%s%s [ERROR] %s%s\n", ColorRed, timestamp, message, ColorReset)
        }
        return
    }
    // ... fancy box rendering ...
}
```

## Best Practices

1. **Always use quiet mode in automation** - Reduces log noise
2. **Check exit codes** - Quiet mode suppresses output but preserves exit codes
3. **Capture errors** - Redirect stderr to capture error messages
4. **Log output if needed** - Pipe to log file for audit trail

```powershell
# Good automation pattern
.\printmaster-agent.exe --service install --quiet 2>&1 | Tee-Object -FilePath install.log
```

## Compatibility

- **Platform**: Windows, Linux, macOS (all platforms)
- **Version**: Added in agent v0.6.4.6+ and server v0.6.8.6+
- **Standard**: Follows `-q` / `--quiet` convention from git, docker, npm, apt, etc.

## Related Flags

- `--version` - Show version information (quiet mode doesn't affect this)
- `--service` - Service control command
- `--config` - Specify config file path

## See Also

- [Service Deployment](SERVICE_DEPLOYMENT.md) - Full service installation guide
- [Configuration](CONFIGURATION.md) - Configuration file reference
- [Build Workflow](BUILD_WORKFLOW.md) - Building from source
