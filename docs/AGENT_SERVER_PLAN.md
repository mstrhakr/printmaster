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
