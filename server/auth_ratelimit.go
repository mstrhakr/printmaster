package main

import (
	"fmt"
	"net"
	"net/http"
	"strings"
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

// getRealIP extracts the real client IP from a request, respecting X-Forwarded-For
// and X-Real-IP headers when behind a trusted proxy.
func getRealIP(r *http.Request) string {
	remoteIP := extractIPFromAddr(r.RemoteAddr)

	// Only trust proxy headers if behind_proxy or cloudflare_proxy is enabled
	if serverConfig == nil || (!serverConfig.Server.BehindProxy && !serverConfig.Server.CloudflareProxy) {
		return remoteIP
	}

	// Check if the direct connection is from a trusted proxy
	if !isTrustedProxy(remoteIP) {
		return remoteIP
	}

	// Try CF-Connecting-IP first (Cloudflare's definitive client IP header)
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		if net.ParseIP(cfIP) != nil {
			return cfIP
		}
	}

	// Try True-Client-IP (Cloudflare Enterprise, also used by Akamai)
	if tcIP := r.Header.Get("True-Client-IP"); tcIP != "" {
		if net.ParseIP(tcIP) != nil {
			return tcIP
		}
	}

	// Try X-Forwarded-For (may contain chain: client, proxy1, proxy2)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first (leftmost) IP which should be the original client
		ips := strings.Split(xff, ",")
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Fall back to X-Real-IP (simpler, set by nginx)
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		// Validate it looks like an IP
		if net.ParseIP(realIP) != nil {
			return realIP
		}
	}

	return remoteIP
}

// defaultTrustedProxyCIDRs contains private network ranges commonly used by Docker/reverse proxies
var defaultTrustedProxyCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"::1/128",
	"fc00::/7",
}

// cloudflareIPv4CIDRs contains Cloudflare's IPv4 ranges
// Source: https://www.cloudflare.com/ips-v4
// Last updated: 2024-12
var cloudflareIPv4CIDRs = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
}

// cloudflareIPv6CIDRs contains Cloudflare's IPv6 ranges
// Source: https://www.cloudflare.com/ips-v6
// Last updated: 2024-12
var cloudflareIPv6CIDRs = []string{
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

// parsedTrustedProxies caches parsed CIDR networks
var parsedTrustedProxies []*net.IPNet
var trustedProxiesOnce sync.Once

// initTrustedProxies parses the trusted proxy CIDRs once
func initTrustedProxies() {
	trustedProxiesOnce.Do(func() {
		var entries []string

		// Start with user-configured proxies or defaults
		if serverConfig != nil && len(serverConfig.Server.TrustedProxies) > 0 {
			entries = serverConfig.Server.TrustedProxies
		} else {
			entries = defaultTrustedProxyCIDRs
		}

		// Add Cloudflare IPs if cloudflare_proxy is enabled
		if serverConfig != nil && serverConfig.Server.CloudflareProxy {
			logInfo("Cloudflare proxy mode enabled, adding Cloudflare IP ranges to trusted proxies")
			entries = append(entries, cloudflareIPv4CIDRs...)
			entries = append(entries, cloudflareIPv6CIDRs...)
		}

		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}

			// Check if it's a CIDR notation
			if strings.Contains(entry, "/") {
				_, network, err := net.ParseCIDR(entry)
				if err != nil {
					logWarn("Invalid trusted proxy CIDR", "entry", entry, "error", err)
					continue
				}
				parsedTrustedProxies = append(parsedTrustedProxies, network)
				continue
			}

			// Check if it's a bare IP address
			if ip := net.ParseIP(entry); ip != nil {
				// Convert to CIDR
				var cidr string
				if ip.To4() != nil {
					cidr = entry + "/32"
				} else {
					cidr = entry + "/128"
				}
				_, network, err := net.ParseCIDR(cidr)
				if err != nil {
					logWarn("Invalid trusted proxy IP", "entry", entry, "error", err)
					continue
				}
				parsedTrustedProxies = append(parsedTrustedProxies, network)
				continue
			}

			// Must be a hostname - resolve it
			ips, err := net.LookupIP(entry)
			if err != nil {
				logWarn("Failed to resolve trusted proxy hostname", "hostname", entry, "error", err)
				continue
			}
			for _, ip := range ips {
				var cidr string
				if ip.To4() != nil {
					cidr = ip.String() + "/32"
				} else {
					cidr = ip.String() + "/128"
				}
				_, network, err := net.ParseCIDR(cidr)
				if err != nil {
					continue
				}
				parsedTrustedProxies = append(parsedTrustedProxies, network)
				logInfo("Resolved trusted proxy hostname", "hostname", entry, "ip", ip.String())
			}
		}
	})
}

// isTrustedProxy checks if an IP is in the trusted proxy list
func isTrustedProxy(ipStr string) bool {
	initTrustedProxies()

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	for _, network := range parsedTrustedProxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
