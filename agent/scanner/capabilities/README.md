# Modular Capability Detection System

The capability detection system uses a **pluggable architecture** where each capability is a self-contained module implementing the `CapabilityDetector` interface.

## Architecture

### Core Interface

```go
type CapabilityDetector interface {
    Name() string              // "color", "copier", "duplex", etc.
    Detect(evidence) float64   // Returns confidence 0.0-1.0
    Threshold() float64        // Minimum confidence (default 0.7)
}
```

### Registry Pattern

```go
registry := NewCapabilityRegistry()

// Built-in detectors auto-registered:
// - PrinterDetector
// - ColorDetector
// - MonoDetector
// - CopierDetector
// - ScannerDetector
// - FaxDetector
// - DuplexDetector

// Add custom detector
registry.Register(&CustomDetector{})

// Detect all capabilities
caps := registry.DetectAll(evidence)
```

## Capability Modules

Each capability is in its own file: `capability_<name>.go`

### 1. Printer (`capability_printer.go`)
- **Evidence**: Serial number, Printer-MIB OIDs, printer ports, vendor
- **Strong**: Serial (0.5), Printer-MIB (0.3)
- **Weak**: Open ports (0.1), vendor match (0.1)

### 2. Color (`capability_color.go`)
- **Evidence**: Colorant names, color page counter, model keywords, consumable count
- **Strong**: CMY colorants (0.9), color pages > 0 (0.8)
- **Medium**: Model has "color" (0.6)
- **Weak**: 4+ consumables (0.3)

### 3. Mono (`capability_mono.go`)
- **Evidence**: Only black colorants, model keywords, low consumable count
- **Strong**: Only black colorant (0.9)
- **Medium**: Model has "mono" (0.7)
- **Weak**: 1-2 consumables (0.3)
- **Note**: Mutually exclusive with color

### 4. Copier (`capability_copier.go`)
- **Evidence**: Copy page counter, scan counters, MFP keywords, ADF
- **Strong**: Copy counter > 0 (0.9), counter exists (0.7)
- **Medium**: Has scan counters (0.5), "MFP" in model (0.6)
- **Weak**: Has ADF (0.3)

### 5. Scanner (`capability_scanner.go`)
- **Evidence**: Scan counters, scanner OID, model keywords, ADF
- **Strong**: Scan counter > 0 (0.9), counter exists (0.6)
- **Medium**: Scanner OID (0.5), "scanner" in model (0.6)
- **Special**: Boost confidence if printer_confidence < 0.3 (standalone scanner)

### 6. Fax (`capability_fax.go`)
- **Evidence**: Fax page counter, fax scan counters, model keywords, modem interface
- **Strong**: Fax counter > 0 (0.9), counter exists (0.6)
- **Medium**: Fax scan counters (0.5), "fax" in model (0.4)
- **Weak**: Modem/PSTN interface (0.3)

### 7. Duplex (`capability_duplex.go`)
- **Evidence**: Duplex counter, duplex unit, model suffix, keywords
- **Strong**: Duplex counter > 0 (0.9), counter exists (0.6)
- **Medium**: Duplex unit (0.7), "dn"/"dw" suffix (0.6)
- **Weak**: "duplex" keyword (0.5)

## Usage Example

```go
// Prepare evidence
evidence := &DetectionEvidence{
    PDUs:      snmpResults,
    SysDescr:  "HP LaserJet Pro M479fdw",
    SysOID:    "1.3.6.1.4.1.11.2.3.9.1",
    Vendor:    "HP",
    Model:     "LaserJet Pro M479fdw",
    Serial:    "JPBHM12345",
    OpenPorts: []int{9100, 80, 443, 631},
}

// Detect capabilities
registry := NewCapabilityRegistry()
caps := registry.DetectAll(evidence)

// Results
fmt.Printf("Printer: %.2f (%v)\n", caps.Scores["printer"], caps.IsPrinter)
fmt.Printf("Color: %.2f (%v)\n", caps.Scores["color"], caps.IsColor)
fmt.Printf("Copier: %.2f (%v)\n", caps.Scores["copier"], caps.IsCopier)
fmt.Printf("Device Type: %s\n", caps.DeviceType)

// Output:
// Printer: 1.00 (true)
// Color: 0.95 (true)
// Copier: 0.90 (true)
// Device Type: Color MFP
```

