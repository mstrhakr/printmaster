package featureflags

import "sync/atomic"

var epsonRemoteMode atomic.Bool

// SetEpsonRemoteMode enables or disables Epson remote-mode queries at runtime.
func SetEpsonRemoteMode(enabled bool) {
	epsonRemoteMode.Store(enabled)
}

// EpsonRemoteModeEnabled reports whether Epson remote-mode helpers should run.
func EpsonRemoteModeEnabled() bool {
	return epsonRemoteMode.Load()
}
