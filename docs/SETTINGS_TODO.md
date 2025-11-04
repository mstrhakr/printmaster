# Settings Implementation TODO

This document tracks all settings marked as TODO in the UI but not yet fully wired to backend functionality.

## ✅ Completed Settings

### SNMP Settings
- **SNMP Timeout (ms)**: ✅ Wired to scanner - controls timeout for SNMP queries in LiveDiscoveryDetect
- **SNMP Retries**: ✅ Wired to scanner - controls retry count for SNMP operations
- **Discover Concurrency**: ✅ Wired to scanner - controls concurrent goroutines during discovery

## 🔧 SNMP Settings (TODO)

### Enable SNMP Bulk GET
**Current**: UI toggle exists, stored in database  
**TODO**: Wire to scanner SNMP implementation  
**Impact**: Would improve performance by fetching multiple OIDs in single request  
**Location**: `agent/scanner/snmp.go` - modify query methods  
**Complexity**: Medium - requires SNMP library support check

### SNMPv3 Support
**Current**: UI has fields for username, auth protocol, auth password, priv protocol, priv password  
**TODO**: Implement SNMPv3 configuration in scanner  
**Impact**: Critical for secure environments requiring encrypted SNMP  
**Location**: `agent/scanner/snmp.go` - add v3 credentials to SNMP client  
**Complexity**: High - requires full SNMPv3 implementation with authentication/encryption

### SNMP Port
**Current**: UI input field, defaults to 161  
**TODO**: Wire to scanner SNMP client configuration  
**Impact**: Allows non-standard SNMP ports  
**Location**: `agent/scanner/snmp.go` - pass port to SNMP connection  
**Complexity**: Low - simple parameter addition

### SNMP Delay Between Queries (ms)
**Current**: UI input field  
**TODO**: Add rate limiting between SNMP queries  
**Impact**: Prevents overwhelming devices with rapid queries  
**Location**: `agent/scanner/snmp.go` - add delay logic in query loops  
**Complexity**: Low - add `time.Sleep()` between queries

### Enable SNMP Result Cache
**Current**: UI toggle exists  
**TODO**: Implement caching layer for SNMP query results  
**Impact**: Reduces redundant queries, improves performance  
**Location**: New cache module or in `agent/scanner/snmp.go`  
**Complexity**: Medium - requires cache invalidation strategy

### SNMP Cache TTL (seconds)
**Current**: UI input field  
**TODO**: Implement cache expiration logic  
**Impact**: Controls freshness of cached SNMP data  
**Dependency**: Requires SNMP cache implementation first  
**Complexity**: Low - once cache exists, add TTL logic

## 🪵 Logging Settings (TODO)

### Log Rotation Size (MB)
**Current**: UI input field, defaults to 10MB  
**TODO**: Wire to logger file rotation  
**Impact**: Prevents log files from growing unbounded  
**Location**: `agent/logger/logger.go` - implement size-based rotation  
**Complexity**: Medium - requires file size monitoring and rotation logic

### Log Backup Count
**Current**: UI input field, defaults to 5  
**TODO**: Implement log file rotation with backup retention  
**Impact**: Controls disk space usage from old logs  
**Location**: `agent/logger/logger.go` - manage backup files  
**Complexity**: Medium - requires file management logic

### Enable Console Logging
**Current**: UI toggle exists  
**TODO**: Add console output to logger (currently file + SSE only)  
**Impact**: Useful for development/debugging  
**Location**: `agent/logger/logger.go` - add stdout writer  
**Complexity**: Low - add additional io.Writer

### Enable JSON Log Format
**Current**: UI toggle exists  
**TODO**: Implement JSON formatter for log entries  
**Impact**: Better for log aggregation tools (ELK, Splunk)  
**Location**: `agent/logger/logger.go` - add JSON encoder  
**Complexity**: Low - use `encoding/json` for structured logs

### Enable Log Compression
**Current**: UI toggle exists  
**TODO**: Compress rotated log files  
**Impact**: Saves disk space on historical logs  
**Location**: `agent/logger/logger.go` - gzip old log files after rotation  
**Complexity**: Low - use `compress/gzip` on rotation

## ⚡ Performance Settings (TODO)

### Ping Timeout (seconds)
**Current**: UI input field, defaults to 1 second  
**TODO**: Wire to ICMP ping operations  
**Impact**: Controls how long to wait for ping responses  
**Location**: `agent/agent/detect.go` - update ping timeout  
**Complexity**: Low - pass to ping function

### Port Probe Timeout (ms)
**Current**: UI input field, defaults to 500ms  
**TODO**: Wire to TCP port probe operations  
**Impact**: Controls TCP connection attempt timeout  
**Location**: `agent/agent/detect.go` - update TCP dialer timeout  
**Complexity**: Low - configure `net.DialTimeout`

### Max Concurrent DB Connections
**Current**: UI input field, defaults to 25  
**TODO**: Configure SQLite connection pool  
**Impact**: Controls database concurrency  
**Location**: `agent/storage/sqlite.go` - call `db.SetMaxOpenConns()`  
**Complexity**: Low - one function call

