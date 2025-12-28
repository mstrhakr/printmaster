# Agent Upload Architecture

## Overview

This document describes the architecture for agent-to-server communication in PrintMaster. Agents discover printers locally and upload data to a central server for multi-site management.

## Design Goals

1. **Zero disk footprint** during normal operation (memory-only mode)
2. **Graceful degradation** during network outages (overflow to disk)
3. **Minimal complexity** - leverage existing storage layer
4. **Data loss acceptable** on agent restart (server is source of truth)
5. **Bandwidth efficient** - avoid re-uploading unchanged data

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                       Agent Process                          │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Discovery Workers                                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                  │
│  │  SNMP    │  │  mDNS    │  │  SSDP    │                  │
│  │  Scanner │  │  Scanner │  │  Scanner │                  │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘                  │
│        │             │              │                        │
│        └─────────────┼──────────────┘                        │
│                      ↓                                       │
│              ┌───────────────┐                               │
│              │  deviceStore  │  (MEMORY or DISK)            │
│              │   (SQLite)    │                               │
│              └───────┬───────┘                               │
│                      │                                       │
│                      ↓                                       │
│              ┌───────────────┐                               │
│              │ Upload Worker │ ← Reads from deviceStore     │
│              │  (goroutine)  │                               │
│              └───────┬───────┘                               │
│                      │                                       │
│                      ↓                                       │
│              ┌───────────────┐                               │
│              │ ServerClient  │ ← HTTP client with retry     │
│              └───────┬───────┘                               │
│                      │                                       │
└──────────────────────┼───────────────────────────────────────┘
                       │
                       ↓ HTTPS + Bearer Token
              ┌────────────────┐
              │ PrintMaster    │
              │ Server         │
              └────────────────┘
```

## Storage Strategy

### Current State

The agent uses two SQLite databases:
- `devices.db` (deviceStore) - Stores discovered devices, scan history, metrics
- `agent.db` (agentConfigStore) - Stores agent settings, credentials, trace tags

Both already support `:memory:` mode for zero-disk operation.

### Phase 1 (v0.2.0 - Stability First)

**Strategy:** Use existing SQLite with file path
- Upload worker reads from DB
- No complex overflow logic yet
- Focus: Get agent-server communication working reliably

**Rationale:**
- Proven reliability
- Handles network outages gracefully (queue persists)
- Simpler debugging during development
- Can track upload history

### Phase 2 (v0.3.0 - Optimize for Production)

**Strategy:** Memory-first with intelligent overflow

**Normal Operation (99% of time):**
- SQLite in `:memory:` mode
- Zero disk footprint
- Ultra-fast operations

**Degraded Mode (network outage):**
Overflow to disk when:
- Memory usage > 50MB threshold
- Network down > 10 minutes
- Queue size > 1000 devices

**Implementation:**
```go
type StorageMode int

const (
    MemoryOnly StorageMode = iota
    DiskOverflow
    DiskPersistent
)

func NewAdaptiveStore(mode StorageMode) (*SQLiteStore, error) {
    switch mode {
    case MemoryOnly:
        return NewSQLiteStore(":memory:")
    case DiskOverflow:
        // Start in memory, monitor pressure
        store := NewSQLiteStore(":memory:")
        go monitorMemoryPressure(store)
        return store
    case DiskPersistent:
        return NewSQLiteStore(defaultPath)
    }
}

func monitorMemoryPressure(store *SQLiteStore) {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        stats := store.Stats()
        if stats.DeviceCount > 1000 || stats.MemoryUsage > 50*1024*1024 {
            store.OverflowToDisk()
        }
    }
}
```

## Upload Worker Design

### Component Structure

```go
type UploadWorker struct {
    client            *agent.ServerClient
    store             storage.DeviceStore
    config            *ServerConfig
    stopCh            chan struct{}
    
    // State tracking
    mu                sync.RWMutex
    lastHeartbeat     time.Time
    lastDeviceUpload  time.Time
    lastMetricsUpload time.Time
    uploadedDevices   map[string]time.Time // Track last upload time per device
}

