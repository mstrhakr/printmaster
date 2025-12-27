# Service Deployment Guide

## Overview

The PrintMaster agent runs as a native system service on Windows, Linux, and macOS using a single cross-platform binary with OS-appropriate service management.

**Status**: ✅ **IMPLEMENTED** (as of Nov 3, 2025)

## Quick Start

### Installation

**Windows** (requires Administrator PowerShell):
```powershell
.\printmaster-agent.exe --service install
.\printmaster-agent.exe --service start
```

**Linux** (requires root):
```bash
sudo ./printmaster-agent --service install
sudo systemctl start PrintMasterAgent
```

**macOS** (requires root):
```bash
sudo ./printmaster-agent --service install
sudo launchctl load /Library/LaunchDaemons/com.printmaster.agent.plist
```

### Service Commands

```bash
# Install as system service
printmaster-agent --service install

# Install silently (automation/scripts)
printmaster-agent --service install --quiet

# Uninstall service
printmaster-agent --service uninstall

# Start service
printmaster-agent --service start

# Stop service
printmaster-agent --service stop

# Run in foreground (for testing/debugging)
printmaster-agent --service run

# Standard interactive mode (default)
printmaster-agent
```

**Quiet Mode**: Use `--quiet` or `-q` to suppress informational output during service operations. Perfect for automated deployments and scripts. See [Quiet Mode Documentation](QUIET_MODE.md) for details.

## Architecture

### Service Wrapper
- **Library**: `kardianos/service` v1.2.4 (Go, cross-platform)
- **Implementation**: `agent/service.go` + `agent/main.go`
- **Single binary**: Same executable across all platforms
- **Auto-detection**: Detects if running as service or interactively
- **Graceful shutdown**: 30-second timeout for clean shutdown

## Implementation Status

### ✅ Completed Features

- **Service installation/uninstall**: Cross-platform via `--service install/uninstall`
- **Service start/stop**: Platform-native commands via `--service start/stop`
- **Automatic directory setup**: Creates platform-specific data directories
- **Service auto-detection**: Detects if running under service manager
- **Service configuration**: Platform-specific service metadata (name, display name, description)
- **Platform-specific paths**:
  - Windows: `C:\ProgramData\PrintMaster\`
  - Linux: `/var/lib/printmaster/`, `/var/log/printmaster/`
  - macOS: `/Library/Application Support/PrintMaster/`

### 🚧 In Progress

- **Graceful shutdown**: Context-based cancellation for HTTP servers and workers (partially implemented)
- **File logging in service mode**: Currently logs to service manager (Windows Event Log, systemd journal, launchd logs)

### 📋 Planned

- **HTTP server shutdown**: Implement graceful HTTP server shutdown with context
- **Background worker cancellation**: Stop discovery workers on service stop
- **Service-specific log files**: Dedicated log files when running as service (in addition to service manager logs)
- **Installer packages**: MSI for Windows, DEB/RPM for Linux, PKG for macOS

## Platform-Specific Behavior

### Windows (Service Control Manager)

**Service Configuration**
- Name: `PrintMasterAgent`
- Display Name: `PrintMaster Agent`
- Description: "PrintMaster printer and copier fleet management agent. Discovers network printers, collects device metadata, and provides web-based management."
- Startup Type: Automatic (Delayed Start)
- Restart on Failure: Yes (5 second delay)
- Run As: Local System or dedicated service account

**File Paths**
```
C:\ProgramData\PrintMaster\
├── agent.db              # Agent configuration
├── devices.db            # Device database
├── config.toml           # Configuration file (TOML)
└── logs\                 # Log files
    ├── agent.log
    └── ...
```

**Logging**
- File logging to `C:\ProgramData\PrintMaster\logs\`
- Optional Windows Event Log integration
- Log rotation (configurable size/age limits)

**Firewall**
- Inbound rule required for remote access
- Default: localhost-only (127.0.0.1:8080)
- Optional: bind to 0.0.0.0 with firewall rule

**Installation**
- MSI installer or PowerShell script
- Auto-creates directories with correct ACLs
- Registers service and sets startup config
- Creates firewall rule (optional, prompted)

### Linux (systemd)

**Service Configuration**

Example systemd unit file included at `agent/printmaster-agent.service`:

```ini
[Unit]
Description=PrintMaster Agent
Documentation=https://github.com/yourorg/printmaster
After=network.target

[Service]
Type=simple
User=printmaster
Group=printmaster
ExecStart=/usr/local/bin/printmaster-agent --service run
Restart=on-failure
RestartSec=5s
TimeoutStopSec=30s
WorkingDirectory=/var/lib/printmaster
StandardOutput=journal
StandardError=journal
SyslogIdentifier=printmaster-agent

# Security hardening
# Note: NoNewPrivileges and ProtectSystem are NOT set to allow sudo for package manager auto-updates
# The sudoers.d/printmaster-agent file restricts sudo to only specific update commands
PrivateTmp=true
ProtectHome=true

