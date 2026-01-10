package metrics

import (
	"regexp"
	"strconv"
	"strings"
)

// HPScraper implements vendor-specific web scraping for HP printers
type HPScraper struct{}

func init() {
	RegisterScraper(&HPScraper{})
}

func (s *HPScraper) Name() string {
	return "HP"
}

func (s *HPScraper) Detect(manufacturer, product string, vendorID uint16) bool {
	// Check USB vendor ID
	if vendorID == USBVendorHP {
		return true
	}

	// Check manufacturer/product strings
	combined := strings.ToLower(manufacturer + " " + product)
	return strings.Contains(combined, "hp ") ||
		strings.Contains(combined, "hewlett") ||
		strings.Contains(combined, "laserjet") ||
		strings.Contains(combined, "officejet") ||
		strings.Contains(combined, "deskjet") ||
		strings.Contains(combined, "envy") ||
		strings.Contains(combined, "photosmart")
}

func (s *HPScraper) Endpoints() []EndpointProbe {
	return []EndpointProbe{
		// HTML pages first - more widely supported across HP models
		{
			Path:        "/hp/device/InternalPages/Index?id=UsagePage",
			ContentType: "html",
			Priority:    1,
			Description: "HP Usage Page - detailed counters",
		},
		{
			Path:        "/hp/device/InternalPages/Index?id=SuppliesStatus",
			ContentType: "html",
			Priority:    2,
			Description: "HP Supplies Status page",
		},

		// XML APIs (machine-readable, newer models)
		{
			Path:        "/DevMgmt/ProductUsageDyn.xml",
			ContentType: "xml",
			Priority:    10,
			Description: "HP ProductUsageDyn XML - comprehensive usage data",
		},
		{
			Path:        "/DevMgmt/ConsumableConfigDyn.xml",
			ContentType: "xml",
			Priority:    11,
			Description: "HP ConsumableConfig XML - supply levels",
		},
		{
			Path:        "/DevMgmt/ProductConfigDyn.xml",
			ContentType: "xml",
			Priority:    12,
			Description: "HP ProductConfig XML - device info",
		},
		{
			Path:        "/DevMgmt/ProductStatusDyn.xml",
			ContentType: "xml",
			Priority:    13,
			Description: "HP ProductStatus XML - current status",
		},
		{
			Path:        "/DevMgmt/MediaHandlingDyn.xml",
			ContentType: "xml",
			Priority:    14,
			Description: "HP MediaHandling XML - paper tray info",
		},
		{
			Path:        "/DevMgmt/IoMgmtDyn.xml",
			ContentType: "xml",
			Priority:    15,
			Description: "HP IoMgmt XML - network/io config",
		},
		{
			Path:        "/hp/device/InternalPages/Index?id=ConfigurationPage",
			ContentType: "html",
			Priority:    20,
			Description: "HP Configuration Page",
		},
		{
			Path:        "/hp/device/info_deviceStatus.html",
			ContentType: "html",
			Priority:    20,
			Description: "HP Device Status HTML",
		},
		{
			Path:        "/hp/device/this.LCDisp498F6",
			ContentType: "html",
			Priority:    25,
			Description: "HP LCD display info",
		},

		// Alternative paths for different HP models
		{
			Path:        "/hp/device/DeviceInformation/View",
			ContentType: "html",
			Priority:    30,
			Description: "HP Device Information View",
		},
		{
			Path:        "/hp/device/UsagePage/Index",
			ContentType: "html",
			Priority:    31,
			Description: "HP Usage Page Index (alt)",
		},
		{
			Path:        "/hp/device/SuppliesStatus/Index",
			ContentType: "html",
			Priority:    32,
			Description: "HP Supplies Status Index (alt)",
		},

		// EWS (Embedded Web Server) paths
		{
			Path:        "/jd/DeviceStatus.xml",
			ContentType: "xml",
			Priority:    50,
			Description: "HP JD DeviceStatus XML",
		},
		{
			Path:        "/SSI/device_status.htm",
			ContentType: "html",
			Priority:    51,
			Description: "HP SSI device status",
		},

		// Info pages
		{
			Path:        "/info_configuration.html",
			ContentType: "html",
			Priority:    60,
			Description: "HP info configuration",
		},
		{
			Path:        "/info_suppliesStatus.html",
			ContentType: "html",
			Priority:    61,
			Description: "HP info supplies status",
		},
		{
			Path:        "/info_deviceStatus.html",
			ContentType: "html",
			Priority:    62,
			Description: "HP info device status",
		},

		// Discovery endpoint (lists all available paths)
		{
			Path:        "/DevMgmt/DiscoveryTree.xml",
			ContentType: "xml",
			Priority:    100,
			Description: "HP Discovery Tree - lists available endpoints",
		},
	}
}

