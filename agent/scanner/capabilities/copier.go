package capabilities

import "strings"

// CopierDetector detects if device has copy capability
type CopierDetector struct{}

func (d *CopierDetector) Name() string {
	return "copier"
}

func (d *CopierDetector) Threshold() float64 {
	return 0.7
}

func (d *CopierDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Copy page counter exists and > 0
	copyPageOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.8.0",    // HP copy pages
		"1.3.6.1.4.1.1602.1.1.1.1.1.1.8.0",        // Canon copy pages
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.10.3.0", // Brother copy pages
		"1.3.6.1.4.1.1248.1.2.2.27.6.1.4.1.1.2",   // Epson copy total
		"1.3.6.1.4.1.1347.42.3.1.1.1.2",           // Kyocera copy total
	}

	for _, oid := range copyPageOIDs {
		if HasOID(evidence.PDUs, oid) {
			copyPages := GetOIDValue(evidence.PDUs, oid)
			if copyPages > 0 {
				score += 0.9
				break
			} else {
				score += 0.7 // Counter exists but not used yet
				break
			}
		}
	}

	// Strong evidence: Found "copy" in any OID names or descriptions
	for _, pdu := range evidence.PDUs {
		oidName := strings.ToLower(pdu.Name)
		// Look for copy counters in enterprise OID trees
		if strings.Contains(oidName, "1.3.6.1.4.1") {
			// Check if this looks like a copy counter (copy in path or high value suggesting page count)
			if strings.Contains(oidName, ".27.6.1.") && pdu.Value != nil { // Epson function counters
				if val := GetOIDValue(evidence.PDUs, pdu.Name); val > 0 {
					score += 0.8
					break
				}
			}
			if strings.Contains(oidName, ".42.3.1.1.1.2") { // Kyocera copy total
				if val := GetOIDValue(evidence.PDUs, pdu.Name); val > 0 {
					score += 0.8
					break
				}
			}
		}
	}

	// Medium evidence: Scan counters exist (copy requires scan)
	scanCounterOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.1.2.0", // HP flatbed scans
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.1.1.0", // HP ADF scans
		"1.3.6.1.4.1.1602.1.1.1.4.1.1.0",       // Canon scan counter
	}
	if HasAnyOID(evidence.PDUs, scanCounterOIDs) {
		score += 0.5
	}

	// Medium evidence: Model contains MFP/multifunction keywords
	mfpKeywords := []string{"mfp", "multifunction", "all-in-one", "aio", "multi function"}
	if ContainsAny(evidence.Model, mfpKeywords) {
		score += 0.6
	}

	// Epson model-based detection
	modelLower := strings.ToLower(evidence.Model)
	if strings.Contains(modelLower, "epson") {
		// AM-C series = Advanced MFP Color (always has copy)
		if strings.Contains(modelLower, "am-c") {
			score += 0.8
		}
		// WF-M series = WorkForce Mono MFP (has copy)
		if strings.Contains(modelLower, "wf-m") {
			score += 0.8
		}
		// WF-C series with higher model numbers often MFP (17xxx, 20xxx)
		if strings.Contains(modelLower, "wf-c17") || strings.Contains(modelLower, "wf-c20") {
			score += 0.7
		}
	}

	// Kyocera model-based detection
	if strings.Contains(modelLower, "kyocera") || strings.Contains(evidence.Vendor, "Kyocera") {
		// TASKalfa series = always MFP with copy
		if strings.Contains(modelLower, "taskalfa") {
			score += 0.9
		}
		// Kyocera models ending in 'ci' = color imaging MFP
		if strings.HasSuffix(modelLower, "ci") {
			score += 0.8
		}
	}

	// Weak evidence: Has ADF (automatic document feeder)
	// prtInputType contains autoDocumentFeeder
	adfOIDs := []string{
		"1.3.6.1.2.1.43.8.2.1.2", // prtInputType
	}
	for _, oid := range adfOIDs {
		value := GetOIDString(evidence.PDUs, oid)
		if ContainsAny(value, []string{"auto", "adf", "feeder"}) {
			score += 0.3
			break
		}
	}

	// Weak evidence: sysDescr contains MFP keywords
	if ContainsAny(evidence.SysDescr, mfpKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
