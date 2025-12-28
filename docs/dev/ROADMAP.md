# PrintMaster Roadmap to 1.0 and Beyond

**Current Release**: Agent 0.9.14 / Server 0.9.14 (November 23, 2025)  
**Historical Baseline in this doc**: 0.3.3 (kept for context)  
**Target**: 1.0.0 (Stable Production Release)

---

## What 1.0.0 Means

**Version 1.0.0 signals production readiness:**
- ✅ **Stable Database Schema** - No more breaking schema changes
- ✅ **Stable Configuration Format** - Config files won't break between versions
- ✅ **Stable API** - HTTP endpoints won't change without deprecation
- ✅ **Production Ready** - Can be deployed with confidence
- ✅ **Migration Path** - Future versions will upgrade cleanly from 1.0
- ✅ **Documentation Complete** - All features documented
- ✅ **Cross-Platform Tested** - Windows, Mac, Linux verified
- ✅ **USB Printer Support** - Full USB device monitoring with metrics

---

## Version Milestone Plan

### ✅ 0.1.0 - Initial Release (Completed)
- Core SNMP discovery working
- Basic web UI
- SQLite storage
- Windows builds

### ✅ 0.2.0 - Multi-Agent Server (Completed)
- ✅ Agent-server communication protocol (REST/JSON)
- ✅ ServerClient implementation with Bearer token auth
- ✅ Server token generation (crypto/rand, base64)
- ✅ Server audit logging (all agent operations)
- ✅ Agent upload worker (heartbeat + metrics sync)
- ✅ Multi-site agent management
- ✅ Pure Go SQLite (modernc.org/sqlite, no CGO)
- ✅ Comprehensive test suite (22 tests: 14 server + 8 client)
- **Released**: November 2025

### ✅ 0.9.x - Schema & Upload Hardening (Current Release)
**Focus**: Ship stable builds with schema v8, rotation safety nets, and the refactored build/release tooling.

**Highlights (shipped in 0.9.14):**
- ✅ Database schema v8 (metrics tiering: raw/hourly/daily/monthly)
- ✅ Automatic database rotation on migration failure + user notifications
- ✅ Count-based backup cleanup (10 files cap)
- ✅ Build system refactor (debug vs. release profiles)
- ✅ Clean semantic versioning + release automation
- ✅ Metrics/device table separation with enhanced SNMP collection
- ✅ Agent/server version parity enforced through shared tooling

**Next Stop**: 0.10.0 discovery/settings polish (see sections below).

### ⚡ 0.3.x - Current Development (In Progress)
> **Historical note:** The 0.3.x plan below represents the original pre-release roadmap. Items not already delivered in 0.9.14 roll into the upcoming 0.10.0 milestone.

**Focus**: Schema stability, database rotation, metrics improvements (superseded by 0.9.x release)

**Completed in 0.3.x:**
- ✅ Database schema v8 (metrics tiering: raw/hourly/daily/monthly)
- ✅ Automatic database rotation on migration failure
- ✅ User notification system for rotation events
- ✅ Count-based backup cleanup (10 files)
- ✅ Build system refactor (dev vs release builds)
- ✅ Clean semantic versioning
- ✅ Metrics separation from device table
- ✅ Enhanced SNMP metrics collection

**Status**: Superseded (shipped across 0.7.0–0.9.14)

### 🎯 0.4.0 - Discovery Methods & Live Detection
**Status**: Planned  
**Goal**: Complete all network discovery protocols

#### Live Discovery Methods
**Completed:**
- ✅ mDNS/DNS-SD (Bonjour/Zeroconf) - `agent/agent/mdns.go`
- ✅ WS-Discovery (SOAP/UDP) - `agent/agent/wsdiscovery.go`
- ✅ SSDP/UPnP - `agent/agent/ssdp.go`
- ✅ SNMP Traps (event-driven) - `agent/agent/snmptraps.go`
- ✅ LLMNR (Link-Local Name Resolution) - `agent/agent/llmnr.go`
- ✅ ARP scanning - `agent/agent/arp.go`