# Optional: raw socket capability for ARP/ping (uncomment if needed)
# AmbientCapabilities=CAP_NET_RAW
# CapabilityBoundingSet=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
```

**Manual Installation** (if not using `--service install`):
```bash
# Copy systemd unit file
sudo cp agent/printmaster-agent.service /etc/systemd/system/

# Reload systemd
sudo systemctl daemon-reload

# Enable and start
sudo systemctl enable printmaster-agent
sudo systemctl start printmaster-agent

# Check status
sudo systemctl status printmaster-agent

# View logs
sudo journalctl -u printmaster-agent -f
```

**File Paths**
```
/etc/printmaster/
└── config.toml           # Configuration (optional override)

/var/lib/printmaster/
├── agent.db
└── devices.db

/var/log/printmaster/
└── agent.log
```

**User/Permissions**
```bash
# Create dedicated user
sudo useradd -r -s /bin/false -d /var/lib/printmaster printmaster

# Set ownership
sudo chown -R printmaster:printmaster /var/lib/printmaster
sudo chown -R printmaster:printmaster /var/log/printmaster
```

**Logging**
- systemd journal integration (stdout/stderr)
- Optional file logging for archival
- View logs: `journalctl -u printmaster-agent -f`

**Installation**
- .deb/.rpm packages with postinst scripts
- Creates user, directories, systemd unit
- Enables and starts service

### macOS (launchd)

**Service Configuration** (`/Library/LaunchDaemons/com.printmaster.agent.plist`)
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.printmaster.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/printmaster-agent</string>
        <string>--service</string>
        <string>run</string>
        <string>--port</string>
        <string>8080</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Library/Logs/PrintMaster/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/Library/Logs/PrintMaster/stderr.log</string>
    <key>WorkingDirectory</key>
    <string>/Library/Application Support/PrintMaster</string>
</dict>
</plist>
```

**File Paths**
```
/Library/Application Support/PrintMaster/
├── agent.db
├── devices.db
└── config.toml

/Library/Logs/PrintMaster/
├── agent.log
├── stdout.log
└── stderr.log
```

**Logging**
- File logging only (launchd captures stdout/stderr)
- Console.app integration for viewing

**Installation**
- .pkg installer with postinstall scripts
- Creates directories with correct ownership
- Loads LaunchDaemon
- `sudo launchctl load /Library/LaunchDaemons/com.printmaster.agent.plist`

## Configuration

### Config File Format (`config.toml`)

> Reference: `agent/config.example.toml`

```toml
asset_id_regex = "\\b\\d{5}\\b"
discovery_concurrency = 50
epson_remote_mode_enabled = false

[snmp]
  community = "public"
  timeout_ms = 2000
  retries = 1

[server]
  enabled = false
  url = "https://printmaster.example.com:9443"
  name = "Warehouse Agent"
  upload_interval_seconds = 300
  heartbeat_interval_seconds = 60

[database]
  path = ""  # default platform path if empty

[logging]
  level = "info"

[web]
  http_port = 8080
  https_port = 8443
  enable_tls = false
  
  [web.auth]
    mode = "local"
    allow_local_admin = true
```

**Web Auth Notes**

- `mode = "server"` routes unauthenticated browsers to the central PrintMaster login and automatically establishes an agent session once the server validates the user. Use this for any deployment that reports into the hub.
- `mode = "local"` keeps the historical behavior but now provides a `/login` page for standalone credentials. Remote users must still reach the host over HTTPS. Set `allow_local_admin = true` to preserve the loopback bypass for on-box recovery.
- `mode = "disabled"` leaves the UI wide open and should only be used temporarily while debugging in isolated networks.

The embedded login experience lives at `/login` and is backed by `POST /api/v1/auth/login`, `/api/v1/auth/logout`, `/api/v1/auth/me`, and `/api/v1/auth/options`. These endpoints remain accessible without authentication so the login screen can bootstrap itself, while all other routes are now protected by the new middleware.

### Environment Variable Overrides
```powershell
# Preferred precedence: AGENT_CONFIG > AGENT_CONFIG_PATH > CONFIG > CONFIG_PATH
$env:AGENT_CONFIG = 'C:\\ProgramData\\PrintMaster\\config.toml'
$env:AGENT_DB_PATH = 'C:\\ProgramData\\PrintMaster\\agent.db'
$env:AGENT_LOG_LEVEL = 'debug'
```

## Implementation Details

### Service Program Structure
```go
import "github.com/kardianos/service"

type program struct {
    ctx    context.Context
    cancel context.CancelFunc
    // existing fields: deviceStore, agentConfigStore, etc.
}

func (p *program) Start(s service.Service) error {
    // Create root context for graceful shutdown
    p.ctx, p.cancel = context.WithCancel(context.Background())
    
    // Initialize stores, start HTTP server, launch workers
    go p.run()
    return nil
}

func (p *program) Stop(s service.Service) error {
    // Signal cancellation
    p.cancel()
    
    // Wait for goroutines with timeout
    // Flush logs, close DB connections
    return nil
}

func (p *program) run() {
    // Existing main() logic but using p.ctx for cancellation
}
```

