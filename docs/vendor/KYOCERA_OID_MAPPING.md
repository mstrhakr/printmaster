# Kyocera Enterprise OID Mapping

**Device**: TASKalfa 6052ci (172.52.105.95)  
**Serial**: W2D8303202  
**Enterprise OID Base**: `1.3.6.1.4.1.1347.*`  
**Analysis Date**: 2025-11-03  
**Status**: ✅ VALIDATED - Complete OID structure decoded

---

## Summary

Kyocera devices provide **exceptional detail** via enterprise OIDs:
- **Standard Printer-MIB** provides total impressions (432,951)
- **Kyocera `.42.3.*` tree** provides complete function and color breakdown
- **Direct OIDs available** for all major metrics (no calculation needed!)
- **Best-in-class metrics** compared to other vendors

---

## Validated OID Mappings

### Page Count Totals
| Metric | OID | Value | Validated |
|--------|-----|-------|-----------|
| **Total Impressions** | `1.3.6.1.2.1.43.10.2.1.4.1.1` | 432,951 | ✅ Standard Printer-MIB |
| **Total Printed Pages** | `1.3.6.1.4.1.1347.43.10.1.1.12.1.1` | 424,405 | ✅ Kyocera Enterprise |
| **Total B&W Printed** | *Calculated* | 241,538 | Sum of B&W functions |
| **Total Color Printed** | *Calculated* | 182,867 | Sum of Color functions |

### Kyocera Enterprise OID Structure (`.42.3.*`)

#### Function Totals (`.42.3.1.1.1.*`)
| Metric | OID | Value | Validated |
|--------|-----|-------|-----------|
| **Printer Total** | `1.3.6.1.4.1.1347.42.3.1.1.1.1` | 304,339 | ✅ |
| **Copy Total** | `1.3.6.1.4.1.1347.42.3.1.1.1.2` | 116,254 | ✅ |
| **Fax Total** | `1.3.6.1.4.1.1347.42.3.1.1.1.4` | 3,812 | ✅ |

#### Print Breakdown by Function and Color (`.42.3.1.2.1.1.*`)
| Metric | OID | Value | Validated |
|--------|-----|-------|-----------|
| **Printer B&W** | `1.3.6.1.4.1.1347.42.3.1.2.1.1.1.1` | 163,915 | ✅ |
| **Printer Color** | `1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3` | 140,424 | ✅ |
| **Copy B&W** | `1.3.6.1.4.1.1347.42.3.1.2.1.1.2.1` | 73,811 | ✅ |
| **Copy Color** | `1.3.6.1.4.1.1347.42.3.1.2.1.1.2.3` | 42,443 | ✅ |
| **Fax B&W** | `1.3.6.1.4.1.1347.42.3.1.2.1.1.4.1` | 3,812 | ✅ |

#### Scan Counters (`.42.3.1.3.1.1.*`)
| Metric | OID | Value | Validated |
|--------|-----|-------|-----------|
| **Copy Scans** | `1.3.6.1.4.1.1347.42.3.1.3.1.1.2` | 42,138 | ✅ |
| **Fax Scans** | `1.3.6.1.4.1.1347.42.3.1.3.1.1.4` | 3,057 | ✅ |
| **Other Scans** | `1.3.6.1.4.1.1347.42.3.1.4.1.1.1` | 43,441 | ✅ |

### Calculated Totals (Validation)
```
Total B&W Printed:
  Printer B&W (163,915) + Copy B&W (73,811) + Fax B&W (3,812) = 241,538 ✅

Total Color Printed:
  Printer Color (140,424) + Copy Color (42,443) + Fax Color (0) = 182,867 ✅

Total Printed:
  Printer (304,339) + Copy (116,254) + Fax (3,812) = 424,405 ✅

Total Scans:
  Copy Scans (42,138) + Fax Scans (3,057) + Other Scans (43,441) = 88,636 ✅
```

### Previously Found OIDs (Still Investigating)

