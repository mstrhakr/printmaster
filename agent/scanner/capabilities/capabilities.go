package capabilities

import (
	"strings"

	"github.com/gosnmp/gosnmp"
)

// CapabilityDetector is the interface that all capability detectors must implement.
// Each detector analyzes SNMP data and returns a confidence score (0.0-1.0).
type CapabilityDetector interface {
	// Name returns the capability name (e.g., "color", "copier", "duplex")
	Name() string

	// Detect analyzes the provided evidence and returns a confidence score (0.0-1.0)
	Detect(evidence *DetectionEvidence) float64

	// Threshold returns the minimum confidence score to consider capability present
	// Default is 0.7, but some capabilities may use different thresholds
	Threshold() float64
}

// DetectionEvidence contains all available data for capability detection
type DetectionEvidence struct {
	// SNMP data
	PDUs     []gosnmp.SnmpPDU
	PDUByOID map[string]gosnmp.SnmpPDU
	SysDescr string
	SysOID   string

	// Device info
	Vendor   string
	Model    string
	Serial   string
	Hostname string

	// Network info
	OpenPorts []int

	// Already detected capabilities (for cross-referencing)
	Capabilities map[string]float64
}

// DeviceCapabilities holds all detected capabilities with confidence scores
type DeviceCapabilities struct {
	// Raw confidence scores (0.0-1.0)
	Scores map[string]float64 `json:"scores"`

	// Derived booleans (confidence >= threshold)
	IsPrinter bool `json:"is_printer"`
	IsColor   bool `json:"is_color"`
	IsMono    bool `json:"is_mono"`
	IsCopier  bool `json:"is_copier"`
	IsScanner bool `json:"is_scanner"`
	IsFax     bool `json:"is_fax"`
	HasDuplex bool `json:"has_duplex"`
	IsLaser   bool `json:"is_laser"`
	IsInkjet  bool `json:"is_inkjet"`

	// Form factor classification
	FormFactor string `json:"form_factor,omitempty"` // "Desktop", "Wide Format", "Label Printer", etc.

	// Classification
	DeviceType string `json:"device_type"` // "Color MFP", "Mono Printer", etc.
}

// CapabilityRegistry manages all registered capability detectors
type CapabilityRegistry struct {
	detectors []CapabilityDetector
}

// NewCapabilityRegistry creates a new registry with default detectors
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{
		detectors: []CapabilityDetector{
			&PrinterDetector{},
			&ColorDetector{},
			&MonoDetector{},
			&CopierDetector{},
			&ScannerDetector{},
			&FaxDetector{},
			&DuplexDetector{},
			&LaserDetector{},
			&InkjetDetector{},
		},
	}
}

// Register adds a custom capability detector
func (r *CapabilityRegistry) Register(detector CapabilityDetector) {
	r.detectors = append(r.detectors, detector)
}

// DetectAll runs all registered detectors and returns capabilities
func (r *CapabilityRegistry) DetectAll(evidence *DetectionEvidence) DeviceCapabilities {
	caps := DeviceCapabilities{
		Scores: make(map[string]float64),
	}

	ensurePDUIndex(evidence)

	// Initialize capabilities map in evidence for cross-referencing
	if evidence.Capabilities == nil {
		evidence.Capabilities = make(map[string]float64)
	}

	// Run all detectors
	for _, detector := range r.detectors {
		score := detector.Detect(evidence)
		name := detector.Name()
		caps.Scores[name] = score
		evidence.Capabilities[name] = score

		// Set boolean based on threshold
		if score >= detector.Threshold() {
			switch name {
			case "printer":
				caps.IsPrinter = true
			case "color":
				caps.IsColor = true
			case "mono":
				caps.IsMono = true
			case "copier":
				caps.IsCopier = true
			case "scanner":
				caps.IsScanner = true
			case "fax":
				caps.IsFax = true
			case "duplex":
				caps.HasDuplex = true
			case "laser":
				caps.IsLaser = true
			case "inkjet":
				caps.IsInkjet = true
			}
		}
	}

	// Classify form factor (Desktop, Wide Format, Label Printer, etc.)
	caps.FormFactor = ClassifyFormFactor(evidence)

	// Classify device type (includes form factor in classification)
	caps.DeviceType = classifyDeviceType(caps)

	return caps
}

func ensurePDUIndex(evidence *DetectionEvidence) {
	if evidence == nil {
		return
	}
	if evidence.PDUByOID != nil {
		return
	}
	if len(evidence.PDUs) == 0 {
		evidence.PDUByOID = map[string]gosnmp.SnmpPDU{}
		return
	}

	idx := make(map[string]gosnmp.SnmpPDU, len(evidence.PDUs))
	for _, pdu := range evidence.PDUs {
		idx[normalizeOID(pdu.Name)] = pdu
	}
	evidence.PDUByOID = idx
}

