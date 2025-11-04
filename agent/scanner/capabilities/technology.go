package capabilities

import "strings"

// LaserDetector detects if device uses laser/toner technology (as opposed to inkjet)
type LaserDetector struct{}

func (d *LaserDetector) Name() string {
	return "laser"
}

func (d *LaserDetector) Threshold() float64 {
	return 0.7
}

func (d *LaserDetector) Detect(evidence *DetectionEvidence) float64 {
	laserScore := 0.0
	inkjetScore := 0.0

	// Strong evidence: Consumable descriptions containing "toner" = laser
	for _, pdu := range evidence.PDUs {
		if strings.Contains(pdu.Name, "1.3.6.1.2.1.43.11.1.1.6.1") { // prtMarkerSuppliesDescription
			desc := GetOIDString(evidence.PDUs, pdu.Name)
			descLower := strings.ToLower(desc)

			// Toner/drum = laser technology
			if strings.Contains(descLower, "toner") || strings.Contains(descLower, "drum unit") {
				laserScore += 0.9
			}

			// Ink/inkjet = inkjet technology (reduces laser score)
			if strings.Contains(descLower, "ink") && !strings.Contains(descLower, "toner") {
				inkjetScore += 0.9
			}
		}
	}

	// Model-based detection for Epson devices
	// Epson is primarily an inkjet manufacturer - strong default to inkjet
	modelLower := strings.ToLower(evidence.Model)
	if strings.Contains(modelLower, "epson") || strings.Contains(evidence.Vendor, "Epson") {
		// AcuLaser (AL-) are the ONLY laser models (discontinued line)
		if strings.Contains(modelLower, "al-") || strings.Contains(modelLower, "aculaser") {
			laserScore += 0.9
		} else {
			// Everything else Epson is inkjet (strong evidence)
			inkjetScore += 0.8
		}

		// WorkForce series (WF-) are inkjet
		if strings.Contains(modelLower, "wf-") {
			inkjetScore += 0.9
		}

		// ColorWorks (CW-) are inkjet label printers
		if strings.Contains(modelLower, "cw-") {
			inkjetScore += 0.9
		}

		// Advanced MFP (AM-C) = inkjet MFPs
		if strings.Contains(modelLower, "am-c") {
			inkjetScore += 0.9
		}

		// EcoTank (ET-) are inkjet
		if strings.Contains(modelLower, "et-") || strings.Contains(modelLower, "ecotank") {
			inkjetScore += 0.9
		}

		// SureColor (SC-) are professional inkjet
		if strings.Contains(modelLower, "sc-") || strings.Contains(modelLower, "surecolor") {
			inkjetScore += 0.9
		}
	}

	// Model-based detection for Kyocera devices (primarily laser)
	if strings.Contains(modelLower, "kyocera") || strings.Contains(evidence.Vendor, "Kyocera") {
		// ECOSYS, TASKalfa series are laser
		if strings.Contains(modelLower, "ecosys") || strings.Contains(modelLower, "taskalfa") {
			laserScore += 0.8
		} else {
			// Generic Kyocera detection (most are laser)
			laserScore += 0.6
		}
	}

	// Model-based detection for HP devices
	if strings.Contains(modelLower, "hp ") || strings.Contains(evidence.Vendor, "HP") || strings.Contains(evidence.Vendor, "Hewlett") {
		// LaserJet series are laser
		if strings.Contains(modelLower, "laserjet") {
			laserScore += 0.9
		}

		// OfficeJet, DeskJet, Envy are inkjet
		if strings.Contains(modelLower, "officejet") || strings.Contains(modelLower, "deskjet") || strings.Contains(modelLower, "envy") {
			inkjetScore += 0.8
		}

		// PageWide are inkjet technology
		if strings.Contains(modelLower, "pagewide") {
			inkjetScore += 0.9
		}
	}

	// Model-based detection for Canon devices
	if strings.Contains(modelLower, "canon") || strings.Contains(evidence.Vendor, "Canon") {
		// imageCLASS are laser
		if strings.Contains(modelLower, "imageclass") {
			laserScore += 0.8
		}

		// PIXMA are inkjet
		if strings.Contains(modelLower, "pixma") {
			inkjetScore += 0.8
		}

		// MAXIFY are inkjet business printers
		if strings.Contains(modelLower, "maxify") {
			inkjetScore += 0.8
		}
	}

	// Model-based detection for Brother devices
	if strings.Contains(modelLower, "brother") || strings.Contains(evidence.Vendor, "Brother") {
		// HL, DCP, MFC with 'L' designation are laser
		if strings.Contains(modelLower, "hl-l") || strings.Contains(modelLower, "dcp-l") || strings.Contains(modelLower, "mfc-l") {
			laserScore += 0.8
		}

		// HL, DCP, MFC with 'J' designation are inkjet
		if strings.Contains(modelLower, "hl-j") || strings.Contains(modelLower, "dcp-j") || strings.Contains(modelLower, "mfc-j") {
			inkjetScore += 0.8
		}
	}

	// Return laser score (higher score wins)
	// If inkjet score is higher, return negative or lower value
	if inkjetScore > laserScore {
		// Device is inkjet, not laser - return low/zero score
		return Max(0.0, laserScore-inkjetScore+0.3)
	}

	return Min(laserScore, 1.0)
}

