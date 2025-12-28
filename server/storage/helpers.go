package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ============================================================================
// SQL Null Value Helpers
// ============================================================================

// nullString returns a sql.NullString for optional string values.
// Empty strings are treated as NULL.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullBytes returns nil for empty byte slices, otherwise the string value.
// This is used for SQLite which handles interface{} values.
func nullBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

// nullInt64 returns a sql.NullInt64 for optional int64 values.
// Zero values are treated as NULL (matching existing sqlite.go behavior).
func nullInt64(i int64) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: i, Valid: true}
}

// nullInt64Ptr returns a sql.NullInt64 for optional *int64 values.
func nullInt64Ptr(i *int64) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: *i, Valid: true}
}

// nullTimePtr returns a sql.NullTime for optional *time.Time values.
func nullTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// nullTime returns a sql.NullTime for time.Time values.
// Zero times are treated as NULL.
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// ============================================================================
// Token/Security Helpers
// ============================================================================

// generateSecureToken creates a cryptographically secure random token
// encoded as URL-safe base64. Returns an error if entropy is unavailable.
func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}


// hashSHA256 returns the hex-encoded SHA-256 hash of a string.
func hashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TokenHash returns the hex-encoded SHA-256 hash of a token string.
// This is the exported version of hashSHA256 for use in main.go and other packages.
func TokenHash(token string) string {
	return hashSHA256(token)
}

// safePrefix returns the first n characters of a string for safe logging.
// Returns "<empty>" for empty strings and "<short>" for strings shorter than n.
func safePrefix(s string, n int) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) < n {
		return "<short>"
	}
	return s[:n]
}

// ============================================================================
// Database Path Helpers
// ============================================================================

// GetDefaultDBPath returns the default database path.
// On Windows, this is typically in ProgramData; on Unix-like systems, /var/lib.
func GetDefaultDBPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("PROGRAMDATA")
		if pd == "" {
			pd = "C:\\ProgramData"
		}
		return filepath.Join(pd, "PrintMaster", "server", "server.db")
	}
	return "/var/lib/printmaster/server.db"
}

// ============================================================================
// JSON Encoding Helpers
// ============================================================================

// encodeMetadata converts a map to a JSON NullString for database storage.
func encodeMetadata(meta map[string]any) (sql.NullString, error) {
	if len(meta) == 0 {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

// decodeMetadata parses a JSON NullString to a map.
func decodeMetadata(raw sql.NullString) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw.String), &result); err != nil {
		return nil
	}
	return result
}