**Remaining:**
- [ ] NetBIOS/WINS broadcasts (legacy Windows) - LOW PRIORITY
- [ ] LLDP (network topology) - LOW PRIORITY

#### Discovery Settings Wiring
**Completed:**
- ✅ SNMP Timeout (ms)
- ✅ SNMP Retries
- ✅ Discover Concurrency

**Remaining:**
- [ ] SNMP Bulk GET
- [ ] SNMPv3 Support (auth/priv)
- [ ] SNMP Port configuration
- [ ] SNMP Delay Between Queries
- [ ] SNMP Result Cache + TTL

**Target**: 1-2 weeks

---

### 🎯 0.5.0 - Configuration & Settings Stability
**Status**: Planned  
**Goal**: Finalize configuration system

#### Quick Wins (Low Complexity)
- [ ] Ping Timeout configuration
- [ ] Port Probe Timeout configuration
- [ ] Max Concurrent DB Connections
- [ ] Enable Console Logging
- [ ] DNS Resolution Timeout
- [ ] CORS headers

#### Logging Enhancements
- [ ] Log Rotation Size (MB)
- [ ] Log Backup Count
- [ ] JSON Log Format (for ELK/Splunk)
- [ ] Log Compression (gzip rotated files)

#### Configuration Validation
- [ ] IP range format validation
- [ ] SNMP community string validation
- [ ] Port number validation
- [ ] Config documentation (all settings explained)

**Files to Finalize:**
- `agent/config.ini.example` - Reference configuration
- Settings API consistency
- Environment variable support

**Target**: 2-3 weeks

---

### 🎯 0.6.0 - Local Printer Tracking & History
**Status**: Planned  
**Goal**: Track printers across IP changes and locations

#### The Problem
Printers move around:
- Employee relocates printer between sites
- DHCP assigns new IP
- Printer offline for maintenance, comes back with different IP
- Mobile workers with portable printers

Current behavior treats same printer at different IPs as two devices ❌

#### The Solution
Track by serial number, not IP:
```
Printer ABC123:
  - First seen: 2025-11-01 at 192.168.1.50 (Site A)
  - Moved: 2025-11-03 to 192.168.1.75 (Site A)
  - Moved: 2025-11-10 to 10.0.0.100 (Site B)
```

#### Implementation
**Database Schema Addition:**
```sql
CREATE TABLE device_location_history (
    id INTEGER PRIMARY KEY,
    serial TEXT NOT NULL,
    ip TEXT,
    mac_address TEXT,
    hostname TEXT,
    first_seen DATETIME,
    last_seen DATETIME,
    agent_id TEXT,  -- Multi-agent support
    site_name TEXT,
    FOREIGN KEY (serial) REFERENCES devices(serial)
);
```

**Features:**
- [ ] IP change detection logic
- [ ] Location history tracking
- [ ] Movement alerts in UI ("Printer moved to new location")
- [ ] Offline detection ("Printer not seen for 7 days")
- [ ] MAC address correlation
- [ ] Multi-site movement tracking

**Critical for:** DHCP environments, mobile fleets, multi-site organizations

**Target**: 3-4 weeks

---

### 🎯 0.7.0 - Agent Deployment & Packaging 📦
**Status**: Planned  
**Goal**: Zero-touch deployment for MSPs

#### Custom Installer Generator
**Problem**: MSPs need to deploy agents to hundreds of sites without manual configuration

**Solution**: Generate per-site installers with embedded credentials:
```powershell
# Generate installer for "Acme Corp - Warehouse"
.\generate-installer.ps1 -SiteName "Acme-Warehouse" -ServerUrl "https://server.msp.com" -Token "xyz..."

# Output: PrintMaster-Agent-Acme-Warehouse-v0.7.0.exe (Windows)
#         printmaster-agent-acme-warehouse-v0.7.0.sh (Linux)
```

#### Features
- [ ] **Windows**: NSIS/WiX installer with embedded config
- [ ] **Linux/macOS**: Universal shell script installer  
- [ ] **Raspberry Pi**: Pre-configured SD card image generator
- [ ] One-time registration token system
- [ ] Agent licensing/revocation system
- [ ] IP whitelisting and geo-validation
- [ ] Auto-update capability

