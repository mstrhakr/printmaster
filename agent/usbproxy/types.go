// Package usbproxy provides HTTP proxy functionality for USB printers.
// It enables browser access to the embedded web interfaces of USB-connected printers
// by tunneling HTTP requests over USB pipes using the IPP-USB protocol.
//
// The IPP-USB specification (IEEE PWG 5100.14-2020) defines how printers expose
// HTTP services over USB interface class 7 (Printer), subclass 1, protocol 4.
//
// This package is inspired by the OpenPrinting/ipp-usb project.
package usbproxy

import (
	"net/http"
	"sync"
	"time"
)

// USBPrinter represents a USB printer with IPP-USB capability
type USBPrinter struct {
	// DevicePath is the OS-specific path to access the device (WinUSB path on Windows)
	DevicePath string `json:"device_path"`

	// VendorID is the USB Vendor ID (e.g., 0x03F0 for HP)
	VendorID uint16 `json:"vendor_id"`

	// ProductID is the USB Product ID
	ProductID uint16 `json:"product_id"`

	// Manufacturer from USB descriptor
	Manufacturer string `json:"manufacturer,omitempty"`

	// Product name from USB descriptor
	Product string `json:"product,omitempty"`

	// SerialNumber from USB descriptor
	SerialNumber string `json:"serial_number,omitempty"`

	// InterfaceNumber is the USB interface that supports IPP-USB
	InterfaceNumber uint8 `json:"interface_number"`

	// SpoolerPortName is the Windows spooler port name (e.g., "USB001") if matched
	SpoolerPortName string `json:"spooler_port_name,omitempty"`

	// Status indicates the current connection status
	Status USBPrinterStatus `json:"status"`

	// LastSeen is when this printer was last detected
	LastSeen time.Time `json:"last_seen"`

	// FirstSeen is when this printer was first discovered
	FirstSeen time.Time `json:"first_seen"`
}

// USBPrinterStatus represents the connection status of a USB printer
type USBPrinterStatus string

const (
	USBPrinterStatusAvailable   USBPrinterStatus = "available"   // Detected and ready for connection
	USBPrinterStatusConnected   USBPrinterStatus = "connected"   // Actively connected/proxying
	USBPrinterStatusUnavailable USBPrinterStatus = "unavailable" // Previously seen but not currently available
	USBPrinterStatusError       USBPrinterStatus = "error"       // Error state
)

// USBInterface represents a USB interface descriptor
type USBInterface struct {
	Number        uint8  `json:"number"`
	AltSetting    uint8  `json:"alt_setting"`
	Class         uint8  `json:"class"`
	SubClass      uint8  `json:"sub_class"`
	Protocol      uint8  `json:"protocol"`
	NumEndpoints  uint8  `json:"num_endpoints"`
	InEndpoint    uint8  `json:"in_endpoint"`  // Bulk IN endpoint
	OutEndpoint   uint8  `json:"out_endpoint"` // Bulk OUT endpoint
	MaxPacketSize uint16 `json:"max_packet_size"`
}

// USB interface class constants
const (
	USBClassPrinter    = 0x07 // Printer class
	USBSubClassPrinter = 0x01 // Printer subclass
	USBProtocolIPPUSB  = 0x04 // IPP over USB (HTTP) protocol
	USBProtocolBiDir   = 0x02 // Bidirectional IEEE 1284.4
	USBProtocolUniDir  = 0x01 // Unidirectional
)

// IsIPPUSBCapable checks if the interface supports IPP-USB (HTTP over USB)
func (i *USBInterface) IsIPPUSBCapable() bool {
	return i.Class == USBClassPrinter &&
		i.SubClass == USBSubClassPrinter &&
		i.Protocol == USBProtocolIPPUSB
}

