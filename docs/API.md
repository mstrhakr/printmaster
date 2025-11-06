# PrintMaster Agent API Reference

## Overview

The PrintMaster Agent provides a REST API for managing network printer discovery, device storage, metrics collection, and configuration. All endpoints return JSON unless otherwise specified.

**Base URL**: `http://localhost:8080` (default)  
**Protocol**: HTTP (development) or HTTPS (production with self-signed certs)

---

## Endpoints by Category

### UI & System

#### `GET /`
Serves the web UI (single-page application).
- Embedded from binary, no external files needed
- Returns HTML template

#### `GET /static/{file}`
Serves static assets (CSS, JS) from embedded filesystem.
- Examples: `/static/app.css`, `/static/app.js`

#### `GET /events`
Server-Sent Events (SSE) stream for real-time UI updates.
- Content-Type: `text/event-stream`
- Events: `connected`, `discovery_update`, `device_change`, etc.
```javascript
// Client usage:
const eventSource = new EventSource('/events');
eventSource.addEventListener('discovery_update', (e) => {
  const data = JSON.parse(e.data);
  console.log('Discovery progress:', data);
});
```

---

### Device Management

#### `GET /devices/discovered`
List devices discovered but not saved.
- Filter: `is_saved=false, visible=true`
- Returns: Array of `PrinterInfo` objects
```json
[{
  "IP": "10.0.0.100",
  "Manufacturer": "HP",
  "Model": "LaserJet Pro M404n",
  "Serial": "JPBCD12345",
  "PageCount": 12453,
  "TonerLevels": {"Black": 45}
}]
```

#### `GET /devices/list`
List all saved devices.
- Filter: `is_saved=true`
- Returns: Array of `Device` objects with full metadata

#### `GET /devices/get?serial={serial}`
Get single device by serial number.
- Query param: `serial` (required)
- Returns: Single `Device` object
- Error: `404` if not found

#### `POST /devices/save`
Save a single discovered device.
```json
{"serial": "JPBCD12345"}
```
- Sets `is_saved=true`
- Returns: `200 OK`

#### `POST /devices/save/all`
Save all currently discovered devices.
- No request body needed
- Sets `is_saved=true` for all discovered devices

#### `POST /devices/delete`
Permanently delete a device.
```json
{"serial": "JPBCD12345"}
```
- **Hard delete**: Removes device + all scan history (CASCADE)
- Returns: `200 OK`

#### `POST /devices/clear_discovered`
Hide all discovered devices (soft delete).
- Sets `visible=false` on all discovered devices
- Preserves data and scan history
- Returns: Plain text count (e.g., `hidden 5 devices`)

#### `POST /devices/refresh`
Refresh device data from network.
```json
{"serial": "JPBCD12345"}
```
- Re-scans device via SNMP
- Updates device record with latest data

#### `POST /devices/update`
Update device fields (user-editable metadata).
```json
{
  "serial": "JPBCD12345",
  "asset_number": "IT-2024-001",
  "location": "3rd Floor Copy Room",
  "description": "Main office printer"
}
```
- Updates: `asset_number`, `location`, `description`
- Respects `locked_fields` to prevent overwriting

#### `GET /devices/preview?serial={serial}`
Preview device data before saving changes.
- Shows diff of pending vs current values

#### `POST /devices/lock`
Lock/unlock specific device fields to prevent auto-updates.
```json
{
  "serial": "JPBCD12345",
  "locked_fields": ["location", "description"]
}
```
- Prevents SNMP discovery from overwriting locked fields

---

### Metrics Collection

#### `POST /devices/metrics/collect`
Manually trigger metrics collection for a device.
```json
{"serial": "JPBCD12345"}
```
- Queries SNMP for page counts, toner levels, scan counts
- Stores snapshot in `metrics_raw` table
- Returns: Latest metrics snapshot

---

### Network Discovery

#### `POST /discover`
Start network discovery scan.
- Scans saved IP ranges (if `manual_ranges` enabled)
- Auto-discovers local subnet (if `subnet_scan` enabled)
- Respects discovery method toggles (ARP, TCP, SNMP, mDNS, WS-Discovery, SSDP)
- Returns: Discovered devices array (synchronous)

#### `POST /discover_now`
Alias for `/discover` (legacy endpoint).

---

### Configuration & Settings

