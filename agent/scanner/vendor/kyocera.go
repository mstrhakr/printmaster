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
	printerBW := getOIDIntIndexed(idx, pdus, oids.KyoceraPrintBW)
	printerColor := getOIDIntIndexed(idx, pdus, oids.KyoceraPrintColor)
	copyBW := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyBW)
	copyColor := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyColor)
	faxBW := getOIDIntIndexed(idx, pdus, oids.KyoceraFaxBW)

	copyScan := getOIDIntIndexed(idx, pdus, oids.KyoceraCopyScans)
	faxScan := getOIDIntIndexed(idx, pdus, oids.KyoceraFaxScans)
	otherScan := getOIDIntIndexed(idx, pdus, oids.KyoceraOtherScans)

	totalPrinted := getOIDIntIndexed(idx, pdus, oids.KyoceraTotalPrinted)

	// Print function counters
	if printerBW > 0 {
		result["print_mono_pages"] = printerBW
	}
	if printerColor > 0 {
		result["print_color_pages"] = printerColor
	}
	if printerBW > 0 || printerColor > 0 {
		result["print_pages"] = printerBW + printerColor
	}

	// Calculate totals
	monoTotal := printerBW + copyBW + faxBW
	colorTotal := printerColor + copyColor

	if monoTotal > 0 {
		result["mono_pages"] = monoTotal
	}

	if colorTotal > 0 {
		result["color_pages"] = colorTotal
	}

	// Determine page_count using best available source:
	// 1. Kyocera enterprise total (most accurate for Kyocera)
	// 2. Standard Printer-MIB PrtMarkerLifeCount (widely supported)
	// 3. Calculated from mono+color (last resort)
	if totalPrinted > 0 {
		result["page_count"] = totalPrinted
		result["total_pages"] = totalPrinted
	} else if pageCount := getOIDIntIndexed(idx, pdus, oids.PrtMarkerLifeCount+".1"); pageCount > 0 {
		result["page_count"] = pageCount
		result["total_pages"] = pageCount
	} else if monoTotal > 0 || colorTotal > 0 {
		// Last resort: calculate from function counters
		result["page_count"] = monoTotal + colorTotal
		result["total_pages"] = monoTotal + colorTotal
	}

	// Copy function counters
	if copyBW > 0 || copyColor > 0 {
		result["copy_pages"] = copyBW + copyColor
		result["copy_mono_pages"] = copyBW
		result["copy_color_pages"] = copyColor
	}

	// Fax counters
	if faxBW > 0 {
		result["fax_pages"] = faxBW
		result["fax_mono_pages"] = faxBW
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

	// Note: PrtMarkerLifeCount is now checked earlier in the priority chain

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
