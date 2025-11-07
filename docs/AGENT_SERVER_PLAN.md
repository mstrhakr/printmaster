# Agent-Server Communication Implementation Plan

## Current State (v0.1.0)

**Server Side** ✅:
- SQLite schema with `agents`, `devices`, `metrics_history` tables
- API endpoints implemented:
  - `/api/v1/agents/register` - Agent registration
  - `/api/v1/agents/heartbeat` - Keep-alive
  - `/api/v1/devices/batch` - Device uploads
  - `/api/v1/metrics/batch` - Metrics uploads
- Basic logging with structured logger

**Agent Side** ❌:
- No server communication code yet
- Config system exists but no `[server]` section handling
- Agent runs standalone only

---

## Phase 1: Basic Agent→Server Communication (v0.2.0)

### Goal
Get agents uploading device/metrics data to server with basic error handling.

### Tasks

#### 1. Agent Configuration
**File**: `agent/config.ini.example`
```ini
[server]
enabled = false
url = http://localhost:9090
agent_id = agent-001
upload_interval = 60
heartbeat_interval = 30
```

#### 2. Agent Upload Client
**New File**: `agent/agent/server_client.go`
```go
type ServerClient struct {
    BaseURL string
    AgentID string
    HTTPClient *http.Client
}

func (c *ServerClient) Register() error
func (c *ServerClient) Heartbeat() error
func (c *ServerClient) UploadDevices(devices []Device) error
func (c *ServerClient) UploadMetrics(metrics []MetricsSnapshot) error
```

#### 3. Background Upload Worker
**Update**: `agent/main.go`
- Read `[server]` config
- Start goroutine for periodic uploads
- Upload devices on discovery complete
- Upload metrics on schedule

#### 4. Basic Error Handling
- Retry on network failure (exponential backoff)
- Log errors to agent logger
- Don't crash agent if server unavailable

---

## Phase 2: Enterprise Features (v0.3.0-v0.5.0)

### Audit Logging 🔍

**Why**: Enterprise customers need compliance tracking for:
- Who accessed what device
- Configuration changes
- Discovery scans initiated
- Data exports

**Implementation**:
1. **Server audit_log table**:
   ```sql
   CREATE TABLE audit_log (
       id INTEGER PRIMARY KEY,
       timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
       agent_id TEXT,
       user_id TEXT,  -- Future: when multi-user
       action TEXT,   -- "device_discovered", "scan_initiated", etc.
       resource_type TEXT,  -- "device", "agent", "config"
       resource_id TEXT,
       details TEXT,  -- JSON blob
       ip_address TEXT,
       INDEX idx_audit_timestamp (timestamp),
       INDEX idx_audit_agent (agent_id),
       INDEX idx_audit_action (action)
   );
   ```

2. **Audit middleware** (server):
   ```go
   func auditMiddleware(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           agentID := r.Header.Get("X-Agent-ID")
           // Log request
           defer logAuditEntry(agentID, r.Method, r.URL.Path)
           next.ServeHTTP(w, r)
       })
   }
   ```

3. **Agent sends audit events**:
   ```go
   client.LogAuditEvent("scan_initiated", "manual", map[string]interface{}{
       "range": "192.168.1.0/24",
       "method": "snmp",
   })
   ```

**Audit API**:
```http
POST /api/v1/audit/log
{
  "agent_id": "agent-001",
  "action": "device_discovered",
  "resource_type": "device",
  "resource_id": "ABC123",
  "details": {"manufacturer": "HP", "model": "LaserJet"}
}

GET /api/v1/audit?agent_id=agent-001&from=2025-11-01&to=2025-11-03
```

---

### Authentication & Authorization 🔒

**Why**: Enterprise needs:
- Agent authentication (verify agents are legitimate)
- User authentication (control who accesses server UI)
- Role-based access (admin vs read-only)

**Phase 2a: Agent Authentication**
```go
// Agent gets token on first register
POST /api/v1/agents/register
Response: {"agent_id": "...", "token": "eyJ..."}

// Agent stores token in database
agent_config.SetConfigValue("server_token", token)

// All future requests include token
req.Header.Set("Authorization", "Bearer " + token)
```

