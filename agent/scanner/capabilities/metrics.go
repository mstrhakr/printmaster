package capabilities

// MetricDefinition defines a metric and its capability requirements.
// This is the single source of truth for which metrics are relevant for which device types.
type MetricDefinition struct {
	Name        string   // Internal name (e.g., "color_pages")
	DisplayName string   // UI display name (e.g., "Color Pages")
	Category    string   // "pages", "supplies", "usage", "status"
	RequiresAll []string // ALL these capabilities must be present (AND logic)
	RequiresAny []string // At least ONE of these capabilities must be present (OR logic)
	ExcludesAny []string // NONE of these capabilities can be present (conflicts)
	Description string   // Tooltip/help text for UI
	VendorHint  string   // Empty = all vendors, or "HP", "Canon", etc.
}

// MetricRegistry holds all metric definitions organized by category.
var MetricRegistry = struct {
	PageCounters []MetricDefinition
	Supplies     []MetricDefinition
	Usage        []MetricDefinition
	Status       []MetricDefinition
}{
	PageCounters: []MetricDefinition{
		// === BASIC PAGE COUNTERS (Always present on printers) ===
		{
			Name:        "total_pages",
			DisplayName: "Total Pages",
			Category:    "pages",
			RequiresAll: []string{"printer"},
			Description: "Total pages printed by the device",
		},
		{
			Name:        "page_count",
			DisplayName: "Page Count",
			Category:    "pages",
			RequiresAll: []string{"printer"},
			Description: "Total impressions (standard counter)",
		},

		// === COLOR PAGE COUNTERS ===
		{
			Name:        "color_pages",
			DisplayName: "Color Pages",
			Category:    "pages",
			RequiresAll: []string{"printer", "color"},
			Description: "Total color pages printed",
		},
		{
			Name:        "mono_pages",
			DisplayName: "Mono Pages",
			Category:    "pages",
			RequiresAll: []string{"printer"},
			Description: "Total monochrome pages printed",
		},

		// === COPY COUNTERS ===
		{
			Name:        "copy_pages",
			DisplayName: "Copy Pages",
			Category:    "pages",
			RequiresAll: []string{"copier"},
			Description: "Total pages copied",
		},
		{
			Name:        "copy_color_pages",
			DisplayName: "Copy Color Pages",
			Category:    "pages",
			RequiresAll: []string{"copier", "color"},
			Description: "Total color pages copied",
		},
		{
			Name:        "copy_mono_pages",
			DisplayName: "Copy Mono Pages",
			Category:    "pages",
			RequiresAll: []string{"copier"},
			Description: "Total monochrome pages copied",
		},
		{
			Name:        "copy_flatbed_scans",
			DisplayName: "Copy Flatbed Scans",
			Category:    "pages",
			RequiresAll: []string{"copier"},
			Description: "Copy jobs from flatbed scanner",
		},
		{
			Name:        "copy_adf_scans",
			DisplayName: "Copy ADF Scans",
			Category:    "pages",
			RequiresAll: []string{"copier"},
			Description: "Copy jobs from automatic document feeder",
		},

		// === SCAN COUNTERS ===
		{
			Name:        "scan_count",
			DisplayName: "Scan Count",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Total scan operations",
		},
		{
			Name:        "scan_to_host_flatbed",
			DisplayName: "Flatbed Scans",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Scans from flatbed to computer",
		},
		{
			Name:        "scan_to_host_adf",
			DisplayName: "ADF Scans",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Scans from ADF to computer",
		},
		{
			Name:        "scan_adf_images",
			DisplayName: "ADF Images Scanned",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Total images scanned using the automatic document feeder",
		},
		{
			Name:        "scan_flatbed_images",
			DisplayName: "Flatbed Images Scanned",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Total images scanned using the flatbed",
		},
		{
			Name:        "scan_adf_to_host_images",
			DisplayName: "ADF Images to Host",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Images captured via ADF workflows that deliver directly to a host",
		},
		{
			Name:        "scan_flatbed_to_host_images",
			DisplayName: "Flatbed Images to Host",
			Category:    "pages",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Images captured via flatbed workflows that deliver directly to a host",
		},

		// === FAX COUNTERS ===
		{
			Name:        "fax_pages",
			DisplayName: "Fax Pages",
			Category:    "pages",
			RequiresAll: []string{"fax"},
			Description: "Total fax pages sent/received",
		},
		{
			Name:        "fax_adf_scans",
			DisplayName: "Fax ADF Scans",
			Category:    "pages",
			RequiresAll: []string{"fax"},
			Description: "Fax pages sent via ADF",
		},
		{
			Name:        "fax_flatbed_scans",
			DisplayName: "Fax Flatbed Scans",
			Category:    "pages",
			RequiresAll: []string{"fax"},
			Description: "Fax pages sent via flatbed",
		},
		{
			Name:        "fax_impressions",
			DisplayName: "Fax Impressions",
			Category:    "pages",
			RequiresAll: []string{"fax"},
			Description: "Total fax impressions processed (ADF + flatbed)",
		},

		// === DUPLEX COUNTERS ===
		{
			Name:        "duplex_sheets",
			DisplayName: "Duplex Sheets",
			Category:    "pages",
			RequiresAll: []string{"printer", "duplex"},
			Description: "Sheets printed on both sides",
		},

		// === CALCULATED/DERIVED COUNTERS ===
		{
			Name:        "other_pages",
			DisplayName: "Other Pages",
			Category:    "pages",
			RequiresAll: []string{"printer"},
			Description: "Pages not categorized as print/copy/fax",
		},
	},

	Supplies: []MetricDefinition{
		// === TONER/INK LEVELS (Standard) ===
		{
			Name:        "toner_black",
			DisplayName: "Black Toner",
			Category:    "supplies",
			RequiresAll: []string{"printer"},
			Description: "Black toner/ink level percentage",
		},
		{
			Name:        "toner_cyan",
			DisplayName: "Cyan Toner",
			Category:    "supplies",
			RequiresAll: []string{"printer", "color"},
			Description: "Cyan toner/ink level percentage",
		},
		{
			Name:        "toner_magenta",
			DisplayName: "Magenta Toner",
			Category:    "supplies",
			RequiresAll: []string{"printer", "color"},
			Description: "Magenta toner/ink level percentage",
		},
		{
			Name:        "toner_yellow",
			DisplayName: "Yellow Toner",
			Category:    "supplies",
			RequiresAll: []string{"printer", "color"},
			Description: "Yellow toner/ink level percentage",
		},

		// === OTHER SUPPLIES ===
		{
			Name:        "drum_life",
			DisplayName: "Drum/Imaging Unit",
			Category:    "supplies",
			RequiresAll: []string{"printer"},
			Description: "Drum or imaging unit remaining life",
		},
		{
			Name:        "waste_toner",
			DisplayName: "Waste Toner",
			Category:    "supplies",
			RequiresAll: []string{"printer"},
			Description: "Waste toner container level",
		},
		{
			Name:        "fuser_life",
			DisplayName: "Fuser Unit",
			Category:    "supplies",
			RequiresAll: []string{"printer"},
			Description: "Fuser unit remaining life",
		},
		{
			Name:        "transfer_belt",
			DisplayName: "Transfer Belt",
			Category:    "supplies",
			RequiresAll: []string{"printer", "color"},
			Description: "Transfer belt remaining life",
		},

		// Generic supply descriptions (for unknown supplies)
		{
			Name:        "toner_levels",
			DisplayName: "Toner Levels",
			Category:    "supplies",
			RequiresAll: []string{"printer"},
			Description: "Generic toner/ink levels",
		},
	},

	Usage: []MetricDefinition{
		// === JAM COUNTERS ===
		{
			Name:        "jam_events",
			DisplayName: "Paper Jams",
			Category:    "usage",
			RequiresAll: []string{"printer"},
			Description: "Total paper jam events",
		},
		{
			Name:        "jam_events_total",
			DisplayName: "Paper Jams (Total)",
			Category:    "usage",
			RequiresAll: []string{"printer"},
			Description: "Device-reported aggregate jam counter when available",
		},
		{
			Name:        "scanner_jam_events",
			DisplayName: "Scanner Jams",
			Category:    "usage",
			RequiresAny: []string{"scanner", "copier"},
			Description: "Scanner/ADF jam events",
		},

		// === SERIAL NUMBER (Always present) ===
		{
			Name:        "serial",
			DisplayName: "Serial Number",
			Category:    "usage",
			RequiresAll: []string{"printer"},
			Description: "Device serial number",
		},
	},

	Status: []MetricDefinition{
		// === DEVICE STATUS ===
		{
			Name:        "status",
			DisplayName: "Status",
			Category:    "status",
			RequiresAll: []string{"printer"},
			Description: "Current device status",
		},
		{
			Name:        "device_state",
			DisplayName: "Device State",
			Category:    "status",
			RequiresAll: []string{"printer"},
			Description: "Operational state of the device",
		},
	},
}

