# Environment Variables Reference

This document provides a comprehensive reference for all environment variables supported by PrintMaster components.

## Configuration Hierarchy

Environment variables override TOML configuration file values. The complete precedence order is:

1. **Environment variables** (highest priority)
2. **TOML config file** (`config.toml`)
3. **Built-in defaults** (lowest priority)

For database-stored settings (UI), see [CONFIGURATION.md](CONFIGURATION.md).

---

## Common Variables (Agent & Server)

These variables work with component prefixes or as generic fallbacks.

### Configuration Path

| Variable | Description | Example |
|----------|-------------|---------|
| `<COMPONENT>_CONFIG` | Component-specific config path (e.g., `AGENT_CONFIG`, `SERVER_CONFIG`) | `/etc/printmaster/agent.toml` |
| `<COMPONENT>_CONFIG_PATH` | Alternative component-specific config path | `/etc/printmaster/server.toml` |
| `CONFIG` | Generic config path fallback | `./config.toml` |
| `CONFIG_PATH` | Alternative generic config path | `./config.toml` |

**Precedence:** `<COMPONENT>_CONFIG` → `<COMPONENT>_CONFIG_PATH` → `CONFIG` → `CONFIG_PATH` → CLI flag

### Database Path

| Variable | Description | Example |
|----------|-------------|---------|
| `AGENT_DB_PATH` | Agent database file path | `/var/lib/printmaster/agent/agent.db` |
| `SERVER_DB_PATH` | Server database file path | `/var/lib/printmaster/server/server.db` |
| `DB_PATH` | Generic database path fallback | `./data/printmaster.db` |

**Precedence:** `<COMPONENT>_DB_PATH` → `DB_PATH`

### Logging

| Variable | Description | Values | Default |
|----------|-------------|--------|---------|
| `LOG_LEVEL` | Logging verbosity | `debug`, `info`, `warn`, `error` | `info` |
| `AGENT_LOG_LEVEL` | Agent-specific log level (overrides `LOG_LEVEL`) | Same as above | — |
| `SERVER_LOG_LEVEL` | Server-specific log level (overrides `LOG_LEVEL`) | Same as above | — |

---

## Agent-Specific Variables

### Discovery & Scanning

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `ASSET_ID_REGEX` | Regex pattern for extracting asset IDs from device data | String | — |
| `DISCOVERY_CONCURRENCY` | Number of concurrent discovery workers | Integer | `100` |

### SNMP Configuration

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `SNMP_COMMUNITY` | SNMP community string for queries | String | `public` |
| `SNMP_VERSION` | SNMP protocol version | `1`, `2c`, `3` | `1` |
| `SNMP_TIMEOUT_MS` | SNMP query timeout in milliseconds | Integer | `3000` |
| `SNMP_RETRIES` | Number of SNMP query retries | Integer | `2` |

### Server Connection

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `SERVER_ENABLED` | Enable agent-to-server communication | `true`, `1`, `false`, `0` | `false` |
| `SERVER_URL` | Central server URL | URL | — |
| `AGENT_NAME` | Display name for this agent | String | Hostname |
| `AGENT_ID` | Override agent UUID (normally auto-generated) | UUID | Auto-generated |
| `SERVER_CA_PATH` | Custom CA certificate for server TLS | File path | — |
| `SERVER_INSECURE_SKIP_VERIFY` | Skip TLS certificate verification | `true`, `1`, `yes` | `false` |

### Web UI

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `WEB_HTTP_PORT` | HTTP port for agent web UI | Integer | `8080` |
| `WEB_HTTPS_PORT` | HTTPS port for agent web UI | Integer | `8443` |
| `WEB_AUTH_MODE` | Authentication mode | `local`, `server`, `disabled` | `local` |
| `WEB_ALLOW_LOCAL_ADMIN` | Allow admin access from loopback | `true`, `1`, `yes` | `true` |

### Features

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `EPSON_REMOTE_MODE_ENABLED` | Enable Epson Remote Mode support | `true`, `1`, `yes` | `false` |
| `AUTO_UPDATE_MODE` | Auto-update behavior | `inherit`, `local`, `disabled` | `inherit` |

---

## Server-Specific Variables

### Network & Binding

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `SERVER_HTTP_PORT` | HTTP port for server | Integer | `9090` |
| `SERVER_HTTPS_PORT` | HTTPS port for server | Integer | `9443` |
| `BIND_ADDRESS` | Network interface to bind to | IP address | `127.0.0.1` |
| `BEHIND_PROXY` | Running behind a reverse proxy | `true`, `1` | `false` |
| `PROXY_USE_HTTPS` | Proxy terminates HTTPS | `true`, `1` | `false` |

### Agent Management

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `AUTO_APPROVE_AGENTS` | Auto-approve new agent registrations | `true`, `1` | `false` |
| `AGENT_TIMEOUT_MINUTES` | Timeout before marking agent offline | Integer | `5` |

### Authentication

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `ADMIN_USER` | Initial admin username | String | `admin` |
| `ADMIN_PASSWORD` | Initial admin password | String | `printmaster` |

> **Security:** Change the default admin password immediately after first login.

### TLS Configuration

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `TLS_MODE` | TLS mode | `none`, `self-signed`, `acme`, `manual` | `self-signed` |
| `TLS_CERT_PATH` | Path to TLS certificate (manual mode) | File path | — |
| `TLS_KEY_PATH` | Path to TLS private key (manual mode) | File path | — |

### Let's Encrypt (ACME)

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `LETSENCRYPT_DOMAIN` | Domain for Let's Encrypt certificate | String | — |
| `LETSENCRYPT_EMAIL` | Email for Let's Encrypt notifications | Email | — |
| `LETSENCRYPT_ACCEPT_TOS` | Accept Let's Encrypt Terms of Service | `true`, `1` | `false` |

