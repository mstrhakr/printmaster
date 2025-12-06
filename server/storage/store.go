package storage

import (
	"fmt"

	"printmaster/common/config"
)

// NewStore creates a new Store implementation based on the database configuration.
// It supports SQLite (default), PostgreSQL, and MySQL/MariaDB backends.
//
// For SQLite: uses Path from config or defaults to "printmaster.db"
// For PostgreSQL/MySQL: uses DSN or builds connection string from Host, Port, User, Password, Name
//
// Example usage:
//
//	cfg := &config.DatabaseConfig{Driver: "postgres", Host: "localhost", Name: "printmaster"}
//	store, err := NewStore(cfg)
func NewStore(cfg *config.DatabaseConfig) (Store, error) {
	if cfg == nil {
		cfg = &config.DatabaseConfig{}
	}

	driver := cfg.EffectiveDriver()
	dsn := cfg.BuildDSN()

	switch driver {
	case "sqlite", "sqlite3", "modernc", "modernc-sqlite":
		// Default SQLite path handling
		path := dsn
		if path == "" {
			path = cfg.Path
		}
		if path == "" {
			path = "printmaster.db"
		}
		return NewSQLiteStore(path)

	case "postgres", "postgresql":
		// PostgreSQL support is in development - schema and methods are being ported
		return nil, fmt.Errorf("PostgreSQL support is not yet complete; use sqlite for now")

	case "mysql", "mariadb":
		return nil, fmt.Errorf("MySQL/MariaDB support is not yet implemented")

	default:
		return nil, fmt.Errorf("unsupported database driver: %q (supported: sqlite, postgres, mysql)", driver)
	}
}
