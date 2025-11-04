# Agent Deployment Strategy

**Target Version:** v0.6.0+  
**Status:** Design Phase - Implementation Deferred  
**Priority:** High (after USB support in v0.3.0)

---

## Overview

This document outlines the agent packaging and deployment strategy for multi-site MSP environments. The goal is **zero-touch deployment** with embedded credentials and security.

## Deployment Options (Recommendation: Option 3)

### Option 1: ZIP + Manual Config ❌
- Not recommended - too error-prone

### Option 2: License Key Entry ⚠️
- User enters license key on first run
- Good fallback option

### Option 3: Custom Installer per Site ✅ **RECOMMENDED**
- Pre-configured installer with embedded credentials
- One-time registration token (expires in 7 days)
- Zero manual configuration required
- Most professional and mistake-proof

## Platform Support Matrix

| Platform | Installer Format | Status | Priority |
|----------|-----------------|--------|----------|
| Windows x64 | `.exe` (NSIS/WiX) | Planned v0.6.0 | High |
| Linux x64 | `.deb`, `.rpm`, shell script | Planned v0.6.0 | Medium |
| macOS Intel/ARM | `.pkg` | Planned v0.7.0 | Low |
| Raspberry Pi (ARM64) | `.img` + shell script | Planned v0.6.0 | **High** |

## Raspberry Pi: The Killer Feature 🎯

### Why Raspberry Pi?

**Problem:** Small offices without dedicated servers
- Installing agent on employee PCs is fragile
- PCs turn off, get rebuilt, users uninstall software
- Unprofessional to rely on end-user hardware

**Solution:** Dedicated Raspberry Pi appliance
- $60-100 hardware cost per site
- Plug and play deployment
- 24/7 uptime, no user interference
- Professional, dedicated monitoring device
- Silent, fanless, low power (<5W)

### Hardware Recommendations

**Raspberry Pi 4 (2GB RAM)** - $35-45
- Handles 100+ printers easily
- Gigabit ethernet
- Perfect for small offices

**Raspberry Pi 5 (4GB RAM)** - $60-75
- Handles 500+ printers
- PCIe for future expansion
- Overkill for most sites but future-proof

**Complete Kit (per site):**
- Raspberry Pi 4/5
- 32GB MicroSD card (pre-flashed)
- Power supply
- Ethernet cable
- Case with mounting hardware
- **Total cost:** $60-100
- **Margin potential:** 50%+

### Deployment Workflow

```
MSP Admin → Server UI → "Add Agent" → Select "Raspberry Pi"
    ↓
Generate pre-configured SD card image
    ↓
Download printmaster-agent-customer-site.img.gz (1.2 GB)
    ↓
Flash to SD card with Raspberry Pi Imager
    ↓
Ship Pi to client site or have tech flash on-site
    ↓
Tech plugs in: Network + Power
    ↓
Pi boots, agent auto-registers with server
    ↓
Agent appears in dashboard (30-60 seconds)
    ↓
Done! No configuration needed.
```

## Security Architecture

### One-Time Registration Token

**Problem:** If installer/image leaks, credentials could be stolen

**Solution:** Time-limited, single-use registration token

```
Server generates:
├─ agent_id: "agent-acme-hq-01"
├─ registration_token: "OTU_xYz..." (random 32 bytes)
├─ expires_at: now + 7 days
└─ status: "pending"

Agent uses registration_token ONCE to get permanent auth token:
1. Agent starts → reads .preauth file
2. POST /api/v1/agents/register with registration_token
3. Server validates: token valid? not expired? not used?
4. Server generates permanent_token, marks registration as used
5. Agent stores permanent_token, deletes .preauth
6. All future requests use permanent_token

If attacker gets installer after legitimate registration:
❌ Server rejects: "token already used"
🔔 Server alerts admin: "Duplicate registration attempt"
```

### Additional Security Layers

1. **IP Whitelisting** (optional)
   - Specify expected IP range during agent creation
   - Alert admin if registration from unexpected location

2. **Time-Limited Token**
   - Registration token expires in 7 days
   - Forces re-generation if not used promptly

3. **Hostname Validation** (optional)
   - Require specific hostname pattern
   - E.g., "ACME-*" or "HQ-PC-*"

4. **Revocation**
   - Admin can revoke registration token before use
   - Can revoke permanent token after registration

## Implementation Plan

### Phase 1: v0.6.0 - Core Packaging

**Windows Installer:**
- NSIS or WiX-based
- Embed config.ini with agent_id, server_url
- Embed .preauth with registration token
- Auto-install as Windows service
- Code signing (authenticode)

**Linux Shell Script:**
- Universal installer for x64 and ARM64
- Detect platform, download correct binary
- Configure systemd service
- Works for Raspberry Pi

**Server-Side:**
- Agent registration token system (database schema)
- Custom installer generator endpoint
- Admin UI for "Add Agent" workflow

### Phase 2: v0.7.0 - Advanced Features

**Raspberry Pi Image Generator:**
- Automate SD card image creation
- Base: Raspberry Pi OS Lite (64-bit)
- Pre-install agent binary
- Pre-configure network (DHCP)
- Enable SSH for troubleshooting (with secure key)

**macOS Installer:**
- Signed .pkg package
- Apple notarization
- LaunchDaemon configuration

**Multi-Tenant Isolation:**
- Customer and site management
- License tier enforcement
- Agent quota per customer

## Database Schema (v0.6.0)

