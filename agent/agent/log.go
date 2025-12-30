package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu sync.Mutex
	// DebugEnabled controls whether debug-level logs are written.
	DebugEnabled = false
)

// ExternalLogger defines the minimal logger the agent package can use.
// Implemented by the app's structured logger. We keep it small to avoid tight coupling.
type ExternalLogger interface {
	Error(msg string, context ...interface{})
	Warn(msg string, context ...interface{})
	Info(msg string, context ...interface{})
	Debug(msg string, context ...interface{})
	TraceTag(tag string, msg string, context ...interface{})
}

var extLogger ExternalLogger

// SetLogger allows the application to inject a structured logger.
// When set, agent.Info/Debug/Error will delegate to this logger.
func SetLogger(l ExternalLogger) {
	extLogger = l
}

func ensureLogDir() string {
	logDir := filepath.Join(".", "logs")
	_ = os.MkdirAll(logDir, 0o755)
	return logDir
}

func writeLine(level string, msg string) {
	// If an external logger is configured, prefer it
	if extLogger != nil {
		switch level {
		case "ERROR":
			extLogger.Error(msg)
		case "WARN":
			extLogger.Warn(msg)
		case "DEBUG":
			extLogger.Debug(msg)
		default:
			extLogger.Info(msg)
		}
		return
	}
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("%s [%s] %s", ts, level, msg)
	logMu.Lock()
	defer logMu.Unlock()
	// stdout for convenience
	fmt.Println(line)
	// append to on-disk logfile
	fpath := filepath.Join(ensureLogDir(), "agent.log")
	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(line + "\n")
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "agent/log: failed to close log file: %v\n", err)
		}
	}
}

// Info logs an informational message.
func Info(msg string) {
	writeLine("INFO", msg)
}

// InfoCtx logs an informational message with optional key/value context.
func InfoCtx(msg string, context ...interface{}) {
	if extLogger != nil {
		extLogger.Info(msg, context...)
		return
	}
	// Fallback: append context as a simple string
	if len(context) > 0 {
		msg = fmt.Sprintf("%s %v", msg, context)
	}
	writeLine("INFO", msg)
}

// Debug logs a debug message.
func Debug(msg string) {
	if !DebugEnabled {
		return
	}
	writeLine("DEBUG", msg)
}

// DebugCtx logs a debug message with optional key/value context.
func DebugCtx(msg string, context ...interface{}) {
	if !DebugEnabled {
		return
	}
	if extLogger != nil {
		extLogger.Debug(msg, context...)
		return
	}
	if len(context) > 0 {
		msg = fmt.Sprintf("%s %v", msg, context)
	}
	writeLine("DEBUG", msg)
}

// SetDebugEnabled toggles debug logging at runtime.
func SetDebugEnabled(v bool) {
	DebugEnabled = v
}

// Error logs an error message.
func Error(msg string) {
	writeLine("ERROR", msg)
}

// Warn logs a warning message.
func Warn(msg string) {
	writeLine("WARN", msg)
}

// WarnCtx logs a warning message with optional key/value context.
func WarnCtx(msg string, context ...interface{}) {
	if extLogger != nil {
		extLogger.Warn(msg, context...)
		return
	}
	if len(context) > 0 {
		msg = fmt.Sprintf("%s %v", msg, context)
	}
	writeLine("WARN", msg)
}

// ErrorCtx logs an error message with optional key/value context.
func ErrorCtx(msg string, context ...interface{}) {
	if extLogger != nil {
		extLogger.Error(msg, context...)
		return
	}
	if len(context) > 0 {
		msg = fmt.Sprintf("%s %v", msg, context)
	}
	writeLine("ERROR", msg)
}

// TraceTagCtx logs a trace-level message with a category tag.
// Used for high-volume diagnostic logs that are filtered by tag.
func TraceTagCtx(tag string, msg string, context ...interface{}) {
	if extLogger != nil {
		extLogger.TraceTag(tag, msg, context...)
		return
	}
	// Fallback: only print if debug is enabled (trace is more verbose)
	if !DebugEnabled {
		return
	}
	if len(context) > 0 {
		msg = fmt.Sprintf("%s %v", msg, context)
	}
	writeLine("TRACE", fmt.Sprintf("[%s] %s", tag, msg))
}
