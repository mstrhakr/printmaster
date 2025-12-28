package spooler

import (
	"testing"
	"time"
)

// =============================================================================
// PrinterType Tests
// =============================================================================

func TestPrinterType_Constants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pt       PrinterType
		expected string
	}{
		{"USB", PrinterTypeUSB, "usb"},
		{"Local", PrinterTypeLocal, "local"},
		{"Network", PrinterTypeNetwork, "network"},
		{"Virtual", PrinterTypeVirtual, "virtual"},
		{"Unknown", PrinterTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.pt) != tt.expected {
				t.Errorf("PrinterType%s = %q, want %q", tt.name, tt.pt, tt.expected)
			}
		})
	}
}

func TestPrinterType_CanBeUsedAsString(t *testing.T) {
	t.Parallel()

	// Verify PrinterType can be cast and compared as string
	pt := PrinterTypeUSB
	if string(pt) != "usb" {
		t.Errorf("PrinterType string conversion failed")
	}

	// Can compare directly
	if pt != "usb" {
		t.Errorf("PrinterType direct comparison failed")
	}
}

// =============================================================================
// JobEventType Tests
// =============================================================================

func TestJobEventType_Constants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		jet      JobEventType
		expected string
	}{
		{"Added", JobEventAdded, "added"},
		{"Started", JobEventStarted, "started"},
		{"Completed", JobEventCompleted, "completed"},
		{"Deleted", JobEventDeleted, "deleted"},
		{"Error", JobEventError, "error"},
		{"Modified", JobEventModified, "modified"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.jet) != tt.expected {
				t.Errorf("JobEventType%s = %q, want %q", tt.name, tt.jet, tt.expected)
			}
		})
	}
}

// =============================================================================
// LocalPrinter Tests
// =============================================================================

