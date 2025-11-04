# Logger Module Documentation

**Location**: `agent/logger/`

The logger module provides structured, level-based logging with multiple output targets including files, console, and Server-Sent Events (SSE) for real-time UI streaming.

## Architecture Overview

```
logger/
├── logger.go       # Core logger implementation
└── logger_test.go  # Comprehensive test suite (13 tests)
```

## Key Features

- **Structured Logging**: Key-value pairs for rich context
- **Log Levels**: ERROR, WARN, INFO, DEBUG, TRACE
- **Multiple Outputs**: File, console, SSE callback
- **Thread-Safe**: Safe for concurrent use
- **Rate Limiting**: Prevent log spam from noisy sources
- **Ring Buffer**: In-memory log storage (1000 entries)
- **Real-Time Streaming**: SSE integration for live UI updates
- **File Rotation**: Time and size-based rotation

## Log Levels

```go
const (
    LevelError LogLevel = 0  // Critical errors requiring attention
    LevelWarn  LogLevel = 1  // Warnings, degraded functionality
    LevelInfo  LogLevel = 2  // General informational messages
    LevelDebug LogLevel = 3  // Detailed debugging information
    LevelTrace LogLevel = 4  // Very verbose tracing (not yet used)
)
```

**Level Filtering**: Only logs at or above the configured level are output.

**Example**:
- Logger set to `INFO`: ERROR, WARN, INFO logged; DEBUG, TRACE dropped
- Logger set to `DEBUG`: ERROR, WARN, INFO, DEBUG logged; TRACE dropped

## Core Types

### Logger Struct

```go
type Logger struct {
    level         LogLevel
    file          *os.File
    mu            sync.RWMutex
    buffer        []LogEntry    // Ring buffer (last 1000 entries)
    bufferSize    int
    onLogCallback func(LogEntry) // SSE callback
    rateLimitMap  map[string]time.Time // Rate limiting state
}
```

### LogEntry Struct

```go
type LogEntry struct {
    Timestamp time.Time              `json:"timestamp"`
    Level     string                 `json:"level"`
    Message   string                 `json:"message"`
    Context   map[string]interface{} `json:"context,omitempty"`
}
```

## Creating a Logger

### Basic Logger (File Only)

```go
logger, err := logger.New("logs/app.log", logger.LevelInfo)
if err != nil {
    log.Fatal(err)
}
defer logger.Close()
```

### Logger with SSE Callback

```go
logger, _ := logger.New("logs/app.log", logger.LevelInfo)

// Set callback for real-time UI updates
logger.SetOnLogCallback(func(entry logger.LogEntry) {
    // Broadcast to SSE clients
    sseManager.Broadcast("log_entry", entry)
})
```

## Logging Methods

### Error

```go
logger.Error("Database connection failed", "error", err, "retries", 3)
```

**Output**:
```json
{
  "timestamp": "2025-11-02T14:30:45Z",
  "level": "ERROR",
  "message": "Database connection failed",
  "context": {
    "error": "connection refused",
    "retries": 3
  }
}
```

### Warn

```go
logger.Warn("SNMP timeout", "ip", "192.168.1.100", "timeout", "2s")
```

### Info

```go
logger.Info("Device discovered", "ip", "10.0.0.50", "vendor", "HP")
```

### Debug

```go
logger.Debug("SNMP PDU received", "oid", ".1.3.6.1.2.1.1.5.0", "value", "Printer-01")
```

### Trace

```go
logger.Trace("Entering function", "function", "QueryDevice", "ip", "10.0.0.1")
```

## Rate Limited Logging

**Purpose**: Prevent log spam from repetitive errors or warnings.

**Function**:
```go
logger.WarnRateLimited(key string, interval time.Duration, message string, keysAndValues ...interface{})
```

**Example**:
```go
// Only log this warning once per 5 minutes per IP
logger.WarnRateLimited(
    "snmp_timeout_"+ip,
    5*time.Minute,
    "SNMP query timeout",
    "ip", ip,
    "attempts", 3,
)
```

**Behavior**:
- First call: Logs immediately
- Subsequent calls within interval: Silently dropped
- After interval expires: Next call logs and resets timer

**Use Cases**:
- SNMP timeouts (same IP failing repeatedly)
- Discovery method failures (mDNS/SSDP errors)
- Network connectivity issues
- Parsing warnings for malformed data

## In-Memory Buffer

The logger maintains a ring buffer of recent log entries:

```go
entries := logger.GetRecentLogs()
for _, entry := range entries {
    fmt.Printf("[%s] %s: %s\n", entry.Level, entry.Timestamp, entry.Message)
}
```

