package storage

import (
	"printmaster/agent/agent"
)

// PrinterInfoToDevice converts agent.PrinterInfo to storage.Device
func PrinterInfoToDevice(pi agent.PrinterInfo, isSaved bool) *Device {
	device := &Device{}

	// Set common Device fields
	device.Serial = pi.Serial
	device.IP = pi.IP
	device.Manufacturer = pi.Manufacturer
	device.Model = pi.Model
	device.Hostname = pi.Hostname
	device.Firmware = pi.Firmware
	device.MACAddress = pi.MAC
	device.SubnetMask = pi.SubnetMask
	device.Gateway = pi.Gateway
	device.StatusMessages = pi.StatusMessages
	device.LastSeen = pi.LastSeen
	device.DiscoveryMethod = "snmp"
	device.AssetNumber = pi.AssetID
	device.Location = pi.Location
	device.Description = pi.Description
	device.WebUIURL = pi.WebUIURL

	// Set agent-specific fields
	device.DNSServers = pi.DNSServers
	device.DHCPServer = pi.DHCPServer
	device.IsSaved = isSaved
	device.Visible = true

	// Handle consumables
	if len(pi.Consumables) > 0 {
		device.Consumables = pi.Consumables
	}

	// Handle discovery methods
	if len(pi.DiscoveryMethods) > 0 {
		device.DiscoveryMethod = pi.DiscoveryMethods[0] // Use first method
	}

	// Store additional info in RawData
	device.RawData = map[string]interface{}{
		"admin_contact":          pi.AdminContact,
		"asset_id":               pi.AssetID,
		"total_mono_impressions": pi.TotalMonoImpressions,
		"black_impressions":      pi.BlackImpressions,
		"cyan_impressions":       pi.CyanImpressions,
		"magenta_impressions":    pi.MagentaImpressions,
		"yellow_impressions":     pi.YellowImpressions,
		"toner_level_black":      pi.TonerLevelBlack,
		"toner_level_cyan":       pi.TonerLevelCyan,
		"toner_level_magenta":    pi.TonerLevelMagenta,
		"toner_level_yellow":     pi.TonerLevelYellow,
		"toner_desc_black":       pi.TonerDescBlack,
		"toner_desc_cyan":        pi.TonerDescCyan,
		"toner_desc_magenta":     pi.TonerDescMagenta,
		"toner_desc_yellow":      pi.TonerDescYellow,
		"open_ports":             pi.OpenPorts,
		"advertised_services":    pi.AdvertisedServices,
		"detection_reasons":      pi.DetectionReasons,
		"mono_impressions":       pi.MonoImpressions,
		"color_impressions":      pi.ColorImpressions,
		"uptime_seconds":         pi.UptimeSeconds,
		"duplex_supported":       pi.DuplexSupported,
		"paper_tray_status":      pi.PaperTrayStatus,
		"toner_alerts":           pi.TonerAlerts,
		"meters":                 pi.Meters,
		"learned_oids":           pi.LearnedOIDs, // Store learned OIDs for efficient metrics
	}

	return device
}

