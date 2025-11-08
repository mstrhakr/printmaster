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
- ✗ Success messages (`ShowSuccess`)
- ✗ Informational messages (`ShowInfo`)
- ✗ "Press Enter to continue" prompts
- ✗ Fancy completion screens

## What Still Shows

These are **ALWAYS** displayed (even in quiet mode):

- ✓ **Errors** (`ShowError`) - Critical for troubleshooting
- ✓ **Warnings** (`ShowWarning`) - Safety-critical messages
- ✓ Completion status (simplified format: `✓ Message` or `✗ Message`)
- ✓ Requested output (like `--version`)

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
✓ PrintMaster Agent Service Installed
```

**That's it!** No banner, no progress bars, just the result.

## Error Handling in Quiet Mode

Errors are **always visible**, ensuring you never miss critical issues:

```powershell
PS> .\printmaster-agent.exe --service install -q
  ✗ Failed to install service: access denied
```

Even in quiet mode, errors use the error formatting (red ✗) to ensure visibility.

## Use Cases

### 1. Automated Deployment Script

```powershell
# deploy.ps1
$ErrorActionPreference = "Stop"

Write-Host "Deploying PrintMaster Agent..."
.\printmaster-agent.exe --service install --quiet

if ($LASTEXITCODE -eq 0) {
    Write-Host "✓ Agent installed successfully"
} else {
    Write-Host "✗ Agent installation failed"
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

Each `Show*` function checks the flag:

```go
func ShowBanner(version, gitCommit, buildDate string) {
    if quietMode {
        return  // Skip banner entirely
    }
    ClearScreen()
    // ... render banner ...
}

func ShowError(message string) {
    // Errors ALWAYS shown (no quietMode check)
    ClearLine()
    fmt.Printf("  %s✗%s %s\n", ColorRed, ColorReset, message)
}
```

### Completion Screen Simplification

In quiet mode, the fancy box is replaced with a simple line:

```go
func ShowCompletionScreen(success bool, message string) {
    if quietMode {
        if success {
            fmt.Printf("✓ %s\n", message)
        } else {
            fmt.Printf("✗ %s\n", message)
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
