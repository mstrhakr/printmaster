package main

import (
	"context"
	"fmt"
	"time"

	"printmaster/agent/agent"
	"printmaster/agent/scanner"
	"printmaster/agent/storage"
)

// Discover performs discovery using the new modular scanner pipeline.
// This bridges the new scanner package to the existing agent API.
//
// Parameters:
//   - ctx: Context for cancellation
//   - ranges: IP ranges to scan (empty = auto-detect local subnet)
//   - mode: "quick" for fast TCP probe, "full" for complete SNMP scan
//   - discoveryConfig: Discovery settings (ARP, TCP, SNMP enabled flags)
//   - deviceStore: Storage for checking saved devices
//   - concurrency: Number of worker goroutines
//   - timeout: Timeout in seconds for SNMP operations
//
// Returns discovered devices as agent.PrinterInfo structs
func Discover(
	ctx context.Context,
	ranges []string,
	mode string,
	discoveryConfig *agent.DiscoveryConfig,
	deviceStore storage.DeviceStore,
	concurrency int,
	timeout int,
) ([]agent.PrinterInfo, error) {

	// Respect master IP scanning toggle stored in discovery_settings.
	if agentConfigStore != nil {
		var stored map[string]interface{}
		if err := agentConfigStore.GetConfigValue("discovery_settings", &stored); err == nil && stored != nil {
			if v, ok := stored["ip_scanning_enabled"]; ok {
				if vb, ok2 := v.(bool); ok2 && !vb {
					return nil, fmt.Errorf("ip scanning is disabled in agent settings")
				}
			}
		}
	}

	if concurrency <= 0 {
		concurrency = 50
	}
	if timeout <= 0 {
		timeout = 5
	}

	// Step 1: Parse ranges and enumerate IPs
	parseAdapter := func(text string, maxAddrs int) (*scanner.ParseResult, error) {
		// Use existing agent.ParseRangeText
		agentResult, err := agent.ParseRangeText(text, maxAddrs)
		if err != nil {
			return nil, err
		}
		// Convert to scanner.ParseResult
		scannerResult := &scanner.ParseResult{
			IPs:        agentResult.IPs,
			Count:      agentResult.Count,
			Normalized: agentResult.Normalized,
		}
		// Convert errors
		for _, e := range agentResult.Errors {
			scannerResult.Errors = append(scannerResult.Errors, scanner.ParseError{
				Line: e.Line,
				Msg:  e.Msg,
			})
		}
		return scannerResult, nil
	}

	// Step 2: Build saved device checker
	savedDeviceChecker := &savedDeviceCheckerImpl{
		store: deviceStore,
		cache: make(map[string]interface{}),
	}
	if deviceStore != nil {
		// Load saved devices into cache
		saved := true
		savedDevices, err := deviceStore.List(ctx, storage.DeviceFilter{IsSaved: &saved})
		if err == nil {
			for _, dev := range savedDevices {
				savedDeviceChecker.cache[dev.IP] = dev
			}
			appLogger.Info("Loaded saved devices for bypass", "count", len(savedDeviceChecker.cache))
		}
	}

	// Step 3: Configure detector
	detectorConfig := scanner.DetectorConfig{
		SavedDeviceChecker: savedDeviceChecker,
		SkipSavedDevices:   !discoveryConfig.SNMPEnabled, // Skip if SNMP disabled
		SNMPTimeout:        timeout,
	}

	// Step 4: Choose mode
	switch mode {
	case "quick":
		// Quick mode: Just TCP probe + minimal SNMP (like old /discover_now)
		results, err := quickDiscovery(ctx, ranges, parseAdapter, detectorConfig, concurrency)
		if err != nil {
			return nil, fmt.Errorf("quick discovery failed: %w", err)
		}
		return results, nil

	case "full":
		// Full mode: Complete pipeline with deep SNMP walks
		results, err := fullDiscovery(ctx, ranges, parseAdapter, detectorConfig, discoveryConfig, concurrency)
		if err != nil {
			return nil, fmt.Errorf("full discovery failed: %w", err)
		}
		return results, nil

	default:
		return nil, fmt.Errorf("invalid discovery mode: %s (must be 'quick' or 'full')", mode)
	}
}

// savedDeviceCheckerImpl implements scanner.SavedDeviceChecker
type savedDeviceCheckerImpl struct {
	store storage.DeviceStore
	cache map[string]interface{}
}

