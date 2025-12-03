package featureflags

import "testing"

func TestEpsonRemoteMode(t *testing.T) {
	t.Parallel()

	// Default should be false
	if EpsonRemoteModeEnabled() {
		t.Error("EpsonRemoteModeEnabled() should default to false")
	}

	// Enable and verify
	SetEpsonRemoteMode(true)
	if !EpsonRemoteModeEnabled() {
		t.Error("EpsonRemoteModeEnabled() should be true after SetEpsonRemoteMode(true)")
	}

	// Disable and verify
	SetEpsonRemoteMode(false)
	if EpsonRemoteModeEnabled() {
		t.Error("EpsonRemoteModeEnabled() should be false after SetEpsonRemoteMode(false)")
	}
}

func TestEpsonRemoteModeToggle(t *testing.T) {
	t.Parallel()

	// Test multiple toggles
	for i := 0; i < 10; i++ {
		expected := i%2 == 0
		SetEpsonRemoteMode(expected)
		if got := EpsonRemoteModeEnabled(); got != expected {
			t.Errorf("iteration %d: got %v, want %v", i, got, expected)
		}
	}
}
