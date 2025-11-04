# PrintMaster 1.0 Roadmap

**Current Version**: 0.1.0  
**Target**: 1.0.0 (Stable Release)

## What 1.0.0 Means

**Version 1.0.0 signals:**
- ✅ **Stable Database Schema** - No more breaking schema changes
- ✅ **Stable Configuration Format** - Config files won't break between versions
- ✅ **Stable API** - HTTP endpoints won't change without deprecation
- ✅ **Production Ready** - Can be deployed with confidence
- ✅ **Migration Path** - Future versions will upgrade cleanly from 1.0
- ✅ **Documentation Complete** - All features documented
- ✅ **Cross-Platform Tested** - Windows, Mac, Linux verified

---

## Critical Blockers for 1.0

### 1. Database Schema Stability ⚠️
**Status**: Schema v7, but still evolving

**Must Complete:**
- [ ] **Finalize Device Model** - Ensure all printer fields are captured
  - Currently: 26 fields in `devices` table
  - Review: Are we missing any critical fields?
  - Lock: No more ADD COLUMN after 1.0
  
- [ ] **Finalize Metrics Model** - Lock what metrics we track
  - Currently: `metrics_history` table with ~20 metric fields
  - Decision: Which metrics are core vs optional?
  - Document: What each metric means (OID mappings)
  
- [ ] **Migration Strategy** - How do we upgrade from 0.x → 1.x → 2.x?
  - Implement: Proper migration system (currently ad-hoc)
  - Test: Upgrade from old database to new schema
  - Safety: Auto-backup before migrations

**Files to Stabilize:**
- `agent/storage/sqlite.go` - Schema definitions
- `agent/storage/migrations.go` - Migration logic
- `agent/storage/types.go` - Data models

---

### 2. Configuration Stability ⚠️
**Status**: Multiple config sources (config.ini, database, env vars)

**Must Complete:**
- [ ] **Unified Config Model** - Single source of truth
  - Currently: Settings spread across DB + config.ini + env vars
  - Decision: What belongs in config.ini vs database vs env?
  - Priority order: ENV > config.ini > database defaults
  
- [ ] **Config Validation** - Prevent invalid configs
  - IP range format validation
  - SNMP community string validation
  - Port number validation
  
- [ ] **Config Documentation** - Every setting explained
  - Document all config.ini options
  - Document all environment variables
  - Provide example configs for common scenarios

**Files to Stabilize:**
- `agent/config.ini.example` - Reference configuration
- Config loading in `agent/main.go` (lines ~1187-1289)
- Settings API endpoints

---

### 3. API Stability ⚠️
**Status**: API exists but may have inconsistencies

**Must Complete:**
- [ ] **API Versioning** - Prepare for future changes
  - Option A: `/api/v1/devices` style versioning
  - Option B: Header-based versioning
  - Option C: No breaking changes (add-only)
  
- [ ] **Consistent Response Format** - All endpoints return same structure
  - Success: `{"success": true, "data": {...}}`
  - Error: `{"success": false, "error": "message"}`
  
- [ ] **API Documentation** - OpenAPI/Swagger spec
  - Document all endpoints
  - Document all request/response formats
  - Provide examples

**Files to Review:**
- All `http.HandleFunc` calls in `agent/main.go`
- Response formats across endpoints
- Error handling consistency

---

### 4. Feature Completeness ⭐ CRITICAL
**Status**: Core features work, USB support is MAKE-OR-BREAK for 1.0

**Must Complete:**
- [ ] **Discovery Methods** - All protocols stable
  - ✅ mDNS/Bonjour
  - ✅ SSDP/UPnP
  - ✅ WS-Discovery
  - ✅ SNMP Traps
  - ✅ LLMNR
  - ✅ ARP scanning
  - ⚠️  Active IP range scanning (verify stability)
  
- [ ] **Vendor Support** - Core vendors fully supported
  - Test: HP, Canon, Epson, Brother, Kyocera, Xerox, Ricoh
  - Verify: OID mappings for each vendor
  - Document: Known limitations per vendor
  
- [ ] **Metrics Tracking** - History works reliably
  - Verify: Metrics captured correctly over time
  - Test: Graphs render properly
  - Fix: Any gaps in metric collection
  
- [ ] **Service Mode** - Windows/Linux service stability
  - Test: Install/uninstall on Windows
  - Test: systemd service on Linux
  - Test: Auto-restart on failure
  - Verify: Runs under service account

