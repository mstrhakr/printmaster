package scanner

import (
	"context"
	"fmt"

	"printmaster/agent/scanner/capabilities"

	"github.com/gosnmp/gosnmp"
)

// QueryProfile defines what OIDs to query during SNMP operations.
// Different profiles optimize for different use cases (speed vs. completeness).
type QueryProfile int

const (
	// QueryMinimal queries only serial number OIDs.
	// Fastest option - use for quick serial lookup.
	// Replaces: QuickGetSerial function
	QueryMinimal QueryProfile = iota

	// QueryEssential queries serial + toner + pages + status.
	// Fast targeted queries for known devices.
	// Replaces: QuickRefreshDevice function
	QueryEssential

	// QueryFull performs a full diagnostic walk of all MIB trees.
	// Comprehensive - use for new device discovery and detailed diagnostics.
	// Replaces: RefreshDevice function
	QueryFull

	// QueryMetrics queries vendor-specific metrics OIDs.
	// Optimized for scheduled metrics collection (page counts, toner, scans, jams).
	// NEW - specifically for periodic metrics snapshots
	QueryMetrics
)

// String returns the string representation of QueryProfile.
func (q QueryProfile) String() string {
	switch q {
	case QueryMinimal:
		return "QueryMinimal"
	case QueryEssential:
		return "QueryEssential"
	case QueryFull:
		return "QueryFull"
	case QueryMetrics:
		return "QueryMetrics"
	default:
		return "Unknown"
	}
}

// QueryDevice performs SNMP queries based on the specified profile.
// This is the unified replacement for QuickGetSerial, QuickRefreshDevice, and RefreshDevice.
//
// Parameters:
//   - ip: Target device IP address
//   - profile: QueryProfile determining which OIDs to query
//   - vendorHint: Vendor name ("HP", "Canon", etc.) for vendor-specific OIDs. Empty string uses generic.
//   - timeout: SNMP timeout in seconds
//
// Returns:
//   - *QueryResult: Raw SNMP data (PDUs) to be parsed by caller
//   - error: Any errors encountered during query
//
// Example usage:
//
//	// Quick serial lookup
//	result, err := QueryDevice(ctx, "10.0.0.1", QueryMinimal, "", 5)
//
//	// Fast refresh with HP-specific OIDs
//	result, err := QueryDevice(ctx, "10.0.0.1", QueryEssential, "HP", 10)
//
//	// Full diagnostic walk
//	result, err := QueryDevice(ctx, "10.0.0.1", QueryFull, "HP", 15)
//
//	// Metrics collection
//	result, err := QueryDevice(ctx, "10.0.0.1", QueryMetrics, "HP", 5)
//
// QueryResult holds the raw SNMP data returned by QueryDevice.
// The caller must parse these PDUs using agent.ParsePDUs or vendor.ExtractMetrics.
type QueryResult struct {
	IP           string
	PDUs         []gosnmp.SnmpPDU
	Profile      QueryProfile
	VendorHint   string
	Capabilities *capabilities.DeviceCapabilities // Populated during QueryFull
}

func QueryDevice(ctx context.Context, ip string, profile QueryProfile, vendorHint string, timeoutSeconds int) (*QueryResult, error) {
	return QueryDeviceWithCapabilities(ctx, ip, profile, vendorHint, timeoutSeconds, nil)
}

// QueryDeviceWithCapabilities performs SNMP queries with optional capability-aware OID optimization.
// When capabilities are provided and profile is QueryMetrics, only relevant OIDs are queried.
func QueryDeviceWithCapabilities(ctx context.Context, ip string, profile QueryProfile, vendorHint string, timeoutSeconds int, caps *capabilities.DeviceCapabilities) (*QueryResult, error) {
	return queryDeviceWithCapabilitiesAndClient(ctx, ip, profile, vendorHint, timeoutSeconds, caps, NewSNMPClient)
}

// queryDeviceWithCapabilitiesAndClient is the internal implementation that accepts a client factory.
// This allows tests to inject mock clients without modifying global state.
func queryDeviceWithCapabilitiesAndClient(ctx context.Context, ip string, profile QueryProfile, vendorHint string, timeoutSeconds int, caps *capabilities.DeviceCapabilities, clientFactory func(*SNMPConfig, string, int) (SNMPClient, error)) (*QueryResult, error) {
	if ip == "" {
		return nil, fmt.Errorf("ip address required")
	}

	// Check if context is already cancelled before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// 1. Get SNMP config
	cfg, err := GetSNMPConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get SNMP config: %w", err)
	}

	// 2. Setup SNMP client
	client, err := clientFactory(cfg, ip, timeoutSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to create SNMP client: %w", err)
	}
	defer client.Close()

	// 4. Build OID list based on profile + capabilities
	oids := buildQueryOIDsWithCapabilities(profile, caps)

	// 5. Query SNMP (Walk for Full, Get for others)
	var pdus []gosnmp.SnmpPDU

	if profile == QueryFull {
		// Full diagnostic walk of standard MIB roots
		roots := []string{
			"1.3.6.1.2.1",    // System MIB
			"1.3.6.1.2.1.43", // Printer-MIB
			"1.3.6.1.4.1",    // Enterprise OIDs
		}

		for _, root := range roots {
			// Check context before each walk
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			err := client.Walk(root, func(pdu gosnmp.SnmpPDU) error {
				// Check context during walk
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				pdus = append(pdus, pdu)
				// Limit to prevent runaway walks
				if len(pdus) >= 10000 {
					return fmt.Errorf("walk limit exceeded")
				}
				return nil
			})
			if err != nil && len(pdus) == 0 {
				return nil, fmt.Errorf("SNMP walk failed: %w", err)
			}
		}
	} else {
		// Targeted GET for specific OIDs
		if len(oids) == 0 {
			return nil, fmt.Errorf("no OIDs to query for profile %s", profile)
		}

		result, err := client.Get(oids)
		if err != nil {
			return nil, fmt.Errorf("SNMP GET failed: %w", err)
		}

		if result != nil {
			pdus = result.Variables
		}
	}

	if len(pdus) == 0 {
		return nil, fmt.Errorf("no SNMP data received from %s", ip)
	}

	result := &QueryResult{
		IP:         ip,
		PDUs:       pdus,
		Profile:    profile,
		VendorHint: vendorHint,
	}

	// Detect capabilities if QueryFull (has comprehensive data)
	if profile == QueryFull {
		caps := detectCapabilities(pdus, vendorHint)
		result.Capabilities = &caps
	}

	return result, nil
}

