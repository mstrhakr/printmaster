package capabilities

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestGetRelevantMetrics_MonoPrinter(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   false,
		IsMono:    true,
		IsCopier:  false,
		IsScanner: false,
		IsFax:     false,
		HasDuplex: false,
	}

	relevant := GetRelevantMetrics(caps)

	// Should have basic page counters
	hasTotal := false
	hasColorPages := false
	hasCopyPages := false
	hasCyanToner := false
	hasBlackToner := false

	for _, metric := range relevant {
		switch metric.Name {
		case "total_pages", "page_count":
			hasTotal = true
		case "color_pages":
			hasColorPages = true
		case "copy_pages":
			hasCopyPages = true
		case "toner_cyan":
			hasCyanToner = true
		case "toner_black":
			hasBlackToner = true
		}
	}

	if !hasTotal {
		t.Error("Mono printer should have total_pages metric")
	}
	if hasColorPages {
		t.Error("Mono printer should NOT have color_pages metric")
	}
	if hasCopyPages {
		t.Error("Simple printer should NOT have copy_pages metric")
	}
	if hasCyanToner {
		t.Error("Mono printer should NOT have cyan toner metric")
	}
	if !hasBlackToner {
		t.Error("Mono printer should have black toner metric")
	}
}

func TestGetRelevantMetrics_ColorMFP(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   true,
		IsMono:    false,
		IsCopier:  true,
		IsScanner: true,
		IsFax:     true,
		HasDuplex: true,
	}

	relevant := GetRelevantMetrics(caps)

	// Should have all feature metrics
	hasColorPages := false
	hasCopyColorPages := false
	hasScanCount := false
	hasFaxPages := false
	hasDuplexSheets := false
	hasCyanToner := false

	for _, metric := range relevant {
		switch metric.Name {
		case "color_pages":
			hasColorPages = true
		case "copy_color_pages":
			hasCopyColorPages = true
		case "scan_count":
			hasScanCount = true
		case "fax_pages":
			hasFaxPages = true
		case "duplex_sheets":
			hasDuplexSheets = true
		case "toner_cyan":
			hasCyanToner = true
		}
	}

	if !hasColorPages {
		t.Error("Color MFP should have color_pages metric")
	}
	if !hasCopyColorPages {
		t.Error("Color copier should have copy_color_pages metric")
	}
	if !hasScanCount {
		t.Error("MFP should have scan_count metric")
	}
	if !hasFaxPages {
		t.Error("Fax-capable device should have fax_pages metric")
	}
	if !hasDuplexSheets {
		t.Error("Duplex-capable device should have duplex_sheets metric")
	}
	if !hasCyanToner {
		t.Error("Color device should have cyan toner metric")
	}
}

func TestGetRelevantMetrics_MonoCopier(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   false,
		IsMono:    true,
		IsCopier:  true,
		IsScanner: true,
		IsFax:     false,
		HasDuplex: true,
	}

	relevant := GetRelevantMetrics(caps)

	hasCopyPages := false
	hasCopyColorPages := false
	hasScanCount := false
	hasCyanToner := false

	for _, metric := range relevant {
		switch metric.Name {
		case "copy_pages":
			hasCopyPages = true
		case "copy_color_pages":
			hasCopyColorPages = true
		case "scan_count":
			hasScanCount = true
		case "toner_cyan":
			hasCyanToner = true
		}
	}

	if !hasCopyPages {
		t.Error("Copier should have copy_pages metric")
	}
	if hasCopyColorPages {
		t.Error("Mono copier should NOT have copy_color_pages metric")
	}
	if !hasScanCount {
		t.Error("Copier should have scan_count metric (copy requires scan)")
	}
	if hasCyanToner {
		t.Error("Mono device should NOT have cyan toner metric")
	}
}