**Phase 2b: User Authentication (Future)**
- JWT tokens for web UI users
- Session management
- SSO integration (SAML/OAuth for enterprise)

---

### Agent Status Monitoring 📊

**Why**: Need to know if agents are healthy/offline

**Implementation**:
1. **Agent status tracking**:
   ```sql
   -- Already in schema, enhance:
   ALTER TABLE agents ADD COLUMN metrics_count INTEGER DEFAULT 0;
   ALTER TABLE agents ADD COLUMN devices_count INTEGER DEFAULT 0;
   ALTER TABLE agents ADD COLUMN last_error TEXT;
   ```

2. **Heartbeat enhancement**:
   ```go
   POST /api/v1/agents/heartbeat
   {
       "agent_id": "agent-001",
       "status": "active",
       "uptime_seconds": 86400,
       "devices_count": 25,
       "last_scan": "2025-11-03T18:00:00Z",
       "memory_mb": 120,
       "cpu_percent": 5.2
   }
   ```

3. **Server monitors**:
   - Agents not seen in N minutes marked "offline"
   - Alert if agent goes offline
   - Track agent health metrics

---

### Data Retention & Archival 📦

**Why**: Metrics grow forever, need cleanup strategy

**Implementation**:
1. **Retention policies**:
   ```sql
   CREATE TABLE retention_policies (
       id INTEGER PRIMARY KEY,
       metric_type TEXT,  -- "page_counts", "toner_levels", etc.
       keep_detailed_days INTEGER,  -- Keep all data
       keep_summary_days INTEGER,   -- Keep daily summaries
       delete_after_days INTEGER     -- Hard delete
   );
   ```

2. **Archival process**:
   - Daily rollup: Aggregate metrics to daily averages
   - Delete old detailed records
   - Optional: Export to S3/backup before delete

3. **Configuration**:
   ```ini
   [retention]
   metrics_detailed_days = 30
   metrics_summary_days = 365
   audit_log_days = 730  # 2 years for compliance
   ```

---

### Rate Limiting & Backpressure 🚦

**Why**: Prevent agents from overwhelming server

**Implementation**:
1. **Request limits**:
   ```go
   // Per-agent rate limiter
   rateLimiter := rate.NewLimiter(rate.Limit(10), 20)  // 10 req/sec, burst 20
   
   func rateLimitMiddleware(next http.Handler) http.Handler {
       return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           agentID := getAgentID(r)
           if !rateLimiter.Allow(agentID) {
               http.Error(w, "Rate limit exceeded", 429)
               return
           }
           next.ServeHTTP(w, r)
       })
   }
   ```

2. **Batch size limits**:
   ```go
   const MaxDevicesPerBatch = 1000
   const MaxMetricsPerBatch = 5000
   ```

3. **Agent backoff**:
   ```go
   // If server returns 429 or 503, agent backs off
   if resp.StatusCode == 429 {
       backoffDuration *= 2
       time.Sleep(backoffDuration)
   }
   ```

---

### Data Compression 📦

**Why**: Large device/metrics batches waste bandwidth

**Implementation**:
```go
// Agent compresses payload
buf := new(bytes.Buffer)
gzWriter := gzip.NewWriter(buf)
json.NewEncoder(gzWriter).Encode(devices)
gzWriter.Close()

req.Header.Set("Content-Encoding", "gzip")
req.Body = ioutil.NopCloser(buf)

// Server decompresses
if r.Header.Get("Content-Encoding") == "gzip" {
    reader, _ := gzip.NewReader(r.Body)
    defer reader.Close()
    json.NewDecoder(reader).Decode(&devices)
}
```

---

### Health & Readiness Endpoints 💚

**Why**: Kubernetes/load balancers need health checks