func TestLocalPrinter_TotalPageCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		baselinePages int64
		totalPages    int64
		expected      int64
	}{
		{
			name:          "ZeroBaseline_ZeroTotal",
			baselinePages: 0,
			totalPages:    0,
			expected:      0,
		},
		{
			name:          "ZeroBaseline_WithTotal",
			baselinePages: 0,
			totalPages:    100,
			expected:      100,
		},
		{
			name:          "WithBaseline_ZeroTotal",
			baselinePages: 5000,
			totalPages:    0,
			expected:      5000,
		},
		{
			name:          "BothNonZero",
			baselinePages: 10000,
			totalPages:    250,
			expected:      10250,
		},
		{
			name:          "LargeValues",
			baselinePages: 1000000,
			totalPages:    500000,
			expected:      1500000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &LocalPrinter{
				BaselinePages: tt.baselinePages,
				TotalPages:    tt.totalPages,
			}

			result := p.TotalPageCount()
			if result != tt.expected {
				t.Errorf("TotalPageCount() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestLocalPrinter_Fields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	earlier := now.Add(-time.Hour)

	p := LocalPrinter{
		Name:            "HP LaserJet Pro M404",
		PortName:        "USB001",
		DriverName:      "HP Universal Print Driver",
		Type:            PrinterTypeUSB,
		IsDefault:       true,
		IsShared:        false,
		Manufacturer:    "HP",
		Model:           "LaserJet Pro M404",
		SerialNumber:    "ABC123XYZ",
		Status:          "ready",
		StatusCode:      0,
		LastSeen:        now,
		FirstSeen:       earlier,
		JobCount:        2,
		PrintingJob:     "Document.pdf",
		TotalPages:      1500,
		TotalColorPages: 300,
		TotalMonoPages:  1200,
		BaselinePages:   5000,
		LastPageUpdate:  now,
		TrackingEnabled: true,
	}

	// Verify all fields are set correctly
	if p.Name != "HP LaserJet Pro M404" {
		t.Errorf("Name = %q, want %q", p.Name, "HP LaserJet Pro M404")
	}
	if p.PortName != "USB001" {
		t.Errorf("PortName = %q, want %q", p.PortName, "USB001")
	}
	if p.DriverName != "HP Universal Print Driver" {
		t.Errorf("DriverName = %q, want %q", p.DriverName, "HP Universal Print Driver")
	}
	if p.Type != PrinterTypeUSB {
		t.Errorf("Type = %v, want %v", p.Type, PrinterTypeUSB)
	}
	if !p.IsDefault {
		t.Error("IsDefault should be true")
	}
	if p.IsShared {
		t.Error("IsShared should be false")
	}
	if p.Manufacturer != "HP" {
		t.Errorf("Manufacturer = %q, want %q", p.Manufacturer, "HP")
	}
	if p.Model != "LaserJet Pro M404" {
		t.Errorf("Model = %q, want %q", p.Model, "LaserJet Pro M404")
	}
	if p.SerialNumber != "ABC123XYZ" {
		t.Errorf("SerialNumber = %q, want %q", p.SerialNumber, "ABC123XYZ")
	}
	if p.Status != "ready" {
		t.Errorf("Status = %q, want %q", p.Status, "ready")
	}
	if p.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", p.StatusCode)
	}
	if p.JobCount != 2 {
		t.Errorf("JobCount = %d, want 2", p.JobCount)
	}
	if p.PrintingJob != "Document.pdf" {
		t.Errorf("PrintingJob = %q, want %q", p.PrintingJob, "Document.pdf")
	}
	if p.TotalPages != 1500 {
		t.Errorf("TotalPages = %d, want 1500", p.TotalPages)
	}
	if p.TotalColorPages != 300 {
		t.Errorf("TotalColorPages = %d, want 300", p.TotalColorPages)
	}
	if p.TotalMonoPages != 1200 {
		t.Errorf("TotalMonoPages = %d, want 1200", p.TotalMonoPages)
	}
	if p.BaselinePages != 5000 {
		t.Errorf("BaselinePages = %d, want 5000", p.BaselinePages)
	}
	if !p.TrackingEnabled {
		t.Error("TrackingEnabled should be true")
	}

	// TotalPageCount should be BaselinePages + TotalPages
	if p.TotalPageCount() != 6500 {
		t.Errorf("TotalPageCount() = %d, want 6500", p.TotalPageCount())
	}
}

func TestLocalPrinter_ZeroValue(t *testing.T) {
	t.Parallel()

	var p LocalPrinter

	// Zero value should work correctly
	if p.TotalPageCount() != 0 {
		t.Errorf("TotalPageCount() on zero value = %d, want 0", p.TotalPageCount())
	}
	if p.Name != "" {
		t.Errorf("Zero value Name = %q, want empty", p.Name)
	}
	if p.Type != "" {
		t.Errorf("Zero value Type = %v, want empty", p.Type)
	}
}

// =============================================================================
// PrintJob Tests
// =============================================================================

func TestPrintJob_Fields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	submitted := now.Add(-5 * time.Minute)

	job := PrintJob{
		JobID:        12345,
		PrinterName:  "HP LaserJet",
		DocumentName: "Report.pdf",
		UserName:     "jsmith",
		MachineName:  "WORKSTATION01",
		Status:       "printing",
		StatusCode:   1,
		Priority:     1,
		TotalPages:   10,
		PagesPrinted: 5,
		Size:         1024000,
		Submitted:    submitted,
		StartTime:    now,
		IsColor:      true,
	}

	if job.JobID != 12345 {
		t.Errorf("JobID = %d, want 12345", job.JobID)
	}
	if job.PrinterName != "HP LaserJet" {
		t.Errorf("PrinterName = %q, want %q", job.PrinterName, "HP LaserJet")
	}
	if job.DocumentName != "Report.pdf" {
		t.Errorf("DocumentName = %q, want %q", job.DocumentName, "Report.pdf")
	}
	if job.UserName != "jsmith" {
		t.Errorf("UserName = %q, want %q", job.UserName, "jsmith")
	}
	if job.MachineName != "WORKSTATION01" {
		t.Errorf("MachineName = %q, want %q", job.MachineName, "WORKSTATION01")
	}
	if job.Status != "printing" {
		t.Errorf("Status = %q, want %q", job.Status, "printing")
	}
	if job.StatusCode != 1 {
		t.Errorf("StatusCode = %d, want 1", job.StatusCode)
	}
	if job.Priority != 1 {
		t.Errorf("Priority = %d, want 1", job.Priority)
	}
	if job.TotalPages != 10 {
		t.Errorf("TotalPages = %d, want 10", job.TotalPages)
	}
	if job.PagesPrinted != 5 {
		t.Errorf("PagesPrinted = %d, want 5", job.PagesPrinted)
	}
	if job.Size != 1024000 {
		t.Errorf("Size = %d, want 1024000", job.Size)
	}
	if !job.IsColor {
		t.Error("IsColor should be true")
	}
}

func TestPrintJob_UnknownPageCount(t *testing.T) {
	t.Parallel()

	// -1 indicates unknown page count
	job := PrintJob{
		TotalPages: -1,
	}

	if job.TotalPages != -1 {
		t.Errorf("TotalPages = %d, want -1 (unknown)", job.TotalPages)
	}
}

// =============================================================================
// JobEvent Tests
// =============================================================================

func TestJobEvent_Fields(t *testing.T) {
	t.Parallel()

	now := time.Now()
	job := &PrintJob{
		JobID:       100,
		PrinterName: "TestPrinter",
		TotalPages:  5,
	}

	event := JobEvent{
		Type:        JobEventCompleted,
		Job:         job,
		PrinterName: "TestPrinter",
		Timestamp:   now,
		PagesAdded:  5,
	}

	if event.Type != JobEventCompleted {
		t.Errorf("Type = %v, want %v", event.Type, JobEventCompleted)
	}
	if event.Job != job {
		t.Error("Job pointer mismatch")
	}
	if event.PrinterName != "TestPrinter" {
		t.Errorf("PrinterName = %q, want %q", event.PrinterName, "TestPrinter")
	}
	if event.PagesAdded != 5 {
		t.Errorf("PagesAdded = %d, want 5", event.PagesAdded)
	}
}

func TestJobEvent_NilJob(t *testing.T) {
	t.Parallel()

	// Events can have nil job (e.g., for delete events where job info is minimal)
	event := JobEvent{
		Type:        JobEventDeleted,
		Job:         nil,
		PrinterName: "TestPrinter",
		Timestamp:   time.Now(),
	}

	if event.Job != nil {
		t.Error("Job should be nil")
	}
}

// =============================================================================
// WatcherConfig Tests
// =============================================================================

func TestDefaultWatcherConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()

	// Test all default values
	if cfg.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want 5s", cfg.PollInterval)
	}
	if cfg.IncludeNetworkPrinters {
		t.Error("IncludeNetworkPrinters should be false by default")
	}
	if cfg.IncludeVirtualPrinters {
		t.Error("IncludeVirtualPrinters should be false by default")
	}
	if !cfg.AutoTrackUSB {
		t.Error("AutoTrackUSB should be true by default")
	}
	if cfg.AutoTrackLocal {
		t.Error("AutoTrackLocal should be false by default")
	}
}

