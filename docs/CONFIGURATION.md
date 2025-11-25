# Configuration

## Database-First Approach

debug_logging=false
dump_parse_debug=false
discover_concurrency=100
PrintMaster stores durable settings in SQLite:
- **devices.db** – Device profiles and scan history
- **agent.db** – Agent configuration, IP ranges, managed settings snapshots

The UI remains the primary place to edit discovery settings, schedules, and ranges. Configuration files act as boot-time defaults/overrides.

## `config.toml` Override (Optional)

Deployments that need immutable defaults (e.g., SNMP communities, server URLs) can drop a `config.toml` next to the binary. Reference `agent/config.example.toml` or `server/config.example.toml` for all fields.

Load order for the agent:
1. Built-in defaults (see `DefaultAgentConfig`)
2. `config.toml` (if present or pointed to via env/flag)
3. Environment variables (component-prefixed)
4. Database-stored settings (UI overrides, managed snapshots)

This allows you to:
- Keep golden values in version control without editing the DB directly
- Provide different defaults per environment/site
- Layer runtime overrides through environment variables or server-managed settings

## Example `config.toml`

```toml
asset_id_regex = "\\bAST-\\d{6}\\b"
discovery_concurrency = 100

[snmp]
	community = "private"
	timeout_ms = 3000
	retries = 2

[logging]
	level = "debug"

[web]
	http_port = 8080
	https_port = 8443
	enable_tls = false

[server]
	enabled = true
	url = "https://printmaster.example.com:9443"
	name = "HQ Agent"
```

### Agent Auto-Update Overrides

Agents can optionally pin their own update cadence when disconnected from the
server by adding an `[auto_update]` block:

```toml
[auto_update]
mode = "inherit" # inherit | local | disabled

	[auto_update.local_policy]
	update_check_days = 7
	version_pin_strategy = "minor"
	allow_major_upgrade = false
	target_version = ""
	collect_telemetry = true

	[auto_update.local_policy.maintenance_window]
	enabled = false
	timezone = "UTC"

	[auto_update.local_policy.rollout_control]
	staggered = true
```

Set `mode = "local"` to force the agent to use the local policy even when a
fleet policy exists. `mode = "disabled"` turns off unattended updates until a
manual install is triggered. Fallback to local policy only occurs when the
agent cannot reach the server.

### Fleet Auto-Update Policies (Server)

When tenancy is enabled, administrators can manage per-tenant fleet policies
through the server API. These policies mirror the agent structure and are
served via:

- `GET /api/v1/update-policies` — list all configured fleet policies.
- `GET /api/v1/tenants/{tenant_id}/update-policy` — fetch a single tenant's policy.
- `PUT /api/v1/tenants/{tenant_id}/update-policy` — upsert the `policy` payload.
- `DELETE /api/v1/tenants/{tenant_id}/update-policy` — remove the stored policy and
	fall back to agent overrides.

Request bodies for `PUT` should wrap the policy spec:

```json
{
	"policy": {
		"update_check_days": 7,
		"version_pin_strategy": "minor",
		"maintenance_window": {
			"enabled": false
		},
		"rollout_control": {
			"staggered": true
		},
		"collect_telemetry": true
	}
}
```

All responses return the persisted policy plus metadata (tenant id and
`updated_at`). A `404` indicates that the tenant has not configured a fleet
policy and agents will fall back to their local override.

## Settings via UI

Most users should configure settings through the web UI:
- **Settings Tab** → Discovery Settings (toggle ARP, TCP, SNMP, etc.)
- **Settings Tab** → Subnet Scanning (auto-scan local network)
- **Devices Tab** → IP Ranges (manual ranges for scanning)

All changes are immediately saved to the database.

## Legacy Note

`dev_settings.json` and `config.ini` were removed. If you still have those files, delete them and migrate any values into `config.toml` or the Settings UI.

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
