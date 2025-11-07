package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoggerLevels(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)
	defer logger.Close()

	// Log at different levels
	logger.Error("error message")
	logger.Warn("warn message")
	logger.Info("info message")
	logger.Debug("debug message") // Should not appear
	logger.Trace("trace message") // Should not appear

	buffer := logger.GetBuffer()

	// Should only have ERROR, WARN, INFO (3 entries)
	if len(buffer) != 3 {
		t.Errorf("expected 3 log entries, got %d", len(buffer))
	}

	// Check levels
	if buffer[0].Level != ERROR || buffer[0].Message != "error message" {
		t.Errorf("first entry should be ERROR, got %v", buffer[0])
	}
	if buffer[1].Level != WARN || buffer[1].Message != "warn message" {
		t.Errorf("second entry should be WARN, got %v", buffer[1])
	}
	if buffer[2].Level != INFO || buffer[2].Message != "info message" {
		t.Errorf("third entry should be INFO, got %v", buffer[2])
	}
}

func TestLoggerContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)
	defer logger.Close()

	logger.Info("test message", "key1", "value1", "key2", 42)

	buffer := logger.GetBuffer()
	if len(buffer) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(buffer))
	}

	entry := buffer[0]
	if entry.Context["key1"] != "value1" {
		t.Errorf("expected context key1=value1, got %v", entry.Context["key1"])
	}
	if entry.Context["key2"] != 42 {
		t.Errorf("expected context key2=42, got %v", entry.Context["key2"])
	}
}

func TestLoggerSetLevel(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)
	defer logger.Close()

	logger.Debug("debug1") // Should not appear

	logger.SetLevel(DEBUG)
	logger.Debug("debug2") // Should appear

	buffer := logger.GetBuffer()
	if len(buffer) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(buffer))
	}
	if buffer[0].Message != "debug2" {
		t.Errorf("expected 'debug2', got %s", buffer[0].Message)
	}
}

func TestLoggerCircularBuffer(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 5) // Small buffer size
	defer logger.Close()

	// Log more than buffer size
	for i := 0; i < 10; i++ {
		logger.Info("message", "num", i)
	}

	buffer := logger.GetBuffer()
	if len(buffer) != 5 {
		t.Errorf("expected buffer size 5, got %d", len(buffer))
	}

	// Should have messages 5-9 (oldest dropped)
	if buffer[0].Context["num"] != 5 {
		t.Errorf("expected oldest entry to be num=5, got %v", buffer[0].Context["num"])
	}
	if buffer[4].Context["num"] != 9 {
		t.Errorf("expected newest entry to be num=9, got %v", buffer[4].Context["num"])
	}
}

func TestLoggerFileOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)

	logger.Info("test message", "key", "value")
	logger.Close()

	// Check that log file was created
	files, err := filepath.Glob(filepath.Join(tmpDir, "agent_*.log"))
	if err != nil {
		t.Fatalf("failed to list log files: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 log file, got %d", len(files))
	}

	// Read file content
	content, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "[INFO]") {
		t.Errorf("log file should contain [INFO], got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "test message") {
		t.Errorf("log file should contain 'test message', got: %s", contentStr)
	}
	if !strings.Contains(contentStr, "key=value") {
		t.Errorf("log file should contain 'key=value', got: %s", contentStr)
	}
}

func TestLoggerRateLimiting(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(WARN, tmpDir, 100)
	defer logger.Close()

	// Log same key multiple times rapidly
	for i := 0; i < 10; i++ {
		logger.WarnRateLimited("test-key", 1*time.Second, "rate limited message", "count", i)
		time.Sleep(50 * time.Millisecond)
	}

	buffer := logger.GetBuffer()
	// Should only have 1 entry (rate limited)
	if len(buffer) != 1 {
		t.Errorf("expected 1 log entry due to rate limiting, got %d", len(buffer))
	}

	// Wait for rate limit to expire
	time.Sleep(1 * time.Second)

	// This should log
	logger.WarnRateLimited("test-key", 1*time.Second, "rate limited message", "count", 10)

	buffer = logger.GetBuffer()
	if len(buffer) != 2 {
		t.Errorf("expected 2 log entries after rate limit expired, got %d", len(buffer))
	}
}

