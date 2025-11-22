package vendor

import (
	"strings"

	"printmaster/agent/scanner/capabilities"

	"github.com/gosnmp/gosnmp"
)

// HPVendor implements vendor-specific support for HP/Hewlett-Packard devices.
// Provides extended metrics via enterprise OID 1.3.6.1.4.1.11.*
type HPVendor struct{}

func init() {
	RegisterVendor(&HPVendor{})
}

func (v *HPVendor) Name() string {
	return "HP"
}

func (v *HPVendor) Detect(sysObjectID, sysDescr, model string) bool {
	// Check enterprise number (11)
	if strings.Contains(sysObjectID, ".1.3.6.1.4.1.11.") {
		return true
	}

	// Check sysDescr or model for "HP" or "Hewlett-Packard"
	combined := strings.ToLower(sysDescr + " " + model)
	return strings.Contains(combined, "hp ") || strings.Contains(combined, "hewlett") || strings.Contains(combined, "laserjet") || strings.Contains(combined, "officejet")
}

func (v *HPVendor) BaseOIDs() []string {
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

func (v *HPVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	oids := []string{
		// Standard Printer-MIB (fallback)
		"1.3.6.1.2.1.43.10.2.1.4.1", // prtMarkerLifeCount

		// HP enterprise counters - common across many models
		// Base: 1.3.6.1.4.1.11.2.3.9.4.2.*
		"1.3.6.1.4.1.11.2.3.9.4.2.1.1.4.1.1", // Total pages (alternative)
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.7.0", // Color pages
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.8.0", // Monochrome pages
	}

	// Add MFP-specific counters if device has copier/scanner
	if caps != nil && (caps.IsCopier || caps.IsScanner) {
		oids = append(oids,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.3.0", // Copy pages
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.0", // ADF scan pages
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.1.0", // Flatbed scan pages
		)
	}

	// Add fax counters if device has fax
	if caps != nil && caps.IsFax {
		oids = append(oids,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.1.0", // Fax pages sent
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.2.0", // Fax pages received
		)
	}

	// Add duplex counter if device has duplex
	if caps != nil && caps.HasDuplex {
		oids = append(oids,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.6.0", // Duplex sheets
		)
	}

	// Jam event counter
	oids = append(oids, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.0") // Paper jams

	return oids
}

func (v *HPVendor) SupplyOIDs() []string {
	// Use standard Printer-MIB supply tables
	return []string{
		"1.3.6.1.2.1.43.11.1.1.6", // prtMarkerSuppliesDescription
		"1.3.6.1.2.1.43.11.1.1.9", // prtMarkerSuppliesLevel
		"1.3.6.1.2.1.43.11.1.1.8", // prtMarkerSuppliesMaxCapacity
		"1.3.6.1.2.1.43.11.1.1.4", // prtMarkerSuppliesClass
		"1.3.6.1.2.1.43.11.1.1.5", // prtMarkerSuppliesType
	}
}

func (v *HPVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})

	// Extract HP enterprise counters
	colorPages := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.7.0")
	monoPages := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.8.0")
	copyPages := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.3.0")
	adfScans := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.0")
	flatbedScans := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.1.0")
	faxSent := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.1.0")
	faxReceived := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.2.0")
	duplexSheets := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.6.0")
	jamEvents := getOIDInt(pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.0")

	// Color/Mono breakdown
	if colorPages > 0 {
		result["color_pages"] = colorPages
	}

	if monoPages > 0 {
		result["mono_pages"] = monoPages
	}

	if colorPages > 0 || monoPages > 0 {
		result["page_count"] = colorPages + monoPages
		result["total_pages"] = colorPages + monoPages
	}

	// Copy counters
	if copyPages > 0 {
		result["copy_pages"] = copyPages
	}

	// Scan counters
	if adfScans > 0 {
		result["scan_to_host_adf"] = adfScans
	}

	if flatbedScans > 0 {
		result["scan_to_host_flatbed"] = flatbedScans
	}

	if adfScans > 0 || flatbedScans > 0 {
		result["scan_count"] = adfScans + flatbedScans
	}

	// Fax counters
	if faxSent > 0 || faxReceived > 0 {
		result["fax_pages"] = faxSent + faxReceived
	}

	// Duplex counter
	if duplexSheets > 0 {
		result["duplex_sheets"] = duplexSheets
	}

	// Jam events
	if jamEvents > 0 {
		result["jam_events"] = jamEvents
	}

	// Attempt firmware extraction from any string PDUs (sysDescr or HP enterprise values)
	if fw := extractFirmwareVersion(pdus); fw != "" {
		result["firmware_version"] = fw
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

	return result
}

// extractFirmwareVersion scans PDUs for firmware/datecode patterns.
func extractFirmwareVersion(pdus []gosnmp.SnmpPDU) string {
	patterns := []string{"firmware", "datecode", "fw", "rev", "revision"}
	for _, pdu := range pdus {
		var s string
		switch v := pdu.Value.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		default:
			continue
		}
		ls := strings.ToLower(s)
		matched := false
		for _, p := range patterns {
			if strings.Contains(ls, p) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		// Common HP pattern: "Firmware Datecode: YYYYMMDD"
		if idx := strings.Index(ls, "firmware datecode"); idx >= 0 {
			// Extract 8 digit code following colon
			// naive scan
			for i := idx; i < len(s)-8; i++ {
				segment := s[i:]
				for j := 0; j < len(segment)-7; j++ {
					candidate := segment[j : j+8]
					if isAllDigits(candidate) {
						return candidate
					}
				}
			}
		}
		// Generic REV/Version token extraction
		parts := strings.Fields(s)
		for i, part := range parts {
			lp := strings.ToLower(part)
			if lp == "rev" || lp == "revision" || lp == "version" || strings.HasPrefix(lp, "rev") {
				// Next token likely version identifier
				if i+1 < len(parts) {
					candidate := strings.Trim(parts[i+1], ":;()")
					if candidate != "" {
						return candidate
					}
				}
			}
		}
	}
	return ""
}

func isAllDigits(s string) bool {
	if len(s) == 0 { return false }
	for _, r := range s {
		if r < '0' || r > '9' { return false }
	}
	return true
}