## Adding Custom Detectors

### Example: Network Fax Detector

```go
type NetworkFaxDetector struct{}

func (d *NetworkFaxDetector) Name() string {
    return "network_fax"
}

func (d *NetworkFaxDetector) Threshold() float64 {
    return 0.6 // Lower threshold
}

func (d *NetworkFaxDetector) Detect(evidence *DetectionEvidence) float64 {
    score := 0.0
    
    // Check if regular fax capability exists
    if faxScore, exists := evidence.Capabilities["fax"]; exists && faxScore > 0.5 {
        score += 0.5
    }
    
    // Check for network fax OIDs (HP Network Fax)
    networkFaxOIDs := []string{
        "1.3.6.1.4.1.11.2.4.3.3.0", // HP Network Fax enabled
    }
    if HasAnyOID(evidence.PDUs, networkFaxOIDs) {
        score += 0.7
    }
    
    // Check model for "network fax" keyword
    if ContainsAny(evidence.Model, []string{"network fax", "lan fax"}) {
        score += 0.4
    }
    
    return Min(score, 1.0)
}

// Register custom detector
registry := NewCapabilityRegistry()
registry.Register(&NetworkFaxDetector{})
```

## Benefits of Modular Design

### 1. **Testability**
Each detector can be unit tested independently:
```go
func TestColorDetector(t *testing.T) {
    detector := &ColorDetector{}
    evidence := &DetectionEvidence{
        Model: "HP Color LaserJet Pro M479fdw",
        PDUs: mockCMYKColorants(),
    }
    score := detector.Detect(evidence)
    if score < 0.9 {
        t.Errorf("Expected high confidence for color device")
    }
}
```

### 2. **Extensibility**
Add new capabilities without modifying core code:
- `WirelessDetector` - WiFi capability
- `NFC` - Near-field communication
- `CloudPrintDetector` - Cloud printing support
- `SecurePrintDetector` - PIN/badge release printing

### 3. **Maintainability**
Each file is focused on one capability (~100 lines):
- Easy to understand
- Clear responsibility
- Independent changes
- No side effects

### 4. **Customization**
Users can override thresholds or add vendor-specific detectors:
```go
// Lower threshold for duplex (more lenient)
type CustomDuplexDetector struct {
    DuplexDetector
}

func (d *CustomDuplexDetector) Threshold() float64 {
    return 0.5 // Lower than default 0.7
}

registry.Register(&CustomDuplexDetector{})
```

### 5. **Cross-Referencing**
Detectors can use results from other detectors:
```go
// In ScannerDetector:
if printerScore, exists := evidence.Capabilities["printer"]; exists {
    if printerScore < 0.3 && score > 0.5 {
        score += 0.2 // Likely standalone scanner
    }
}
```

## Testing Strategy

### Unit Tests (per detector)
```go
// capability_color_test.go
func TestColorDetector_CMYKColorants(t *testing.T) { }
func TestColorDetector_ColorPageCounter(t *testing.T) { }
func TestColorDetector_ModelKeywords(t *testing.T) { }
func TestColorDetector_MonoDevice(t *testing.T) { } // Should score low
```

### Integration Tests
```go
// capabilities_test.go
func TestCapabilityRegistry_HPColorMFP(t *testing.T) {
    evidence := loadRealDeviceData("hp_m479fdw.json")
    registry := NewCapabilityRegistry()
    caps := registry.DetectAll(evidence)
    
    assert.True(t, caps.IsPrinter)
    assert.True(t, caps.IsColor)
    assert.True(t, caps.IsCopier)
    assert.Equal(t, "Color MFP", caps.DeviceType)
}
```

### Regression Tests
```go
func TestCapabilityDetection_BackwardCompatibility(t *testing.T) {
    // Ensure existing printer detection still works
    // after capability system addition
}
```

## File Structure

```
scanner/
├── capabilities.go                  # Core interface & registry
├── capability_printer.go            # Printer detection
├── capability_color.go              # Color detection
├── capability_mono.go               # Monochrome detection
├── capability_copier.go             # Copier detection
├── capability_scanner.go            # Scanner detection
├── capability_fax.go                # Fax detection
├── capability_duplex.go             # Duplex detection
├── capabilities_test.go             # Integration tests
├── capability_printer_test.go       # Printer unit tests
├── capability_color_test.go         # Color unit tests
└── ... (one test file per detector)
```

