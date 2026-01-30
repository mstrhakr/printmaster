package metrics

import (
	"regexp"
	"strconv"
	"strings"
)

// KyoceraScraper implements vendor-specific web scraping for Kyocera printers.
// Kyocera printers expose their embedded web interface (Command Center) which
// provides device status, page counts, and supply levels via HTML pages.
type KyoceraScraper struct{}

func init() {
	RegisterScraper(&KyoceraScraper{})
}

func (s *KyoceraScraper) Name() string {
	return "Kyocera"
}

func (s *KyoceraScraper) Detect(manufacturer, product string, vendorID uint16) bool {
	// Check USB vendor ID (Kyocera = 0x0482)
	if vendorID == USBVendorKyocera {
		return true
	}

	// Check manufacturer/product strings
	combined := strings.ToLower(manufacturer + " " + product)
	return strings.Contains(combined, "kyocera") ||
		strings.Contains(combined, "taskalfa") ||
		strings.Contains(combined, "ecosys") ||
		strings.Contains(combined, "kyocera mita") ||
		strings.Contains(combined, "triumph-adler") || // Kyocera OEM brand
		strings.Contains(combined, "utax") // Kyocera OEM brand
}

func (s *KyoceraScraper) Endpoints() []EndpointProbe {
	return []EndpointProbe{
		// Kyocera Command Center RX endpoints (newer models)
		{
			Path:        "/startwlm/Start_Wlm.htm",
			ContentType: "html",
			Priority:    1,
			Description: "Kyocera Command Center main page",
		},
		{
			Path:        "/js/jssrc/model/startwlm/Counter.model.htm",
			ContentType: "html",
			Priority:    2,
			Description: "Kyocera Counter model data",
		},
		{
			Path:        "/js/jssrc/model/startwlm/Toner.model.htm",
			ContentType: "html",
			Priority:    3,
			Description: "Kyocera Toner model data",
		},
		{
			Path:        "/js/jssrc/model/dvcinfo/DvcInfo_Top.model.htm",
			ContentType: "html",
			Priority:    4,
			Description: "Kyocera Device Info model data",
		},

		// Direct access endpoints
		{
			Path:        "/wlm/counter/counter",
			ContentType: "html",
			Priority:    5,
			Description: "Kyocera counter page (direct)",
		},
		{
			Path:        "/wlm/system/devinfo",
			ContentType: "html",
			Priority:    6,
			Description: "Kyocera device info page",
		},

		// Status and supplies
		{
			Path:        "/startwlm/index.htm",
			ContentType: "html",
			Priority:    10,
			Description: "Kyocera start page (alt)",
		},
		{
			Path:        "/frame/index.htm",
			ContentType: "html",
			Priority:    11,
			Description: "Kyocera frame page",
		},

		// Classic endpoints for older models
		{
			Path:        "/start/start.htm",
			ContentType: "html",
			Priority:    20,
			Description: "Kyocera classic start page",
		},
		{
			Path:        "/dvcinfo/dvcinfo.htm",
			ContentType: "html",
			Priority:    21,
			Description: "Kyocera classic device info",
		},
		{
			Path:        "/tonerinfo/tonerinfo.htm",
			ContentType: "html",
			Priority:    22,
			Description: "Kyocera classic toner info",
		},
		{
			Path:        "/counter/counter.htm",
			ContentType: "html",
			Priority:    23,
			Description: "Kyocera classic counter page",
		},

		// XML/JSON APIs (some newer models)
		{
			Path:        "/api/getTonerStatus",
			ContentType: "json",
			Priority:    30,
			Description: "Kyocera toner status API",
		},
		{
			Path:        "/api/getDeviceInfo",
			ContentType: "json",
			Priority:    31,
			Description: "Kyocera device info API",
		},
		{
			Path:        "/api/getCounterStatus",
			ContentType: "json",
			Priority:    32,
			Description: "Kyocera counter status API",
		},

		// Generic fallbacks
		{
			Path:        "/",
			ContentType: "html",
			Priority:    100,
			Description: "Root page",
		},
		{
			Path:        "/index.htm",
			ContentType: "html",
			Priority:    101,
			Description: "Index page (htm)",
		},
		{
			Path:        "/index.html",
			ContentType: "html",
			Priority:    102,
			Description: "Index page (html)",
		},
	}
}

