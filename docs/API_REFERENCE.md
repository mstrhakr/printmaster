# PrintMaster Agent API Reference

## Overview

The PrintMaster Agent provides a REST API for managing network printer discovery, device storage, and scan history. All endpoints return JSON unless otherwise specified.

**Base URL**: `http://localhost:8080` (default)

---

## Device Management API

### Get Discovered Devices

Retrieve list of devices that have been discovered but not saved.

**Endpoint**: `GET /devices/discovered`

**Query Parameters**: None

**Response**: `200 OK`
```json
[
  {
    "IP": "10.2.106.72",
    "Manufacturer": "HP",
    "Model": "LaserJet Pro M404n",
    "Serial": "JPBCD12345",
    "Hostname": "hp-printer-01",
    "Firmware": "002.1916A",
    "MAC": "00:11:22:33:44:55",
    "SubnetMask": "255.255.255.0",
    "Gateway": "10.2.106.1",
    "DNSServers": ["8.8.8.8"],
    "DHCPServer": "10.2.106.1",
    "PageCount": 12453,
    "TonerLevels": {
      "Black": 45
    },
    "Consumables": ["Toner Cartridge"],
    "StatusMessages": ["Ready"],
    "LastSeen": "2025-11-01T15:30:45Z"
  }
]
```

**Notes**:
- Only returns devices with `is_saved=false` and `visible=true`
- Background sync updates every 10 seconds
- Devices without serial numbers may not be saved

---

### Get Saved Devices

Retrieve list of all saved devices.

**Endpoint**: `GET /devices/list`

**Query Parameters**: None

**Response**: `200 OK`
```json
[
  {
    "serial": "JPBCD12345",
    "ip": "10.2.106.72",
    "manufacturer": "HP",
    "model": "LaserJet Pro M404n",
    "hostname": "hp-printer-01",
    "firmware": "002.1916A",
    "mac_address": "00:11:22:33:44:55",
    "subnet_mask": "255.255.255.0",
    "gateway": "10.2.106.1",
    "dns_servers": ["8.8.8.8"],
    "dhcp_server": "10.2.106.1",
    "page_count": 12453,
    "toner_levels": {"Black": 45},
    "consumables": ["Toner Cartridge"],
    "status_messages": ["Ready"],
    "last_seen": "2025-11-01T15:30:45Z",
    "created_at": "2025-10-25T10:15:30Z",
    "first_seen": "2025-10-25T10:15:30Z",
    "is_saved": true,
    "visible": true,
    "discovery_method": "snmp",
    "walk_filename": "mib_walk_10_2_106_72_20251025T101530.json",
    "last_scan_id": 42,
    "raw_data": {}
  }
]
```

**Notes**:
- Only returns devices with `is_saved=true`
- Contains full device history and metadata
- `last_scan_id` references most recent scan in `scan_history` table

---

### Get Device by Serial

Retrieve a single device by its serial number.

**Endpoint**: `GET /devices/get?serial={serial}`

**Query Parameters**:
- `serial` (required) - Device serial number

**Response**: `200 OK`
```json
{
  "serial": "JPBCD12345",
  "ip": "10.2.106.72",
  // ... (same structure as /devices/list)
}
```

**Error Responses**:
- `400 Bad Request` - Missing serial parameter
- `404 Not Found` - Device not found in database or files

**Notes**:
- Queries database first, falls back to legacy JSON files
- Returns full device object with all metadata

---

### Save/Merge Device

Save a discovered device or merge new data into existing saved device.

**Endpoint**: `POST /devices/merge`

**Request Body** (Option 1 - from walk file):
```json
{
  "walk": "mib_walk_10_2_106_72_20251101T153045.json"
}
```

**Request Body** (Option 2 - from IP):
```json
{
  "ip": "10.2.106.72"
}
```

**Response**: `200 OK`
```json
{
  "message": "Device merged successfully"
}
```

**Error Responses**:
- `400 Bad Request` - Invalid JSON or missing parameters
- `404 Not Found` - Walk file not found or IP not in discovered list
- `500 Internal Server Error` - Database error

**Notes**:
- **Fast path** (`ip`): Uses data from discovered devices in memory/database
- **Slow path** (`walk`): Parses saved MIB walk file from disk
- Sets `is_saved=true` on the device
- Preserves scan history and changelog

---

### Delete Device

Permanently remove a device from the database.

**Endpoint**: `POST /devices/delete`

**Request Body**:
```json
{
  "serial": "JPBCD12345"
}
```