// DeviceToPrinterInfo converts storage.Device to agent.PrinterInfo
func DeviceToPrinterInfo(device *Device) agent.PrinterInfo {
	pi := agent.PrinterInfo{
		Serial:       device.Serial,
		IP:           device.IP,
		Manufacturer: device.Manufacturer,
		Model:        device.Model,
		Hostname:     device.Hostname,
		Firmware:     device.Firmware,
		MAC:          device.MACAddress,
		SubnetMask:   device.SubnetMask,
		Gateway:      device.Gateway,
		DNSServers:   device.DNSServers,
		DHCPServer:   device.DHCPServer,
		// NOTE: PageCount and TonerLevels retrieved from latest MetricsSnapshot, not Device
		Consumables:    device.Consumables,
		StatusMessages: device.StatusMessages,
		LastSeen:       device.LastSeen,
		WebUIURL:       device.WebUIURL,
	}

	// Extract from RawData if present
	if device.RawData != nil {
		if v, ok := device.RawData["admin_contact"].(string); ok {
			pi.AdminContact = v
		}
		if v, ok := device.RawData["asset_id"].(string); ok {
			pi.AssetID = v
		}
		if v, ok := device.RawData["total_mono_impressions"].(float64); ok {
			pi.TotalMonoImpressions = int(v)
		} else if v, ok := device.RawData["total_mono_impressions"].(int); ok {
			pi.TotalMonoImpressions = v
		}
		if v, ok := device.RawData["black_impressions"].(float64); ok {
			pi.BlackImpressions = int(v)
		} else if v, ok := device.RawData["black_impressions"].(int); ok {
			pi.BlackImpressions = v
		}
		if v, ok := device.RawData["uptime_seconds"].(float64); ok {
			pi.UptimeSeconds = int(v)
		} else if v, ok := device.RawData["uptime_seconds"].(int); ok {
			pi.UptimeSeconds = v
		}
		if v, ok := device.RawData["duplex_supported"].(bool); ok {
			pi.DuplexSupported = v
		}
		// Extract learned OIDs for efficient metrics collection
		if v, ok := device.RawData["learned_oids"].(map[string]interface{}); ok {
			learnedOIDs := agent.LearnedOIDMap{}
			if pageCountOID, ok := v["page_count_oid"].(string); ok {
				learnedOIDs.PageCountOID = pageCountOID
			}
			if monoPagesOID, ok := v["mono_pages_oid"].(string); ok {
				learnedOIDs.MonoPagesOID = monoPagesOID
			}
			if colorPagesOID, ok := v["color_pages_oid"].(string); ok {
				learnedOIDs.ColorPagesOID = colorPagesOID
			}
			if cyanOID, ok := v["cyan_oid"].(string); ok {
				learnedOIDs.CyanOID = cyanOID
			}
			if magentaOID, ok := v["magenta_oid"].(string); ok {
				learnedOIDs.MagentaOID = magentaOID
			}
			if yellowOID, ok := v["yellow_oid"].(string); ok {
				learnedOIDs.YellowOID = yellowOID
			}
			if tonerOIDPrefix, ok := v["toner_oid_prefix"].(string); ok {
				learnedOIDs.TonerOIDPrefix = tonerOIDPrefix
			}
			if serialOID, ok := v["serial_oid"].(string); ok {
				learnedOIDs.SerialOID = serialOID
			}
			if modelOID, ok := v["model_oid"].(string); ok {
				learnedOIDs.ModelOID = modelOID
			}
			if vendorOIDs, ok := v["vendor_specific_oids"].(map[string]interface{}); ok {
				learnedOIDs.VendorSpecificOIDs = make(map[string]string)
				for key, val := range vendorOIDs {
					if oid, ok := val.(string); ok {
						learnedOIDs.VendorSpecificOIDs[key] = oid
					}
				}
			}
			pi.LearnedOIDs = learnedOIDs
		}
		// Add more field extractions as needed
	}

	return pi
}

// PrinterInfoToScanSnapshot converts agent.PrinterInfo to storage.ScanSnapshot
// Note: Only device state (IP, hostname, firmware) is stored in scan_history.
// Metrics data (page counts, toner levels) should be stored separately using SaveMetricsSnapshot.
func PrinterInfoToScanSnapshot(pi agent.PrinterInfo) *ScanSnapshot {
	snapshot := &ScanSnapshot{
		Serial:          pi.Serial,
		CreatedAt:       pi.LastSeen,
		IP:              pi.IP,
		Hostname:        pi.Hostname,
		Firmware:        pi.Firmware,
		Consumables:     pi.Consumables,
		StatusMessages:  pi.StatusMessages,
		DiscoveryMethod: "snmp",
	}

	if len(pi.DiscoveryMethods) > 0 {
		snapshot.DiscoveryMethod = pi.DiscoveryMethods[0]
	}

	return snapshot
}

// PrinterInfoToMetricsSnapshot converts agent.PrinterInfo to storage.MetricsSnapshot
// This extracts only metrics data (page counts, toner levels) for time-series storage.
func PrinterInfoToMetricsSnapshot(pi agent.PrinterInfo) *MetricsSnapshot {
	// Convert TonerLevels map[string]int to map[string]interface{} for storage
	tonerLevels := make(map[string]interface{})
	for k, v := range pi.TonerLevels {
		tonerLevels[k] = v
	}

	snapshot := &MetricsSnapshot{}

	// Set common MetricsSnapshot fields
	snapshot.Serial = pi.Serial
	snapshot.Timestamp = pi.LastSeen
	snapshot.PageCount = pi.PageCount
	snapshot.ColorPages = pi.ColorImpressions
	snapshot.MonoPages = pi.MonoImpressions
	snapshot.TonerLevels = tonerLevels

	return snapshot
}
