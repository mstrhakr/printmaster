# Service Mode - Quick Reference

## Overview

PrintMaster Agent can run as a system service for production deployments. This ensures the agent starts automatically on boot and runs continuously in the background.

## Commands

```bash
# Install service (requires admin/root)
printmaster-agent --service install

# Start service
printmaster-agent --service start

# Stop service
printmaster-agent --service stop

# Uninstall service
printmaster-agent --service uninstall

# Run in foreground (testing)
printmaster-agent --service run

# Interactive mode (default)
printmaster-agent
```

## Platform-Specific Details

### Windows

**Requirements**: Administrator privileges

**Data Directory**: `C:\ProgramData\PrintMaster\`

**Installation**:
```powershell
# Open PowerShell as Administrator
cd C:\Path\To\PrintMaster
.\printmaster-agent.exe --service install
.\printmaster-agent.exe --service start

# Verify service is running
Get-Service PrintMasterAgent

# Check logs
Get-Content "C:\ProgramData\PrintMaster\logs\agent.log" -Tail 50
```

**Access Web UI**: http://localhost:8080 (or https://localhost:8443)

### Linux

**Requirements**: Root privileges

**Data Directories**:
- Config: `/etc/printmaster/`
- Data: `/var/lib/printmaster/`
- Logs: `/var/log/printmaster/`

**Installation**:
```bash
# Install service
sudo ./printmaster-agent --service install

# OR manually with systemd unit file:
sudo cp agent/printmaster-agent.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable printmaster-agent
sudo systemctl start printmaster-agent

# Check status
sudo systemctl status printmaster-agent

# View logs
sudo journalctl -u printmaster-agent -f
```

### macOS

**Requirements**: Root privileges

**Data Directory**: `/Library/Application Support/PrintMaster/`

**Installation**:
```bash
# Install service
sudo ./printmaster-agent --service install

# Start service
sudo launchctl load /Library/LaunchDaemons/com.printmaster.agent.plist

# Stop service
sudo launchctl unload /Library/LaunchDaemons/com.printmaster.agent.plist

# View logs
log show --predicate 'process == "printmaster-agent"' --last 1h
```

## Troubleshooting

### Service won't start

**Windows**:
```powershell
# Check Windows Event Viewer
eventvwr.msc
# Navigate to: Applications and Services Logs > PrintMasterAgent
```

**Linux**:
```bash
# Check service status
sudo systemctl status printmaster-agent

# View recent logs
sudo journalctl -u printmaster-agent -n 100 --no-pager

# Check for permission issues
sudo ls -la /var/lib/printmaster /var/log/printmaster
```

### Cannot access web UI

1. Check if service is running
2. Verify firewall allows port 8080/8443
3. Check settings in agent config database
4. Review logs for port binding errors

### Service fails to install

- **Windows**: Run PowerShell as Administrator
- **Linux/macOS**: Use `sudo` for installation
- Verify binary has execute permissions (`chmod +x printmaster-agent`)

## Configuration

Service uses the same configuration as interactive mode:
- Settings stored in agent database (`agent.db`)
- Web UI accessible at configured ports
- All discovery and proxy features available

## Security

When running as service:
- **Windows**: Runs as Local System (or configure specific service account)
- **Linux**: Runs as dedicated `printmaster` user (create with `useradd -r printmaster`)
- **macOS**: Runs as root (can be configured to run as specific user)

For production deployments:
1. Use dedicated service account with minimal privileges
2. Configure firewall rules appropriately
3. Enable HTTPS for web UI access
4. Review security settings in Web UI > Settings > Security

## See Also

- [Full Service Deployment Guide](../docs/SERVICE_DEPLOYMENT.md)
- [Configuration Guide](../docs/CONFIGURATION.md)
- [Security Architecture](../docs/SECURITY_ARCHITECTURE.md)
