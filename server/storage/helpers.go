package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"sort"
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

// ============================================================================
// Token/Security Helpers
// ============================================================================

// generateSecureToken creates a cryptographically secure random token
// encoded as URL-safe base64.
func generateSecureToken(length int) string {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate secure token: " + err.Error())
	}
	return base64.URLEncoding.EncodeToString(b)
}

// hashSHA256 returns the hex-encoded SHA-256 hash of a string.
func hashSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ============================================================================
// Slice Helpers
// ============================================================================

// sortStrings sorts a string slice in place and returns it.
func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}