- [ ] **Multi-Agent Server** ⭐ **CRITICAL FOR 1.0**
  - Agent→Server communication working
  - Server tracks multiple agents across different sites
  - Centralized dashboard showing all agents and their devices
  - Agent status monitoring (online/offline/last-seen)
  - Cross-site reporting and analytics

- [ ] **Local Printer Tracking** ⭐ **CRITICAL FOR 1.0**
  - Detect when printers move between network locations
  - Track printer by serial number across IP changes
  - History: "This printer was at Site A, now at Site B"
  - Alert when printer disappears from expected location
  - Support for mobile/DHCP environments

- [ ] **USB Printer Support** ⭐ **CRITICAL FOR 1.0** 🚀 **GAME CHANGER**
  - **Problem**: Many clients have USB printers with NO network visibility
  - **Solution**: Cross-platform USB printer monitoring with full metrics
  - **Architecture**: **Pure Go** with gousbsnmp library (SNMP-over-USB via IEEE 1284.4)
  - **Strategy**: Build gousbsnmp as side project during v0.3.0-v0.9.0, integrate in v1.0
  - **Rationale**: Single cross-platform solution, super lightweight, avoids double work
  - **Deliverable**: Same rich metrics (page counts, toner levels) as network printers
  - **Target**: v1.0 (gousbsnmp development during earlier versions)
  - See: `docs/USB_IMPLEMENTATION.md` for detailed plan

---

### 5. Cross-Platform Compatibility ✅
**Status**: Designed for Windows/Mac/Linux but needs verification

**Must Complete:**
- [ ] **Windows Testing**
  - [x] Build works
  - [ ] Service installation works
  - [ ] Discovery works (all methods)
  - [ ] UI works in all browsers
  - [ ] File paths work (C:\ProgramData\...)
  
- [ ] **Linux Testing**
  - [ ] Build works
  - [ ] systemd service works
  - [ ] Discovery works (requires root/capabilities?)
  - [ ] File paths work (~/.local/share/...)
  
- [ ] **macOS Testing**
  - [ ] Build works
  - [ ] launchd service works (if applicable)
  - [ ] Discovery works (Bonjour native)
  - [ ] File paths work (~/Library/Application Support/...)

---

### 6. Security Hardening 🔒
**Status**: Basic security in place, needs review

**Must Complete:**
- [ ] **HTTPS by Default** - Self-signed cert generation working
  - ✅ Certificate generation exists
  - [ ] Verify cert renewal/rotation
  - [ ] Document how to use custom certs
  
- [ ] **Authentication** - Optional but should be ready
  - [ ] Basic auth for web UI (optional but available)
  - [ ] API key support for programmatic access?
  - [ ] Document security model
  
- [ ] **Input Validation** - Prevent injection attacks
  - [ ] SQL injection prevention (using parameterized queries)
  - [ ] SNMP community string validation
  - [ ] IP range validation (prevent scanning internet)
  
- [ ] **Secrets Management** - Don't store passwords in plain text
  - [ ] SNMP v3 credentials (if supported)
  - [ ] Proxy credentials encryption
  - [ ] Database encryption option?

---

### 7. Documentation 📚
**Status**: Good internal docs, needs user-facing docs

**Must Complete:**
- [ ] **User Guide** - How to use PrintMaster
  - Installation (Windows/Linux/Mac)
  - Quick start guide
  - Common tasks (add printers, view metrics, export data)
  
- [ ] **Admin Guide** - Deployment and configuration
  - Service deployment
  - Network requirements (ports, protocols)
  - Performance tuning
  - Troubleshooting
  
- [ ] **API Reference** - Complete endpoint documentation
  - All endpoints listed
  - Request/response examples
  - Authentication details
  
- [ ] **Developer Guide** - For contributors
  - ✅ Project structure documented
  - [ ] Build process
  - [ ] Testing guide
  - [ ] Contributing guidelines

**Existing Docs to Complete:**
- `README.md` - High-level overview
- `docs/API_REFERENCE.md` - Needs expansion
- `docs/CONFIGURATION.md` - Needs completion
- `docs/SERVICE_DEPLOYMENT.md` - Needs testing verification

---

### 8. Testing & Quality 🧪
**Status**: Some tests exist, needs expansion

**Must Complete:**
- [ ] **Unit Test Coverage** - Core logic tested
  - Current: Agent tests, storage tests
  - Target: >70% coverage for critical paths
  - Areas: Parser, SNMP, discovery, storage
  
- [ ] **Integration Tests** - End-to-end scenarios
  - [ ] Discovery finds real printers
  - [ ] Metrics collected correctly
  - [ ] Database operations reliable
  - [ ] API endpoints work together
  