### Graceful Shutdown
- All long-running goroutines must respect `ctx.Done()`
- HTTP server shutdown with timeout (5-10 seconds)
- Database flush and close
- Log rotation and final flush
- Worker pool drain

## Security Considerations

### Network Binding
- **Default**: localhost-only (127.0.0.1)
- **Remote access**: requires explicit config + firewall rules
- **TLS**: optional, configured via cert/key paths

### Privileges
- **Windows**: Run as Local System or dedicated account
- **Linux**: Run as unprivileged user; add capabilities only if needed
- **macOS**: Run as daemon (no user session)

### SNMP Credentials
- Store in config file with restrictive permissions
- Never log community strings or credentials
- Consider encrypted config file or keychain integration

### File Permissions
- Config files: 0600 (owner read/write only)
- Database: 0644 (owner write, others read)
- Logs: 0644 or 0600 depending on sensitivity
- Directories: 0755

## Deployment Workflows

### Windows Deployment
```powershell
# 1. Download/extract binary
Expand-Archive printmaster-agent-windows-amd64.zip C:\Program Files\PrintMaster

# 2. Install service
cd "C:\Program Files\PrintMaster"
.\printmaster-agent.exe --service install --port 8080

# 3. Configure firewall (if remote access needed)
New-NetFirewallRule -DisplayName "PrintMaster Agent" `
    -Direction Inbound -LocalPort 8080 -Protocol TCP -Action Allow

# 4. Start service
.\printmaster-agent.exe --service start

# Or use Services.msc / sc.exe
sc start PrintMasterAgent
```

### Linux Deployment (.deb)
```bash
# 1. Install package
sudo dpkg -i printmaster-agent_1.0.0_amd64.deb

# 2. Edit config if needed
sudo nano /etc/printmaster/config.json

# 3. Start and enable service
sudo systemctl start printmaster-agent
sudo systemctl enable printmaster-agent

# 4. Check status
sudo systemctl status printmaster-agent
journalctl -u printmaster-agent -f
```

### macOS Deployment
```bash
# 1. Install package
sudo installer -pkg printmaster-agent-1.0.0.pkg -target /

# 2. Edit config if needed
sudo nano "/Library/Application Support/PrintMaster/config.json"

# 3. Load service
sudo launchctl load /Library/LaunchDaemons/com.printmaster.agent.plist

# 4. Check status
sudo launchctl list | grep printmaster
tail -f "/Library/Logs/PrintMaster/agent.log"
```

## Updates and Maintenance

### In-Place Update
```bash
# 1. Stop service
printmaster-agent --service stop   # or systemctl stop / launchctl unload

# 2. Replace binary
sudo cp printmaster-agent-new /usr/local/bin/printmaster-agent

# 3. Verify config compatibility (check for breaking changes)
printmaster-agent --version
printmaster-agent --config-check

# 4. Start service
printmaster-agent --service start
```

### Database Migration
- Automatic schema migrations run on service start
- Backup database before major version upgrades
- Migration errors logged; service exits non-zero

### Log Rotation
- Built-in rotation (configurable via config.json)
- Or use OS tools: logrotate (Linux), Event Log rollover (Windows)

## Monitoring and Health Checks

### Health Endpoint
```
GET /healthz
Response: {"status": "ok", "uptime_seconds": 12345}
```

### Metrics Endpoint (optional)
```
GET /metrics
Response: Prometheus-format metrics
```

### Service Status
- Windows: `sc query PrintMasterAgent` or Services.msc
- Linux: `systemctl status printmaster-agent`
- macOS: `launchctl list com.printmaster.agent`

## Troubleshooting

### Service Won't Start
- Check logs for port binding errors, DB path permissions
- Verify config file syntax
- Ensure dependencies installed (e.g., SNMP libraries)

### Can't Access Web UI
- Verify service is running and listening on expected port
- Check firewall rules
- Confirm bind_address in config (localhost vs 0.0.0.0)

### Discovery Not Working
- Check SNMP community/credentials
- Verify network routing and firewall (UDP 161)
- Review discovery settings in config

### High Memory/CPU Usage
- Check metrics collection interval (too aggressive?)
- Review discovery worker count
- Inspect logs for infinite loops or errors

## Future Enhancements

- [ ] Self-update mechanism (download, verify, replace binary)
- [ ] Clustered deployment (multiple agents, central server)
- [ ] TLS certificate auto-renewal (Let's Encrypt integration)
- [ ] Encrypted configuration (OS keychain integration)
- [ ] Multi-tenant support (separate DB per tenant)
- [ ] Advanced monitoring (StatsD, OpenTelemetry export)
- [ ] Web UI proxy (reverse proxy printer web interfaces)

## References

- kardianos/service: https://github.com/kardianos/service
- systemd best practices: https://www.freedesktop.org/software/systemd/man/systemd.service.html
- Windows Services: https://learn.microsoft.com/en-us/windows/win32/services/services
- macOS launchd: https://www.launchd.info/
