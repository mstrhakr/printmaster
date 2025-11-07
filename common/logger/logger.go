package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log message
type LogLevel int

const (
	ERROR LogLevel = iota
	WARN
	INFO
	DEBUG
	TRACE
)

var levelNames = map[LogLevel]string{
	ERROR: "ERROR",
	WARN:  "WARN",
	INFO:  "INFO",
	DEBUG: "DEBUG",
	TRACE: "TRACE",
}

var levelColors = map[LogLevel]string{
	ERROR: "#ef4444",
	WARN:  "#f59e0b",
	INFO:  "#93a1a1",
	DEBUG: "#6b7280",
	TRACE: "#4b5563",
}

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Context   map[string]interface{}
}

// Logger provides structured logging with levels
type Logger struct {
	mu              sync.RWMutex
	level           LogLevel
	logDir          string
	currentFile     *os.File
	currentFilePath string // path to current log file for rotation
	buffer          []LogEntry
	maxBufferSize   int
	diagnostics     map[string]bool
	rotationPolicy  RotationPolicy
	rateLimiters    map[string]*rateLimiter
	consoleOutput   bool
	traceTags       map[string]bool // enabled trace tags for granular filtering
	onLogCallback   func(LogEntry)  // callback for SSE broadcasting
}

// RotationPolicy defines when and how to rotate log files
type RotationPolicy struct {
	Enabled    bool
	MaxSizeMB  int
	MaxAgeDays int
	MaxFiles   int
}

type rateLimiter struct {
	lastLog  time.Time
	interval time.Duration
}

// New creates a new Logger instance
func New(level LogLevel, logDir string, maxBufferSize int) *Logger {
	return &Logger{
		level:         level,
		logDir:        logDir,
		buffer:        make([]LogEntry, 0, maxBufferSize),
		maxBufferSize: maxBufferSize,
		diagnostics:   make(map[string]bool),
		rateLimiters:  make(map[string]*rateLimiter),
		consoleOutput: true, // Default to console output
		traceTags:     make(map[string]bool),
		rotationPolicy: RotationPolicy{
			Enabled:    true,
			MaxSizeMB:  50,
			MaxAgeDays: 7,
			MaxFiles:   10,
		},
	}
}

// SetConsoleOutput enables or disables console output
func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enabled
}

// SetOnLogCallback sets a callback function to be called when a new log entry is created
// This is used for SSE broadcasting of log entries in real-time
func (l *Logger) SetOnLogCallback(callback func(LogEntry)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onLogCallback = callback
}

// SetLevel changes the current log level
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

// SetDiagnostic enables or disables a diagnostic output type
func (l *Logger) SetDiagnostic(name string, enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.diagnostics[name] = enabled
}

// DiagnosticEnabled checks if a diagnostic type is enabled
func (l *Logger) DiagnosticEnabled(name string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.diagnostics[name]
}

// SetRotationPolicy configures log rotation
func (l *Logger) SetRotationPolicy(policy RotationPolicy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotationPolicy = policy
}

// Error logs an error level message
func (l *Logger) Error(msg string, context ...interface{}) {
	l.log(ERROR, msg, context...)
}

// Warn logs a warning level message
func (l *Logger) Warn(msg string, context ...interface{}) {
	l.log(WARN, msg, context...)
}

// WarnRateLimited logs a warning with rate limiting (max once per interval)
func (l *Logger) WarnRateLimited(key string, interval time.Duration, msg string, context ...interface{}) {
	l.mu.Lock()
	limiter, exists := l.rateLimiters[key]
	if !exists {
		limiter = &rateLimiter{interval: interval}
		l.rateLimiters[key] = limiter
	}

	now := time.Now()
	if now.Sub(limiter.lastLog) < limiter.interval {
		l.mu.Unlock()
		return // Skip this log
	}
	limiter.lastLog = now
	l.mu.Unlock()

	l.log(WARN, msg, context...)
}

