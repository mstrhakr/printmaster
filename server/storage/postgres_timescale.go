package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TimescaleConfig holds configuration for TimescaleDB features.
type TimescaleConfig struct {
	// Enabled controls whether TimescaleDB features are used.
	// If nil, auto-detection is performed.
	Enabled *bool

	// Compression enables automatic compression for chunks older than this duration.
	// Default: 7 days
	CompressionAfter time.Duration

	// RetentionRaw is how long to keep raw (10-second) metrics. Default: 2 hours
	RetentionRaw time.Duration
	// RetentionMinute is how long to keep minute-level aggregates. Default: 7 days
	RetentionMinute time.Duration
	// RetentionHourly is how long to keep hourly aggregates. Default: 90 days
	RetentionHourly time.Duration
	// RetentionDaily is how long to keep daily aggregates. Default: 365 days
	RetentionDaily time.Duration
}

// DefaultTimescaleConfig returns the default TimescaleDB configuration.
func DefaultTimescaleConfig() *TimescaleConfig {
	return &TimescaleConfig{
		CompressionAfter: 7 * 24 * time.Hour,
		RetentionRaw:     ServerMetricsRawRetention,
		RetentionMinute:  ServerMetricsMinuteRetention,
		RetentionHourly:  ServerMetricsHourlyRetention,
		RetentionDaily:   ServerMetricsDailyRetention,
	}
}

// TimescaleSupport provides TimescaleDB-specific operations for PostgreSQL stores.
type TimescaleSupport struct {
	db      *sql.DB
	enabled bool
	config  *TimescaleConfig
}

// NewTimescaleSupport creates a new TimescaleSupport instance.
// It auto-detects whether TimescaleDB is available if config.Enabled is nil.
// If TimescaleDB is available but not yet enabled, it will attempt to create the extension.
func NewTimescaleSupport(db *sql.DB, config *TimescaleConfig) (*TimescaleSupport, error) {
	if config == nil {
		config = DefaultTimescaleConfig()
	}

	ts := &TimescaleSupport{
		db:     db,
		config: config,
	}

	// Check if TimescaleDB extension is already installed
	installed, err := ts.isTimescaleInstalled()
	if err != nil {
		return nil, fmt.Errorf("checking TimescaleDB availability: %w", err)
	}

	// Determine if we should enable TimescaleDB features
	if config.Enabled != nil {
		// Explicit configuration
		if *config.Enabled {
			if !installed {
				// User wants TimescaleDB - try to enable it
				if err := ts.tryCreateExtension(); err != nil {
					return nil, fmt.Errorf("TimescaleDB is explicitly enabled but extension could not be created: %w", err)
				}
				installed = true
				logInfo("TimescaleDB extension created successfully")
			}
			ts.enabled = true
		} else {
			ts.enabled = false
		}
	} else {
		// Auto-detect mode
		logDebug("TimescaleDB auto-detection starting", "already_installed", installed)
		if installed {
			ts.enabled = true
			logDebug("TimescaleDB already installed, enabling features")
		} else {
			// Not installed - try to create it (handles postgres->timescaledb upgrade)
			logDebug("TimescaleDB not installed, attempting to create extension")
			if err := ts.tryCreateExtension(); err == nil {
				// Success! Extension was available but not yet enabled
				ts.enabled = true
				logInfo("TimescaleDB extension auto-enabled (detected available but not installed)")
			} else {
				// Extension not available (plain PostgreSQL) - that's fine
				logDebug("TimescaleDB extension creation failed (plain PostgreSQL assumed)", "error", err)
				ts.enabled = false
			}
		}
	}

	return ts, nil
}

// Enabled returns whether TimescaleDB features are enabled.
func (ts *TimescaleSupport) Enabled() bool {
	return ts.enabled
}