func (s *savedDeviceCheckerImpl) IsKnownDevice(ip string) (bool, interface{}) {
	if data, ok := s.cache[ip]; ok {
		return true, data
	}
	return false, nil
}

// quickDiscovery performs fast TCP probe + minimal SNMP check
func quickDiscovery(
	ctx context.Context,
	ranges []string,
	parseAdapter scanner.ParseRangeAdapter,
	detectorConfig scanner.DetectorConfig,
	concurrency int,
) ([]agent.PrinterInfo, error) {

	var results []agent.PrinterInfo

	// Step 1: Enumerate IPs from ranges
	var allIPs []string
	if len(ranges) == 0 {
		// Auto-detect local subnet
		subnets, err := agent.GetLocalSubnets()
		if err != nil || len(subnets) == 0 {
			return results, fmt.Errorf("no ranges provided and could not auto-detect subnet")
		}
		// Use first subnet
		ranges = []string{subnets[0].String()}
	}

	// Parse each range
	for _, rangeText := range ranges {
		scannerResult, err := parseAdapter(rangeText, 10000)
		if err != nil {
			appLogger.Warn("Failed to parse range", "range", rangeText, "error", err)
			continue
		}
		allIPs = append(allIPs, scannerResult.IPs...)
	}

	if len(allIPs) == 0 {
		return results, fmt.Errorf("no IPs to scan after parsing ranges")
	}

	appLogger.Info("Quick discovery starting", "ips", len(allIPs))

	// Step 2: Create scanner config for liveness probe
	scannerConfig := scanner.ScannerConfig{
		LivenessWorkers:  concurrency,
		LivenessTimeout:  500 * time.Millisecond,
		LivenessPorts:    []int{9100, 80, 443},
		DetectionWorkers: 10,
		DetectFunc:       scanner.DetectFunc(detectorConfig),
	}

	// Step 3: Create job channel
	jobs := make(chan scanner.ScanJob, len(allIPs))
	for _, ip := range allIPs {
		jobs <- scanner.ScanJob{
			IP:     ip,
			Source: "quick-discovery",
		}
	}
	close(jobs)

	// Step 4: Run liveness pool -> detection pool
	livenessResults := scanner.StartLivenessPool(ctx, scannerConfig, jobs)
	detectionResults := scanner.StartDetectionPool(ctx, scannerConfig, livenessResults)

	// Step 5: Collect results and convert QueryResult to PrinterInfo
	for dr := range detectionResults {
		if !dr.IsPrinter {
			continue
		}

		// Extract basic info from QueryResult
		if qr, ok := dr.Info.(*scanner.QueryResult); ok {
			// Parse PDUs to get printer info
			pi, _ := agent.ParsePDUs(qr.IP, qr.PDUs, nil, nil)
			// Merge vendor-specific metrics (ICE-style OIDs)
			agent.MergeVendorMetrics(&pi, qr.PDUs, qr.VendorHint)
			pi.DiscoveryMethods = append(pi.DiscoveryMethods, "quick-discovery")

			// Copy capabilities from QueryResult
			if qr.Capabilities != nil {
				pi.IsColor = qr.Capabilities.IsColor
				pi.IsMono = qr.Capabilities.IsMono
				pi.IsCopier = qr.Capabilities.IsCopier
				pi.IsScanner = qr.Capabilities.IsScanner
				pi.IsFax = qr.Capabilities.IsFax
				pi.IsLaser = qr.Capabilities.IsLaser
				pi.IsInkjet = qr.Capabilities.IsInkjet
				pi.HasDuplex = qr.Capabilities.HasDuplex
				pi.DeviceType = qr.Capabilities.DeviceType
			}

			results = append(results, pi)
		}
	}

	appLogger.Info("Quick discovery complete", "found", len(results))
	return results, nil
}

