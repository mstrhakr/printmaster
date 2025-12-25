package vendor

import (
	"fmt"
	"strconv"
	"strings"

	"printmaster/agent/scanner/capabilities"
	"printmaster/agent/supplies"
	"printmaster/common/logger"
	"printmaster/common/snmp/oids"

	"github.com/gosnmp/gosnmp"
)

type pduIndex struct {
	byOID map[string]gosnmp.SnmpPDU
}

func newPDUIndex(pdus []gosnmp.SnmpPDU) *pduIndex {
	idx := &pduIndex{byOID: make(map[string]gosnmp.SnmpPDU, len(pdus))}
	for _, pdu := range pdus {
		idx.byOID[normalizeOID(pdu.Name)] = pdu
	}
	return idx
}

// GenericVendor is the fallback module for devices without vendor-specific support.
// Uses standard Printer-MIB (RFC 3805) OIDs only.
type GenericVendor struct{}

func init() {
	// Register generic module as fallback
	SetGenericModule(&GenericVendor{})
}

func (v *GenericVendor) Name() string {
	return "Generic"
}

func (v *GenericVendor) Detect(sysObjectID, sysDescr, model string) bool {
	// Generic module is always a valid fallback
	return true
}

func (v *GenericVendor) BaseOIDs() []string {
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
		// PWG Port Monitor IEEE 1284 Device ID (contains capability info)
		oids.PpmPrinterIEEE1284DeviceID + ".1",
	}
}

func (v *GenericVendor) MetricOIDs(caps *capabilities.DeviceCapabilities) []string {
	return []string{
		oids.PrtMarkerLifeCount + ".1", // prtMarkerLifeCount (instance .1)
	}
}

func (v *GenericVendor) SupplyOIDs() []string {
	// Return OID roots for SNMP walk of supply tables
	return []string{
		oids.PrtMarkerSuppliesDesc,
		oids.PrtMarkerSuppliesLevel,
		oids.PrtMarkerSuppliesMaxCap,
		oids.PrtMarkerSuppliesClass,
		oids.PrtMarkerSuppliesType,
	}
}

func (v *GenericVendor) Parse(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Parsing Generic vendor PDUs", "pdu_count", len(pdus))
	}

	idx := newPDUIndex(pdus)

	// Parse basic page count
	if pageCount := getOIDIntIndexed(idx, pdus, oids.PrtMarkerLifeCount+".1"); pageCount > 0 {
		result["total_pages"] = pageCount
		result["page_count"] = pageCount
	}

	// Parse supply levels from prtMarkerSuppliesTable
	supplies := parseSuppliesTable(pdus)
	if logger.Global != nil {
		logger.Global.Debug("Generic supply table parsed", "supplies_count", len(supplies))
	}
	for k, v := range supplies {
		result[k] = v
	}

	return result
}

func getOIDIntIndexed(idx *pduIndex, pdus []gosnmp.SnmpPDU, oid string) int {
	oid = normalizeOID(oid)
	if idx != nil {
		if pdu, ok := idx.byOID[oid]; ok {
			return coerceToInt(pdu.Value)
		}
	}
	return getOIDInt(pdus, oid)
}