func (s *HPScraper) Parse(body []byte, endpoint EndpointProbe) (*USBMetrics, error) {
	metrics := &USBMetrics{
		Source:     endpoint.Path,
		SourceType: endpoint.ContentType,
	}

	switch endpoint.ContentType {
	case "xml":
		return s.parseXML(body, endpoint, metrics)
	case "html":
		return s.parseHTML(body, endpoint, metrics)
	}

	return metrics, nil
}

// parseXML parses HP XML endpoints
func (s *HPScraper) parseXML(body []byte, endpoint EndpointProbe, metrics *USBMetrics) (*USBMetrics, error) {
	content := string(body)

	switch {
	case strings.Contains(endpoint.Path, "ProductUsageDyn"):
		return s.parseProductUsageXML(content, metrics)
	case strings.Contains(endpoint.Path, "ConsumableConfigDyn"):
		return s.parseConsumablesXML(content, metrics)
	case strings.Contains(endpoint.Path, "ProductConfigDyn"):
		return s.parseProductConfigXML(content, metrics)
	case strings.Contains(endpoint.Path, "ProductStatusDyn"):
		return s.parseProductStatusXML(content, metrics)
	}

	return metrics, nil
}

// parseProductUsageXML extracts page counts from HP ProductUsageDyn.xml
func (s *HPScraper) parseProductUsageXML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// HP ProductUsageDyn.xml format example:
	// <pudyn:TotalImpressions>12345</pudyn:TotalImpressions>
	// <pudyn:ColorImpressions>5000</pudyn:ColorImpressions>
	// <pudyn:MonochromeImpressions>7345</pudyn:MonochromeImpressions>
	// <pudyn:CopyImpressions>100</pudyn:CopyImpressions>
	// <pudyn:ScanImpressions>200</pudyn:ScanImpressions>

	metrics.TotalPages = extractXMLInt(content, "TotalImpressions")
	metrics.ColorPages = extractXMLInt(content, "ColorImpressions")
	metrics.MonoPages = extractXMLInt(content, "MonochromeImpressions")
	metrics.CopyPages = extractXMLInt(content, "CopyImpressions")
	metrics.ScanPages = extractXMLInt(content, "ScanImpressions")

	// Alternative tag names
	if metrics.TotalPages == 0 {
		metrics.TotalPages = extractXMLInt(content, "TotalPageCount")
	}
	if metrics.TotalPages == 0 {
		metrics.TotalPages = extractXMLInt(content, "LifeCount")
	}

	return metrics, nil
}

// parseConsumablesXML extracts supply levels from HP ConsumableConfigDyn.xml
func (s *HPScraper) parseConsumablesXML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// HP uses ConsumableInfo sections with ConsumableLabelCode and ConsumablePercentageLevelRemaining
	// <ccdyn:ConsumableLabelCode>BLACK</ccdyn:ConsumableLabelCode>
	// <ccdyn:ConsumablePercentageLevelRemaining>75</ccdyn:ConsumablePercentageLevelRemaining>

	// Parse each consumable section
	consumableSections := regexp.MustCompile(`(?s)<\w*:?ConsumableInfo[^>]*>.*?</\w*:?ConsumableInfo>`).FindAllString(content, -1)

	for _, section := range consumableSections {
		label := extractXMLString(section, "ConsumableLabelCode")
		level := extractXMLInt(section, "ConsumablePercentageLevelRemaining")

		if level == 0 {
			// Try alternative percentage tag
			level = extractXMLInt(section, "PercentageRemaining")
		}

		switch strings.ToUpper(label) {
		case "BLACK", "K":
			metrics.TonerBlack = level
		case "CYAN", "C":
			metrics.TonerCyan = level
		case "MAGENTA", "M":
			metrics.TonerMagenta = level
		case "YELLOW", "Y":
			metrics.TonerYellow = level
		}
	}

	return metrics, nil
}

// parseProductConfigXML extracts device info from HP ProductConfigDyn.xml
func (s *HPScraper) parseProductConfigXML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	metrics.Model = extractXMLString(content, "ProductName")
	if metrics.Model == "" {
		metrics.Model = extractXMLString(content, "MakeAndModel")
	}
	metrics.Serial = extractXMLString(content, "SerialNumber")

	return metrics, nil
}

