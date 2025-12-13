package vendor

import (
	"context"
	"strings"

	"printmaster/agent/featureflags"
	"printmaster/agent/scanner/capabilities"
	"printmaster/common/logger"
	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

// EpsonVendor implements vendor-specific support for Epson devices.
// Provides color/mono breakdown and function-specific counters via enterprise OID 1.3.6.1.4.1.1248.*
// OID structure derived from ICE packet capture analysis.
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
		oids.SysDescr,
		oids.SysObjectID,
		oids.SysUpTime, // ICE queries this
		oids.SysName,
		oids.SysLocation, // ICE queries this
		oids.HrDeviceDescr,
		oids.HrDeviceStatus, // ICE queries this
		oids.PrtGeneralPrinterName,
		oids.PrtGeneralSerialNumber,
		oids.HrPrinterStatus + ".1",
		oids.HrPrinterDetectedErrorState + ".1",
		// Epson-specific identity
		oids.EpsonModelName,
		oids.EpsonSerialNumber,
	}
}

func (v *EpsonVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	baseOIDs := []string{
		// Standard Printer-MIB (fallback)
		oids.PrtMarkerLifeCount + ".1", // prtMarkerLifeCount (instance .1)

		// Epson enterprise page count summaries - ICE OIDs
		oids.EpsonTotalPages,
		oids.EpsonColorPages,
		oids.EpsonMonoPages,
		// Legacy OIDs for older devices
		oids.EpsonTotalPagesLegacy,
		oids.EpsonMonoPagesLegacy,
	}

	// ICE-style function counters for all 4 functions (print, copy, fax, scan)
	// These are table OIDs that walk: .27.6.1.<column>.1.1.<function>
	functionCounters := []string{
		// Function names (column 2)
		oids.EpsonFunctionNames + ".1", // Print
		oids.EpsonFunctionNames + ".2", // Copy
		oids.EpsonFunctionNames + ".3", // Fax
		oids.EpsonFunctionNames + ".4", // Scan

		// B&W counts (column 3)
		oids.EpsonFunctionBWCount + ".1", // Print B&W
		oids.EpsonFunctionBWCount + ".2", // Copy B&W
		oids.EpsonFunctionBWCount + ".3", // Fax B&W
		oids.EpsonFunctionBWCount + ".4", // Scan B&W

		// Total counts (column 4)
		oids.EpsonFunctionTotalCount + ".1", // Print Total
		oids.EpsonFunctionTotalCount + ".2", // Copy Total
		oids.EpsonFunctionTotalCount + ".3", // Fax Total
		oids.EpsonFunctionTotalCount + ".4", // Scan Total

		// Color counts (column 5)
		oids.EpsonFunctionColorCount + ".1", // Print Color
		oids.EpsonFunctionColorCount + ".2", // Copy Color
		oids.EpsonFunctionColorCount + ".3", // Fax Color
		oids.EpsonFunctionColorCount + ".4", // Scan Color
	}

	baseOIDs = append(baseOIDs, functionCounters...)
	return baseOIDs
}

func (v *EpsonVendor) SupplyOIDs() []string {
	// Use both standard Printer-MIB and Epson-specific supply tables
	return []string{
		// Standard Printer-MIB supplies
		oids.PrtMarkerSuppliesDesc,
		oids.PrtMarkerSuppliesLevel,
		oids.PrtMarkerSuppliesMaxCap,
		oids.PrtMarkerSuppliesClass,
		oids.PrtMarkerSuppliesType,
		// Epson-specific ink levels (more detailed)
		oids.EpsonSuppliesStatus,
		oids.EpsonSuppliesLevel,
	}
}