type ServerConfig struct {
    Enabled            bool
    URL                string
    AgentID            string
    Token              string
    HeartbeatInterval  time.Duration
    UploadInterval     time.Duration
    RetryAttempts      int
    RetryBackoff       time.Duration
}
```

### Workflow

#### 1. Registration (Once on Startup)

```go
func (w *UploadWorker) ensureRegistered(ctx context.Context) error {
    if w.config.Token != "" {
        if err := w.client.Heartbeat(ctx); err == nil {
            return nil
        }
        // Token invalid, request a new one using the stored join token
        w.config.Token = ""
    }

    joinToken := loadJoinTokenFromDisk()
    if joinToken == "" {
        return fmt.Errorf("join token missing; agent must be re-onboarded")
    }

    token, _, err := w.client.RegisterWithToken(ctx, joinToken, Version)
    if err != nil {
        return fmt.Errorf("register-with-token failed: %w", err)
    }

    w.config.Token = token
    w.client.SetToken(token)
    persistToken(token)
    return nil
}
```

#### 2. Heartbeat Loop (Every 60 seconds)

```go
func (w *UploadWorker) heartbeatLoop() {
    ticker := time.NewTicker(w.config.HeartbeatInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
            err := w.uploadWithRetry(func() error {
                return w.client.Heartbeat(ctx)
            })
            cancel()
            
            if err != nil {
                log.Warn("Heartbeat failed after retries", "error", err)
                // Don't exit - keep trying on next tick
            } else {
                w.mu.Lock()
                w.lastHeartbeat = time.Now()
                w.mu.Unlock()
            }
            
        case <-w.stopCh:
            return
        }
    }
}
```

#### 3. Upload Loop (Every 5 minutes)

```go
func (w *UploadWorker) uploadLoop() {
    ticker := time.NewTicker(w.config.UploadInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            w.uploadDevices()
            w.uploadMetrics()
            
        case <-w.stopCh:
            return
        }
    }
}

func (w *UploadWorker) uploadDevices() {
    ctx := context.Background()
    
    // Get all visible devices from store
    filter := storage.DeviceFilter{
        Visible: boolPtr(true),
    }
    devices, err := w.store.List(ctx, filter)
    if err != nil {
        log.Error("Failed to list devices", "error", err)
        return
    }
    
    if len(devices) == 0 {
        return // Nothing to upload
    }
    
    // Filter: only upload if changed since last upload (v0.3.0 optimization)
    // For v0.2.0: upload all devices every interval (simpler)
    
    // Convert to upload format
    deviceMaps := make([]map[string]interface{}, len(devices))
    for i, dev := range devices {
        deviceMaps[i] = deviceToMap(dev)
    }
    
    // Upload with retry
    err = w.uploadWithRetry(func() error {
        return w.client.UploadDevices(ctx, deviceMaps)
    })
    
    if err != nil {
        log.Error("Failed to upload devices", "count", len(devices), "error", err)
    } else {
        log.Info("Uploaded devices", "count", len(devices))
        w.mu.Lock()
        w.lastDeviceUpload = time.Now()
        w.mu.Unlock()
    }
}

func (w *UploadWorker) uploadMetrics() {
    ctx := context.Background()
    
    // Get latest metrics for all devices
    // For v0.2.0: upload all latest metrics
    // For v0.3.0: only upload metrics changed since last upload
    
    filter := storage.DeviceFilter{Visible: boolPtr(true)}
    devices, _ := w.store.List(ctx, filter)
    
    var metricMaps []map[string]interface{}
    for _, dev := range devices {
        metrics, err := w.store.GetLatestMetrics(ctx, dev.Serial)
        if err != nil || metrics == nil {
            continue
        }
        metricMaps = append(metricMaps, metricsToMap(metrics))
    }
    
    if len(metricMaps) == 0 {
        return
    }
    
    err := w.uploadWithRetry(func() error {
        return w.client.UploadMetrics(ctx, metricMaps)
    })
    
    if err != nil {
        log.Error("Failed to upload metrics", "count", len(metricMaps), "error", err)
    } else {
        log.Info("Uploaded metrics", "count", len(metricMaps))
        w.mu.Lock()
        w.lastMetricsUpload = time.Now()
        w.mu.Unlock()
    }
}
```

#### 4. Retry Logic with Exponential Backoff

```go
func (w *UploadWorker) uploadWithRetry(fn func() error) error {
    var lastErr error
    
    for attempt := 0; attempt < w.config.RetryAttempts; attempt++ {
        err := fn()
        if err == nil {
            return nil // Success
        }
        
        lastErr = err
        
        // Don't retry on auth errors (token invalid)
        if isAuthError(err) {
            log.Warn("Authentication failed, attempting re-registration")
            if regErr := w.ensureRegistered(context.Background()); regErr != nil {
                return fmt.Errorf("re-registration failed: %w", regErr)
            }
            // Try one more time with new token
            if err := fn(); err == nil {
                return nil
            }
            return err
        }
        
        // Exponential backoff: 1s, 2s, 4s, 8s, 16s
        backoff := w.config.RetryBackoff * time.Duration(1<<attempt)
        log.Debug("Upload failed, retrying", "attempt", attempt+1, "backoff", backoff, "error", err)
        time.Sleep(backoff)
    }
    
    return fmt.Errorf("upload failed after %d attempts: %w", w.config.RetryAttempts, lastErr)
}