// parseProductStatusXML extracts status from HP ProductStatusDyn.xml
func (s *HPScraper) parseProductStatusXML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// Status codes and messages
	metrics.Status = extractXMLString(content, "StatusCategory")
	if metrics.Status == "" {
		metrics.Status = extractXMLString(content, "DeviceStatus")
	}

	return metrics, nil
}

// parseHTML parses HP HTML pages
func (s *HPScraper) parseHTML(body []byte, endpoint EndpointProbe, metrics *USBMetrics) (*USBMetrics, error) {
	content := string(body)

	switch {
	case strings.Contains(endpoint.Path, "UsagePage"):
		return s.parseUsagePageHTML(content, metrics)
	case strings.Contains(endpoint.Path, "SuppliesStatus"):
		return s.parseSuppliesHTML(content, metrics)
	default:
		return s.parseGenericHTML(content, metrics)
	}
}

// parseUsagePageHTML extracts page counts from HP Usage Page HTML
func (s *HPScraper) parseUsagePageHTML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// HP M506 Usage Page format has specific IDs like:
	// <td id="UsagePage.EquivalentImpressionsTable.Print.Total" class="align-right">169,706.0</td>
	// <td id="UsagePage.ImpressionsByMediaSizeTable.Print.Letter.Total" class="align-right">169,706</td>
	// <strong id="UsagePage.DeviceInformation.DeviceSerialNumber">PHBGR10696</strong>
	// <strong id="UsagePage.DeviceInformation.ProductName">HP LaserJet M506</strong>

	// Extract total print pages from EquivalentImpressionsTable or ImpressionsByMediaSizeTable
	if metrics.TotalPages == 0 {
		metrics.TotalPages = extractHPTableValue(content, "EquivalentImpressionsTable.Print.Total")
	}
	if metrics.TotalPages == 0 {
		metrics.TotalPages = extractHPTableValue(content, "ImpressionsByMediaSizeTable.Print.Letter.Total")
	}
	if metrics.TotalPages == 0 {
		metrics.TotalPages = extractHPTableValue(content, "ImpressionsByMediaSizeTable.Print.Total")
	}

	// Try color and mono specific
	metrics.ColorPages = extractHPTableValue(content, "EquivalentImpressionsTable.Color.Total")
	metrics.MonoPages = extractHPTableValue(content, "EquivalentImpressionsTable.Mono.Total")

	// Copy/Scan/Fax if this is an MFP
	metrics.CopyPages = extractHPTableValue(content, "EquivalentImpressionsTable.Copy.Total")
	metrics.ScanPages = extractHPTableValue(content, "EquivalentImpressionsTable.Scan.Total")
	metrics.FaxPagesSent = extractHPTableValue(content, "EquivalentImpressionsTable.FaxSend.Total")
	metrics.FaxPagesRecv = extractHPTableValue(content, "EquivalentImpressionsTable.FaxReceive.Total")

	// Device info
	if metrics.Serial == "" {
		metrics.Serial = extractHPStrongValue(content, "DeviceInformation.DeviceSerialNumber")
	}
	if metrics.Model == "" {
		metrics.Model = extractHPStrongValue(content, "DeviceInformation.ProductName")
	}

	// Fallback to generic label-based parsing if HP-specific patterns didn't work
	if metrics.TotalPages == 0 {
		s.parseGenericUsageHTML(content, metrics)
	}

	return metrics, nil
}

// parseGenericUsageHTML is a fallback parser using label patterns
func (s *HPScraper) parseGenericUsageHTML(content string, metrics *USBMetrics) {
	patterns := []struct {
		labels []string
		target *int
	}{
		{[]string{"total pages", "total printed", "lifetime pages", "total impressions"}, &metrics.TotalPages},
		{[]string{"color pages", "color printed", "color impressions"}, &metrics.ColorPages},
		{[]string{"mono pages", "monochrome", "black only", "black/white"}, &metrics.MonoPages},
		{[]string{"copy pages", "copies made", "copy impressions"}, &metrics.CopyPages},
		{[]string{"scan pages", "scans", "scan impressions", "flatbed", "adf"}, &metrics.ScanPages},
		{[]string{"fax sent", "faxes sent", "fax pages sent"}, &metrics.FaxPagesSent},
		{[]string{"fax received", "faxes received", "fax pages received"}, &metrics.FaxPagesRecv},
	}

	lowerContent := strings.ToLower(content)

	for _, p := range patterns {
		for _, label := range p.labels {
			if idx := strings.Index(lowerContent, label); idx != -1 {
				// Look for a number after the label
				remaining := content[idx:]
				if num := extractFirstNumber(remaining); num > 0 {
					*p.target = num
					break
				}
			}
		}
	}
}