// GetRelevantMetrics returns only the metrics relevant for the given device capabilities.
// This is used by both the metrics parser and UI to filter out irrelevant metrics.
func GetRelevantMetrics(caps *DeviceCapabilities) []MetricDefinition {
	if caps == nil {
		// If no capabilities, return all metrics (safe default)
		return getAllMetrics()
	}

	var relevant []MetricDefinition

	allMetrics := getAllMetrics()
	for _, metric := range allMetrics {
		if isMetricRelevant(metric, caps) {
			relevant = append(relevant, metric)
		}
	}

	return relevant
}

// GetRelevantMetricNames returns just the names of relevant metrics.
// Useful for quick lookups in parsers.
func GetRelevantMetricNames(caps *DeviceCapabilities) []string {
	metrics := GetRelevantMetrics(caps)
	names := make([]string, len(metrics))
	for i, m := range metrics {
		names[i] = m.Name
	}
	return names
}

// GetRelevantMetricsByCategory returns relevant metrics organized by category.
// Useful for UI rendering.
func GetRelevantMetricsByCategory(caps *DeviceCapabilities) map[string][]MetricDefinition {
	metrics := GetRelevantMetrics(caps)
	byCategory := make(map[string][]MetricDefinition)

	for _, metric := range metrics {
		byCategory[metric.Category] = append(byCategory[metric.Category], metric)
	}

	return byCategory
}

