package capabilities

import "strings"

// MonoDetector detects if device is monochrome only
type MonoDetector struct{}

func (d *MonoDetector) Name() string {
	return "mono"
}

func (d *MonoDetector) Threshold() float64 {
	return 0.7
}

func (d *MonoDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// If color capability is detected, mono is low
	if colorScore, exists := evidence.Capabilities["color"]; exists && colorScore >= 0.5 {
		return 0.0 // Mutually exclusive with color
	}

	// Strong evidence: Only black colorant
	colorantOIDs := []string{
		"1.3.6.1.2.1.43.12.1.1.4.1.1", // First colorant
		"1.3.6.1.2.1.43.12.1.1.4.1.2", // Second colorant
	}

	colorants := []string{}
	for _, oid := range colorantOIDs {
		value := GetOIDString(evidence.PDUs, oid)
		if value != "" {
			colorants = append(colorants, strings.ToLower(value))
		}
	}

	// If only black colorant(s) found
	if len(colorants) > 0 {
		onlyBlack := true
		for _, colorant := range colorants {
			if !strings.Contains(colorant, "black") {
				onlyBlack = false
				break
			}
		}
		if onlyBlack {
			score += 0.9
		}
	}

	// Medium evidence: Model contains mono keywords
	monoKeywords := []string{"mono", "monochrome", "b&w", "black", "ml-", "slm"}
	if ContainsAny(evidence.Model, monoKeywords) {
		score += 0.7
	}

	// Weak evidence: Only 1-2 consumables (black + drum)
	supplyCount := 0
	for _, pdu := range evidence.PDUs {
		if strings.HasPrefix(normalizeOID(pdu.Name), "1.3.6.1.2.1.43.11.1.1.6.1") {
			supplyCount++
		}
	}
	if supplyCount <= 2 && supplyCount > 0 {
		score += 0.3
	}

	// Weak evidence: sysDescr contains mono keywords
	if ContainsAny(evidence.SysDescr, monoKeywords) {
		score += 0.2
	}

	return Min(score, 1.0)
}