func TestLoggerDiagnostics(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)
	defer logger.Close()

	// Initially disabled
	if logger.DiagnosticEnabled("mib_walks") {
		t.Error("mib_walks should be disabled by default")
	}

	// Enable diagnostic
	logger.SetDiagnostic("mib_walks", true)

	if !logger.DiagnosticEnabled("mib_walks") {
		t.Error("mib_walks should be enabled after SetDiagnostic")
	}

	// Disable diagnostic
	logger.SetDiagnostic("mib_walks", false)

	if logger.DiagnosticEnabled("mib_walks") {
		t.Error("mib_walks should be disabled after SetDiagnostic(false)")
	}
}

func TestLoggerFilteredBuffer(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(TRACE, tmpDir, 100)
	defer logger.Close()

	logger.Error("error")
	logger.Warn("warn")
	logger.Info("info")
	logger.Debug("debug")
	logger.Trace("trace")

	// Filter to INFO and above (ERROR, WARN, INFO)
	filtered := logger.GetBufferFiltered(INFO)
	if len(filtered) != 3 {
		t.Errorf("expected 3 entries filtered to INFO, got %d", len(filtered))
	}

	// Filter to WARN and above (ERROR, WARN)
	filtered = logger.GetBufferFiltered(WARN)
	if len(filtered) != 2 {
		t.Errorf("expected 2 entries filtered to WARN, got %d", len(filtered))
	}

	// Filter to ERROR only
	filtered = logger.GetBufferFiltered(ERROR)
	if len(filtered) != 1 {
		t.Errorf("expected 1 entry filtered to ERROR, got %d", len(filtered))
	}
}

func TestLevelFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected LogLevel
	}{
		{"ERROR", ERROR},
		{"WARN", WARN},
		{"INFO", INFO},
		{"DEBUG", DEBUG},
		{"TRACE", TRACE},
		{"invalid", INFO}, // Default
	}

	for _, tt := range tests {
		result := LevelFromString(tt.input)
		if result != tt.expected {
			t.Errorf("LevelFromString(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestLevelToString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    LogLevel
		expected string
	}{
		{ERROR, "ERROR"},
		{WARN, "WARN"},
		{INFO, "INFO"},
		{DEBUG, "DEBUG"},
		{TRACE, "TRACE"},
	}

	for _, tt := range tests {
		result := LevelToString(tt.input)
		if result != tt.expected {
			t.Errorf("LevelToString(%v) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestLoggerRotation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 100)

	// Test with custom rotation policy - use fractional MB for testing
	logger.rotationPolicy = RotationPolicy{
		Enabled:    true,
		MaxSizeMB:  0, // Will set manually in bytes
		MaxAgeDays: 1,
		MaxFiles:   3,
	}

	// Write a few KB and manually trigger rotation
	logger.Info("first message")
	logger.Info(strings.Repeat("x", 1000))

	// Manually trigger rotation
	logger.mu.Lock()
	if logger.currentFile != nil {
		logger.currentFile.Close()
		logger.currentFile = nil
	}
	logger.mu.Unlock()

	// Ensure different timestamp (format is down to seconds)
	time.Sleep(1100 * time.Millisecond)

	// Write to new file
	logger.Info("second message")
	logger.Close()

	// Check that multiple log files exist
	files, err := filepath.Glob(filepath.Join(tmpDir, "agent_*.log"))
	if err != nil {
		t.Fatalf("failed to list log files: %v", err)
	}

	if len(files) < 2 {
		t.Errorf("expected at least 2 log files after rotation, got %d files: %v", len(files), files)
	}
}

func TestLoggerConcurrency(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logger := New(INFO, tmpDir, 1000)
	defer logger.Close()

	// Concurrent logging from multiple goroutines
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				logger.Info("concurrent message", "goroutine", id, "iteration", j)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	buffer := logger.GetBuffer()
	// Should have 1000 entries (buffer size)
	if len(buffer) != 1000 {
		t.Errorf("expected 1000 entries in buffer, got %d", len(buffer))
	}
}

func TestFormatLogEntry(t *testing.T) {
	t.Parallel()

	entry := LogEntry{
		Timestamp: time.Date(2025, 11, 1, 12, 0, 0, 0, time.UTC),
		Level:     INFO,
		Message:   "test message",
		Context: map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		},
	}

	formatted := formatLogEntry(entry)

	if !strings.Contains(formatted, "[INFO]") {
		t.Errorf("formatted entry should contain [INFO], got: %s", formatted)
	}
	if !strings.Contains(formatted, "test message") {
		t.Errorf("formatted entry should contain message, got: %s", formatted)
	}
	if !strings.Contains(formatted, "key1=value1") {
		t.Errorf("formatted entry should contain context, got: %s", formatted)
	}
}
