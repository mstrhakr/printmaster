//go:build !windows
// +build !windows

package main

import (
	"context"
	"errors"
	"net/http"

	"printmaster/agent/storage"
)

// InitUSBProxy is a no-op on non-Windows platforms
func InitUSBProxy(logger Logger) error {
	if logger != nil {
		logger.Debug("USB proxy not supported on this platform")
	}
	return nil
}

// StopUSBProxy is a no-op on non-Windows platforms
func StopUSBProxy() {}

// CanUSBProxySerial always returns false on non-Windows platforms
func CanUSBProxySerial(serial string) bool {
	return false
}

// HandleUSBProxy always returns false on non-Windows platforms
func HandleUSBProxy(w http.ResponseWriter, r *http.Request, serial string) bool {
	return false
}

// GetUSBTransportForSerial returns an error on non-Windows platforms
func GetUSBTransportForSerial(serial string) (http.RoundTripper, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}

// CollectUSBMetricsSnapshot returns an error on non-Windows platforms
func CollectUSBMetricsSnapshot(ctx context.Context, serial string) (*storage.MetricsSnapshot, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}

// RegisterUSBProxyHandlers registers stub endpoints on non-Windows platforms
func RegisterUSBProxyHandlers() {
	// Stub endpoint that reports USB proxy as unsupported
	http.HandleFunc("/api/usb-printers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"supported":false,"printers":[]}`))
	})

	http.HandleFunc("/api/usb-printers/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"supported":false,"running":false}`))
	})
}
