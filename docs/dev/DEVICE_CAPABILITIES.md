# Device Capabilities Detection Plan

## Overview

Add confidence-based capability detection to intelligently determine device features (print, copy, scan, fax, color, duplex) and gate UI/API functionality based on those capabilities. This prevents wasted SNMP queries (e.g., querying color pages on mono device) and provides better UX.

## Current State

### What We Have
- **IsPrinter Detection**: Boolean detection in scanner pipeline
- **Vendor Detection**: Auto-detect HP, Canon, Brother, etc. via sysObjectID
- **Metrics Collection**: Various counters (pages, color, mono, duplex, scan, fax)
- **Device Storage**: Basic device info in SQLite

### What's Missing
- No capability detection (mono/color, copier, scanner, fax)
- UI queries all metrics regardless of device capabilities
- No confidence scoring for capabilities
- No way to filter/search by capability
- Duplicate effort querying unavailable features

## Proposed Capability System

### Core Capabilities

```go
type DeviceCapabilities struct {
    // Confidence scores 0.0-1.0
    PrinterConfidence  float64 `json:"printer_confidence"`   // Can print
    CopierConfidence   float64 `json:"copier_confidence"`    // Has copy function
    ScannerConfidence  float64 `json:"scanner_confidence"`   // Can scan (standalone or MFP)
    FaxConfidence      float64 `json:"fax_confidence"`       // Has fax capability
    
    // Print capabilities
    ColorConfidence    float64 `json:"color_confidence"`     // Color printing
    MonoConfidence     float64 `json:"mono_confidence"`      // Monochrome only
    DuplexConfidence   float64 `json:"duplex_confidence"`    // Duplex/2-sided printing
    
    // Derived booleans (from confidence thresholds)
    IsPrinter   bool `json:"is_printer"`    // printer_confidence >= 0.7
    IsCopier    bool `json:"is_copier"`     // copier_confidence >= 0.7
    IsScanner   bool `json:"is_scanner"`    // scanner_confidence >= 0.7
    IsFax       bool `json:"is_fax"`        // fax_confidence >= 0.7
    IsColor     bool `json:"is_color"`      // color_confidence >= 0.7
    IsMono      bool `json:"is_mono"`       // mono_confidence >= 0.7
    HasDuplex   bool `json:"has_duplex"`    // duplex_confidence >= 0.7
    
    // Classification helpers
    DeviceType  string `json:"device_type"`  // "Printer", "MFP", "Scanner", "Copier/Printer", etc.
}
```

## Detection Strategy

### 1. Printer Detection (Already Exists)

**Evidence Sources**:
- ✅ Serial number present (1.3.6.1.2.1.43.5.1.1.17.1)
- ✅ Printer-MIB OIDs respond
- ✅ Open printer ports (9100, 515, 631)
- ✅ sysDescr contains "printer" keywords
- ✅ Vendor enterprise OID known printer manufacturer

**Confidence Calculation**:
```
confidence = 0.0
if serial_found:        confidence += 0.5
if printer_oids_found:  confidence += 0.3
if open_print_ports:    confidence += 0.1
if vendor_match:        confidence += 0.1
threshold = 0.7
```

### 2. Color vs Mono Detection (NEW)

**Evidence Sources**:

**Strong Evidence**:
- `prtMarkerColorantValue` (1.3.6.1.2.1.43.12.1.1.4) - Colorant names
  - If contains ["Cyan", "Magenta", "Yellow"] → **Color (0.9)**
  - If only ["Black", "Black Cartridge"] → **Mono (0.9)**
- Device model/description keywords:
  - "Color", "CLX", "CLP", "C5" → **Color (0.7)**
  - "Mono", "Monochrome", "B&W" → **Mono (0.8)**
- Color page counter exists (vendor-specific):
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.7.0
  - If color_pages > 0 → **Color (0.8)**
  - If color_pages == 0 but counter exists → **Mono (0.6)** (might not have printed color yet)

**Weak Evidence**:
- Number of consumables:
  - 4+ consumables → **Color (0.5)** (likely CMYK)
  - 1-2 consumables → **Mono (0.5)** (black + drum)