### SMTP / Email

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `SMTP_ENABLED` | Enable SMTP notifications | `true`, `1` | `false` |
| `SMTP_HOST` | SMTP server hostname | String | — |
| `SMTP_PORT` | SMTP server port | Integer | `587` |
| `SMTP_USER` | SMTP authentication username | String | — |
| `SMTP_PASS` | SMTP authentication password | String | — |
| `SMTP_FROM` | Sender email address | Email | — |

### Self-Update

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `SERVER_SELF_UPDATE_ENABLED` | Enable server self-update feature | `true`, `1` | `true` |
| `PM_DISABLE_SELFUPDATE` | Force-disable self-update (Docker/orchestration) | `true`, `1` | `false` |
| `SELF_UPDATE_CHANNEL` | Release channel to track | `stable`, `beta` | `stable` |
| `SELF_UPDATE_MAX_ARTIFACTS` | Max cached update artifacts | Integer | `12` |
| `SELF_UPDATE_CHECK_INTERVAL_MINUTES` | Update check frequency | Integer | `360` |

### Release Management

| Variable | Description | Type | Default |
|----------|-------------|------|---------|
| `RELEASES_MAX_RELEASES` | Max releases to cache | Integer | `6` |
| `RELEASES_POLL_INTERVAL_MINUTES` | GitHub release poll frequency | Integer | `240` |
| `GITHUB_TOKEN` | GitHub API token for release fetching | String | — |

---

## Container Detection Variables

These are read-only environment variables that PrintMaster detects to adjust behavior.

| Variable | Description | Effect |
|----------|-------------|--------|
| `CONTAINER` | Set to `docker` or `lxc` | Disables self-update |
| `KUBERNETES_SERVICE_HOST` | Kubernetes service discovery | Disables self-update |
| `CI` | CI environment detected | Disables self-update |
| `GITHUB_ACTIONS` | GitHub Actions environment | Disables self-update |
| `GITLAB_CI` | GitLab CI environment | Disables self-update |
| `BUILDKITE` | Buildkite environment | Disables self-update |
| `TF_BUILD` | Azure DevOps environment | Disables self-update |

---

## Platform-Specific Variables

### Windows

| Variable | Description | Effect |
|----------|-------------|--------|
| `ProgramData` | System data directory | Used for service-mode data/config paths |
| `LOCALAPPDATA` | User-specific data directory | Used for user-mode data paths |
| `SERVICE_NAME` | Windows service name (runtime) | Enables service-mode behaviors |
| `SESSIONNAME` | Terminal session name | Detects service context (`Services`) |

### Linux / macOS

| Variable | Description | Effect |
|----------|-------------|--------|
| `XDG_DATA_HOME` | XDG base directory spec | User data directory (default: `~/.local/share`) |
| `HOME` | User home directory | Fallback for user-mode paths |

---

## Docker Compose Example

```yaml
version: '3.8'
services:
  printmaster-server:
    image: ghcr.io/mstrhakr/printmaster-server:latest
    ports:
      - "9443:9443"
    environment:
      # Network
      - BIND_ADDRESS=0.0.0.0
      - BEHIND_PROXY=false
      
      # TLS
      - TLS_MODE=self-signed
      
      # Authentication
      - ADMIN_USER=admin
      - ADMIN_PASSWORD=changeme
      - AUTO_APPROVE_AGENTS=true
      
      # Logging
      - LOG_LEVEL=info
      
      # Disable self-update (container managed)
      - PM_DISABLE_SELFUPDATE=true
      
      # SMTP (optional)
      - SMTP_ENABLED=false
      # - SMTP_HOST=smtp.example.com
      # - SMTP_PORT=587
      # - SMTP_USER=user
      # - SMTP_PASS=pass
      # - SMTP_FROM=printmaster@example.com
    volumes:
      - printmaster-data:/var/lib/printmaster/server

volumes:
  printmaster-data:
```

---

## Agent systemd Service Example

```ini
[Unit]
Description=PrintMaster Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/printmaster-agent
Restart=always
RestartSec=10

# Configuration
Environment=SERVER_ENABLED=true
Environment=SERVER_URL=https://printmaster.example.com:9443
Environment=AGENT_NAME=office-hq
Environment=LOG_LEVEL=info

# SNMP
Environment=SNMP_COMMUNITY=public
Environment=SNMP_VERSION=2c

[Install]
WantedBy=multi-user.target
```

---

## Quick Reference

### Minimum Docker Setup

```bash
docker run -d \
  -p 9443:9443 \
  -e BIND_ADDRESS=0.0.0.0 \
  -e ADMIN_PASSWORD=mysecurepassword \
  -e PM_DISABLE_SELFUPDATE=true \
  -v printmaster-data:/var/lib/printmaster/server \
  ghcr.io/mstrhakr/printmaster-server:latest
```

### Agent Connecting to Server

```bash
# PowerShell
$env:SERVER_ENABLED = "true"
$env:SERVER_URL = "https://printmaster.example.com:9443"
$env:AGENT_NAME = "building-a"
.\printmaster-agent.exe
```

```bash
# Bash
export SERVER_ENABLED=true
export SERVER_URL=https://printmaster.example.com:9443
export AGENT_NAME=building-a
./printmaster-agent
```

---

## See Also

- [CONFIGURATION.md](CONFIGURATION.md) — TOML config file reference
- [DOCKER_DEPLOYMENT.md](DOCKER_DEPLOYMENT.md) — Docker deployment guide
- [SERVICE_DEPLOYMENT.md](SERVICE_DEPLOYMENT.md) — Service installation guide
- [AGENT_DEPLOYMENT.md](AGENT_DEPLOYMENT.md) — Agent deployment strategies
