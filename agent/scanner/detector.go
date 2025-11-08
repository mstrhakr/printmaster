package scanner

import (
	"context"
	"fmt"
)

// SavedDeviceChecker is an interface for checking if a device IP is already known.
// This allows bypassing expensive SNMP queries for devices we already have in storage.
type SavedDeviceChecker interface {
	// IsKnownDevice returns true if the IP is a known device, along with any cached metadata.
	IsKnownDevice(ip string) (bool, interface{})
}

// DetectorConfig holds configuration for the detector stage.
type DetectorConfig struct {
	// SavedDeviceChecker checks if device is already known (optional, can be nil)
	SavedDeviceChecker SavedDeviceChecker

	// SNMPTimeout for detection queries (seconds)
	SNMPTimeout int

	// SkipSavedDevices when true, bypasses SNMP for known devices
	SkipSavedDevices bool
}

// DetectFunc creates a detection function that uses QueryDevice for compact printer detection.
// This function is designed to be used with scanner.StartDetectionPool.
//
// Detection logic:
//  1. If SkipSavedDevices=true and device is known, return cached data immediately
//  2. Otherwise, use QueryDevice with QueryMinimal profile to get serial number
//  3. If serial found, mark as printer
func DetectFunc(cfg DetectorConfig) func(ctx context.Context, job ScanJob, openPorts []int) (interface{}, bool, error) {
	if cfg.SNMPTimeout == 0 {
		cfg.SNMPTimeout = 5 // default 5 second timeout for detection
	}

	return func(ctx context.Context, job ScanJob, openPorts []int) (interface{}, bool, error) {
		// 1. Check if device is already known (saved device bypass)
		if cfg.SkipSavedDevices && cfg.SavedDeviceChecker != nil {
			isKnown, cachedData := cfg.SavedDeviceChecker.IsKnownDevice(job.IP)
			if isKnown {
				// Return cached data without querying
				return cachedData, true, nil
			}
		}

		// 2. No open ports = can't be a network printer
		if len(openPorts) == 0 {
			return nil, false, nil
		}

		// 3. Use QueryDevice with QueryMinimal to check for printer
		// This queries only serial number OIDs (fastest check)
		result, err := QueryDevice(ctx, job.IP, QueryMinimal, "", cfg.SNMPTimeout)
		if err != nil {
			// SNMP failed - not a printer or SNMP disabled
			return nil, false, nil
		}

		// 4. Check if we got any SNMP data
		if result == nil || len(result.PDUs) == 0 {
			return nil, false, nil
		}

		// 5. Look for serial number in PDUs
		hasSerial := false
		for _, pdu := range result.PDUs {
			// Check if this is a serial number OID
			// Standard serial: 1.3.6.1.2.1.43.5.1.1.17.1
			if len(pdu.Value.([]byte)) > 0 {
				hasSerial = true
				break
			}
		}

		if hasSerial {
			// Found serial = likely a printer
			return result, true, nil
		}

		// No serial found = probably not a printer
		return result, false, nil
	}
}

// DeepScanFunc creates a deep scan function that uses QueryDevice for full device enumeration.
// This function is designed to be used with scanner.StartDeepScanPool.
//
// Deep scan logic:
//  1. Extract vendor hint from DetectionResult (if available)
//  2. Use QueryDevice with QueryFull profile for complete SNMP walk
//  3. Return full device information
func DeepScanFunc(cfg DetectorConfig) func(ctx context.Context, dr DetectionResult) (interface{}, error) {
	if cfg.SNMPTimeout == 0 {
		cfg.SNMPTimeout = 30 // default 30 second timeout for deep scan
	}

	return func(ctx context.Context, dr DetectionResult) (interface{}, error) {
		// 1. Extract vendor hint from detection result
		vendorHint := ""
		if dr.Info != nil {
			// Try to extract vendor from QueryResult
			if qr, ok := dr.Info.(*QueryResult); ok {
				vendorHint = qr.VendorHint
			}
		}

		// 2. Perform full SNMP walk
		result, err := QueryDevice(ctx, dr.Job.IP, QueryFull, vendorHint, cfg.SNMPTimeout)
		if err != nil {
			return nil, fmt.Errorf("deep scan failed for %s: %w", dr.Job.IP, err)
		}

		return result, nil
	}
}

// EnrichFunc creates an enrichment function for refreshing known devices.
// This uses QueryEssential profile to update toner levels, page counts, and status.
func EnrichFunc(cfg DetectorConfig) func(ctx context.Context, ip string, vendorHint string) (interface{}, error) {
	if cfg.SNMPTimeout == 0 {
		cfg.SNMPTimeout = 10 // default 10 second timeout for enrichment
	}

	return func(ctx context.Context, ip string, vendorHint string) (interface{}, error) {
		// Use QueryEssential to get serial + toner + pages + status
		result, err := QueryDevice(ctx, ip, QueryEssential, vendorHint, cfg.SNMPTimeout)
		if err != nil {
			return nil, fmt.Errorf("enrichment failed for %s: %w", ip, err)
		}

		return result, nil
	}
}