**Implementation**:
```go
// Server health endpoint
GET /healthz
Response: {"status": "ok", "database": "connected", "uptime": 3600}

// Readiness check
GET /readyz
Response: {"ready": true, "agents_connected": 5}

// Metrics (Prometheus format)
GET /metrics
Response: 
# HELP printmaster_agents_total Total registered agents
printmaster_agents_total 12
# HELP printmaster_devices_total Total devices across all agents
printmaster_devices_total 347
```

---

## Recommended Implementation Order

### Sprint 1 (v0.2.0): Basic Communication
- [ ] Agent config for server connection
- [ ] Server client in agent
- [ ] Upload devices/metrics
- [ ] Basic retry logic
- [ ] Test with 1 agent → 1 server

### Sprint 2 (v0.3.0): Reliability
- [ ] Audit logging (server side)
- [ ] Agent authentication (tokens)
- [ ] Enhanced heartbeat with metrics
- [ ] Server health endpoints
- [ ] Test with 5 agents → 1 server

### Sprint 3 (v0.4.0): Enterprise Prep
- [ ] Rate limiting
- [ ] Data compression
- [ ] Retention policies
- [ ] Batch size limits
- [ ] Test with 20+ agents

### Sprint 4 (v0.5.0): Monitoring
- [ ] Agent status dashboard (server UI)
- [ ] Offline detection
- [ ] Error tracking
- [ ] Performance metrics
- [ ] Load testing

---

## Start Here: Basic Communication (Next Steps)

1. **Add server config to agent**:
   ```bash
   # Update agent/config.ini.example
   # Add [server] section
   ```

2. **Create server client**:
   ```bash
   # New file: agent/agent/server_client.go
   ```

3. **Wire up in main.go**:
   ```go
   if serverConfig.Enabled {
       client := NewServerClient(serverConfig.URL, serverConfig.AgentID)
       go client.StartUploadWorker(ctx)
   }
   ```

4. **Test locally**:
   ```powershell
   # Terminal 1: Start server
   cd server
   .\printmaster-server.exe
   
   # Terminal 2: Start agent with server enabled
   cd agent
   # Edit config.ini: enabled = true
   .\printmaster-agent.exe
   
   # Check server logs for registration
   ```

---

## Questions for You

1. **Authentication timing**: Implement agent tokens now or wait until we have more agents?
   - **Recommendation**: Do it now, it's simple and critical for enterprise

2. **Audit logging scope**: Log everything or just critical actions?
   - **Recommendation**: Start with critical (discovery, config changes), expand later

3. **Data retention**: Implement now or later?
   - **Recommendation**: Later (v0.4.0), not critical for initial communication

4. **Compression**: Add now or optimize later?
   - **Recommendation**: Later, not needed until many devices per agent

**My suggestion**: Let's start with basic communication (Sprint 1), add auth immediately after (Sprint 2), then iterate based on real usage patterns.

Ready to start coding?

---

## Phase 4: Real-Time WebSocket Communication (Future Enhancement)

### Overview
Replace polling-based heartbeat with WebSocket connections for instant agent status updates and reduced server load.

### Architecture

#### **Hybrid Approach** (Recommended)
- **Primary**: WebSocket for real-time bidirectional communication
- **Fallback**: Keep existing HTTP heartbeat endpoints for compatibility

#### **Connection Patterns**
```
Agent → Server:  /ws/agent  (persistent connection, auth via Bearer token)
UI → Server:     /ws/ui     (persistent connection, broadcasts agent status changes)
```

### Scalability Considerations

**Performance Characteristics**:
- **Memory per connection**: ~2-8KB goroutine stack + ~few KB buffers
- **1000 agents**: ~2-8MB goroutines + ~5-10MB buffers = ~15MB total
- **10,000 agents**: ~150MB total (very manageable)
- **Bottleneck**: Network bandwidth and JSON marshaling, not connections

**Go's Advantages**:
- Non-blocking I/O with efficient goroutines
- Built-in concurrency primitives (channels, select)
- Excellent WebSocket library ecosystem (gorilla/websocket)

### Implementation Plan

#### Server-Side Components (~200 lines)

**1. Add Dependencies** (5 min)
```bash
cd server
go get github.com/gorilla/websocket
```