#### `GET /settings`
Get all agent settings.
```json
{
  "discovery": {
    "subnet_scan": true,
    "manual_ranges": true,
    "ranges_text": "10.0.0.1-10.0.0.254\n192.168.1.0/24",
    "enable_arp": true,
    "enable_tcp": true,
    "enable_snmp": true,
    "enable_mdns": true,
    "snmp_timeout_ms": 2000,
    "snmp_retries": 1,
    "discover_concurrency": 20
  },
  "developer": {
    "debug_mode": false,
    "verbose_logging": false
  },
  "security": {
    "enable_https": false,
    "require_auth": false
  }
}
```

#### `POST /settings`
Update agent settings.
```json
{
  "discovery": {
    "subnet_scan": true,
    "ranges_text": "10.0.0.1-10.0.0.254"
  }
}
```
- Partial updates supported (only send changed sections)
- Triggers `applyDiscoveryEffects()` to enable/disable live discovery methods
- IP ranges saved separately to avoid duplication

#### `GET /settings/subnet_scan` *(deprecated)*
Legacy endpoint for subnet scan toggle.
- Use `/settings` instead

---

### Logging & Debugging

#### `GET /logs`
Get recent log messages from memory buffer.
- Query params:
  - `level` = ERROR | WARN | INFO | DEBUG | TRACE (filters by severity)
  - `tail` = N (returns last N lines after filtering)
- Returns: Array of log strings

#### `GET /logfile`
Download full log file from disk.
- Path: `logs/agent.log`
- Content-Type: `text/plain`

#### `POST /logs/archive`
Archive current log file with timestamp.
- Creates: `logs/agent.log.{timestamp}`
- Starts fresh `agent.log`

#### `POST /logs/clear`
Clear in-memory log buffer (does not delete log file).

#### `GET /unknown_manufacturers`
List manufacturers not recognized by vendor detection.
- Debug endpoint for improving vendor detection

#### `GET /parse_debug?ip={ip}`
Get detailed parse debug info for a device.
- Returns: OID-level parsing details

#### `GET /scan_metrics`
Get scanning statistics and performance metrics.

---

### Database Management

#### `GET /database/rotation_warning`
Check if database rotation occurred (needs user acknowledgment).
```json
{
  "rotated": true,
  "rotated_at": "2025-11-06T10-37-28",
  "backup_path": "C:\\Users\\...\\devices.db.backup.2025-11-06T10-37-28"
}
```

#### `POST /database/rotation_warning`
Clear rotation warning flag after user notification.
```json
{"success": true, "message": "Rotation warning cleared"}
```

#### `POST /database/clear`
**DANGER**: Clear entire device database.
- Removes all devices and metrics
- Cannot be undone
- Use for testing/development only

---

### Device Web UI Credentials

#### `GET /device/webui-credentials?serial={serial}`
Get saved web UI credentials for a device.
```json
{
  "username": "admin",
  "password": "encrypted_value"
}
```

#### `POST /device/webui-credentials`
Save web UI credentials for a device.
```json
{
  "serial": "JPBCD12345",
  "username": "admin",
  "password": "printer_password"
}
```
- Passwords encrypted at rest using AES-256

---

### TLS Certificate Management

#### `POST /api/regenerate-certs`
Regenerate self-signed TLS certificates.
- Deletes old `server.crt` and `server.key`
- Generates new certificate with current system hostname
- Requires agent restart to take effect

---

## Database Schema

### Tables Overview

| Table | Purpose | Retention |
|-------|---------|-----------|
| `devices` | Device inventory | Permanent |
| `metrics_raw` | 5-minute metrics snapshots | 7 days |
| `metrics_hourly` | 1-hour aggregates | 30 days |
| `metrics_daily` | 1-day aggregates | 365 days |
| `metrics_monthly` | 1-month aggregates | Forever |
| `scan_history` | Discovery scan history | 30 days |
| `agent_config` | Settings storage | Permanent |
| `schema_version` | Migration tracking | Permanent |

### `devices` Table

**Primary device inventory** (27 fields):

```sql
CREATE TABLE devices (
  serial TEXT PRIMARY KEY,
  ip TEXT NOT NULL,
  manufacturer TEXT,
  model TEXT,
  hostname TEXT,
  firmware TEXT,
  mac_address TEXT,
  subnet_mask TEXT,
  gateway TEXT,
  dns_servers TEXT,          -- JSON array
  dhcp_server TEXT,
  consumables TEXT,          -- JSON array
  status_messages TEXT,      -- JSON array
  last_seen DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  first_seen DATETIME NOT NULL,
  is_saved BOOLEAN DEFAULT 0,
  visible BOOLEAN DEFAULT 1,
  discovery_method TEXT,
  walk_filename TEXT,
  last_scan_id INTEGER,
  asset_number TEXT,         -- User-defined
  location TEXT,             -- User-defined
  description TEXT,          -- User-defined
  web_ui_url TEXT,
  locked_fields TEXT,        -- JSON array
  raw_data TEXT              -- JSON blob
);
```

