package storage

import (
	"database/sql"
	"testing"
	"time"
)

func TestNullString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  sql.NullString
	}{
		{"empty string returns invalid", "", sql.NullString{String: "", Valid: false}},
		{"non-empty string returns valid", "hello", sql.NullString{String: "hello", Valid: true}},
		{"whitespace is valid", " ", sql.NullString{String: " ", Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullString(tt.input)
			if got.String != tt.want.String || got.Valid != tt.want.Valid {
				t.Errorf("nullString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNullBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  interface{}
	}{
		{"nil returns nil", nil, nil},
		{"empty slice returns nil", []byte{}, nil},
		{"non-empty returns string", []byte("hello"), "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullBytes(tt.input)
			if got != tt.want {
				t.Errorf("nullBytes(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNullInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int64
		want  sql.NullInt64
	}{
		{"zero returns invalid", 0, sql.NullInt64{Int64: 0, Valid: false}},
		{"positive returns valid", 42, sql.NullInt64{Int64: 42, Valid: true}},
		{"negative returns valid", -1, sql.NullInt64{Int64: -1, Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullInt64(tt.input)
			if got.Int64 != tt.want.Int64 || got.Valid != tt.want.Valid {
				t.Errorf("nullInt64(%d) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNullInt64Ptr(t *testing.T) {
	t.Parallel()

	val := int64(42)
	tests := []struct {
		name  string
		input *int64
		want  sql.NullInt64
	}{
		{"nil returns invalid", nil, sql.NullInt64{Int64: 0, Valid: false}},
		{"non-nil returns valid", &val, sql.NullInt64{Int64: 42, Valid: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullInt64Ptr(tt.input)
			if got.Int64 != tt.want.Int64 || got.Valid != tt.want.Valid {
				t.Errorf("nullInt64Ptr(%v) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNullTimePtr(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name  string
		input *time.Time
		valid bool
	}{
		{"nil returns invalid", nil, false},
		{"non-nil returns valid", &now, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullTimePtr(tt.input)
			if got.Valid != tt.valid {
				t.Errorf("nullTimePtr(%v) valid = %v, want %v", tt.input, got.Valid, tt.valid)
			}
			if tt.valid && !got.Time.Equal(now) {
				t.Errorf("nullTimePtr time mismatch: got %v, want %v", got.Time, now)
			}
		})
	}
}

func TestNullTime(t *testing.T) {
	t.Parallel()

	now := time.Now()
	zero := time.Time{}

	tests := []struct {
		name  string
		input time.Time
		valid bool
	}{
		{"zero time returns invalid", zero, false},
		{"non-zero time returns valid", now, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullTime(tt.input)
			if got.Valid != tt.valid {
				t.Errorf("nullTime(%v) valid = %v, want %v", tt.input, got.Valid, tt.valid)
			}
		})
	}
}

func TestGenerateSecureToken(t *testing.T) {
	t.Parallel()

	// Test various lengths
	lengths := []int{16, 24, 32}
	for _, length := range lengths {
		token, err := generateSecureToken(length)
		if err != nil {
			t.Fatalf("generateSecureToken(%d) error = %v", length, err)
		}
		if token == "" {
			t.Errorf("generateSecureToken(%d) returned empty string", length)
		}
		// Base64 encoding increases length
		if len(token) == 0 {
			t.Errorf("generateSecureToken(%d) returned zero-length token", length)
		}
	}

	// Test uniqueness
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateSecureToken(32)
		if err != nil {
			t.Fatalf("generateSecureToken(32) error = %v", err)
		}
		if tokens[token] {
			t.Error("generateSecureToken produced duplicate token")
		}
		tokens[token] = true
	}
}

func TestHashSHA256(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"simple string", "hello"},
		{"longer string", "The quick brown fox jumps over the lazy dog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hashSHA256(tt.input)
			// SHA-256 produces 64 hex characters
			if len(hash) != 64 {
				t.Errorf("hashSHA256(%q) length = %d, want 64", tt.input, len(hash))
			}

			// Same input should produce same output
			hash2 := hashSHA256(tt.input)
			if hash != hash2 {
				t.Error("hashSHA256 not deterministic")
			}
		})
	}

	// Different inputs should produce different hashes
	h1 := hashSHA256("test1")
	h2 := hashSHA256("test2")
	if h1 == h2 {
		t.Error("different inputs produced same hash")
	}
}

// Note: TestTokenHash and TestGetDefaultDBPath are in users_test.go

func TestEncodeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   map[string]any
		valid   bool
		wantErr bool
	}{
		{"nil map returns invalid", nil, false, false},
		{"empty map returns invalid", map[string]any{}, false, false},
		{"non-empty map returns valid", map[string]any{"key": "value"}, true, false},
		{"complex map", map[string]any{"num": 42, "str": "hello", "bool": true}, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeMetadata(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("encodeMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.Valid != tt.valid {
				t.Errorf("encodeMetadata() valid = %v, want %v", got.Valid, tt.valid)
			}
		})
	}
}

func TestDecodeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input sql.NullString
		want  bool // Whether result should be non-nil
	}{
		{"invalid nullstring returns nil", sql.NullString{Valid: false}, false},
		{"empty string returns nil", sql.NullString{String: "", Valid: true}, false},
		{"valid JSON returns map", sql.NullString{String: `{"key":"value"}`, Valid: true}, true},
		{"invalid JSON returns nil", sql.NullString{String: "not json", Valid: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeMetadata(tt.input)
			if (got != nil) != tt.want {
				t.Errorf("decodeMetadata() = %v, want non-nil = %v", got, tt.want)
			}
		})
	}
}
