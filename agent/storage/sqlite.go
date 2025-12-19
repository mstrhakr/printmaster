package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	commonstorage "printmaster/common/storage"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Logger interface for storage operations
type Logger interface {
	Error(msg string, context ...interface{})
	Warn(msg string, context ...interface{})
	Info(msg string, context ...interface{})
	Debug(msg string, context ...interface{})
	WarnRateLimited(key string, interval time.Duration, msg string, context ...interface{})
}

// Global logger for storage package
var storageLogger Logger

// SetLogger sets the logger for the storage package
func SetLogger(logger Logger) {
	storageLogger = logger
}

// SQLiteStore implements DeviceStore using SQLite
type SQLiteStore struct {
	db     *sql.DB
	dbPath string // Store path for backup operations
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// NewSQLiteStore creates a new SQLite-based device store
// If dbPath is empty, uses in-memory database (:memory:)
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	return NewSQLiteStoreWithConfig(dbPath, nil)
}

// NewSQLiteStoreWithConfig creates a new SQLite-based device store with optional config store
// for tracking rotation events. If configStore is provided, rotation events will set a flag
// that the UI can use to warn users.
func NewSQLiteStoreWithConfig(dbPath string, configStore AgentConfigStore) (*SQLiteStore, error) {
	if dbPath == "" {
		dbPath = ":memory:"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool settings for SQLite:
	// - MaxOpenConns: Allow multiple connections for reads (WAL mode supports this)
	// - MaxIdleConns: Keep some connections ready to reduce open/close overhead
	// - ConnMaxLifetime: Prevent stale connections
	// Note: SQLite handles write serialization internally with busy_timeout
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)

	// Enable foreign keys and set pragmas for better performance
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 30000", // 30 second timeout for busy retries
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA cache_size = -64000", // 64MB cache
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma: %w", err)
		}
	}

	store := &SQLiteStore{
		db:     db,
		dbPath: dbPath,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()

		// If schema initialization fails and we have a real database file (not in-memory),
		// rotate the corrupted database and try again with a fresh one
		if dbPath != ":memory:" {
			if storageLogger != nil {
				storageLogger.Error("Database schema initialization failed, attempting to rotate database",
					"error", err, "path", dbPath)
			}

			backupPath, rotateErr := RotateDatabase(dbPath, configStore)
			if rotateErr != nil {
				return nil, fmt.Errorf("failed to initialize schema and unable to rotate database: %w (rotation error: %v)", err, rotateErr)
			}

			if storageLogger != nil {
				storageLogger.Warn("Database rotated due to migration failure - starting with fresh database",
					"backupPath", backupPath,
					"newPath", dbPath,
					"originalError", err.Error())
			}

			// Try to open the new database (pass config store through)
			return NewSQLiteStoreWithConfig(dbPath, configStore)
		}

		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Run auto-migration
	if err := store.autoMigrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate schema: %w", err)
	}

	// Seed default metrics for devices without any
	if err := store.seedDefaultMetrics(); err != nil {
		if storageLogger != nil {
			storageLogger.Warn("Failed to seed default metrics", "error", err)
		}
		// Non-fatal, continue
	}

	return store, nil
}