**Deliverable**: MSP can email customer a single .exe that installs, configures, and registers agent automatically

**Target**: 4-5 weeks

---

### 🎯 0.8.0 - Cross-Platform Verification & Raspberry Pi
**Status**: Planned  
**Goal**: Production-grade support for all platforms

#### Platform Testing
**Windows:**
- [ ] Service installation verified
- [ ] Discovery (all methods) tested
- [ ] UI tested (Edge, Chrome, Firefox)
- [ ] File paths verified
- [ ] Installer tested (WiX/NSIS)

**Linux (Ubuntu/Debian):**
- [ ] systemd service verified
- [ ] Discovery (requires capabilities?) tested
- [ ] File paths verified
- [ ] .deb package created
- [ ] SELinux compatibility

**macOS (Intel/ARM):**
- [ ] launchd service verified
- [ ] Bonjour/mDNS native support tested
- [ ] File paths verified
- [ ] .dmg or .pkg installer
- [ ] Code signing for Gatekeeper

**Raspberry Pi 4/5:**
- [ ] ARM64 builds verified
- [ ] Performance on Pi hardware tested
- [ ] SD card image generator
- [ ] Headless deployment documentation
- [ ] Power consumption measurements

#### Hardware Recommendations
- [ ] Minimum specs (Pi 3B+, 1GB RAM)
- [ ] Recommended specs (Pi 4, 4GB RAM)
- [ ] Network requirements
- [ ] Storage requirements (SD card vs SSD)

**Target**: 3-4 weeks

---

### 🚀 0.9.0 - gousbsnmp Side Project (Parallel Development)
**Status**: Planned (parallel to 0.4-0.8)  
**Goal**: Develop USB SNMP library as independent project

#### Why USB Support is Critical
**The Market Gap:**
- 40-60% of small business printers are **USB-only** (no network interface)
- Current tools: WMI (Windows-only), CUPS (limited metrics), proprietary vendor software
- **No open-source cross-platform USB printer monitoring exists**

**The Opportunity:**
PrintMaster will be the **first open-source tool** to provide comprehensive USB printer metrics across all platforms.

#### gousbsnmp Architecture
**Pure Go Implementation:**
```
gousbsnmp (library)
├── USB enumeration (github.com/google/gousb)
├── IEEE 1284.4 packet framing
├── SNMP marshaling (github.com/gosnmp/gosnmp)
└── Cross-platform (Windows, Linux, macOS, Pi)
```

**Why Not C++ Wrapper?**
- ❌ CUPS SNMP backend: Linux-only, limited vendors
- ❌ libusb + SNMP: Cross-platform but requires C toolchain, complex builds
- ✅ Pure Go: Single binary, no dependencies, super lightweight

#### Development Phases
**Phase 1: Foundation (Weeks 1-2)**
- [ ] USB device enumeration with gousb
- [ ] Printer class detection (USB class 0x07)
- [ ] Basic device info extraction (VID/PID, serial, manufacturer)

**Phase 2: IEEE 1284.4 (Weeks 3-5)**
- [ ] Packet framing implementation
- [ ] Socket ID management
- [ ] Credit-based flow control
- [ ] Error handling and retries

**Phase 3: SNMP Integration (Weeks 6-8)**
- [ ] Integrate gosnmp for marshaling
- [ ] SNMP GET request over USB
- [ ] SNMP GET response parsing
- [ ] OID traversal support

**Phase 4: Testing & Vendor Support (Weeks 9-12)**
- [ ] Test with HP USB printers
- [ ] Test with Canon USB printers
- [ ] Test with Epson USB printers
- [ ] Test with Brother USB printers
- [ ] Document vendor quirks
- [ ] Error recovery and edge cases

**Phase 5: Library Maturity (Weeks 13-16)**
- [ ] Comprehensive test suite
- [ ] Performance optimization
- [ ] Documentation and examples
- [ ] Release as standalone library (github.com/mstrhakr/gousbsnmp)

**Development Strategy:**
- Build gousbsnmp as **separate project** during v0.4-v0.8 development
- Allows independent testing and iteration
- Can be used by other projects (ecosystem benefit)
- Integrate mature library into PrintMaster at v1.0