- Supply descriptions:
  - Contains "Cyan", "Magenta" → **Color (0.6)**
  - Only "Black" → **Mono (0.4)** (not definitive)

**Confidence Calculation**:
```go
func DetectColorCapability(pdus []SnmpPDU, model string) (colorConf, monoConf float64) {
    colorScore := 0.0
    monoScore := 0.0
    
    // Check colorant names (strongest signal)
    colorants := extractColorants(pdus)
    if containsAny(colorants, []string{"Cyan", "Magenta", "Yellow"}) {
        colorScore += 0.9
    } else if onlyContains(colorants, []string{"Black", "black"}) {
        monoScore += 0.9
    }
    
    // Check model keywords
    modelLower := strings.ToLower(model)
    if containsAny(modelLower, []string{"color", "clx", "clp"}) {
        colorScore += 0.7
    }
    if containsAny(modelLower, []string{"mono", "monochrome", "b&w"}) {
        monoScore += 0.8
    }
    
    // Check color page counter existence
    if hasOID(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.7.0") {
        colorPages := getOIDValue(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.7.0")
        if colorPages > 0 {
            colorScore += 0.8
        } else {
            monoScore += 0.3  // Might not have printed color yet
        }
    }
    
    // Check consumable count (weak signal)
    consumables := extractConsumables(pdus)
    if len(consumables) >= 4 {
        colorScore += 0.3
    } else if len(consumables) <= 2 {
        monoScore += 0.3
    }
    
    // Normalize scores to 0.0-1.0
    colorConf = min(colorScore, 1.0)
    monoConf = min(monoScore, 1.0)
    
    return colorConf, monoConf
}
```

### 3. Copier Detection (NEW)

**Evidence Sources**:

**Strong Evidence**:
- Copy page counter exists:
  - Standard: 1.3.6.1.2.1.43.10.2.1.4.1.1.2 (copy counter)
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.8.0 (copy pages)
  - If counter > 0 → **Copier (0.9)**
  - If counter exists but == 0 → **Copier (0.7)** (hasn't been used yet)
- Scan counters exist (copy requires scan):
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.x (scan counters)
  - Canon: 1.3.6.1.4.1.1602.1.1.1.4.1.x (scan counters)
  - If scan counters exist → **Copier (0.6)** (MFP can copy)
- Model contains "MFP", "Multifunction", "All-in-One":
  - → **Copier (0.8)**

**Weak Evidence**:
- Device has ADF (automatic document feeder):
  - prtInputType contains "autoDocumentFeeder"
  - → **Copier (0.4)** (AFD common in copiers/MFPs)
- sysDescr contains "MFP", "multifunction":
  - → **Copier (0.5)**

**Confidence Calculation**:
```go
func DetectCopierCapability(pdus []SnmpPDU, model string, sysDescr string) float64 {
    score := 0.0
    
    // Check for copy counters (strongest)
    if hasOID(pdus, HP_COPY_PAGES_OID) {
        copyPages := getOIDValue(pdus, HP_COPY_PAGES_OID)
        if copyPages > 0 {
            score += 0.9
        } else {
            score += 0.7
        }
    }
    
    // Check for scan counters (implies can copy)
    if hasAnyOID(pdus, HP_SCAN_OIDS) || hasAnyOID(pdus, CANON_SCAN_OIDS) {
        score += 0.6
    }
    
    // Check model keywords
    modelLower := strings.ToLower(model)
    if containsAny(modelLower, []string{"mfp", "multifunction", "all-in-one"}) {
        score += 0.8
    }
    
    // Check for ADF (weak signal)
    if hasADF(pdus) {
        score += 0.4
    }
    
    return min(score, 1.0)
}
```

### 4. Scanner Detection (NEW)

**Two Scenarios**:
1. **Standalone Scanner**: Can scan but NOT print (rare in enterprise)
2. **MFP Scanner**: Can scan AND print (most common)

**Evidence Sources**:

**Strong Evidence**:
- Scan counter exists and > 0:
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.x
  - Canon: 1.3.6.1.4.1.1602.1.1.1.4.1.x
  - → **Scanner (0.9)**
- Scanner-specific OIDs respond:
  - Scanner object ID prefix (1.3.6.1.4.1.xxx for scanner vendors)
  - → **Scanner (0.8)**
- Model contains "Scanner", "ScanStation":
  - → **Scanner (0.8)**

**Weak Evidence**:
- Has ADF but no print capability:
  - hasADF && !isPrinter → **Scanner (0.7)**
- sysDescr contains "scanner":
  - → **Scanner (0.5)**

**Special Case**:
```go
// Standalone scanner (rare)
if scannerConf >= 0.7 && printerConf < 0.3 {
    deviceType = "Scanner"  // Pure scanner, no print
    // Only collect scan metrics, skip print/copy
}

// MFP with scan capability
if scannerConf >= 0.7 && printerConf >= 0.7 {
    deviceType = "MFP"  // Multifunction
    // Collect all metrics
}
```

### 5. Fax Detection (NEW)

**Evidence Sources**:

**Strong Evidence**:
- Fax page counter exists and > 0:
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.5.0
  - → **Fax (0.9)**
- Fax-specific OIDs respond:
  - Fax scan counters (HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.2.x)
  - → **Fax (0.8)**
- Model contains "Fax", "MFP" (MFP often has fax):
  - → **Fax (0.6)**

**Weak Evidence**:
- Has fax capability in sysDescr/model:
  - → **Fax (0.4)**

### 6. Duplex Detection (NEW)

**Evidence Sources**:

**Strong Evidence**:
- Duplex counter exists and > 0:
  - HP: 1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.36.0
  - Standard: prtOutputDuplexed
  - → **Duplex (0.9)**
- Duplex unit present:
  - prtOutputCapacityUnit contains "duplexer"
  - → **Duplex (0.8)**
- Model contains "Duplex", "2-sided", "D" suffix:
  - "LaserJet Pro M404dn" (n=network, d=duplex)
  - → **Duplex (0.7)**

**Weak Evidence**:
- Device capabilities string mentions duplex:
  - → **Duplex (0.5)**

## Database Schema Changes

### Add to Device Table

```sql
ALTER TABLE devices ADD COLUMN capabilities TEXT;  -- JSON of DeviceCapabilities
ALTER TABLE devices ADD COLUMN device_type TEXT;   -- "Printer", "MFP", "Scanner", "Copier"
```

### Indexes for Filtering

```sql
CREATE INDEX idx_devices_capabilities ON devices((json_extract(capabilities, '$.is_color')));
CREATE INDEX idx_devices_device_type ON devices(device_type);
```

## API Changes

### Device Response Enhancement

```json
{
  "serial": "JPBHM12345",
  "model": "HP LaserJet Pro M479fdw",
  "capabilities": {
    "printer_confidence": 1.0,
    "copier_confidence": 0.9,
    "scanner_confidence": 0.9,
    "fax_confidence": 0.8,
    "color_confidence": 0.95,
    "mono_confidence": 0.05,
    "duplex_confidence": 0.9,
    
    "is_printer": true,
    "is_copier": true,
    "is_scanner": true,
    "is_fax": true,
    "is_color": true,
    "is_mono": false,
    "has_duplex": true,
    
    "device_type": "Color MFP"
  }
}
```

### Filter Endpoints

```
GET /api/devices?is_color=true              # Only color devices
GET /api/devices?device_type=MFP            # Only multifunction devices
GET /api/devices?is_copier=true             # Has copy capability
GET /api/devices?is_mono=true               # Mono only
GET /api/devices?has_duplex=true            # Duplex capable
```

## UI Enhancements

### Device Cards

```html
<!-- Show badges based on capabilities -->
<div class="device-badges">
  <span v-if="device.capabilities.is_color" class="badge badge-color">Color</span>
  <span v-if="device.capabilities.is_mono" class="badge badge-mono">Mono</span>
  <span v-if="device.capabilities.is_copier" class="badge badge-copy">Copy</span>
  <span v-if="device.capabilities.is_scanner" class="badge badge-scan">Scan</span>
  <span v-if="device.capabilities.is_fax" class="badge badge-fax">Fax</span>
  <span v-if="device.capabilities.has_duplex" class="badge badge-duplex">Duplex</span>
</div>
```

### Conditional Metrics Display

```javascript
// Only show color metrics if device is color-capable
if (device.capabilities.is_color) {
    displayMetric("Color Pages", metrics.color_pages);
}

// Only show mono metrics if device is mono
if (device.capabilities.is_mono) {
    displayMetric("Mono Pages", metrics.mono_pages);
}

// Only show copy metrics if device is copier
if (device.capabilities.is_copier) {
    displayMetric("Copy Pages", metrics.copy_pages);
}

// Only show scan metrics if device is scanner
if (device.capabilities.is_scanner) {
    displayMetric("Scan Count", metrics.scan_count);
}

// Only show fax metrics if device has fax
if (device.capabilities.is_fax) {
    displayMetric("Fax Pages", metrics.fax_pages);
}

// Only show duplex metrics if device has duplex
if (device.capabilities.has_duplex) {
    displayMetric("Duplex Sheets", metrics.duplex_sheets);
}
```

### Filter Controls

```html
<div class="filters">
  <label><input type="checkbox" name="is_color"> Color Only</label>
  <label><input type="checkbox" name="is_mono"> Mono Only</label>
  <label><input type="checkbox" name="is_copier"> Has Copy</label>
  <label><input type="checkbox" name="is_scanner"> Has Scan</label>
  <label><input type="checkbox" name="is_fax"> Has Fax</label>
  <label><input type="checkbox" name="has_duplex"> Has Duplex</label>
  
  <select name="device_type">
    <option value="">All Types</option>
    <option value="Printer">Printer Only</option>
    <option value="MFP">MFP</option>
    <option value="Scanner">Scanner Only</option>
    <option value="Copier/Printer">Copier/Printer</option>
  </select>
</div>
```

## Scanner Package Changes

### New File: `scanner/capabilities.go`

```go
package scanner

type DeviceCapabilities struct {
    PrinterConfidence float64
    CopierConfidence  float64
    ScannerConfidence float64
    FaxConfidence     float64
    ColorConfidence   float64
    MonoConfidence    float64
    DuplexConfidence  float64
    
    // Derived
    IsPrinter  bool
    IsCopier   bool
    IsScanner  bool
    IsFax      bool
    IsColor    bool
    IsMono     bool
    HasDuplex  bool
    DeviceType string
}

func DetectCapabilities(pdus []SnmpPDU, model, sysDescr, vendor string) DeviceCapabilities {
    caps := DeviceCapabilities{}
    
    // Detect each capability
    caps.PrinterConfidence = detectPrinter(pdus, model, sysDescr)
    caps.ColorConfidence, caps.MonoConfidence = detectColor(pdus, model)
    caps.CopierConfidence = detectCopier(pdus, model, sysDescr)
    caps.ScannerConfidence = detectScanner(pdus, model, sysDescr, caps.PrinterConfidence)
    caps.FaxConfidence = detectFax(pdus, model)
    caps.DuplexConfidence = detectDuplex(pdus, model)
    
    // Apply threshold (0.7 = confident)
    caps.IsPrinter = caps.PrinterConfidence >= 0.7
    caps.IsColor = caps.ColorConfidence >= 0.7
    caps.IsMono = caps.MonoConfidence >= 0.7
    caps.IsCopier = caps.CopierConfidence >= 0.7
    caps.IsScanner = caps.ScannerConfidence >= 0.7
    caps.IsFax = caps.FaxConfidence >= 0.7
    caps.HasDuplex = caps.DuplexConfidence >= 0.7
    
    // Determine device type
    caps.DeviceType = classifyDeviceType(caps)
    
    return caps
}

func classifyDeviceType(caps DeviceCapabilities) string {
    // Standalone scanner (rare)
    if caps.IsScanner && !caps.IsPrinter {
        return "Scanner"
    }
    
    // MFP (multifunction: print + copy/scan)
    if caps.IsPrinter && (caps.IsCopier || caps.IsScanner) {
        if caps.IsColor {
            return "Color MFP"
        }
        return "Mono MFP"
    }
    
    // Printer with copy but no scan (copier/printer combo)
    if caps.IsPrinter && caps.IsCopier && !caps.IsScanner {
        return "Copier/Printer"
    }
    
    // Simple printer
    if caps.IsPrinter {
        if caps.IsColor {
            return "Color Printer"
        }
        return "Mono Printer"
    }
    
    return "Unknown"
}
```

## Implementation Phases

### Phase 1: Core Capability Detection
1. Create `scanner/capabilities.go` with detection functions
2. Add `DeviceCapabilities` struct to storage `Device`
3. Update scanner `QueryDevice` to call `DetectCapabilities`
4. Store capabilities in device JSON field
5. Write tests for each detection function

### Phase 2: Database & API
1. Add `capabilities` and `device_type` columns to devices table
2. Create migration for existing devices (re-detect from SNMP data)
3. Update API endpoints to return capabilities
4. Add filter parameters to `/api/devices`
5. Create indexes for capability-based queries

### Phase 3: UI Integration
1. Add capability badges to device cards
2. Implement conditional metrics display
3. Add filter controls for capabilities
4. Update search/filter logic
5. Add device type icons

### Phase 4: Optimization
1. Skip metrics queries based on capabilities (e.g., no color queries on mono device)
2. Cache capability detection results
3. Add capability-based SNMP query profiles (mono_printer, color_mfp, etc.)
4. Metrics collection optimization (only query relevant counters)

## Benefits

### Performance
- **Reduce SNMP queries**: Skip irrelevant metrics (e.g., color pages on mono printer)
- **Faster scans**: Profile-based OID lists per device type
- **Less network traffic**: Only query what device supports

### User Experience
- **Better filtering**: Find all color MFPs, mono printers, etc.
- **Cleaner UI**: Hide irrelevant metrics (no "Color Pages: 0" on mono printer)
- **Accurate data**: Device type clearly labeled

### Data Quality
- **Confidence scores**: Know certainty of detection
- **Type classification**: Clear device categorization
- **Capability tracking**: Historical capability changes (device upgrade)

## Open Questions

1. **Threshold Tuning**: Is 0.7 the right confidence threshold? Should it be configurable?
2. **Capability Changes**: How to handle device upgrades (e.g., adds color kit)?
3. **Unknown Devices**: How to handle low-confidence devices (all scores < 0.5)?
4. **Manual Override**: Should users be able to manually set capabilities?
5. **Vendor-Specific**: Do we need per-vendor detection logic (HP vs Canon)?

## Testing Strategy

### Unit Tests
- Test each detection function with known SNMP data
- Test edge cases (no colorants, empty model, etc.)
- Test confidence score calculations
- Test device type classification

### Integration Tests
- Test full capability detection with real device data
- Test capability-based filtering
- Test conditional metrics display
- Test capability persistence and retrieval

### Regression Tests
- Ensure existing printer detection still works
- Ensure no breaking changes to API
- Ensure metrics collection still works

## Migration Plan

### Existing Devices
1. Run capability detection on all existing devices in database
2. Update capabilities field for each device
3. Backfill device_type based on capabilities
4. Log devices with low confidence for manual review

### Backward Compatibility
- Keep existing boolean `is_saved`, `visible` fields
- Add capabilities as optional field (null for legacy)
- Gradual migration: detect on next scan if capabilities missing
- API returns empty capabilities object if not detected yet

## Next Steps

1. **Review & Approve**: Review this plan, adjust confidence thresholds
2. **Prototype**: Implement Phase 1 (core detection) with HP devices
3. **Test**: Gather real-world data, tune confidence scores
4. **Expand**: Add vendor-specific detection logic (Canon, Brother, etc.)
5. **UI**: Implement capability badges and filtering
6. **Optimize**: Add profile-based SNMP querying

## Related Documentation

- [Scanner Module](../agent/scanner/README.md) - SNMP querying
- [Storage Module](../agent/storage/README.md) - Database schema
- [API Reference](API_REFERENCE.md) - HTTP endpoints