// parseSuppliesHTML extracts supply levels from HP Supplies Status HTML
func (s *HPScraper) parseSuppliesHTML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// Look for supply level patterns
	// Often displayed as percentages or progress bars
	lowerContent := strings.ToLower(content)

	supplyPatterns := []struct {
		labels []string
		target *int
	}{
		{[]string{"black toner", "black cartridge", "black ink"}, &metrics.TonerBlack},
		{[]string{"cyan toner", "cyan cartridge", "cyan ink"}, &metrics.TonerCyan},
		{[]string{"magenta toner", "magenta cartridge", "magenta ink"}, &metrics.TonerMagenta},
		{[]string{"yellow toner", "yellow cartridge", "yellow ink"}, &metrics.TonerYellow},
	}

	for _, p := range supplyPatterns {
		for _, label := range p.labels {
			if idx := strings.Index(lowerContent, label); idx != -1 {
				// Look for percentage after the label
				remaining := content[idx:]
				if pct := extractPercentage(remaining); pct >= 0 {
					*p.target = pct
					break
				}
			}
		}
	}

	return metrics, nil
}

// parseGenericHTML attempts to extract any useful data from HTML
func (s *HPScraper) parseGenericHTML(content string, metrics *USBMetrics) (*USBMetrics, error) {
	// Try to find any page count data
	s.parseUsagePageHTML(content, metrics)
	s.parseSuppliesHTML(content, metrics)
	return metrics, nil
}

// extractXMLInt extracts an integer value from an XML tag
func extractXMLInt(content, tagName string) int {
	// Match any namespace prefix: <prefix:tagName>value</prefix:tagName> or <tagName>value</tagName>
	pattern := regexp.MustCompile(`<\w*:?` + tagName + `[^>]*>(\d+)</\w*:?` + tagName + `>`)
	match := pattern.FindStringSubmatch(content)
	if len(match) >= 2 {
		val, _ := strconv.Atoi(match[1])
		return val
	}
	return 0
}

// extractXMLString extracts a string value from an XML tag
func extractXMLString(content, tagName string) string {
	pattern := regexp.MustCompile(`<\w*:?` + tagName + `[^>]*>([^<]+)</\w*:?` + tagName + `>`)
	match := pattern.FindStringSubmatch(content)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

// extractFirstNumber finds the first number in a string
func extractFirstNumber(s string) int {
	pattern := regexp.MustCompile(`(\d{1,10})`)
	match := pattern.FindStringSubmatch(s)
	if len(match) >= 2 {
		val, _ := strconv.Atoi(match[1])
		return val
	}
	return 0
}

// extractPercentage finds a percentage value (0-100)
func extractPercentage(s string) int {
	// Look for patterns like "75%", "75 %", "75 percent"
	pattern := regexp.MustCompile(`(\d{1,3})\s*(%|percent)`)
	match := pattern.FindStringSubmatch(strings.ToLower(s))
	if len(match) >= 2 {
		val, _ := strconv.Atoi(match[1])
		if val >= 0 && val <= 100 {
			return val
		}
	}
	return -1
}

// extractHPTableValue extracts a numeric value from an HP Usage Page table cell
// HP uses IDs like: id="UsagePage.EquivalentImpressionsTable.Print.Total"
// Values can be formatted with commas like "169,706" or "169,706.0"
func extractHPTableValue(content, idSuffix string) int {
	// Match pattern: id="UsagePage.{idSuffix}"...>value<
	// Value may contain commas and decimal points
	pattern := regexp.MustCompile(`id="UsagePage\.` + regexp.QuoteMeta(idSuffix) + `"[^>]*>([^<]+)<`)
	match := pattern.FindStringSubmatch(content)
	if len(match) >= 2 {
		// Remove commas and decimal points, extract integer
		valueStr := strings.ReplaceAll(match[1], ",", "")
		// Handle decimal like "169706.0" - truncate to integer
		if dotIdx := strings.Index(valueStr, "."); dotIdx != -1 {
			valueStr = valueStr[:dotIdx]
		}
		valueStr = strings.TrimSpace(valueStr)
		val, _ := strconv.Atoi(valueStr)
		return val
	}
	return 0
}

// extractHPStrongValue extracts a string value from an HP <strong> element
// HP uses IDs like: id="UsagePage.DeviceInformation.DeviceSerialNumber"
func extractHPStrongValue(content, idSuffix string) string {
	pattern := regexp.MustCompile(`id="UsagePage\.` + regexp.QuoteMeta(idSuffix) + `"[^>]*>([^<]+)<`)
	match := pattern.FindStringSubmatch(content)
	if len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