// parseSuppliesTable walks prtMarkerSuppliesTable and extracts toner/ink levels.
// Maps supply descriptions to standardized names (toner_black, toner_cyan, etc.).
func parseSuppliesTable(pdus []gosnmp.SnmpPDU) map[string]interface{} {
	result := make(map[string]interface{})

	// Group PDUs by instance suffix (e.g., ".1.1.6.1.1" → instance "1")
	type SupplyEntry struct {
		Description string
		Level       int
		MaxCapacity int
		Class       int
		Type        int
	}

	entries := make(map[string]*SupplyEntry)

	for _, pdu := range pdus {
		oid := normalizeOID(pdu.Name)

		// Extract instance from OID (last component after table root)
		var instance string
		var field string

		if strings.HasPrefix(oid, oids.PrtMarkerSuppliesDesc+".1.") {
			// Description
			instance = strings.TrimPrefix(oid, "1.3.6.1.2.1.43.11.1.1.6.1.")
			field = "description"
		} else if strings.HasPrefix(oid, oids.PrtMarkerSuppliesLevel+".1.") {
			// Level
			instance = strings.TrimPrefix(oid, "1.3.6.1.2.1.43.11.1.1.9.1.")
			field = "level"
		} else if strings.HasPrefix(oid, oids.PrtMarkerSuppliesMaxCap+".1.") {
			// MaxCapacity
			instance = strings.TrimPrefix(oid, "1.3.6.1.2.1.43.11.1.1.8.1.")
			field = "max_capacity"
		} else if strings.HasPrefix(oid, oids.PrtMarkerSuppliesClass+".1.") {
			// Class
			instance = strings.TrimPrefix(oid, "1.3.6.1.2.1.43.11.1.1.4.1.")
			field = "class"
		} else if strings.HasPrefix(oid, oids.PrtMarkerSuppliesType+".1.") {
			// Type
			instance = strings.TrimPrefix(oid, "1.3.6.1.2.1.43.11.1.1.5.1.")
			field = "type"
		} else {
			continue
		}

		if instance == "" {
			continue
		}

		// Initialize entry if not exists
		if entries[instance] == nil {
			entries[instance] = &SupplyEntry{
				Level:       -1,
				MaxCapacity: -1,
			}
		}
		entry := entries[instance]

		// Populate fields
		switch field {
		case "description":
			if bytes, ok := pdu.Value.([]byte); ok {
				entry.Description = string(bytes)
			} else if str, ok := pdu.Value.(string); ok {
				entry.Description = str
			}
		case "level":
			entry.Level = coerceToInt(pdu.Value)
		case "max_capacity":
			entry.MaxCapacity = coerceToInt(pdu.Value)
		case "class":
			entry.Class = coerceToInt(pdu.Value)
		case "type":
			entry.Type = coerceToInt(pdu.Value)
		}
	}

	// Map supplies to standardized names
	processed := 0
	for _, entry := range entries {
		if entry.Description == "" {
			continue
		}

		// Class 3 = supplyThatIsConsumed (toner, ink, etc.)
		// Class 4 = receptacleThatIsFilled (waste toner containers)
		// Skip other classes
		if entry.Class != 3 && entry.Class != 4 {
			continue
		}

		// Normalize description for matching
		// Use original description for part number matching, lowercase for word matching
		desc := entry.Description

		// Calculate percentage if we have both level and capacity
		// Per RFC 3805 (Printer-MIB):
		//   MaxCapacity = -1: other/unknown
		//   MaxCapacity = -2: unknown (manufacturer-defined)
		//   MaxCapacity = -3: someRemaining (level is not quantifiable)
		//   Level = -1: other/unknown
		//   Level = -2: unknown
		//   Level = -3: someRemaining (low but usable)
		var percentage float64
		if entry.MaxCapacity > 0 && entry.Level >= 0 {
			// Normal case: calculate percentage from level/capacity
			percentage = (float64(entry.Level) / float64(entry.MaxCapacity)) * 100.0
		} else if entry.Level >= 0 && entry.Level <= 100 && entry.MaxCapacity <= 0 {
			// MaxCapacity is unknown/special but Level is valid 0-100 range
			// Treat level as percentage directly (common for many printers)
			percentage = float64(entry.Level)
		} else if entry.Level == -3 {
			// someRemaining: report as low (10%) as a reasonable estimate
			percentage = 10.0
		} else {
			// Unknown level - cannot determine
			percentage = -1
		}

		// Match description to canonical metric key
		metricName := supplies.NormalizeDescription(desc)

		// For Class 4 (waste containers), force to waste_toner if not already mapped
		if entry.Class == 4 && metricName == "" {
			metricName = "waste_toner"
		}

		if metricName != "" {
			// Deduplication: if key already exists, prefer the value that's not 100%
			// (100% often indicates a "new toner" placeholder entry from some vendors)
			if existing, ok := result[metricName]; ok {
				existingVal, _ := existing.(float64)
				// Keep the existing value if:
				// - new value is exactly 100 (likely placeholder) and existing isn't
				// - new value is invalid (-1)
				if percentage == 100.0 && existingVal != 100.0 && existingVal >= 0 {
					// Skip this duplicate - keep existing more realistic value
					if logger.Global != nil {
						logger.Global.Debug("Supply dedup: keeping existing value",
							"key", metricName, "existing", existingVal, "skipped", percentage)
					}
					processed++
					continue
				}
				// If existing is 100 and new isn't, we'll overwrite (fall through)
				if existingVal == 100.0 && percentage != 100.0 && percentage >= 0 {
					if logger.Global != nil {
						logger.Global.Debug("Supply dedup: replacing placeholder",
							"key", metricName, "old", existingVal, "new", percentage)
					}
				}
			}
			result[metricName] = percentage
		}

		// Store raw description for unknown supplies
		if metricName == "" && percentage >= 0 {
			// Store with sanitized description as key
			sanitized := strings.ToLower(entry.Description)
			sanitized = strings.ReplaceAll(sanitized, " ", "_")
			result[fmt.Sprintf("supply_%s", sanitized)] = percentage
		}
		processed++
	}
	if logger.Global != nil {
		logger.Global.TraceTag("vendor_parse", "Supply entries processed", "count", processed)
	}

	return result
}
func getOIDInt(pdus []gosnmp.SnmpPDU, oid string) int {
	oid = normalizeOID(oid)
	for _, pdu := range pdus {
		if normalizeOID(pdu.Name) == oid || strings.HasPrefix(normalizeOID(pdu.Name), oid+".") {
			return coerceToInt(pdu.Value)
		}
	}
	return 0
}

func normalizeOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

func coerceToInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return -1
}