- [ ] **Performance Testing** - Handles scale
  - [ ] Can discover 100+ printers
  - [ ] Can track 50+ devices continuously
  - [ ] Database doesn't grow uncontrolled
  - [ ] Memory doesn't leak
  
- [ ] **Regression Tests** - Prevent breakage
  - [ ] Test suite runs on every build
  - [ ] Known issues documented
  - [ ] No breaking changes without major version bump

---

## Enterprise Features for 1.0

### Multi-Agent Architecture 🏢

**The Problem:**
MSPs and enterprise customers need to monitor printer fleets across **multiple physical locations** (headquarters, branch offices, warehouses, remote sites). Each site needs a local agent, but management needs centralized visibility.

**The Solution:**
```
Site A (Office)              Central Server              Site B (Warehouse)
┌─────────────┐                    │                    ┌─────────────┐
│  Agent A    │────────────────────┼────────────────────│  Agent B    │
│  - 25 printers                   │                    │  - 15 printers
│  - 192.168.1.x                   │                    │  - 10.0.0.x
└─────────────┘                    │                    └─────────────┘
                                   │
                          ┌────────▼────────┐
                          │  PrintMaster    │
                          │  Server         │
                          │  - All agents   │
                          │  - All devices  │
                          │  - Reporting    │
                          └─────────────────┘
```

**Requirements:**
1. **Agent Registration** - Each agent registers with unique ID
2. **Data Upload** - Agents periodically send devices/metrics to server
3. **Agent Monitoring** - Server knows which agents are online/offline
4. **Multi-Tenant UI** - Server UI shows all sites in one dashboard
5. **Site Isolation** - Data from Site A doesn't interfere with Site B
6. **Audit Trail** - Track which agent reported what

**Implementation Status (v0.2.0):**
- [x] Server database schema (agents, devices, metrics tables)
- [x] Server API endpoints (register, heartbeat, upload)
- [ ] Agent ServerClient (HTTP client to send data)
- [ ] Upload worker (background sync)
- [ ] Server dashboard UI

**Critical for:** MSPs managing multiple customers, enterprises with branch offices

---

### Local Printer Tracking 📍

**The Problem:**
Printers **move around**:
- Employee takes printer from Office A to Office B
- DHCP assigns new IP when printer reconnects
- Printer offline for maintenance, comes back with different IP
- Mobile workers with portable printers

**Current Behavior (BROKEN):**
```
Day 1: Printer S/N ABC123 found at 192.168.1.50
Day 2: Same printer now at 192.168.1.75 (new DHCP lease)
Result: Agent thinks there are TWO printers (old one "disappeared", new one "appeared")
```

**The Solution:**
Track printers by **serial number**, not IP address:
```
Printer ABC123:
  - First seen: 2025-11-01 at 192.168.1.50
  - Now at: 192.168.1.75 (moved 2025-11-03)
  - Location history:
    - 192.168.1.50 (2025-11-01 to 2025-11-03)
    - 192.168.1.75 (2025-11-03 to present)
```

**Requirements:**
1. **Serial Number as Primary Key** - Already done in schema ✅
2. **IP Change Detection** - Flag when printer's IP changes
3. **Location History** - Track IP addresses over time
4. **Movement Alerts** - "Printer XYZ moved to new location"
5. **Offline Detection** - "Printer XYZ not seen for 7 days"
6. **MAC Address Correlation** - Use MAC as secondary identifier

**Implementation Status:**
- [x] Serial number primary key in database
- [ ] IP change detection logic
- [ ] Location history table/tracking
- [ ] Movement alerts in UI
- [ ] MAC address tracking

**Database Schema Addition Needed:**
```sql
CREATE TABLE device_location_history (
    id INTEGER PRIMARY KEY,
    serial TEXT NOT NULL,
    ip TEXT,
    mac_address TEXT,
    hostname TEXT,
    first_seen DATETIME,
    last_seen DATETIME,
    agent_id TEXT,  -- Which agent saw it (for multi-agent deployments)
    FOREIGN KEY (serial) REFERENCES devices(serial)
);
```

**Critical for:** 
- DHCP environments
- Mobile printer fleets
- Multi-site organizations
- Tracking stolen/missing equipment

---

## Version Milestone Plan

### 0.1.0 (Current) - Initial Release
- Core discovery works
- Basic web UI
- SQLite storage
- Windows builds

### 0.2.0 - Multi-Agent Server ⚡ IN PROGRESS
- ✅ Agent-server communication protocol (REST/JSON)
- ✅ ServerClient implementation with Bearer token auth
- ⚠️ Server token generation (in progress)
- ⚠️ Server audit logging (in progress)
- ⚠️ Agent upload worker (in progress)
- Multi-site agent management
- Centralized dashboard
- **Target**: Ship this week

