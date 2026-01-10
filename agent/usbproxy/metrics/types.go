// Package metrics provides USB printer metrics collection via web UI scraping.
// Since SNMP is not available over USB, this package fetches device metrics
// by probing known web UI endpoints and parsing the responses.
package metrics

import (
	"context"
	"io"
	"net/http"
	"time"
)

// USBMetrics contains collected metrics from a USB printer
type USBMetrics struct {
	// Identity
	Serial       string `json:"serial,omitempty"`
	Model        string `json:"model,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`

	// Page counts
	TotalPages int `json:"total_pages,omitempty"`
	ColorPages int `json:"color_pages,omitempty"`
	MonoPages  int `json:"mono_pages,omitempty"`

	// MFP counters
	CopyPages    int `json:"copy_pages,omitempty"`
	ScanPages    int `json:"scan_pages,omitempty"`
	FaxPagesSent int `json:"fax_pages_sent,omitempty"`
	FaxPagesRecv int `json:"fax_pages_recv,omitempty"`

	// Supplies (0-100 percent, -1 if unknown)
	TonerBlack   int `json:"toner_black,omitempty"`
	TonerCyan    int `json:"toner_cyan,omitempty"`
	TonerMagenta int `json:"toner_magenta,omitempty"`
	TonerYellow  int `json:"toner_yellow,omitempty"`

	// Drum/imaging units (0-100 percent)
	DrumBlack   int `json:"drum_black,omitempty"`
	DrumCyan    int `json:"drum_cyan,omitempty"`
	DrumMagenta int `json:"drum_magenta,omitempty"`
	DrumYellow  int `json:"drum_yellow,omitempty"`

	// Maintenance
	FuserLife  int `json:"fuser_life,omitempty"`
	WasteToner int `json:"waste_toner,omitempty"`

	// Status
	Status       string `json:"status,omitempty"`
	StatusCode   int    `json:"status_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	// Collection metadata
	CollectedAt time.Time `json:"collected_at"`
	Source      string    `json:"source,omitempty"`      // URL that provided the data
	SourceType  string    `json:"source_type,omitempty"` // "html", "xml", "json"
	Vendor      string    `json:"vendor,omitempty"`      // Detected vendor module
}

// EndpointProbe defines a URL endpoint to probe for metrics
type EndpointProbe struct {
	// Path is the URL path to probe (e.g., "/hp/device/InternalPages/Index?id=UsagePage")
	Path string

	// ContentType expected ("html", "xml", "json")
	ContentType string

	// Priority determines probe order (lower = higher priority)
	Priority int

	// Description for logging/debugging
	Description string
}

// VendorScraper defines the interface for vendor-specific web scraping
type VendorScraper interface {
	// Name returns the vendor identifier (e.g., "HP", "Brother")
	Name() string

	// Detect returns true if this scraper should be used for the device
	// based on manufacturer string, product name, or USB vendor ID
	Detect(manufacturer, product string, vendorID uint16) bool

	// Endpoints returns the list of endpoints to probe, in priority order
	Endpoints() []EndpointProbe

	// Parse extracts metrics from an HTTP response body
	Parse(body []byte, endpoint EndpointProbe) (*USBMetrics, error)
}

// Transport provides HTTP request capability for USB printers
type Transport interface {
	// RoundTrip sends an HTTP request and returns the response
	RoundTrip(*http.Request) (*http.Response, error)
}

// Collector handles metrics collection from USB printers
type Collector struct {
	// Transport is the HTTP transport (USB proxy)
	Transport Transport

	// Vendor is the detected vendor scraper
	Vendor VendorScraper

	// Timeout for HTTP requests
	Timeout time.Duration

	// Logger for debug output (optional)
	Logger Logger
}

// Logger interface for optional logging
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// nullLogger discards all log messages
type nullLogger struct{}

func (nullLogger) Debug(msg string, args ...interface{}) {}
func (nullLogger) Info(msg string, args ...interface{})  {}
func (nullLogger) Warn(msg string, args ...interface{})  {}
func (nullLogger) Error(msg string, args ...interface{}) {}

// NewCollector creates a new metrics collector for a USB printer
func NewCollector(transport Transport, manufacturer, product string, vendorID uint16) *Collector {
	vendor := DetectVendor(manufacturer, product, vendorID)
	return &Collector{
		Transport: transport,
		Vendor:    vendor,
		Timeout:   30 * time.Second, // USB proxy is slow - pages can take 5-10+ seconds
		Logger:    nullLogger{},
	}
}

// Collect fetches metrics from the USB printer by probing known endpoints
// USB is slow, so this may take 30+ seconds to complete
func (c *Collector) Collect(ctx context.Context) (*USBMetrics, error) {
	endpoints := c.Vendor.Endpoints()

	// Only try a few high-priority endpoints - each takes 5-10 seconds via USB
	maxEndpoints := 3
	if len(endpoints) > maxEndpoints {
		endpoints = endpoints[:maxEndpoints]
	}

	for _, ep := range endpoints {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		c.Logger.Debug("Probing endpoint",
			"vendor", c.Vendor.Name(),
			"path", ep.Path,
			"type", ep.ContentType)

		metrics, err := c.probeEndpoint(ctx, ep)
		if err != nil {
			c.Logger.Debug("Endpoint probe failed",
				"path", ep.Path,
				"error", err)
			continue
		}

		if metrics != nil && metrics.TotalPages > 0 {
			metrics.Vendor = c.Vendor.Name()
			metrics.CollectedAt = time.Now()
			c.Logger.Info("Collected USB metrics",
				"vendor", c.Vendor.Name(),
				"source", ep.Path,
				"total_pages", metrics.TotalPages)
			return metrics, nil
		}
	}

	// Return empty metrics if nothing found
	return &USBMetrics{
		Vendor:      c.Vendor.Name(),
		CollectedAt: time.Now(),
	}, nil
}

// probeEndpoint fetches and parses a single endpoint
func (c *Collector) probeEndpoint(ctx context.Context, ep EndpointProbe) (*USBMetrics, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "http://usb-printer"+ep.Path, nil)
	if err != nil {
		return nil, err
	}

	// Set appropriate Accept header
	switch ep.ContentType {
	case "xml":
		req.Header.Set("Accept", "application/xml, text/xml")
	case "json":
		req.Header.Set("Accept", "application/json")
	default:
		req.Header.Set("Accept", "text/html, */*")
	}

	resp, err := c.Transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // Not found, try next endpoint
	}

	// Read body with size limit (512KB)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}

	return c.Vendor.Parse(body, ep)
}