// IsHTTPCapable checks if the interface might support HTTP
// Some printers use vendor-specific interfaces for HTTP
func (i *USBInterface) IsHTTPCapable() bool {
	// Standard IPP-USB
	if i.IsIPPUSBCapable() {
		return true
	}
	// Some printers use protocol 0xFF (vendor-specific) but still support HTTP
	// We'll try these as fallback
	if i.Class == USBClassPrinter && i.SubClass == USBSubClassPrinter {
		return true
	}
	return false
}

// USBTransport implements http.RoundTripper for USB connections
type USBTransport interface {
	http.RoundTripper
	// Open initializes the USB connection
	Open() error
	// Close releases the USB connection
	Close() error
	// IsOpen returns whether the transport is currently open
	IsOpen() bool
	// DevicePath returns the device path this transport is connected to
	DevicePath() string
}

// USBDeviceEnumerator finds USB printers with IPP-USB capability
type USBDeviceEnumerator interface {
	// Enumerate finds all USB printers that might support IPP-USB
	Enumerate() ([]*USBPrinter, error)
	// GetDeviceDetails retrieves detailed information about a specific device
	GetDeviceDetails(devicePath string) (*USBPrinter, error)
	// CreateTransport creates a transport for the specified device
	CreateTransport(printer *USBPrinter) (USBTransport, error)
}

// ProxySession represents an active proxy session for a USB printer
type ProxySession struct {
	mu           sync.Mutex
	Printer      *USBPrinter
	Transport    USBTransport
	Handler      http.Handler
	StartedAt    time.Time
	LastUsed     time.Time
	RequestCount int64
}

// UpdateLastUsed updates the last used timestamp
func (s *ProxySession) UpdateLastUsed() {
	s.mu.Lock()
	s.LastUsed = time.Now()
	s.RequestCount++
	s.mu.Unlock()
}

// Logger interface for USB proxy operations
type Logger interface {
	Error(msg string, context ...interface{})
	Warn(msg string, context ...interface{})
	Info(msg string, context ...interface{})
	Debug(msg string, context ...interface{})
}

// nullLogger is a no-op logger
type nullLogger struct{}

func (nullLogger) Error(msg string, context ...interface{}) {}
func (nullLogger) Warn(msg string, context ...interface{})  {}
func (nullLogger) Info(msg string, context ...interface{})  {}
func (nullLogger) Debug(msg string, context ...interface{}) {}

// Config holds configuration for the USB proxy manager
type Config struct {
	// IdleTimeout is how long a session can be idle before being closed
	IdleTimeout time.Duration `json:"idle_timeout"`

	// ScanInterval is how often to scan for new USB printers
	ScanInterval time.Duration `json:"scan_interval"`

	// RequestTimeout is the timeout for individual HTTP requests over USB
	RequestTimeout time.Duration `json:"request_timeout"`

	// ReadTimeout is the timeout for USB read operations
	ReadTimeout time.Duration `json:"read_timeout"`

	// WriteTimeout is the timeout for USB write operations
	WriteTimeout time.Duration `json:"write_timeout"`

	// Logger for proxy operations
	Logger Logger `json:"-"`
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		IdleTimeout:    5 * time.Minute,
		ScanInterval:   30 * time.Second,
		RequestTimeout: 30 * time.Second,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		Logger:         nullLogger{},
	}
}

// Well-known USB Vendor IDs for printers
var KnownPrinterVendors = map[uint16]string{
	0x03F0: "HP",
	0x04B8: "Epson",
	0x04A9: "Canon",
	0x04F9: "Brother",
	0x043D: "Lexmark",
	0x0924: "Xerox",
	0x05CA: "Ricoh",
	0x0482: "Kyocera",
	0x132B: "Konica Minolta",
	0x04E8: "Samsung",
	0x06BC: "OKI",
	0x413C: "Dell",
	0x04C5: "Fujitsu",
	0x0409: "NEC",
	0x04DA: "Panasonic",
	0x1A8A: "Sharp",
}

// GetVendorName returns the vendor name for a VID, or "Unknown" if not found
func GetVendorName(vid uint16) string {
	if name, ok := KnownPrinterVendors[vid]; ok {
		return name
	}
	return "Unknown"
}
