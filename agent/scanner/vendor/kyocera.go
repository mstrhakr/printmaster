package vendor

import (
	"fmt"
	"strings"

	"printmaster/agent/scanner/capabilities"
	"printmaster/common/logger"
	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

// KyoceraVendor implements vendor-specific support for Kyocera devices.
// Provides comprehensive metrics via enterprise OID tree 1.3.6.1.4.1.1347.*
// OID structure derived from ICE packet capture analysis.
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
		// Kyocera-specific identity
		oids.KyoceraModelName,
		oids.KyoceraSerialNumber,
		oids.KyoceraStatusInfo,
	}
}

func (v *KyoceraVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	baseOIDs := []string{
		// Standard Printer-MIB (fallback)
		oids.PrtMarkerLifeCount + ".1", // prtMarkerLifeCount (instance .1)

		// Kyocera enterprise - Total printed (extended Printer MIB)
		oids.KyoceraTotalPrinted,

		// Print breakdown by function and color (.42.3.1.2.1.1.*)
		// Function 1 = Print, 2 = Copy, 3 = Scan(N/A), 4 = Fax
		// Color mode: 1 = B&W, 2 = Single color, 3 = Full color
		oids.KyoceraPrintBW,
		oids.KyoceraPrintColor,
		oids.KyoceraCopyBW,
		oids.KyoceraCopyColor,
	}

	// Add fax OIDs always (let the parsing handle missing values)
	baseOIDs = append(baseOIDs, oids.KyoceraFaxBW)

	// Add scan OIDs always
	baseOIDs = append(baseOIDs,
		oids.KyoceraCopyScans,
		oids.KyoceraFaxScans,
		oids.KyoceraOtherScans,
	)

	// ICE-style: Add extended counter table rows (42.5.4.1.2.1-17)
	// These contain more granular breakdowns
	for i := 1; i <= 17; i++ {
		baseOIDs = append(baseOIDs, oids.KyoceraDetailedCounters+"."+fmt.Sprint(i))
	}

	return baseOIDs
}

func (v *KyoceraVendor) SupplyOIDs() []string {
	// Use standard Printer-MIB supply tables
	return []string{
		oids.PrtMarkerSuppliesDesc,
		oids.PrtMarkerSuppliesLevel,
		oids.PrtMarkerSuppliesMaxCap,
		oids.PrtMarkerSuppliesClass,
		oids.PrtMarkerSuppliesType,
	}
}

