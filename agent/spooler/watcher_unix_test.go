//go:build linux || darwin
// +build linux darwin

package spooler

import (
	"testing"
	"time"
)

// =============================================================================
// classifyPrinterFromURI Tests (pure function, testable without CUPS)
// =============================================================================

func TestClassifyPrinterFromURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uri      string
		expected PrinterType
	}{
		// USB printers
		{"USB scheme", "usb://HP/LaserJet%20Pro?serial=ABC123", PrinterTypeUSB},
		{"USB colon scheme", "usb:HP_LaserJet", PrinterTypeUSB},
		{"USB uppercase", "USB://Brother/MFC-7460DN", PrinterTypeUSB},

		// Local printers
		{"Parallel port", "parallel://dev/lp0", PrinterTypeLocal},
		{"Serial port", "serial://dev/ttyUSB0", PrinterTypeLocal},
		{"Device path", "/dev/usb/lp0", PrinterTypeLocal},

		// Network printers
		{"IPP", "ipp://192.168.1.100/ipp/print", PrinterTypeNetwork},
		{"IPPS", "ipps://print.example.com:631/printers/color", PrinterTypeNetwork},
		{"HTTP", "http://192.168.1.100:9100", PrinterTypeNetwork},
		{"HTTPS", "https://print.example.com/printer", PrinterTypeNetwork},
		{"Socket", "socket://192.168.1.100:9100", PrinterTypeNetwork},
		{"LPD", "lpd://printserver/queue", PrinterTypeNetwork},
		{"SMB", "smb://server/printer", PrinterTypeNetwork},

		// Virtual printers
		{"CUPS-PDF", "cups-pdf://", PrinterTypeVirtual},
		{"File", "file:///tmp/output.ps", PrinterTypeVirtual},
		{"Pipe", "pipe:///usr/bin/lpr", PrinterTypeVirtual},
		{"PDF in name", "backend://pdf-printer", PrinterTypeVirtual},

		// Unknown
		{"Empty", "", PrinterTypeUnknown},
		{"Unknown scheme", "foo://bar", PrinterTypeUnknown},
		{"Random string", "some-random-string", PrinterTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := classifyPrinterFromURI(tt.uri)
			if result != tt.expected {
				t.Errorf("classifyPrinterFromURI(%q) = %v, want %v", tt.uri, result, tt.expected)
			}
		})
	}
}

func TestClassifyPrinterFromURI_CaseInsensitive(t *testing.T) {
	t.Parallel()

	// Verify case insensitivity
	tests := []struct {
		uri      string
		expected PrinterType
	}{
		{"USB://HP/Printer", PrinterTypeUSB},
		{"usb://hp/printer", PrinterTypeUSB},
		{"IPP://PRINTER/QUEUE", PrinterTypeNetwork},
		{"ipp://printer/queue", PrinterTypeNetwork},
	}

	for _, tt := range tests {
		result := classifyPrinterFromURI(tt.uri)
		if result != tt.expected {
			t.Errorf("classifyPrinterFromURI(%q) = %v, want %v (case insensitive)", tt.uri, result, tt.expected)
		}
	}
}

// =============================================================================
// NewWatcher Tests
// =============================================================================

func TestNewWatcher_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}

	// Verify config was applied
	if w.config.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", w.config.PollInterval)
	}
}

func TestNewWatcher_WithCustomConfig_Unix(t *testing.T) {
	t.Parallel()

	cfg := WatcherConfig{
		PollInterval:           10 * time.Second,
		IncludeNetworkPrinters: true,
		IncludeVirtualPrinters: true,
		AutoTrackUSB:           false,
		AutoTrackLocal:         true,
	}
	w := NewWatcher(cfg, nil)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}

	if w.config.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want 10s", w.config.PollInterval)
	}
	if !w.config.IncludeNetworkPrinters {
		t.Error("IncludeNetworkPrinters should be true")
	}
	if !w.config.IncludeVirtualPrinters {
		t.Error("IncludeVirtualPrinters should be true")
	}
	if w.config.AutoTrackUSB {
		t.Error("AutoTrackUSB should be false")
	}
	if !w.config.AutoTrackLocal {
		t.Error("AutoTrackLocal should be true")
	}
}