// classifyDeviceType determines device type based on capabilities and form factor
func classifyDeviceType(caps DeviceCapabilities) string {
	// Form factor takes precedence for specialized device types
	switch caps.FormFactor {
	case FormFactorWideFormat:
		if caps.IsColor {
			return "Color Wide Format"
		}
		return "Wide Format"

	case FormFactorLabelPrint:
		if caps.IsColor {
			return "Color Label Printer"
		}
		return "Label Printer"

	case FormFactorProduction:
		if caps.IsColor {
			return "Color Production"
		}
		return "Production"

	case FormFactorFloorCopier:
		// Floor copiers are typically MFPs
		if caps.IsColor {
			return "Color MFP"
		}
		return "Mono MFP"
	}

	// Standalone scanner (rare)
	if caps.IsScanner && !caps.IsPrinter {
		return "Scanner"
	}

	// MFP (multifunction: print + copy/scan)
	if caps.IsPrinter && (caps.IsCopier || caps.IsScanner) {
		if caps.IsColor {
			return "Color MFP"
		}
		return "Mono MFP"
	}

	// Copier/Printer combo (no scan)
	if caps.IsPrinter && caps.IsCopier && !caps.IsScanner {
		return "Copier/Printer"
	}

	// Simple printer
	if caps.IsPrinter {
		if caps.IsColor {
			return "Color Printer"
		}
		return "Mono Printer"
	}

	return "Unknown"
}

// Helper functions for detectors

// HasOID checks if a specific OID exists in PDUs
func HasOID(pdus []gosnmp.SnmpPDU, oid string) bool {
	oid = normalizeOID(oid)
	for _, pdu := range pdus {
		if normalizeOID(pdu.Name) == oid {
			return true
		}
	}
	return false
}

// HasOIDIn checks if a specific OID exists, using the evidence index when available.
func HasOIDIn(evidence *DetectionEvidence, oid string) bool {
	if evidence == nil {
		return false
	}
	ensurePDUIndex(evidence)
	oid = normalizeOID(oid)
	_, ok := evidence.PDUByOID[oid]
	return ok
}

// HasAnyOID checks if any of the specified OIDs exist
func HasAnyOID(pdus []gosnmp.SnmpPDU, oids []string) bool {
	for _, oid := range oids {
		if HasOID(pdus, oid) {
			return true
		}
	}
	return false
}

// HasAnyOIDIn checks if any of the specified OIDs exist, using the evidence index when available.
func HasAnyOIDIn(evidence *DetectionEvidence, oids []string) bool {
	for _, oid := range oids {
		if HasOIDIn(evidence, oid) {
			return true
		}
	}
	return false
}

// GetOIDValue retrieves the integer value of an OID
func GetOIDValue(pdus []gosnmp.SnmpPDU, oid string) int64 {
	oid = normalizeOID(oid)
	for _, pdu := range pdus {
		if normalizeOID(pdu.Name) == oid {
			return pduValueToInt64(pdu.Value)
		}
	}
	return 0
}

// GetOIDValueIn retrieves the integer value of an OID, using the evidence index when available.
func GetOIDValueIn(evidence *DetectionEvidence, oid string) int64 {
	if evidence == nil {
		return 0
	}
	ensurePDUIndex(evidence)
	oid = normalizeOID(oid)
	if pdu, ok := evidence.PDUByOID[oid]; ok {
		return pduValueToInt64(pdu.Value)
	}
	return 0
}

// GetOIDString retrieves the string value of an OID
func GetOIDString(pdus []gosnmp.SnmpPDU, oid string) string {
	oid = normalizeOID(oid)
	for _, pdu := range pdus {
		if normalizeOID(pdu.Name) == oid {
			if bytes, ok := pdu.Value.([]byte); ok {
				return string(bytes)
			}
			return ""
		}
	}
	return ""
}

// GetOIDStringIn retrieves the string value of an OID, using the evidence index when available.
func GetOIDStringIn(evidence *DetectionEvidence, oid string) string {
	if evidence == nil {
		return ""
	}
	ensurePDUIndex(evidence)
	oid = normalizeOID(oid)
	if pdu, ok := evidence.PDUByOID[oid]; ok {
		if bytes, ok := pdu.Value.([]byte); ok {
			return string(bytes)
		}
		if s, ok := pdu.Value.(string); ok {
			return s
		}
		return ""
	}
	return ""
}

// ContainsAny checks if string contains any of the substrings (case-insensitive)
func ContainsAny(s string, substrings []string) bool {
	s = strings.ToLower(s)
	for _, sub := range substrings {
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// ContainsAll checks if string contains all of the substrings (case-insensitive)
func ContainsAll(s string, substrings []string) bool {
	s = strings.ToLower(s)
	for _, sub := range substrings {
		if !strings.Contains(s, strings.ToLower(sub)) {
			return false
		}
	}
	return true
}

// normalizeOID removes leading dot if present
func normalizeOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

// pduValueToInt64 converts PDU value to int64
func pduValueToInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint64:
		return int64(v)
	case []byte:
		// Try to parse as string
		return 0
	default:
		return 0
	}
}

// Min returns the minimum of two float64 values
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Max returns the maximum of two float64 values
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