**Key fields**:
- `is_saved`: User-saved device (vs discovered-only)
- `visible`: Soft-delete flag (hidden devices)
- `locked_fields`: Prevents SNMP from overwriting user edits
- **NO `page_count`, NO `toner_levels`**: Moved to `metrics_raw` (schema v7+)

**Indexes**: `is_saved`, `visible`, `ip`, `last_seen`, `manufacturer`

---

### `metrics_raw` Table

**High-resolution metrics** (5-minute snapshots, 7-day retention):

```sql
CREATE TABLE metrics_raw (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  serial TEXT NOT NULL,
  timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  page_count INTEGER DEFAULT 0,
  color_pages INTEGER DEFAULT 0,
  mono_pages INTEGER DEFAULT 0,
  scan_count INTEGER DEFAULT 0,
  toner_levels TEXT,         -- JSON: {"Black": 45, "Cyan": 78}
  FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);
```

**Usage**: Real-time monitoring, recent trends, current device state

---

### `metrics_hourly` Table

**Hourly aggregates** (1-hour buckets, 30-day retention):

```sql
CREATE TABLE metrics_hourly (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  serial TEXT NOT NULL,
  hour_start DATETIME NOT NULL,
  sample_count INTEGER DEFAULT 0,
  page_count_min INTEGER,
  page_count_max INTEGER,
  page_count_avg INTEGER,
  -- (similar for color_pages, mono_pages, scan_count)
  toner_levels_avg TEXT,
  UNIQUE(serial, hour_start)
);
```

**Usage**: Hourly charts, recent activity patterns

---

### `metrics_daily` Table

**Daily aggregates** (1-day buckets, 365-day retention):

```sql
CREATE TABLE metrics_daily (
  -- Same structure as metrics_hourly, but day_start instead
);
```

**Usage**: Monthly reports, historical trends

---

### `metrics_monthly` Table

**Monthly aggregates** (1-month buckets, permanent retention):

```sql
CREATE TABLE metrics_monthly (
  -- Same structure, but month_start
);
```

**Usage**: Long-term capacity planning, yearly reports

---

### `scan_history` Table

**Discovery scan history** (30-day retention):

```sql
CREATE TABLE scan_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  serial TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  ip TEXT NOT NULL,
  hostname TEXT,
  firmware TEXT,
  consumables TEXT,
  status_messages TEXT,
  discovery_method TEXT,
  walk_filename TEXT,
  raw_data TEXT,
  FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);
```

**Usage**: Audit trail, device change tracking (IP changes, firmware updates)

---

### `agent_config` Table

**Settings storage** (key-value store):

```sql
CREATE TABLE agent_config (
  key TEXT PRIMARY KEY,
  value TEXT,                -- JSON
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

**Common keys**:
- `ip_ranges`: Saved IP ranges (newline-separated)
- `discovery_settings`: Discovery toggles and parameters
- `dev_settings`: Developer options
- `security_settings`: Security configuration
- `database_rotated`: Rotation event flag
- `database_rotation_timestamp`: Rotation timestamp

---

## Data Flow

### Discovery Flow
```
1. User clicks "Discover Now"
2. POST /discover → scanner.Discover()
3. Scanner uses enabled methods (ARP, SNMP, mDNS, etc.)
4. Devices stored: deviceStore.StoreDiscoveredDevice(is_saved=false, visible=true)
5. Background sync (10s): Snapshot → metrics_raw
6. UI polls /devices/discovered → Shows new devices
```

### Save Flow
```
1. User clicks "Save" on discovered device
2. POST /devices/save {"serial": "XXX"}
3. deviceStore.MarkSaved(serial) → is_saved=true
4. Device moves from "Discovered" to "Saved" tab
```

### Metrics Collection
```
1. Background job (5-minute interval)
2. For each saved device:
   - SNMP query: page counts, toner levels, scan counts
   - Store snapshot in metrics_raw
3. Hourly aggregation job:
   - Roll up metrics_raw → metrics_hourly (min/max/avg)
4. Daily aggregation job:
   - Roll up metrics_hourly → metrics_daily