### Device Cache Size (entries)
**Current**: UI input field, defaults to 1000  
**TODO**: Implement device info cache  
**Impact**: Reduces database queries for frequently accessed devices  
**Location**: New cache module  
**Complexity**: Medium - requires LRU cache implementation

### Device Cache TTL (seconds)
**Current**: UI input field, defaults to 300 (5 min)  
**TODO**: Implement cache expiration  
**Impact**: Controls freshness of cached device data  
**Dependency**: Requires device cache implementation first  
**Complexity**: Low - once cache exists, add TTL

### Enable Rate Limiting
**Current**: UI toggle exists  
**TODO**: Implement API rate limiting  
**Impact**: Prevents API abuse  
**Location**: `agent/main.go` - add middleware for HTTP endpoints  
**Complexity**: Medium - requires rate limiter implementation

### Rate Limit (requests/min)
**Current**: UI input field, defaults to 100  
**TODO**: Configure rate limiter threshold  
**Impact**: Controls API request rate  
**Dependency**: Requires rate limiting implementation first  
**Complexity**: Low - configuration parameter

## 🔌 Integration Settings (TODO)

### Enable Webhook Notifications
**Current**: UI toggle exists  
**TODO**: Implement webhook sending on device discovery  
**Impact**: Allows integration with external systems  
**Location**: New webhook module in `agent/`  
**Complexity**: Medium - HTTP client with retry logic

### Webhook URL
**Current**: UI input field  
**TODO**: Target endpoint for webhook notifications  
**Impact**: Where to send discovery events  
**Dependency**: Requires webhook implementation  
**Complexity**: Low - configuration parameter

### Enable Syslog Forwarding
**Current**: UI toggle exists  
**TODO**: Forward logs to syslog server  
**Impact**: Centralized logging integration  
**Location**: `agent/logger/logger.go` - add syslog writer  
**Complexity**: Medium - requires syslog protocol implementation

### Syslog Server
**Current**: UI input field (host:port)  
**TODO**: Syslog destination configuration  
**Impact**: Where to forward logs  
**Dependency**: Requires syslog implementation  
**Complexity**: Low - configuration parameter

### Enable MQTT Publishing
**Current**: UI toggle exists  
**TODO**: Publish device events to MQTT broker  
**Impact**: IoT integration capabilities  
**Location**: New MQTT module  
**Complexity**: High - requires MQTT client library and message design

### MQTT Broker URL
**Current**: UI input field  
**TODO**: MQTT broker connection string  
**Impact**: Where to publish events  
**Dependency**: Requires MQTT implementation  
**Complexity**: Low - configuration parameter

### MQTT Topic Prefix
**Current**: UI input field, defaults to "printmaster/"  
**TODO**: Namespace for published messages  
**Impact**: Prevents topic collisions  
**Dependency**: Requires MQTT implementation  
**Complexity**: Low - string prefix

### Enable Prometheus Metrics
**Current**: UI toggle exists  
**TODO**: Expose /metrics endpoint with Prometheus format  
**Impact**: Monitoring and observability  
**Location**: `agent/main.go` - add metrics endpoint  
**Complexity**: Medium - requires prometheus client library

### Metrics Port
**Current**: UI input field, defaults to 9090  
**TODO**: Separate port for metrics endpoint  
**Impact**: Isolate metrics from main API  
**Dependency**: Requires Prometheus implementation  
**Complexity**: Low - additional HTTP server

## 🌐 Network Settings (TODO)

### Bind Address
**Current**: UI input field, defaults to "0.0.0.0"  
**TODO**: Configure HTTP server bind address  
**Impact**: Control which network interfaces to listen on  
**Location**: `agent/main.go` - pass to `http.ListenAndServe()`  
**Complexity**: Low - configuration parameter (partially implemented)

### Enable TLS/HTTPS
**Current**: UI toggle exists  
**TODO**: Start HTTPS server instead of HTTP  
**Impact**: Encrypted web interface access  
**Location**: `agent/main.go` - use `http.ListenAndServeTLS()`  
**Complexity**: Medium - requires certificate management

### TLS Certificate Path
**Current**: UI input field  
**TODO**: Path to TLS certificate file  
**Impact**: Required for HTTPS  
**Dependency**: Requires TLS implementation  
**Complexity**: Low - configuration parameter

### TLS Key Path
**Current**: UI input field  
**TODO**: Path to TLS private key file  
**Impact**: Required for HTTPS  
**Dependency**: Requires TLS implementation  
**Complexity**: Low - configuration parameter

### Enable IPv6 Discovery
**Current**: UI toggle exists  
**TODO**: Include IPv6 addresses in discovery  
**Impact**: Support IPv6-only networks  
**Location**: `agent/agent/detect.go` - add IPv6 logic  
**Complexity**: Medium - requires IPv6 network handling

### Network Interfaces (allowlist)
**Current**: UI textarea for comma-separated interface names  
**TODO**: Filter discovery to specific network interfaces  
**Impact**: Prevents scanning unwanted networks  
**Location**: `agent/agent/detect.go` - filter by interface  
**Complexity**: Medium - requires interface enumeration

