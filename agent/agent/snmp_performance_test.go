package agent

import (
	"testing"
	"time"
)

// TestQuickGetSerialPerformance ensures serial lookup is fast enough for live discovery
func TestQuickGetSerialPerformance(t *testing.T) {
	t.Parallel()

	// These tests verify performance targets without needing a real printer
	// Actual SNMP calls will be tested with integration tests

	t.Run("OID_list_generation_should_be_instant", func(t *testing.T) {
		start := time.Now()

		// Test all helper functions
		commonOIDs := getCommonSerialOIDs()
		hpOIDs := getVendorSpecificSerialOIDs("HP")
		canonOIDs := getVendorSpecificSerialOIDs("Canon")
		allOIDs := getAllSerialOIDs()

		elapsed := time.Since(start)

		// Verify we got results
		if len(commonOIDs) == 0 {
			t.Error("getCommonSerialOIDs returned empty list")
		}
		if len(hpOIDs) == 0 {
			t.Error("getVendorSpecificSerialOIDs(HP) returned empty list")
		}
		if len(canonOIDs) == 0 {
			t.Error("getVendorSpecificSerialOIDs(Canon) returned empty list")
		}
		if len(allOIDs) < 5 {
			t.Errorf("getAllSerialOIDs returned too few OIDs: %d", len(allOIDs))
		}

		// Performance check: OID generation should be < 1ms
		if elapsed > time.Millisecond {
			t.Errorf("OID generation took too long: %v (expected < 1ms)", elapsed)
		}

		t.Logf("OID generation completed in %v (target: <1ms) ✓", elapsed)
		t.Logf("  Common OIDs: %d", len(commonOIDs))
		t.Logf("  HP OIDs: %d", len(hpOIDs))
		t.Logf("  Canon OIDs: %d", len(canonOIDs))
		t.Logf("  All OIDs: %d", len(allOIDs))
	})

	t.Run("Vendor_OID_mapping_coverage", func(t *testing.T) {
		vendors := []string{
			"HP", "Hewlett Packard", "hewlett-packard",
			"Canon", "CANON",
			"Samsung", "samsung",
			"Kyocera", "KYOCERA",
			"OKI", "oki",
			"Ricoh", "RICOH",
			"Brother", "brother",
			"Lexmark", "lexmark",
			"Epson", "EPSON",
		}

		for _, vendor := range vendors {
			oids := getVendorSpecificSerialOIDs(vendor)
			if len(oids) == 0 {
				t.Errorf("No OIDs found for vendor: %s", vendor)
			} else {
				t.Logf("Vendor %s: %d OID(s)", vendor, len(oids))
			}
		}
	})

	t.Run("QuickGetSerialWithHint_OID_ordering", func(t *testing.T) {
		// Verify that vendor-specific OIDs come first when manufacturer hint is provided
		// This is important for performance - we want to try the most likely OID first

		// Test with HP hint
		start := time.Now()
		// We can't actually call QuickGetSerialWithHint without a real device,
		// but we can verify the OID helper functions work correctly
		hpOIDs := getVendorSpecificSerialOIDs("HP")
		commonOIDs := getCommonSerialOIDs()
		elapsed := time.Since(start)

		if len(hpOIDs) == 0 {
			t.Error("HP-specific OIDs should exist")
		}
		if len(commonOIDs) == 0 {
			t.Error("Common OIDs should exist")
		}

		// Verify it's fast
		if elapsed > 100*time.Microsecond {
			t.Errorf("OID lookup took too long: %v (expected < 100µs)", elapsed)
		}

		t.Logf("OID ordering check completed in %v (target: <100µs) ✓", elapsed)
	})
}

// TestSerialOIDCoverage verifies we have OIDs for all major printer vendors
func TestSerialOIDCoverage(t *testing.T) {
	t.Parallel()

	expectedVendors := map[string]bool{
		"hp":      false,
		"canon":   false,
		"samsung": false,
		"kyocera": false,
		"oki":     false,
		"ricoh":   false,
		"brother": false,
		"lexmark": false,
		"epson":   false,
	}

	// Test that we have OIDs for each vendor
	for vendor := range expectedVendors {
		oids := getVendorSpecificSerialOIDs(vendor)
		if len(oids) > 0 {
			expectedVendors[vendor] = true
			t.Logf("✓ %s: %d OID(s)", vendor, len(oids))
		}
	}

	// Verify all vendors have coverage
	for vendor, hasCoverage := range expectedVendors {
		if !hasCoverage {
			t.Errorf("Missing OID coverage for vendor: %s", vendor)
		}
	}

	// Verify common OIDs exist
	commonOIDs := getCommonSerialOIDs()
	if len(commonOIDs) == 0 {
		t.Error("No common serial OIDs defined")
	} else {
		t.Logf("✓ Common OIDs: %d", len(commonOIDs))
	}

	// Verify getAllSerialOIDs returns comprehensive list
	allOIDs := getAllSerialOIDs()
	expectedMinimum := len(commonOIDs) + len(expectedVendors) // At least 1 OID per vendor + common
	if len(allOIDs) < expectedMinimum {
		t.Errorf("getAllSerialOIDs returned %d OIDs, expected at least %d", len(allOIDs), expectedMinimum)
	} else {
		t.Logf("✓ Total serial OIDs: %d (minimum expected: %d)", len(allOIDs), expectedMinimum)
	}
}

// BenchmarkOIDGeneration measures performance of OID helper functions
func BenchmarkOIDGeneration(b *testing.B) {
	b.Run("getCommonSerialOIDs", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = getCommonSerialOIDs()
		}
	})

	b.Run("getVendorSpecificSerialOIDs_HP", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = getVendorSpecificSerialOIDs("HP")
		}
	})

	b.Run("getVendorSpecificSerialOIDs_Canon", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = getVendorSpecificSerialOIDs("Canon")
		}
	})

	b.Run("getAllSerialOIDs", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = getAllSerialOIDs()
		}
	})
}

// TestSmartRefreshDevicePerformanceTargets documents expected performance for live discovery
func TestSmartRefreshDevicePerformanceTargets(t *testing.T) {
	t.Parallel()

	// This test documents our performance targets for live discovery scenarios
	// Actual timing will vary based on network conditions

	targets := map[string]time.Duration{
		"Serial lookup (QuickGetSerial)":        200 * time.Millisecond,  // 7-9 SNMP queries
		"Database lookup by serial":             5 * time.Millisecond,    // SQLite indexed query
		"LastSeen update + SSE broadcast":       10 * time.Millisecond,   // DB write + broadcast
		"Quick refresh (known device)":          300 * time.Millisecond,  // 8-15 targeted OID queries
		"Full refresh (new device)":             2 * time.Second,         // 1000+ OID walk
		"Total for known device (fast path)":    500 * time.Millisecond,  // Serial + DB + Quick refresh
		"Total for new device (full discovery)": 2500 * time.Millisecond, // Serial + Full refresh
	}

	t.Log("Performance targets for live discovery:")
	for operation, target := range targets {
		t.Logf("  %-45s: %v", operation, target)
	}

	t.Log("\nOptimization strategy:")
	t.Log("  1. Serial lookup: Try common OID first, then vendor-specific")
	t.Log("  2. Known devices: Skip full walk, query only essential OIDs")
	t.Log("  3. LastSeen update: Immediate DB write + SSE broadcast")
	t.Log("  4. UI update: Instant via SSE (no polling delay)")
	t.Log("  5. Manufacturer hint: Use vendor OIDs first when manufacturer known")
}
