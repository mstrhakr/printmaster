package capabilities

import "strings"

// DuplexDetector detects if device has duplex (2-sided printing) capability
type DuplexDetector struct{}

func (d *DuplexDetector) Name() string {
	return "duplex"
}

func (d *DuplexDetector) Threshold() float64 {
	return 0.7
}

func (d *DuplexDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Duplex counter exists and > 0
	duplexCounterOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.36.0",   // HP duplex sheets
		"1.3.6.1.4.1.1602.1.1.1.1.1.1.10.0",       // Canon duplex pages
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.10.6.0", // Brother duplex pages
	}

	maxDuplexCount := int64(0)
	hasDuplexCounter := false
	for _, oid := range duplexCounterOIDs {
		if HasOID(evidence.PDUs, oid) {
			hasDuplexCounter = true
			duplexCount := GetOIDValue(evidence.PDUs, oid)
			if duplexCount > maxDuplexCount {
				maxDuplexCount = duplexCount
			}
		}
	}

	if maxDuplexCount > 0 {
		score += 0.9
	} else if hasDuplexCounter {
		score += 0.6 // Has counter but not used yet
	}

	// Medium evidence: Duplex unit present in device config
	// prtOutputCapacityUnit or prtInputCapacityUnit mentions duplex
	capacityOIDs := []string{
		"1.3.6.1.2.1.43.9.2.1.13", // prtOutputCapacityUnit
		"1.3.6.1.2.1.43.8.2.1.13", // prtInputCapacityUnit
	}
	for _, oid := range capacityOIDs {
		value := GetOIDString(evidence.PDUs, oid)
		if ContainsAny(value, []string{"duplex", "2-sided", "two-sided"}) {
			score += 0.7
			break
		}
	}

	// Medium evidence: Model name indicates duplex
	// Many printers use 'd' or 'dn' suffix (n=network, d=duplex)
	// Examples: "M404dn", "M479fdw" (f=fax, d=duplex, w=wireless)
	modelLower := strings.ToLower(evidence.Model)
	if strings.Contains(modelLower, "dn") || strings.Contains(modelLower, "dw") {
		score += 0.6
	}

	// Weak evidence: Model contains duplex keywords
	duplexKeywords := []string{"duplex", "2-sided", "two-sided", "double-sided"}
	if ContainsAny(evidence.Model, duplexKeywords) {
		score += 0.5
	}

	// Weak evidence: sysDescr mentions duplex
	if ContainsAny(evidence.SysDescr, duplexKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
