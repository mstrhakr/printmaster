# Configuration Guide

Complete reference for configuring PrintMaster agent and server.

## Table of Contents

- [Configuration Methods](#configuration-methods)
- [Agent Configuration](#agent-configuration)
- [Server Configuration](#server-configuration)
- [Environment Variables](#environment-variables)
- [Command Line Options](#command-line-options)

---

## Configuration Methods

PrintMaster supports multiple configuration methods, applied in this order (later overrides earlier):

1. **Built-in defaults**
2. **Configuration file** (`config.toml`)
3. **Environment variables**
4. **Database-stored settings** (UI changes)
5. **Command-line flags**

### Configuration File Location

| Platform | Agent Path | Server Path |
|----------|------------|-------------|
| Windows | `C:\ProgramData\PrintMaster\agent\config.toml` | `C:\ProgramData\PrintMaster\server\config.toml` |
| Linux | `/etc/printmaster/agent.toml` | `/etc/printmaster/server.toml` |
| macOS | `/Library/Application Support/PrintMaster/agent/config.toml` | `/Library/Application Support/PrintMaster/server/config.toml` |
| Docker | `/var/lib/printmaster/agent/config.toml` | `/var/lib/printmaster/server/config.toml` |

Or place `config.toml` in the same directory as the binary.

---

## Agent Configuration

### Complete Example

```toml
# PrintMaster Agent Configuration

# Asset ID regex pattern for extracting asset tags from device data
asset_id_regex = "\\b\\d{5}\\b"

# Number of concurrent SNMP queries (adjust based on network capacity)
discovery_concurrency = 50

# Enable Epson remote-mode commands (experimental)
epson_remote_mode_enabled = false

[snmp]
  # Default SNMP community string
  community = "public"
  
  # SNMP timeout in milliseconds
  timeout_ms = 2000
  
  # Number of retries for failed SNMP queries
  retries = 1

[web]
  # HTTP port for web UI
  http_port = 8080
  
  # HTTPS port (if TLS enabled)
  https_port = 8443
  
  # Enable TLS/HTTPS
  enable_tls = false
  
  # TLS certificate file (if enable_tls = true)
  # cert_file = "/path/to/cert.pem"
  
  # TLS key file (if enable_tls = true)
  # key_file = "/path/to/key.pem"

[web.auth]
  # Authentication mode: local, server, disabled
  mode = "local"
  
  # Allow admin access from localhost without login
  allow_local_admin = true

[server]
  # Enable server upload mode
  enabled = false
  
  # PrintMaster Server URL
  url = "http://printmaster-server:9090"
  
  # Friendly name for this agent
  agent_name = "Main Office"
  
  # Path to server CA certificate (for self-signed certs)
  ca_path = ""
  
  # How often to upload discovery data (seconds)
  upload_interval_seconds = 300
  
  # How often to send heartbeat (seconds)
  heartbeat_interval_seconds = 60
  
  # Authentication token (if server requires it)
  token = ""

[database]
  # SQLite database path (blank = default location)
  path = ""

[logging]
  # Log level: debug, info, warn, error
  level = "info"

[auto_update]
  # Update mode: inherit, local, disabled
  mode = "inherit"

  [auto_update.local_policy]
    # Days between update checks
    update_check_days = 7
    
    # Version pin strategy: minor, major
    version_pin_strategy = "minor"
    
    # Allow major version upgrades
    allow_major_upgrade = false
    
    # Pin to specific version (blank = latest)
    target_version = ""
    
    # Send telemetry data
    collect_telemetry = true

    [auto_update.local_policy.maintenance_window]
      enabled = false
      timezone = "UTC"
      start_hour = 2
      start_min = 0
      end_hour = 5
      end_min = 0
      days_of_week = ["Sunday"]

    [auto_update.local_policy.rollout_control]
      staggered = true
      jitter_seconds = 300
```

### SNMP Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `community` | `public` | SNMP v1/v2c community string |
| `timeout_ms` | `2000` | Query timeout in milliseconds |
| `retries` | `1` | Retry attempts for failed queries |

**Tip**: If you use a different community string, set it here to avoid manual configuration for each scan.

### Web UI Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `http_port` | `8080` | HTTP port for web interface |
| `https_port` | `8443` | HTTPS port (when TLS enabled) |
| `enable_tls` | `false` | Enable HTTPS |
| `cert_file` | - | Path to TLS certificate |
| `key_file` | - | Path to TLS private key |

### Server Connection Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `enabled` | `false` | Enable server upload mode |
| `url` | - | Server URL (e.g., `http://server:9090`) |
| `agent_name` | hostname | Friendly name for this agent |
| `upload_interval_seconds` | `300` | Full sync interval |
| `heartbeat_interval_seconds` | `60` | Status ping interval |
| `token` | - | Authentication token |
| `ca_path` | - | CA cert for self-signed server certs |

### Auto-Update Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `mode` | `inherit` | `inherit`, `local`, or `disabled` |
| `update_check_days` | `7` | Days between checks |
| `version_pin_strategy` | `minor` | `minor` or `major` |
| `allow_major_upgrade` | `false` | Allow major version jumps |

---

## Server Configuration

### Complete Example

```toml
# PrintMaster Server Configuration

[web]
  # HTTP port
  http_port = 9090
  
  # HTTPS port
  https_port = 9443
  
  # Enable TLS
  enable_tls = false
  
  # Bind address
  bind_address = "0.0.0.0"

[database]
  # Database type: sqlite, postgres
  type = "sqlite"
  
  # SQLite path (if type = sqlite)
  path = "/var/lib/printmaster/server/printmaster.db"
  
  # PostgreSQL connection string (if type = postgres)
  # postgres_url = "postgres://user:pass@host:5432/printmaster"

[logging]
  # Log level: debug, info, warn, error
  level = "info"
  
  # Log file path (blank = stdout only)
  file = "/var/log/printmaster/server/server.log"

[auth]
  # Session timeout in hours
  session_timeout_hours = 24
  
  # Allow registration (multi-tenant mode)
  allow_registration = false

[self_update]
  # Enable server self-update feature
  enabled = true
  
  # Release channel: stable, beta
  channel = "stable"
  
  # Check interval in minutes
  check_interval_minutes = 360

[releases]
  # Max releases to cache
  max_releases = 6
  
  # Poll interval for new releases
  poll_interval_minutes = 240
```

### Database Settings

**SQLite (Default)**:
```toml
[database]
type = "sqlite"
path = "/var/lib/printmaster/server/printmaster.db"
```

**PostgreSQL**:
```toml
[database]
type = "postgres"
postgres_url = "postgres://printmaster:password@localhost:5432/printmaster?sslmode=disable"
```

### Authentication Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `session_timeout_hours` | `24` | Session expiration time |
| `allow_registration` | `false` | Allow new user registration |

---

## Environment Variables

Both agent and server support environment variable configuration. Variables override config file values.

### Common Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `LOG_LEVEL` | Log level: debug, info, warn, error | `info` |
| `CONFIG` | Path to config file | — |
| `DB_PATH` | Database path | Component default |
| `PM_DISABLE_SELFUPDATE` | Disable auto-updates | `false` |

### Agent Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AGENT_CONFIG` | Path to config file | — |
| `AGENT_DB_PATH` | Agent database path | — |
| `WEB_HTTP_PORT` | HTTP port | `8080` |
| `WEB_HTTPS_PORT` | HTTPS port | `8443` |
| `WEB_AUTH_MODE` | Auth mode: local, server, disabled | `local` |
| `WEB_ALLOW_LOCAL_ADMIN` | Allow localhost admin | `true` |
| `SNMP_COMMUNITY` | SNMP community string | `public` |
| `SNMP_TIMEOUT_MS` | SNMP timeout (ms) | `3000` |
| `SNMP_RETRIES` | SNMP retry count | `2` |
| `DISCOVERY_CONCURRENCY` | Concurrent scans | `100` |
| `SERVER_ENABLED` | Enable server mode | `false` |
| `SERVER_URL` | Central server URL | — |
| `AGENT_NAME` | Display name | Hostname |

### Server Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_CONFIG` | Path to config file | — |
| `SERVER_DB_PATH` | Server database path | — |
| `SERVER_HTTP_PORT` | HTTP port | `9090` |
| `SERVER_HTTPS_PORT` | HTTPS port | `9443` |
| `BIND_ADDRESS` | Bind address | `127.0.0.1` |
| `BEHIND_PROXY` | Behind reverse proxy | `false` |
| `TRUSTED_PROXIES` | Trusted proxy CIDRs | Private ranges |
| `ADMIN_USER` | Initial admin username | `admin` |
| `ADMIN_PASSWORD` | Initial admin password | `printmaster` |
| `AUTO_APPROVE_AGENTS` | Auto-approve agents | `false` |
| `AGENT_TIMEOUT_MINUTES` | Agent offline timeout | `5` |

### TLS Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `TLS_MODE` | none, self-signed, acme, manual | `self-signed` |
| `TLS_CERT_PATH` | Certificate path (manual) | — |
| `TLS_KEY_PATH` | Key path (manual) | — |
| `LETSENCRYPT_DOMAIN` | Let's Encrypt domain | — |
| `LETSENCRYPT_EMAIL` | Let's Encrypt email | — |
| `LETSENCRYPT_ACCEPT_TOS` | Accept ToS | `false` |

### SMTP Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SMTP_ENABLED` | Enable email | `false` |
| `SMTP_HOST` | SMTP server | — |
| `SMTP_PORT` | SMTP port | `587` |
| `SMTP_USER` | SMTP username | — |
| `SMTP_PASS` | SMTP password | — |
| `SMTP_FROM` | Sender address | — |

### Docker Example

```yaml
environment:
  - ADMIN_PASSWORD=secure-password
  - BIND_ADDRESS=0.0.0.0
  - LOG_LEVEL=info
  - BEHIND_PROXY=true
  - PM_DISABLE_SELFUPDATE=true
```

### systemd Service Example

```ini
[Service]
Environment=SERVER_ENABLED=true
Environment=SERVER_URL=https://printmaster.example.com:9443
Environment=AGENT_NAME=office-hq
Environment=LOG_LEVEL=info
```

---

## Command Line Options

### Agent

```bash
printmaster-agent [options]

Options:
  -config string
        Path to configuration file
  -port int
        HTTP port (default 8080)
  -data-dir string
        Data directory path
  -log-level string
        Log level: debug, info, warn, error
  -service string
        Service command: install, uninstall, start, stop, run
  -quiet
        Suppress informational output
  -help
        Show help
  -version
        Show version
```

### Server

```bash
printmaster-server [options]

Options:
  -config string
        Path to configuration file
  -port int
        HTTP port (default 9090)
  -data-dir string
        Data directory path
  -log-level string
        Log level: debug, info, warn, error
  -help
        Show help
  -version
        Show version
```

---

## Configuration via Web UI

Most settings can be changed through the web interface:

### Agent UI

- **Settings** → **Discovery**: SNMP settings, concurrency
- **Settings** → **Server**: Server connection settings
- **Settings** → **Updates**: Auto-update preferences
- **Devices** → **IP Ranges**: Networks to scan

### Server UI

- **Settings** → **General**: Basic server settings
- **Settings** → **Authentication**: User management
- **Settings** → **Updates**: Fleet update policies
- **Settings** → **Integrations**: Webhooks and API

Changes made in the UI are saved to the database and take effect immediately. They override file-based configuration.

---

## Configuration Precedence

When the same setting is configured in multiple places, the last value wins:

1. Built-in defaults (lowest priority)
2. Configuration file (`config.toml`)
3. Environment variables
4. Database (UI settings)
5. Command-line flags (highest priority)

**Example**: If you set `http_port = 8080` in the config file but run with `-port 9000`, the agent will use port 9000.