```sql
-- Agent registration tokens
CREATE TABLE agent_registrations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id TEXT UNIQUE NOT NULL,
    registration_token TEXT UNIQUE NOT NULL,
    customer_id INTEGER NOT NULL,
    site_id INTEGER,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at DATETIME,                  -- NULL = not yet used
    used_by_hostname TEXT,
    used_by_ip TEXT,
    permanent_token TEXT,              -- Generated after registration
    status TEXT NOT NULL,              -- 'pending', 'registered', 'expired', 'revoked'
    
    FOREIGN KEY(customer_id) REFERENCES customers(id)
);

-- Customers (for multi-tenant support)
CREATE TABLE customers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    license_tier TEXT,                 -- 'basic', 'standard', 'enterprise'
    max_agents INTEGER,
    max_devices INTEGER,
    active BOOLEAN DEFAULT 1,
    created_at DATETIME NOT NULL
);

-- Sites (for organizing agents by location)
CREATE TABLE sites (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    customer_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    location TEXT,
    ip_whitelist TEXT,                 -- CIDR ranges, comma-separated
    created_at DATETIME NOT NULL,
    
    FOREIGN KEY(customer_id) REFERENCES customers(id)
);
```

## Installer File Structure

### Windows Installer Contents
```
printmaster-agent-acme-hq.exe (self-extracting)
├── installer.nsi (NSIS script)
├── printmaster-agent.exe
├── config.ini
│   [server]
│   server_enabled = true
│   server_url = https://yourserver.com:9090
│   agent_id = agent-acme-hq-01
├── .preauth
│   OTU_xYz_one_time_registration_token_here...
└── install-service.ps1
```

### Raspberry Pi Image Contents
```
printmaster-agent-acme-hq.img
├── /boot
│   ├── config.txt (optimized for headless)
│   └── cmdline.txt
├── /
│   ├── /usr/local/bin/printmaster-agent (ARM64 binary)
│   ├── /etc/printmaster/
│   │   ├── config.ini (pre-configured)
│   │   └── .preauth (one-time token)
│   ├── /etc/systemd/system/printmaster-agent.service
│   └── /home/pi/.ssh/authorized_keys (optional: for support access)
```

## Cost-Benefit Analysis

### Traditional PC-Based Deployment
- **Cost:** $0 hardware (use existing PC)
- **Risk:** PC turns off, rebuilt, software conflicts
- **Support:** ~1 hour/year troubleshooting per site
- **Professionalism:** Low (piggybacks on user hardware)

### Raspberry Pi Appliance
- **Cost:** $60-100 hardware per site
- **Risk:** Minimal (dedicated device, no user interference)
- **Support:** ~15 min/year (nearly zero maintenance)
- **Professionalism:** High (dedicated monitoring device)
- **Margin:** $40-50 profit per device
- **ROI:** Pays for itself in <1 hour of saved support time

## Marketing Positioning

### For MSPs:
- "Enterprise monitoring for small business budgets"
- "Plug-and-play printer monitoring appliance"
- "No client PC dependencies - works 24/7"
- "Saves 1-2 hours per deployment vs traditional methods"

### For End Customers:
- "Silent, maintenance-free monitoring device"
- "No software on your PCs"
- "24/7 printer health monitoring"
- "Catches problems before you run out of toner"

## Future Enhancements (Post-v0.7.0)

### Advanced Raspberry Pi Features:
- **LCD screen** (optional): Show status, device count, last sync
- **LED indicators**: Green = OK, Red = Issue, Blue = Syncing
- **Bluetooth**: Optional mobile app for on-site tech diagnostics
- **PoE HAT**: Power over Ethernet (eliminate power supply)
- **Dual ethernet**: Span monitoring (advanced network visibility)

### Cloud Images:
- Docker container for cloud deployment
- AWS/Azure/GCP marketplace listings
- Kubernetes helm charts

### OEM Opportunities:
- Partner with Raspberry Pi Foundation
- Custom-branded Pi cases with logo
- "PrintMaster Monitoring Appliance" branding
- Reseller program for other MSPs

---

## Implementation Checklist (v0.6.0)

### Server-Side
- [ ] Add agent_registrations table
- [ ] Add customers and sites tables
- [ ] Implement registration token generation
- [ ] Implement token validation (one-time use)
- [ ] Admin UI: "Add Agent" workflow
- [ ] Admin UI: Agent status dashboard
- [ ] Installer generator endpoint
- [ ] Windows .exe generation
- [ ] Linux shell script generation
- [ ] Audit logging for registrations
- [ ] Email/alert on duplicate registration attempts

### Agent-Side
- [ ] Read .preauth file on startup
- [ ] Implement one-time registration flow
- [ ] Store permanent token securely
- [ ] Delete .preauth after successful registration
- [ ] Handle registration failures gracefully

### Build System
- [ ] NSIS build script for Windows
- [ ] Shell script template for Linux/Pi
- [ ] Code signing integration (Windows)
- [ ] Automated binary hosting

### Documentation
- [ ] Deployment guide for MSPs
- [ ] Raspberry Pi setup instructions
- [ ] Troubleshooting guide
- [ ] Security best practices

### Testing
- [ ] Test registration token expiry
- [ ] Test duplicate registration rejection
- [ ] Test IP whitelisting
- [ ] Test cross-platform installers
- [ ] Load test: 1000 agents registering

---

**Document Version:** 1.0  
**Last Updated:** 2025-11-03  
**Status:** Planning Phase - Implementation Deferred to v0.6.0
