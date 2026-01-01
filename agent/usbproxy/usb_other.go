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