func isAuthError(err error) bool {
    // Check if error is 401 Unauthorized
    return err != nil && strings.Contains(err.Error(), "401")
}
```

## Data Flow

### Normal Operation
```
Discovery → SQLite (disk or memory) → Upload Worker → Server
                                           ↓
                                      Track uploaded
                                           ↓
                                   Server responds 200 OK
```

### Network Outage
```
Discovery → SQLite (disk) → Upload Worker (fails)
                                     ↓
                                 Queue builds up
                                     ↓
                              Retry with backoff
                                     ↓
                              (network restored)
                                     ↓
                                 Drain queue
```

### Agent Restart
```
SQLite on disk:
  1. Load token from agentConfigStore
  2. Validate token with heartbeat
  3. Drain any pending uploads
  4. Continue normal operation

SQLite in memory (v0.3.0):
  1. Load token from agentConfigStore
  2. Validate token (or re-register)
  3. Start fresh (acceptable - server has history)
```

## Configuration

### config.ini Example

```ini
[server]
# Enable server integration
server_enabled = true

# Server URL (HTTPS recommended)
server_url = https://printmaster.company.com:9090

# Unique agent identifier (auto-generated if not specified)
agent_id = agent-site-01

# Upload interval in seconds (default: 300 = 5 minutes)
server_upload_interval = 300

# Heartbeat interval in seconds (default: 60 = 1 minute)
server_heartbeat_interval = 60

