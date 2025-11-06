# HTTP API and Web UI Summary

This document lists the key HTTP endpoints the agent exposes and the corresponding behaviour in the web UI.

## Core Endpoints

### UI & System
- `GET /` - Web UI served from the binary (single-page application)
- `GET /logs` - In-memory recent log lines (plain text). Optional query params:
  - `level` = ERROR | WARN | INFO | DEBUG | TRACE (filters to logs at or above severity)
  - `tail` = N (returns only the last N lines after filtering)
- `GET /logfile` - Full on-disk `logs/agent.log` file download

### Device Management

**Discovered Devices** (not saved, visible only):
- `GET /devices/discovered` - List discovered devices from database (is_saved=false, visible=true)
- `POST /devices/clear_discovered` - Hide discovered devices (soft delete, sets visible=false)

**Saved Devices**:
- `GET /devices/list` - List all saved devices from database (is_saved=true)
- `GET /devices/get?serial=XXX` - Get single device by serial number
- `POST /devices/merge` - Save/merge device (accepts `{walk: "filename"}` or `{ip: "10.0.0.1"}`)
- `POST /devices/delete` - Delete device by serial (accepts `{serial: "XXX"}`)

### Network Scanning
- `POST /discover` - Start network discovery (saved ranges and/or local subnet)
  - Scans saved IP ranges when `manual_ranges` enabled in settings
  - Auto-discovers local subnet when `subnet_scan` enabled in settings
  - Respects discovery method toggles (ARP, TCP, SNMP, mDNS)

## Data Storage

### Database (SQLite)
- **Location**: 
  - Windows: `%LOCALAPPDATA%\PrintMaster\devices.db`
  - macOS: `~/Library/Application Support/PrintMaster/devices.db`
  - Linux: `~/.local/share/printmaster/devices.db`

- **Tables**:
  - `devices` - Main device records with is_saved and visible flags
  - `scan_history` - Time-series snapshots of each scan (page counts, toner levels, etc.)
  - `schema_version` - Migration tracking

### Device Lifecycle
1. **Discovery**: Device scanned → added to database with `is_saved=false, visible=true`
2. **Background Sync**: Every 10 seconds, in-memory devices → database + scan history snapshot
3. **Save**: User clicks save → `is_saved=true` (via `/devices/merge`)
4. **Clear**: User clears discovered → `visible=false` (soft delete, data preserved)
5. **Delete**: User deletes saved device → removed from database entirely

### Scan History
- Every background sync creates a `scan_history` entry
- Tracks changes over time: toner levels, page counts, firmware updates
- Configurable retention period (default: 30 days)
- Garbage collection removes old scans and hidden devices automatically

## UI Behavior

### Range Editor
- Textarea shows current planned IP ranges
- Radio buttons: **Add** (prepend to saved ranges) or **Override** (replace saved ranges)
- "Apply Discovered" button merges IPs from discovered devices into ranges

### Scanning
- **"Discover Now"**: Triggers network discovery using current settings
  - Scans saved IP ranges if `manual_ranges` enabled
  - Scans local subnet if `subnet_scan` enabled
  - Both sources can be used simultaneously
- Discovery settings control which methods are used (ARP, TCP, SNMP, mDNS)

### Device Management
- **Discovered Devices Tab**: Shows devices with `is_saved=false, visible=true`
- **Saved Devices Tab**: Shows devices with `is_saved=true`
- **Save Button**: Converts discovered → saved (`is_saved=true`)
- **Clear Button**: Hides discovered devices (`visible=false`, preserves data)
- **Delete Button**: Permanently removes saved device from database

### Autosave Feature
- Checkbox enables automatic saving of newly discovered devices
- Tracks IPs already auto-saved to prevent duplicates
- Runs every 5 seconds when enabled
- Requires device to have serial number to save

## Log Viewer
- Auto-scrolls and polls `/logfile` periodically
- Recent lines available from `/logs` endpoint; can filter by level and tail
- Shows discovery events, errors, and system messages

## Asynchronous Operation
- All scans return immediately (non-blocking)
- Use `/scan_status` endpoint to poll progress
- Background sync runs every 10 seconds
- Garbage collection runs periodically (configurable interval)

## Notes
- **Soft Delete**: "Clear" hides devices but preserves all data and scan history
- **Hard Delete**: "Delete" removes device and all associated scan history (CASCADE)
- **Thread-Safe**: SQLite with WAL mode handles concurrent access
- **Cross-Platform**: Pure Go SQLite driver (no CGO), works on Windows/Mac/Linux
- **Migration Ready**: Legacy JSON file support for backward compatibility during transition
