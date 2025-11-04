# Capability-Aware Scanning Integration

## Overview

The scanner now automatically detects device capabilities (color, duplex, scanner, copier, fax) and uses this information to optimize SNMP queries. This prevents wasted queries for features a device doesn't have (e.g., querying color metrics on mono printers).

## Usage Flow

### 1. Initial Discovery (QueryFull)

```go
// Perform full SNMP walk to discover device
result, err := scanner.QueryDevice(ctx, "10.0.0.100", scanner.QueryFull, "HP", 15)
if err != nil {
    log.Fatal(err)
}

// Capabilities are automatically detected and stored in result
caps := result.Capabilities
fmt.Printf("Device Type: %s\n", caps.DeviceType)
fmt.Printf("Is Color: %v (confidence: %.2f)\n", caps.IsColor, caps.Scores["color"])
fmt.Printf("Is Copier: %v (confidence: %.2f)\n", caps.IsCopier, caps.Scores["copier"])
fmt.Printf("Has Duplex: %v (confidence: %.2f)\n", caps.HasDuplex, caps.Scores["duplex"])
```

**Output Example:**
```
Device Type: Color MFP
Is Color: true (confidence: 0.95)
Is Copier: true (confidence: 0.90)
Has Duplex: true (confidence: 0.85)
```

### 2. Optimized Metrics Collection (QueryMetrics)

```go
// Use detected capabilities for optimized queries
result, err := scanner.QueryDeviceWithCapabilities(
    ctx,
    "10.0.0.100",
    scanner.QueryMetrics,
    "HP",
    5,
    caps, // Pass capabilities from initial discovery
)

// Only relevant OIDs were queried (no color OIDs on mono devices)
metrics := vendor.GetVendor("HP").ExtractMetrics(result.PDUs)
fmt.Printf("Page Count: %d\n", metrics.PageCount)
```

### 3. Backward Compatibility

```go
// Old code still works (queries all OIDs)
result, err := scanner.QueryDevice(ctx, "10.0.0.100", scanner.QueryMetrics, "HP", 5)
```

## Query Optimization Examples

### Example 1: Mono Printer (HP LaserJet Pro M404dn)

**Without Capabilities:**
- Queries 14 OIDs (including color, scan, copy, fax OIDs)
- Wasted queries: ~50%

**With Capabilities:**
```go
caps.IsColor = false
caps.IsScanner = false
caps.IsCopier = false
caps.IsFax = false
caps.HasDuplex = true
```

**Optimized OIDs (7 OIDs, 50% reduction):**
- ✅ Total pages
- ✅ Toner levels
- ✅ Toner descriptions
- ✅ Jam events
- ✅ Duplex sheets (has duplex)
- ❌ Fax pages (no fax)
- ❌ Scan counters (no scanner)

### Example 2: Color MFP (HP Color LaserJet Pro M479fdw)

**Without Capabilities:**
- Queries 14 OIDs

**With Capabilities:**
```go
caps.IsColor = true
caps.IsScanner = true
caps.IsCopier = true
caps.IsFax = true
caps.HasDuplex = true
```

**Optimized OIDs (14 OIDs, all relevant):**
- ✅ All OIDs queried (device has all features)

### Example 3: Simple Mono Printer (No MFP features)

**Without Capabilities:**
- Queries 14 OIDs
- ~70% wasted (querying scan/copy/fax/duplex OIDs that don't exist)

**With Capabilities:**
```go
caps.IsColor = false
caps.IsScanner = false
caps.IsCopier = false
caps.IsFax = false
caps.HasDuplex = false
```

**Optimized OIDs (4 OIDs, 71% reduction):**
- ✅ Total pages
- ✅ Toner levels
- ✅ Toner descriptions
- ✅ Jam events

## Architecture

### Capability Detection Pipeline

```
QueryFull
   ↓
Extract PDUs
   ↓
DetectionEvidence (sysDescr, sysOID, model, vendor, PDUs)
   ↓
CapabilityRegistry.DetectAll()
   ↓
Run all detectors (printer, color, mono, copier, scanner, fax, duplex)
   ↓
Calculate confidence scores (0.0-1.0)
   ↓
Apply thresholds (default 0.7)
   ↓
DeviceCapabilities (booleans + scores + device type)
   ↓
Store in QueryResult.Capabilities
```

### Metrics Query Pipeline

```
QueryMetricsWithCapabilities(caps)
   ↓
Build OID list
   ↓
VendorModule.GetCapabilityAwareMetricsOIDs(caps)
   ↓
Filter OIDs based on capabilities
   ↓
SNMP GET (only relevant OIDs)
   ↓
Parse metrics
```

## Vendor Module Implementation

### HP Vendor (Most Optimized)

```go
func (v *HPVendor) GetCapabilityAwareMetricsOIDs(caps) []string {
    oids := []string{
        // Always query: pages, toner, jams
    }
    
    if caps.HasDuplex {
        oids = append(oids, "...duplex_sheets")
    }
    
    if caps.IsFax {
        oids = append(oids, "...fax_pages", "...fax_scans")
    }
    
    if caps.IsScanner || caps.IsCopier {
        oids = append(oids, "...scan_counters", "...scanner_jams")
    }
    
    return oids
}
```

**Optimization:**
- Mono printer: Queries 4 OIDs (no fax/scan/copy/duplex)
- Mono MFP: Queries 7 OIDs (adds scan counters)
- Color MFP with fax: Queries all 14 OIDs

### Generic/Canon/Brother Vendors

Currently return all OIDs (no vendor-specific capability filtering yet):
```go
func (v *GenericVendor) GetCapabilityAwareMetricsOIDs(caps) []string {
    return v.GetMetricsOIDs() // No capability filtering
}
```

**Future Enhancement:**
Add color page counter filtering based on `caps.IsColor`.

## Next Steps

1. **Storage Integration**: Add `capabilities` JSON column to `devices` table
2. **API Integration**: Expose capabilities in device info endpoints
3. **UI Integration**: Show capability badges on device cards
4. **Advanced Filtering**: Add more vendor-specific capability-aware OID sets
5. **Metrics**: Track query time savings from capability optimization

## Benefits

### Performance
- **30-50% fewer SNMP queries** for targeted devices (mono printers, simple printers)
- **Faster metrics collection** (less network overhead, faster response)
- **Reduced device load** (fewer OID lookups on device)

### Accuracy
- **No more "missing data" false alarms** for features device doesn't have
- **Cleaner metrics** (only shows relevant counters in UI)
- **Better error handling** (don't retry queries for non-existent features)

### User Experience
- **Device type classification** ("Color MFP", "Mono Printer", "Scanner")
- **Capability badges** in UI (Color, Duplex, Fax icons)
- **Conditional UI elements** (hide scan/copy tabs on simple printers)
- **Search/filter by capability** ("Show only color MFPs")

## Testing

Run existing tests to verify backward compatibility:
```bash
go test ./agent/scanner -v
```

All existing tests pass without modification (backward compatible).
