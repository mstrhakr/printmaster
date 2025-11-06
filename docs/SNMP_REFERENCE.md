# SNMP Reference Guide

This document covers SNMP discovery, Printer-MIB OIDs, vendor detection, metrics collection, and meter history tracking in PrintMaster.

---

## Overview

PrintMaster uses SNMP (Simple Network Management Protocol) to discover and monitor network printers. The implementation follows a **two-phase scan approach**:

1. **Phase 1 (Quick Probe)** - Small number of SNMP GETs to quickly verify device is a printer
2. **Phase 2 (Deep Scan)** - Targeted walk for confirmed printers to gather detailed metrics

This approach keeps discovery fast on large networks while still capturing rich vendor-specific data.

---

## Core Printer-MIB OIDs

### Standard Detection OIDs

**System Information:**
- `sysObjectID` - `1.3.6.1.2.1.1.2.0` - Vendor enterprise OID (identifies manufacturer)
- `sysDescr` - `1.3.6.1.2.1.1.1.0` - System description (fallback for vendor/model detection)
- `sysName` - `1.3.6.1.2.1.1.5.0` - Device hostname

**Printer-MIB (RFC 3805):**
- `prtGeneralSerialNumber` - `1.3.6.1.2.1.43.5.1.1.17.1` - Device serial number
- `prtMarkerLifeCount.1` - `1.3.6.1.2.1.43.10.2.1.4.1.1` - Total impressions (marker 1, typically black/mono)
- `prtMarkerSuppliesDescr.1` - `1.3.6.1.2.1.43.11.1.1.6.1.1` - Supply description (e.g., "Black Toner")
- `prtMarkerSuppliesLevel.1` - `1.3.6.1.2.1.43.11.1.1.9.1.1` - Supply level/remaining
- `prtMarkerSuppliesMaxCapacity.1` - `1.3.6.1.2.1.43.11.1.1.8.1.1` - Supply max capacity

**Additional Markers (for color printers):**
- Marker 2: `.1.3.6.1.2.1.43.10.2.1.4.1.2` - Combined color impressions
- Marker 3: `.1.3.6.1.2.1.43.10.2.1.4.1.3` - Cyan impressions
- Marker 4: `.1.3.6.1.2.1.43.10.2.1.4.1.4` - Magenta impressions
- Marker 5: `.1.3.6.1.2.1.43.10.2.1.4.1.5` - Yellow impressions
- Marker 6: `.1.3.6.1.2.1.43.10.2.1.4.1.6` - Additional vendor-specific marker

---

## Vendor Detection

### Vendor Enterprise OIDs

**Supported Vendors:**
- **HP**: `1.3.6.1.4.1.11.*`
- **Brother**: `1.3.6.1.4.1.2435.*`
- **Canon**: `1.3.6.1.4.1.1602.*`
- **Lexmark**: `1.3.6.1.4.1.641.*`
- **Epson**: `1.3.6.1.4.1.231.*`
- **Kyocera**: Detected via `sysDescr` containing "kyocera"
- **Xerox**: Detected via `sysDescr` or enterprise OID
- **Ricoh**: Detected via `sysDescr` or enterprise OID

**Unknown Manufacturers:**
When a device cannot be confidently identified, the agent logs to `logs/unknown_mfg.log`:
```
2025-11-06T10:30:00Z | 10.0.0.100 | sysObjectID: 1.3.6.1.4.1.9999.x.x | sysDescr: "Unknown Printer Model"
```
Use this file to identify new vendor enterprise OIDs and add them to vendor detection.

### Vendor-Specific Modules

PrintMaster includes vendor-specific modules for enhanced metrics:
- `agent/scanner/vendor/hp.go` - HP-specific OIDs and parsing
- `agent/scanner/vendor/canon.go` - Canon-specific OIDs
- `agent/scanner/vendor/brother.go` - Brother-specific OIDs
- `agent/scanner/vendor/generic.go` - Fallback for unknown vendors

