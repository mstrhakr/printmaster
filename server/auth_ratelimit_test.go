package main

import (
	"testing"
	"time"
)

func TestAuthRateLimiter_BasicFunctionality(t *testing.T) {
	t.Parallel()

	// Create rate limiter: 3 attempts, 1 minute block, 30 second window
	rl := NewAuthRateLimiter(3, 1*time.Minute, 30*time.Second)
	defer rl.Stop()

	ip := "192.168.1.100"
	token := "abc123"

	// First attempt should not be blocked
	isBlocked, shouldLog, count := rl.RecordFailure(ip, token)
	if isBlocked {
		t.Error("First attempt should not be blocked")
	}
	if !shouldLog {
		t.Error("First attempt should be logged")
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Second attempt
	isBlocked, _, count = rl.RecordFailure(ip, token)
	if isBlocked {
		t.Error("Second attempt should not be blocked")
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	// Third attempt - should trigger block
	isBlocked, _, count = rl.RecordFailure(ip, token)
	if !isBlocked {
		t.Error("Third attempt should be blocked")
	}
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}

	// Fourth attempt - should still be blocked
	isBlocked, _, _ = rl.RecordFailure(ip, token)
	if !isBlocked {
		t.Error("Fourth attempt should still be blocked")
	}
}

func TestAuthRateLimiter_SuccessResetsCount(t *testing.T) {
	t.Parallel()

	rl := NewAuthRateLimiter(3, 1*time.Minute, 30*time.Second)
	defer rl.Stop()

	ip := "192.168.1.100"
	token := "abc123"

	// Record two failures
	rl.RecordFailure(ip, token)
	rl.RecordFailure(ip, token)

	// Record success - should clear the record
	rl.RecordSuccess(ip, token)

	// Next failure should start fresh at count 1
	isBlocked, _, count := rl.RecordFailure(ip, token)
	if isBlocked {
		t.Error("Should not be blocked after successful auth cleared the record")
	}
	if count != 1 {
		t.Errorf("Expected count to reset to 1 after success, got %d", count)
	}
}

func TestAuthRateLimiter_DifferentIPsIndependent(t *testing.T) {
	t.Parallel()

	rl := NewAuthRateLimiter(2, 1*time.Minute, 30*time.Second)
	defer rl.Stop()

	ip1 := "192.168.1.100"
	ip2 := "192.168.1.101"
	token := "abc123"

	// Block IP1
	rl.RecordFailure(ip1, token)
	isBlocked, _, _ := rl.RecordFailure(ip1, token)
	if !isBlocked {
		t.Error("IP1 should be blocked")
	}

	// IP2 should not be affected
	isBlocked, _, count := rl.RecordFailure(ip2, token)
	if isBlocked {
		t.Error("IP2 should not be blocked")
	}
	if count != 1 {
		t.Errorf("IP2 should start at count 1, got %d", count)
	}
}

func TestAuthRateLimiter_DifferentTokensIndependent(t *testing.T) {
	t.Parallel()

	rl := NewAuthRateLimiter(2, 1*time.Minute, 30*time.Second)
	defer rl.Stop()

	ip := "192.168.1.100"
	token1 := "abc123"
	token2 := "xyz789"

	// Block token1
	rl.RecordFailure(ip, token1)
	isBlocked, _, _ := rl.RecordFailure(ip, token1)
	if !isBlocked {
		t.Error("Token1 should be blocked")
	}

	// Token2 should not be affected
	isBlocked, _, count := rl.RecordFailure(ip, token2)
	if isBlocked {
		t.Error("Token2 should not be blocked")
	}
	if count != 1 {
		t.Errorf("Token2 should start at count 1, got %d", count)
	}
}

func TestAuthRateLimiter_IsBlocked(t *testing.T) {
	t.Parallel()

	rl := NewAuthRateLimiter(2, 100*time.Millisecond, 30*time.Second)
	defer rl.Stop()

	ip := "192.168.1.100"
	token := "abc123"

	// Not blocked initially
	isBlocked, _ := rl.IsBlocked(ip, token)
	if isBlocked {
		t.Error("Should not be blocked initially")
	}

	// Block the IP+token
	rl.RecordFailure(ip, token)
	rl.RecordFailure(ip, token)

	// Should be blocked now
	isBlocked, blockedUntil := rl.IsBlocked(ip, token)
	if !isBlocked {
		t.Error("Should be blocked after max attempts")
	}
	if blockedUntil.IsZero() {
		t.Error("blockedUntil should be set")
	}

	// Wait for block to expire
	time.Sleep(150 * time.Millisecond)

	// Should not be blocked anymore
	isBlocked, _ = rl.IsBlocked(ip, token)
	if isBlocked {
		t.Error("Should not be blocked after expiry")
	}
}

func TestAuthRateLimiter_GetStats(t *testing.T) {
	t.Parallel()

	rl := NewAuthRateLimiter(2, 1*time.Minute, 30*time.Second)
	defer rl.Stop()

	// Initial stats
	stats := rl.GetStats()
	if stats["total_records"].(int) != 0 {
		t.Error("Should have 0 records initially")
	}
	if stats["blocked_clients"].(int) != 0 {
		t.Error("Should have 0 blocked clients initially")
	}

	// Add some records
	rl.RecordFailure("192.168.1.100", "abc123")
	rl.RecordFailure("192.168.1.101", "xyz789")
	rl.RecordFailure("192.168.1.101", "xyz789") // Block this one

	stats = rl.GetStats()
	if stats["total_records"].(int) != 2 {
		t.Errorf("Expected 2 total records, got %d", stats["total_records"].(int))
	}
	if stats["blocked_clients"].(int) != 1 {
		t.Errorf("Expected 1 blocked client, got %d", stats["blocked_clients"].(int))
	}
}

func TestExtractIPFromAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.100:54321", "192.168.1.100"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1:443", "10.0.0.1"},
		{"invalid", "invalid"}, // Should return as-is if parse fails
	}

	for _, tt := range tests {
		result := extractIPFromAddr(tt.input)
		if result != tt.expected {
			t.Errorf("extractIPFromAddr(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