**Target**: 3-4 months (parallel development)

---

### 🎯 0.10.0 - Security & Performance Hardening
**Status**: Planned  
**Goal**: Production-ready security and performance

#### Security Features
**Authentication System:**
- [ ] User management (create/delete/modify users)
- [ ] Session management with secure cookies
- [ ] Password hashing (bcrypt)
- [ ] Login rate limiting
- [ ] Account lockout (5 attempts, 15 min lockout)
- [ ] TOTP-based Two-Factor Auth (optional)
- [ ] Audit logging (all security events)

**TLS/HTTPS:**
- [ ] HTTPS enforced by default
- [ ] Auto-generate self-signed certs on first run
- [ ] Certificate renewal/rotation
- [ ] Support for custom certificates
- [ ] TLS 1.2+ only

**Input Validation:**
- [ ] SQL injection prevention (parameterized queries - already done ✅)
- [ ] SNMP community string validation
- [ ] IP range validation (prevent scanning internet)
- [ ] API rate limiting middleware
- [ ] CORS configuration

**Secrets Management:**
- [ ] Encrypt SNMP v3 credentials at rest
- [ ] Encrypt proxy credentials
- [ ] Database encryption option (SQLCipher)

#### Performance Optimization
**Load Testing:**
- [ ] Test with 100+ agents reporting to server
- [ ] Test with 1000+ printers tracked
- [ ] Database performance under load
- [ ] Memory leak detection
- [ ] CPU profiling and optimization

**Caching:**
- [ ] SNMP result cache (configurable TTL)
- [ ] Device info cache (LRU, 1000 entries)
- [ ] HTTP response caching where appropriate

**Database Optimization:**
- [ ] Index optimization for common queries
- [ ] Connection pooling configuration
- [ ] Vacuum and analyze automation
- [ ] Query performance monitoring

#### Vendor Support Verification
- [ ] HP printers (top 10 models tested)
- [ ] Canon printers (top 10 models tested)
- [ ] Epson printers (top 10 models tested)
- [ ] Brother printers (top 10 models tested)
- [ ] Kyocera printers (top 5 models tested)
- [ ] Xerox printers (top 5 models tested)
- [ ] Ricoh printers (top 5 models tested)
- [ ] Document known limitations per vendor

**Target**: 5-6 weeks

---

### 🎯 0.11.0 - Documentation & Release Candidate
**Status**: Planned  
**Goal**: Complete documentation, bug fixes, final testing

#### User Documentation
- [ ] **Installation Guide** (Windows/Linux/macOS/Pi)
- [ ] **Quick Start Guide** (15-minute setup)
- [ ] **User Manual** (common tasks, screenshots)
- [ ] **Admin Guide** (deployment, performance tuning)
- [ ] **Troubleshooting Guide** (common issues, solutions)
- [ ] **FAQ** (frequently asked questions)

#### Developer Documentation
- [ ] **API Reference** (all endpoints documented) - ✅ Already done (API.md)
- [ ] **Contributing Guidelines** (how to contribute)
- [ ] **Code Architecture** (design decisions, patterns)
- [ ] **Testing Guide** (how to run tests, add new tests)
- [ ] **Release Process** (how releases work)

#### Video Content
- [ ] Installation walkthrough (YouTube)
- [ ] Discovery configuration demo
- [ ] Multi-agent setup demo
- [ ] USB printer setup demo (v1.0)

#### Final Testing
- [ ] **Beta Testing Program** (10-20 friendly MSPs)
- [ ] Bug fix sprint (address beta feedback)
- [ ] Performance regression testing
- [ ] Security audit (external if possible)
- [ ] Accessibility testing (web UI)
- [ ] Browser compatibility (Chrome, Firefox, Edge, Safari)

#### Release Preparation
- [ ] Changelog complete (all changes since 0.1.0)
- [ ] Migration guide (0.x → 1.0)
- [ ] Known issues documented
- [ ] Support plan defined
- [ ] Versioning policy documented

**Target**: 4-5 weeks

---

