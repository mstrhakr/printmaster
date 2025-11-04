# Storage Module Documentation

**Location**: `agent/storage/`

The storage module provides SQLite-based persistence for devices, metrics, configuration, and scan history. It uses a pure Go SQLite driver (modernc.org/sqlite) for cross-platform compatibility without CGO dependencies.

## Architecture Overview

```
storage/
├── sqlite.go           # Main SQLite store implementation
├── device.go           # Device data structures
├── interface.go        # Storage interface definitions
├── agent_config.go     # Configuration storage
├── migrations.go       # Schema migrations
├── convert.go          # Data type conversions
├── paths.go            # Database file path helpers
└── *_test.go           # Test files
```

## Core Components

### Device Store (`sqlite.go`, `interface.go`)

**Purpose**: CRUD operations for printer devices with history tracking.

**Interface**:
```go
type DeviceStore interface {
    Create(ctx, *Device) error
    Get(ctx, serial) (*Device, error)
    Update(ctx, *Device) error
    Upsert(ctx, *Device) error
    Delete(ctx, serial) error
    List(ctx, filter) ([]*Device, error)
    MarkSaved(ctx, serial) error
    MarkDiscovered(ctx, serial) error
    AddScanHistory(ctx, *ScanSnapshot) error
    GetScanHistory(ctx, serial, limit) ([]*ScanSnapshot, error)
    // ... more methods
}
```

**Key Features**:
- **Upsert**: Insert or update in single operation (handles duplicate scans)
- **Soft Delete**: Devices marked `visible=false` instead of hard delete
- **Field Locking**: Prevent auto-update of manually-edited fields
- **Scan History**: Track device changes over time

### Device Structure (`device.go`)

```go
type Device struct {
    Serial          string              // Primary key
    IP              string              
    Manufacturer    string
    Model           string
    Hostname        string
    Firmware        string
    MACAddress      string
    SubnetMask      string
    Gateway         string
    DNSServers      []string
    DHCPServer      string
    Consumables     []string            // Supply names (not levels)
    StatusMessages  []string
    LastSeen        time.Time
    CreatedAt       time.Time
    FirstSeen       time.Time
    IsSaved         bool                // User saved vs auto-discovered
    Visible         bool                // Soft delete flag
    DiscoveryMethod string
    AssetNumber     string              // User-defined asset tag
    Location        string              // Physical location
    Description     string              // Notes/UUID
    WebUIURL        string              // Device web interface
    LockedFields    []FieldLock         // Protected fields
    RawData         map[string]interface{} // Extended data
}
```

**Important Notes**:
- `PageCount` and `TonerLevels` removed from Device struct (moved to metrics history)
- Time-series data belongs in `metrics_history` table, not device record
- `Consumables` stores supply names only (e.g., "Black Toner", "Cyan Ink")

### Scan History (`interface.go`)

```go
type ScanSnapshot struct {
    ID              int64
    Serial          string
    CreatedAt       time.Time
    IP              string
    Hostname        string
    Firmware        string
    PageCount       int                 // Snapshot of page count at scan time
    TonerLevels     map[string]int      // Snapshot of toner at scan time
    Consumables     []string
    StatusMessages  []string
    DiscoveryMethod string
    WalkFilename    string
    RawData         json.RawMessage     // Full scan data
}
```

**Use Cases**:
- Track device changes over time
- Calculate page count deltas (usage between scans)
- Monitor supply level trends
- Audit trail for device modifications
- Rollback/compare historical states

### Agent Configuration (`agent_config.go`)

**Purpose**: Store agent settings separate from device data.

**Interface**:
```go
type AgentConfigStore interface {
    GetRanges() (string, error)
    SetRanges(text string) error
    GetRangesList() ([]string, error)
    SetConfigValue(key string, value interface{}) error
    GetConfigValue(key string, dest interface{}) error
}
```

**Stored Settings**:
- IP ranges for scanning
- SNMP community strings
- Discovery method toggles
- Performance settings (timeouts, concurrency)
- Integration credentials (webhooks, MQTT)

**Configuration Priority**:
1. Database settings (highest priority)
2. `config.json` file
3. Built-in defaults

## Database Schema