## Performance

### Detection Cost
- **Per detector**: 10-50μs (mostly map lookups)
- **All 7 detectors**: < 500μs per device
- **Overhead**: Negligible compared to SNMP query time (100ms-2s)

### Optimization
Detectors run sequentially, allowing cross-referencing. Could parallelize if needed:
```go
func (r *CapabilityRegistry) DetectAllParallel(evidence) DeviceCapabilities {
    results := make(chan result, len(r.detectors))
    for _, detector := range r.detectors {
        go func(d CapabilityDetector) {
            results <- result{d.Name(), d.Detect(evidence)}
        }(detector)
    }
    // Collect results...
}
```

## Next Steps

1. **Create unit tests** for each detector
2. **Gather real-world data** from various devices
3. **Tune confidence scores** based on test results
4. **Add vendor-specific detectors** (HP-specific, Canon-specific, etc.)
5. **Integrate with storage** (add capabilities column to devices table)
6. **Update UI** (show capability badges, conditional metrics)

## Metrics Filtering System

The capability detection system integrates with **metrics filtering** to control which metrics are queried, parsed, and displayed based on device capabilities.

### Three-Layer Optimization

```
┌─────────────────────────────────────────────────────────┐
│ Layer 1: Scanner OID Selection                          │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ QueryDeviceWithCapabilities()                       │ │
│ │ • Detects capabilities during QueryFull             │ │
│ │ • Filters OIDs using GetCapabilityAwareMetricsOIDs()│ │
│ │ • 30-71% fewer SNMP queries on targeted devices     │ │
│ └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ Layer 2: Metrics Parsing (GetRelevantMetrics)           │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ GetRelevantMetrics(caps)                            │ │
│ │ • Filters 40+ metric definitions                    │ │
│ │ • Checks RequiresAll/RequiresAny/ExcludesAny        │ │
│ │ • Returns only metrics applicable to device         │ │
│ └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│ Layer 3: UI Display (GetRelevantMetricsByCategory)      │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ GetRelevantMetricsByCategory(caps, category)        │ │
│ │ • Groups metrics by category (Page, Supplies, etc.) │ │
│ │ • Hides irrelevant metrics from user interface      │ │
│ │ • Shows capability-appropriate data only            │ │
│ └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### Metric Definition Structure

Each metric is defined with capability requirements:

```go
type MetricDefinition struct {
    Name        string   // "color_pages", "toner_cyan", etc.
    Category    string   // "PageCounters", "Supplies", etc.
    RequiresAll []string // All must be true: ["printer", "color"]
    RequiresAny []string // At least one true: ["copier", "scanner"]
    ExcludesAny []string // None can be true: ["mono"]
}
```

### Filtering Logic

```go
// Example: Color-specific metrics
{
    Name:        "color_pages",
    Category:    "PageCounters",
    RequiresAll: []string{"printer", "color"}, // Must be color printer
    ExcludesAny: []string{"mono"},             // NOT mono
}

// Example: MFP-specific metrics
{
    Name:        "copy_total",
    Category:    "PageCounters",
    RequiresAny: []string{"copier"},           // Must have copier
}

// Example: Supply metrics
{
    Name:        "toner_cyan",
    Category:    "Supplies",
    RequiresAll: []string{"color"},            // Must be color
    ExcludesAny: []string{"mono"},             // NOT mono
}
```

### Usage Example

#### Basic Filtering

```go
// After capability detection
caps := registry.DetectAll(evidence)

// Get all relevant metrics
relevant := GetRelevantMetrics(caps)
fmt.Printf("Device has %d relevant metrics\n", len(relevant))

// Filter by category
pageMetrics := GetRelevantMetricsByCategory(caps, "PageCounters")
supplyMetrics := GetRelevantMetricsByCategory(caps, "Supplies")

// Check individual metric
if IsMetricRelevant("color_pages", caps) {
    // Parse and display color page counter
}
```

#### Mono Printer Example

```go
caps := DeviceCapabilities{
    IsPrinter: true,
    IsMono:    true,
    IsColor:   false,
    HasDuplex: true,
}

