package vendor

import (
	"strings"

	"printmaster/agent/scanner/capabilities"
	"printmaster/common/logger"

	"github.com/gosnmp/gosnmp"
)

// KyoceraVendor implements vendor-specific support for Kyocera devices.
// Provides comprehensive metrics via enterprise OID tree 1.3.6.1.4.1.1347.*
// Reference: docs/vendor/KYOCERA_OID_MAPPING.md
type KyoceraVendor struct{}

func init() {
	RegisterVendor(&KyoceraVendor{})
}

func (v *KyoceraVendor) Name() string {
	return "Kyocera"
}

func (v *KyoceraVendor) Detect(sysObjectID, sysDescr, model string) bool {
	// Check enterprise number (1347)
	if strings.Contains(sysObjectID, ".1.3.6.1.4.1.1347.") {
		return true
	}

	// Check sysDescr or model for "Kyocera" or "TASKalfa"
	combined := strings.ToLower(sysDescr + " " + model)
	return strings.Contains(combined, "kyocera") || strings.Contains(combined, "taskalfa") || strings.Contains(combined, "ecosys")
}

func (v *KyoceraVendor) BaseOIDs() []string {
	return []string{
		"1.3.6.1.2.1.1.1.0",         // sysDescr
		"1.3.6.1.2.1.1.2.0",         // sysObjectID
		"1.3.6.1.2.1.1.5.0",         // sysName
		"1.3.6.1.2.1.25.3.2.1.3.1",  // hrDeviceDescr
		"1.3.6.1.2.1.43.5.1.1.16.1", // prtGeneralPrinterName
		"1.3.6.1.2.1.43.5.1.1.17.1", // prtGeneralSerialNumber
		"1.3.6.1.2.1.25.3.5.1.1.1",  // hrPrinterStatus
	}
}

func (v *KyoceraVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	oids := []string{
		// Standard Printer-MIB (fallback)
		"1.3.6.1.2.1.43.10.2.1.4.1", // prtMarkerLifeCount

		// Kyocera enterprise - Total printed
		"1.3.6.1.4.1.1347.43.10.1.1.12.1.1", // Total printed pages

		// Function totals (.42.3.1.1.1.*)
		"1.3.6.1.4.1.1347.42.3.1.1.1.1", // Printer total
		"1.3.6.1.4.1.1347.42.3.1.1.1.2", // Copy total

		// Print breakdown by function and color (.42.3.1.2.1.1.*)
		"1.3.6.1.4.1.1347.42.3.1.2.1.1.1.1", // Printer B&W
		"1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3", // Printer Color
		"1.3.6.1.4.1.1347.42.3.1.2.1.1.2.1", // Copy B&W
		"1.3.6.1.4.1.1347.42.3.1.2.1.1.2.3", // Copy Color
	}

	// Add fax OIDs if device has fax capability
	if caps != nil && caps.IsFax {
		oids = append(oids,
			"1.3.6.1.4.1.1347.42.3.1.1.1.4",     // Fax total
			"1.3.6.1.4.1.1347.42.3.1.2.1.1.4.1", // Fax B&W
		)
	}

	// Add scan OIDs if device has scanner/copier capability
	if caps != nil && (caps.IsScanner || caps.IsCopier) {
		oids = append(oids,
			"1.3.6.1.4.1.1347.42.3.1.3.1.1.2", // Copy scans
			"1.3.6.1.4.1.1347.42.3.1.3.1.1.4", // Fax scans
			"1.3.6.1.4.1.1347.42.3.1.4.1.1.1", // Other scans
		)
	}

	return oids
}

func (v *KyoceraVendor) SupplyOIDs() []string {
	// Use standard Printer-MIB supply tables
	return []string{
		"1.3.6.1.2.1.43.11.1.1.6", // prtMarkerSuppliesDescription
		"1.3.6.1.2.1.43.11.1.1.9", // prtMarkerSuppliesLevel
		"1.3.6.1.2.1.43.11.1.1.8", // prtMarkerSuppliesMaxCapacity
		"1.3.6.1.2.1.43.11.1.1.4", // prtMarkerSuppliesClass
		"1.3.6.1.2.1.43.11.1.1.5", // prtMarkerSuppliesType
	}
}

func (v *KyoceraVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing Kyocera vendor PDUs", "pdu_count", len(pdus))
	}

	// Extract Kyocera enterprise counters
	printerBW := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.2.1.1.1.1")
	printerColor := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.2.1.1.1.3")
	copyBW := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.2.1.1.2.1")
	copyColor := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.2.1.1.2.3")
	faxBW := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.2.1.1.4.1")

	copyScan := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.3.1.1.2")
	faxScan := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.3.1.1.4")
	otherScan := getOIDInt(pdus, "1.3.6.1.4.1.1347.42.3.1.4.1.1.1")

	// Calculate totals
	if printerBW > 0 || copyBW > 0 || faxBW > 0 {
		result["mono_pages"] = printerBW + copyBW + faxBW
	}

	if printerColor > 0 || copyColor > 0 {
		result["color_pages"] = printerColor + copyColor
	}

	if monoPages, ok := result["mono_pages"].(int); ok {
		if colorPages, ok := result["color_pages"].(int); ok {
			result["page_count"] = monoPages + colorPages
			result["total_pages"] = monoPages + colorPages
		}
	}

	// Function-specific counters
	if copyBW > 0 || copyColor > 0 {
		result["copy_pages"] = copyBW + copyColor
		result["copy_mono_pages"] = copyBW
		result["copy_color_pages"] = copyColor
	}

	if faxBW > 0 {
		result["fax_pages"] = faxBW
	}

	// Scan counters
	if copyScan > 0 || faxScan > 0 || otherScan > 0 {
		result["scan_count"] = copyScan + faxScan + otherScan
	}

	if copyScan > 0 {
		result["copy_scans"] = copyScan
	}

	if faxScan > 0 {
		result["fax_scans"] = faxScan
	}

	if otherScan > 0 {
		result["other_scans"] = otherScan
	}

	// Parse supply levels using generic parser
	supplies := parseSuppliesTable(pdus)
	for k, v := range supplies {
		result[k] = v
	}

	// Fallback to standard Printer-MIB if enterprise OIDs failed
	if _, ok := result["page_count"]; !ok {
		if pageCount := getOIDInt(pdus, "1.3.6.1.2.1.43.10.2.1.4.1"); pageCount > 0 {
			result["page_count"] = pageCount
			result["total_pages"] = pageCount
		}
	}

	if logger.Global != nil {
		mono := 0
		color := 0
		if m, ok := result["mono_pages"].(int); ok {
			mono = m
		}
		if c, ok := result["color_pages"].(int); ok {
			color = c
		}
		logger.Global.Debug("Kyocera parsing complete", "mono_pages", mono, "color_pages", color)
	}
	return result
}