5. Monthly aggregation job:
   - Roll up metrics_daily → metrics_monthly
```

### Database Rotation
```
1. Agent starts → NewSQLiteStoreWithConfig()
2. Schema migration fails (corrupted DB)
3. RotateDatabase():
   - Rename devices.db → devices.db.backup.{timestamp}
   - Set agent_config flag: database_rotated=true
4. Create fresh devices.db, retry initialization
5. UI loads → GET /database/rotation_warning → Show popup
6. User acknowledges → POST /database/rotation_warning → Clear flag
```

---

## Background Processes

### Device Sync (10-second interval)
- Syncs discovered devices to database
- Creates `scan_history` snapshots
- Updates `last_seen` timestamps

### Metrics Collection (5-minute interval)
- Queries saved devices via SNMP
- Stores snapshots in `metrics_raw`

### Metrics Aggregation (hourly/daily/monthly)
- Rolls up raw data into aggregates
- Deletes old raw data per retention policy

### Garbage Collection
- Removes `metrics_raw` older than 7 days
- Removes `scan_history` older than 30 days
- Removes hidden devices older than threshold (configurable)

---

## Error Handling

**HTTP Status Codes**:
- `200 OK` - Success
- `400 Bad Request` - Invalid parameters or JSON
- `404 Not Found` - Resource not found
- `405 Method Not Allowed` - Wrong HTTP method
- `500 Internal Server Error` - Server error

**Error Response Format**:
```json
{
  "error": "Device not found",
  "details": "No device with serial JPBCD12345"
}
```

Or plain text for simple endpoints:
```
invalid json
```

---

## Storage Locations

### Database Files
- **Windows**: `%LOCALAPPDATA%\PrintMaster\devices.db`
- **macOS**: `~/Library/Application Support/PrintMaster/devices.db`
- **Linux**: `~/.local/share/printmaster/devices.db`

### Configuration Database
- Same directory as `devices.db`, named `agent.db`

### Log Files
- `logs/agent.log` (current log, rotated at 100MB)
- `logs/agent.log.{timestamp}` (archived logs)

### TLS Certificates
- `server.crt` and `server.key` (same directory as databases)

---

## Migration Notes

### Schema Version History
- **v1-v6**: Legacy schema (page_count, toner_levels in devices table)
- **v7**: Metrics separation (metrics_raw, metrics_history split)
- **v8**: Current schema (metrics tiering: raw/hourly/daily/monthly)

### Database Rotation
- Automatic on migration failure
- Creates timestamped backup
- UI notification required
- Manual recovery: Restore from backup, fix schema, restart agent

### Deprecated Endpoints
- `/saved_ranges` → Use `/settings` (GET/POST for `ip_ranges`)
- `/settings/subnet_scan` → Use `/settings` (discovery section)

---

## API Usage Examples

### Discover and Save Workflow
```javascript
// 1. Start discovery
await fetch('/discover', {method: 'POST'});

// 2. Wait a few seconds, then get discovered devices
const resp = await fetch('/devices/discovered');
const devices = await resp.json();

// 3. Save a device
await fetch('/devices/save', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({serial: devices[0].Serial})
});

// 4. View saved devices
const saved = await fetch('/devices/list');
const savedDevices = await saved.json();
```

### Update Device Metadata
```javascript
await fetch('/devices/update', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    serial: 'JPBCD12345',
    asset_number: 'IT-2024-001',
    location: '3rd Floor Copy Room',
    description: 'Main office printer'
  })
});
```

### Configure Discovery Settings
```javascript
await fetch('/settings', {
  method: 'POST',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    discovery: {
      subnet_scan: true,
      manual_ranges: true,
      ranges_text: '10.0.0.1-10.0.0.254\n192.168.1.0/24',
      enable_snmp: true,
      snmp_timeout_ms: 3000
    }
  })
});
```

---

## Future Enhancements

- [ ] Add `/devices/history?serial=XXX` to query metrics history
- [ ] Add `/devices/restore` to restore hidden devices
- [ ] Add filtering/pagination to `/devices/list` and `/devices/discovered`
- [ ] Add WebSocket support for real-time updates (in addition to SSE)
- [ ] Add job IDs and cancellation endpoints for long-running scans
- [ ] Add OpenAPI/Swagger spec generation
- [ ] Add `/devices/stats` for fleet-wide statistics
- [ ] Add `/devices/export` for bulk export (CSV/JSON)

---

*Last Updated: November 6, 2025 (Schema v8)*