func TestNewWatcher_ZeroPollInterval_Unix(t *testing.T) {
	t.Parallel()

	cfg := WatcherConfig{
		PollInterval: 0, // Zero should be replaced with default
	}
	w := NewWatcher(cfg, nil)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}

	// Zero poll interval should default to 5 seconds
	if w.config.PollInterval != 5*time.Second {
		t.Errorf("Zero PollInterval should default to 5s, got %v", w.config.PollInterval)
	}
}

func TestNewWatcher_NegativePollInterval_Unix(t *testing.T) {
	t.Parallel()

	cfg := WatcherConfig{
		PollInterval: -1 * time.Second, // Negative should be replaced with default
	}
	w := NewWatcher(cfg, nil)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}

	// Negative poll interval should default to 5 seconds
	if w.config.PollInterval != 5*time.Second {
		t.Errorf("Negative PollInterval should default to 5s, got %v", w.config.PollInterval)
	}
}

func TestNewWatcher_WithLogger_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	log := &testLogger{}
	w := NewWatcher(cfg, log)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
	if w.logger != log {
		t.Error("Logger was not set correctly")
	}
}

func TestNewWatcher_InitializesInternalMaps_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	if w.printers == nil {
		t.Error("printers map should be initialized")
	}
	if w.jobs == nil {
		t.Error("jobs map should be initialized")
	}
	if w.seenJobs == nil {
		t.Error("seenJobs map should be initialized")
	}
	if w.events == nil {
		t.Error("events channel should be initialized")
	}
}

// =============================================================================
// Watcher Methods (without starting - safe to test)
// =============================================================================

func TestWatcher_GetPrinters_Empty_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	printers := w.GetPrinters()
	if len(printers) != 0 {
		t.Errorf("GetPrinters() returned %d printers, want 0", len(printers))
	}
}

func TestWatcher_GetPrinter_NotFound_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	printer, found := w.GetPrinter("NonExistent")
	if found {
		t.Error("GetPrinter() found non-existent printer")
	}
	if printer != nil {
		t.Error("GetPrinter() returned non-nil for non-existent printer")
	}
}

func TestWatcher_GetJobs_NoPrinter_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	jobs := w.GetJobs("NonExistent")
	if jobs != nil {
		t.Error("GetJobs() should return nil for non-existent printer")
	}
}

func TestWatcher_SetBaseline_NoPrinter_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	success := w.SetBaseline("NonExistent", 1000)
	if success {
		t.Error("SetBaseline() should return false for non-existent printer")
	}
}

func TestWatcher_SetTracking_NoPrinter_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	success := w.SetTracking("NonExistent", true)
	if success {
		t.Error("SetTracking() should return false for non-existent printer")
	}
}

func TestWatcher_Events_ReturnsChannel_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	ch := w.Events()
	if ch == nil {
		t.Error("Events() should return non-nil channel")
	}
}

// =============================================================================
// Watcher with Manually Added Printers (for testing)
// =============================================================================

func TestWatcher_GetPrinters_WithData_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	// Manually add a printer for testing
	w.printers["TestPrinter"] = &LocalPrinter{
		Name:   "TestPrinter",
		Type:   PrinterTypeUSB,
		Status: "ready",
	}

	printers := w.GetPrinters()
	if len(printers) != 1 {
		t.Errorf("GetPrinters() returned %d printers, want 1", len(printers))
	}
	if printers[0].Name != "TestPrinter" {
		t.Errorf("Printer name = %q, want %q", printers[0].Name, "TestPrinter")
	}
}

func TestWatcher_GetPrinters_ReturnsCopy_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{
		Name:   "TestPrinter",
		Status: "ready",
	}

	printers := w.GetPrinters()
	// Modify the returned printer
	printers[0].Status = "offline"

	// Original should be unchanged
	if w.printers["TestPrinter"].Status != "ready" {
		t.Error("GetPrinters() should return copies, not references")
	}
}

