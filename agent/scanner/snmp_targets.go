package scanner

import "strings"

// VendorIDTarget describes a vendor-specific IEEE-1284 style device ID OID.
type VendorIDTarget struct {
	Key         string   // Stable identifier for learned-oid maps
	OID         string   // SNMP OID to query
	Description string   // Human-friendly description of what the OID stores
	Vendors     []string // Vendor names associated with this OID
}

// VendorIDTargets enumerates vendor-specific device ID OIDs we probe opportunistically.
var VendorIDTargets = []VendorIDTarget{
	{
		Key:         "hp_ieee1284",
		OID:         "1.3.6.1.4.1.11.2.3.9.1.1.7.0",
		Description: "HP IEEE-1284 device ID (MFG/MDL/CMD payload)",
		Vendors:     []string{"hp", "hewlett-packard"},
	},
	{
		Key:         "lexmark_device_id",
		OID:         "1.3.6.1.4.1.641.2.1.2.1.3.1",
		Description: "Lexmark device ID / IEEE-1284 descriptor",
		Vendors:     []string{"lexmark"},
	},
	{
		Key:         "ricoh_device_id",
		OID:         "1.3.6.1.4.1.367.3.2.1.1.1.11.0",
		Description: "Ricoh device ID payload (similar to IEEE-1284)",
		Vendors:     []string{"ricoh", "lanier", "savin"},
	},
	{
		Key:         "xerox_device_id",
		OID:         "1.3.6.1.4.1.128.2.1.3.1.2.0",
		Description: "Xerox device ID descriptor",
		Vendors:     []string{"xerox"},
	},
}

var vendorIDTargetIndex map[string]VendorIDTarget

func init() {
	vendorIDTargetIndex = make(map[string]VendorIDTarget, len(VendorIDTargets))
	for _, target := range VendorIDTargets {
		normalized := strings.TrimPrefix(target.OID, ".")
		vendorIDTargetIndex[normalized] = target
	}
}

// VendorIDTargetOIDs returns the list of OIDs we use for vendor-specific device IDs.
func VendorIDTargetOIDs() []string {
	oids := make([]string, 0, len(VendorIDTargets))
	for _, target := range VendorIDTargets {
		oids = append(oids, target.OID)
	}
	return oids
}

// LookupVendorIDTarget returns the metadata for a vendor-specific device ID OID.
func LookupVendorIDTarget(oid string) (VendorIDTarget, bool) {
	normalized := strings.TrimPrefix(oid, ".")
	target, ok := vendorIDTargetIndex[normalized]
	return target, ok
}
