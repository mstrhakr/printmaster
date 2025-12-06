package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"printmaster/common/config"
	// Import postgres driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresStore implements Store interface for PostgreSQL.
type PostgresStore struct {
	BaseStore
}

// NewPostgresStore creates a new PostgreSQL store.
func NewPostgresStore(cfg *config.DatabaseConfig) (*PostgresStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config required")
	}

	dsn := cfg.BuildDSN()
	if dsn == "" {
		return nil, fmt.Errorf("invalid database configuration: could not build DSN")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Configure connection pool
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetimeSecs > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSecs) * time.Second)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	store := &PostgresStore{
		BaseStore: BaseStore{
			db:      db,
			dialect: &PostgresDialect{},
		},
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize postgres schema: %w", err)
	}

	return store, nil
}

// initSchema creates the database schema for PostgreSQL.
func (s *PostgresStore) initSchema() error {
	// TODO: Implement PostgreSQL schema initialization
	// This will be similar to SQLite but using PostgreSQL-specific DDL:
	// - SERIAL/BIGSERIAL instead of INTEGER PRIMARY KEY AUTOINCREMENT
	// - TIMESTAMP instead of TEXT/INTEGER for dates
	// - TEXT instead of BLOB for binary data
	// - ON CONFLICT DO UPDATE instead of ON CONFLICT(...) DO UPDATE

	return fmt.Errorf("PostgreSQL schema initialization not yet implemented")
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Path returns an empty string since PostgreSQL doesn't use file paths.
func (s *PostgresStore) Path() string {
	return ""
}