// IsMetricRelevant checks if a specific metric is relevant for the device.
func IsMetricRelevant(metricName string, caps *DeviceCapabilities) bool {
	allMetrics := getAllMetrics()
	for _, metric := range allMetrics {
		if metric.Name == metricName {
			return isMetricRelevant(metric, caps)
		}
	}
	return false // Unknown metric - assume not relevant
}

// getAllMetrics returns all registered metrics from all categories.
func getAllMetrics() []MetricDefinition {
	all := make([]MetricDefinition, 0)
	all = append(all, MetricRegistry.PageCounters...)
	all = append(all, MetricRegistry.Supplies...)
	all = append(all, MetricRegistry.Usage...)
	all = append(all, MetricRegistry.Status...)
	return all
}

// isMetricRelevant checks if a metric meets the capability requirements.
func isMetricRelevant(metric MetricDefinition, caps *DeviceCapabilities) bool {
	// Build capability map for easy lookup
	capMap := map[string]bool{
		"printer": caps.IsPrinter,
		"color":   caps.IsColor,
		"mono":    caps.IsMono,
		"copier":  caps.IsCopier,
		"scanner": caps.IsScanner,
		"fax":     caps.IsFax,
		"duplex":  caps.HasDuplex,
	}

	// Check ExcludesAny (conflicts) - if any excluded capability is present, hide metric
	for _, excluded := range metric.ExcludesAny {
		if capMap[excluded] {
			return false
		}
	}

	// Check RequiresAll - ALL must be present
	if len(metric.RequiresAll) > 0 {
		for _, required := range metric.RequiresAll {
			if !capMap[required] {
				return false
			}
		}
	}

	// Check RequiresAny - at least ONE must be present
	if len(metric.RequiresAny) > 0 {
		anyPresent := false
		for _, required := range metric.RequiresAny {
			if capMap[required] {
				anyPresent = true
				break
			}
		}
		if !anyPresent {
			return false
		}
	}

	return true
}
