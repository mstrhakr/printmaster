package proxy

import (
	"testing"
	"time"
)

func TestNewSessionCache(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache()
	if cache == nil {
		t.Fatal("NewSessionCache() returned nil")
	}
	if cache.sessions == nil {
		t.Error("sessions map should be initialized")
	}
}

func TestSessionCacheGetNonexistent(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache()
	jar := cache.Get("nonexistent")
	if jar != nil {
		t.Error("Get() should return nil for nonexistent serial")
	}
}

func TestSessionCacheSetAndGet(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache()

	// Create a mock jar (nil is fine for testing the cache logic)
	cache.Set("serial123", nil)

	// Should be retrievable
	// Note: nil jar will still return nil, but we can test with non-nil
	// For this test, we verify the entry exists by checking expiration logic
	entry := cache.sessions["serial123"]
	if entry == nil {
		t.Fatal("Set() should create session entry")
	}
	if time.Until(entry.ExpiresAt) <= 0 {
		t.Error("Session should have future expiration")
	}
	if time.Until(entry.ExpiresAt) > 16*time.Minute {
		t.Error("Session expiration should be ~15 minutes")
	}
}

func TestSessionCacheClear(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache()
	cache.Set("serial123", nil)

	// Verify it exists
	if _, ok := cache.sessions["serial123"]; !ok {
		t.Fatal("session should exist after Set()")
	}

	// Clear it
	cache.Clear("serial123")

	// Verify it's gone
	if _, ok := cache.sessions["serial123"]; ok {
		t.Error("session should be removed after Clear()")
	}
}

func TestSessionCacheClearNonexistent(t *testing.T) {
	t.Parallel()

	cache := NewSessionCache()
	// Should not panic
	cache.Clear("nonexistent")
}

func TestEpsonLoginAdapterName(t *testing.T) {
	t.Parallel()

	adapter := &EpsonLoginAdapter{}
	if name := adapter.Name(); name != "Epson" {
		t.Errorf("Name() = %q, want %q", name, "Epson")
	}
}

func TestKyoceraLoginAdapterName(t *testing.T) {
	t.Parallel()

	adapter := &KyoceraLoginAdapter{}
	if name := adapter.Name(); name != "Kyocera" {
		t.Errorf("Name() = %q, want %q", name, "Kyocera")
	}
}
