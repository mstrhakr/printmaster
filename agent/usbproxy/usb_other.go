//go:build !windows
// +build !windows

package usbproxy

import "errors"

// IsSupported returns whether USB proxy is supported on this platform
func IsSupported() bool {
	return false
}

// NewEnumerator creates a USB device enumerator for non-Windows platforms
func NewEnumerator(logger Logger) (USBDeviceEnumerator, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}

// Stub implementations for non-Windows builds

type stubEnumerator struct{}

func (e *stubEnumerator) Enumerate() ([]*USBPrinter, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}

func (e *stubEnumerator) GetDeviceDetails(devicePath string) (*USBPrinter, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}

func (e *stubEnumerator) CreateTransport(printer *USBPrinter) (USBTransport, error) {
	return nil, errors.New("USB proxy not supported on this platform")
}
