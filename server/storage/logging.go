package storage

import (
	"fmt"
	"os"
	"strings"
	"time"

	"printmaster/common/logger"
)

// Package-level logger that can be set by the application (server)
var Log *logger.Logger

// SetLogger injects the structured logger from the main application.
func SetLogger(l *logger.Logger) {
	Log = l
}

func logWithLevel(level logger.LogLevel, msg string, kv ...interface{}) {
	if Log != nil {
		switch level {
		case logger.ERROR:
			Log.Error(msg, kv...)
		case logger.WARN:
			Log.Warn(msg, kv...)
		case logger.DEBUG:
			Log.Debug(msg, kv...)
		case logger.TRACE:
			Log.Trace(msg, kv...)
		default:
			Log.Info(msg, kv...)
		}
		return
	}

	timestamp := time.Now().Format(time.RFC3339)
	levelStr := logger.LevelToString(level)
	fmt.Fprintf(os.Stderr, "%s [storage][%s] %s%s\n", timestamp, levelStr, msg, formatKeyValues(kv...))
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

func logDebug(msg string, kv ...interface{}) {
	logWithLevel(logger.DEBUG, msg, kv...)
}

// Note: trace-level logging intentionally omitted until storage emits trace events.