**2. WebSocket Hub** (~100 lines)
```go
// server/websocket/hub.go
type Hub struct {
    // Agent connections
    agents map[string]*AgentClient
    
    // UI connections
    uiClients map[*UIClient]bool
    
    // Channels for thread-safe operations
    registerAgent   chan *AgentClient
    unregisterAgent chan *AgentClient
    registerUI      chan *UIClient
    unregisterUI    chan *UIClient
    broadcast       chan AgentStatusUpdate
}

type AgentClient struct {
    ID         string
    Conn       *websocket.Conn
    Send       chan []byte
    LastSeen   time.Time
    Hub        *Hub
}

type UIClient struct {
    Conn *websocket.Conn
    Send chan []byte
    Hub  *Hub
}
```

**3. WebSocket Endpoints** (~60 lines)

```go
// server/main.go

// Agent WebSocket endpoint
func handleAgentWebSocket(w http.ResponseWriter, r *http.Request) {
    // Upgrade HTTP to WebSocket
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    // Authenticate via Bearer token
    agent := authenticateAgentConnection(r)
    if agent == nil {
        conn.Close()
        return
    }
    
    // Register agent connection
    client := &AgentClient{
        ID:   agent.AgentID,
        Conn: conn,
        Send: make(chan []byte, 256),
        Hub:  wsHub,
    }
    
    wsHub.registerAgent <- client
    
    // Start read/write pumps
    go client.readPump()
    go client.writePump()
}

// UI WebSocket endpoint
func handleUIWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    
    client := &UIClient{
        Conn: conn,
        Send: make(chan []byte, 256),
        Hub:  wsHub,
    }
    
    wsHub.registerUI <- client
    
    go client.readPump()
    go client.writePump()
}
```

**4. Connection Lifecycle** (~40 lines)
```go
// Agent disconnect = instant status update
func (c *AgentClient) readPump() {
    defer func() {
        c.Hub.unregisterAgent <- c
        c.Conn.Close()
        
        // Update DB: agent offline
        updateAgentStatus(c.ID, "offline")
        
        // Broadcast to UI
        c.Hub.broadcast <- AgentStatusUpdate{
            AgentID: c.ID,
            Status:  "offline",
            Time:    time.Now(),
        }
    }()
    
    // Read messages from agent
    for {
        var msg AgentMessage
        err := c.Conn.ReadJSON(&msg)
        if err != nil {
            break // Connection closed
        }
        
        // Handle heartbeat, metrics, etc.
        handleAgentMessage(c, msg)
    }
}
```

#### Client-Side Components (~100 lines)

**Agent WebSocket Client** (agent/agent/websocket_client.go)
```go
type WebSocketClient struct {
    URL      string
    AgentID  string
    Token    string
    conn     *websocket.Conn
    done     chan struct{}
}

func (c *WebSocketClient) Connect() error {
    headers := http.Header{}
    headers.Add("Authorization", "Bearer "+c.Token)
    
    conn, _, err := websocket.DefaultDialer.Dial(c.URL, headers)
    if err != nil {
        return err
    }
    
    c.conn = conn
    go c.readMessages()
    go c.sendHeartbeats()
    
    return nil
}

func (c *WebSocketClient) sendHeartbeats() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            msg := AgentMessage{
                Type:      "heartbeat",
                AgentID:   c.AgentID,
                Timestamp: time.Now(),
            }
            c.conn.WriteJSON(msg)
        case <-c.done:
            return
        }
    }
}

// Auto-reconnect on disconnect
func (c *WebSocketClient) MaintainConnection(ctx context.Context) {
    for {
        err := c.Connect()
        if err != nil {
            log.Printf("WebSocket connection failed: %v, retrying in 5s", err)
            time.Sleep(5 * time.Second)
            continue
        }
        
        // Wait for disconnect
        <-c.done
        
        // Retry connection
        time.Sleep(2 * time.Second)
    }
}
```

