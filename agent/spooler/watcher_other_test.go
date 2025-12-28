//go:build !windows && !linux && !darwin
// +build !windows,!linux,!darwin

package spooler

import (
	"testing"
)

// =============================================================================
// Watcher Tests for Unsupported Platforms
// =============================================================================

func TestNewWatcher_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
}

func TestNewWatcher_WithLogger_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	log := &testLogger{}
	w := NewWatcher(cfg, log)

	if w == nil {
		t.Fatal("NewWatcher returned nil")
	}
}

func TestWatcher_Start_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	err := w.Start()
	if err == nil {
		t.Error("Start() should return an error on unsupported platforms")
	}
	if err.Error() != "spooler watching is not supported on this platform" {
		t.Errorf("Start() error = %q, want 'spooler watching is not supported on this platform'", err.Error())
	}
}

func TestWatcher_Stop_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	// Stop should be a no-op, shouldn't panic
	w.Stop()
}

func TestWatcher_Events_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	ch := w.Events()
	if ch != nil {
		t.Error("Events() should return nil on unsupported platforms")
	}
}

func TestWatcher_GetPrinters_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	printers := w.GetPrinters()
	if printers != nil {
		t.Error("GetPrinters() should return nil on unsupported platforms")
	}
}

func TestWatcher_GetPrinter_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	printer, found := w.GetPrinter("TestPrinter")
	if found {
		t.Error("GetPrinter() should return false on unsupported platforms")
	}
	if printer != nil {
		t.Error("GetPrinter() should return nil printer on unsupported platforms")
	}
}

func TestWatcher_GetJobs_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	jobs := w.GetJobs("TestPrinter")
	if jobs != nil {
		t.Error("GetJobs() should return nil on unsupported platforms")
	}
}

func TestWatcher_SetBaseline_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	success := w.SetBaseline("TestPrinter", 1000)
	if success {
		t.Error("SetBaseline() should return false on unsupported platforms")
	}
}

func TestWatcher_SetTracking_Unsupported(t *testing.T) {
	t.Parallel()

	cfg := DefaultWatcherConfig()
	w := NewWatcher(cfg, nil)

	success := w.SetTracking("TestPrinter", true)
	if success {
		t.Error("SetTracking() should return false on unsupported platforms")
	}
}

func TestIsSupported_Unsupported(t *testing.T) {
	t.Parallel()

	if IsSupported() {
		t.Error("IsSupported() should return false on unsupported platforms")
	}
}

// testLogger for testing
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
