# Security Architecture for Agent-Server Communication

## Overview

This document outlines the security architecture for when the PrintMaster agent communicates with a central server, particularly for the reverse proxy feature where the server may relay connections between users and printers through the agent.

## Threat Model

- **Agent → Server**: Agent authenticates to server, sends discovery data, receives config
- **User → Server → Agent → Printer**: User accesses printer web UI through server proxy that routes through agent
- **Key Risks**: Unauthorized agent access, credential interception, session hijacking, MITM attacks, unauthorized printer access

## Security Layers

### 1. Agent Authentication (MVP)

**Purpose**: Verify agent identity before accepting connections or data uploads.

**Implementation**:
- Bearer token authentication for agent API calls
- Long-lived tokens generated per agent on first registration
- Token stored securely in agent config (encrypted with machine key)
- All agent→server requests include `Authorization: Bearer <token>`

**Server Code Sketch**:
```go
func agentAuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if !strings.HasPrefix(token, "Bearer ") {
            http.Error(w, "Unauthorized", 401)
            return
        }
        
        agentID, err := validateAgentToken(token[7:])
        if err != nil {
            http.Error(w, "Invalid token", 401)
            return
        }
        
        ctx := context.WithValue(r.Context(), "agentID", agentID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

**Agent Code Sketch**:
```go
func (c *Client) uploadDevices(devices []Device) error {
    token, _ := c.cfg.GetConfigValue("agent_token")
    
    req, _ := http.NewRequest("POST", c.serverURL+"/agent/devices/batch", body)
    req.Header.Set("Authorization", "Bearer "+token)
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := c.httpClient.Do(req)
    // ... handle response
}
```

### 2. Mutual TLS (Production)

**Purpose**: Cryptographically verify both agent and server identity; prevent MITM.

**Implementation**:
- Server presents valid TLS certificate (Let's Encrypt or internal CA)
- Agent presents client certificate signed by organization CA
- Both sides verify certificates during handshake
- Certificate rotation every 90 days

**Configuration**:
```go
// Server side
tlsConfig := &tls.Config{
    ClientAuth:   tls.RequireAndVerifyClientCert,
    ClientCAs:    caCertPool,
    MinVersion:   tls.VersionTLS13,
    CipherSuites: []uint16{tls.TLS_AES_256_GCM_SHA384},
}

// Agent side
cert, _ := tls.LoadX509KeyPair("agent-cert.pem", "agent-key.pem")
tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
    MinVersion:   tls.VersionTLS13,
}
```

### 3. Request Signing (Production)

**Purpose**: Ensure message integrity; prevent replay attacks.

**Implementation**:
- HMAC-SHA256 signature of request body + timestamp
- Shared secret per agent (rotated monthly)
- Server validates signature and timestamp freshness (<5 min)

**Example**:
```go
func signRequest(body []byte, secret []byte) string {
    timestamp := time.Now().Unix()
    message := append([]byte(strconv.FormatInt(timestamp, 10)), body...)
    
    h := hmac.New(sha256.New, secret)
    h.Write(message)
    sig := base64.StdEncoding.EncodeToString(h.Sum(nil))
    
    return fmt.Sprintf("%d.%s", timestamp, sig)
}

// Header: X-Signature: 1730505123.a3f2b9c...
```

### 4. Proxy Session Isolation (Production)

**Purpose**: Prevent one user's session from accessing another user's printer connections.

**Implementation**:
- Server generates unique session tokens for each user proxy request
- Agent validates session token before establishing printer connection
- Session tokens expire after 15 minutes or on explicit disconnect
- Rate limiting per user (10 concurrent proxy sessions max)

**Flow**:
```
1. User requests printer proxy → Server validates user auth
2. Server generates session token → POST /agent/proxy/session/create
3. Agent returns WebSocket URL with session token
4. User connects via WebSocket → Agent validates token
5. Agent proxies user ↔ printer until disconnect or timeout
```

### 5. Credential Encryption in Transit (Production)

**Purpose**: Protect stored printer credentials when syncing with server.

**Implementation**:
- Printer web UI credentials encrypted with agent-specific key before upload
- Server stores encrypted blobs, never sees plaintext passwords
- Agent decrypts locally when needed for auto-login
- Key rotation via secure channel (mTLS)

**Schema**:
```go
type DeviceCredentials struct {
    Serial          string `json:"serial"`
    EncryptedCreds  string `json:"encrypted_creds"` // AES-GCM(username:password)
    KeyID           string `json:"key_id"`          // For key rotation
    AuthType        string `json:"auth_type"`
    AutoLogin       bool   `json:"auto_login"`
}
```

### 6. IP Whitelisting (Production)

**Purpose**: Restrict agent connections to known networks.

**Implementation**:
- Server maintains per-agent IP whitelist
- Agents behind NAT report external IP on registration
- Dynamic IP updates via authenticated endpoint
- Fail2ban integration for abuse detection

### 7. Audit Logging (MVP)

**Purpose**: Track all security-relevant events for compliance and forensics.

**Implementation**:
- Log all agent auth attempts (success/failure)
- Log all proxy session creation/destruction
- Log credential access/updates
- Structured logging with correlation IDs
- Retention: 90 days hot, 1 year cold storage

**Example Entry**:
```json
{
  "timestamp": "2025-11-01T22:45:32Z",
  "event": "proxy_session_created",
  "agent_id": "agent-001",
  "user_id": "user@example.com",
  "device_serial": "X59F000014",
  "session_id": "sess-a3f2b9c",
  "source_ip": "203.0.113.42"
}
```

### 8. Rate Limiting (MVP)

**Purpose**: Prevent abuse and DoS attacks.

**Implementation**:
- Per-agent: 100 device uploads/hour, 1000 metrics/hour
- Per-user: 10 proxy sessions, 100 requests/min
- Per-IP: 1000 requests/hour (global)
- Token bucket algorithm with burst allowance
- HTTP 429 with Retry-After header

### 9. Secure Communication Channels (MVP)

**Purpose**: Encrypt all data in transit.

**Implementation**:
- TLS 1.3 minimum for all connections
- HSTS headers (max-age=31536000, includeSubDomains)
- Certificate pinning for agent→server (optional)
- Disable weak ciphers and protocols

**Headers**:
```
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Content-Security-Policy: default-src 'self'
```

### 10. Zero-Trust Proxy Architecture (Enterprise)

**Purpose**: Minimize trust assumptions; assume breach.

**Implementation**:
- Server never directly proxies to printers
- All printer connections originate from agent (inside network)
- Server only routes authenticated user ↔ agent WebSocket
- Agent performs final authorization check before printer connection
- No credential storage on server (only encrypted blobs)

**Architecture**:
```
User Browser ←→ Server (Auth/Route) ←→ Agent (Inside Network) ←→ Printer
              TLS 1.3                  mTLS/WSS               HTTPS