// isTimescaleInstalled checks if the TimescaleDB extension is currently installed.
func (ts *TimescaleSupport) isTimescaleInstalled() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First, list all available extensions for debugging
	rows, err := ts.db.QueryContext(ctx, `SELECT name FROM pg_available_extensions WHERE name LIKE '%timescale%'`)
	if err != nil {
		logDebug("Failed to query available extensions", "error", err)
	} else {
		defer rows.Close()
		var available []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err == nil {
				available = append(available, name)
			}
		}
		logDebug("TimescaleDB availability check", "available_extensions", available)
	}

	// Check installed extensions
	rows2, err := ts.db.QueryContext(ctx, `SELECT extname, extversion FROM pg_extension`)
	if err != nil {
		logDebug("Failed to query installed extensions", "error", err)
	} else {
		defer rows2.Close()
		var installed []string
		for rows2.Next() {
			var name, version string
			if err := rows2.Scan(&name, &version); err == nil {
				installed = append(installed, fmt.Sprintf("%s@%s", name, version))
			}
		}
		logDebug("Currently installed PostgreSQL extensions", "extensions", installed)
	}

	var isInstalled bool
	err = ts.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM pg_extension WHERE extname = 'timescaledb'
		)
	`).Scan(&isInstalled)
	if err != nil {
		logDebug("Failed to check if TimescaleDB is installed", "error", err)
		return false, err
	}

	logDebug("TimescaleDB installation check result", "installed", isInstalled)
	return isInstalled, nil
}

// tryCreateExtension attempts to create the TimescaleDB extension.
// Returns nil on success, error if the extension is not available or creation fails.
func (ts *TimescaleSupport) tryCreateExtension() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logDebug("Attempting to create TimescaleDB extension")

	// Try to create the extension - this will succeed on TimescaleDB images
	// and fail on plain PostgreSQL (extension not available)
	_, err := ts.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE")
	if err != nil {
		logDebug("CREATE EXTENSION failed", "error", err)
		return err
	}

	logDebug("CREATE EXTENSION command succeeded, verifying installation")

	// Verify it was created - use a simple check without the debug logging
	var installed bool
	err = ts.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'timescaledb')`).Scan(&installed)
	if err != nil {
		logDebug("Failed to verify extension creation", "error", err)
		return fmt.Errorf("verifying extension creation: %w", err)
	}
	if !installed {
		logDebug("Extension creation command succeeded but extension not found in pg_extension")
		return fmt.Errorf("extension creation succeeded but extension not found")
	}

	logDebug("TimescaleDB extension created and verified successfully")

	return nil
}

// EnsureExtension creates the TimescaleDB extension if it doesn't exist.
// This requires superuser privileges; returns nil if already installed or if permissions
// are insufficient (assumes DBA will install it).
func (ts *TimescaleSupport) EnsureExtension(ctx context.Context) error {
	if !ts.enabled {
		return nil
	}

	// Try to create extension; ignore errors (DBA may have already created it)
	_, err := ts.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE")
	if err != nil {
		// Check if extension is already installed
		installed, checkErr := ts.isTimescaleInstalled()
		if checkErr == nil && installed {
			return nil // Already installed by DBA
		}
		return fmt.Errorf("failed to create timescaledb extension (may require superuser): %w", err)
	}

	return nil
}

// InitializeHypertables converts the metrics tables to TimescaleDB hypertables.
// This enables automatic time-based partitioning for efficient time-series queries.
// Safe to call multiple times - skips tables already converted.
func (ts *TimescaleSupport) InitializeHypertables(ctx context.Context) error {
	if !ts.enabled {
		return nil
	}

	// Convert metrics_history to hypertable
	if err := ts.createHypertable(ctx, "metrics_history", "timestamp", "1 day"); err != nil {
		return fmt.Errorf("creating metrics_history hypertable: %w", err)
	}

	// Convert server_metrics_history to hypertable
	if err := ts.createHypertable(ctx, "server_metrics_history", "timestamp", "1 hour"); err != nil {
		return fmt.Errorf("creating server_metrics_history hypertable: %w", err)
	}

	return nil
}

