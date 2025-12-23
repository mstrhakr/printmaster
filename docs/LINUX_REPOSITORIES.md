# Linux Package Repositories

PrintMaster provides official package repositories for easy installation and automatic updates on Linux systems.

## Supported Distributions

| Distribution | Package Format | Repository Type |
|-------------|----------------|-----------------|
| Debian 11+ | .deb | APT |
| Ubuntu 20.04+ | .deb | APT |
| Linux Mint | .deb | APT |
| Fedora 38+ | .rpm | DNF |
| RHEL 8+ | .rpm | DNF/YUM |
| CentOS Stream 8+ | .rpm | DNF/YUM |
| Rocky Linux 8+ | .rpm | DNF/YUM |
| AlmaLinux 8+ | .rpm | DNF/YUM |

## Quick Install

### Debian/Ubuntu (APT)

```bash
# Add repository and install (one command)
echo "deb [trusted=yes] https://mstrhakr.github.io/printmaster stable main" | sudo tee /etc/apt/sources.list.d/printmaster.list && sudo apt-get update && sudo apt-get install -y printmaster-agent
```

Or step by step:

```bash
# 1. Add the repository
echo "deb [trusted=yes] https://mstrhakr.github.io/printmaster stable main" | sudo tee /etc/apt/sources.list.d/printmaster.list

# 2. Update and install
sudo apt-get update
sudo apt-get install printmaster-agent
```

### Fedora/RHEL (DNF)

```bash
# Add repository and install (one command)
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo && sudo dnf install -y printmaster-agent
```

Or step by step:

```bash
# 1. Add the repository
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo

# 2. Install
sudo dnf install printmaster-agent
```

### Older RHEL/CentOS (YUM)

```bash
# Download repo file
sudo curl -o /etc/yum.repos.d/printmaster.repo https://mstrhakr.github.io/printmaster/printmaster.repo

# Install
sudo yum install printmaster-agent
```

---

## Secure Installation (GPG Verification)

For production environments, we recommend verifying package signatures.

### APT with GPG

```bash
# 1. Import the GPG key
curl -fsSL https://mstrhakr.github.io/printmaster/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/printmaster.gpg

# 2. Add repository with signature verification
echo "deb [signed-by=/usr/share/keyrings/printmaster.gpg] https://mstrhakr.github.io/printmaster stable main" | sudo tee /etc/apt/sources.list.d/printmaster.list

# 3. Update and install
sudo apt-get update
sudo apt-get install printmaster-agent
```

### DNF with GPG

```bash
# 1. Import the GPG key
sudo rpm --import https://mstrhakr.github.io/printmaster/gpg.key

# 2. Add repository and install (repo file has gpgcheck=1)
sudo dnf config-manager addrepo --from-repofile=https://mstrhakr.github.io/printmaster/printmaster.repo
sudo dnf install printmaster-agent
```

---

## After Installation

The package automatically:
- Creates a `printmaster` system user
- Installs the systemd service
- Enables and starts the service

### Verify Installation

```bash
# Check service status
sudo systemctl status printmaster-agent

# View logs
journalctl -u printmaster-agent -f

# Test web UI
curl -s http://localhost:8080/api/v1/status | jq
```

### Important Paths

| Path | Description |
|------|-------------|
| `/usr/bin/printmaster-agent` | Agent binary |
| `/etc/printmaster/agent.toml` | Configuration file |
| `/var/lib/printmaster` | Data directory (SQLite DB) |
| `/var/log/printmaster` | Log files |

### Web UI

Access the agent web interface at: **http://localhost:8080**

If connecting from another machine, use the server's IP address.

---

## Managing the Service

```bash
# Start/stop/restart
sudo systemctl start printmaster-agent
sudo systemctl stop printmaster-agent
sudo systemctl restart printmaster-agent

# Enable/disable auto-start
sudo systemctl enable printmaster-agent
sudo systemctl disable printmaster-agent

# View logs
journalctl -u printmaster-agent          # All logs
journalctl -u printmaster-agent -f       # Follow logs
journalctl -u printmaster-agent --since today
```

---

## Updating

Updates are handled automatically by your package manager.

### APT (Debian/Ubuntu)

```bash
sudo apt-get update
sudo apt-get upgrade printmaster-agent
```

### DNF (Fedora/RHEL)

```bash
sudo dnf upgrade printmaster-agent
```

### Automatic Updates

The agent supports automatic self-updates when configured. Updates are applied using your system's package manager (apt-get or dnf), ensuring proper dependency handling and service restarts.

See [Configuration Guide](CONFIGURATION.md) for auto-update settings.

---

## Uninstalling

### APT (Debian/Ubuntu)

```bash
# Remove package (keeps config files)
sudo apt-get remove printmaster-agent

# Remove package and all config/data
sudo apt-get purge printmaster-agent
sudo rm -rf /var/lib/printmaster /var/log/printmaster

# Remove repository
sudo rm /etc/apt/sources.list.d/printmaster.list
```

### DNF (Fedora/RHEL)

```bash
# Remove package
sudo dnf remove printmaster-agent

# Remove all data
sudo rm -rf /var/lib/printmaster /var/log/printmaster /etc/printmaster

# Remove repository
sudo rm /etc/yum.repos.d/printmaster.repo
```

---

## Troubleshooting

### Service won't start

```bash
# Check for errors
journalctl -u printmaster-agent -e

# Verify binary exists
ls -la /usr/bin/printmaster-agent

# Check permissions
ls -la /var/lib/printmaster
```

### Port already in use

```bash
# Check what's using port 8080
sudo ss -tlnp | grep 8080

# Change port in config
sudo nano /etc/printmaster/agent.toml
# Set: http_port = 8001
sudo systemctl restart printmaster-agent
```

### Package not found

```bash
# APT: Verify repository is added
cat /etc/apt/sources.list.d/printmaster.list
sudo apt-get update

# DNF: Verify repository is added
dnf repolist | grep printmaster
```

### GPG errors

```bash
# APT: Re-import key
curl -fsSL https://mstrhakr.github.io/printmaster/gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/printmaster.gpg

# DNF: Re-import key
sudo rpm --import https://mstrhakr.github.io/printmaster/gpg.key
```

---

## Repository URLs

| Resource | URL |
|----------|-----|
| APT Repository | `https://mstrhakr.github.io/printmaster` |
| DNF Repository | `https://mstrhakr.github.io/printmaster/rpm` |
| DNF .repo file | `https://mstrhakr.github.io/printmaster/printmaster.repo` |
| GPG Public Key | `https://mstrhakr.github.io/printmaster/gpg.key` |
| GitHub Releases | `https://github.com/mstrhakr/printmaster/releases` |

---

## See Also

- [APT Repository Details](APT_REPOSITORY.md) - Detailed APT setup for maintainers
- [DNF Repository Details](DNF_REPOSITORY.md) - Detailed DNF setup for maintainers
- [Configuration Guide](CONFIGURATION.md) - All configuration options
- [Docker Deployment](DOCKER_DEPLOYMENT.md) - Container-based installation