### DNS Resolution Timeout (seconds)
**Current**: UI input field, defaults to 2 seconds  
**TODO**: Configure DNS lookup timeout  
**Impact**: Prevents hanging on slow DNS  
**Location**: `agent/agent/detect.go` - configure resolver timeout  
**Complexity**: Low - pass to DNS resolver

### Enable CORS
**Current**: UI toggle exists  
**TODO**: Add CORS headers to HTTP responses  
**Impact**: Allows web apps on different origins to access API  
**Location**: `agent/main.go` - CORS middleware  
**Complexity**: Low - add headers or use middleware

### CORS Allowed Origins
**Current**: UI textarea for comma-separated origins  
**TODO**: Whitelist of allowed CORS origins  
**Impact**: Security control for CORS  
**Dependency**: Requires CORS implementation  
**Complexity**: Low - configuration parameter

## 🔒 Security Settings (TODO)

### Enable Authentication
**Current**: UI toggle exists  
**TODO**: Require login for web interface and API  
**Impact**: Prevents unauthorized access  
**Location**: `agent/main.go` - add auth middleware  
**Complexity**: High - requires user management, sessions, password hashing

### Admin Username
**Current**: UI input field  
**TODO**: Administrator account username  
**Impact**: Login credentials  
**Dependency**: Requires authentication implementation  
**Complexity**: Low - configuration parameter

### Admin Password Hash
**Current**: UI input field (shows hash placeholder)  
**TODO**: Store bcrypt hash of admin password  
**Impact**: Secure password storage  
**Dependency**: Requires authentication implementation  
**Complexity**: Low - use `bcrypt` library

### Session Timeout (minutes)
**Current**: UI input field, defaults to 60  
**TODO**: Auto-logout inactive users  
**Impact**: Security for unattended sessions  
**Dependency**: Requires authentication implementation  
**Complexity**: Low - session expiration logic

### Enable Two-Factor Auth
**Current**: UI toggle exists  
**TODO**: TOTP-based 2FA for login  
**Impact**: Enhanced security  
**Dependency**: Requires authentication implementation first  
**Complexity**: High - requires TOTP library and QR code generation

### Enable Audit Logging
**Current**: UI toggle exists  
**TODO**: Log all security-relevant actions  
**Impact**: Compliance and security monitoring  
**Location**: New audit log module  
**Complexity**: Medium - separate audit log implementation

### Max Login Attempts
**Current**: UI input field, defaults to 5  
**TODO**: Account lockout after failed attempts  
**Impact**: Prevents brute force attacks  
**Dependency**: Requires authentication implementation  
**Complexity**: Low - counter with lockout logic

### Lockout Duration (minutes)
**Current**: UI input field, defaults to 15  
**TODO**: How long to lock account after max failures  
**Impact**: Balance security vs usability  
**Dependency**: Requires authentication implementation  
**Complexity**: Low - time-based lockout

## 📊 Implementation Priority

### High Priority (Quick Wins)
1. ✅ SNMP Timeout/Retries - **DONE**
2. ✅ Discover Concurrency - **DONE**
3. 🔧 SNMP Port - simple parameter
4. 🔧 SNMP Delay - simple rate limiting
5. 🔧 Ping Timeout - simple parameter
6. 🔧 Port Probe Timeout - simple parameter
7. 🔧 Max Concurrent DB Connections - one function call
8. 🔧 Enable Console Logging - add stdout writer
9. 🔧 DNS Resolution Timeout - simple parameter
10. 🔧 Enable CORS - add headers

### Medium Priority (Moderate Effort)
1. Enable SNMP Bulk GET - scanner modification
2. SNMP Result Cache + TTL - caching layer
3. Log Rotation (size + backup count) - file management
4. Device Cache + TTL - LRU cache implementation
5. Webhook Notifications - HTTP client with retry
6. Syslog Forwarding - syslog protocol
7. Enable TLS/HTTPS - certificate handling
8. Enable IPv6 Discovery - IPv6 support
9. Network Interface Allowlist - interface filtering
10. Rate Limiting - middleware implementation
11. Prometheus Metrics - metrics library integration
12. Enable Audit Logging - audit trail system

### Low Priority (Major Features)
1. SNMPv3 Support - full v3 protocol implementation
2. MQTT Publishing - MQTT client + message design
3. Authentication System - users, sessions, passwords
4. Two-Factor Auth - TOTP implementation
5. JSON Log Format - log reformatting (less critical with SSE)
6. Log Compression - nice-to-have for disk space

## 🧪 Testing Considerations

Each implemented setting should include:
- Unit tests for configuration parsing
- Integration tests for actual functionality
- Documentation updates in relevant files
- Update to this file marking as ✅ complete

## 📝 Notes

- Many settings are interdependent (e.g., cache size + TTL)
- Security settings form a cohesive system (auth → 2FA → audit)
- Integration settings are independent and can be tackled separately
- Performance settings have measurable impact and should include benchmarks