### 🎉 1.0.0 - Stable Production Release
**Status**: Future  
**Goal**: USB integration, production readiness guarantee

#### USB Printer Support Integration 🚀
**The Game Changer:**
- [ ] Integrate mature gousbsnmp library (from 0.9.0 parallel development)
- [ ] Agent USB discovery with pure Go
- [ ] Cross-platform USB printer enumeration (Windows, Linux, macOS, Pi)
- [ ] Same rich metrics as network printers (page counts, toner levels, consumables)
- [ ] No C++ dependencies - 100% pure Go, super lightweight
- [ ] Single binary works everywhere
- [ ] USB printer configuration UI
- [ ] USB printer troubleshooting tools
- [ ] Automatic USB/network printer differentiation

**Deliverable**: **World's first open-source cross-platform USB printer monitoring tool**

#### Final Checklist
- [ ] All critical blockers resolved
- [ ] All tests passing (unit, integration, e2e)
- [ ] All documentation complete
- [ ] Performance benchmarks met
- [ ] Security audit passed
- [ ] Beta testing feedback addressed
- [ ] Migration path from 0.x verified
- [ ] Support commitment defined

#### Success Criteria
✅ "The database schema is stable and we can migrate forward from 1.0 forever"  
✅ "The configuration format won't break existing deployments"  
✅ "The API is documented and we won't break existing integrations"  
✅ "We've tested on Windows, Linux, macOS, and Raspberry Pi"  
✅ "We've tested with the top 7 printer vendors"  
✅ "We have full test suite preventing regressions"  
✅ "Documentation is complete enough for new users"  
✅ "USB and network printers both work seamlessly"  
✅ "We're committed to supporting 1.x for at least 12 months"

**Target**: Q2 2026

---

## Critical Blockers for 1.0

### 1. Database Schema Stability ⚠️
**Current**: Schema v8, stable but may need minor additions

**Must Complete:**
- [x] Finalize Device Model (27 fields, separated from metrics) ✅
- [x] Finalize Metrics Model (raw/hourly/daily/monthly tiering) ✅
- [ ] Verify no missing critical fields
- [ ] Lock schema: No more ADD COLUMN after 1.0
- [ ] Document all fields and their meanings
- [ ] Migration system fully tested

**Files:**
- `agent/storage/sqlite.go` - Schema definitions ✅
- `agent/storage/migrations.go` - Migration logic ✅
- `agent/storage/rotation.go` - Rotation handling ✅

---

### 2. Configuration Stability ⚠️
**Current**: Multiple config sources (agent.db, config.ini, env vars)

**Must Complete:**
- [ ] Document config priority: ENV > config.ini > database defaults
- [ ] Config validation for all settings
- [ ] Example configs for common scenarios
- [ ] Document all environment variables

**Files:**
- `agent/config.ini.example` - Reference configuration
- Settings loading in `agent/main.go`
- `/settings` API endpoint

---

### 3. API Stability ✅
**Current**: API documented, stable endpoints

**Completed:**
- [x] API documentation complete (docs/API.md) ✅
- [x] Consistent response formats ✅
- [x] Error handling standardized ✅

**Decision: No API versioning pre-1.0**
- Use deprecation strategy instead of versioning
- Breaking changes allowed in 0.x
- After 1.0: Deprecate old endpoints before removal

---

### 4. USB Printer Support ⭐ MAKE-OR-BREAK
**Status**: Planned for 1.0

**Why Critical:**
- 40-60% of small business printers are USB-only
- No existing open-source cross-platform solution
- Differentiator from all competitors
- Pure Go = single binary everywhere

**See**: 0.9.0 milestone for gousbsnmp development plan

---

### 5. Multi-Agent Server ✅
**Status**: COMPLETE (v0.2.0)

**Delivered:**
- [x] Agent-server communication ✅
- [x] Multi-site tracking ✅
- [x] Server dashboard ✅
- [x] Agent monitoring ✅

**Future Enhancements (post-1.0):**
- [ ] Server UI improvements
- [ ] Cross-site reporting
- [ ] Agent auto-update distribution

---

## Post-1.0 Roadmap