// fullDiscovery performs complete SNMP scan with worker pools
func fullDiscovery(
	ctx context.Context,
	ranges []string,
	parseAdapter scanner.ParseRangeAdapter,
	detectorConfig scanner.DetectorConfig,
	discoveryConfig *agent.DiscoveryConfig,
	concurrency int,
) ([]agent.PrinterInfo, error) {

	var results []agent.PrinterInfo

	// Step 1: Enumerate IPs from ranges
	var allIPs []string
	if len(ranges) == 0 {
		// Auto-detect local subnet
		subnets, err := agent.GetLocalSubnets()
		if err != nil || len(subnets) == 0 {
			return results, fmt.Errorf("no ranges provided and could not auto-detect subnet")
		}
		ranges = []string{subnets[0].String()}
	}

	// Parse each range
	for _, rangeText := range ranges {
		scannerResult, err := parseAdapter(rangeText, 10000)
		if err != nil {
			appLogger.Warn("Failed to parse range", "range", rangeText, "error", err)
			continue
		}
		allIPs = append(allIPs, scannerResult.IPs...)
	}

	if len(allIPs) == 0 {
		return results, fmt.Errorf("no IPs to scan after parsing ranges")
	}

	appLogger.Info("Full discovery starting", "ips", len(allIPs))

	// Step 2: Configure scanner pipeline
	// Adjust timeout based on discovery config
	deepScanTimeout := 30
	if !discoveryConfig.SNMPEnabled {
		deepScanTimeout = 5 // Fast scan if SNMP disabled
	}

	deepScanConfig := detectorConfig
	deepScanConfig.SNMPTimeout = deepScanTimeout

	scannerConfig := scanner.ScannerConfig{
		LivenessWorkers:  concurrency,
		LivenessTimeout:  500 * time.Millisecond,
		LivenessPorts:    []int{9100, 80, 443, 515, 631},
		DetectionWorkers: concurrency / 5,
		DetectFunc:       scanner.DetectFunc(detectorConfig),
		DeepScanWorkers:  concurrency / 10,
		DeepScanFunc:     scanner.DeepScanFunc(deepScanConfig),
	}

	// Step 3: Create job channel
	jobs := make(chan scanner.ScanJob, len(allIPs))
	for _, ip := range allIPs {
		jobs <- scanner.ScanJob{
			IP:     ip,
			Source: "full-discovery",
		}
	}
	close(jobs)

	// Step 4: Run full pipeline: Liveness -> Detection -> DeepScan
	livenessResults := scanner.StartLivenessPool(ctx, scannerConfig, jobs)
	detectionResults := scanner.StartDetectionPool(ctx, scannerConfig, livenessResults)
	deepScanResults := scanner.StartDeepScanPool(ctx, scannerConfig, detectionResults)

	// Step 5: Collect results and convert QueryResult to PrinterInfo
	for rawResult := range deepScanResults {
		// Handle errors
		if err, ok := rawResult.(error); ok {
			appLogger.Warn("Deep scan error", "error", err)
			continue
		}

		// Convert QueryResult to PrinterInfo
		if qr, ok := rawResult.(*scanner.QueryResult); ok {
			pi, isPrinter := agent.ParsePDUs(qr.IP, qr.PDUs, nil, nil)
			// Merge vendor-specific metrics (ICE-style OIDs)
			agent.MergeVendorMetrics(&pi, qr.PDUs, qr.VendorHint)
			if isPrinter {
				pi.DiscoveryMethods = append(pi.DiscoveryMethods, "full-discovery")
				pi.LastSeen = time.Now()

				// Transfer detected capabilities from QueryResult
				if qr.Capabilities != nil {
					pi.IsColor = qr.Capabilities.IsColor
					pi.IsMono = qr.Capabilities.IsMono
					pi.IsCopier = qr.Capabilities.IsCopier
					pi.IsScanner = qr.Capabilities.IsScanner
					pi.IsFax = qr.Capabilities.IsFax
					pi.IsLaser = qr.Capabilities.IsLaser
					pi.IsInkjet = qr.Capabilities.IsInkjet
					pi.HasDuplex = qr.Capabilities.HasDuplex
					pi.DeviceType = qr.Capabilities.DeviceType
				}

				results = append(results, pi)

				// Store device using the helper function
				agent.UpsertDiscoveredPrinter(pi)
			}
		}
	}

	appLogger.Info("Full discovery complete", "found", len(results))
	return results, nil
}

// DiscoveredDevice holds a discovery result with source hints
type DiscoveredDevice struct {
	IP     string `json:"ip"`
	Source string `json:"source"` // tcp, arp, mdns
	Ports  []int  `json:"ports,omitempty"`
}