func TestWatcherConfig_CustomValues(t *testing.T) {
	t.Parallel()

	cfg := WatcherConfig{
		PollInterval:           10 * time.Second,
		IncludeNetworkPrinters: true,
		IncludeVirtualPrinters: true,
		AutoTrackUSB:           false,
		AutoTrackLocal:         true,
	}

	if cfg.PollInterval != 10*time.Second {
		t.Errorf("PollInterval = %v, want 10s", cfg.PollInterval)
	}
	if !cfg.IncludeNetworkPrinters {
		t.Error("IncludeNetworkPrinters should be true")
	}
	if !cfg.IncludeVirtualPrinters {
		t.Error("IncludeVirtualPrinters should be true")
	}
	if cfg.AutoTrackUSB {
		t.Error("AutoTrackUSB should be false")
	}
	if !cfg.AutoTrackLocal {
		t.Error("AutoTrackLocal should be true")
	}
}

func TestWatcherConfig_ZeroValue(t *testing.T) {
	t.Parallel()

	var cfg WatcherConfig

	// Zero value has different behavior than default
	if cfg.PollInterval != 0 {
		t.Errorf("Zero value PollInterval = %v, want 0", cfg.PollInterval)
	}
	if cfg.AutoTrackUSB {
		t.Error("Zero value AutoTrackUSB should be false")
	}
}

// =============================================================================
// PrinterFilter Tests
// =============================================================================

func TestPrinterFilter_NilValues(t *testing.T) {
	t.Parallel()

	// Nil filter values mean "match all"
	filter := PrinterFilter{
		Type:            nil,
		TrackingEnabled: nil,
		Name:            "",
	}

	if filter.Type != nil {
		t.Error("Type should be nil")
	}
	if filter.TrackingEnabled != nil {
		t.Error("TrackingEnabled should be nil")
	}
}

func TestPrinterFilter_WithValues(t *testing.T) {
	t.Parallel()

	usbType := PrinterTypeUSB
	enabled := true

	filter := PrinterFilter{
		Type:            &usbType,
		TrackingEnabled: &enabled,
		Name:            "HP",
	}

	if filter.Type == nil || *filter.Type != PrinterTypeUSB {
		t.Errorf("Type = %v, want %v", filter.Type, PrinterTypeUSB)
	}
	if filter.TrackingEnabled == nil || !*filter.TrackingEnabled {
		t.Error("TrackingEnabled should be true")
	}
	if filter.Name != "HP" {
		t.Errorf("Name = %q, want %q", filter.Name, "HP")
	}
}

