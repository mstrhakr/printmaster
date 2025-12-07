package storage

import (
	"testing"
)

func TestConvertPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no placeholders", "SELECT * FROM users", "SELECT * FROM users"},
		{"single placeholder", "SELECT * FROM users WHERE id = ?", "SELECT * FROM users WHERE id = $1"},
		{"multiple placeholders", "INSERT INTO users (a, b, c) VALUES (?, ?, ?)", "INSERT INTO users (a, b, c) VALUES ($1, $2, $3)"},
		{"mixed content", "SELECT * FROM users WHERE name = ? AND age > ? ORDER BY ?", "SELECT * FROM users WHERE name = $1 AND age > $2 ORDER BY $3"},
		{"empty string", "", ""},
		{"just question mark", "?", "$1"},
		{"question marks with text between", "? AND ?", "$1 AND $2"},
		{"ten placeholders", "?, ?, ?, ?, ?, ?, ?, ?, ?, ?", "$1, $2, $3, $4, $5, $6, $7, $8, $9, $10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertPlaceholders(tt.input)
			if got != tt.want {
				t.Errorf("ConvertPlaceholders(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPlaceholderSet(t *testing.T) {
	t.Parallel()

	sqliteDialect := &SQLiteDialect{}
	postgresDialect := &PostgresDialect{}

	tests := []struct {
		name       string
		dialect    Dialect
		count      int
		startIndex int
		want       string
	}{
		// SQLite tests
		{"sqlite zero count", sqliteDialect, 0, 1, ""},
		{"sqlite negative count", sqliteDialect, -1, 1, ""},
		{"sqlite single placeholder", sqliteDialect, 1, 1, "?"},
		{"sqlite three placeholders", sqliteDialect, 3, 1, "?, ?, ?"},
		{"sqlite five placeholders", sqliteDialect, 5, 1, "?, ?, ?, ?, ?"},
		{"sqlite different start index", sqliteDialect, 3, 5, "?, ?, ?"},

		// PostgreSQL tests
		{"postgres zero count", postgresDialect, 0, 1, ""},
		{"postgres negative count", postgresDialect, -1, 1, ""},
		{"postgres single placeholder", postgresDialect, 1, 1, "$1"},
		{"postgres three placeholders", postgresDialect, 3, 1, "$1, $2, $3"},
		{"postgres five placeholders", postgresDialect, 5, 1, "$1, $2, $3, $4, $5"},
		{"postgres different start index", postgresDialect, 3, 5, "$5, $6, $7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlaceholderSet(tt.dialect, tt.count, tt.startIndex)
			if got != tt.want {
				t.Errorf("PlaceholderSet(%s, %d, %d) = %q, want %q", tt.dialect.Name(), tt.count, tt.startIndex, got, tt.want)
			}
		})
	}
}

func TestSQLiteDialect_ReturningClause_Empty(t *testing.T) {
	t.Parallel()

	d := SQLiteDialect{}

	// Test empty columns
	got := d.ReturningClause()
	if got != "" {
		t.Errorf("ReturningClause() with no columns = %q, want empty", got)
	}
}

func TestPostgresDialect_ReturningClause_Empty(t *testing.T) {
	t.Parallel()

	d := PostgresDialect{}

	// Test empty columns
	got := d.ReturningClause()
	if got != "" {
		t.Errorf("ReturningClause() with no columns = %q, want empty", got)
	}
}

func TestPostgresDialect_LimitOffset_WithOffset(t *testing.T) {
	t.Parallel()

	d := PostgresDialect{}

	// Test with offset
	got := d.LimitOffset(10, 20)
	if got == "" {
		t.Error("LimitOffset with offset returned empty string")
	}
	// Should contain both LIMIT and OFFSET
	if got != "LIMIT 10 OFFSET 20" {
		t.Errorf("LimitOffset(10, 20) = %q, want 'LIMIT 10 OFFSET 20'", got)
	}
}

func TestPostgresDialect_LimitOffset_ZeroOffset(t *testing.T) {
	t.Parallel()

	d := PostgresDialect{}

	// Test with zero offset
	got := d.LimitOffset(10, 0)
	// Should only have LIMIT, no OFFSET
	if got != "LIMIT 10" {
		t.Errorf("LimitOffset(10, 0) = %q, want 'LIMIT 10'", got)
	}
}
