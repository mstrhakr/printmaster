package capabilities

// PrinterDetector detects if device is a printer
type PrinterDetector struct{}

func (d *PrinterDetector) Name() string {
	return "printer"
}

func (d *PrinterDetector) Threshold() float64 {
	return 0.7
}

func (d *PrinterDetector) Detect(evidence *DetectionEvidence) float64 {
	score := 0.0

	// Strong evidence: Serial number present
	if evidence.Serial != "" {
		score += 0.5
	}

	// Strong evidence: Printer-MIB OIDs present
	printerOIDs := []string{
		"1.3.6.1.2.1.43.5.1.1.17.1",   // prtGeneralSerialNumber
		"1.3.6.1.2.1.43.10.2.1.4.1.1", // prtMarkerLifeCount
		"1.3.6.1.2.1.25.3.2.1.3.1",    // hrDeviceDescr
	}
	if HasAnyOIDIn(evidence, printerOIDs) {
		score += 0.3
	}

	// Medium evidence: Open printer ports
	printerPorts := []int{9100, 515, 631}
	for _, port := range printerPorts {
		for _, openPort := range evidence.OpenPorts {
			if port == openPort {
				score += 0.1
				break
			}
		}
	}

	// Weak evidence: Vendor is known printer manufacturer
	printerVendors := []string{"HP", "Canon", "Brother", "Epson", "Xerox", "Kyocera", "Ricoh", "Lexmark", "Samsung"}
	for _, vendor := range printerVendors {
		if evidence.Vendor == vendor {
			score += 0.1
			break
		}
	}

	// Weak evidence: Model/sysDescr contains printer keywords
	keywords := []string{"printer", "laserjet", "deskjet", "officejet", "imagerunner", "imageclass"}
	if ContainsAny(evidence.Model, keywords) || ContainsAny(evidence.SysDescr, keywords) {
		score += 0.1
	}

	return Min(score, 1.0)
}