#### OID Pair 1: B&W vs Color Split
| OID | Value | Likely Meaning |
|-----|-------|----------------|
| `.43.10.1.1.16.1.1` | 221,912 | B&W Total or Color Total? |
| *Calculated* | 211,039 | Complementary (Total - 221,912) |

**Math Check**: 221,912 + 211,039 = 432,951 ✅

#### OID Pair 2: Function Split (Copy/Print?)
| OID | Value | Likely Meaning |
|-----|-------|----------------|
| `.43.8.1.1.8.1.4` | 170,078 | Copy or Print? |
| *Calculated* | 262,873 | Complementary (Total - 170,078) |

**Math Check**: 170,078 + 262,873 = 432,951 ✅

#### OID Pair 3: Another Function Split
| OID | Value | Likely Meaning |
|-----|-------|----------------|
| `.43.8.1.1.8.1.2` | 125,196 | Function counter? |
| *Calculated* | 307,755 | Complementary (Total - 125,196) |

**Math Check**: 125,196 + 307,755 = 432,951 ✅

#### Additional Counter (Not Adding to Total)
| OID | Value | Likely Meaning |
|-----|-------|----------------|
| `.43.10.1.1.12.1.1` | 424,405 | Total minus something (432,951 - 8,546) |

---

## Web UI Validation ✅

Counters from TASKalfa 6052ci web interface (serial W2D8303202):

**Printed Pages:**
- Total: 424,405 ✅
- B&W: 241,538 ✅  
- Color: 182,867 ✅
- Copy: 116,254 ✅
- Printer: 304,339 ✅
- Fax: 3,812 ✅

**Scanned Pages:**
- Copy: 42,138 ✅
- Fax: 3,057 ✅
- Other: 43,441 ✅
- Total: 88,636 ✅

**All values perfectly match SNMP OID data!**

---

## Implementation Strategy for Kyocera Vendor Module

### GetMetricsOIDs() - Core Counters
```go
return []string{
    // Standard Printer-MIB (fallback)
    "1.3.6.1.2.1.43.10.2.1.4.1.1", // Total impressions
    "1.3.6.1.2.1.43.11.1.1.9.1",   // Toner levels
    "1.3.6.1.2.1.43.11.1.1.6.1",   // Toner descriptions
    
    // Kyocera enterprise - Total printed
    "1.3.6.1.4.1.1347.43.10.1.1.12.1.1", // Total printed pages
    
    // Function totals (.42.3.1.1.1.*)
    "1.3.6.1.4.1.1347.42.3.1.1.1.1", // Printer total
    "1.3.6.1.4.1.1347.42.3.1.1.1.2", // Copy total
    "1.3.6.1.4.1.1347.42.3.1.1.1.4", // Fax total
    
    // Print breakdown by function and color (.42.3.1.2.1.1.*)
    "1.3.6.1.4.1.1347.42.3.1.2.1.1.1.1", // Printer B&W
    "1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3", // Printer Color
    "1.3.6.1.4.1.1347.42.3.1.2.1.1.2.1", // Copy B&W
    "1.3.6.1.4.1.1347.42.3.1.2.1.1.2.3", // Copy Color
    "1.3.6.1.4.1.1347.42.3.1.2.1.1.4.1", // Fax B&W
    
    // Scan counters (.42.3.1.3.* and .42.3.1.4.*)
    "1.3.6.1.4.1.1347.42.3.1.3.1.1.2", // Copy scans
    "1.3.6.1.4.1.1347.42.3.1.3.1.1.4", // Fax scans
    "1.3.6.1.4.1.1347.42.3.1.4.1.1.1", // Other scans
}
```

