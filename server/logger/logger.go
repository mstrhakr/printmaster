package logger

import (
	"fmt"
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

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time
	Level     LogLevel
	Message   string
	Context   map[string]interface{}
}

// Logger provides structured logging with levels
type Logger struct {
	mu            sync.RWMutex
	level         LogLevel
	logDir        string
	currentFile   *os.File
	buffer        []LogEntry
	maxBufferSize int
	consoleOutput bool
	// TODO: Add SSE broadcasting when web UI is ready
	// onLogCallback func(LogEntry)
}

// New creates a new Logger instance
func New(level LogLevel, logDir string, maxBufferSize int) *Logger {
	return &Logger{
		level:         level,
		logDir:        logDir,
		buffer:        make([]LogEntry, 0, maxBufferSize),
		maxBufferSize: maxBufferSize,
		consoleOutput: true,
	}
}

// SetConsoleOutput enables or disables console output
func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleOutput = enabled
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

// Error logs an error level message
func (l *Logger) Error(msg string, context ...interface{}) {
	l.log(ERROR, msg, context...)
}

// Warn logs a warning level message
func (l *Logger) Warn(msg string, context ...interface{}) {
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

// log is the internal logging implementation
func (l *Logger) log(level LogLevel, msg string, context ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if this level should be logged
	if level > l.level {
		return
	}

	// Build context map from variadic args
	ctx := make(map[string]interface{})
	for i := 0; i < len(context); i += 2 {
		if i+1 < len(context) {
			key := fmt.Sprintf("%v", context[i])
			ctx[key] = context[i+1]
		}
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Context:   ctx,
	}

	// Add to buffer
	l.buffer = append(l.buffer, entry)
	if len(l.buffer) > l.maxBufferSize {
		l.buffer = l.buffer[1:] // Remove oldest
	}

	// Console output
	if l.consoleOutput {
		l.printToConsole(entry)
	}

	// File output
	l.writeToFile(entry)

	// TODO: SSE broadcasting
	// if l.onLogCallback != nil {
	//     l.onLogCallback(entry)
	// }
}

// printToConsole outputs the log entry to console
func (l *Logger) printToConsole(entry LogEntry) {
	timestamp := entry.Timestamp.Format("2006-01-02 15:04:05")
	levelStr := levelNames[entry.Level]

	contextStr := ""
	if len(entry.Context) > 0 {
		contextStr = " |"
		for k, v := range entry.Context {
			contextStr += fmt.Sprintf(" %s=%v", k, v)
		}
	}

	fmt.Printf("[%s] [%s] %s%s\n", timestamp, levelStr, entry.Message, contextStr)
}

// writeToFile writes the log entry to the current log file
func (l *Logger) writeToFile(entry LogEntry) {
	if l.logDir == "" {
		return
	}

	// Ensure log directory exists
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		return
	}

	// Open/create log file if needed
	if l.currentFile == nil {
		logPath := filepath.Join(l.logDir, "server.log")
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return
		}
		l.currentFile = f
	}

	// Write entry
	timestamp := entry.Timestamp.Format("2006-01-02 15:04:05")
	levelStr := levelNames[entry.Level]

	contextStr := ""
	if len(entry.Context) > 0 {
		contextStr = " |"
		for k, v := range entry.Context {
			contextStr += fmt.Sprintf(" %s=%v", k, v)
		}
	}

	logLine := fmt.Sprintf("[%s] [%s] %s%s\n", timestamp, levelStr, entry.Message, contextStr)
	l.currentFile.WriteString(logLine)
}

// GetBuffer returns recent log entries
func (l *Logger) GetBuffer() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	entries := make([]LogEntry, len(l.buffer))
	copy(entries, l.buffer)
	return entries
}

// Close closes any open file handles
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.currentFile != nil {
		return l.currentFile.Close()
	}
	return nil
}

// ParseLevel converts a string to LogLevel
func ParseLevel(s string) LogLevel {
	switch s {
	case "ERROR", "error":
		return ERROR
	case "WARN", "warn":
		return WARN
	case "INFO", "info":
		return INFO
	case "DEBUG", "debug":
		return DEBUG
	case "TRACE", "trace":
		return TRACE
	default:
		return INFO
	}
}
