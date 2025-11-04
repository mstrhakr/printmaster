package agent

// Mapping of known enterprise OID numeric identifiers and recommended vendor
// MIB roots to probe when that vendor is detected. This is a small, safe
// conservative mapping; we can expand it as we see more devices in the logs.

var enterpriseToManufacturer = map[string]string{
	"11":   "HP",
	"2435": "Brother",
	"1602": "Canon",
	"641":  "Lexmark",
	"231":  "Epson",
	"9":    "Dell",
}

var vendorProbeRoots = map[string][]string{
	"HP":      {"1.3.6.1.4.1.11"},
	"Brother": {"1.3.6.1.4.1.2435"},
	"Canon":   {"1.3.6.1.4.1.1602"},
	"Lexmark": {"1.3.6.1.4.1.641"},
	"Epson":   {"1.3.6.1.4.1.231"},
	"Dell":    {"1.3.6.1.4.1.9"},
}

// VendorRootsFor returns a list of additional MIB roots we should probe for
// the given manufacturer name. The caller should provide a normalized
// manufacturer string (e.g., "HP", "Canon"). If no roots are known, an
// empty slice is returned.
func VendorRootsFor(manufacturer string) []string {
	if manufacturer == "" {
		return nil
	}
	if roots, ok := vendorProbeRoots[manufacturer]; ok {
		return roots
	}
	return nil
}

// ManufacturerForEnterprise returns a best-effort manufacturer name for a
// numeric enterprise id string (e.g., "11" -> "HP"). Returns empty string
// if unknown.
func ManufacturerForEnterprise(ent string) string {
	if m, ok := enterpriseToManufacturer[ent]; ok {
		return m
	}
	return ""
}