// DiscoverNow performs a quick synchronous discovery (replacement for /discover_now)
// This is a convenience wrapper around Discover with mode="quick"
func DiscoverNow(ctx context.Context, timeout time.Duration) ([]DiscoveredDevice, error) {
	// Convert timeout to seconds
	timeoutSec := int(timeout.Seconds())
	if timeoutSec == 0 {
		timeoutSec = 5
	}

	// Auto-detect local subnet ranges
	ranges := []string{} // Empty = auto-detect

	discoveryConfig := &agent.DiscoveryConfig{
		TCPEnabled:  true,
		SNMPEnabled: false, // Quick mode doesn't need SNMP
	}

	results, err := Discover(
		ctx,
		ranges,
		"quick",
		discoveryConfig,
		nil, // no device store for quick mode
		50,  // concurrency
		timeoutSec,
	)
	if err != nil {
		return nil, err
	}

	// Convert agent.PrinterInfo to DiscoveredDevice
	var discovered []DiscoveredDevice
	for _, pi := range results {
		discovered = append(discovered, DiscoveredDevice{
			IP:     pi.IP,
			Source: "tcp",
			Ports:  pi.OpenPorts,
		})
	}

	return discovered, nil
}

// LiveDiscoveryDetect performs detection on a single IP for live discovery (mDNS, SSDP, WS-Discovery).
// This is a lightweight wrapper around the new scanner that uses QueryEssential for detailed device info.
//
// Returns the discovered printer info or an error if detection fails.
func LiveDiscoveryDetect(ctx context.Context, ip string, timeoutSeconds int) (*agent.PrinterInfo, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}

	// Use QueryDevice directly with QueryEssential profile
	// This gets serial + toner + page counts in one operation
	result, err := scanner.QueryDevice(
		ctx,
		ip,
		scanner.QueryEssential,
		"", // vendor auto-detected
		timeoutSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("query failed for %s: %w", ip, err)
	}

	// Check if we got any data
	if result == nil || len(result.PDUs) == 0 {
		return nil, fmt.Errorf("no SNMP data received from %s", ip)
	}

	// Parse the SNMP result to get PrinterInfo
	meta := &agent.ScanMeta{
		OpenPorts:        []int{9100}, // Assume printer port for live discovery
		DiscoveryMethods: []string{},
	}
	pi, _ := agent.ParsePDUs(ip, result.PDUs, meta, func(msg string) {
		appLogger.Debug("SNMP parse", "ip", ip, "msg", msg)
	})
	// Merge vendor-specific metrics (ICE-style OIDs)
	agent.MergeVendorMetrics(&pi, result.PDUs, result.VendorHint)

	// Copy capabilities from QueryResult
	if result.Capabilities != nil {
		pi.IsColor = result.Capabilities.IsColor
		pi.IsMono = result.Capabilities.IsMono
		pi.IsCopier = result.Capabilities.IsCopier
		pi.IsScanner = result.Capabilities.IsScanner
		pi.IsFax = result.Capabilities.IsFax
		pi.IsLaser = result.Capabilities.IsLaser
		pi.IsInkjet = result.Capabilities.IsInkjet
		pi.HasDuplex = result.Capabilities.HasDuplex
		pi.DeviceType = result.Capabilities.DeviceType
	}

	return &pi, nil
}

// LiveDiscoveryDeepScan performs a full SNMP WALK on a single IP discovered via live methods.
// This is used when the lightweight QueryEssential didn't return a serial number.
// Since the device was already discovered by mDNS/WS-Discovery/etc, we know it's alive
// and worth doing a complete scan.
func LiveDiscoveryDeepScan(ctx context.Context, ip string, timeoutSeconds int) (*agent.PrinterInfo, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30 // Use longer timeout for full WALK
	}

	appLogger.Debug("Live discovery: performing deep scan", "ip", ip)

	// Use QueryDevice with QueryFull profile to get everything
	result, err := scanner.QueryDevice(
		ctx,
		ip,
		scanner.QueryFull,
		"", // vendor auto-detected
		timeoutSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("deep scan failed for %s: %w", ip, err)
	}

	// Check if we got any data
	if result == nil || len(result.PDUs) == 0 {
		return nil, fmt.Errorf("no SNMP data received from deep scan of %s", ip)
	}

	// Parse the SNMP result to get PrinterInfo
	meta := &agent.ScanMeta{
		OpenPorts:        []int{9100}, // Assume printer port for live discovery
		DiscoveryMethods: []string{},
	}
	pi, _ := agent.ParsePDUs(ip, result.PDUs, meta, func(msg string) {
		appLogger.Debug("SNMP deep scan parse", "ip", ip, "msg", msg)
	})
	// Merge vendor-specific metrics (ICE-style OIDs)
	agent.MergeVendorMetrics(&pi, result.PDUs, result.VendorHint)

	// Copy capabilities from QueryResult
	if result.Capabilities != nil {
		pi.IsColor = result.Capabilities.IsColor
		pi.IsMono = result.Capabilities.IsMono
		pi.IsCopier = result.Capabilities.IsCopier
		pi.IsScanner = result.Capabilities.IsScanner
		pi.IsFax = result.Capabilities.IsFax
		pi.IsLaser = result.Capabilities.IsLaser
		pi.IsInkjet = result.Capabilities.IsInkjet
		pi.HasDuplex = result.Capabilities.HasDuplex
		pi.DeviceType = result.Capabilities.DeviceType
	}

	appLogger.Debug("Live discovery: deep scan complete", "ip", ip, "serial", pi.Serial, "manufacturer", pi.Manufacturer, "model", pi.Model)

	return &pi, nil
}

