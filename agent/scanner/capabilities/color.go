package capabilities

import "strings"

// ColorDetector detects if device has color printing capability
type ColorDetector struct{}

func (d *ColorDetector) Name() string {
	return "color"
}

func (d *ColorDetector) Threshold() float64 {
	return 0.7
}

func (d *ColorDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Colorant names (prtMarkerColorantValue)
	colorantOIDs := []string{
		"1.3.6.1.2.1.43.12.1.1.4.1.1", // Black
		"1.3.6.1.2.1.43.12.1.1.4.1.2", // Cyan
		"1.3.6.1.2.1.43.12.1.1.4.1.3", // Magenta
		"1.3.6.1.2.1.43.12.1.1.4.1.4", // Yellow
	}

	hasCyan := false
	hasMagenta := false
	hasYellow := false

	for _, oid := range colorantOIDs {
		value := GetOIDString(evidence.PDUs, oid)
		valueLower := strings.ToLower(value)
		if strings.Contains(valueLower, "cyan") {
			hasCyan = true
		}
		if strings.Contains(valueLower, "magenta") {
			hasMagenta = true
		}
		if strings.Contains(valueLower, "yellow") {
			hasYellow = true
		}
	}

	// If has CMY, definitely color
	if hasCyan && hasMagenta && hasYellow {
		score += 0.9
		return Min(score, 1.0) // Early return, high confidence
	}

	// Strong evidence: Color page counter exists and > 0
	colorPageOIDs := []string{
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.7.0",    // HP color pages
		"1.3.6.1.4.1.1602.1.1.1.1.1.1.7.0",        // Canon color pages
		"1.3.6.1.4.1.2435.2.3.9.4.2.1.5.5.10.2.0", // Brother color pages
		"1.3.6.1.4.1.1248.1.2.2.27.1.1.4.1.1",     // Epson color total
		"1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3",       // Kyocera printer color
	}
	for _, oid := range colorPageOIDs {
		if HasOID(evidence.PDUs, oid) {
			colorPages := GetOIDValue(evidence.PDUs, oid)
			if colorPages > 0 {
				score += 0.8
				break
			} else {
				// Counter exists but zero - might not have printed color yet
				score += 0.3
				break
			}
		}
	}

	// Strong evidence: Consumable names contain color indicators
	consumableKeywords := []string{"cyan", "magenta", "yellow", "cmy", "color"}
	for _, pdu := range evidence.PDUs {
		// Check supply descriptions
		if strings.Contains(pdu.Name, "1.3.6.1.2.1.43.11.1.1.6.1") {
			desc := GetOIDString(evidence.PDUs, pdu.Name)
			descLower := strings.ToLower(desc)
			if ContainsAny(descLower, consumableKeywords) {
				score += 0.7
				break
			}
		}
	}

	// Medium evidence: Model name contains color keywords
	colorKeywords := []string{"color", "colour", "clx", "clp", "c5", "cp"}
	if ContainsAny(evidence.Model, colorKeywords) {
		score += 0.6
	}

	// Epson model-based color detection
	modelLower := strings.ToLower(evidence.Model)
	if strings.Contains(modelLower, "epson") {
		// AM-C = Advanced MFP Color (color device)
		if strings.Contains(modelLower, "am-c") {
			score += 0.9
		}
		// WF-C = WorkForce Color (color device)
		if strings.Contains(modelLower, "wf-c") {
			score += 0.9
		}
		// CW-C = ColorWorks Color (color label printer)
		if strings.Contains(modelLower, "cw-c") {
			score += 0.9
		}
		// WF-M = WorkForce Mono (NOT color)
		// ST-M = SureColor Mono sublimation (NOT color, despite "SureColor" brand)
		// These are explicitly mono devices
	}

	// Kyocera model-based color detection
	if strings.Contains(modelLower, "kyocera") || strings.Contains(evidence.Vendor, "Kyocera") {
		// Models ending in 'ci' = color imaging
		if strings.HasSuffix(modelLower, "ci") || strings.Contains(modelLower, "ci ") {
			score += 0.9
		}
		// ECOSYS without 'c' in model number is typically mono
		if strings.Contains(modelLower, "ecosys") && !strings.Contains(modelLower, "ecosys m") {
			// Check if model has 'c' prefix indicating color (e.g., ECOSYS M6635cidn has 'c')
			// But plain ECOSYS PA4000wx is mono
		}
	}

	// Weak evidence: Consumable count (4+ suggests CMYK)
	supplyOIDs := []string{
		"1.3.6.1.2.1.43.11.1.1.6.1", // prtMarkerSuppliesDescription
	}
	supplyCount := 0
	for _, pdu := range evidence.PDUs {
		for _, supplyOID := range supplyOIDs {
			if strings.HasPrefix(normalizeOID(pdu.Name), normalizeOID(supplyOID)) {
				supplyCount++
			}
		}
	}
	if supplyCount >= 4 {
		score += 0.3
	}

	// Weak evidence: sysDescr contains color keywords
	if ContainsAny(evidence.SysDescr, colorKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