func TestWatcher_GetPrinter_Found_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{
		Name:            "TestPrinter",
		Type:            PrinterTypeUSB,
		Status:          "ready",
		TrackingEnabled: true,
	}

	printer, found := w.GetPrinter("TestPrinter")
	if !found {
		t.Error("GetPrinter() should find existing printer")
	}
	if printer == nil {
		t.Fatal("GetPrinter() returned nil for existing printer")
	}
	if printer.Name != "TestPrinter" {
		t.Errorf("Printer name = %q, want %q", printer.Name, "TestPrinter")
	}
	if printer.Type != PrinterTypeUSB {
		t.Errorf("Printer type = %v, want %v", printer.Type, PrinterTypeUSB)
	}
}

func TestWatcher_GetPrinter_ReturnsCopy_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{
		Name:   "TestPrinter",
		Status: "ready",
	}

	printer, _ := w.GetPrinter("TestPrinter")
	// Modify the returned printer
	printer.Status = "offline"

	// Original should be unchanged
	if w.printers["TestPrinter"].Status != "ready" {
		t.Error("GetPrinter() should return a copy, not a reference")
	}
}

func TestWatcher_SetBaseline_Success_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{
		Name:          "TestPrinter",
		BaselinePages: 0,
	}

	success := w.SetBaseline("TestPrinter", 5000)
	if !success {
		t.Error("SetBaseline() should return true for existing printer")
	}
	if w.printers["TestPrinter"].BaselinePages != 5000 {
		t.Errorf("BaselinePages = %d, want 5000", w.printers["TestPrinter"].BaselinePages)
	}
}

func TestWatcher_SetTracking_Success_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{
		Name:            "TestPrinter",
		TrackingEnabled: false,
	}

	success := w.SetTracking("TestPrinter", true)
	if !success {
		t.Error("SetTracking() should return true for existing printer")
	}
	if !w.printers["TestPrinter"].TrackingEnabled {
		t.Error("TrackingEnabled should be true")
	}

	// Disable tracking
	success = w.SetTracking("TestPrinter", false)
	if !success {
		t.Error("SetTracking() should return true")
	}
	if w.printers["TestPrinter"].TrackingEnabled {
		t.Error("TrackingEnabled should be false")
	}
}

func TestWatcher_GetJobs_WithData_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	// Set up printer and jobs
	w.printers["TestPrinter"] = &LocalPrinter{Name: "TestPrinter"}
	w.jobs["TestPrinter"] = map[uint32]*PrintJob{
		1: {JobID: 1, PrinterName: "TestPrinter", DocumentName: "Doc1.pdf"},
		2: {JobID: 2, PrinterName: "TestPrinter", DocumentName: "Doc2.pdf"},
	}

	jobs := w.GetJobs("TestPrinter")
	if len(jobs) != 2 {
		t.Errorf("GetJobs() returned %d jobs, want 2", len(jobs))
	}
}

func TestWatcher_GetJobs_ReturnsCopy_Unix(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	w.printers["TestPrinter"] = &LocalPrinter{Name: "TestPrinter"}
	w.jobs["TestPrinter"] = map[uint32]*PrintJob{
		1: {JobID: 1, Status: "queued"},
	}

	jobs := w.GetJobs("TestPrinter")
	// Modify returned job
	jobs[0].Status = "printing"

	// Original should be unchanged
	if w.jobs["TestPrinter"][1].Status != "queued" {
		t.Error("GetJobs() should return copies, not references")
	}
}

// =============================================================================
// IsSupported Tests
// =============================================================================

func TestIsSupported_Unix(t *testing.T) {
	t.Parallel()

	// On Unix systems, this depends on whether CUPS (lpstat) is installed
	// We can't guarantee the result, but we can verify it doesn't panic
	result := IsSupported()
	t.Logf("IsSupported() = %v (depends on CUPS availability)", result)
}

// =============================================================================
// Test Logger Helper
// =============================================================================

type testLogger struct {
	errorCount int
	warnCount  int
	infoCount  int
	debugCount int
}

func (l *testLogger) Error(msg string, context ...interface{}) { l.errorCount++ }
func (l *testLogger) Warn(msg string, context ...interface{})  { l.warnCount++ }
func (l *testLogger) Info(msg string, context ...interface{})  { l.infoCount++ }
func (l *testLogger) Debug(msg string, context ...interface{}) { l.debugCount++ }
