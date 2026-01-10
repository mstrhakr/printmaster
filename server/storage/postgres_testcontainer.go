//go:build integration

package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"printmaster/common/config"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresTestContainer holds a running Postgres container for testing
type PostgresTestContainer struct {
	Container testcontainers.Container
	DSN       string
}

// NewPostgresTestContainer creates a new Postgres container for testing.
// It returns the container and a cleanup function that should be called
// when the test is complete.
func NewPostgresTestContainer(t *testing.T) (*PostgresTestContainer, func()) {
	t.Helper()

	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("printmaster_test"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	// Get connection string
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	cleanup := func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	}

	return &PostgresTestContainer{
		Container: pgContainer,
		DSN:       connStr,
	}, cleanup
}

// NewPostgresStoreFromContainer creates a PostgresStore connected to the test container
func NewPostgresStoreFromContainer(t *testing.T, container *PostgresTestContainer) *PostgresStore {
	t.Helper()

	cfg := &config.DatabaseConfig{
		Driver: "postgres",
		DSN:    container.DSN,
	}

	store, err := NewPostgresStore(cfg)
	if err != nil {
		t.Fatalf("failed to create PostgresStore: %v", err)
	}

	return store
}

// SkipIfNoDocker skips the test if Docker is not available
func SkipIfNoDocker(t *testing.T) {
	t.Helper()

	// Catch any panics from testcontainers (e.g., "rootless Docker is not supported on Windows")
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("Docker not available (panic recovered): %v", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("Docker not available, skipping integration test: %v", err)
		return
	}
	defer provider.Close()

	// Try to ping Docker
	_, err = provider.Client().Ping(ctx)
	if err != nil {
		t.Skipf("Docker not responding, skipping integration test: %v", err)
	}
}

// WithPostgresStore is a test helper that creates a Postgres container,
// initializes a store, runs the test function, and cleans up.
func WithPostgresStore(t *testing.T, testFn func(t *testing.T, store *PostgresStore)) {
	t.Helper()

	SkipIfNoDocker(t)

	container, cleanup := NewPostgresTestContainer(t)
	defer cleanup()

	store := NewPostgresStoreFromContainer(t, container)
	defer store.Close()

	testFn(t, store)
}

// PostgresTestDSN returns a test DSN for use with manual container management
func PostgresTestDSN(host string, port int) string {
	return fmt.Sprintf("postgres://testuser:testpass@%s:%d/printmaster_test?sslmode=disable", host, port)
}

// TimescaleDBTestContainer holds a running TimescaleDB container for testing
type TimescaleDBTestContainer struct {
	Container testcontainers.Container
	DSN       string
}

// NewTimescaleDBTestContainer creates a new TimescaleDB container for testing.
// This uses the official TimescaleDB image which includes PostgreSQL with the
// TimescaleDB extension pre-installed.
func NewTimescaleDBTestContainer(t *testing.T) (*TimescaleDBTestContainer, func()) {
	t.Helper()

	ctx := context.Background()

	// Use the official TimescaleDB image with PostgreSQL 16
	pgContainer, err := postgres.Run(ctx,
		"timescale/timescaledb:latest-pg16",
		postgres.WithDatabase("printmaster_test"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start TimescaleDB container: %v", err)
	}

	// Get connection string
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		pgContainer.Terminate(ctx)
		t.Fatalf("failed to get connection string: %v", err)
	}

	cleanup := func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate TimescaleDB container: %v", err)
		}
	}

	return &TimescaleDBTestContainer{
		Container: pgContainer,
		DSN:       connStr,
	}, cleanup
}

// NewTimescaleDBStoreFromContainer creates a PostgresStore connected to the TimescaleDB test container
func NewTimescaleDBStoreFromContainer(t *testing.T, container *TimescaleDBTestContainer) *PostgresStore {
	t.Helper()

	cfg := &config.DatabaseConfig{
		Driver: "postgres",
		DSN:    container.DSN,
	}

	store, err := NewPostgresStore(cfg)
	if err != nil {
		t.Fatalf("failed to create PostgresStore with TimescaleDB: %v", err)
	}

	return store
}

// WithTimescaleDBStore is a test helper that creates a TimescaleDB container,
// initializes a store, runs the test function, and cleans up.
func WithTimescaleDBStore(t *testing.T, testFn func(t *testing.T, store *PostgresStore)) {
	t.Helper()

	SkipIfNoDocker(t)

	container, cleanup := NewTimescaleDBTestContainer(t)
	defer cleanup()

	store := NewTimescaleDBStoreFromContainer(t, container)
	defer store.Close()

	testFn(t, store)
}