**Buffer Size**: 1000 entries (configurable)

**Behavior**: When full, oldest entries are overwritten (FIFO)

**Use Cases**:
- UI log display (last N entries)
- Debugging recent events
- Crash dump inclusion

## SSE Integration

The logger supports real-time log streaming to the web UI via Server-Sent Events:

### Setup (in main.go)

```go
// Create logger
appLogger, _ := logger.New("logs/agent.log", logger.LevelInfo)

// Set SSE callback
appLogger.SetOnLogCallback(func(entry logger.LogEntry) {
    sseManager.Broadcast("log_entry", entry)
})
```

### UI Consumption (JavaScript)

```javascript
const eventSource = new EventSource('/sse');

eventSource.addEventListener('log_entry', (event) => {
    const entry = JSON.parse(event.data);
    console.log(`[${entry.level}] ${entry.message}`);
    
    // Display in UI
    appendToLogWindow(entry);
});
```

**Benefits**:
- No polling required (vs old 1-second interval)
- Instant log delivery to UI
- Efficient (only pushes when logs occur)
- Automatic reconnection on disconnect

## File Rotation

**Current Status**: ⏳ Planned (not yet implemented)

**Planned Features**:
- Size-based rotation (e.g., 10MB per file)
- Time-based rotation (daily, weekly)
- Backup retention (keep last N files)
- Compression of old logs (gzip)

**Configuration** (future):
```json
{
  "logging": {
    "rotation": {
      "max_size_mb": 10,
      "max_age_days": 30,
      "max_backups": 5,
      "compress": true
    }
  }
}
```

## Structured Context

All logging methods accept key-value pairs for structured context:

```go
logger.Info("Device discovered",
    "ip", "192.168.1.100",
    "vendor", "HP",
    "model", "LaserJet Pro M404n",
    "serial", "JPBHM12345",
    "page_count", 12543,
    "discovered_by", "mDNS",
)
```

**Output**:
```json
{
  "timestamp": "2025-11-02T14:30:45Z",
  "level": "INFO",
  "message": "Device discovered",
  "context": {
    "ip": "192.168.1.100",
    "vendor": "HP",
    "model": "LaserJet Pro M404n",
    "serial": "JPBHM12345",
    "page_count": 12543,
    "discovered_by": "mDNS"
  }
}
```

**Benefits**:
- Machine-parseable logs
- Easy filtering/searching
- Rich debugging context
- JSON export for log aggregators

## Thread Safety

The logger is fully thread-safe:

```go
// Safe to call from multiple goroutines
go logger.Info("Worker 1 log")
go logger.Info("Worker 2 log")
go logger.Info("Worker 3 log")
```

**Synchronization**: Uses `sync.RWMutex` for safe concurrent access

**Lock Behavior**:
- Write operations (logging): Acquires exclusive lock
- Read operations (GetRecentLogs): Acquires shared lock

## Testing

**Test File**: `logger_test.go`

**Test Coverage**: 13 tests, all passing

**Test Cases**:
- Basic logging at each level
- Level filtering (logs below threshold dropped)
- Structured context encoding
- Rate limiting behavior
- Buffer management (FIFO eviction)
- SSE callback invocation
- Thread safety (concurrent logging)

**Run Tests**:
```powershell
cd agent/logger
go test -v
```

**Run with Coverage**:
```powershell
go test -cover
```

## Usage Patterns

### Application Startup

```go
func main() {
    // Initialize logger
    appLogger, err := logger.New("logs/agent.log", logger.LevelInfo)
    if err != nil {
        log.Fatalf("Failed to create logger: %v", err)
    }
    defer appLogger.Close()
    
    appLogger.Info("Application started", "version", "1.0.0")
    
    // ... application logic ...
}
```

### Error Handling

```go
func queryDevice(ip string) error {
    pi, err := scanner.QueryDevice(ctx, ip, "public", 5)
    if err != nil {
        appLogger.Error("SNMP query failed",
            "ip", ip,
            "error", err,
            "function", "queryDevice",
        )
        return err
    }
    
    appLogger.Info("Device queried successfully",
        "ip", ip,
        "vendor", pi.Vendor,
        "model", pi.Model,
    )
    return nil
}
```

### Discovery Logging

```go
func discoverNetwork() {
    appLogger.Info("Starting network discovery", "range", "192.168.1.0/24")
    
    results, err := Discover(ctx, ranges, "full", config, db, 50, 10)
    if err != nil {
        appLogger.Error("Discovery failed", "error", err)
        return
    }
    
    appLogger.Info("Discovery complete",
        "devices_found", len(results),
        "duration", time.Since(start),
    )
}
```

### Debug Logging

