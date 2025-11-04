package capabilities

import "strings"

// ScannerDetector detects if device has scan capability
type ScannerDetector struct{}

func (d *ScannerDetector) Name() string {
	return "scanner"
}

func (d *ScannerDetector) Threshold() float64 {
	return 0.7
}

func (d *ScannerDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Scan counter exists and > 0
	scanCounterOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.1.2.0",     // HP flatbed scans
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.1.1.0",     // HP ADF scans
		"1.3.6.1.4.1.1602.1.1.1.4.1.1.0",           // Canon scan counter
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.8.1.3.0", // Brother scan counter
	}

	maxScanCount := int64(0)
	hasScanner := false
	for _, oid := range scanCounterOIDs {
		if HasOID(evidence.PDUs, oid) {
			hasScanner = true
			scanCount := GetOIDValue(evidence.PDUs, oid)
			if scanCount > maxScanCount {
				maxScanCount = scanCount
			}
		}
	}

	if maxScanCount > 0 {
		score += 0.9
	} else if hasScanner {
		score += 0.6 // Has counter but not used yet
	}

	// Medium evidence: Scanner-specific sysObjectID
	// Some standalone scanners have specific OID prefixes
	scannerOIDPrefixes := []string{
		"1.3.6.1.4.1.367",  // Ricoh scanners
		"1.3.6.1.4.1.1602", // Canon (includes scanners)
	}
	for _, prefix := range scannerOIDPrefixes {
		if strings.HasPrefix(normalizeOID(evidence.SysOID), normalizeOID(prefix)) {
			score += 0.5
			break
		}
	}

	// Medium evidence: Model contains scanner keywords
	scannerKeywords := []string{"scanner", "scanstation", "scan", "document scanner"}
	if ContainsAny(evidence.Model, scannerKeywords) {
		score += 0.6
	}

	// Special case: Standalone scanner (rare in enterprise)
	// If has scanner but low printer confidence, likely standalone scanner
	if printerScore, exists := evidence.Capabilities["printer"]; exists {
		if printerScore < 0.3 && score > 0.5 {
			score += 0.2 // Boost confidence for standalone scanner
		}
	}

	// Weak evidence: Has ADF (common in scanners)
	adfOIDs := []string{
		"1.3.6.1.2.1.43.8.2.1.2", // prtInputType
	}
	for _, oid := range adfOIDs {
		value := GetOIDString(evidence.PDUs, oid)
		if ContainsAny(value, []string{"auto", "adf", "feeder"}) {
			score += 0.2
			break
		}
	}

	// Weak evidence: sysDescr contains scanner keywords
	if ContainsAny(evidence.SysDescr, scannerKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