func (v *KyoceraVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing Kyocera vendor PDUs", "pdu_count", len(pdus))
	}

	idx := newPDUIndex(pdus)

	// Extract Kyocera enterprise counters
	// Print function (direct prints from network/USB)
	printerBW := getOIDIntIndexed(idx, pdus, oids.KyoceraPrintBW)
	printerColor := getOIDIntIndexed(idx, pdus, oids.KyoceraPrintColor)
	// Copy function (uses scanner, outputs to print engine)
	copyBW := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyBW)
	copyColor := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyColor)
	// Fax function - prints are mono only
	faxBW := getOIDIntIndexed(idx, pdus, oids.KyoceraFaxBW)

	// Scanner unit usage counters
	copyScan := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyScans)
	faxScan := getOIDIntIndexed(idx, pdus, oids.KyoceraFaxScans)
	otherScan := getOIDIntIndexed(idx, pdus, oids.KyoceraOtherScans) // scan-to-email, scan-to-folder, etc.

	totalPrinted := getOIDIntIndexed(idx, pdus, oids.KyoceraTotalPrinted)

	// === PRINT IMPRESSIONS ===
	// Print engine output = all pages that went through the fuser
	// Includes: direct prints + copy outputs + fax prints (received faxes)

	// Store per-function print counters
	if printerBW > 0 {
		result["print_mono_pages"] = printerBW
	}
	if printerColor > 0 {
		result["print_color_pages"] = printerColor
	}
	if printerBW > 0 || printerColor > 0 {
		result["print_pages"] = printerBW + printerColor
	}

	// Copy function print output
	if copyBW > 0 || copyColor > 0 {
		result["copy_pages"] = copyBW + copyColor
		result["copy_mono_pages"] = copyBW
		result["copy_color_pages"] = copyColor
	}

	// Fax print output (received faxes printed)
	if faxBW > 0 {
		result["fax_pages"] = faxBW
		result["fax_mono_pages"] = faxBW
	}

	// Calculate TOTAL PRINT IMPRESSIONS (mono + color)
	// These are all the physical prints made by the print engine
	monoTotal := printerBW + copyBW + faxBW
	colorTotal := printerColor + copyColor

	if monoTotal > 0 {
		result["mono_pages"] = monoTotal
	}

	if colorTotal > 0 {
		result["color_pages"] = colorTotal
	}

	// Determine page_count (total print impressions) using best available source:
	// 1. Kyocera enterprise total (most accurate for Kyocera)
	// 2. Standard Printer-MIB PrtMarkerLifeCount (widely supported)
	// 3. Calculated from mono+color (last resort)
	var finalTotal int
	var totalSource string
	if totalPrinted > 0 {
		finalTotal = totalPrinted
		totalSource = "KyoceraTotalPrinted"
		result["page_count"] = totalPrinted
		result["total_pages"] = totalPrinted
	} else if pageCount := getOIDIntIndexed(idx, pdus, oids.PrtMarkerLifeCount+".1"); pageCount > 0 {
		finalTotal = pageCount
		totalSource = "PrtMarkerLifeCount"
		result["page_count"] = pageCount
		result["total_pages"] = pageCount
	} else if monoTotal > 0 || colorTotal > 0 {
		// Last resort: calculate from function counters
		finalTotal = monoTotal + colorTotal
		totalSource = "calculated"
		result["page_count"] = monoTotal + colorTotal
		result["total_pages"] = monoTotal + colorTotal
	}

	// Validate: Check if parts (mono + color) match total when all are available
	if finalTotal > 0 && (monoTotal > 0 || colorTotal > 0) {
		partsSum := monoTotal + colorTotal
		if partsSum != finalTotal && logger.Global != nil {
			// Log mismatch for investigation - this is expected when using KyoceraTotalPrinted
			// which may include additional job types not broken down
			logger.Global.Debug("Kyocera total/parts mismatch (normal for KyoceraTotalPrinted)",
				"total", finalTotal, "source", totalSource,
				"mono", monoTotal, "color", colorTotal, "parts_sum", partsSum,
				"diff", finalTotal-partsSum)
		}
	}

	// === SCAN IMPRESSIONS ===
	// Scanner unit usage = all pages digitized by the scanner
	// Includes: copy scans + fax scans (outgoing) + scan-to-host/email/folder
	// Note: copy_scans = scanner usage for copying (results in print impressions too)
	//       fax_scans = scanner usage for sending faxes (outgoing fax)
	//       other_scans = scan-to-email, scan-to-folder, scan-to-USB, etc.

	if copyScan > 0 {
		result["copy_scans"] = copyScan
	}

	if faxScan > 0 {
		result["fax_scans"] = faxScan
	}

	if otherScan > 0 {
		result["other_scans"] = otherScan
	}

	// Total scan impressions = all uses of the scanner unit
	totalScanImpressions := copyScan + faxScan + otherScan
	if totalScanImpressions > 0 {
		result["scan_count"] = totalScanImpressions
	}

	// Parse supply levels using generic parser
	supplies := parseSuppliesTable(pdus)
	for k, v := range supplies {
		result[k] = v
	}

	if logger.Global != nil {
		logger.Global.Debug("Kyocera parsing complete",
			"print_total", finalTotal, "mono_pages", monoTotal, "color_pages", colorTotal,
			"scan_count", totalScanImpressions)
	}
	return result
}