relevant := GetRelevantMetrics(caps)
// Returns: total_pages, mono_pages, duplex_pages, toner_black, drum_black
// Excludes: color_pages, toner_cyan, toner_magenta, toner_yellow
```

#### Color MFP Example

```go
caps := DeviceCapabilities{
    IsPrinter: true,
    IsColor:   true,
    IsCopier:  true,
    IsScanner: true,
}

relevant := GetRelevantMetrics(caps)
// Returns: total_pages, color_pages, copy_total, scan_total, 
//          toner_cyan, toner_magenta, toner_yellow, toner_black
```

### Metric Categories

The system defines metrics across 4 categories:

#### 1. PageCounters (20 metrics)
- **Total**: `total_pages`, `total_impressions`
- **Color/Mono**: `color_pages`, `mono_pages`
- **Function**: `copy_total`, `scan_total`, `fax_total`
- **Sided**: `simplex_pages`, `duplex_pages`
- **Color Detail**: `color_copy`, `mono_copy`, `color_print`, `mono_print`

#### 2. Supplies (9 metrics)
- **Toner**: `toner_black`, `toner_cyan`, `toner_magenta`, `toner_yellow`
- **Drums**: `drum_black`, `drum_cyan`, `drum_magenta`, `drum_yellow`
- **Maintenance**: `maintenance_kit`

#### 3. Usage (3 metrics)
- **Utilization**: `uptime_hours`, `energy_kwh`, `duty_cycle_percent`

#### 4. Status (2 metrics)
- **State**: `device_status`, `alert_count`

### Benefits

#### 1. Performance
```go
// Without capabilities:
oids := []string{...100 OIDs...}
// Query all, parse all, store all

// With capabilities:
oids := vendor.GetCapabilityAwareMetricsOIDs(caps)
// Query 30-40 OIDs (30-71% reduction)
// Parse only relevant metrics
// Store only applicable data
```

#### 2. User Experience
```go
// UI displays only relevant metrics
if IsMetricRelevant("color_pages", caps) {
    renderMetric("Color Pages", colorPages)
}

// Group by category for clean layout
pageMetrics := GetRelevantMetricsByCategory(caps, "PageCounters")
for _, metric := range pageMetrics {
    renderMetric(metric.Name, values[metric.Name])
}
```

#### 3. Data Quality
- No confusing zero values for non-existent features
- Accurate device representation
- Cleaner database (only store applicable metrics)

### Integration Example

```go
// In vendor's ExtractMetrics method:
func (h *HPModule) ExtractMetrics(snmpData, caps) Metrics {
    relevant := GetRelevantMetrics(caps)
    metrics := Metrics{}
    
    for _, metric := range relevant {
        switch metric.Name {
        case "color_pages":
            if caps.IsColor {
                metrics.ColorPages = extractColorPages(snmpData)
            }
        case "toner_cyan":
            if caps.IsColor {
                metrics.TonerCyan = extractTonerLevel(snmpData, "cyan")
            }
        // ... only parse relevant metrics
        }
    }
    
    return metrics
}
```

### Testing

All metric filtering logic is tested in `capabilities_test.go`:

```go
// Test mono printer filtering
func TestGetRelevantMetrics_MonoPrinter(t *testing.T) {
    caps := DeviceCapabilities{IsPrinter: true, IsMono: true}
    metrics := GetRelevantMetrics(caps)
    
    // Should have mono metrics
    assertContains(t, metrics, "mono_pages", "toner_black")
    
    // Should NOT have color metrics
    assertNotContains(t, metrics, "color_pages", "toner_cyan")
}

// Test color MFP filtering
func TestGetRelevantMetrics_ColorMFP(t *testing.T) {
    caps := DeviceCapabilities{IsPrinter: true, IsColor: true, IsCopier: true}
    metrics := GetRelevantMetrics(caps)
    
    // Should have printer, color, and copier metrics
    assertContains(t, metrics, "total_pages", "color_pages", "copy_total")
}
```

## Related Documentation

- [Scanner Module](../README.md) - SNMP querying and vendor profiles
- [Capability Integration Guide](../../../docs/CAPABILITY_INTEGRATION.md) - Usage examples
- [Storage Module](../../storage/README.md) - Database persistence