**Response**: `200 OK`

**Error Responses**:
- `400 Bad Request` - Invalid JSON or missing serial
- `404 Not Found` - Device not found
- `500 Internal Server Error` - Delete failed

**Notes**:
- **Hard delete**: Removes device and all scan history (CASCADE)
- Tries database first, falls back to deleting legacy JSON file
- Cannot be undone - use "hide" instead if you want to preserve data

---

### Clear Discovered Devices

Hide all discovered devices (soft delete).

**Endpoint**: `POST /devices/clear_discovered`

**Request Body**: None

**Response**: `200 OK`
```
hidden 5 devices
```

**Error Responses**:
- `405 Method Not Allowed` - Must use POST
- `500 Internal Server Error` - Database error

**Notes**:
- **Soft delete**: Sets `visible=false` on all `is_saved=false` devices
- Data and scan history preserved in database
- Can be restored by setting `visible=true` (no API endpoint yet)
- Fallback: clears in-memory array if database unavailable

---

## Scanning API

### Start IP Range Scan

Start scanning a list of saved IP ranges.

**Endpoint**: `POST /scan_ips`

**Request Body**: None (uses saved ranges from config)

**Response**: `200 OK`
```json
{
  "message": "Scan started",
  "total_queued": 254
}
```

**Notes**:
- Uses IP ranges from `config.json`
- Non-blocking - returns immediately
- Poll `/scan_status` for progress

---

### Auto-Discover Network

Scan local network using ARP and subnet enumeration.

**Endpoint**: `POST /discover`

**Request Body**: None

**Response**: `200 OK`
```json
{
  "message": "Discovery started"
}
```

**Notes**:
- Discovers devices on local subnet automatically
- Non-blocking - returns immediately
- Poll `/scan_status` for progress

---

### Get Scan Status

Check progress of currently running scan.

**Endpoint**: `GET /scan_status`

**Response**: `200 OK`
```json
{
  "running": true,
  "source": "scan_ips",
  "total_queued": 254,
  "completed": 127
}
```

**Fields**:
- `running` - Boolean indicating if scan is active
- `source` - Origin of scan (`"scan_ips"`, `"discover"`, or `""`)
- `total_queued` - Total IPs to scan
- `completed` - Number of IPs scanned so far

---

## Configuration API

### Get Saved Ranges

Retrieve saved IP ranges from configuration.

**Endpoint**: `GET /saved_ranges`

**Response**: `200 OK`
```json
{
  "text": "10.0.0.1-10.0.0.254\n192.168.1.0/24\nprinter.local"
}
```

---

### Save IP Ranges

Persist IP ranges to configuration file.

**Endpoint**: `POST /save_ranges`

**Request Body**:
```json
{
  "mode": "override",
  "text": "10.0.0.1-10.0.0.254\n192.168.1.0/24"
}
```

**Fields**:
- `mode` - `"add"` (prepend) or `"override"` (replace)
- `text` - Newline-separated IP ranges

**Response**: `200 OK`

**Supported Range Formats**:
- Single IP: `10.0.0.1`
- IP range: `10.0.0.1-10.0.0.254`
- CIDR: `192.168.1.0/24`
- Hostname: `printer.local`

---

## Logging API

### Get Recent Logs

Retrieve recent log messages from memory.

**Endpoint**: `GET /logs`

**Response**: `200 OK`
```json
[
  "2025-11-01 15:30:45 - Scan started: 254 IPs queued",
  "2025-11-01 15:30:46 - Found device: 10.2.106.72 (HP LaserJet)"
]
```

---

### Get Full Log File

Download complete log file from disk.

**Endpoint**: `GET /logfile`

**Response**: `200 OK` (text/plain)
```
2025-11-01 15:30:45 - Agent starting...
2025-11-01 15:30:45 - Using device database: C:\Users\...\devices.db
...
```

---

## Database Schema

### devices Table