Each module provides:
- Additional vendor OIDs for richer metrics
- Vendor-specific parsing logic
- Workarounds for vendor quirks

---

## Discovery Process

### Phase 1: Quick Probe (Fast Path)

**Goal**: Determine if device is a printer with minimal SNMP queries

**OIDs Queried** (5-10 queries):
1. `sysObjectID` - Identify vendor
2. `sysDescr` - Fallback vendor/model detection
3. `prtGeneralSerialNumber` - Confirms printer (Printer-MIB support)
4. `prtMarkerLifeCount.1` - Page count (marker 1)
5. `prtMarkerSuppliesLevel.1` - Toner level (marker 1)
6. Vendor-specific probe (if vendor detected)

**Decision**: If device responds to Printer-MIB OIDs → Proceed to Phase 2

---

### Phase 2: Deep Scan (Confirmed Printers)

**Goal**: Gather comprehensive metrics and capabilities

**Walks Performed**:
1. **Printer-MIB Walk** (bounded, ~100-200 OIDs):
   - All markers (mono + color impressions)
   - All supplies (toner, drum, fuser levels)
   - Input trays (paper levels, media types)
   - Device capabilities

2. **Vendor Enterprise Walk** (if vendor detected, bounded):
   - HP: Walk `1.3.6.1.4.1.11.2.3.9.4.2.*` (HP MIB subtree)
   - Canon/Brother/etc: Walk appropriate vendor subtree
   - Capped at 200 OIDs to keep discovery fast

**Walk Bounds**: All walks are intentionally capped (max 200 entries) to prevent network slowdowns on large deployments.

---

## Metrics Collection

### Normalized Meters

PrintMaster normalizes vendor-specific counters into a standard `meters` map:

```json
{
  "total_pages": 123456,        // Lifetime total pages
  "mono_pages": 100000,          // Monochrome impressions
  "black": 100000,               // Alias for mono_pages
  "color_pages": 23456,          // Combined color impressions
  "cyan": 8000,                  // Cyan marker count
  "magenta": 7800,               // Magenta marker count
  "yellow": 7656,                // Yellow marker count
  "copier_pages": 5000,          // Copier function pages
  "printer_pages": 118456,       // Printer function pages
  "fax_pages": 0,                // Fax function pages
  "scan_pages": 0,               // Scanner function pages
  "local_pages": 0,              // Local copy pages
  "banner_pages": 0              // Banner/poster pages
}
```

**Source Priority**:
1. Explicit page count OID (if available)
2. Sum of marker counters (prtMarkerLifeCount)
3. Vendor-specific counters with keyword matching

**Keyword Heuristics**: Parser scans PDU descriptions for keywords like "copier", "printer", "fax", "scan" to map to PrintAudit-like categories.

---

## Meter History & Time-Series Data

### Design

**Goal**: Track impressions over time for:
- "Impressions today" metric
- Time-window deltas (1 day, 7 day, 30 day)
- Charts and trend analysis

**Implementation**: Timestamped full snapshots with computed deltas

```json
{
  "printer_info": {
    "meters": { "total_pages": 123456, ... },
    "meter_history": [
      {
        "ts": "2025-11-01T10:00:00Z",
        "source": "mib_walk_10_2_106_72_20251101T100000.json",
        "meters": { "total_pages": 120000, ... },
        "deltas": { "total_pages": 500 }  // vs previous snapshot
      },
      {
        "ts": "2025-11-02T10:00:00Z",
        "source": "refresh",
        "meters": { "total_pages": 121000, ... },
        "deltas": { "total_pages": 1000 }  // 1000 pages in 24h
      }
    ]
  }
}
```

### Retention Policy

**Default Configuration**:
- **Hourly snapshots**: Last 48 hours (high resolution)
- **Daily snapshots**: Last 365 days (one per day)
- **Older data**: Pruned automatically

**Configurable**: Adjust retention windows via developer settings

### Why Snapshots + Deltas?

**Advantages**:
- **Resilient**: Can recompute deltas if normalization changes
- **Flexible**: Lifetime counters preserved for long-term analysis
- **Fast reads**: Deltas pre-computed for common "impressions today" queries