### Devices Table

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
    dns_servers TEXT,        -- JSON array
    dhcp_server TEXT,
    consumables TEXT,        -- JSON array (names only)
    status_messages TEXT,    -- JSON array
    last_seen DATETIME,
    created_at DATETIME,
    first_seen DATETIME,
    is_saved BOOLEAN DEFAULT 0,
    visible BOOLEAN DEFAULT 1,
    discovery_method TEXT,
    walk_filename TEXT,
    last_scan_id INTEGER,
    asset_number TEXT,
    location TEXT,
    description TEXT,
    web_ui_url TEXT,
    locked_fields TEXT,      -- JSON array of FieldLock
    raw_data TEXT            -- JSON object
);
```

### Scan History Table

```sql
CREATE TABLE scan_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    ip TEXT,
    hostname TEXT,
    firmware TEXT,
    page_count INTEGER,
    toner_levels TEXT,       -- JSON map
    consumables TEXT,        -- JSON array
    status_messages TEXT,    -- JSON array
    discovery_method TEXT,
    walk_filename TEXT,
    raw_data TEXT,           -- Full snapshot JSON
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);
```

### Metrics History Table

```sql
CREATE TABLE metrics_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    page_count INTEGER,
    color_page_count INTEGER,
    metrics_json TEXT,       -- All metrics as JSON
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);
```

### Agent Config Table

```sql
CREATE TABLE agent_config (
    key TEXT PRIMARY KEY,
    value TEXT,              -- JSON-encoded value
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## Key Operations

### Upsert Device

```go
device := &Device{
    Serial:       "JPBHM12345",
    IP:           "192.168.1.100",
    Manufacturer: "HP",
    Model:        "LaserJet Pro M404n",
    LastSeen:     time.Now(),
}

err := store.Upsert(ctx, device)
```

**Behavior**:
- If device exists: Updates fields, preserves `is_saved` status
- If new device: Inserts with `is_saved=false`, `visible=true`
- Respects field locks (doesn't overwrite locked fields)

### Field Locking

```go
// Lock hostname to prevent scanner overwriting manual edits
device.LockedFields = []FieldLock{
    {
        Field:    "hostname",
        Reason:   "manually_entered",
        LockedAt: time.Now(),
        LockedBy: "admin",
    },
}
store.Update(ctx, device)
```

**Locked Field Behavior**:
- Scanner upserts skip locked fields (preserve user values)
- Manual updates via API always succeed (override locks)
- Locks stored as JSON array in `locked_fields` column

### Filtering Devices

```go
// Get all saved devices seen in last 24 hours
saved := true
cutoff := time.Now().Add(-24 * time.Hour)

filter := DeviceFilter{
    IsSaved:       &saved,
    LastSeenAfter: &cutoff,
}

devices, err := store.List(ctx, filter)
```

### Scan History

```go
// Record scan snapshot
snapshot := &ScanSnapshot{
    Serial:     "JPBHM12345",
    IP:         "192.168.1.100",
    PageCount:  12543,
    TonerLevels: map[string]int{"Black": 85, "Cyan": 60},
    CreatedAt:  time.Now(),
}
store.AddScanHistory(ctx, snapshot)

// Retrieve last 10 scans for device
history, err := store.GetScanHistory(ctx, "JPBHM12345", 10)

// Calculate page usage between scans
if len(history) >= 2 {
    pagesUsed := history[0].PageCount - history[1].PageCount
    fmt.Printf("Printed %d pages since last scan\n", pagesUsed)
}
```

## Migrations (`migrations.go`)

**Purpose**: Automatic schema upgrades for existing databases.

**Migration Process**:
1. Check schema version in `schema_version` table
2. Run pending migrations in order
3. Update schema version

**Example Migration**:
```go
{
    Version: 2,
    Name:    "add_asset_fields",
    Up: func(db *sql.DB) error {
        _, err := db.Exec(`
            ALTER TABLE devices ADD COLUMN asset_number TEXT;
            ALTER TABLE devices ADD COLUMN location TEXT;
            ALTER TABLE devices ADD COLUMN description TEXT;
        `)
        return err
    },
}
```

**Current Schema Version**: Check `migrations.go` for latest version number

## Database Configuration

### SQLite Pragmas

```go
PRAGMA foreign_keys = ON;        // Enable FK constraints
PRAGMA journal_mode = WAL;        // Write-Ahead Logging for performance
PRAGMA synchronous = NORMAL;      // Balance safety/speed
PRAGMA cache_size = -64000;       // 64MB cache
```

**Benefits**:
- WAL mode: Concurrent reads during writes
- Foreign keys: Referential integrity (cascade deletes)
- Large cache: Faster queries on repeated access

### Database File Locations

```go
// Platform-specific paths
Windows: %APPDATA%\printmaster\devices.db
Linux:   ~/.local/share/printmaster/devices.db
macOS:   ~/Library/Application Support/printmaster/devices.db

// Or use custom path
store, _ := NewSQLiteStore("/path/to/custom.db")
```

## Performance Characteristics

### Query Performance

| Operation | Typical Time | Notes |
|-----------|-------------|-------|
| Upsert single device | 1-5ms | Includes index updates |
| Get by serial | <1ms | Indexed primary key |
| List all devices | 5-20ms | ~100 devices |
| Add scan history | 2-10ms | Includes FK check |
| Get last 10 scans | 2-5ms | Indexed by serial + created_at |

### Concurrency

- **WAL Mode**: Multiple readers + single writer concurrently
- **Thread Safety**: All methods use `context.Context` for cancellation
- **Connection Pooling**: SQLite driver handles connection reuse
- **Lock Behavior**: Writes acquire exclusive lock briefly (sub-millisecond)

### Scalability

- **Tested**: 10,000+ devices with sub-second queries
- **Bottlenecks**: Full table scans without filters
- **Optimization**: Ensure filters use indexed columns (serial, ip, is_saved)

## Testing

**Test Files**:
- `sqlite_test.go`: Device CRUD operations (20+ tests)
- `scan_history_test.go`: Scan history tracking
- `paths_test.go`: Cross-platform path resolution

**Run Tests**:
```powershell
cd agent/storage
go test -v
```

**Test Coverage**:
```powershell
go test -cover
```

## Error Handling

### Standard Errors

```go
var (
    ErrNotFound      = errors.New("device not found")
    ErrDuplicate     = errors.New("device already exists")
    ErrInvalidSerial = errors.New("invalid or empty serial")
)
```

**Usage**:
```go
device, err := store.Get(ctx, serial)
if errors.Is(err, storage.ErrNotFound) {
    // Handle missing device
}
```

### Database Errors

- **Constraint violations**: Wrapped with context (e.g., "UNIQUE constraint failed")
- **Connection errors**: Transient, retry recommended
- **Schema errors**: Fatal, requires migration or reset

## Integration Points

### With Scanner (`agent/scanner/`)

Scanner calls storage after device detection:

```go
pi := scanner.QueryDevice(ctx, ip, "public", 5)

device := &storage.Device{
    Serial:       pi.Serial,
    IP:           pi.IP,
    Manufacturer: pi.Vendor,
    Model:        pi.Model,
    // ... map fields ...
}

store.Upsert(ctx, device)
```

### With Agent (`agent/agent/`)

Agent discovery updates last seen times:

```go
// After mDNS/SSDP/WS-Discovery discovery
store.Upsert(ctx, &Device{
    Serial:          discoveredSerial,
    IP:              discoveredIP,
    DiscoveryMethod: "mdns",
    LastSeen:        time.Now(),
})
```

### With Main Application (`main.go`)

HTTP API handlers use storage for CRUD:

```go
// GET /api/devices
devices, _ := store.List(ctx, DeviceFilter{})

// DELETE /api/devices/{serial}
store.Delete(ctx, serial)

// POST /api/devices/{serial}/save
store.MarkSaved(ctx, serial)
```

## Future Enhancements

### Planned Features
- [ ] Backup/restore functionality
- [ ] Export to CSV/JSON
- [ ] Device groups/tags
- [ ] Custom fields (user-defined metadata)
- [ ] Audit logging (who changed what when)
- [ ] Device relationships (parent/child for managed print servers)
- [ ] Alerting thresholds (stored per-device)

### Performance Improvements
- [ ] Batch upsert for bulk imports
- [ ] Read replicas for reporting queries
- [ ] Query result caching (Redis/in-memory)
- [ ] Archival of old scan history (compress/move to cold storage)

## Troubleshooting

### Database Locked

**Symptom**: `database is locked` error during writes

**Causes**:
- Long-running transaction blocking writes
- WAL mode not enabled (check pragmas)
- External process accessing database

**Solutions**:
1. Ensure WAL mode: `PRAGMA journal_mode = WAL;`
2. Use shorter transactions
3. Close all external DB connections (DB Browser, etc.)

### Missing Devices After Scan

**Symptom**: Scanner finds devices but they don't appear in UI

**Checks**:
1. Check `visible=true` filter in query
2. Verify Upsert succeeded (check logs)
3. Look for constraint violations (duplicate serial with different IP)
4. Check `is_saved` filter (may be showing only saved devices)

### Slow Queries

**Symptom**: List operations taking >100ms

**Diagnostics**:
```sql
EXPLAIN QUERY PLAN SELECT * FROM devices WHERE manufacturer = 'HP';
```

**Solutions**:
1. Add indexes on frequently-filtered columns
2. Use specific filters (avoid full table scans)
3. Increase cache size: `PRAGMA cache_size = -128000;` (128MB)

### Schema Version Mismatch

**Symptom**: App crashes on startup with schema errors

**Cause**: Database from older version, migration failed

**Recovery**:
1. Backup existing database: `copy devices.db devices.db.bak`
2. Delete database (will recreate with current schema)
3. Re-scan network to repopulate
4. Or run migrations manually (see `migrations.go`)

## Related Documentation

- [Scanner Module](../scanner/README.md) - Generates device data to store
- [Agent Module](../agent/README.md) - Discovery triggers storage updates
- [API Reference](../../docs/API_REFERENCE.md) - HTTP endpoints using storage
- [Configuration](../../docs/CONFIGURATION.md) - Database path configuration