// Info logs an info level message
func (l *Logger) Info(msg string, context ...interface{}) {
	l.log(INFO, msg, context...)
}

// Debug logs a debug level message
func (l *Logger) Debug(msg string, context ...interface{}) {
	l.log(DEBUG, msg, context...)
}

// Trace logs a trace level message
func (l *Logger) Trace(msg string, context ...interface{}) {
	l.log(TRACE, msg, context...)
}

// TraceTag logs a trace level message only if the specified tag is enabled.
// If no tags are enabled (empty traceTags map), all trace messages are logged (backward compatible).
// Usage: logger.TraceTag("proxy_request", "Proxying request", "path", "/foo", "method", "GET")
func (l *Logger) TraceTag(tag string, msg string, context ...interface{}) {
	l.mu.RLock()
	enabled := l.traceTags[tag]
	anyTagsEnabled := len(l.traceTags) > 0
	l.mu.RUnlock()

	// If no tags are configured, log everything at TRACE level (backward compatible)
	// If tags are configured, only log if this specific tag is enabled
	if !anyTagsEnabled || enabled {
		l.log(TRACE, msg, context...)
	}
}

// EnableTraceTag enables trace logging for a specific tag
func (l *Logger) EnableTraceTag(tag string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.traceTags[tag] = true
}

// DisableTraceTag disables trace logging for a specific tag
func (l *Logger) DisableTraceTag(tag string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.traceTags, tag)
}

// GetTraceTags returns a copy of the enabled trace tags
func (l *Logger) GetTraceTags() map[string]bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	tags := make(map[string]bool, len(l.traceTags))
	for k, v := range l.traceTags {
		tags[k] = v
	}
	return tags
}

// SetTraceTags replaces the enabled trace tags with the provided map
func (l *Logger) SetTraceTags(tags map[string]bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.traceTags = make(map[string]bool, len(tags))
	for k, v := range tags {
		if v {
			l.traceTags[k] = true
		}
	}
}

// log is the internal logging function
func (l *Logger) log(level LogLevel, msg string, context ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if we should log this level
	if level > l.level {
		return
	}

	// Parse context into map
	ctx := make(map[string]interface{})
	for i := 0; i < len(context)-1; i += 2 {
		if key, ok := context[i].(string); ok {
			ctx[key] = context[i+1]
		}
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Context:   ctx,
	}

	// Add to buffer (circular)
	if len(l.buffer) >= l.maxBufferSize {
		l.buffer = l.buffer[1:]
	}
	l.buffer = append(l.buffer, entry)

	// Write to console if enabled
	if l.consoleOutput {
		fmt.Println(formatLogEntry(entry))
	}

	// Write to file
	l.writeToFile(entry)

	// Broadcast to SSE if callback is set
	if l.onLogCallback != nil {
		// Call callback without holding the lock to avoid deadlocks
		// Make a copy of the callback
		callback := l.onLogCallback
		l.mu.Unlock()
		callback(entry)
		l.mu.Lock()
	}
}

// writeToFile writes a log entry to the current log file
func (l *Logger) writeToFile(entry LogEntry) {
	// Ensure log directory exists
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		return
	}

	// Open current file if not open
	if l.currentFile == nil {
		filename := filepath.Join(l.logDir, "agent.log")
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		l.currentFile = f
		l.currentFilePath = filename
	}

	// Format and write entry
	line := formatLogEntry(entry)
	l.currentFile.WriteString(line + "\n")
	l.currentFile.Sync() // Flush to disk for accurate size checks

	// Check if we need to rotate AFTER writing
	if l.shouldRotate() {
		l.rotate()
	}
}