**Alternative (deltas-only)**: Would lose lifetime counters, limiting flexibility

---

## Device Storage

### File Structure

Devices stored in `logs/devices/<serial>.json`:

```json
{
  "serial": "JPBCD12345",
  "properties": {
    "created_at": "2025-10-30T12:00:00Z",
    "modified_at": "2025-11-01T15:30:00Z",
    "last_walk": "logs/mib_walk_10_2_106_72_20251101T153000.json",
    "oid_count": 832,
    "missing_fields": 1
  },
  "printer_info": {
    "ip": "10.2.106.72",
    "manufacturer": "HP",
    "model": "LaserJet Pro M404n",
    "serial": "JPBCD12345",
    "hostname": "printer-office-1",
    "meters": { "total_pages": 123456, ... }
  },
  "changelog": [
    {
      "timestamp": "2025-11-01T15:30:00Z",
      "changes": { "total_pages": { "old": 123000, "new": 123456 } }
    }
  ],
  "reference_walk": [...],
  "reference_walk_files": ["mib_walk_10_2_106_72_20251030T120000.json"]
}
```

### Database Storage (Current)

**Since v0.3.x**: Devices stored in SQLite database (`devices.db`)

**Key Tables**:
- `devices` - Device inventory (27 fields, NO page_count/toner_levels)
- `metrics_raw` - 5-minute snapshots (page counts, toner levels)
- `metrics_hourly` - 1-hour aggregates
- `metrics_daily` - 1-day aggregates
- `metrics_monthly` - 1-month aggregates

**See**: `docs/API.md` for complete database schema

---

## SNMP Configuration

### Settings (Configurable via UI)

**Connection:**
- SNMP Port: `161` (default)
- SNMP Community: `"public"` (default)
- SNMP Version: `v2c` (default)

**Timing:**
- SNMP Timeout: `2000ms` (default, configurable)
- SNMP Retries: `1` (default, configurable)
- SNMP Delay Between Queries: Not implemented yet

**Advanced:**
- Enable SNMP Bulk GET: Not implemented yet
- SNMP Result Cache + TTL: Not implemented yet
- SNMPv3 Support: Not implemented yet (planned for v0.5.0)

**See**: `docs/ROADMAP.md` for implementation status

---

## API Endpoints

### Get Device Meters

**Planned**: `GET /devices/{serial}/meters`

**Response**:
```json
{
  "current": {
    "total_pages": 123456,
    "mono_pages": 100000,
    "color_pages": 23456
  },
  "history": [...],
  "aggregates": {
    "last_24h": 500,
    "last_7d": 3500,
    "last_30d": 15000
  }
}
```

**Status**: Endpoint not yet implemented (planned for v0.5.0)

**Current Alternative**: Use `GET /devices/get?serial=XXX` and parse `raw_data`

---

## Merge Behavior

### Concurrent Merge Protection

**Challenge**: Multiple scans/refreshes could corrupt device files

**Solution**: Merge queue with per-serial locks
- All merges for a device are serialized
- Concurrent scans for different devices run in parallel
- Prevents race conditions and corruption

### Merge Process

**Steps**:
1. Parse PDUs into `PrinterInfo` (including `Meters`)
2. Load existing device profile (if any)
3. Compute changelog diff
4. Append changelog entry
5. Append `meter_history` snapshot with deltas
6. Persist device file atomically (write to temp, rename)

**Database Merge** (Current):
- Updates device record in `devices` table
- Creates snapshot in `metrics_raw` table
- Updates `last_seen` timestamp
- Creates `scan_history` entry

---

## UI Display

### Current Device Card

Displays:
- Manufacturer, Model, Serial
- IP Address, Hostname
- Firmware version
- Current toner levels (from latest `metrics_raw`)
- Last seen timestamp

### Planned Metrics Display

Will add:
- **Impressions Today**: Delta from last 24h
- **Weekly Impressions**: Delta from last 7 days
- **Chart**: Line chart of daily impressions (Chart.js or similar)

