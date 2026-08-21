//go:build windows
// +build windows

package main

import (
	"context"
	"net/http"

	"printmaster/agent/storage"
)

func usbProxySupported() bool {
	return true
}

func usbProxyTransportForSerial(serial string) (http.RoundTripper, bool) {
	transport, err := GetUSBTransportForSerial(serial)
	if err != nil {
		return nil, false
	}
	return transport, true
}

func usbProxyMetricsSnapshot(ctx context.Context, serial string) (*storage.MetricsSnapshot, bool) {
	snapshot, err := CollectUSBMetricsSnapshot(ctx, serial)
	if err != nil {
		return nil, false
	}
	return snapshot, true
}