// formatLogEntry formats a log entry for file output
func formatLogEntry(entry LogEntry) string {
	timestamp := entry.Timestamp.Format("2006-01-02T15:04:05-07:00")
	level := levelNames[entry.Level]

	line := fmt.Sprintf("%s [%s] %s", timestamp, level, entry.Message)

	if len(entry.Context) > 0 {
		for k, v := range entry.Context {
			line += fmt.Sprintf(" %s=%v", k, v)
		}
	}

	return line
}

// shouldRotate checks if the current log file should be rotated
func (l *Logger) shouldRotate() bool {
	if !l.rotationPolicy.Enabled || l.currentFile == nil {
		return false
	}

	// Check file size
	if l.rotationPolicy.MaxSizeMB > 0 {
		if stat, err := l.currentFile.Stat(); err == nil {
			sizeBytes := stat.Size()
			maxBytes := int64(l.rotationPolicy.MaxSizeMB) * 1024 * 1024
			if sizeBytes >= maxBytes {
				return true
			}
		}
	}

	return false
}

// rotate closes the current log file, renames it with timestamp, and starts a new one
func (l *Logger) rotate() {
	if l.currentFile != nil {
		l.currentFile.Close()
		l.currentFile = nil

		// Rename current log file to timestamped backup
		if l.currentFilePath != "" {
			timestamp := time.Now().Format("20060102_150405")
			backupPath := filepath.Join(l.logDir, fmt.Sprintf("agent_%s.log", timestamp))
			os.Rename(l.currentFilePath, backupPath)
		}
	}

	// Clean up old files
	l.cleanOldFiles()
}

// cleanOldFiles removes log files older than MaxAgeDays
func (l *Logger) cleanOldFiles() {
	if l.rotationPolicy.MaxAgeDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -l.rotationPolicy.MaxAgeDays)

	files, err := filepath.Glob(filepath.Join(l.logDir, "agent_*.log"))
	if err != nil {
		return
	}

	for _, file := range files {
		if stat, err := os.Stat(file); err == nil {
			if stat.ModTime().Before(cutoff) {
				os.Remove(file)
			}
		}
	}

	// Also limit by MaxFiles
	if l.rotationPolicy.MaxFiles > 0 && len(files) > l.rotationPolicy.MaxFiles {
		// Remove oldest files
		for i := 0; i < len(files)-l.rotationPolicy.MaxFiles; i++ {
			os.Remove(files[i])
		}
	}
}

// GetBuffer returns a copy of the in-memory log buffer
func (l *Logger) GetBuffer() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	buffer := make([]LogEntry, len(l.buffer))
	copy(buffer, l.buffer)
	return buffer
}

// GetBufferFiltered returns logs filtered by minimum level
func (l *Logger) GetBufferFiltered(minLevel LogLevel) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	filtered := []LogEntry{}
	for _, entry := range l.buffer {
		if entry.Level <= minLevel {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// ForceRotate immediately rotates the current log file
// This is useful for clearing logs or starting fresh
func (l *Logger) ForceRotate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotate()
}

// ClearBuffer clears the in-memory log buffer
func (l *Logger) ClearBuffer() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buffer = make([]LogEntry, 0, l.maxBufferSize)
}

// Close closes the current log file
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.currentFile != nil {
		err := l.currentFile.Close()
		l.currentFile = nil
		return err
	}
	return nil
}

// LevelFromString converts a string to a LogLevel
func LevelFromString(s string) LogLevel {
	switch s {
	case "ERROR":
		return ERROR
	case "WARN":
		return WARN
	case "INFO":
		return INFO
	case "DEBUG":
		return DEBUG
	case "TRACE":
		return TRACE
	default:
		return INFO
	}
}

// LevelToString converts a LogLevel to a string
func LevelToString(level LogLevel) string {
	return levelNames[level]
}

// LevelColor returns the color for a log level
func LevelColor(level LogLevel) string {
	return levelColors[level]
}

// Copy writes all buffered logs to a writer
func (l *Logger) Copy(w io.Writer) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, entry := range l.buffer {
		line := formatLogEntry(entry)
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