func (v *EpsonVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing Epson vendor PDUs", "pdu_count", len(pdus))
	}

	// Extract Epson enterprise summary counters
	// Try ICE OIDs first, fall back to legacy if not found
	totalPages := getOIDInt(pdus, oids.EpsonTotalPages)
	if totalPages == 0 {
		totalPages = getOIDInt(pdus, oids.EpsonTotalPagesLegacy)
	}
	colorPages := getOIDInt(pdus, oids.EpsonColorPages)
	monoPages := getOIDInt(pdus, oids.EpsonMonoPages)
	if monoPages == 0 {
		monoPages = getOIDInt(pdus, oids.EpsonMonoPagesLegacy)
	}

	// Page count totals
	if totalPages > 0 {
		result["total_pages"] = totalPages
		result["page_count"] = totalPages
	}

	if monoPages > 0 {
		result["mono_pages"] = monoPages
	}

	if colorPages > 0 {
		result["color_pages"] = colorPages
	}

	// Parse function counters (ICE-style)
	// Print function (1)
	printTotal := getOIDInt(pdus, oids.EpsonFunctionTotalCount+".1")
	printColor := getOIDInt(pdus, oids.EpsonFunctionColorCount+".1")
	printBW := getOIDInt(pdus, oids.EpsonFunctionBWCount+".1")
	if printTotal > 0 {
		result["print_pages"] = printTotal
	}
	if printColor > 0 {
		result["print_color_pages"] = printColor
	}
	if printBW > 0 {
		result["print_mono_pages"] = printBW
	} else if printTotal > 0 && printColor > 0 {
		// Calculate B&W by subtraction if not directly available
		if bw := printTotal - printColor; bw > 0 {
			result["print_mono_pages"] = bw
		}
	}

	// Copy function (2)
	copyTotal := getOIDInt(pdus, oids.EpsonFunctionTotalCount+".2")
	copyColor := getOIDInt(pdus, oids.EpsonFunctionColorCount+".2")
	copyBW := getOIDInt(pdus, oids.EpsonFunctionBWCount+".2")
	if copyTotal > 0 {
		result["copy_pages"] = copyTotal
	}
	if copyColor > 0 {
		result["copy_color_pages"] = copyColor
	}
	if copyBW > 0 {
		result["copy_mono_pages"] = copyBW
	} else if copyTotal > 0 && copyColor > 0 {
		if bw := copyTotal - copyColor; bw > 0 {
			result["copy_mono_pages"] = bw
		}
	}

	// Fax function (3)
	faxTotal := getOIDInt(pdus, oids.EpsonFunctionTotalCount+".3")
	faxBW := getOIDInt(pdus, oids.EpsonFunctionBWCount+".3")
	if faxTotal > 0 {
		result["fax_pages"] = faxTotal
	}
	if faxBW > 0 {
		result["fax_mono_pages"] = faxBW
	}

	// Scan function (4)
	scanTotal := getOIDInt(pdus, oids.EpsonFunctionTotalCount+".4")
	if scanTotal > 0 {
		result["scan_count"] = scanTotal
	}

	// Parse supply levels using generic parser (handles both standard and Epson supplies)
	supplies := parseSuppliesTable(pdus)
	for k, v := range supplies {
		result[k] = v
	}

	// Also try Epson-specific ink levels
	epsonSupplies := v.parseEpsonSupplies(pdus)
	for k, v := range epsonSupplies {
		// Only add if not already set by standard table
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}

	// Fallback to standard Printer-MIB if enterprise OIDs failed
	if _, ok := result["page_count"]; !ok {
		if pageCount := getOIDInt(pdus, oids.PrtMarkerLifeCount+".1"); pageCount > 0 {
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

// ParseWithRemoteMode is an extended version of Parse that also queries
// Epson remote-mode commands when the feature flag is enabled.
// This provides richer data (ink levels, maintenance box status) that
// standard SNMP OIDs often don't expose on Epson printers.
func (v *EpsonVendor) ParseWithRemoteMode(ctx context.Context, pdus []gosnmp.SnmpPDU, ip string, timeoutSeconds int) map[string]interface{} {
	// Start with standard parsing
	result := v.Parse(pdus)

	// If remote mode is enabled, fetch additional metrics
	if featureflags.EpsonRemoteModeEnabled() && ip != "" {
		if logger.Global != nil {
			logger.Global.TraceTag("epson_remote", "Epson remote mode: fetching extended metrics", "ip", ip)
		}

		remoteMetrics := FetchEpsonRemoteMetricsWithIP(ctx, ip, timeoutSeconds)
		if remoteMetrics != nil {
			// Merge remote metrics, preferring remote data for ink levels
			// since it's typically more accurate than standard SNMP
			for k, v := range remoteMetrics {
				// For ink levels, always prefer remote mode data
				if strings.HasPrefix(k, "ink_") || strings.HasPrefix(k, "maintenance_") ||
					strings.HasPrefix(k, "waste_") || strings.HasPrefix(k, "epson_") {
					result[k] = v
				} else if _, exists := result[k]; !exists {
					// For other metrics, only add if not already present
					result[k] = v
				}
			}

			if logger.Global != nil {
				logger.Global.Debug("Epson remote mode: merged metrics",
					"ip", ip,
					"remote_metrics", len(remoteMetrics),
					"total_metrics", len(result))
			}
		}
	} else if logger.Global != nil {
		// Keep this as trace: it can be called a lot during discovery.
		logger.Global.TraceTag("epson_remote", "Epson remote mode: skipped", "enabled", featureflags.EpsonRemoteModeEnabled(), "ip_set", ip != "")
	}

	return result
}

// parseEpsonSupplies parses Epson-specific ink level table (1.2.2.2.1.1)
func (v *EpsonVendor) parseEpsonSupplies(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})

	// Epson ink table: .1.2.2.2.1.1.3.1.<ink_index>
	// Indexes typically: 1=Black, 2=Cyan, 3=Magenta, 4=Yellow, etc.
	colorMap := map[string]string{
		".1": "ink_black",
		".2": "ink_cyan",
		".3": "ink_magenta",
		".4": "ink_yellow",
		".5": "ink_light_cyan",
		".6": "ink_light_magenta",
	}

	for _, pdu := range pdus {
		oid := normalizeOID(pdu.Name)
		// Check if this is an Epson ink level OID
		if strings.HasPrefix(oid, "1.3.6.1.4.1.1248.1.2.2.2.1.1.3.1.") {
			suffix := strings.TrimPrefix(oid, "1.3.6.1.4.1.1248.1.2.2.2.1.1.3.1")
			if metricName, ok := colorMap[suffix]; ok {
				level := coerceToInt(pdu.Value)
				if level >= 0 && level <= 100 {
					result[metricName] = float64(level)
				}
			}
		}
	}

	return result
}
