package agent

import "time"

// MergePrinterInfo merges two PrinterInfo values, preferring non-empty and
// more complete fields from `extra` while preserving `base` as the fallback.
func MergePrinterInfo(base PrinterInfo, extra PrinterInfo) PrinterInfo {
	// strings: prefer extra if non-empty
	if extra.Manufacturer != "" {
		base.Manufacturer = extra.Manufacturer
	}
	if extra.Model != "" {
		base.Model = extra.Model
	}
	if extra.Serial != "" {
		base.Serial = extra.Serial
	}
	if extra.AdminContact != "" {
		base.AdminContact = extra.AdminContact
	}
	if extra.AssetID != "" {
		base.AssetID = extra.AssetID
	}

	// numeric counters: prefer non-zero extra values
	if extra.PageCount != 0 {
		base.PageCount = extra.PageCount
		base.TotalMonoImpressions = extra.PageCount
	}
	if extra.TotalMonoImpressions != 0 {
		base.TotalMonoImpressions = extra.TotalMonoImpressions
	}
	if extra.MonoImpressions != 0 {
		base.MonoImpressions = extra.MonoImpressions
	}
	if extra.ColorImpressions != 0 {
		base.ColorImpressions = extra.ColorImpressions
	}
	if extra.BlackImpressions != 0 {
		base.BlackImpressions = extra.BlackImpressions
	}
	if extra.CyanImpressions != 0 {
		base.CyanImpressions = extra.CyanImpressions
	}
	if extra.MagentaImpressions != 0 {
		base.MagentaImpressions = extra.MagentaImpressions
	}
	if extra.YellowImpressions != 0 {
		base.YellowImpressions = extra.YellowImpressions
	}

	// merge toner levels (prefer extra non-zero values)
	if base.TonerLevels == nil {
		base.TonerLevels = map[string]int{}
	}
	for k, v := range extra.TonerLevels {
		if v != 0 {
			base.TonerLevels[k] = v
		} else {
			// preserve existing if extra is zero
			if _, ok := base.TonerLevels[k]; !ok {
				base.TonerLevels[k] = v
			}
		}
	}

	// consumables: union
	seen := map[string]bool{}
	newConsum := []string{}
	for _, c := range base.Consumables {
		if c == "" {
			continue
		}
		seen[c] = true
		newConsum = append(newConsum, c)
	}
	for _, c := range extra.Consumables {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		newConsum = append(newConsum, c)
	}
	base.Consumables = newConsum

	// firmware: prefer extra
	if extra.Firmware != "" {
		base.Firmware = extra.Firmware
	}
	// network fields: prefer extra if present
	if extra.SubnetMask != "" {
		base.SubnetMask = extra.SubnetMask
	}
	if extra.Gateway != "" {
		base.Gateway = extra.Gateway
	}
	if len(extra.DNSServers) > 0 {
		base.DNSServers = extra.DNSServers
	}
	if extra.Hostname != "" {
		base.Hostname = extra.Hostname
	}
	// uptime: prefer newer (larger) value if present
	if extra.UptimeSeconds != 0 {
		if base.UptimeSeconds == 0 || extra.UptimeSeconds > base.UptimeSeconds {
			base.UptimeSeconds = extra.UptimeSeconds
		}
	}
	// duplex: prefer true if any indicates support
	if extra.DuplexSupported {
		base.DuplexSupported = true
	}

	// merge paper tray status map
	if base.PaperTrayStatus == nil {
		base.PaperTrayStatus = map[string]string{}
	}
	for k, v := range extra.PaperTrayStatus {
		base.PaperTrayStatus[k] = v
	}

	// merge toner alerts (unique)
	seen = map[string]bool{}
	msgs2 := []string{}
	for _, m := range base.TonerAlerts {
		if m == "" {
			continue
		}
		seen[m] = true
		msgs2 = append(msgs2, m)
	}
	for _, m := range extra.TonerAlerts {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		msgs2 = append(msgs2, m)
	}
	base.TonerAlerts = msgs2

	// status messages: append unique
	seen = map[string]bool{}
	msgs := []string{}
	for _, m := range base.StatusMessages {
		if m == "" {
			continue
		}
		seen[m] = true
		msgs = append(msgs, m)
	}
	for _, m := range extra.StatusMessages {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		msgs = append(msgs, m)
	}
	base.StatusMessages = msgs

	// detection reasons: union
	seen = map[string]bool{}
	reasons := []string{}
	for _, r := range base.DetectionReasons {
		if r == "" {
			continue
		}
		seen[r] = true
		reasons = append(reasons, r)
	}
	for _, r := range extra.DetectionReasons {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		reasons = append(reasons, r)
	}
	base.DetectionReasons = reasons

	// last seen: prefer newer timestamp if present
	if !extra.LastSeen.IsZero() {
		base.LastSeen = extra.LastSeen
	}

	// MAC / OpenPorts / DiscoveryMethods: prefer extra if base empty
	if base.MAC == "" && extra.MAC != "" {
		base.MAC = extra.MAC
	}
	if len(base.OpenPorts) == 0 && len(extra.OpenPorts) > 0 {
		base.OpenPorts = extra.OpenPorts
	}
	if len(base.DiscoveryMethods) == 0 && len(extra.DiscoveryMethods) > 0 {
		base.DiscoveryMethods = extra.DiscoveryMethods
	}

	// ensure LastSeen has some value
	if base.LastSeen.IsZero() {
		base.LastSeen = time.Now()
	}

	return base
}