func (s *KyoceraScraper) Parse(body []byte, endpoint EndpointProbe) (*USBMetrics, error) {
	metrics := &USBMetrics{
		Source:     endpoint.Path,
		SourceType: endpoint.ContentType,
	}

	content := string(body)

	switch endpoint.ContentType {
	case "json":
		return s.parseJSON(content, endpoint, metrics)
	case "html":
		return s.parseHTML(content, endpoint, metrics)
	}

	return metrics, nil
}

// parseJSON parses Kyocera JSON API responses
func (s *KyoceraScraper) parseJSON(content string, endpoint EndpointProbe, metrics *USBMetrics) (*USBMetrics, error) {
	// Kyocera JSON APIs are relatively simple
	// Look for common field patterns

	if strings.Contains(endpoint.Path, "Counter") || strings.Contains(endpoint.Path, "counter") {
		metrics.TotalPages = extractJSONInt(content, "totalPrint")
		if metrics.TotalPages == 0 {
			metrics.TotalPages = extractJSONInt(content, "total")
		}
		metrics.ColorPages = extractJSONInt(content, "colorPrint")
		metrics.MonoPages = extractJSONInt(content, "monoPrint")
		metrics.CopyPages = extractJSONInt(content, "copyCount")
		metrics.ScanPages = extractJSONInt(content, "scanCount")
	}

	if strings.Contains(endpoint.Path, "Toner") || strings.Contains(endpoint.Path, "toner") {
		metrics.TonerBlack = extractJSONInt(content, "tonerK")
		if metrics.TonerBlack == 0 {
			metrics.TonerBlack = extractJSONInt(content, "tonerBlack")
		}
		metrics.TonerCyan = extractJSONInt(content, "tonerC")
		if metrics.TonerCyan == 0 {
			metrics.TonerCyan = extractJSONInt(content, "tonerCyan")
		}
		metrics.TonerMagenta = extractJSONInt(content, "tonerM")
		if metrics.TonerMagenta == 0 {
			metrics.TonerMagenta = extractJSONInt(content, "tonerMagenta")
		}
		metrics.TonerYellow = extractJSONInt(content, "tonerY")
		if metrics.TonerYellow == 0 {
			metrics.TonerYellow = extractJSONInt(content, "tonerYellow")
		}
	}

	if strings.Contains(endpoint.Path, "Device") || strings.Contains(endpoint.Path, "device") {
		metrics.Model = extractJSONString(content, "modelName")
		if metrics.Model == "" {
			metrics.Model = extractJSONString(content, "productName")
		}
		metrics.Serial = extractJSONString(content, "serialNumber")
	}

	return metrics, nil
}

// parseHTML parses Kyocera HTML pages
func (s *KyoceraScraper) parseHTML(content string, endpoint EndpointProbe, metrics *USBMetrics) (*USBMetrics, error) {
	// Determine what kind of page this is based on path and content
	pathLower := strings.ToLower(endpoint.Path)
	contentLower := strings.ToLower(content)

	// Counter/page count parsing
	if strings.Contains(pathLower, "counter") || strings.Contains(contentLower, "counter") ||
		strings.Contains(contentLower, "page count") || strings.Contains(contentLower, "total print") {
		s.parseCounterHTML(content, metrics)
	}

	// Toner/supply parsing
	if strings.Contains(pathLower, "toner") || strings.Contains(contentLower, "toner") ||
		strings.Contains(contentLower, "supply") || strings.Contains(contentLower, "cartridge") {
		s.parseTonerHTML(content, metrics)
	}

	// Device info parsing
	if strings.Contains(pathLower, "info") || strings.Contains(pathLower, "device") ||
		strings.Contains(contentLower, "serial") || strings.Contains(contentLower, "model") {
		s.parseDeviceInfoHTML(content, metrics)
	}

	// If this is a main/index page, try to extract everything
	if strings.Contains(pathLower, "start") || strings.Contains(pathLower, "index") ||
		pathLower == "/" {
		s.parseCounterHTML(content, metrics)
		s.parseTonerHTML(content, metrics)
		s.parseDeviceInfoHTML(content, metrics)
	}

	return metrics, nil
}