### 1.1.0 - Enterprise Enhancements
- Advanced reporting (PDF, Excel exports)
- Scheduled reports (email digest)
- Custom dashboards
- Role-based access control (RBAC)
- Multi-tenant isolation

### 1.2.0 - Integration & Automation
- Webhook notifications (device discovery, alerts)
- MQTT publishing (IoT integration)
- Prometheus metrics endpoint
- Syslog forwarding
- REST API for external tools

### 1.3.0 - Advanced Monitoring
- Alert rules engine (low toner, offline devices)
- Predictive maintenance (toner depletion forecasts)
- Usage analytics and trends
- Cost tracking (cost-per-page calculations)
- Supply ordering integration

### 2.0.0 - Cloud-Native Architecture
- Kubernetes deployment
- Horizontal scaling
- Redis caching
- PostgreSQL option (in addition to SQLite)
- Cloud-native monitoring (Datadog, New Relic)
- Multi-region support

---

## Implementation Notes

### Settings Implementation Priority

**Quick Wins** (1-2 days each):
1. ✅ SNMP Timeout/Retries - COMPLETE
2. ✅ Discover Concurrency - COMPLETE
3. SNMP Port configuration
4. SNMP Delay Between Queries
5. Ping Timeout
6. Port Probe Timeout
7. Max Concurrent DB Connections
8. DNS Resolution Timeout
9. Enable CORS

**Medium Effort** (3-5 days each):
1. SNMP Bulk GET
2. SNMP Result Cache + TTL
3. Log Rotation (size + backup count)
4. Device Cache + TTL
5. Webhook Notifications
6. Syslog Forwarding
7. Enable TLS/HTTPS
8. Enable IPv6 Discovery
9. Rate Limiting
10. Prometheus Metrics

**Major Features** (1-2 weeks each):
1. SNMPv3 Support (auth/priv/encryption)
2. MQTT Publishing
3. Authentication System (users, sessions, passwords)
4. Two-Factor Auth (TOTP)
5. Audit Logging System

*See docs/SETTINGS_TODO.md for detailed implementation notes (to be archived)*

---

## Breaking Changes After 1.0

**After 1.0.0, breaking changes require MAJOR version bump:**
- Database schema changes → 2.0.0 (must provide migration)
- API endpoint removal → 2.0.0 (must deprecate in 1.x first)
- Config format changes → 2.0.0 (must support old format)

**Allowed in MINOR versions (1.x.0):**
- New database columns (backward compatible)
- New API endpoints
- New configuration options (with defaults)
- New features (opt-in)

**Allowed in PATCH versions (1.0.x):**
- Bug fixes
- Performance improvements
- Documentation updates
- Security fixes (if non-breaking)

---

## Timeline Estimates

**To 1.0.0:**
- **Aggressive**: 4-5 months (full-time development)
- **Realistic**: 6-9 months (part-time development)
- **Conservative**: 9-12 months (careful testing + beta program)

**Milestone Breakdown:**
- 0.4.0 - 2 weeks
- 0.5.0 - 3 weeks
- 0.6.0 - 4 weeks
- 0.7.0 - 5 weeks
- 0.8.0 - 4 weeks
- 0.9.0 - 16 weeks (parallel)
- 0.10.0 - 6 weeks
- 0.11.0 - 5 weeks
- 1.0.0 - 2 weeks (integration + final testing)

**Total**: ~25 weeks sequential + 16 weeks parallel = **6-7 months realistic**

---

## Notes

- **Don't rush to 1.0** - Once you hit 1.0, you're committing to stability
- **0.x is your friend** - Use 0.4, 0.5, 0.6... to make breaking changes freely
- **Deprecation over deletion** - After 1.0, deprecate old features before removing
- **Semantic versioning is a contract** - Users rely on version numbers
- **USB support is the killer feature** - This is what makes PrintMaster unique

**Current Status**: We're at 0.3.3 - great progress on schema stability, database rotation, and build workflow. Next focus: complete discovery methods (0.4.0) and settings wiring (0.5.0).

---

*Last Updated: November 6, 2025*  
*Current Version: 0.3.3*  
*Next Milestone: 0.4.0 (Discovery Methods & Live Detection)*
