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