```

## Implementation Phases

### Phase 1: MVP Security (Required for Launch)
- [ ] Agent bearer token authentication
- [ ] TLS 1.3 for all connections
- [ ] Audit logging (auth, proxy, credentials)
- [ ] Basic rate limiting (per-agent/per-user)
- [ ] Secure headers (HSTS, CSP, X-Frame-Options)

### Phase 2: Production Hardening
- [ ] Request signing (HMAC-SHA256)
- [ ] Proxy session isolation with expiring tokens
- [ ] Credential encryption in transit (agent-specific keys)
- [ ] IP whitelisting with dynamic updates
- [ ] Enhanced rate limiting with token bucket

### Phase 3: Enterprise Features
- [ ] Mutual TLS (client certificates)
- [ ] Zero-trust proxy architecture
- [ ] Certificate rotation automation
- [ ] SIEM integration for audit logs
- [ ] Compliance reporting (SOC2, HIPAA)

## Configuration Example

**Agent Config** (`agent_settings.json`):
```json
{
  "server_url": "https://printmaster.example.com",
  "agent_token": "encrypted:AgentTokenHere",
  "tls_cert_path": "/etc/printmaster/agent-cert.pem",
  "tls_key_path": "/etc/printmaster/agent-key.pem",
  "ca_cert_path": "/etc/printmaster/ca-cert.pem",
  "request_signing_enabled": true,
  "max_proxy_sessions": 10
}
```

**Server Config** (`server.yaml`):
```yaml
security:
  tls:
    cert: /etc/printmaster/server-cert.pem
    key: /etc/printmaster/server-key.pem
    ca: /etc/printmaster/ca-cert.pem
    min_version: "1.3"
    require_client_cert: true
  
  auth:
    agent_token_expiry: 90d
    user_session_expiry: 24h
    proxy_session_expiry: 15m
  
  rate_limits:
    agent_uploads_per_hour: 100
    user_proxy_sessions: 10
    requests_per_minute: 100
  
  audit:
    enabled: true
    retention_days: 90
    log_path: /var/log/printmaster/audit.log
```

## Security Best Practices

1. **Secrets Management**: Use environment variables or secret management service (Vault, AWS Secrets Manager) for tokens and keys
2. **Key Rotation**: Rotate agent tokens every 90 days, TLS certificates every 90 days, signing secrets monthly
3. **Least Privilege**: Agents only access devices in their network segment; users only access authorized devices
4. **Defense in Depth**: Multiple overlapping security controls; assume any single layer can fail
5. **Monitoring**: Alert on auth failures, unusual traffic patterns, rate limit hits, session anomalies
6. **Incident Response**: Document breach response plan; include agent revocation, credential rotation, forensic logging

## Threat Mitigation Summary

| Threat | Mitigated By | Phase |
|--------|--------------|-------|
| Unauthorized agent access | Bearer token auth, mTLS | MVP, Production |
| Credential interception | TLS 1.3, encrypted creds | MVP, Production |
| MITM attacks | TLS 1.3, cert pinning, mTLS | MVP, Production |
| Session hijacking | Expiring tokens, session isolation | Production |
| Replay attacks | Request signing, timestamps | Production |
| DoS/abuse | Rate limiting, IP whitelist | MVP, Production |
| Insider threat | Audit logging, least privilege | MVP |
| Data breach | Credential encryption, zero-trust | Production, Enterprise |

## Future Considerations

- **Multi-tenancy**: Isolate agents and devices by organization/tenant
- **Role-Based Access**: Fine-grained permissions (view-only, manage, admin)
- **SSO Integration**: SAML/OIDC for user authentication
- **Compliance**: GDPR, CCPA data handling for printer logs and credentials
- **Hardware Security**: TPM-backed key storage for agent certificates

---

Last Updated: November 1, 2025
