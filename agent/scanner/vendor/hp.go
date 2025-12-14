package vendor

import (
	"strings"

	"printmaster/agent/scanner/capabilities"
	"printmaster/common/logger"
	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

const hpIEEE1284DeviceIDOID = "1.3.6.1.4.1.11.2.3.9.1.1.7.0"

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
		oids.SysDescr,
		oids.SysObjectID,
		oids.SysName,
		oids.HrDeviceDescr,
		oids.PrtGeneralPrinterName,
		oids.PrtGeneralSerialNumber,
		oids.HrPrinterStatus + ".1",
		hpIEEE1284DeviceIDOID, // HP-specific IEEE-1284 payload
	}
}

func (v *HPVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	oidList := []string{
		// Standard Printer-MIB (fallback)
		oids.PrtMarkerLifeCount + ".1", // prtMarkerLifeCount (instance .1)

		// HP enterprise counters - common across many models
		// Base: 1.3.6.1.4.1.11.2.3.9.4.2.*
		"1.3.6.1.4.1.11.2.3.9.4.2.1.1.4.1.1", // Total pages (alternative)
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.7.0", // Color pages
		"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.8.0", // Monochrome pages
	}

	// Add MFP-specific counters if device has copier/scanner
	if caps != nil && (caps.IsCopier || caps.IsScanner) {
		oidList = append(oidList,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.3.0",    // Copy pages
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.0",    // ADF scan pages
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.1.0",    // Flatbed scan pages
			"1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.44.0", // ADF images scanned (lifetime)
			"1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.45.0", // ADF images scanned to host
			"1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.73.0", // Flatbed images scanned to host
			"1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.74.0", // Flatbed images scanned (lifetime)
			"1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.43.0", // Scanner jam events
		)
	}

	// Add fax counters if device has fax
	if caps != nil && caps.IsFax {
		oidList = append(oidList,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.1.0",    // Fax pages sent
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.2.0",    // Fax pages received
			"1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.31.0", // Fax ADF images scanned
			"1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.36.0", // Fax flatbed images scanned
			"1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.32.0", // Fax impressions
		)
	}

	// Add duplex counter if device has duplex
	if caps != nil && caps.HasDuplex {
		oidList = append(oidList,
			"1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.6.0", // Duplex sheets
		)
	}

	// Jam event counter
	oidList = append(oidList, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.0") // Paper jams

	// Extended jam summary counter (always-on due to minimal cost)
	oidList = append(oidList, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.34.0")
	return oidList
}

func (v *HPVendor) SupplyOIDs() []string {
	// Use standard Printer-MIB supply tables
	return []string{
		oids.PrtMarkerSuppliesDesc,
		oids.PrtMarkerSuppliesLevel,
		oids.PrtMarkerSuppliesMaxCap,
		oids.PrtMarkerSuppliesClass,
		oids.PrtMarkerSuppliesType,
	}
}

func (v *HPVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing HP vendor PDUs", "pdu_count", len(pdus))
	}

	idx := newPDUIndex(pdus)

	// Extract HP enterprise counters
	colorPages := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.7.0")
	monoPages := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.8.0")
	copyPages := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.3.0")
	adfScans := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.0")
	flatbedScans := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.1.0")
	faxSent := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.1.0")
	faxReceived := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.2.2.0")
	duplexSheets := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.4.6.0")
	jamEvents := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.9.0")

	// Extended metrics
	jamEventsTotal := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.4.1.2.34.0")
	adfScansToHost := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.45.0")
	flatbedScansToHost := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.73.0")
	faxAdfScans := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.31.0")
	faxFlatbedScans := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.36.0")
	faxImpressions := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.3.7.1.32.0")
	adfImagesScanned := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.44.0")
	flatbedImagesScanned := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.74.0")
	scannerJamEvents := getOIDIntIndexed(idx, pdus, "1.3.6.1.4.1.11.2.3.9.4.2.1.2.2.1.43.0")

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
	if jamEventsTotal > 0 {
		result["jam_events_total"] = jamEventsTotal
	}
	if scannerJamEvents > 0 {
		result["scanner_jam_events"] = scannerJamEvents
	}

	// Extended scan metrics
	if adfScansToHost > 0 {
		result["scan_adf_to_host_images"] = adfScansToHost
	}
	if flatbedScansToHost > 0 {
		result["scan_flatbed_to_host_images"] = flatbedScansToHost
	}
	if adfImagesScanned > 0 {
		result["scan_adf_images"] = adfImagesScanned
	}
	if flatbedImagesScanned > 0 {
		result["scan_flatbed_images"] = flatbedImagesScanned
	}

	// Extended fax metrics
	if faxAdfScans > 0 {
		result["fax_scan_adf"] = faxAdfScans
	}
	if faxFlatbedScans > 0 {
		result["fax_scan_flatbed"] = faxFlatbedScans
	}
	if faxImpressions > 0 {
		result["fax_impressions"] = faxImpressions
	}

	// Attempt firmware extraction from any string PDUs (sysDescr or HP enterprise values)
	if fw := extractFirmwareVersion(pdus); fw != "" {
		result["firmware_version"] = fw
		if logger.Global != nil {
			logger.Global.Info("HP firmware extracted", "version", fw)
		}
	}

	// Parse supply levels using generic parser
	supplies := parseSuppliesTable(pdus)
	for k, v := range supplies {
		result[k] = v
	}
	if logger.Global != nil {
		logger.Global.Debug("HP supplies parsed", "supplies_count", len(supplies))
	}

	// Fallback to standard Printer-MIB if enterprise OIDs failed
	if _, ok := result["page_count"]; !ok {
		if pageCount := getOIDIntIndexed(idx, pdus, oids.PrtMarkerLifeCount+".1"); pageCount > 0 {
			result["page_count"] = pageCount
			result["total_pages"] = pageCount
		}
	}

	if logger.Global != nil {
		logger.Global.Debug("HP parsing complete",
			"color_pages", colorPages,
			"mono_pages", monoPages,
			"copy_pages", copyPages,
			"scan_adf", adfScans,
			"scan_flatbed", flatbedScans,
			"fax_pages", faxSent+faxReceived,
			"duplex_sheets", duplexSheets,
			"jam_events", jamEvents,
		)
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
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