// InkjetDetector detects if device uses inkjet/ink technology
type InkjetDetector struct{}

func (d *InkjetDetector) Name() string {
	return "inkjet"
}

func (d *InkjetDetector) Threshold() float64 {
	return 0.7
}

func (d *InkjetDetector) Detect(evidence *DetectionEvidence) float64 {
	laserScore := 0.0
	inkjetScore := 0.0

	// Strong evidence: Consumable descriptions containing "ink" = inkjet
	for _, pdu := range evidence.PDUs {
		if strings.Contains(pdu.Name, "1.3.6.1.2.1.43.11.1.1.6.1") { // prtMarkerSuppliesDescription
			desc := GetOIDString(evidence.PDUs, pdu.Name)
			descLower := strings.ToLower(desc)

			// Ink/inkjet = inkjet technology
			if strings.Contains(descLower, "ink") && !strings.Contains(descLower, "toner") {
				inkjetScore += 0.9
			}

			// Toner = laser (reduces inkjet score)
			if strings.Contains(descLower, "toner") || strings.Contains(descLower, "drum unit") {
				laserScore += 0.9
			}
		}
	}

	// Model-based detection (same as LaserDetector but inverted scoring)
	modelLower := strings.ToLower(evidence.Model)

	// Epson - primarily inkjet manufacturer (strong default to inkjet)
	if strings.Contains(modelLower, "epson") || strings.Contains(evidence.Vendor, "Epson") {
		// AcuLaser (AL-) are the ONLY laser models (discontinued)
		if strings.Contains(modelLower, "al-") || strings.Contains(modelLower, "aculaser") {
			laserScore += 0.9
		} else {
			// Everything else Epson is inkjet (strong evidence)
			inkjetScore += 0.8
		}

		// All current Epson lines are inkjet
		if strings.Contains(modelLower, "wf-") || strings.Contains(modelLower, "workforce") {
			inkjetScore += 0.9
		}
		if strings.Contains(modelLower, "cw-") || strings.Contains(modelLower, "colorworks") {
			inkjetScore += 0.9
		}
		if strings.Contains(modelLower, "am-c") {
			inkjetScore += 0.9
		}
		if strings.Contains(modelLower, "et-") || strings.Contains(modelLower, "ecotank") {
			inkjetScore += 0.9
		}
		if strings.Contains(modelLower, "sc-") || strings.Contains(modelLower, "surecolor") {
			inkjetScore += 0.9
		}
	}

	// Kyocera (primarily laser)
	if strings.Contains(modelLower, "kyocera") || strings.Contains(evidence.Vendor, "Kyocera") {
		if strings.Contains(modelLower, "ecosys") || strings.Contains(modelLower, "taskalfa") {
			laserScore += 0.8
		} else {
			laserScore += 0.6
		}
	}

	// HP
	if strings.Contains(modelLower, "hp ") || strings.Contains(evidence.Vendor, "HP") || strings.Contains(evidence.Vendor, "Hewlett") {
		if strings.Contains(modelLower, "laserjet") {
			laserScore += 0.9
		}
		if strings.Contains(modelLower, "officejet") || strings.Contains(modelLower, "deskjet") || strings.Contains(modelLower, "envy") {
			inkjetScore += 0.8
		}
		if strings.Contains(modelLower, "pagewide") {
			inkjetScore += 0.9
		}
	}

	// Canon
	if strings.Contains(modelLower, "canon") || strings.Contains(evidence.Vendor, "Canon") {
		if strings.Contains(modelLower, "imageclass") {
			laserScore += 0.8
		}
		if strings.Contains(modelLower, "pixma") {
			inkjetScore += 0.8
		}
		if strings.Contains(modelLower, "maxify") {
			inkjetScore += 0.8
		}
	}

	// Brother
	if strings.Contains(modelLower, "brother") || strings.Contains(evidence.Vendor, "Brother") {
		if strings.Contains(modelLower, "hl-l") || strings.Contains(modelLower, "dcp-l") || strings.Contains(modelLower, "mfc-l") {
			laserScore += 0.8
		}
		if strings.Contains(modelLower, "hl-j") || strings.Contains(modelLower, "dcp-j") || strings.Contains(modelLower, "mfc-j") {
			inkjetScore += 0.8
		}
	}

	// Return inkjet score (higher score wins)
	if laserScore > inkjetScore {
		// Device is laser, not inkjet - return low/zero score
		return Max(0.0, inkjetScore-laserScore+0.3)
	}

	return Min(inkjetScore, 1.0)
}