### 0.3.0 - Local Printer Tracking & History
- Track printer by serial number across IP changes
- Device location history table
- Alert when printer moves between sites
- Last-seen timestamp tracking
- DHCP environment support
- **Target**: 2-3 weeks after v0.2.0

### 0.4.0 - Configuration Stability
- Finalize config format
- Config validation
- Example configs for common setups
- Environment variable support

### 0.5.0 - Database Stability
- Lock schema for devices table
- Lock schema for metrics_history
- Migration system in place
- Backup/restore tools
- Schema versioning system

### 0.6.0 - Agent Deployment & Packaging 📦
- Custom installer generator (per-site credentials)
- One-time registration token system
- Windows: NSIS/WiX installer with embedded config
- Linux/macOS: Universal shell script installer
- Raspberry Pi: Pre-configured SD card image generator
- Agent licensing/revocation system
- IP whitelisting and geo-validation
- **Goal**: Zero-touch deployment for MSPs

### 0.7.0 - Cross-Platform & Hardware
- Linux x64 support verified
- macOS (Intel/ARM) support verified
- Raspberry Pi 4/5 support verified
- Raspberry Pi deployment documentation
- Hardware recommendation guide
- Platform-specific optimizations

### 0.8.0 - gousbsnmp Side Project (Parallel Development)
- Start gousbsnmp library development
- USB device enumeration with gousb
- IEEE 1284.4 packet framing implementation
- SNMP marshaling integration with gosnmp
- Basic SNMP query/response over USB
- Test with 2-3 printer brands
- **Note**: Development happens in parallel, doesn't block other milestones

### 0.9.0 - Security Hardening & Production Polish
- HTTPS enforced
- Certificate management
- Multi-tenant isolation
- Security audit complete
- Vendor support (top 7 vendors fully tested)
- OID mappings verified
- Known limitations documented
- Performance benchmarking
- Load testing (100+ agents, 1000+ printers)

### 0.10.0 - Release Candidate
- All tests passing
- Performance verified
- Bug fix sprint
- Documentation complete (user guide, admin guide, API reference)

### 1.0.0 - USB Printer Support Integration 🚀 GAME CHANGER
- Integrate mature gousbsnmp library (from v0.8.0 parallel development)
- Agent USB discovery with pure Go implementation
- Cross-platform USB printer enumeration
- Same rich metrics as network printers (page counts, toner levels)
- No C++ dependencies - 100% pure Go, super lightweight
- Single binary works on Windows, Linux, macOS, Raspberry Pi
- USB printer configuration and management
- USB printer troubleshooting tools
- **Final deliverable**: World's first open-source tool with comprehensive USB printer monitoring
- Beta testing with friendly MSPs

### 1.0.0 - Stable Release 🎉
- All above complete
- Production ready
- Support commitment begins

---

## Breaking Changes After 1.0

**After 1.0.0, breaking changes require MAJOR version bump:**

- **Database schema changes** → 2.0.0 (must provide migration)
- **API endpoint removal** → 2.0.0 (must deprecate in 1.x first)
- **Config format changes** → 2.0.0 (must support old format)

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

## Success Criteria for 1.0

**Before releasing 1.0.0, we must be able to say:**

✅ "The database schema is stable and we can migrate forward from 1.0 forever"  
✅ "The configuration format won't break existing deployments"  
✅ "The API is documented and we won't break existing integrations"  
✅ "We've tested on Windows, Linux, and macOS"  
✅ "We've tested with the top 7 printer vendors"  
✅ "We have a full test suite preventing regressions"  
✅ "Documentation is complete enough for new users"  
✅ "We're committed to supporting 1.x for at least 12 months"

---

## Timeline Estimate

**Aggressive**: 2-3 months (if full-time)  
**Realistic**: 4-6 months (part-time development)  
**Conservative**: 6-12 months (careful testing + polish)

**The timeline depends on:**
- How many breaking changes are needed in schema/config
- How much testing infrastructure needs to be built
- How thorough you want cross-platform testing
- Whether you need external user testing before 1.0

---

## Notes

- **Don't rush to 1.0** - Once you hit 1.0, you're committing to stability
- **0.x is your friend** - Use 0.2, 0.3, 0.4... to make breaking changes freely
- **Deprecation over deletion** - After 1.0, deprecate old features before removing
- **Semantic versioning is a contract** - Users rely on version numbers meaning something

**Current Status**: We're in early 0.x - great time to experiment and refine!
