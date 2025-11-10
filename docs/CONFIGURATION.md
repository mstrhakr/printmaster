# Configuration

## Database-First Approach

PrintMaster stores all settings in SQLite databases:
- **devices.db** - Device profiles and scan history
- **agent.db** - Agent configuration and settings

All configuration is managed through the web UI Settings tab and persists across restarts.

## config.ini Override (Optional)

For deployment-specific overrides without modifying the database, create a `config.ini` file in the agent directory. See `config.ini.example` for available settings.

The agent loads settings in this order:
1. Database defaults
2. Stored database values
3. config.ini overrides (if file exists)

This allows you to:
- Deploy with environment-specific settings (SNMP community, regex patterns, etc.)
- Override database settings without UI changes
- Keep deployment configs in version control

## Example config.ini

```ini
# SNMP Settings
snmp_community=private
snmp_timeout_ms=3000
snmp_retries=2

# Asset ID extraction (regex for extracting asset tags from SNMP fields)
asset_id_regex=\bAST-\d{6}\b

# Debug
debug_logging=false
dump_parse_debug=false

# Performance
discover_concurrency=100
```

## Settings via UI

Most users should configure settings through the web UI:
- **Settings Tab** → Discovery Settings (toggle ARP, TCP, SNMP, etc.)
- **Settings Tab** → Subnet Scanning (auto-scan local network)
- **Devices Tab** → IP Ranges (manual ranges for scanning)

All changes are immediately saved to the database.

## Legacy Note

The `dev_settings.json` file is no longer used. Settings have been migrated to the database. If you have an existing `dev_settings.json`, delete it - the agent now uses database + optional config.ini.

## Environment variables (CLI / Docker friendly)

PrintMaster supports component-prefixed and generic environment variables for configuration and database paths. Precedence when resolving a config file path is:

1. <COMPONENT>_CONFIG (e.g., AGENT_CONFIG or SERVER_CONFIG)
2. <COMPONENT>_CONFIG_PATH (e.g., AGENT_CONFIG_PATH or SERVER_CONFIG_PATH)
3. CONFIG
4. CONFIG_PATH
5. --config CLI flag

Examples:

- To run the server with a custom config in Docker:

```powershell
# Windows / PowerShell example
$env:SERVER_CONFIG = 'C:\configs\server.toml'
.\printmaster-server.exe
```

- To run the agent with a custom config file via env var:

```powershell
$env:AGENT_CONFIG = '/etc/printmaster/agent.toml'
.\printmaster-agent.exe
```

Database path overrides (prefix-aware):

- AGENT_DB_PATH and SERVER_DB_PATH are checked first for component-specific DB locations.
- If those are not set, the generic DB_PATH is used as a fallback.

Examples:

```powershell
$env:SERVER_DB_PATH = 'C:\data\printmaster\server.db'
$env:AGENT_DB_PATH = '/var/lib/printmaster/agent.db'
```

Other useful env vars supported (non-exhaustive):

- LOG_LEVEL or <COMPONENT>_LOG_LEVEL (e.g., SERVER_LOG_LEVEL) — logging verbosity (debug/info/warn/error)
- CONFIG/CONFIG_PATH — generic config path fallbacks

If you want different behavior (for example prefer CLI flag over generic CONFIG), update the helper in `common/config/` which centralizes precedence rules.