func TestIsMetricRelevant(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   true,
		IsCopier:  false,
	}

	tests := []struct {
		metricName string
		relevant   bool
	}{
		{"total_pages", true},         // Printer capability
		{"color_pages", true},         // Printer + color
		{"copy_pages", false},         // Requires copier
		{"toner_cyan", true},          // Printer + color
		{"scan_count", false},         // Requires scanner or copier
		{"nonexistent_metric", false}, // Unknown metric
	}

	for _, tt := range tests {
		result := IsMetricRelevant(tt.metricName, caps)
		if result != tt.relevant {
			t.Errorf("IsMetricRelevant(%q) = %v, want %v", tt.metricName, result, tt.relevant)
		}
	}
}

func TestGetRelevantMetricsByCategory(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   true,
		IsCopier:  true,
	}

	byCategory := GetRelevantMetricsByCategory(caps)

	// Should have all categories
	if len(byCategory["pages"]) == 0 {
		t.Error("Should have page counter metrics")
	}
	if len(byCategory["supplies"]) == 0 {
		t.Error("Should have supply metrics")
	}
	if len(byCategory["usage"]) == 0 {
		t.Error("Should have usage metrics")
	}

	// Verify color supplies are included
	hasCyan := false
	for _, metric := range byCategory["supplies"] {
		if metric.Name == "toner_cyan" {
			hasCyan = true
			break
		}
	}
	if !hasCyan {
		t.Error("Color copier should have cyan toner in supplies category")
	}
}

func TestGetRelevantMetricNames(t *testing.T) {
	t.Parallel()

	caps := &DeviceCapabilities{
		IsPrinter: true,
		IsColor:   false,
		IsMono:    true,
	}

	names := GetRelevantMetricNames(caps)

	// Should have some metrics
	if len(names) == 0 {
		t.Error("Should have some relevant metrics")
	}

	// Should NOT have color-specific metrics
	for _, name := range names {
		if name == "color_pages" || name == "toner_cyan" {
			t.Errorf("Mono printer should not have metric: %s", name)
		}
	}
}

// ===== CAPABILITY DETECTION TESTS =====

func TestPrinterDetector(t *testing.T) {
	t.Parallel()

	detector := &PrinterDetector{}

	tests := []struct {
		name       string
		evidence   *DetectionEvidence
		minScore   float64
		expectHigh bool
	}{
		{
			name: "Strong evidence - serial + printer OIDs",
			evidence: &DetectionEvidence{
				Serial: "JPBHM12345",
				PDUs: []gosnmp.SnmpPDU{
					{Name: "1.3.6.1.2.1.43.5.1.1.17.1", Value: []byte("JPBHM12345")},
					{Name: "1.3.6.1.2.1.43.10.2.1.4.1.1", Value: 10000},
				},
				Model:  "HP LaserJet Pro M404dn",
				Vendor: "HP",
			},
			minScore:   0.8,
			expectHigh: true,
		},
		{
			name: "Weak evidence - only vendor",
			evidence: &DetectionEvidence{
				Vendor: "HP",
			},
			minScore:   0.0,
			expectHigh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := detector.Detect(tt.evidence)
			if tt.expectHigh && score < tt.minScore {
				t.Errorf("Expected high score (>%.2f), got %.2f", tt.minScore, score)
			}
			if !tt.expectHigh && score >= detector.Threshold() {
				t.Errorf("Expected low score (<%.2f), got %.2f", detector.Threshold(), score)
			}
		})
	}
}