### ExtractMetrics() - Parsing Logic
```go
// Direct assignments (no calculation needed!)
printerBW := getOIDValue("1.3.6.1.4.1.1347.42.3.1.2.1.1.1.1")
printerColor := getOIDValue("1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3")
copyBW := getOIDValue("1.3.6.1.4.1.1347.42.3.1.2.1.1.2.1")
copyColor := getOIDValue("1.3.6.1.4.1.1347.42.3.1.2.1.1.2.3")
faxBW := getOIDValue("1.3.6.1.4.1.1347.42.3.1.2.1.1.4.1")

// Calculate totals
snapshot.MonoPages = printerBW + copyBW + faxBW
snapshot.ColorPages = printerColor + copyColor
snapshot.PageCount = snapshot.MonoPages + snapshot.ColorPages

// Function-specific
snapshot.CopyPages = copyBW + copyColor
snapshot.FaxPages = faxBW
snapshot.OtherPages = printerBW + printerColor // Print pages

// Scan counters
snapshot.ScanCount = copyScan + faxScan + otherScan
```

### Capability-Aware Filtering
```go
func (v *KyoceraVendor) GetCapabilityAwareMetricsOIDs(caps *capabilities.DeviceCapabilities) []string {
    oids := baseOIDs // Always include page counts and toner
    
    if caps.IsCopier {
        oids = append(oids, copyOIDs...)
    }
    if caps.IsFax {
        oids = append(oids, faxOIDs...)
    }
    if caps.IsScanner {
        oids = append(oids, scanOIDs...)
    }
    
    return oids
}
```

---

## Comparison: Kyocera vs Epson vs HP

### Kyocera (Best Metrics) 🏆
```
✅ Direct OIDs for EVERYTHING:
   - Print B&W, Print Color
   - Copy B&W, Copy Color
   - Fax B&W, Fax Color (if available)
   - Copy Scans, Fax Scans, Other Scans
   
✅ No calculation needed
✅ Most detailed metrics of any vendor
✅ Function separation (Print vs Copy vs Fax)
✅ Scan counters available
```

### Epson (Good Metrics)
```
✅ Direct OIDs for:
   - Total, B&W, Color
   - Total Print Computer, Color Print Computer
   - Total Copy, Color Copy
   
⚠️ Calculate:
   - B&W Print = Total Print - Color Print
   - B&W Copy = Total Copy - Color Copy
   
❌ No scan counters found
❌ No fax counters
```

### HP (Moderate Metrics)
```
✅ Direct OIDs for:
   - Total pages (multiple marker indices)
   - Fax, duplex sheets
   - Scan to host (ADF/Flatbed)
   - Jam events
   
❌ No B&W vs Color breakdown
❌ No function separation (Print vs Copy)
```

### Generic (Basic Metrics)
```
✅ Standard Printer-MIB only:
   - Total pages
   - Toner levels
   - Serial number
   
❌ No B&W/Color breakdown
❌ No function counters
❌ No scan counters
```

---

## Next Steps

### ✅ COMPLETED
1. ✅ **Web UI validation** - All counters match SNMP data perfectly
2. ✅ **OID structure decoded** - Complete `.42.3.*` tree mapped

### 🔄 TODO
1. **Create Kyocera vendor module** (`agent/scanner/vendor/kyocera.go`)
   - Implement VendorModule interface
   - Add all validated OIDs from `.42.3.*` tree
   - Capability-aware filtering (copy/fax/scan)
   - Register in vendor registry with enterprise 1347

2. **Validate on second Kyocera device**
   - Test on ECOSYS PA4000wx (172.52.105.114)
   - Verify OID structure is consistent
   - Check if mono-only device uses same OIDs (without color indices)

3. **Deploy and test**
   - Build agent with Kyocera vendor module
   - Verify enhanced metrics appear in UI
   - Monitor for any OID inconsistencies

### 📊 Expected Impact
- **20% additional coverage** (2 out of 10 devices)
- **Best metrics in the industry** for Kyocera devices
- **Complete function breakdown** (Print/Copy/Fax/Scan)
- **Full B&W/Color separation** for all functions

---

## Notes

- Kyocera's enterprise number is 1347
- OID structure is more complex than Epson
- Multiple subtrees (`.42.*`, `.43.*`, `.47.*`)
- Standard Printer-MIB total (432,951) is reliable
- Need web UI to decode enterprise OID meanings
- Single Color prints are ignored (focus on B&W, Full Color, Total only)
