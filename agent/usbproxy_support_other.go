//go:build !windows
// +build !windows

package main

import (
	"context"
	"net/http"

	"printmaster/agent/storage"
)

func usbProxySupported() bool {
	return false
}

func usbProxyTransportForSerial(serial string) (http.RoundTripper, bool) {
	return nil, false
}

func usbProxyMetricsSnapshot(ctx context.Context, serial string) (*storage.MetricsSnapshot, bool) {
	return nil, false
}