func TestColorDetector(t *testing.T) {
	t.Parallel()

	detector := &ColorDetector{}

	tests := []struct {
		name       string
		evidence   *DetectionEvidence
		expectHigh bool
	}{
		{
			name: "Strong - CMY colorants present",
			evidence: &DetectionEvidence{
				PDUs: []gosnmp.SnmpPDU{
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.1", Value: []byte("Black")},
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.2", Value: []byte("Cyan")},
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.3", Value: []byte("Magenta")},
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.4", Value: []byte("Yellow")},
				},
			},
			expectHigh: true,
		},
		{
			name: "Low - only black colorant",
			evidence: &DetectionEvidence{
				PDUs: []gosnmp.SnmpPDU{
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.1", Value: []byte("Black")},
				},
			},
			expectHigh: false,
		},
		{
			name: "Medium - color in model name",
			evidence: &DetectionEvidence{
				Model: "HP Color LaserJet Pro M479fdw",
			},
			expectHigh: false, // Not enough alone
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := detector.Detect(tt.evidence)
			threshold := detector.Threshold()
			if tt.expectHigh && score < threshold {
				t.Errorf("Expected high score (>=%.2f), got %.2f", threshold, score)
			}
			if !tt.expectHigh && score >= threshold {
				t.Errorf("Expected low score (<%.2f), got %.2f", threshold, score)
			}
		})
	}
}

func TestMonoDetector(t *testing.T) {
	t.Parallel()

	detector := &MonoDetector{}

	tests := []struct {
		name       string
		evidence   *DetectionEvidence
		expectHigh bool
	}{
		{
			name: "Strong - only black colorant",
			evidence: &DetectionEvidence{
				PDUs: []gosnmp.SnmpPDU{
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.1", Value: []byte("Black")},
				},
			},
			expectHigh: true,
		},
		{
			name: "Low - color already detected",
			evidence: &DetectionEvidence{
				Capabilities: map[string]float64{
					"color": 0.9,
				},
				PDUs: []gosnmp.SnmpPDU{
					{Name: "1.3.6.1.2.1.43.12.1.1.4.1.1", Value: []byte("Black")},
				},
			},
			expectHigh: false,
		},
		{
			name: "High - mono keyword in model",
			evidence: &DetectionEvidence{
				Model: "HP LaserJet Pro M404dn Monochrome",
			},
			expectHigh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := detector.Detect(tt.evidence)
			threshold := detector.Threshold()
			if tt.expectHigh && score < threshold {
				t.Errorf("Expected high score (>=%.2f), got %.2f", threshold, score)
			}
			if !tt.expectHigh && score >= threshold {
				t.Errorf("Expected low score (<%.2f), got %.2f", threshold, score)
			}
		})
	}
}

func TestCapabilityRegistry(t *testing.T) {
	t.Parallel()

	registry := NewCapabilityRegistry()

	// Verify all detectors are registered
	evidence := &DetectionEvidence{
		Serial: "TEST123",
		Model:  "HP Color LaserJet Pro MFP M479fdw",
		Vendor: "HP",
		PDUs: []gosnmp.SnmpPDU{
			{Name: "1.3.6.1.2.1.43.5.1.1.17.1", Value: []byte("TEST123")},
			{Name: "1.3.6.1.2.1.43.12.1.1.4.1.2", Value: []byte("Cyan")},
			{Name: "1.3.6.1.2.1.43.12.1.1.4.1.3", Value: []byte("Magenta")},
			{Name: "1.3.6.1.2.1.43.12.1.1.4.1.4", Value: []byte("Yellow")},
		},
	}

	caps := registry.DetectAll(evidence)

	// Verify scores map is populated
	if len(caps.Scores) == 0 {
		t.Error("Expected scores to be populated")
	}

	// Verify at least printer capability detected
	if !caps.IsPrinter {
		t.Error("Expected printer capability to be detected")
	}

	// Verify color detected (has CMY colorants)
	if !caps.IsColor {
		t.Error("Expected color capability to be detected")
	}

	// Verify device type is set
	if caps.DeviceType == "" {
		t.Error("Expected device type to be set")
	}
}

func TestDeviceTypeClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		caps         DeviceCapabilities
		expectedType string
	}{
		{
			name: "Color MFP",
			caps: DeviceCapabilities{
				IsPrinter: true,
				IsColor:   true,
				IsCopier:  true,
			},
			expectedType: "Color MFP",
		},
		{
			name: "Mono MFP",
			caps: DeviceCapabilities{
				IsPrinter: true,
				IsMono:    true,
				IsScanner: true,
			},
			expectedType: "Mono MFP",
		},
		{
			name: "Color Printer",
			caps: DeviceCapabilities{
				IsPrinter: true,
				IsColor:   true,
				IsCopier:  false,
				IsScanner: false,
			},
			expectedType: "Color Printer",
		},
		{
			name: "Mono Printer",
			caps: DeviceCapabilities{
				IsPrinter: true,
				IsMono:    true,
				IsCopier:  false,
			},
			expectedType: "Mono Printer",
		},
		{
			name: "Standalone Scanner",
			caps: DeviceCapabilities{
				IsPrinter: false,
				IsScanner: true,
			},
			expectedType: "Scanner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyDeviceType(tt.caps)
			if result != tt.expectedType {
				t.Errorf("Expected device type %q, got %q", tt.expectedType, result)
			}
		})
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	pdus := []gosnmp.SnmpPDU{
		{Name: "1.3.6.1.2.1.1.1.0", Value: []byte("HP LaserJet")},
		{Name: ".1.3.6.1.2.1.1.2.0", Value: []byte("1.3.6.1.4.1.11")},
		{Name: "1.3.6.1.2.1.43.10.2.1.4.1.1", Value: int64(10000)},
	}

	t.Run("HasOID", func(t *testing.T) {
		if !HasOID(pdus, "1.3.6.1.2.1.1.1.0") {
			t.Error("Should find OID without leading dot")
		}
		if !HasOID(pdus, ".1.3.6.1.2.1.1.2.0") {
			t.Error("Should find OID with leading dot")
		}
		if HasOID(pdus, "1.2.3.4.5") {
			t.Error("Should not find non-existent OID")
		}
	})

	t.Run("HasAnyOID", func(t *testing.T) {
		if !HasAnyOID(pdus, []string{"1.2.3.4", "1.3.6.1.2.1.1.1.0"}) {
			t.Error("Should find at least one OID")
		}
		if HasAnyOID(pdus, []string{"1.2.3.4", "5.6.7.8"}) {
			t.Error("Should not find any OID")
		}
	})

	t.Run("GetOIDValue", func(t *testing.T) {
		val := GetOIDValue(pdus, "1.3.6.1.2.1.43.10.2.1.4.1.1")
		if val != 10000 {
			t.Errorf("Expected 10000, got %d", val)
		}
		val = GetOIDValue(pdus, "1.2.3.4.5")
		if val != 0 {
			t.Errorf("Expected 0 for non-existent OID, got %d", val)
		}
	})

	t.Run("GetOIDString", func(t *testing.T) {
		str := GetOIDString(pdus, "1.3.6.1.2.1.1.1.0")
		if str != "HP LaserJet" {
			t.Errorf("Expected 'HP LaserJet', got %q", str)
		}
		str = GetOIDString(pdus, "1.2.3.4.5")
		if str != "" {
			t.Errorf("Expected empty string for non-existent OID, got %q", str)
		}
	})

	t.Run("ContainsAny", func(t *testing.T) {
		if !ContainsAny("HP Color LaserJet", []string{"color", "xerox"}) {
			t.Error("Should find 'color' in string")
		}
		if ContainsAny("HP LaserJet", []string{"color", "xerox"}) {
			t.Error("Should not find any substring")
		}
	})

	t.Run("ContainsAll", func(t *testing.T) {
		if !ContainsAll("HP Color LaserJet Pro MFP", []string{"color", "mfp"}) {
			t.Error("Should find all substrings")
		}
		if ContainsAll("HP Color LaserJet", []string{"color", "mfp"}) {
			t.Error("Should not find all substrings")
		}
	})
}

func TestMinMax(t *testing.T) {
	t.Parallel()

	if Min(0.5, 0.8) != 0.5 {
		t.Error("Min should return smaller value")
	}
	if Max(0.5, 0.8) != 0.8 {
		t.Error("Max should return larger value")
	}
}
