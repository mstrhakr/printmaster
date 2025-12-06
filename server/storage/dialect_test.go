package storage

import (
	"testing"
)

func TestSQLiteDialect(t *testing.T) {
	t.Parallel()

	d := SQLiteDialect{}

	t.Run("Name", func(t *testing.T) {
		if got := d.Name(); got != "sqlite" {
			t.Errorf("Name() = %q, want %q", got, "sqlite")
		}
	})

	t.Run("Placeholder", func(t *testing.T) {
		tests := []struct {
			index int
			want  string
		}{
			{1, "?"},
			{5, "?"},
			{10, "?"},
		}
		for _, tt := range tests {
			got := d.Placeholder(tt.index)
			if got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.index, got, tt.want)
			}
		}
	})

	t.Run("AutoIncrement", func(t *testing.T) {
		if got := d.AutoIncrement(false); got == "" {
			t.Error("AutoIncrement(false) returned empty string")
		}
		if got := d.AutoIncrement(true); got == "" {
			t.Error("AutoIncrement(true) returned empty string")
		}
	})

	t.Run("CurrentTimestamp", func(t *testing.T) {
		got := d.CurrentTimestamp()
		if got == "" {
			t.Error("CurrentTimestamp() returned empty string")
		}
	})

	t.Run("TimestampType", func(t *testing.T) {
		got := d.TimestampType()
		if got == "" {
			t.Error("TimestampType() returned empty string")
		}
	})

	t.Run("BoolType", func(t *testing.T) {
		got := d.BoolType()
		if got == "" {
			t.Error("BoolType() returned empty string")
		}
	})

	t.Run("TextType", func(t *testing.T) {
		got := d.TextType()
		if got == "" {
			t.Error("TextType() returned empty string")
		}
	})

	t.Run("IntegerType", func(t *testing.T) {
		if got := d.IntegerType(false); got == "" {
			t.Error("IntegerType(false) returned empty string")
		}
		if got := d.IntegerType(true); got == "" {
			t.Error("IntegerType(true) returned empty string")
		}
	})

	t.Run("UpsertConflict", func(t *testing.T) {
		got := d.UpsertConflict([]string{"id"})
		if got == "" {
			t.Error("UpsertConflict returned empty string")
		}
	})

	t.Run("ReturningClause", func(t *testing.T) {
		got := d.ReturningClause("id")
		if got == "" {
			t.Error("ReturningClause returned empty string")
		}
		got = d.ReturningClause("id", "name")
		if got == "" {
			t.Error("ReturningClause with multiple columns returned empty string")
		}
	})

	t.Run("LimitOffset", func(t *testing.T) {
		got := d.LimitOffset(10, 0)
		if got == "" {
			t.Error("LimitOffset returned empty string")
		}
		got = d.LimitOffset(10, 20)
		if got == "" {
			t.Error("LimitOffset with offset returned empty string")
		}
	})

	t.Run("ILike", func(t *testing.T) {
		got := d.ILike("name", 1)
		if got == "" {
			t.Error("ILike returned empty string")
		}
	})

	t.Run("NullSafeEqual", func(t *testing.T) {
		got := d.NullSafeEqual("col", 1)
		if got == "" {
			t.Error("NullSafeEqual returned empty string")
		}
	})

	t.Run("JSONExtract", func(t *testing.T) {
		got := d.JSONExtract("data", "key")
		if got == "" {
			t.Error("JSONExtract returned empty string")
		}
	})

	t.Run("ForUpdate", func(t *testing.T) {
		// SQLite may return empty string for ForUpdate (not supported)
		_ = d.ForUpdate()
	})
}

func TestPostgresDialect(t *testing.T) {
	t.Parallel()

	d := PostgresDialect{}

	t.Run("Name", func(t *testing.T) {
		if got := d.Name(); got != "postgres" {
			t.Errorf("Name() = %q, want %q", got, "postgres")
		}
	})

	t.Run("Placeholder", func(t *testing.T) {
		tests := []struct {
			index int
			want  string
		}{
			{1, "$1"},
			{5, "$5"},
			{10, "$10"},
			{100, "$100"},
		}
		for _, tt := range tests {
			got := d.Placeholder(tt.index)
			if got != tt.want {
				t.Errorf("Placeholder(%d) = %q, want %q", tt.index, got, tt.want)
			}
		}
	})

	t.Run("AutoIncrement", func(t *testing.T) {
		got := d.AutoIncrement(false)
		if got == "" {
			t.Error("AutoIncrement(false) returned empty string")
		}
		got = d.AutoIncrement(true)
		if got == "" {
			t.Error("AutoIncrement(true) returned empty string")
		}
	})

	t.Run("CurrentTimestamp", func(t *testing.T) {
		got := d.CurrentTimestamp()
		if got == "" {
			t.Error("CurrentTimestamp() returned empty string")
		}
	})

	t.Run("TimestampType", func(t *testing.T) {
		got := d.TimestampType()
		if got == "" {
			t.Error("TimestampType() returned empty string")
		}
	})

	t.Run("BoolType", func(t *testing.T) {
		got := d.BoolType()
		if got == "" {
			t.Error("BoolType() returned empty string")
		}
	})

	t.Run("TextType", func(t *testing.T) {
		got := d.TextType()
		if got == "" {
			t.Error("TextType() returned empty string")
		}
	})

	t.Run("IntegerType", func(t *testing.T) {
		if got := d.IntegerType(false); got == "" {
			t.Error("IntegerType(false) returned empty string")
		}
		if got := d.IntegerType(true); got == "" {
			t.Error("IntegerType(true) returned empty string")
		}
	})

	t.Run("UpsertConflict", func(t *testing.T) {
		got := d.UpsertConflict([]string{"id"})
		if got == "" {
			t.Error("UpsertConflict returned empty string")
		}
	})

	t.Run("ReturningClause", func(t *testing.T) {
		got := d.ReturningClause("id")
		if got == "" {
			t.Error("ReturningClause returned empty string")
		}
	})

	t.Run("LimitOffset", func(t *testing.T) {
		got := d.LimitOffset(10, 0)
		if got == "" {
			t.Error("LimitOffset returned empty string")
		}
	})

	t.Run("ILike", func(t *testing.T) {
		got := d.ILike("name", 1)
		if got == "" {
			t.Error("ILike returned empty string")
		}
	})

	t.Run("NullSafeEqual", func(t *testing.T) {
		got := d.NullSafeEqual("col", 1)
		if got == "" {
			t.Error("NullSafeEqual returned empty string")
		}
	})

	t.Run("JSONExtract", func(t *testing.T) {
		got := d.JSONExtract("data", "key")
		if got == "" {
			t.Error("JSONExtract returned empty string")
		}
	})

	t.Run("ForUpdate", func(t *testing.T) {
		got := d.ForUpdate()
		if got == "" {
			t.Error("ForUpdate returned empty string")
		}
	})
}

func TestDialectInterfaceCompliance(t *testing.T) {
	t.Parallel()

	// Ensure both dialects implement the Dialect interface
	var _ Dialect = &SQLiteDialect{}
	var _ Dialect = &PostgresDialect{}
}