// =============================================================================
// Logger Interface Tests
// =============================================================================

func TestNullLogger_NoOp(t *testing.T) {
	t.Parallel()

	// nullLogger should not panic when called
	var log nullLogger

	// These should all be no-ops (just verify they don't panic)
	log.Error("error message", "key", "value")
	log.Warn("warn message", "key", "value")
	log.Info("info message", "key", "value")
	log.Debug("debug message", "key", "value")

	// Call with no context
	log.Error("error message")
	log.Warn("warn message")
	log.Info("info message")
	log.Debug("debug message")

	// Call with multiple context pairs
	log.Info("message", "k1", "v1", "k2", "v2", "k3", "v3")
}

func TestNullLogger_ImplementsLogger(t *testing.T) {
	t.Parallel()

	// Verify nullLogger implements Logger interface
	var _ Logger = nullLogger{}
	var _ Logger = &nullLogger{}
}

// =============================================================================
// Integration-style Tests
// =============================================================================

func TestLocalPrinter_SimulatePageTracking(t *testing.T) {
	t.Parallel()

	// Simulate a printer being discovered and tracking pages over time
	p := &LocalPrinter{
		Name:            "USB Printer",
		Type:            PrinterTypeUSB,
		TrackingEnabled: true,
		BaselinePages:   10000, // User set baseline from device counter
		TotalPages:      0,     // Start tracking from 0
	}

	// Initial state
	if p.TotalPageCount() != 10000 {
		t.Errorf("Initial TotalPageCount() = %d, want 10000", p.TotalPageCount())
	}

	// Simulate job completion adding 15 pages
	p.TotalPages += 15
	if p.TotalPageCount() != 10015 {
		t.Errorf("After 15 pages, TotalPageCount() = %d, want 10015", p.TotalPageCount())
	}

	// Simulate another job completion adding 5 pages
	p.TotalPages += 5
	if p.TotalPageCount() != 10020 {
		t.Errorf("After 20 total pages, TotalPageCount() = %d, want 10020", p.TotalPageCount())
	}

	// User adjusts baseline
	p.BaselinePages = 9500
	if p.TotalPageCount() != 9520 {
		t.Errorf("After baseline adjustment, TotalPageCount() = %d, want 9520", p.TotalPageCount())
	}
}

func TestPrintJob_SimulateJobProgress(t *testing.T) {
	t.Parallel()

	// Simulate a print job progressing
	job := &PrintJob{
		JobID:        1,
		PrinterName:  "Test Printer",
		DocumentName: "Large Report.pdf",
		TotalPages:   100,
		PagesPrinted: 0,
		Status:       "spooled",
	}

	// Job starts printing
	job.Status = "printing"
	job.StartTime = time.Now()

	// Simulate pages being printed
	for i := int32(1); i <= 100; i++ {
		job.PagesPrinted = i
	}

	if job.PagesPrinted != job.TotalPages {
		t.Errorf("PagesPrinted = %d, TotalPages = %d, want equal", job.PagesPrinted, job.TotalPages)
	}
}

func TestJobEvent_AllEventTypes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	job := &PrintJob{
		JobID:       1,
		TotalPages:  10,
		PrinterName: "TestPrinter",
	}

	eventTypes := []struct {
		eventType  JobEventType
		pagesAdded int32
	}{
		{JobEventAdded, 0},
		{JobEventStarted, 0},
		{JobEventModified, 0},
		{JobEventCompleted, 10},
		{JobEventError, 0},
		{JobEventDeleted, 0},
	}

	for _, et := range eventTypes {
		t.Run(string(et.eventType), func(t *testing.T) {
			t.Parallel()

			event := JobEvent{
				Type:        et.eventType,
				Job:         job,
				PrinterName: job.PrinterName,
				Timestamp:   now,
				PagesAdded:  et.pagesAdded,
			}

			if event.Type != et.eventType {
				t.Errorf("Type = %v, want %v", event.Type, et.eventType)
			}
			if event.PagesAdded != et.pagesAdded {
				t.Errorf("PagesAdded = %d, want %d", event.PagesAdded, et.pagesAdded)
			}
		})
	}
}
