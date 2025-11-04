package util

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DecodeOctetString attempts to convert raw octet string bytes into a
// human-friendly UTF-8 string. It tries UTF-8 first, then falls back to a
// single-byte ISO-8859-1 style decoding (direct byte->rune mapping). It also
// strips common non-printable control characters and trims whitespace.
func DecodeOctetString(b []byte) string {
	if b == nil {
		return ""
	}
	// Prefer valid UTF-8
	if utf8.Valid(b) {
		s := string(b)
		return sanitizeString(s)
	}
	// Fallback: map bytes to runes (ISO-8859-1 / Windows-1252 best-effort)
	runes := make([]rune, 0, len(b))
	for _, by := range b {
		runes = append(runes, rune(by))
	}
	return sanitizeString(string(runes))
}

// sanitizeString removes C0 control characters (except newline and carriage return)
// and trims surrounding whitespace.
func sanitizeString(s string) string {
	// remove nulls and other control chars except \n and \r and tab
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if r < 0x20 {
			// skip other control chars
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// CoerceToInt attempts to convert a variety of SNMP-returned value types to
// an integer. It supports numeric types, decimal strings, hex-prefixed strings
// (0x...), and byte-slices containing textual numbers. Returns (value, ok).
func CoerceToInt(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case uint:
		return int64(t), true
	case uint32:
		return int64(t), true
	case uint64:
		return int64(t), true
	case string:
		return parseStringInt(t)
	case []byte:
		return parseStringInt(string(t))
	}
	return 0, false
}

func parseStringInt(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// handle hex with 0x prefix; strconv.ParseInt with 0 base handles 0x
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if i, err := strconv.ParseInt(s, 0, 64); err == nil {
			return i, true
		}
	}
	// try parse as decimal
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, true
	}
	// last-resort: use ParseInt with base 0 to accept 0x or leading 0
	if i, err := strconv.ParseInt(s, 0, 64); err == nil {
		return i, true
	}
	return 0, false
}

// WriteFileAtomic writes data to a temp file in the same directory and atomically
// renames it to the destination path. This avoids partial writes being observed
// by other readers and reduces race conditions when multiple writers target the
// same file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// create temp file
	f, err := os.CreateTemp(dir, "tmpfile-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := f.Name()
	// ensure cleanup on failure
	defer func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}()
	if _, err := io.Copy(f, strings.NewReader(string(data))); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// set permissions and rename
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
