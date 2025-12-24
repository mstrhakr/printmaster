package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"printmaster/common/logger"
)

// logWithLevel routes structured logs to the shared logger when available,
// and falls back to stderr with a consistent format during bootstrap.
func logWithLevel(level logger.LogLevel, msg string, kv ...interface{}) {
	if serverLogger != nil {
		switch level {
		case logger.ERROR:
			serverLogger.Error(msg, kv...)
		case logger.WARN:
			serverLogger.Warn(msg, kv...)
		case logger.DEBUG:
			serverLogger.Debug(msg, kv...)
		case logger.TRACE:
			serverLogger.Trace(msg, kv...)
		default:
			serverLogger.Info(msg, kv...)
		}
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	levelStr := logger.LevelToString(level)
	fmt.Fprintf(os.Stderr, "%s [%s] %s%s\n", timestamp, levelStr, msg, formatKeyValues(kv...))
}

func formatKeyValues(kv ...interface{}) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		key := fmt.Sprintf("arg%d", i)
		var val interface{} = "<missing>"
		if k, ok := kv[i].(string); ok {
			key = k
		} else {
			val = kv[i]
		}
		if i+1 < len(kv) {
			val = kv[i+1]
		}
		b.WriteString(" ")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(fmt.Sprint(val))
	}
	return b.String()
}

func logInfo(msg string, kv ...interface{}) {
	logWithLevel(logger.INFO, msg, kv...)
}

func logWarn(msg string, kv ...interface{}) {
	logWithLevel(logger.WARN, msg, kv...)
}

func logError(msg string, kv ...interface{}) {
	logWithLevel(logger.ERROR, msg, kv...)
}

func logDebug(msg string, kv ...interface{}) {
	logWithLevel(logger.DEBUG, msg, kv...)
}

// logTraceTag logs a trace-level message with a category tag for filtering.
// Used for high-volume diagnostic logs (e.g., proxy requests).
func logTraceTag(tag string, msg string, kv ...interface{}) {
	if serverLogger != nil {
		serverLogger.TraceTag(tag, msg, kv...)
		return
	}
	// Fallback during bootstrap - skip trace logs
}

func logFatal(msg string, kv ...interface{}) {
	logError(msg, kv...)
	os.Exit(1)
}

// logBridgeWriter allows stdlib loggers (e.g., http.Server ErrorLog) to route
// their output through the shared structured logger.
type logBridgeWriter struct {
	level logger.LogLevel
}

func (w logBridgeWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}
	logWithLevel(w.level, msg)
	return len(p), nil
}