// CollectMetrics performs metrics collection using the new scanner's QueryMetrics profile.
// This replaces agent.CollectMetricsSnapshot when the feature flag is enabled.
//
// Returns vendor-specific metrics snapshot optimized for scheduled collection.
func CollectMetrics(ctx context.Context, ip string, serial string, vendorHint string, timeoutSeconds int) (*agent.DeviceMetricsSnapshot, error) {
	return CollectMetricsWithOIDs(ctx, ip, serial, vendorHint, timeoutSeconds, nil)
}

// CollectMetricsWithOIDs collects metrics from a device, optionally using learned OIDs for efficiency
func CollectMetricsWithOIDs(ctx context.Context, ip string, serial string, vendorHint string, timeoutSeconds int, learnedOIDs *agent.LearnedOIDMap) (*agent.DeviceMetricsSnapshot, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}

	// Build OID list from learned OIDs if available
	var oidList []string
	useLearnedOIDs := learnedOIDs != nil && (learnedOIDs.PageCountOID != "" || learnedOIDs.MonoPagesOID != "")

	if useLearnedOIDs {
		appLogger.Info("Using learned OIDs for metrics collection", "ip", ip, "serial", serial)

		// Add learned OIDs to query list
		if learnedOIDs.PageCountOID != "" {
			oidList = append(oidList, learnedOIDs.PageCountOID)
		}
		if learnedOIDs.MonoPagesOID != "" && learnedOIDs.MonoPagesOID != learnedOIDs.PageCountOID {
			oidList = append(oidList, learnedOIDs.MonoPagesOID)
		}
		if learnedOIDs.ColorPagesOID != "" {
			oidList = append(oidList, learnedOIDs.ColorPagesOID)
		}
		if learnedOIDs.CyanOID != "" {
			oidList = append(oidList, learnedOIDs.CyanOID)
		}
		if learnedOIDs.MagentaOID != "" {
			oidList = append(oidList, learnedOIDs.MagentaOID)
		}
		if learnedOIDs.YellowOID != "" {
			oidList = append(oidList, learnedOIDs.YellowOID)
		}
		// Add vendor-specific OIDs
		for _, oid := range learnedOIDs.VendorSpecificOIDs {
			oidList = append(oidList, oid)
		}

		// Also add standard OIDs for serial, model, status
		oidList = append(oidList,
			"1.3.6.1.2.1.43.5.1.1.17.1", // prtGeneralSerialNumber
			"1.3.6.1.2.1.1.1.0",         // sysDescr
			"1.3.6.1.2.1.25.3.2.1.3.1",  // hrDeviceDescr (model)
		)
	}

	var result *scanner.QueryResult
	var err error

	if useLearnedOIDs && len(oidList) > 0 {
		// Query specific learned OIDs directly using SNMP GET
		cfg, err := scanner.GetSNMPConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get SNMP config: %w", err)
		}

		client, err := scanner.NewSNMPClient(cfg, ip, timeoutSeconds)
		if err != nil {
			return nil, fmt.Errorf("failed to create SNMP client: %w", err)
		}
		defer client.Close()

		packet, err := client.Get(oidList)
		if err != nil {
			appLogger.Warn("Learned OID query failed, falling back to vendor defaults", "ip", ip, "error", err)
			useLearnedOIDs = false
		} else if packet != nil {
			result = &scanner.QueryResult{
				IP:   ip,
				PDUs: packet.Variables,
			}
		}
	}

	// Fall back to vendor defaults if no learned OIDs or if query failed
	if !useLearnedOIDs || result == nil {
		// Use QueryDevice with QueryMetrics profile
		// This queries vendor-specific metrics OIDs (page counts, toner, scans, jams, etc.)
		result, err = scanner.QueryDevice(
			ctx,
			ip,
			scanner.QueryMetrics,
			vendorHint, // Use vendor hint for targeted OID selection
			timeoutSeconds,
		)
		if err != nil {
			return nil, fmt.Errorf("metrics query failed for %s: %w", ip, err)
		}
	}

	// Check if we got any data
	if result == nil || len(result.PDUs) == 0 {
		return nil, fmt.Errorf("no SNMP metrics data received from %s", ip)
	}

	appLogger.Info("Metrics SNMP query complete", "ip", ip, "vendor", vendorHint, "pdus_received", len(result.PDUs))

	// Debug log all PDU values
	for i, pdu := range result.PDUs {
		appLogger.Debug("Metrics PDU received",
			"ip", ip,
			"index", i,
			"oid", pdu.Name,
			"type", pdu.Type,
			"value", pdu.Value)
	}

	// Parse PDUs to get PrinterInfo with metrics
	meta := &agent.ScanMeta{
		OpenPorts:        []int{9100},
		DiscoveryMethods: []string{"metrics"},
	}
	pi, _ := agent.ParsePDUs(ip, result.PDUs, meta, func(msg string) {
		appLogger.Debug("Metrics parse", "ip", ip, "msg", msg)
	})
	// Merge vendor-specific metrics (ICE-style OIDs)
	agent.MergeVendorMetrics(&pi, result.PDUs, vendorHint)

	// Copy capabilities from QueryResult if present
	if result.Capabilities != nil {
		pi.IsColor = result.Capabilities.IsColor
		pi.IsMono = result.Capabilities.IsMono
		pi.IsCopier = result.Capabilities.IsCopier
		pi.IsScanner = result.Capabilities.IsScanner
		pi.IsFax = result.Capabilities.IsFax
		pi.IsLaser = result.Capabilities.IsLaser
		pi.IsInkjet = result.Capabilities.IsInkjet
		pi.HasDuplex = result.Capabilities.HasDuplex
		pi.DeviceType = result.Capabilities.DeviceType
	}

	appLogger.Info("Metrics parsed from PrinterInfo",
		"ip", ip,
		"page_count", pi.PageCount,
		"mono_impressions", pi.MonoImpressions,
		"color_impressions", pi.ColorImpressions,
		"meters_count", len(pi.Meters))

	// Convert PrinterInfo to DeviceMetricsSnapshot
	snapshot := &agent.DeviceMetricsSnapshot{
		Serial:      serial,
		TonerLevels: make(map[string]interface{}),
	}

	// Extract page counts
	if pi.PageCount > 0 {
		snapshot.PageCount = pi.PageCount
	}
	if pi.MonoImpressions > 0 {
		snapshot.MonoPages = pi.MonoImpressions
	}
	if pi.ColorImpressions > 0 {
		snapshot.ColorPages = pi.ColorImpressions
	}

	// Use Meters map if available (vendor-specific metrics)
	if pi.Meters != nil {
		if v, ok := pi.Meters["total_pages"]; ok && v > 0 {
			snapshot.PageCount = v
		}
		if v, ok := pi.Meters["mono_pages"]; ok && v > 0 {
			snapshot.MonoPages = v
		}
		if v, ok := pi.Meters["color_pages"]; ok && v > 0 {
			snapshot.ColorPages = v
		}
		if v, ok := pi.Meters["scans"]; ok && v > 0 {
			snapshot.ScanCount = v
		}
		if v, ok := pi.Meters["copies"]; ok && v > 0 {
			snapshot.CopyPages = v
		}
		if v, ok := pi.Meters["faxes"]; ok && v > 0 {
			snapshot.FaxPages = v
		}
		if v, ok := pi.Meters["jams"]; ok && v > 0 {
			snapshot.JamEvents = v
		}
	}

	// Extract toner levels
	if pi.TonerLevelBlack > 0 {
		snapshot.TonerLevels["black"] = pi.TonerLevelBlack
	}
	if pi.TonerLevelCyan > 0 {
		snapshot.TonerLevels["cyan"] = pi.TonerLevelCyan
	}
	if pi.TonerLevelMagenta > 0 {
		snapshot.TonerLevels["magenta"] = pi.TonerLevelMagenta
	}
	if pi.TonerLevelYellow > 0 {
		snapshot.TonerLevels["yellow"] = pi.TonerLevelYellow
	}

	return snapshot, nil
}
