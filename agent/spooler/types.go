// Package spooler provides print spooler monitoring for USB/local printer tracking.
// On Windows, it monitors the Windows Print Spooler API.
// On Linux and macOS, it monitors CUPS (Common Unix Printing System).
// This package enables PrintMaster to discover locally-attached printers (USB, LPT, etc.)
// and track print job page counts, even when SNMP metrics are unavailable.
package spooler

import (
	"time"
)

// PrinterType categorizes how a printer is connected
type PrinterType string

const (
	PrinterTypeUSB     PrinterType = "usb"
	PrinterTypeLocal   PrinterType = "local"   // LPT, COM, or other local port
	PrinterTypeNetwork PrinterType = "network" // Network printer shared via spooler
	PrinterTypeVirtual PrinterType = "virtual" // PDF printers, XPS, etc.
	PrinterTypeUnknown PrinterType = "unknown"
)

// LocalPrinter represents a printer discovered through the Windows print spooler
type LocalPrinter struct {
	// Identity
	Name       string `json:"name"`        // Windows printer name (e.g., "HP LaserJet Pro M404")
	PortName   string `json:"port_name"`   // Port (e.g., "USB001", "LPT1:", "192.168.1.100")
	DriverName string `json:"driver_name"` // Driver name

	// Classification
	Type      PrinterType `json:"type"`       // USB, local, network, virtual
	IsDefault bool        `json:"is_default"` // Is this the default printer?
	IsShared  bool        `json:"is_shared"`  // Is this printer shared?

	// Hardware info (if available)
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"` // May be empty for many USB printers

	// Status
	Status      string    `json:"status"`       // "ready", "offline", "error", etc.
	StatusCode  uint32    `json:"status_code"`  // Raw Windows status code
	LastSeen    time.Time `json:"last_seen"`    // Last time we saw this printer
	FirstSeen   time.Time `json:"first_seen"`   // When we first discovered it
	JobCount    int       `json:"job_count"`    // Number of pending jobs
	PrintingJob string    `json:"printing_job"` // Name of currently printing job, if any

	// Page tracking (cumulative from print jobs)
	TotalPages      int64     `json:"total_pages"`       // Total pages printed (tracked from jobs)
	TotalColorPages int64     `json:"total_color_pages"` // Color pages (if detectable)
	TotalMonoPages  int64     `json:"total_mono_pages"`  // Mono pages (if detectable)
	BaselinePages   int64     `json:"baseline_pages"`    // User-set baseline (added to tracked pages)
	LastPageUpdate  time.Time `json:"last_page_update"`  // When page count was last updated
	TrackingEnabled bool      `json:"tracking_enabled"`  // Whether to track this printer
}

// TotalPageCount returns the effective page count (baseline + tracked)
func (p *LocalPrinter) TotalPageCount() int64 {
	return p.BaselinePages + p.TotalPages
}

// PrintJob represents a print job from the spooler
type PrintJob struct {
	JobID        uint32    `json:"job_id"`
	PrinterName  string    `json:"printer_name"`
	DocumentName string    `json:"document_name"`
	UserName     string    `json:"user_name"`
	MachineName  string    `json:"machine_name"`
	Status       string    `json:"status"`      // "printing", "paused", "error", "deleting", etc.
	StatusCode   uint32    `json:"status_code"` // Raw Windows status code
	Priority     uint32    `json:"priority"`
	TotalPages   int32     `json:"total_pages"`   // Total pages in job (-1 if unknown)
	PagesPrinted int32     `json:"pages_printed"` // Pages printed so far
	Size         int64     `json:"size"`          // Job size in bytes
	Submitted    time.Time `json:"submitted"`
	StartTime    time.Time `json:"start_time,omitempty"`
	IsColor      bool      `json:"is_color"` // True if color job (if detectable)
}

// JobEvent represents a change in job state
type JobEvent struct {
	Type        JobEventType `json:"type"`
	Job         *PrintJob    `json:"job"`
	PrinterName string       `json:"printer_name"`
	Timestamp   time.Time    `json:"timestamp"`
	PagesAdded  int32        `json:"pages_added,omitempty"` // For completion events
}

// JobEventType categorizes job state changes
type JobEventType string

const (
	JobEventAdded     JobEventType = "added"     // New job added to queue
	JobEventStarted   JobEventType = "started"   // Job started printing
	JobEventCompleted JobEventType = "completed" // Job finished successfully
	JobEventDeleted   JobEventType = "deleted"   // Job deleted/cancelled
	JobEventError     JobEventType = "error"     // Job encountered an error
	JobEventModified  JobEventType = "modified"  // Job properties changed
)

// WatcherConfig configures the spooler watcher behavior
type WatcherConfig struct {
	// PollInterval is how often to check for printer/job changes (default: 5s)
	PollInterval time.Duration `json:"poll_interval"`

	// IncludeNetworkPrinters includes network printers discovered via spooler
	IncludeNetworkPrinters bool `json:"include_network_printers"`

	// IncludeVirtualPrinters includes virtual printers (PDF, XPS, etc.)
	IncludeVirtualPrinters bool `json:"include_virtual_printers"`

	// AutoTrackUSB automatically enables tracking for new USB printers
	AutoTrackUSB bool `json:"auto_track_usb"`

	// AutoTrackLocal automatically enables tracking for new local (non-USB) printers
	AutoTrackLocal bool `json:"auto_track_local"`
}

// DefaultWatcherConfig returns sensible defaults
func DefaultWatcherConfig() WatcherConfig {
	return WatcherConfig{
		PollInterval:           5 * time.Second,
		IncludeNetworkPrinters: false, // Don't duplicate network printer tracking
		IncludeVirtualPrinters: false, // Skip PDF/XPS printers by default
		AutoTrackUSB:           true,  // Auto-track USB printers
		AutoTrackLocal:         false, // Don't auto-track LPT printers
	}
}

// PrinterFilter allows filtering printers by various criteria
type PrinterFilter struct {
	Type            *PrinterType // Filter by type (nil = all)
	TrackingEnabled *bool        // Filter by tracking status (nil = all)
	Name            string       // Filter by name (contains, case-insensitive)
}

// Logger interface for spooler operations
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