// initSchema creates the database schema
func (s *SQLiteStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS devices (
		serial TEXT PRIMARY KEY,
		ip TEXT NOT NULL,
		manufacturer TEXT,
		model TEXT,
		hostname TEXT,
		firmware TEXT,
		mac_address TEXT,
		subnet_mask TEXT,
		gateway TEXT,
		dns_servers TEXT,
		dhcp_server TEXT,
		page_count INTEGER DEFAULT 0,
		toner_levels TEXT,
		consumables TEXT,
		status_messages TEXT,
		last_seen DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		first_seen DATETIME NOT NULL,
		is_saved BOOLEAN DEFAULT 0,
		visible BOOLEAN DEFAULT 1,
		discovery_method TEXT,
		walk_filename TEXT,
		last_scan_id INTEGER,
		raw_data TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_devices_is_saved ON devices(is_saved);
	CREATE INDEX IF NOT EXISTS idx_devices_visible ON devices(visible);
	CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
	CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);
	CREATE INDEX IF NOT EXISTS idx_devices_manufacturer ON devices(manufacturer);

	CREATE TABLE IF NOT EXISTS scan_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ip TEXT NOT NULL,
		hostname TEXT,
		firmware TEXT,
		consumables TEXT,
		status_messages TEXT,
		discovery_method TEXT,
		walk_filename TEXT,
		raw_data TEXT,
		FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_scan_history_serial ON scan_history(serial);
	CREATE INDEX IF NOT EXISTS idx_scan_history_created ON scan_history(created_at);

	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL
	);

	-- Raw metrics: high-resolution 5-minute data, retained for 7 days
	CREATE TABLE IF NOT EXISTS metrics_raw (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		page_count INTEGER DEFAULT 0,
		color_pages INTEGER DEFAULT 0,
		mono_pages INTEGER DEFAULT 0,
		scan_count INTEGER DEFAULT 0,
		toner_levels TEXT,
		FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
	);

	-- Hourly aggregates: 1-hour buckets, retained for 30 days
	CREATE TABLE IF NOT EXISTS metrics_hourly (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		hour_start DATETIME NOT NULL,  -- Start of the hour bucket
		sample_count INTEGER DEFAULT 0,  -- Number of raw samples aggregated
		page_count_min INTEGER DEFAULT 0,
		page_count_max INTEGER DEFAULT 0,
		page_count_avg INTEGER DEFAULT 0,
		color_pages_min INTEGER DEFAULT 0,
		color_pages_max INTEGER DEFAULT 0,
		color_pages_avg INTEGER DEFAULT 0,
		mono_pages_min INTEGER DEFAULT 0,
		mono_pages_max INTEGER DEFAULT 0,
		mono_pages_avg INTEGER DEFAULT 0,
		scan_count_min INTEGER DEFAULT 0,
		scan_count_max INTEGER DEFAULT 0,
		scan_count_avg INTEGER DEFAULT 0,
		toner_levels_avg TEXT,  -- JSON with averaged toner levels
		FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
		UNIQUE(serial, hour_start)  -- One aggregate per device per hour
	);

	-- Daily aggregates: 1-day buckets, retained for 365 days
	CREATE TABLE IF NOT EXISTS metrics_daily (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		day_start DATETIME NOT NULL,  -- Start of the day (midnight UTC)
		sample_count INTEGER DEFAULT 0,  -- Number of hourly samples aggregated
		page_count_min INTEGER DEFAULT 0,
		page_count_max INTEGER DEFAULT 0,
		page_count_avg INTEGER DEFAULT 0,
		color_pages_min INTEGER DEFAULT 0,
		color_pages_max INTEGER DEFAULT 0,
		color_pages_avg INTEGER DEFAULT 0,
		mono_pages_min INTEGER DEFAULT 0,
		mono_pages_max INTEGER DEFAULT 0,
		mono_pages_avg INTEGER DEFAULT 0,
		scan_count_min INTEGER DEFAULT 0,
		scan_count_max INTEGER DEFAULT 0,
		scan_count_avg INTEGER DEFAULT 0,
		toner_levels_avg TEXT,
		FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
		UNIQUE(serial, day_start)
	);

	-- Monthly aggregates: 1-month buckets, retained forever
	CREATE TABLE IF NOT EXISTS metrics_monthly (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		month_start DATETIME NOT NULL,  -- Start of the month (first day, midnight UTC)
		sample_count INTEGER DEFAULT 0,  -- Number of daily samples aggregated
		page_count_min INTEGER DEFAULT 0,
		page_count_max INTEGER DEFAULT 0,
		page_count_avg INTEGER DEFAULT 0,
		color_pages_min INTEGER DEFAULT 0,
		color_pages_max INTEGER DEFAULT 0,
		color_pages_avg INTEGER DEFAULT 0,
		mono_pages_min INTEGER DEFAULT 0,
		mono_pages_max INTEGER DEFAULT 0,
		mono_pages_avg INTEGER DEFAULT 0,
		scan_count_min INTEGER DEFAULT 0,
		scan_count_max INTEGER DEFAULT 0,
		scan_count_avg INTEGER DEFAULT 0,
		toner_levels_avg TEXT,
		FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
		UNIQUE(serial, month_start)
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_raw_serial ON metrics_raw(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_raw_timestamp ON metrics_raw(timestamp);
	CREATE INDEX IF NOT EXISTS idx_metrics_raw_serial_timestamp ON metrics_raw(serial, timestamp);
	
	CREATE INDEX IF NOT EXISTS idx_metrics_hourly_serial ON metrics_hourly(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_hourly_hour_start ON metrics_hourly(hour_start);
	CREATE INDEX IF NOT EXISTS idx_metrics_hourly_serial_hour ON metrics_hourly(serial, hour_start);
	
	CREATE INDEX IF NOT EXISTS idx_metrics_daily_serial ON metrics_daily(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_daily_day_start ON metrics_daily(day_start);
	CREATE INDEX IF NOT EXISTS idx_metrics_daily_serial_day ON metrics_daily(serial, day_start);
	
	CREATE INDEX IF NOT EXISTS idx_metrics_monthly_serial ON metrics_monthly(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_monthly_month_start ON metrics_monthly(month_start);
	CREATE INDEX IF NOT EXISTS idx_metrics_monthly_serial_month ON metrics_monthly(serial, month_start);

	-- Local printers discovered via Windows print spooler
	CREATE TABLE IF NOT EXISTS local_printers (
		name TEXT PRIMARY KEY,
		port_name TEXT NOT NULL,
		driver_name TEXT,
		printer_type TEXT NOT NULL,
		is_default BOOLEAN DEFAULT 0,
		is_shared BOOLEAN DEFAULT 0,
		manufacturer TEXT,
		model TEXT,
		serial_number TEXT,
		status TEXT DEFAULT 'unknown',
		first_seen DATETIME NOT NULL,
		last_seen DATETIME NOT NULL,
		total_pages INTEGER DEFAULT 0,
		total_color_pages INTEGER DEFAULT 0,
		total_mono_pages INTEGER DEFAULT 0,
		baseline_pages INTEGER DEFAULT 0,
		last_page_update DATETIME,
		tracking_enabled BOOLEAN DEFAULT 0,
		asset_number TEXT,
		location TEXT,
		description TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_local_printers_type ON local_printers(printer_type);
	CREATE INDEX IF NOT EXISTS idx_local_printers_tracking ON local_printers(tracking_enabled);

	-- Print job history for local printers
	CREATE TABLE IF NOT EXISTS local_print_jobs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		printer_name TEXT NOT NULL,
		job_id INTEGER NOT NULL,
		document_name TEXT,
		user_name TEXT,
		machine_name TEXT,
		total_pages INTEGER DEFAULT 0,
		pages_printed INTEGER DEFAULT 0,
		is_color BOOLEAN DEFAULT 0,
		size_bytes INTEGER DEFAULT 0,
		submitted_at DATETIME NOT NULL,
		completed_at DATETIME,
		status TEXT DEFAULT 'completed',
		FOREIGN KEY (printer_name) REFERENCES local_printers(name) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_local_print_jobs_printer ON local_print_jobs(printer_name);
	CREATE INDEX IF NOT EXISTS idx_local_print_jobs_submitted ON local_print_jobs(submitted_at);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Run migrations if needed
	if err := s.runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// runMigrations handles schema migrations for existing databases
func (s *SQLiteStore) runMigrations() error {
	// Check current version
	var currentVersion int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		// Table might not exist yet, treat as version 0
		currentVersion = 0
	}

	// Migration 1 -> 2: Add visible and first_seen columns
	if currentVersion < 2 {
		// Check if devices table exists
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// Add visible column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN visible BOOLEAN DEFAULT 1`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add visible column: %w", err)
			}

			// Add first_seen column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN first_seen DATETIME`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add first_seen column: %w", err)
			}

			// Populate first_seen with created_at for existing records
			_, err = s.db.Exec(`UPDATE devices SET first_seen = created_at WHERE first_seen IS NULL`)
			if err != nil {
				return fmt.Errorf("failed to populate first_seen: %w", err)
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (2, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 2 -> 3: Add asset_number, location, and web_ui_url columns
	if currentVersion < 3 {
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// Add asset_number column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN asset_number TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add asset_number column: %w", err)
			}

			// Add location column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN location TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add location column: %w", err)
			}

			// Add web_ui_url column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN web_ui_url TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add web_ui_url column: %w", err)
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (3, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 3 -> 4: Add locked_fields column for field locking
	if currentVersion < 4 {
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// Add locked_fields column (JSON) if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN locked_fields TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add locked_fields column: %w", err)
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (4, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 4 -> 5: Add detailed impression counter fields to metrics_history
	if currentVersion < 5 {
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='metrics_history'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// Add new counter columns
			columns := []string{
				"fax_pages INTEGER DEFAULT 0",
				"copy_pages INTEGER DEFAULT 0",
				"other_pages INTEGER DEFAULT 0",
				"copy_mono_pages INTEGER DEFAULT 0",
				"copy_flatbed_scans INTEGER DEFAULT 0",
				"copy_adf_scans INTEGER DEFAULT 0",
				"fax_flatbed_scans INTEGER DEFAULT 0",
				"fax_adf_scans INTEGER DEFAULT 0",
				"scan_to_host_flatbed INTEGER DEFAULT 0",
				"scan_to_host_adf INTEGER DEFAULT 0",
				"duplex_sheets INTEGER DEFAULT 0",
				"jam_events INTEGER DEFAULT 0",
				"scanner_jam_events INTEGER DEFAULT 0",
			}

			for _, col := range columns {
				_, err = s.db.Exec(fmt.Sprintf(`ALTER TABLE metrics_history ADD COLUMN %s`, col))
				if err != nil && !strings.Contains(err.Error(), "duplicate column") {
					return fmt.Errorf("failed to add column %s: %w", col, err)
				}
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (5, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 5 -> 6: Add description column to devices table
	if currentVersion < 6 {
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// Add description column if it doesn't exist
			_, err = s.db.Exec(`ALTER TABLE devices ADD COLUMN description TEXT`)
			if err != nil && !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("failed to add description column: %w", err)
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (6, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 6 -> 7: Remove page_count and toner_levels from devices table
	// These fields now live exclusively in metrics_history table
	if currentVersion < 7 {
		var tableExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='devices'").Scan(&tableExists)
		if err == nil && tableExists > 0 {
			// SQLite doesn't support DROP COLUMN directly, so we need to recreate the table
			// First, check if columns exist
			var hasPageCount, hasTonerLevels bool
			rows, err := s.db.Query("PRAGMA table_info(devices)")
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var cid int
					var name string
					var ctype string
					var notnull int
					var dfltValue interface{}
					var pk int
					if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err == nil {
						if name == "page_count" {
							hasPageCount = true
						}
						if name == "toner_levels" {
							hasTonerLevels = true
						}
					}
				}
			}

			// Only migrate if columns exist
			if hasPageCount || hasTonerLevels {
				// Create new table without page_count and toner_levels
				_, err = s.db.Exec(`
					CREATE TABLE devices_new (
						serial TEXT PRIMARY KEY,
						ip TEXT NOT NULL,
						manufacturer TEXT,
						model TEXT,
						hostname TEXT,
						firmware TEXT,
						mac_address TEXT,
						subnet_mask TEXT,
						gateway TEXT,
						dns_servers TEXT,
						dhcp_server TEXT,
						consumables TEXT,
						status_messages TEXT,
						last_seen DATETIME NOT NULL,
						created_at DATETIME NOT NULL,
						first_seen DATETIME NOT NULL,
						is_saved BOOLEAN DEFAULT 0,
						visible BOOLEAN DEFAULT 1,
						discovery_method TEXT,
						walk_filename TEXT,
						last_scan_id INTEGER,
						asset_number TEXT,
						location TEXT,
						description TEXT,
						web_ui_url TEXT,
						locked_fields TEXT,
						raw_data TEXT
					)
				`)
				if err != nil {
					return fmt.Errorf("failed to create devices_new table: %w", err)
				}

				// Copy data (excluding page_count and toner_levels)
				_, err = s.db.Exec(`
					INSERT INTO devices_new 
					SELECT serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
					       consumables, status_messages, last_seen, created_at, first_seen, is_saved, visible, discovery_method, walk_filename,
					       last_scan_id, asset_number, location, description, web_ui_url, locked_fields, raw_data
					FROM devices
				`)
				if err != nil {
					return fmt.Errorf("failed to copy data to devices_new: %w", err)
				}

				// Drop old table and rename new one
				_, err = s.db.Exec(`DROP TABLE devices`)
				if err != nil {
					return fmt.Errorf("failed to drop old devices table: %w", err)
				}

				_, err = s.db.Exec(`ALTER TABLE devices_new RENAME TO devices`)
				if err != nil {
					return fmt.Errorf("failed to rename devices_new: %w", err)
				}

				// Recreate indexes
				_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_is_saved ON devices(is_saved)`)
				_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_visible ON devices(visible)`)
				_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip)`)
				_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen)`)
				_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_devices_manufacturer ON devices(manufacturer)`)
			}
		}

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (7, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}
	}

	// Migration 7 -> 8: Rename metrics_history to metrics_raw, add tiered aggregation tables
	// This implements Netdata-style tiered storage: raw (7d), hourly (30d), daily (365d), monthly (forever)
	if currentVersion < 8 {
		var historyExists int
		var rawExists int
		err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='metrics_history'").Scan(&historyExists)
		if err != nil {
			return fmt.Errorf("failed to check for metrics_history table: %w", err)
		}

		err = s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='metrics_raw'").Scan(&rawExists)
		if err != nil {
			return fmt.Errorf("failed to check for metrics_raw table: %w", err)
		}

		// Only rename if metrics_history exists AND metrics_raw doesn't
		if historyExists > 0 && rawExists == 0 {
			// Rename existing metrics_history to metrics_raw
			_, err = s.db.Exec(`ALTER TABLE metrics_history RENAME TO metrics_raw`)
			if err != nil {
				return fmt.Errorf("failed to rename metrics_history to metrics_raw: %w", err)
			}

			// Drop old indexes
			_, _ = s.db.Exec(`DROP INDEX IF EXISTS idx_metrics_serial`)
			_, _ = s.db.Exec(`DROP INDEX IF EXISTS idx_metrics_timestamp`)
			_, _ = s.db.Exec(`DROP INDEX IF EXISTS idx_metrics_serial_timestamp`)

			// Create new indexes for metrics_raw
			_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_raw_serial ON metrics_raw(serial)`)
			_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_raw_timestamp ON metrics_raw(timestamp)`)
			_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_raw_serial_timestamp ON metrics_raw(serial, timestamp)`)
		} else if historyExists > 0 && rawExists > 0 {
			// Both tables exist - this is a conflict, drop the old one
			_, _ = s.db.Exec(`DROP TABLE IF EXISTS metrics_history`)
		}

		// Create hourly aggregation table
		_, err = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS metrics_hourly (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				serial TEXT NOT NULL,
				hour_start DATETIME NOT NULL,
				sample_count INTEGER DEFAULT 0,
				page_count_min INTEGER DEFAULT 0,
				page_count_max INTEGER DEFAULT 0,
				page_count_avg INTEGER DEFAULT 0,
				color_pages_min INTEGER DEFAULT 0,
				color_pages_max INTEGER DEFAULT 0,
				color_pages_avg INTEGER DEFAULT 0,
				mono_pages_min INTEGER DEFAULT 0,
				mono_pages_max INTEGER DEFAULT 0,
				mono_pages_avg INTEGER DEFAULT 0,
				scan_count_min INTEGER DEFAULT 0,
				scan_count_max INTEGER DEFAULT 0,
				scan_count_avg INTEGER DEFAULT 0,
				toner_levels_avg TEXT,
				FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
				UNIQUE(serial, hour_start)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create metrics_hourly table: %w", err)
		}

		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_hourly_serial ON metrics_hourly(serial)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_hourly_hour_start ON metrics_hourly(hour_start)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_hourly_serial_hour ON metrics_hourly(serial, hour_start)`)

		// Create daily aggregation table
		_, err = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS metrics_daily (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				serial TEXT NOT NULL,
				day_start DATETIME NOT NULL,
				sample_count INTEGER DEFAULT 0,
				page_count_min INTEGER DEFAULT 0,
				page_count_max INTEGER DEFAULT 0,
				page_count_avg INTEGER DEFAULT 0,
				color_pages_min INTEGER DEFAULT 0,
				color_pages_max INTEGER DEFAULT 0,
				color_pages_avg INTEGER DEFAULT 0,
				mono_pages_min INTEGER DEFAULT 0,
				mono_pages_max INTEGER DEFAULT 0,
				mono_pages_avg INTEGER DEFAULT 0,
				scan_count_min INTEGER DEFAULT 0,
				scan_count_max INTEGER DEFAULT 0,
				scan_count_avg INTEGER DEFAULT 0,
				toner_levels_avg TEXT,
				FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
				UNIQUE(serial, day_start)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create metrics_daily table: %w", err)
		}

		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_daily_serial ON metrics_daily(serial)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_daily_day_start ON metrics_daily(day_start)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_daily_serial_day ON metrics_daily(serial, day_start)`)

		// Create monthly aggregation table
		_, err = s.db.Exec(`
			CREATE TABLE IF NOT EXISTS metrics_monthly (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				serial TEXT NOT NULL,
				month_start DATETIME NOT NULL,
				sample_count INTEGER DEFAULT 0,
				page_count_min INTEGER DEFAULT 0,
				page_count_max INTEGER DEFAULT 0,
				page_count_avg INTEGER DEFAULT 0,
				color_pages_min INTEGER DEFAULT 0,
				color_pages_max INTEGER DEFAULT 0,
				color_pages_avg INTEGER DEFAULT 0,
				mono_pages_min INTEGER DEFAULT 0,
				mono_pages_max INTEGER DEFAULT 0,
				mono_pages_avg INTEGER DEFAULT 0,
				scan_count_min INTEGER DEFAULT 0,
				scan_count_max INTEGER DEFAULT 0,
				scan_count_avg INTEGER DEFAULT 0,
				toner_levels_avg TEXT,
				FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
				UNIQUE(serial, month_start)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create metrics_monthly table: %w", err)
		}

		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_monthly_serial ON metrics_monthly(serial)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_monthly_month_start ON metrics_monthly(month_start)`)
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_metrics_monthly_serial_month ON metrics_monthly(serial, month_start)`)

		// Record migration
		_, err = s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (8, ?)`, time.Now())
		if err != nil {
			return fmt.Errorf("failed to record schema version: %w", err)
		}

		if storageLogger != nil {
			storageLogger.Info("Applied schema migration 7->8: Tiered metrics storage (raw/hourly/daily/monthly)")
		}
	}

	return nil
}

// Create adds a new device
func (s *SQLiteStore) Create(ctx context.Context, device *Device) error {
	if device.Serial == "" {
		return ErrInvalidSerial
	}

	now := time.Now()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	if device.FirstSeen.IsZero() {
		device.FirstSeen = now
	}
	if device.LastSeen.IsZero() {
		device.LastSeen = now
	}

	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	dnsJSON, _ := json.Marshal(device.DNSServers)
	rawJSON, _ := json.Marshal(device.RawData)
	lockedFieldsJSON, _ := json.Marshal(device.LockedFields)

	query := `
		INSERT INTO devices (
			serial, ip, manufacturer, model, hostname, firmware,
			mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
			consumables, status_messages,
			last_seen, created_at, first_seen, is_saved, visible,
			discovery_method, walk_filename, last_scan_id, raw_data,
			asset_number, location, description, web_ui_url, locked_fields
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		device.Serial, device.IP, device.Manufacturer, device.Model,
		device.Hostname, device.Firmware, device.MACAddress, device.SubnetMask,
		device.Gateway, string(dnsJSON), device.DHCPServer,
		string(consumablesJSON), string(statusJSON),
		device.LastSeen, device.CreatedAt, device.FirstSeen, device.IsSaved, device.Visible,
		device.DiscoveryMethod, device.WalkFilename, device.LastScanID, string(rawJSON),
		device.AssetNumber, device.Location, device.Description, device.WebUIURL, string(lockedFieldsJSON),
	)

	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			if storageLogger != nil {
				storageLogger.Debug("Device already exists", "serial", device.Serial, "ip", device.IP)
			}
			return ErrDuplicate
		}
		if storageLogger != nil {
			storageLogger.Error("Failed to create device", "serial", device.Serial, "ip", device.IP, "error", err)
		}
		return fmt.Errorf("failed to create device: %w", err)
	}

	if storageLogger != nil {
		storageLogger.Info("Device created", "serial", device.Serial, "ip", device.IP, "model", device.Model)
	}

	return nil
}

// Get retrieves a device by serial
func (s *SQLiteStore) Get(ctx context.Context, serial string) (*Device, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}

	query := `
		SELECT serial, ip, manufacturer, model, hostname, firmware,
			   mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
			   consumables, status_messages,
			   last_seen, created_at, first_seen, is_saved, visible,
			   discovery_method, walk_filename, last_scan_id, raw_data,
			   asset_number, location, description, web_ui_url, locked_fields
		FROM devices WHERE serial = ?
	`

	device := &Device{}
	var consumablesJSON, statusJSON, dnsJSON, rawJSON sql.NullString
	var assetNumber, location, description, webUIURL, lockedFieldsJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, serial).Scan(
		&device.Serial, &device.IP, &device.Manufacturer, &device.Model,
		&device.Hostname, &device.Firmware, &device.MACAddress, &device.SubnetMask,
		&device.Gateway, &dnsJSON, &device.DHCPServer,
		&consumablesJSON, &statusJSON,
		&device.LastSeen, &device.CreatedAt, &device.FirstSeen, &device.IsSaved, &device.Visible,
		&device.DiscoveryMethod, &device.WalkFilename, &device.LastScanID, &rawJSON,
		&assetNumber, &location, &description, &webUIURL, &lockedFieldsJSON,
	)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get device: %w", err)
	}

	// Unmarshal JSON fields
	if consumablesJSON.Valid && consumablesJSON.String != "" {
		json.Unmarshal([]byte(consumablesJSON.String), &device.Consumables)
	}
	if statusJSON.Valid && statusJSON.String != "" {
		json.Unmarshal([]byte(statusJSON.String), &device.StatusMessages)
	}
	if dnsJSON.Valid && dnsJSON.String != "" {
		json.Unmarshal([]byte(dnsJSON.String), &device.DNSServers)
	}
	if rawJSON.Valid && rawJSON.String != "" {
		json.Unmarshal([]byte(rawJSON.String), &device.RawData)
	}
	if assetNumber.Valid {
		device.AssetNumber = assetNumber.String
	}
	if location.Valid {
		device.Location = location.String
	}
	if description.Valid {
		device.Description = description.String
	}
	if webUIURL.Valid {
		device.WebUIURL = webUIURL.String
	}
	if lockedFieldsJSON.Valid && lockedFieldsJSON.String != "" {
		json.Unmarshal([]byte(lockedFieldsJSON.String), &device.LockedFields)
	}

	return device, nil
}

// Update modifies an existing device
func (s *SQLiteStore) Update(ctx context.Context, device *Device) error {
	if device.Serial == "" {
		return ErrInvalidSerial
	}

	device.LastSeen = time.Now()

	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	dnsJSON, _ := json.Marshal(device.DNSServers)
	rawJSON, _ := json.Marshal(device.RawData)
	lockedFieldsJSON, _ := json.Marshal(device.LockedFields)

	query := `
		UPDATE devices SET
			ip = ?, manufacturer = ?, model = ?, hostname = ?, firmware = ?,
			mac_address = ?, subnet_mask = ?, gateway = ?, dns_servers = ?, dhcp_server = ?,
			consumables = ?, status_messages = ?,
			last_seen = ?, is_saved = ?, visible = ?,
			discovery_method = ?, walk_filename = ?, last_scan_id = ?, raw_data = ?,
			asset_number = ?, location = ?, description = ?, web_ui_url = ?, locked_fields = ?
		WHERE serial = ?
	`

	result, err := s.db.ExecContext(ctx, query,
		device.IP, device.Manufacturer, device.Model, device.Hostname, device.Firmware,
		device.MACAddress, device.SubnetMask, device.Gateway, string(dnsJSON), device.DHCPServer,
		string(consumablesJSON), string(statusJSON),
		device.LastSeen, device.IsSaved, device.Visible,
		device.DiscoveryMethod, device.WalkFilename, device.LastScanID, string(rawJSON),
		device.AssetNumber, device.Location, device.Description, device.WebUIURL, string(lockedFieldsJSON), device.Serial,
	)

	if err != nil {
		return fmt.Errorf("failed to update device: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// Upsert creates or updates a device using a single INSERT ... ON CONFLICT statement.
// Preserves created_at, first_seen, is_saved, and locked_fields from the existing row.
func (s *SQLiteStore) Upsert(ctx context.Context, device *Device) error {
	if device.Serial == "" {
		return ErrInvalidSerial
	}

	now := time.Now()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	if device.FirstSeen.IsZero() {
		device.FirstSeen = now
	}
	if device.LastSeen.IsZero() {
		device.LastSeen = now
	}

	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	dnsJSON, _ := json.Marshal(device.DNSServers)
	rawJSON, _ := json.Marshal(device.RawData)
	lockedFieldsJSON, _ := json.Marshal(device.LockedFields)

	// Single-statement UPSERT: insert or update while preserving important fields from the existing row.
	// On conflict (serial exists), we:
	// - Keep created_at, first_seen, is_saved, locked_fields from the existing row (devices.*)
	// - Update all other fields from incoming data (excluded.*)
	query := `
		INSERT INTO devices (
			serial, ip, manufacturer, model, hostname, firmware,
			mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
			consumables, status_messages,
			last_seen, created_at, first_seen, is_saved, visible,
			discovery_method, walk_filename, last_scan_id, raw_data,
			asset_number, location, description, web_ui_url, locked_fields
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			ip = excluded.ip,
			manufacturer = excluded.manufacturer,
			model = excluded.model,
			hostname = excluded.hostname,
			firmware = excluded.firmware,
			mac_address = excluded.mac_address,
			subnet_mask = excluded.subnet_mask,
			gateway = excluded.gateway,
			dns_servers = excluded.dns_servers,
			dhcp_server = excluded.dhcp_server,
			consumables = excluded.consumables,
			status_messages = excluded.status_messages,
			last_seen = excluded.last_seen,
			visible = excluded.visible,
			discovery_method = excluded.discovery_method,
			walk_filename = excluded.walk_filename,
			last_scan_id = excluded.last_scan_id,
			raw_data = excluded.raw_data,
			asset_number = excluded.asset_number,
			location = excluded.location,
			description = excluded.description,
			web_ui_url = excluded.web_ui_url
			-- IMPORTANT: created_at, first_seen, is_saved, locked_fields are NOT updated (preserved from existing row)
	`

	_, err := s.db.ExecContext(ctx, query,
		device.Serial, device.IP, device.Manufacturer, device.Model,
		device.Hostname, device.Firmware, device.MACAddress, device.SubnetMask,
		device.Gateway, string(dnsJSON), device.DHCPServer,
		string(consumablesJSON), string(statusJSON),
		device.LastSeen, device.CreatedAt, device.FirstSeen, device.IsSaved, device.Visible,
		device.DiscoveryMethod, device.WalkFilename, device.LastScanID, string(rawJSON),
		device.AssetNumber, device.Location, device.Description, device.WebUIURL, string(lockedFieldsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert device: %w", err)
	}
	return nil
}

// StoreDiscoveryAtomic persists a discovered device update as a single atomic unit:
// - upsert device row
// - append scan history snapshot
// - save metrics snapshot
// If any step fails, no partial writes are committed.
func (s *SQLiteStore) StoreDiscoveryAtomic(ctx context.Context, device *Device, scan *ScanSnapshot, metrics *MetricsSnapshot) error {
	if device == nil {
		return fmt.Errorf("device is nil")
	}
	if scan == nil {
		return fmt.Errorf("scan is nil")
	}
	if metrics == nil {
		return fmt.Errorf("metrics is nil")
	}
	if device.Serial == "" || scan.Serial == "" || metrics.Serial == "" {
		return ErrInvalidSerial
	}
	if device.Serial != scan.Serial || device.Serial != metrics.Serial {
		return fmt.Errorf("serial mismatch between device/scan/metrics")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := s.upsertWithExecer(ctx, tx, device); err != nil {
		return err
	}
	if err := s.addScanHistoryWithExecer(ctx, tx, scan); err != nil {
		return err
	}
	if err := s.saveMetricsSnapshotWithExecer(ctx, tx, metrics); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	committed = true
	return nil
}

// Delete removes a device by serial
func (s *SQLiteStore) Delete(ctx context.Context, serial string) error {
	if serial == "" {
		return ErrInvalidSerial
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM devices WHERE serial = ?", serial)
	if err != nil {
		return fmt.Errorf("failed to delete device: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// List returns devices matching the filter
func (s *SQLiteStore) List(ctx context.Context, filter DeviceFilter) ([]*Device, error) {
	query := `
		SELECT serial, ip, manufacturer, model, hostname, firmware,
			   mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
			   consumables, status_messages,
			   last_seen, created_at, first_seen, is_saved, visible,
			   discovery_method, walk_filename, last_scan_id, raw_data,
			   asset_number, location, description, web_ui_url, locked_fields
		FROM devices WHERE 1=1
	`
	args := []interface{}{}

	if filter.IsSaved != nil {
		query += " AND is_saved = ?"
		args = append(args, *filter.IsSaved)
	}
	if filter.Visible != nil {
		query += " AND visible = ?"
		args = append(args, *filter.Visible)
	}
	if filter.IP != "" {
		query += " AND ip = ?"
		args = append(args, filter.IP)
	}
	if filter.Serial != "" {
		query += " AND serial = ?"
		args = append(args, filter.Serial)
	}
	if filter.Manufacturer != "" {
		query += " AND manufacturer LIKE ?"
		args = append(args, "%"+filter.Manufacturer+"%")
	}
	if filter.LastSeenAfter != nil {
		query += " AND last_seen > ?"
		args = append(args, *filter.LastSeenAfter)
	}

	query += " ORDER BY last_seen DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}
	defer rows.Close()

	devices := []*Device{}
	for rows.Next() {
		device := &Device{}
		var consumablesJSON, statusJSON, dnsJSON, rawJSON sql.NullString
		var assetNumber, location, description, webUIURL, lockedFieldsJSON sql.NullString

		err := rows.Scan(
			&device.Serial, &device.IP, &device.Manufacturer, &device.Model,
			&device.Hostname, &device.Firmware, &device.MACAddress, &device.SubnetMask,
			&device.Gateway, &dnsJSON, &device.DHCPServer,
			&consumablesJSON, &statusJSON,
			&device.LastSeen, &device.CreatedAt, &device.FirstSeen, &device.IsSaved, &device.Visible,
			&device.DiscoveryMethod, &device.WalkFilename, &device.LastScanID, &rawJSON,
			&assetNumber, &location, &description, &webUIURL, &lockedFieldsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan device: %w", err)
		}

		// Unmarshal JSON fields
		if consumablesJSON.Valid && consumablesJSON.String != "" {
			json.Unmarshal([]byte(consumablesJSON.String), &device.Consumables)
		}
		if statusJSON.Valid && statusJSON.String != "" {
			json.Unmarshal([]byte(statusJSON.String), &device.StatusMessages)
		}
		if dnsJSON.Valid && dnsJSON.String != "" {
			json.Unmarshal([]byte(dnsJSON.String), &device.DNSServers)
		}
		if rawJSON.Valid && rawJSON.String != "" {
			json.Unmarshal([]byte(rawJSON.String), &device.RawData)
		}
		if assetNumber.Valid {
			device.AssetNumber = assetNumber.String
		}
		if location.Valid {
			device.Location = location.String
		}
		if description.Valid {
			device.Description = description.String
		}
		if webUIURL.Valid {
			device.WebUIURL = webUIURL.String
		}
		if lockedFieldsJSON.Valid && lockedFieldsJSON.String != "" {
			json.Unmarshal([]byte(lockedFieldsJSON.String), &device.LockedFields)
		}

		devices = append(devices, device)
	}

	return devices, rows.Err()
}

// MarkSaved sets is_saved=true for a device
func (s *SQLiteStore) MarkSaved(ctx context.Context, serial string) error {
	if serial == "" {
		return ErrInvalidSerial
	}

	result, err := s.db.ExecContext(ctx, "UPDATE devices SET is_saved = 1, last_seen = ? WHERE serial = ?", time.Now(), serial)
	if err != nil {
		return fmt.Errorf("failed to mark device as saved: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkAllSaved sets is_saved=true for all visible, unsaved devices
func (s *SQLiteStore) MarkAllSaved(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE devices SET is_saved = 1, last_seen = ? WHERE is_saved = 0 AND visible = 1",
		time.Now())
	if err != nil {
		return 0, fmt.Errorf("failed to mark all devices as saved: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// MarkDiscovered sets is_saved=false for a device
func (s *SQLiteStore) MarkDiscovered(ctx context.Context, serial string) error {
	if serial == "" {
		return ErrInvalidSerial
	}

	result, err := s.db.ExecContext(ctx, "UPDATE devices SET is_saved = 0, last_seen = ? WHERE serial = ?", time.Now(), serial)
	if err != nil {
		return fmt.Errorf("failed to mark device as discovered: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteAll removes all devices matching the filter
func (s *SQLiteStore) DeleteAll(ctx context.Context, filter DeviceFilter) (int, error) {
	query := "DELETE FROM devices WHERE 1=1"
	args := []interface{}{}

	if filter.IsSaved != nil {
		query += " AND is_saved = ?"
		args = append(args, *filter.IsSaved)
	}
	if filter.IP != "" {
		query += " AND ip = ?"
		args = append(args, filter.IP)
	}
	if filter.Manufacturer != "" {
		query += " AND manufacturer LIKE ?"
		args = append(args, "%"+filter.Manufacturer+"%")
	}

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete devices: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// Stats returns storage statistics
func (s *SQLiteStore) Stats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var total, saved, discovered, visible, hidden, totalScans int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices").Scan(&total)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE is_saved = 1").Scan(&saved)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE is_saved = 0").Scan(&discovered)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE visible = 1").Scan(&visible)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE visible = 0").Scan(&hidden)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scan_history").Scan(&totalScans)

	stats["total_devices"] = total
	stats["saved_devices"] = saved
	stats["discovered_devices"] = discovered
	stats["visible_devices"] = visible
	stats["hidden_devices"] = hidden
	stats["total_scans"] = totalScans

	return stats, nil
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// AddScanHistory records a new scan snapshot for a device
// Note: This tracks device state changes (IP, hostname, firmware) for audit purposes.
// Metrics data (page counts, toner levels) should be stored using SaveMetricsSnapshot instead.
func (s *SQLiteStore) AddScanHistory(ctx context.Context, scan *ScanSnapshot) error {
	return s.addScanHistoryWithExecer(ctx, s.db, scan)
}

func (s *SQLiteStore) addScanHistoryWithExecer(ctx context.Context, ex execer, scan *ScanSnapshot) error {
	if scan.Serial == "" {
		return ErrInvalidSerial
	}

	// Marshal complex fields to JSON
	consumablesJSON, _ := json.Marshal(scan.Consumables)
	statusJSON, _ := json.Marshal(scan.StatusMessages)

	query := `
		INSERT INTO scan_history (
			serial, created_at, ip, hostname, firmware, 
			consumables, status_messages,
			discovery_method, walk_filename, raw_data
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := ex.ExecContext(ctx, query,
		scan.Serial,
		scan.CreatedAt,
		scan.IP,
		scan.Hostname,
		scan.Firmware,
		consumablesJSON,
		statusJSON,
		scan.DiscoveryMethod,
		scan.WalkFilename,
		scan.RawData,
	)
	if err != nil {
		return fmt.Errorf("failed to add scan history: %w", err)
	}

	// Update device's last_scan_id
	lastInsertID, _ := result.LastInsertId()
	_, err = ex.ExecContext(ctx,
		"UPDATE devices SET last_scan_id = ? WHERE serial = ?",
		lastInsertID, scan.Serial)

	return err
}

// GetScanHistory returns the last N scan snapshots for a device, newest first
func (s *SQLiteStore) GetScanHistory(ctx context.Context, serial string, limit int) ([]*ScanSnapshot, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}
	if limit <= 0 {
		limit = 10 // Default limit
	}

	query := `
		SELECT id, serial, created_at, ip, hostname, firmware,
		       consumables, status_messages,
		       discovery_method, walk_filename, raw_data
		FROM scan_history
		WHERE serial = ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, serial, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query scan history: %w", err)
	}
	defer rows.Close()

	var scans []*ScanSnapshot
	for rows.Next() {
		scan := &ScanSnapshot{}
		var consumablesJSON, statusJSON, rawDataJSON sql.NullString

		err := rows.Scan(
			&scan.ID,
			&scan.Serial,
			&scan.CreatedAt,
			&scan.IP,
			&scan.Hostname,
			&scan.Firmware,
			&consumablesJSON,
			&statusJSON,
			&scan.DiscoveryMethod,
			&scan.WalkFilename,
			&rawDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Unmarshal JSON fields
		if consumablesJSON.Valid {
			json.Unmarshal([]byte(consumablesJSON.String), &scan.Consumables)
		}
		if statusJSON.Valid {
			json.Unmarshal([]byte(statusJSON.String), &scan.StatusMessages)
		}
		if rawDataJSON.Valid {
			scan.RawData = json.RawMessage(rawDataJSON.String)
		}

		scans = append(scans, scan)
	}

	return scans, rows.Err()
}

// DeleteOldScans removes scan history older than the given timestamp
func (s *SQLiteStore) DeleteOldScans(ctx context.Context, olderThan int64) (int, error) {
	cutoffTime := time.Unix(olderThan, 0)

	result, err := s.db.ExecContext(ctx,
		"DELETE FROM scan_history WHERE created_at < ?",
		cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old scans: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// HideDiscovered sets visible=false for all devices where is_saved=false
func (s *SQLiteStore) HideDiscovered(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE devices SET visible = 0 WHERE is_saved = 0")
	if err != nil {
		return 0, fmt.Errorf("failed to hide discovered devices: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// ShowAll sets visible=true for all devices
func (s *SQLiteStore) ShowAll(ctx context.Context) (int, error) {
	result, err := s.db.ExecContext(ctx,
		"UPDATE devices SET visible = 1")
	if err != nil {
		return 0, fmt.Errorf("failed to show all devices: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// MetricsSnapshot represents a point-in-time snapshot of device metrics with agent-specific fields
type MetricsSnapshot struct {
	commonstorage.MetricsSnapshot // Embed common fields

	// HP-specific detailed impression counters (agent-specific)
	FaxPages          int `json:"fax_pages,omitempty"`
	CopyPages         int `json:"copy_pages,omitempty"`
	OtherPages        int `json:"other_pages,omitempty"`
	CopyMonoPages     int `json:"copy_mono_pages,omitempty"`
	CopyFlatbedScans  int `json:"copy_flatbed_scans,omitempty"`
	CopyADFScans      int `json:"copy_adf_scans,omitempty"`
	FaxFlatbedScans   int `json:"fax_flatbed_scans,omitempty"`
	FaxADFScans       int `json:"fax_adf_scans,omitempty"`
	ScanToHostFlatbed int `json:"scan_to_host_flatbed,omitempty"`
	ScanToHostADF     int `json:"scan_to_host_adf,omitempty"`
	DuplexSheets      int `json:"duplex_sheets,omitempty"`
	JamEvents         int `json:"jam_events,omitempty"`
	ScannerJamEvents  int `json:"scanner_jam_events,omitempty"`
	// Tier indicates which storage tier this snapshot came from (raw/hourly/daily/monthly)
	Tier string `json:"tier,omitempty"`
}

// SaveMetricsSnapshot stores a metrics snapshot for a device
func (s *SQLiteStore) SaveMetricsSnapshot(ctx context.Context, snapshot *MetricsSnapshot) error {
	return s.saveMetricsSnapshotWithExecer(ctx, s.db, snapshot)
}

func (s *SQLiteStore) saveMetricsSnapshotWithExecer(ctx context.Context, ex execer, snapshot *MetricsSnapshot) error {
	if snapshot.Serial == "" {
		return ErrInvalidSerial
	}

	if snapshot.Timestamp.IsZero() {
		snapshot.Timestamp = time.Now()
	}
	// Normalize to UTC for consistent storage/comparison
	snapshot.Timestamp = snapshot.Timestamp.UTC()

	tonerJSON, _ := json.Marshal(snapshot.TonerLevels)

	// Ignore snapshots where all primary counters are zero — likely an error/reset from device
	if snapshot.PageCount == 0 && snapshot.ColorPages == 0 && snapshot.MonoPages == 0 && snapshot.ScanCount == 0 {
		if storageLogger != nil {
			storageLogger.WarnRateLimited("metrics_zero_"+snapshot.Serial, 5*time.Minute,
				"Ignoring metrics snapshot where all counters are zero",
				"serial", snapshot.Serial)
		}
		return nil
	}

	// Defensive check: ignore snapshots where cumulative counters decreased compared to latest saved
	// (e.g., device returned 0 where previous was 45000). This avoids polluting history with invalid resets.
	if latest, err := s.GetLatestMetrics(ctx, snapshot.Serial); err == nil && latest != nil {
		// If page_count decreased, drop this snapshot
		if snapshot.PageCount < latest.PageCount {
			if storageLogger != nil {
				storageLogger.WarnRateLimited("metrics_decrease_"+snapshot.Serial, 1*time.Minute,
					"Dropping metrics snapshot because page_count decreased compared to latest",
					"serial", snapshot.Serial, "latest", latest.PageCount, "incoming", snapshot.PageCount)
			}
			// Treat as non-fatal: do not save the snapshot
			return nil
		}
		// Similarly for color/mono/scan counts (only if provided/non-zero on incoming)
		if snapshot.ColorPages > 0 && snapshot.ColorPages < latest.ColorPages {
			if storageLogger != nil {
				storageLogger.WarnRateLimited("metrics_decrease_color_"+snapshot.Serial, 1*time.Minute,
					"Dropping metrics snapshot because color_pages decreased compared to latest",
					"serial", snapshot.Serial, "latest", latest.ColorPages, "incoming", snapshot.ColorPages)
			}
			return nil
		}
		if snapshot.MonoPages > 0 && snapshot.MonoPages < latest.MonoPages {
			if storageLogger != nil {
				storageLogger.WarnRateLimited("metrics_decrease_mono_"+snapshot.Serial, 1*time.Minute,
					"Dropping metrics snapshot because mono_pages decreased compared to latest",
					"serial", snapshot.Serial, "latest", latest.MonoPages, "incoming", snapshot.MonoPages)
			}
			return nil
		}
		if snapshot.ScanCount > 0 && snapshot.ScanCount < latest.ScanCount {
			if storageLogger != nil {
				storageLogger.WarnRateLimited("metrics_decrease_scan_"+snapshot.Serial, 1*time.Minute,
					"Dropping metrics snapshot because scan_count decreased compared to latest",
					"serial", snapshot.Serial, "latest", latest.ScanCount, "incoming", snapshot.ScanCount)
			}
			return nil
		}
	}

	query := `
		INSERT INTO metrics_raw (
			serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	// Store timestamp as RFC3339Nano UTC string to ensure consistent lexicographic
	// ordering and comparability inside SQLite.
	tsStr := snapshot.Timestamp.Format(time.RFC3339Nano)
	result, err := ex.ExecContext(ctx, query,
		snapshot.Serial, tsStr, snapshot.PageCount,
		snapshot.ColorPages, snapshot.MonoPages, snapshot.ScanCount,
		string(tonerJSON),
	)

	if err != nil {
		if storageLogger != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
				storageLogger.WarnRateLimited("metrics_fk_"+snapshot.Serial, 1*time.Minute,
					"Metrics save failed: device not found", "serial", snapshot.Serial)
			} else {
				storageLogger.Error("Failed to save metrics snapshot", "serial", snapshot.Serial, "error", err)
			}
		}
		return fmt.Errorf("failed to save metrics snapshot: %w", err)
	}

	id, _ := result.LastInsertId()
	snapshot.ID = id

	if storageLogger != nil {
		storageLogger.Debug("Metrics snapshot saved", "serial", snapshot.Serial, "timestamp", snapshot.Timestamp)
	}

	return nil
}

func (s *SQLiteStore) upsertWithExecer(ctx context.Context, ex execer, device *Device) error {
	if device.Serial == "" {
		return ErrInvalidSerial
	}

	now := time.Now()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	if device.FirstSeen.IsZero() {
		device.FirstSeen = now
	}
	if device.LastSeen.IsZero() {
		device.LastSeen = now
	}

	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	dnsJSON, _ := json.Marshal(device.DNSServers)
	rawJSON, _ := json.Marshal(device.RawData)
	lockedFieldsJSON, _ := json.Marshal(device.LockedFields)

	// Single-statement UPSERT: insert or update while preserving important fields from the existing row.
	// On conflict (serial exists), we:
	// - Keep created_at, first_seen, is_saved, locked_fields from the existing row (devices.*)
	// - Update all other fields from incoming data (excluded.*)
	query := `
		INSERT INTO devices (
			serial, ip, manufacturer, model, hostname, firmware,
			mac_address, subnet_mask, gateway, dns_servers, dhcp_server,
			consumables, status_messages,
			last_seen, created_at, first_seen, is_saved, visible,
			discovery_method, walk_filename, last_scan_id, raw_data,
			asset_number, location, description, web_ui_url, locked_fields
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			ip = excluded.ip,
			manufacturer = excluded.manufacturer,
			model = excluded.model,
			hostname = excluded.hostname,
			firmware = excluded.firmware,
			mac_address = excluded.mac_address,
			subnet_mask = excluded.subnet_mask,
			gateway = excluded.gateway,
			dns_servers = excluded.dns_servers,
			dhcp_server = excluded.dhcp_server,
			consumables = excluded.consumables,
			status_messages = excluded.status_messages,
			last_seen = excluded.last_seen,
			visible = excluded.visible,
			discovery_method = excluded.discovery_method,
			walk_filename = excluded.walk_filename,
			last_scan_id = excluded.last_scan_id,
			raw_data = excluded.raw_data,
			asset_number = excluded.asset_number,
			location = excluded.location,
			description = excluded.description,
			web_ui_url = excluded.web_ui_url
			-- IMPORTANT: created_at, first_seen, is_saved, locked_fields are NOT updated (preserved from existing row)
	`

	_, err := ex.ExecContext(ctx, query,
		device.Serial, device.IP, device.Manufacturer, device.Model,
		device.Hostname, device.Firmware, device.MACAddress, device.SubnetMask,
		device.Gateway, string(dnsJSON), device.DHCPServer,
		string(consumablesJSON), string(statusJSON),
		device.LastSeen, device.CreatedAt, device.FirstSeen, device.IsSaved, device.Visible,
		device.DiscoveryMethod, device.WalkFilename, device.LastScanID, string(rawJSON),
		device.AssetNumber, device.Location, device.Description, device.WebUIURL, string(lockedFieldsJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert device: %w", err)
	}
	return nil
}

// GetMetricsHistory retrieves metrics history for a device within a time range
func (s *SQLiteStore) GetMetricsHistory(ctx context.Context, serial string, since time.Time, until time.Time) ([]*MetricsSnapshot, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}

	// Now that timestamps are stored as UTC RFC3339Nano strings we can use
	// indexed range queries in SQL. This is faster and avoids scanning all rows.
	query := `
		SELECT id, serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels,
			   fax_pages, copy_pages, other_pages, copy_mono_pages, copy_flatbed_scans, copy_adf_scans,
			   fax_flatbed_scans, fax_adf_scans, scan_to_host_flatbed, scan_to_host_adf,
			   duplex_sheets, jam_events, scanner_jam_events
		FROM metrics_raw
		WHERE serial = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`

	sinceStr := since.UTC().Format(time.RFC3339Nano)
	untilStr := until.UTC().Format(time.RFC3339Nano)

	rows, err := s.db.QueryContext(ctx, query, serial, sinceStr, untilStr)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics history: %w", err)
	}
	defer rows.Close()

	var snapshots []*MetricsSnapshot
	for rows.Next() {
		snapshot := &MetricsSnapshot{}
		var tonerJSON sql.NullString

		err := rows.Scan(
			&snapshot.ID, &snapshot.Serial, &snapshot.Timestamp,
			&snapshot.PageCount, &snapshot.ColorPages, &snapshot.MonoPages,
			&snapshot.ScanCount, &tonerJSON,
			&snapshot.FaxPages, &snapshot.CopyPages, &snapshot.OtherPages, &snapshot.CopyMonoPages,
			&snapshot.CopyFlatbedScans, &snapshot.CopyADFScans, &snapshot.FaxFlatbedScans, &snapshot.FaxADFScans,
			&snapshot.ScanToHostFlatbed, &snapshot.ScanToHostADF, &snapshot.DuplexSheets,
			&snapshot.JamEvents, &snapshot.ScannerJamEvents,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metrics row: %w", err)
		}

		if tonerJSON.Valid {
			json.Unmarshal([]byte(tonerJSON.String), &snapshot.TonerLevels)
		}

		snapshots = append(snapshots, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating metrics rows: %w", err)
	}

	return snapshots, nil
}

// GetLatestMetrics retrieves the most recent metrics snapshot for a device
func (s *SQLiteStore) GetLatestMetrics(ctx context.Context, serial string) (*MetricsSnapshot, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}

	query := `
		SELECT id, serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels
		FROM metrics_raw
		WHERE serial = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	snapshot := &MetricsSnapshot{}
	var tonerJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, serial).Scan(
		&snapshot.ID, &snapshot.Serial, &snapshot.Timestamp,
		&snapshot.PageCount, &snapshot.ColorPages, &snapshot.MonoPages,
		&snapshot.ScanCount, &tonerJSON,
	)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get latest metrics: %w", err)
	}

	if tonerJSON.Valid {
		json.Unmarshal([]byte(tonerJSON.String), &snapshot.TonerLevels)
	}

	return snapshot, nil
}

// DeleteOldMetrics removes metrics history older than the given timestamp
// TODO: Update to handle tiered cleanup (raw>7d, hourly>30d, daily>365d)
func (s *SQLiteStore) DeleteOldMetrics(ctx context.Context, olderThan time.Time) (int, error) {
	olderStr := olderThan.UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM metrics_raw WHERE timestamp < ?",
		olderStr)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old metrics: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// DeleteMetricByID removes a single metrics row by id from the specified tier/table.
// If tier is empty, attempt to delete from known metric tables until one succeeds.
func (s *SQLiteStore) DeleteMetricByID(ctx context.Context, tier string, id int64) error {
	tables := []string{"metrics_raw", "metrics_hourly", "metrics_daily", "metrics_monthly"}
	if tier != "" {
		// Normalize tier input to table name
		switch tier {
		case "raw":
			tables = []string{"metrics_raw"}
		case "hourly":
			tables = []string{"metrics_hourly"}
		case "daily":
			tables = []string{"metrics_daily"}
		case "monthly":
			tables = []string{"metrics_monthly"}
		default:
			tables = []string{tier}
		}
	}

	for _, tbl := range tables {
		res, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", tbl), id)
		if err != nil {
			// If table doesn't exist or other error, continue to next
			continue
		}
		rows, _ := res.RowsAffected()
		if rows > 0 {
			if storageLogger != nil {
				storageLogger.Info("Deleted metrics row", "table", tbl, "id", id)
			}
			return nil
		}
	}

	return fmt.Errorf("metrics row id %d not found", id)
}

// DeleteOldHiddenDevices removes devices that are hidden and older than timestamp
func (s *SQLiteStore) DeleteOldHiddenDevices(ctx context.Context, olderThan int64) (int, error) {
	cutoffTime := time.Unix(olderThan, 0)

	result, err := s.db.ExecContext(ctx,
		"DELETE FROM devices WHERE visible = 0 AND last_seen < ?",
		cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old hidden devices: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}