// buildQueryOIDs constructs the list of OIDs to query based on profile.
// Uses standard Printer-MIB OIDs only (no vendor-specific modules).
func buildQueryOIDs(profile QueryProfile) []string {
	return buildQueryOIDsWithCapabilities(profile, nil)
}

// buildQueryOIDsWithCapabilities constructs the list of OIDs with optional capability-aware filtering.
// Hardcoded standard Printer-MIB OIDs for simplicity and maintainability.
func buildQueryOIDsWithCapabilities(profile QueryProfile, _ *capabilities.DeviceCapabilities) []string { //nolint:revive
	var oids []string

	switch profile {
	case QueryMinimal:
		// Just serial number OID (fastest detection)
		oids = []string{
			"1.3.6.1.2.1.43.5.1.1.17.1", // prtGeneralSerialNumber
		}

	case QueryEssential:
		// Serial + basic device info + toner + pages + status
		oids = []string{
			"1.3.6.1.2.1.1.1.0",         // sysDescr
			"1.3.6.1.2.1.1.5.0",         // sysName
			"1.3.6.1.2.1.25.3.2.1.3.1",  // hrDeviceDescr (model)
			"1.3.6.1.2.1.43.5.1.1.17.1", // prtGeneralSerialNumber
			"1.3.6.1.2.1.43.10.2.1.4.1", // prtMarkerLifeCount (page count)
			"1.3.6.1.2.1.43.11.1.1.6.1", // prtMarkerSuppliesLevel (toner level)
			"1.3.6.1.2.1.43.11.1.1.9.1", // prtMarkerSuppliesMaxCapacity
			"1.3.6.1.2.1.25.3.5.1.1.1",  // hrPrinterStatus
		}

	case QueryFull:
		// Full walk - return nil to indicate walk mode
		return nil

	case QueryMetrics:
		// Standard metrics (page counts + toner levels + status)
		oids = []string{
			"1.3.6.1.2.1.43.10.2.1.4.1", // prtMarkerLifeCount
			"1.3.6.1.2.1.43.11.1.1.6.1", // prtMarkerSuppliesLevel
			"1.3.6.1.2.1.43.11.1.1.9.1", // prtMarkerSuppliesMaxCapacity
			"1.3.6.1.2.1.25.3.5.1.1.1",  // hrPrinterStatus
		}
	}

	return oids
}

// detectCapabilities analyzes SNMP PDUs to determine device capabilities.
// This is called during QueryFull to populate the Capabilities field.
func detectCapabilities(pdus []gosnmp.SnmpPDU, vendorHint string) capabilities.DeviceCapabilities {
	// Extract basic device info from PDUs for capability detection
	evidence := &capabilities.DetectionEvidence{
		PDUs:     pdus,
		Vendor:   vendorHint,
		SysDescr: extractOIDString(pdus, "1.3.6.1.2.1.1.1.0"),
		SysOID:   extractOIDString(pdus, "1.3.6.1.2.1.1.2.0"),
		Model:    extractOIDString(pdus, "1.3.6.1.2.1.25.3.2.1.3.1"),  // hrDeviceDescr
		Serial:   extractOIDString(pdus, "1.3.6.1.2.1.43.5.1.1.17.1"), // prtGeneralSerialNumber
	}

	// If model is empty, try to extract from sysDescr
	if evidence.Model == "" && evidence.SysDescr != "" {
		// SysDescr often contains model info (e.g., "HP LaserJet Pro M404dn")
		evidence.Model = evidence.SysDescr
	}

	// Run capability detection
	registry := capabilities.NewCapabilityRegistry()
	return registry.DetectAll(evidence)
}

// extractOIDString extracts a string value from PDUs for the given OID.
func extractOIDString(pdus []gosnmp.SnmpPDU, oid string) string {
	oid = normalizeOID(oid)
	for _, pdu := range pdus {
		if normalizeOID(pdu.Name) == oid {
			if bytes, ok := pdu.Value.([]byte); ok {
				return string(bytes)
			}
			if str, ok := pdu.Value.(string); ok {
				return str
			}
		}
	}
	return ""
}

// normalizeOID removes leading dot if present
func normalizeOID(oid string) string {
	if len(oid) > 0 && oid[0] == '.' {
		return oid[1:]
	}
	return oid
}