**UI WebSocket Client** (server/web/app.js)
```javascript
class AgentStatusWebSocket {
    constructor() {
        this.ws = null;
        this.reconnectDelay = 1000;
        this.maxReconnectDelay = 30000;
    }
    
    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const url = `${protocol}//${window.location.host}/ws/ui`;
        
        this.ws = new WebSocket(url);
        
        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.reconnectDelay = 1000; // Reset delay
        };
        
        this.ws.onmessage = (event) => {
            const update = JSON.parse(event.data);
            this.handleAgentUpdate(update);
        };
        
        this.ws.onclose = () => {
            console.log('WebSocket disconnected, reconnecting...');
            setTimeout(() => this.connect(), this.reconnectDelay);
            this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
        };
        
        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }
    
    handleAgentUpdate(update) {
        // Update agent card in real-time
        const card = document.querySelector(`[data-agent-id="${update.agent_id}"]`);
        if (!card) {
            // New agent - reload full list
            loadAgents();
            return;
        }
        
        // Update status indicator
        const statusEl = card.querySelector('.device-card-value[style*="color"]');
        if (statusEl) {
            const colors = {
                'active': 'var(--success)',
                'inactive': 'var(--muted)',
                'offline': 'var(--error)'
            };
            statusEl.style.color = colors[update.status] || 'var(--muted)';
            statusEl.textContent = `● ${update.status}`;
        }
        
        // Update last seen
        const lastSeenEl = card.querySelector('[title*="Last Seen"]');
        if (lastSeenEl) {
            lastSeenEl.textContent = 'Just now';
            lastSeenEl.title = new Date().toLocaleString();
        }
        
        // Flash animation for visual feedback
        card.style.animation = 'flash 0.3s ease';
        setTimeout(() => card.style.animation = '', 300);
    }
}

// Initialize on page load
const agentWS = new AgentStatusWebSocket();
agentWS.connect();
```

### Benefits

✅ **Instant Feedback**: Agent offline within 1-5 seconds (vs 60s polling)  
✅ **Reduced Load**: No constant polling from UI (60+ requests/min → 0)  
✅ **Lower Bandwidth**: Push updates only when state changes  
✅ **Better UX**: Live pulsing indicators, smooth transitions  
✅ **Scalable**: 1000 agents = ~15MB memory, well within server capacity  

### Fallback Strategy

**If WebSocket fails** (corporate firewall, proxy issues):
1. Agent tries WebSocket first
2. On failure, falls back to HTTP heartbeat (existing endpoint)
3. UI detects WebSocket failure, continues polling `/api/v1/agents/list`

**Detection**:
```go
// Agent side
err := wsClient.Connect()
if err != nil {
    log.Warn("WebSocket failed, using HTTP heartbeat")
    return NewHTTPHeartbeatClient(url, token)
}
```

### Security Considerations

1. **Authentication**: Bearer token in WebSocket upgrade request
2. **Rate Limiting**: Max 1 heartbeat per 10s per agent
3. **Timeout**: Auto-disconnect after 90s of silence
4. **Validation**: JSON schema validation on all messages

### Testing Plan

1. **Unit tests**: Hub registration/unregistration, broadcast
2. **Integration tests**: Agent connect/disconnect scenarios
3. **Load tests**: 1000 concurrent connections
4. **UI tests**: Status updates within 5s of agent disconnect

### Implementation Timeline

- **Server Hub & Endpoints**: 2 hours
- **Agent WebSocket Client**: 1 hour
- **UI WebSocket Client**: 1 hour
- **Testing & Debug**: 2 hours
- **Total**: ~6 hours (1 day)

### Migration Path

1. ✅ **v0.3.x**: Current state (HTTP polling)
2. 🔄 **v0.4.0**: Add WebSocket support (opt-in)
3. 🔄 **v0.5.0**: Make WebSocket default (HTTP fallback)
4. 🔄 **v1.0.0**: WebSocket only (remove HTTP heartbeat)

---

**Status**: 📋 Planned (not yet implemented)  
**Priority**: Medium (nice-to-have for v0.5.0, critical for v1.0)  
**Owner**: TBD