// createHypertable converts a table to a TimescaleDB hypertable.
// chunkInterval specifies the time range covered by each chunk (e.g., "1 day", "1 hour").
func (ts *TimescaleSupport) createHypertable(ctx context.Context, table, timeColumn, chunkInterval string) error {
	// Check if already a hypertable
	var isHypertable bool
	err := ts.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM timescaledb_information.hypertables 
			WHERE hypertable_name = $1
		)
	`, table).Scan(&isHypertable)
	if err != nil {
		return fmt.Errorf("checking hypertable status: %w", err)
	}

	if isHypertable {
		logInfo("Table already a hypertable", "table", table)
		return nil
	}

	// Convert to hypertable
	// migrate_data => true: migrate existing data into chunks
	// if_not_exists => true: don't error if already a hypertable
	query := fmt.Sprintf(`
		SELECT create_hypertable('%s', '%s', 
			chunk_time_interval => INTERVAL '%s',
			migrate_data => true,
			if_not_exists => true
		)
	`, table, timeColumn, chunkInterval)

	_, err = ts.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("creating hypertable: %w", err)
	}

	logInfo("Created TimescaleDB hypertable", "table", table, "chunk_interval", chunkInterval)
	return nil
}

// SetupCompression enables compression for older chunks.
// Compressed data uses significantly less storage (up to 95% reduction).
func (ts *TimescaleSupport) SetupCompression(ctx context.Context) error {
	if !ts.enabled {
		return nil
	}

	// Enable compression on server_metrics_history
	if err := ts.enableCompression(ctx, "server_metrics_history", ts.config.CompressionAfter); err != nil {
		return fmt.Errorf("enabling compression on server_metrics_history: %w", err)
	}

	// Enable compression on metrics_history
	if err := ts.enableCompression(ctx, "metrics_history", ts.config.CompressionAfter); err != nil {
		return fmt.Errorf("enabling compression on metrics_history: %w", err)
	}

	return nil
}

// enableCompression sets up compression for a hypertable.
func (ts *TimescaleSupport) enableCompression(ctx context.Context, table string, olderThan time.Duration) error {
	// Check if compression is already enabled
	var compressionEnabled bool
	err := ts.db.QueryRowContext(ctx, `
		SELECT compression_enabled 
		FROM timescaledb_information.hypertables 
		WHERE hypertable_name = $1
	`, table).Scan(&compressionEnabled)
	if err != nil {
		if err == sql.ErrNoRows {
			// Not a hypertable, skip
			return nil
		}
		return fmt.Errorf("checking compression status: %w", err)
	}

	if !compressionEnabled {
		// Enable compression with segmentby on common query columns
		var segmentBy string
		switch table {
		case "server_metrics_history":
			segmentBy = "tier"
		case "metrics_history":
			segmentBy = "serial"
		}

		query := fmt.Sprintf(`
			ALTER TABLE %s SET (
				timescaledb.compress,
				timescaledb.compress_segmentby = '%s',
				timescaledb.compress_orderby = 'timestamp DESC'
			)
		`, table, segmentBy)
		if _, err := ts.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("enabling compression: %w", err)
		}
		logInfo("Enabled compression on hypertable", "table", table)
	}

	// Add compression policy if not exists
	intervalHours := int(olderThan.Hours())
	_, err = ts.db.ExecContext(ctx, `
		SELECT add_compression_policy($1, INTERVAL '1 hour' * $2, if_not_exists => true)
	`, table, intervalHours)
	if err != nil {
		// Policy might already exist with different interval; that's okay
		logWarn("Could not add compression policy (may already exist)", "table", table, "error", err)
	}

	return nil
}

// SetupRetentionPolicies adds automatic data retention policies.
// Old data is automatically dropped based on the configured retention periods.
func (ts *TimescaleSupport) SetupRetentionPolicies(ctx context.Context) error {
	if !ts.enabled {
		return nil
	}

	// For server_metrics_history, we need retention per tier
	// TimescaleDB policies apply to the whole table, so we'll set the longest retention
	// and rely on manual pruning for tier-specific retention (or use continuous aggregates)
	longestRetention := ts.config.RetentionDaily
	if err := ts.addRetentionPolicy(ctx, "server_metrics_history", longestRetention); err != nil {
		return fmt.Errorf("adding retention policy for server_metrics_history: %w", err)
	}

	// For device metrics_history, keep data for 1 year
	if err := ts.addRetentionPolicy(ctx, "metrics_history", ts.config.RetentionDaily); err != nil {
		return fmt.Errorf("adding retention policy for metrics_history: %w", err)
	}

	return nil
}

// addRetentionPolicy adds a retention policy to automatically drop old data.
func (ts *TimescaleSupport) addRetentionPolicy(ctx context.Context, table string, retention time.Duration) error {
	intervalDays := int(retention.Hours() / 24)
	_, err := ts.db.ExecContext(ctx, `
		SELECT add_retention_policy($1, INTERVAL '1 day' * $2, if_not_exists => true)
	`, table, intervalDays)
	if err != nil {
		// Policy might already exist; that's okay
		logWarn("Could not add retention policy (may already exist)", "table", table, "error", err)
	}
	return nil
}

// CreateContinuousAggregates sets up materialized views for hourly/daily rollups.
// These are automatically maintained by TimescaleDB when new data arrives.
func (ts *TimescaleSupport) CreateContinuousAggregates(ctx context.Context) error {
	if !ts.enabled {
		return nil
	}

	// Create hourly aggregate for server metrics
	if err := ts.createServerMetricsHourlyAggregate(ctx); err != nil {
		return fmt.Errorf("creating hourly aggregate: %w", err)
	}

	// Create daily aggregate for server metrics
	if err := ts.createServerMetricsDailyAggregate(ctx); err != nil {
		return fmt.Errorf("creating daily aggregate: %w", err)
	}

	return nil
}

// createServerMetricsHourlyAggregate creates a continuous aggregate for hourly server metrics.
func (ts *TimescaleSupport) createServerMetricsHourlyAggregate(ctx context.Context) error {
	// Check if aggregate already exists
	var exists bool
	err := ts.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM timescaledb_information.continuous_aggregates 
			WHERE view_name = 'server_metrics_hourly_agg'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking continuous aggregate: %w", err)
	}

	if exists {
		logInfo("Continuous aggregate already exists", "name", "server_metrics_hourly_agg")
		return nil
	}

	// Create the continuous aggregate
	// Note: We aggregate JSON fields by taking the last value in the bucket
	_, err = ts.db.ExecContext(ctx, `
		CREATE MATERIALIZED VIEW server_metrics_hourly_agg
		WITH (timescaledb.continuous) AS
		SELECT 
			time_bucket('1 hour', timestamp) AS bucket,
			'hourly' AS tier,
			last(fleet_json, timestamp) AS fleet_json,
			last(server_json, timestamp) AS server_json,
			count(*) AS point_count
		FROM server_metrics_history
		WHERE tier = 'raw'
		GROUP BY bucket
		WITH NO DATA
	`)
	if err != nil {
		return fmt.Errorf("creating continuous aggregate: %w", err)
	}

	// Add refresh policy - refresh hourly data every 30 minutes
	_, err = ts.db.ExecContext(ctx, `
		SELECT add_continuous_aggregate_policy('server_metrics_hourly_agg',
			start_offset => INTERVAL '3 hours',
			end_offset => INTERVAL '1 hour',
			schedule_interval => INTERVAL '30 minutes',
			if_not_exists => true
		)
	`)
	if err != nil {
		logWarn("Could not add continuous aggregate policy", "name", "server_metrics_hourly_agg", "error", err)
	}

	logInfo("Created continuous aggregate", "name", "server_metrics_hourly_agg")
	return nil
}

// createServerMetricsDailyAggregate creates a continuous aggregate for daily server metrics.
func (ts *TimescaleSupport) createServerMetricsDailyAggregate(ctx context.Context) error {
	// Check if aggregate already exists
	var exists bool
	err := ts.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM timescaledb_information.continuous_aggregates 
			WHERE view_name = 'server_metrics_daily_agg'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking continuous aggregate: %w", err)
	}

	if exists {
		logInfo("Continuous aggregate already exists", "name", "server_metrics_daily_agg")
		return nil
	}

	// Create the continuous aggregate from hourly data
	_, err = ts.db.ExecContext(ctx, `
		CREATE MATERIALIZED VIEW server_metrics_daily_agg
		WITH (timescaledb.continuous) AS
		SELECT 
			time_bucket('1 day', timestamp) AS bucket,
			'daily' AS tier,
			last(fleet_json, timestamp) AS fleet_json,
			last(server_json, timestamp) AS server_json,
			count(*) AS point_count
		FROM server_metrics_history
		WHERE tier = 'raw'
		GROUP BY bucket
		WITH NO DATA
	`)
	if err != nil {
		return fmt.Errorf("creating continuous aggregate: %w", err)
	}

	// Add refresh policy - refresh daily data every 6 hours
	_, err = ts.db.ExecContext(ctx, `
		SELECT add_continuous_aggregate_policy('server_metrics_daily_agg',
			start_offset => INTERVAL '3 days',
			end_offset => INTERVAL '1 day',
			schedule_interval => INTERVAL '6 hours',
			if_not_exists => true
		)
	`)
	if err != nil {
		logWarn("Could not add continuous aggregate policy", "name", "server_metrics_daily_agg", "error", err)
	}

	logInfo("Created continuous aggregate", "name", "server_metrics_daily_agg")
	return nil
}

// GetHypertableInfo returns information about TimescaleDB hypertables.
func (ts *TimescaleSupport) GetHypertableInfo(ctx context.Context) ([]HypertableInfo, error) {
	if !ts.enabled {
		return nil, nil
	}

	rows, err := ts.db.QueryContext(ctx, `
		SELECT 
			hypertable_name,
			num_chunks,
			COALESCE(compression_enabled, false) as compression_enabled,
			COALESCE(
				(SELECT pg_size_pretty(hypertable_size(format('%I.%I', hypertable_schema, hypertable_name)::regclass))),
				'0 bytes'
			) as total_size
		FROM timescaledb_information.hypertables
		ORDER BY hypertable_name
	`)
	if err != nil {
		return nil, fmt.Errorf("querying hypertable info: %w", err)
	}
	defer rows.Close()

	var infos []HypertableInfo
	for rows.Next() {
		var info HypertableInfo
		if err := rows.Scan(&info.Name, &info.NumChunks, &info.CompressionEnabled, &info.TotalSize); err != nil {
			continue
		}
		infos = append(infos, info)
	}

	return infos, nil
}

// HypertableInfo contains status information about a TimescaleDB hypertable.
type HypertableInfo struct {
	Name               string `json:"name"`
	NumChunks          int    `json:"num_chunks"`
	CompressionEnabled bool   `json:"compression_enabled"`
	TotalSize          string `json:"total_size"`
}

// CompressChunks manually compresses chunks older than the specified age.
// This is useful for immediate compression without waiting for the policy.
func (ts *TimescaleSupport) CompressChunks(ctx context.Context, table string, olderThan time.Duration) (int, error) {
	if !ts.enabled {
		return 0, nil
	}

	intervalHours := int(olderThan.Hours())
	var compressed int
	err := ts.db.QueryRowContext(ctx, `
		SELECT count(*) FROM (
			SELECT compress_chunk(i, if_not_compressed => true)
			FROM show_chunks($1, older_than => INTERVAL '1 hour' * $2) i
		) t
	`, table, intervalHours).Scan(&compressed)
	if err != nil {
		return 0, fmt.Errorf("compressing chunks: %w", err)
	}

	return compressed, nil
}