| Column | Type | Description |
|--------|------|-------------|
| serial | TEXT (PK) | Device serial number |
| ip | TEXT | Current IP address |
| manufacturer | TEXT | Device manufacturer |
| model | TEXT | Device model |
| hostname | TEXT | Network hostname |
| firmware | TEXT | Firmware version |
| mac_address | TEXT | MAC address |
| subnet_mask | TEXT | Subnet mask |
| gateway | TEXT | Default gateway |
| dns_servers | TEXT (JSON) | DNS servers |
| dhcp_server | TEXT | DHCP server |
| page_count | INTEGER | Total pages printed |
| toner_levels | TEXT (JSON) | Toner percentages |
| consumables | TEXT (JSON) | Consumable list |
| status_messages | TEXT (JSON) | Status messages |
| last_seen | DATETIME | Last discovery time |
| created_at | DATETIME | First creation time |
| first_seen | DATETIME | First discovery time |
| is_saved | BOOLEAN | Saved by user flag |
| visible | BOOLEAN | Visible in UI flag |
| discovery_method | TEXT | How device was found |
| walk_filename | TEXT | Associated MIB walk file |
| last_scan_id | INTEGER | FK to scan_history |
| raw_data | TEXT (JSON) | Extended fields |

**Indexes**:
- `idx_devices_is_saved` on `is_saved`
- `idx_devices_visible` on `visible`
- `idx_devices_ip` on `ip`
- `idx_devices_last_seen` on `last_seen`
- `idx_devices_manufacturer` on `manufacturer`

---

### scan_history Table

| Column | Type | Description |
|--------|------|-------------|
| id | INTEGER (PK) | Auto-increment ID |
| serial | TEXT (FK) | Device serial (CASCADE) |
| created_at | DATETIME | Scan timestamp |
| ip | TEXT | IP at scan time |
| hostname | TEXT | Hostname at scan time |
| firmware | TEXT | Firmware at scan time |
| page_count | INTEGER | Page count at scan time |
| toner_levels | TEXT (JSON) | Toner levels at scan time |
| consumables | TEXT (JSON) | Consumables at scan time |
| status_messages | TEXT (JSON) | Status at scan time |
| discovery_method | TEXT | How discovered |
| walk_filename | TEXT | Associated walk file |
| raw_data | TEXT (JSON) | Full snapshot |

**Indexes**:
- `idx_scan_history_serial` on `serial`
- `idx_scan_history_created` on `created_at`

**Usage**:
- Track changes over time (toner depletion, page count increases)
- Generate reports and trends
- Audit device changes
- Configurable retention (default 30 days)

---

## Background Processes

### Device Sync (10s interval)
- Syncs in-memory discovered devices to database
- Creates scan history snapshots
- Updates `last_seen` timestamps
- Sets `is_saved=false, visible=true` by default

### Garbage Collection (configurable)
- Deletes scan history older than retention period (default 30 days)
- Removes hidden devices older than threshold
- Configurable via settings (not yet implemented in UI)

---

## Error Handling

All endpoints use standard HTTP status codes:

- `200 OK` - Success
- `400 Bad Request` - Invalid parameters or JSON
- `404 Not Found` - Resource not found
- `405 Method Not Allowed` - Wrong HTTP method
- `500 Internal Server Error` - Server error

Error responses include descriptive text in the response body.

---

## Data Flow

```
1. Discovery:
   Scanner → PrinterInfo → deviceStore.StoreDiscoveredDevice(is_saved=false, visible=true)
                        → scan_history snapshot
   
2. User Saves:
   /devices/save → deviceStore.MarkSaved(serial) → is_saved=true
   
3. User Clears:
   /devices/clear_discovered → deviceStore.HideDiscovered() → visible=false
   
4. User Deletes:
   /devices/delete → deviceStore.Delete(serial) → CASCADE scan_history
   
5. Query Discovered:
   /devices/discovered → deviceStore.ListDevices(filter: is_saved=false, visible=true)
```

---

## Migration Notes

### From Legacy JSON Files

Legacy devices stored in `logs/devices/{serial}.json` are supported via fallback:
- `/devices/get` reads from JSON if not in database
- `/devices/delete` removes JSON file if not in database
- Migration tool planned to import JSON → SQLite

### Storage Architecture (Updated November 2, 2025)

**Database is now the single source of truth**:
- All discovered devices are immediately stored in SQLite
- No in-memory global arrays (removed for consistency)
- Web UI queries `/devices/discovered` endpoint for real-time data
- Scanning logic writes directly to database via `UpsertDiscoveredPrinter()`

---

## Future Enhancements

- [ ] Add `/devices/restore` to restore hidden devices
- [ ] Add `/devices/history?serial=XXX` to query scan history
- [ ] Add configuration UI for retention policies
- [ ] Add `/devices/stats` for device statistics
- [ ] Add filtering/pagination to `/devices/list` and `/devices/discovered`
- [ ] Add WebSocket support for real-time updates
- [ ] Add job IDs and cancellation endpoints for scans
