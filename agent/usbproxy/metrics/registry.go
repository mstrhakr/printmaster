package metrics

import (
	"strings"
)

// USB Vendor IDs for printer manufacturers
const (
	USBVendorHP      uint16 = 0x03F0
	USBVendorBrother uint16 = 0x04F9
	USBVendorCanon   uint16 = 0x04A9
	USBVendorEpson   uint16 = 0x04B8
	USBVendorKyocera uint16 = 0x0482
	USBVendorLexmark uint16 = 0x043D
	USBVendorRicoh   uint16 = 0x05CA
	USBVendorXerox   uint16 = 0x040A
	USBVendorSamsung uint16 = 0x04E8
)

// vendorScrapers holds registered vendor scraper implementations
var vendorScrapers []VendorScraper

// genericScraper is the fallback when no vendor-specific scraper matches
var genericScraper VendorScraper

// RegisterScraper adds a vendor scraper to the registry
func RegisterScraper(scraper VendorScraper) {
	vendorScrapers = append(vendorScrapers, scraper)
}

// SetGenericScraper sets the fallback scraper used when no vendor matches
func SetGenericScraper(scraper VendorScraper) {
	genericScraper = scraper
}

// DetectVendor finds the appropriate scraper for a device
func DetectVendor(manufacturer, product string, vendorID uint16) VendorScraper {
	// Try each registered scraper
	for _, scraper := range vendorScrapers {
		if scraper.Detect(manufacturer, product, vendorID) {
			return scraper
		}
	}

	// Fall back to generic
	if genericScraper != nil {
		return genericScraper
	}

	// Last resort: return a minimal generic scraper
	return &GenericScraper{}
}

// GetVendorByUSBID returns the vendor name for a USB vendor ID
func GetVendorByUSBID(vendorID uint16) string {
	switch vendorID {
	case USBVendorHP:
		return "HP"
	case USBVendorBrother:
		return "Brother"
	case USBVendorCanon:
		return "Canon"
	case USBVendorEpson:
		return "Epson"
	case USBVendorKyocera:
		return "Kyocera"
	case USBVendorLexmark:
		return "Lexmark"
	case USBVendorRicoh:
		return "Ricoh"
	case USBVendorXerox:
		return "Xerox"
	case USBVendorSamsung:
		return "Samsung"
	default:
		return ""
	}
}

// GenericScraper provides a fallback for unknown vendors
type GenericScraper struct{}

func init() {
	SetGenericScraper(&GenericScraper{})
}

func (s *GenericScraper) Name() string {
	return "Generic"
}

func (s *GenericScraper) Detect(manufacturer, product string, vendorID uint16) bool {
	// Generic always matches as fallback
	return true
}

func (s *GenericScraper) Endpoints() []EndpointProbe {
	// Try common endpoints across multiple vendors
	return []EndpointProbe{
		// XML APIs (machine-readable, preferred)
		{Path: "/DevMgmt/ProductUsageDyn.xml", ContentType: "xml", Priority: 10, Description: "HP/Generic ProductUsage XML"},
		{Path: "/jd/DeviceStatus.xml", ContentType: "xml", Priority: 10, Description: "Generic device status XML"},
		{Path: "/webglue/getDeviceStatus", ContentType: "json", Priority: 10, Description: "Generic device status JSON"},

		// Common HTML pages
		{Path: "/", ContentType: "html", Priority: 100, Description: "Root page"},
		{Path: "/index.html", ContentType: "html", Priority: 100, Description: "Index page"},
		{Path: "/status.html", ContentType: "html", Priority: 50, Description: "Status page"},
		{Path: "/info.html", ContentType: "html", Priority: 50, Description: "Info page"},
	}
}

func (s *GenericScraper) Parse(body []byte, endpoint EndpointProbe) (*USBMetrics, error) {
	metrics := &USBMetrics{
		Source:     endpoint.Path,
		SourceType: endpoint.ContentType,
	}

	// Try to extract any numbers that might be page counts
	// This is a very basic fallback
	content := strings.ToLower(string(body))

	// Look for common patterns
	if strings.Contains(content, "page") || strings.Contains(content, "count") {
		// Generic parsing would go here
		// For now, return empty metrics
	}

	return metrics, nil
}