**Endpoint**: Will use `/devices/{serial}/meters` when implemented

---

## Testing Notes

### Mock SNMP Client

**Recommendation**: Use `SNMPClient` interface with mock implementation for tests

**Example**:
```go
type MockSNMPClient struct {
    responses map[string]gosnmp.SnmpPDU
}

func (m *MockSNMPClient) Get(oids []string) (*gosnmp.SnmpPacket, error) {
    // Return mock PDUs without network access
}
```

**Benefits**:
- Tests run without network
- Deterministic results
- Faster CI/CD
- Can simulate vendor-specific quirks

---

## Adding New Vendor Support

### Steps

1. **Identify Vendor Enterprise OID**:
   - Check `logs/unknown_mfg.log` for `sysObjectID`
   - Research vendor's SNMP MIB documentation

2. **Add to Vendor Detection**:
   ```go
   // agent/scanner/vendor/registry.go
   case strings.Contains(sysOID, "1.3.6.1.4.1.XXXX"):
       return "VendorName"
   ```

3. **Create Vendor Module** (optional):
   ```go
   // agent/scanner/vendor/vendorname.go
   func GetVendorNameOIDs() []string {
       return []string{
           "1.3.6.1.4.1.XXXX.specific.oid",
           // Vendor-specific OIDs
       }
   }
   ```

4. **Test with Real Device**:
   - Run discovery against actual printer
   - Verify metrics collected correctly
   - Document any vendor quirks

5. **Add Unit Tests**:
   - Mock SNMP responses
   - Test parsing logic
   - Verify normalization

---

## Deprecated Features

### ❌ On-Demand MIB Walk Endpoint

**Status**: Removed  
**Former Endpoint**: `POST /mib_walk`

**Rationale**: 
- Encouraged unbounded walks (network performance issues)
- Replaced by targeted walks in discovery pipeline

**Migration**: Use discovery flow and "Walk All" device action in UI (triggers bounded, targeted walks)

### ❌ External MIB File Ingestion

**Status**: Never implemented / explicitly avoided  
**Rationale**: Keep agent lean, avoid tight coupling to vendor-specific files

**Approach**: Self-contained probe OIDs in code, prefer Printer-MIB, minimal vendor-specific additions

---

## Next Steps & Improvements

### Planned Enhancements (v0.4-v0.6)

- [ ] **SNMPv3 Support** - Auth/priv encryption (v0.5.0)
- [ ] **SNMP Bulk GET** - Performance improvement for walks (v0.5.0)
- [ ] **SNMP Result Cache** - Reduce redundant queries (v0.5.0)
- [ ] **Meter History API** - `/devices/{serial}/meters` endpoint (v0.6.0)
- [ ] **Chart UI** - Impressions over time visualization (v0.6.0)
- [ ] **More Vendor Modules** - Expand Kyocera, Xerox, Ricoh support

### Development Best Practices

1. **Keep walks bounded** - Max 200 OIDs per walk
2. **Prefer Printer-MIB** - Standard OIDs over vendor-specific
3. **Mock SNMP in tests** - No network in CI
4. **Log unknown vendors** - Feed into vendor detection improvements
5. **Document quirks** - Note vendor-specific behaviors

---

## Reference Links

**RFCs:**
- [RFC 3805 - Printer MIB v2](https://www.rfc-editor.org/rfc/rfc3805)
- [RFC 1213 - MIB-II (System Group)](https://www.rfc-editor.org/rfc/rfc1213)

**Internal Documentation:**
- `docs/API.md` - API endpoints and database schema
- `docs/ROADMAP.md` - Feature implementation timeline
- `docs/vendor/` - Vendor-specific OID mappings (Epson, Kyocera, etc.)

**Code Locations:**
- `agent/scanner/` - SNMP scanner implementation
- `agent/scanner/vendor/` - Vendor-specific modules
- `agent/storage/` - Database and device storage

---

*Last Updated: November 6, 2025*  
*Current Version: 0.3.3*
