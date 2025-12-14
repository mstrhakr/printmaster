package capabilities

// FaxDetector detects if device has fax capability
type FaxDetector struct{}

func (d *FaxDetector) Name() string {
	return "fax"
}

func (d *FaxDetector) Threshold() float64 {
	return 0.7
}

func (d *FaxDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Fax page counter exists and > 0
	faxPageOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.5.0",    // HP fax pages
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.6.0",    // HP fax pages (alternate)
		"1.3.6.1.4.1.1602.1.1.1.1.1.1.9.0",        // Canon fax pages
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.10.4.0", // Brother fax pages
	}

	maxFaxPages := int64(0)
	hasFaxCounter := false
	for _, oid := range faxPageOIDs {
		if HasOIDIn(evidence, oid) {
			hasFaxCounter = true
			faxPages := GetOIDValueIn(evidence, oid)
			if faxPages > maxFaxPages {
				maxFaxPages = faxPages
			}
		}
	}

	if maxFaxPages > 0 {
		score += 0.9
	} else if hasFaxCounter {
		score += 0.6 // Has counter but not used yet
	}

	// Medium evidence: Fax scan counters exist
	faxScanOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.2.1.0", // HP fax ADF scans
		"1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.2.2.0", // HP fax flatbed scans
	}
	if HasAnyOIDIn(evidence, faxScanOIDs) {
		score += 0.5
	}

	// Medium evidence: Model contains fax keywords
	faxKeywords := []string{"fax", "mfp", "multifunction"}
	if ContainsAny(evidence.Model, faxKeywords) {
		score += 0.4
	}

	// Weak evidence: Has modem/fax interface
	// Some devices expose fax interface info via SNMP
	ifaceOIDs := []string{
		"1.3.6.1.2.1.2.2.1.2", // ifDescr
	}
	for _, oid := range ifaceOIDs {
		value := GetOIDStringIn(evidence, oid)
		if ContainsAny(value, []string{"fax", "modem", "pstn"}) {
			score += 0.3
			break
		}
	}

	// Weak evidence: sysDescr mentions fax
	if ContainsAny(evidence.SysDescr, faxKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
