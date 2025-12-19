//go:build !windows && !linux && !darwin
// +build !windows,!linux,!darwin

package spooler

import (
	"fmt"
)

// Watcher is not supported on this platform
type Watcher struct {
	config WatcherConfig
	logger Logger
}

// NewWatcher creates a new spooler watcher (no-op on unsupported platforms)
func NewWatcher(config WatcherConfig, logger Logger) *Watcher {
	if logger == nil {
		logger = nullLogger{}
	}
	return &Watcher{
		config: config,
		logger: logger,
	}
}

// Start returns an error on unsupported platforms
func (w *Watcher) Start() error {
	return fmt.Errorf("spooler watching is not supported on this platform")
}

// Stop is a no-op on unsupported platforms
func (w *Watcher) Stop() {}

// Events returns a nil channel on unsupported platforms
func (w *Watcher) Events() <-chan JobEvent {
	return nil
}

// GetPrinters returns empty on unsupported platforms
func (w *Watcher) GetPrinters() []*LocalPrinter {
	return nil
}

// GetPrinter returns not found on unsupported platforms
func (w *Watcher) GetPrinter(name string) (*LocalPrinter, bool) {
	return nil, false
}

// GetJobs returns empty on unsupported platforms
func (w *Watcher) GetJobs(printerName string) []*PrintJob {
	return nil
}

// SetBaseline returns false on unsupported platforms
func (w *Watcher) SetBaseline(printerName string, baseline int64) bool {
	return false
}

// SetTracking returns false on unsupported platforms
func (w *Watcher) SetTracking(printerName string, enabled bool) bool {
	return false
}

// IsSupported returns whether spooler watching is supported on this platform
func IsSupported() bool {
	return false
}