// parseCounterHTML extracts page counts from Kyocera HTML
func (s *KyoceraScraper) parseCounterHTML(content string, metrics *USBMetrics) {
	lowerContent := strings.ToLower(content)

	// Kyocera uses various patterns for page counts
	// Common patterns in Command Center RX:
	// - JavaScript variables: var cntPrint = 12345;
	// - Table cells: <td>Total Print</td><td>12345</td>
	// - Model data: "cntPrint":"12345"

	// Try JavaScript variable patterns first
	if val := extractJSVar(content, "cntPrint"); val > 0 {
		metrics.TotalPages = val
	}
	if val := extractJSVar(content, "cntCopy"); val > 0 {
		metrics.CopyPages = val
	}
	if val := extractJSVar(content, "cntFax"); val > 0 {
		metrics.FaxPagesSent = val
	}
	if val := extractJSVar(content, "cntColor"); val > 0 {
		metrics.ColorPages = val
	}
	if val := extractJSVar(content, "cntMono"); val > 0 {
		metrics.MonoPages = val
	}
	if val := extractJSVar(content, "cntScan"); val > 0 {
		metrics.ScanPages = val
	}

	// Try table-based patterns
	counterPatterns := []struct {
		labels []string
		target *int
	}{
		{[]string{"total print", "print total", "total pages", "life count", "total impressions"}, &metrics.TotalPages},
		{[]string{"color print", "full color", "color pages"}, &metrics.ColorPages},
		{[]string{"mono print", "black print", "b/w print", "monochrome"}, &metrics.MonoPages},
		{[]string{"copy total", "total copy", "copy count"}, &metrics.CopyPages},
		{[]string{"scan total", "total scan", "scan count"}, &metrics.ScanPages},
		{[]string{"fax total", "total fax", "fax sent"}, &metrics.FaxPagesSent},
		{[]string{"fax receive", "fax recv"}, &metrics.FaxPagesRecv},
	}

	for _, p := range counterPatterns {
		if *p.target > 0 {
			continue // Already found
		}
		for _, label := range p.labels {
			if idx := strings.Index(lowerContent, label); idx != -1 {
				// Look for a number after the label (within reasonable distance)
				remaining := content[idx:]
				if len(remaining) > 200 {
					remaining = remaining[:200]
				}
				if num := extractFirstNumber(remaining); num > 0 {
					*p.target = num
					break
				}
			}
		}
	}
}

// parseTonerHTML extracts toner levels from Kyocera HTML
func (s *KyoceraScraper) parseTonerHTML(content string, metrics *USBMetrics) {
	lowerContent := strings.ToLower(content)

	// Try JavaScript variable patterns
	if val := extractJSVar(content, "tonerK"); val >= 0 && val <= 100 {
		metrics.TonerBlack = val
	}
	if val := extractJSVar(content, "tonerC"); val >= 0 && val <= 100 {
		metrics.TonerCyan = val
	}
	if val := extractJSVar(content, "tonerM"); val >= 0 && val <= 100 {
		metrics.TonerMagenta = val
	}
	if val := extractJSVar(content, "tonerY"); val >= 0 && val <= 100 {
		metrics.TonerYellow = val
	}

	// Also try common HTML patterns
	tonerPatterns := []struct {
		labels []string
		target *int
	}{
		{[]string{"black toner", "toner black", "toner k", "black cartridge"}, &metrics.TonerBlack},
		{[]string{"cyan toner", "toner cyan", "toner c", "cyan cartridge"}, &metrics.TonerCyan},
		{[]string{"magenta toner", "toner magenta", "toner m", "magenta cartridge"}, &metrics.TonerMagenta},
		{[]string{"yellow toner", "toner yellow", "toner y", "yellow cartridge"}, &metrics.TonerYellow},
	}

	for _, p := range tonerPatterns {
		if *p.target > 0 {
			continue // Already found
		}
		for _, label := range p.labels {
			if idx := strings.Index(lowerContent, label); idx != -1 {
				// Look for percentage after the label
				remaining := content[idx:]
				if len(remaining) > 100 {
					remaining = remaining[:100]
				}
				if pct := extractPercentage(remaining); pct >= 0 {
					*p.target = pct
					break
				}
			}
		}
	}
}