# Optional: Advanced settings
# retry_attempts = 5              # Number of retry attempts (default: 5)
# retry_backoff = 1               # Initial backoff in seconds (default: 1s)
# memory_only = false             # Force memory-only mode (v0.3.0)
# max_memory_devices = 1000       # Overflow threshold (v0.3.0)
```

### Environment Variables (Optional)

For containerized deployments:
```bash
PRINTMASTER_SERVER_URL=https://server.company.com:9090
PRINTMASTER_AGENT_ID=agent-01
PRINTMASTER_SERVER_ENABLED=true
```

## Server-Side Handling

The server provides these endpoints:

1. **POST /api/v1/agents/register** - Register agent, receive token
2. **POST /api/v1/agents/heartbeat** - Periodic heartbeat (requires auth)
3. **POST /api/v1/devices/batch** - Batch device upload (requires auth)
4. **POST /api/v1/metrics/batch** - Batch metrics upload (requires auth)

All endpoints (except register) require Bearer token authentication:
```
Authorization: Bearer <token>
```

Server implements:
- Token-based authentication
- Upsert logic (handles duplicates gracefully)
- Audit logging for all agent operations
- Device and metrics storage with foreign key constraints

## Implementation Phases

### Phase 1 - v0.2.0 (This Week)

**Goal:** Basic agent-server communication working reliably

- [x] Server token generation
- [x] Server audit logging
- [ ] Implement UploadWorker in agent/main.go
- [ ] Wire up to existing deviceStore (file-based)
- [ ] Basic upload logic (upload all devices/metrics every interval)
- [ ] End-to-end testing
- [ ] Documentation

**Success Criteria:**
- Agent registers with server and receives token
- Agent sends heartbeat every 60 seconds
- Agent uploads devices every 5 minutes
- Agent uploads metrics every 5 minutes
- Server validates tokens correctly
- Server stores uploaded data in database
- Agent handles network failures gracefully (retry logic)
- Audit log shows all agent operations

### Phase 2 - v0.3.0 (With USB Support)

**Goal:** Production-ready optimization

- [ ] Switch to `:memory:` by default
- [ ] Add overflow-to-disk logic
- [ ] Delta tracking (only upload changed devices)
- [ ] Memory pressure monitoring
- [ ] Advanced retry with circuit breaker pattern
- [ ] Metrics dashboard showing upload stats
- [ ] Agent status endpoint showing upload health

**Optimizations:**
- Track `last_uploaded_at` per device
- Only upload devices that changed since last upload
- Compress upload payloads for large batches
- Implement circuit breaker (stop trying if server persistently down)
- Add upload queue metrics to agent UI

## Security Considerations

1. **Token Security:**
   - Tokens are 256-bit random values (base64-encoded)
   - Stored encrypted in agent.db
   - Transmitted only over HTTPS
   - Server validates token on every request

2. **Network Security:**
   - HTTPS required for production
   - Certificate validation enabled
   - No sensitive data in logs (tokens truncated)

3. **Agent Identity:**
   - Each agent has unique ID
   - Server tracks agent version, platform, IP
   - Audit log provides full operation history

## Monitoring and Debugging

### Agent Logs
```
INFO  Agent registered successfully agent_id=agent-01 token=AbCdEfGh...
DEBUG Heartbeat sent successfully
INFO  Uploaded devices count=42
WARN  Upload failed, retrying attempt=1 backoff=2s error="connection timeout"
ERROR Failed to upload devices after 5 attempts
```

### Server Logs
```
INFO  Agent registering agent_id=agent-01 version=v0.2.0 host=site-01-pc
INFO  Agent registered successfully agent_id=agent-01 token=AbCdEfGh...
DEBUG Heartbeat received agent_id=agent-01 status=active
INFO  Devices batch received agent_id=agent-01 count=42
INFO  Devices stored agent_id=agent-01 stored=42 total=42
```

### Audit Log (Server Database)
```sql
SELECT * FROM audit_log WHERE agent_id = 'agent-01' ORDER BY timestamp DESC LIMIT 10;

| timestamp           | agent_id  | action          | details                    | ip_address    |
|---------------------|-----------|-----------------|----------------------------|---------------|
| 2025-11-03 14:32:15 | agent-01  | upload_devices  | Uploaded 42 devices        | 192.168.1.100 |
| 2025-11-03 14:31:45 | agent-01  | heartbeat       | Status: active             | 192.168.1.100 |
| 2025-11-03 14:27:12 | agent-01  | upload_metrics  | Uploaded 42 snapshots      | 192.168.1.100 |
| 2025-11-03 14:26:48 | agent-01  | register        | Agent registered: v0.2.0   | 192.168.1.100 |
```

## Troubleshooting

### Agent Won't Connect to Server

**Check:**
1. Is `server_enabled = true` in config.ini?
2. Is `server_url` correct and reachable?
3. Is server running and accessible?
4. Check agent logs for error messages
5. Test connectivity: `curl -k https://server.company.com:9090/health`

### Token Authentication Failures

**Check:**
1. Agent logs show "Invalid token" → Delete agent.db, restart agent
2. Server logs show "Invalid token" → Check server database for agent record
3. Token might have expired → Agent will auto re-register

### Uploads Failing

**Check:**
1. Network connectivity to server
2. Agent logs show retry attempts
3. Server logs show what error occurred
4. Check server database to see if partial data arrived

### Agent Using Too Much Memory (v0.3.0)

**Check:**
1. How many devices discovered? (should handle 1000+ in memory)
2. Is overflow-to-disk triggering? (check logs)
3. Consider lowering `max_memory_devices` threshold

## Future Enhancements (Post-v0.3.0)

- **Compression:** Gzip upload payloads for large batches
- **Streaming:** Stream large device lists instead of batching
- **Webhooks:** Server can push alerts back to agents
- **Agent groups:** Organize agents by site, customer, region
- **Centralized config:** Server can push config updates to agents
- **Remote commands:** Server can trigger discovery, restart, etc.

---

**Document Version:** 1.0
**Last Updated:** 2025-11-03
**Status:** Design Complete - Implementation In Progress (v0.2.0)