```go
// Only logged if level >= DEBUG
logger.Debug("Parsing SNMP PDU",
    "oid", ".1.3.6.1.2.1.1.5.0",
    "type", "OctetString",
    "value", "Printer-01",
    "length", len(value),
)
```

## Performance Characteristics

### Timing

| Operation | Duration | Notes |
|-----------|----------|-------|
| Log write (file) | ~100μs | Buffered I/O |
| Log write (SSE) | ~50μs | In-memory callback |
| GetRecentLogs | ~10μs | Read from memory |
| Rate limit check | ~1μs | Map lookup |

### Memory Usage

| Component | Size | Notes |
|-----------|------|-------|
| Buffer (1000 entries) | ~100KB | Ring buffer |
| Rate limit map | ~1KB per key | Grows with unique keys |
| Logger struct | ~1KB | Fixed overhead |

## Configuration

Logger behavior is controlled via:

1. **Initialization**: `New(filepath, level)`
2. **Runtime**: `SetLevel(level)`, `SetOnLogCallback(callback)`
3. **Environment**: Future config file support

**Current Defaults**:
- Level: `INFO`
- Buffer: 1000 entries
- File: `logs/agent.log`
- Rotation: Not implemented (TODO)

## Integration Points

### With Main Application (`main.go`)

```go
// Create global logger
var appLogger *logger.Logger

func init() {
    appLogger, _ = logger.New("logs/agent.log", logger.LevelInfo)
}

// Use throughout application
appLogger.Info("Starting HTTP server", "port", 8080)
```

### With Agent Package (`agent/agent/`)

```go
// Agent functions use global logger
func Discover(...) {
    appLogger.Info("Discovery started", "ranges", len(ranges))
    // ... discovery logic ...
}
```

### With Scanner Package (`agent/scanner/`)

```go
// Scanner uses logger for SNMP operations
func QueryDevice(...) {
    appLogger.Debug("SNMP query", "ip", ip, "oids", len(oids))
    // ... query logic ...
}
```

## Migration from Old System

**Old System** (removed):
- Single global log buffer (`logBuffer`)
- Mutex-protected append (`logMutex`)
- 1-second polling via `/api/logs` endpoint
- `logMsg()` callback function passed to discovery methods

**New System** (current):
- Structured logger package (`logger.Logger`)
- SSE-based real-time streaming
- Level-based filtering
- Rate limiting support
- No callbacks needed (logger is global)

**Migration Steps** (completed):
1. ✅ Created logger package with SSE callback
2. ✅ Removed `logMsg()`, `logBuffer`, `logMutex` from main.go
3. ✅ Removed `logFn` parameters from discovery methods
4. ✅ Updated UI to use SSE instead of polling
5. ✅ Updated all log calls to use `appLogger.Info()` etc.

## Future Enhancements

### Planned Features
- [ ] Log rotation (size and time-based)
- [ ] Console output (stdout/stderr)
- [ ] JSON format output
- [ ] Log compression (gzip old files)
- [ ] External log forwarding (syslog, webhook)
- [ ] Dynamic level adjustment (runtime via API)
- [ ] Sampling (log 1% of high-frequency events)

### Configuration Improvements
- [ ] Load settings from config file
- [ ] Per-module log levels (e.g., scanner=DEBUG, agent=INFO)
- [ ] Custom log formatters
- [ ] Log filtering by context keys

## Troubleshooting

### Logs Not Appearing in UI
1. Check SSE connection: Open browser DevTools → Network → SSE
2. Verify callback set: `appLogger.SetOnLogCallback(...)` called
3. Check log level: UI may filter by level
4. Test with `appLogger.Info("test")` directly

### Log File Not Created
1. Check permissions: Ensure `logs/` directory writable
2. Check path: Verify absolute path or relative from binary location
3. Check disk space: Ensure sufficient space available
4. Review error from `logger.New()`: Should return error if failed

### Rate Limiting Not Working
1. Ensure unique keys per rate-limited log
2. Check interval: `5*time.Minute` = 300 seconds
3. Verify using `WarnRateLimited` not `Warn`

### High Memory Usage
1. Reduce buffer size (default 1000 entries)
2. Check for memory leaks in callback
3. Enable log rotation to prevent unbounded file growth
4. Clear rate limit map periodically (currently unbounded)

## Related Documentation

- [Agent Module](../agent/README.md) - Uses logger for discovery logging
- [Scanner Module](../scanner/README.md) - Uses logger for SNMP operations
- [API Reference](../../docs/API_REFERENCE.md) - SSE endpoint documentation
- [Settings TODO](../../docs/SETTINGS_TODO.md) - Future logging configuration options
