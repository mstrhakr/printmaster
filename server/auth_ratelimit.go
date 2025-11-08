package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// AuthRateLimiter tracks failed authentication attempts and blocks repeat offenders
type AuthRateLimiter struct {
	mu              sync.RWMutex
	attempts        map[string]*authAttemptRecord // key: IP+TokenPrefix
	maxAttempts     int                           // Maximum failed attempts before blocking
	blockDuration   time.Duration                 // How long to block after max attempts
	cleanupInterval time.Duration                 // How often to clean up old records
	attemptsWindow  time.Duration                 // Time window for counting attempts
	stopCleanup     chan struct{}
}

// authAttemptRecord tracks authentication attempts from a specific IP+token
type authAttemptRecord struct {
	firstAttempt    time.Time
	lastAttempt     time.Time
	failureCount    int
	blockedUntil    time.Time
	lastLoggedCount int // Track last logged count to avoid log spam
}

// NewAuthRateLimiter creates a new rate limiter with specified parameters
func NewAuthRateLimiter(maxAttempts int, blockDuration, attemptsWindow time.Duration) *AuthRateLimiter {
	rl := &AuthRateLimiter{
		attempts:        make(map[string]*authAttemptRecord),
		maxAttempts:     maxAttempts,
		blockDuration:   blockDuration,
		cleanupInterval: 1 * time.Minute,
		attemptsWindow:  attemptsWindow,
		stopCleanup:     make(chan struct{}),
	}

	// Start background cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// RecordFailure records a failed authentication attempt
// Returns (isBlocked, shouldLog, attemptCount)
func (rl *AuthRateLimiter) RecordFailure(ip, tokenPrefix string) (bool, bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := rl.makeKey(ip, tokenPrefix)
	now := time.Now()

	record, exists := rl.attempts[key]
	if !exists {
		record = &authAttemptRecord{
			firstAttempt: now,
			lastAttempt:  now,
			failureCount: 1,
		}
		rl.attempts[key] = record
		return false, true, 1 // Not blocked, should log first attempt
	}

	// Check if still blocked
	if now.Before(record.blockedUntil) {
		record.lastAttempt = now
		record.failureCount++
		// Only log periodically during block (every 10 attempts)
		shouldLog := record.failureCount%10 == 0
		return true, shouldLog, record.failureCount
	}

	// Check if we're in a new time window
	if now.Sub(record.firstAttempt) > rl.attemptsWindow {
		// Reset the window
		record.firstAttempt = now
		record.lastAttempt = now
		record.failureCount = 1
		record.lastLoggedCount = 0
		return false, true, 1
	}

	// Increment failure count
	record.lastAttempt = now
	record.failureCount++

	// Check if we should block
	if record.failureCount >= rl.maxAttempts {
		record.blockedUntil = now.Add(rl.blockDuration)
		return true, true, record.failureCount // Blocked, log the block
	}

	// Log every attempt up to max, then log less frequently
	shouldLog := record.failureCount <= rl.maxAttempts ||
		(record.failureCount-record.lastLoggedCount >= 5)

	if shouldLog {
		record.lastLoggedCount = record.failureCount
	}

	return false, shouldLog, record.failureCount
}

// IsBlocked checks if an IP+token is currently blocked
func (rl *AuthRateLimiter) IsBlocked(ip, tokenPrefix string) (bool, time.Time) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	key := rl.makeKey(ip, tokenPrefix)
	record, exists := rl.attempts[key]
	if !exists {
		return false, time.Time{}
	}

	now := time.Now()
	if now.Before(record.blockedUntil) {
		return true, record.blockedUntil
	}

	return false, time.Time{}
}

// RecordSuccess records a successful authentication (clears failure record)
func (rl *AuthRateLimiter) RecordSuccess(ip, tokenPrefix string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	key := rl.makeKey(ip, tokenPrefix)
	delete(rl.attempts, key)
}

// makeKey creates a unique key for tracking attempts
func (rl *AuthRateLimiter) makeKey(ip, tokenPrefix string) string {
	return fmt.Sprintf("%s:%s", ip, tokenPrefix)
}

// cleanupLoop periodically removes old records
func (rl *AuthRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// cleanup removes expired records
func (rl *AuthRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, record := range rl.attempts {
		// Remove if:
		// 1. Block expired and no recent attempts
		// 2. Last attempt was beyond the attempts window
		if (now.After(record.blockedUntil) && now.Sub(record.lastAttempt) > rl.attemptsWindow) ||
			now.Sub(record.lastAttempt) > rl.blockDuration*2 {
			delete(rl.attempts, key)
		}
	}
}

// Stop stops the cleanup goroutine
func (rl *AuthRateLimiter) Stop() {
	close(rl.stopCleanup)
}

// GetStats returns statistics about current state
func (rl *AuthRateLimiter) GetStats() map[string]interface{} {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	blocked := 0
	now := time.Now()
	for _, record := range rl.attempts {
		if now.Before(record.blockedUntil) {
			blocked++
		}
	}

	return map[string]interface{}{
		"total_records":   len(rl.attempts),
		"blocked_clients": blocked,
	}
}

// extractIPFromAddr extracts IP address from remote address (removes port)
func extractIPFromAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr // Return as-is if parse fails
	}
	return host
}
