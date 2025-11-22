package vendor

import (
	"strings"

	"printmaster/agent/scanner/capabilities"
	"printmaster/common/logger"

	"github.com/gosnmp/gosnmp"
)

// EpsonVendor implements vendor-specific support for Epson devices.
// Provides color/mono breakdown and function-specific counters via enterprise OID 1.3.6.1.4.1.1248.*
// Reference: docs/vendor/EPSON_OID_MAPPING.md
type EpsonVendor struct{}

func init() {
	RegisterVendor(&EpsonVendor{})
}

func (v *EpsonVendor) Name() string {
	return "Epson"
}

func (v *EpsonVendor) Detect(sysObjectID, sysDescr, model string) bool {
	// Check enterprise number (1248)
	if strings.Contains(sysObjectID, ".1.3.6.1.4.1.1248.") {
		return true
	}

	// Check sysDescr or model for "Epson"
	combined := strings.ToLower(sysDescr + " " + model)
	return strings.Contains(combined, "epson") || strings.Contains(combined, "workforce") || strings.Contains(combined, "surecolor")
}

func (v *EpsonVendor) BaseOIDs() []string {
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

func (v *EpsonVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	oids := []string{
		// Standard Printer-MIB (fallback)
		"1.3.6.1.2.1.43.10.2.1.4.1.1", // prtMarkerLifeCount (instance .1)

		// Epson enterprise OID base: 1.3.6.1.4.1.1248.1.2.2.27.*
		// Page count totals
		"1.3.6.1.4.1.1248.1.2.2.27.1.1.30.1.1", // Total pages
		"1.3.6.1.4.1.1248.1.2.2.27.1.1.3.1.1",  // B&W pages
		"1.3.6.1.4.1.1248.1.2.2.27.1.1.4.1.1",  // Color pages
	}

	// Add function counters if device is MFP
	if caps != nil && (caps.IsCopier || caps.IsScanner) {
		oids = append(oids,
			// Function counters (combined B&W + Color)
			"1.3.6.1.4.1.1248.1.2.2.27.6.1.4.1.1.1", // Total print from computer
			"1.3.6.1.4.1.1248.1.2.2.27.6.1.4.1.1.2", // Total copy

			// Color-specific counters
			"1.3.6.1.4.1.1248.1.2.2.27.6.1.5.1.1.1", // Color print from computer
			"1.3.6.1.4.1.1248.1.2.2.27.6.1.5.1.1.2", // Color copy
		)
	}

	return oids
}

func (v *EpsonVendor) SupplyOIDs() []string {
	// Use standard Printer-MIB supply tables
	// Epson uses standard prtMarkerSuppliesTable for toner/ink levels
	return []string{
		"1.3.6.1.2.1.43.11.1.1.6", // prtMarkerSuppliesDescription
		"1.3.6.1.2.1.43.11.1.1.9", // prtMarkerSuppliesLevel
		"1.3.6.1.2.1.43.11.1.1.8", // prtMarkerSuppliesMaxCapacity
		"1.3.6.1.2.1.43.11.1.1.4", // prtMarkerSuppliesClass
		"1.3.6.1.2.1.43.11.1.1.5", // prtMarkerSuppliesType
	}
}

func (v *EpsonVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing Epson vendor PDUs", "pdu_count", len(pdus))
	}

	// Extract Epson enterprise counters
	totalPages := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.1.1.30.1.1")
	bwPages := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.1.1.3.1.1")
	colorPages := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.1.1.4.1.1")

	totalPrintComputer := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.6.1.4.1.1.1")
	totalCopy := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.6.1.4.1.1.2")

	colorPrintComputer := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.6.1.5.1.1.1")
	colorCopy := getOIDInt(pdus, "1.3.6.1.4.1.1248.1.2.2.27.6.1.5.1.1.2")

	// Page count totals
	if totalPages > 0 {
		result["total_pages"] = totalPages
		result["page_count"] = totalPages
	}

	if bwPages > 0 {
		result["mono_pages"] = bwPages
	}

	if colorPages > 0 {
		result["color_pages"] = colorPages
	}

	// Function-specific counters
	if totalCopy > 0 {
		result["copy_pages"] = totalCopy
	}

	if colorCopy > 0 {
		result["copy_color_pages"] = colorCopy

		// Calculate B&W copy by subtraction
		if totalCopy > 0 {
			bwCopy := totalCopy - colorCopy
			if bwCopy > 0 {
				result["copy_mono_pages"] = bwCopy
			}
		}
	}

	// Print from computer counters
	if colorPrintComputer > 0 {
		result["print_color_pages"] = colorPrintComputer
	}

	if totalPrintComputer > 0 && colorPrintComputer > 0 {
		// Calculate B&W print by subtraction
		bwPrint := totalPrintComputer - colorPrintComputer
		if bwPrint > 0 {
			result["print_mono_pages"] = bwPrint
		}
	}

	// Parse supply levels using generic parser
	supplies := parseSuppliesTable(pdus)
	for k, v := range supplies {
		result[k] = v
	}

	// Fallback to standard Printer-MIB if enterprise OIDs failed
	if _, ok := result["page_count"]; !ok {
		if pageCount := getOIDInt(pdus, "1.3.6.1.2.1.43.10.2.1.4.1.1"); pageCount > 0 {
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
		logger.Global.Debug("Epson parsing complete", "mono_pages", mono, "color_pages", color)
	}
	return result
}