// parseDeviceInfoHTML extracts device info from Kyocera HTML
func (s *KyoceraScraper) parseDeviceInfoHTML(content string, metrics *USBMetrics) {
	lowerContent := strings.ToLower(content)

	// Try JavaScript variables
	if val := extractJSString(content, "modelName"); val != "" {
		metrics.Model = val
	}
	if val := extractJSString(content, "serialNo"); val != "" {
		metrics.Serial = val
	}

	// Try HTML patterns for serial number
	if metrics.Serial == "" {
		serialPatterns := []string{"serial number", "serial no", "serialnumber"}
		for _, pattern := range serialPatterns {
			if idx := strings.Index(lowerContent, pattern); idx != -1 {
				remaining := content[idx:]
				if len(remaining) > 100 {
					remaining = remaining[:100]
				}
				// Look for alphanumeric string that looks like a serial
				serialPattern := regexp.MustCompile(`[A-Z0-9]{6,20}`)
				if match := serialPattern.FindString(remaining); match != "" {
					metrics.Serial = match
					break
				}
			}
		}
	}

	// Try HTML patterns for model
	if metrics.Model == "" {
		modelPatterns := []string{"model name", "product name", "device name", "ecosys", "taskalfa"}
		for _, pattern := range modelPatterns {
			if idx := strings.Index(lowerContent, pattern); idx != -1 {
				remaining := content[idx:]
				if len(remaining) > 100 {
					remaining = remaining[:100]
				}
				// Look for model pattern (ECOSYS P2040dw, TASKalfa 6052ci, etc.)
				modelPattern := regexp.MustCompile(`(ECOSYS|TASKalfa|FS-|TRIUMPH-ADLER|UTAX)\s*[A-Za-z0-9\-]+`)
				if match := modelPattern.FindString(remaining); match != "" {
					metrics.Model = strings.TrimSpace(match)
					break
				}
			}
		}
	}
}

// extractJSVar extracts an integer value from a JavaScript variable assignment
// Pattern: var varName = 12345; or varName: 12345
func extractJSVar(content, varName string) int {
	patterns := []string{
		`var\s+` + varName + `\s*=\s*(\d+)`,  // var cntPrint = 12345;
		`"` + varName + `"\s*:\s*(\d+)`,      // "cntPrint": 12345
		`'` + varName + `'\s*:\s*(\d+)`,      // 'cntPrint': 12345
		varName + `\s*:\s*(\d+)`,             // cntPrint: 12345
		`data-` + varName + `\s*=\s*"(\d+)"`, // data-cntPrint="12345"
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		if match := re.FindStringSubmatch(content); len(match) >= 2 {
			val, _ := strconv.Atoi(match[1])
			return val
		}
	}
	return 0
}

// extractJSString extracts a string value from a JavaScript variable assignment
func extractJSString(content, varName string) string {
	patterns := []string{
		`var\s+` + varName + `\s*=\s*["']([^"']+)["']`, // var modelName = "ECOSYS P2040dw";
		`"` + varName + `"\s*:\s*"([^"]+)"`,            // "modelName": "ECOSYS P2040dw"
		`'` + varName + `'\s*:\s*'([^']+)'`,            // 'modelName': 'ECOSYS P2040dw'
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		if match := re.FindStringSubmatch(content); len(match) >= 2 {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

// extractJSONInt extracts an integer from JSON-like content
func extractJSONInt(content, key string) int {
	patterns := []string{
		`"` + key + `"\s*:\s*(\d+)`,
		`"` + key + `"\s*:\s*"(\d+)"`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		if match := re.FindStringSubmatch(content); len(match) >= 2 {
			val, _ := strconv.Atoi(match[1])
			return val
		}
	}
	return 0
}

// extractJSONString extracts a string from JSON-like content
func extractJSONString(content, key string) string {
	pattern := regexp.MustCompile(`(?i)"` + key + `"\s*:\s*"([^"]+)"`)
	if match := pattern.FindStringSubmatch(content); len(match) >= 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}
