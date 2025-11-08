package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Ensure driver is imported for BackupAndReset
)

const targetSchemaVersion = 8

// expectedSchema defines the target schema structure for auto-migration
var expectedSchema = map[string][]string{
	"devices": {
		"serial TEXT PRIMARY KEY",
		"ip TEXT NOT NULL",
		"manufacturer TEXT",
		"model TEXT",
		"hostname TEXT",
		"firmware TEXT",
		"mac_address TEXT",
		"subnet_mask TEXT",
		"gateway TEXT",
		"dns_servers TEXT",
		"dhcp_server TEXT",
		"consumables TEXT",
		"status_messages TEXT",
		"last_seen DATETIME NOT NULL",
		"created_at DATETIME NOT NULL",
		"first_seen DATETIME NOT NULL",
		"is_saved BOOLEAN DEFAULT 0",
		"visible BOOLEAN DEFAULT 1",
		"discovery_method TEXT",
		"walk_filename TEXT",
		"last_scan_id INTEGER",
		"asset_number TEXT",
		"location TEXT",
		"description TEXT",
		"web_ui_url TEXT",
		"locked_fields TEXT",
		"raw_data TEXT",
	},
	"metrics_history": {
		"id INTEGER PRIMARY KEY AUTOINCREMENT",
		"serial TEXT NOT NULL",
		"timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"page_count INTEGER DEFAULT 0",
		"color_pages INTEGER DEFAULT 0",
		"mono_pages INTEGER DEFAULT 0",
		"scan_count INTEGER DEFAULT 0",
		"toner_levels TEXT",
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
	},
	// metrics_raw is the runtime name for migrated metrics; include the same
	// expected columns so auto-migration can add missing fields to metrics_raw
	// when upgrading older databases that already have a metrics_raw table.
	"metrics_raw": {
		"id INTEGER PRIMARY KEY AUTOINCREMENT",
		"serial TEXT NOT NULL",
		"timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"page_count INTEGER DEFAULT 0",
		"color_pages INTEGER DEFAULT 0",
		"mono_pages INTEGER DEFAULT 0",
		"scan_count INTEGER DEFAULT 0",
		"toner_levels TEXT",
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
	},
	"scan_history": {
		"id INTEGER PRIMARY KEY AUTOINCREMENT",
		"serial TEXT NOT NULL",
		"created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP",
		"ip TEXT NOT NULL",
		"hostname TEXT",
		"firmware TEXT",
		"consumables TEXT",
		"status_messages TEXT",
		"discovery_method TEXT",
		"walk_filename TEXT",
		"raw_data TEXT",
	},
}

// autoMigrate performs intelligent schema migration with backup
func (s *SQLiteStore) autoMigrate() error {
	// Get current schema version
	currentVersion := s.getCurrentVersion()

	if currentVersion == targetSchemaVersion {
		if storageLogger != nil {
			storageLogger.Debug("Schema is up to date", "version", currentVersion)
		}
		return nil
	}

	if storageLogger != nil {
		storageLogger.Info("Schema migration needed", "current", currentVersion, "target", targetSchemaVersion)
	}

	// For major changes or unknown versions, offer backup and fresh start
	if currentVersion > targetSchemaVersion || currentVersion == 0 {
		if storageLogger != nil {
			storageLogger.Warn("Schema version mismatch - database may be from newer version or corrupted")
		}
		return s.BackupAndReset()
	}

	// Auto-detect and fix schema differences
	if err := s.detectAndFixSchema(); err != nil {
		if storageLogger != nil {
			storageLogger.Error("Auto-migration failed, backing up and resetting", "error", err)
		}
		return s.BackupAndReset()
	}

	// Update version
	_, err := s.db.Exec(`INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (?, ?)`,
		targetSchemaVersion, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	if storageLogger != nil {
		storageLogger.Info("Schema migration completed", "version", targetSchemaVersion)
	}
	return nil
}

// getCurrentVersion returns the current schema version
func (s *SQLiteStore) getCurrentVersion() int {
	var version int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0
	}
	return version
}

// detectAndFixSchema compares actual schema with expected and applies fixes
func (s *SQLiteStore) detectAndFixSchema() error {
	for tableName, expectedCols := range expectedSchema {
		// Get actual columns
		actualCols, err := s.getTableColumns(tableName)
		if err != nil {
			return fmt.Errorf("failed to get columns for %s: %w", tableName, err)
		}

		// Build expected column map
		expectedColMap := make(map[string]bool)
		for _, col := range expectedCols {
			// Extract column name (first word before space)
			parts := strings.Fields(col)
			if len(parts) > 0 {
				expectedColMap[strings.ToLower(parts[0])] = true
			}
		}

		// Check for columns that need to be removed (table recreation needed)
		var needsRecreation bool
		var columnsToRemove []string
		for _, actualCol := range actualCols {
			if !expectedColMap[actualCol] {
				needsRecreation = true
				columnsToRemove = append(columnsToRemove, actualCol)
			}
		}

		if needsRecreation {
			storageLogger.Info("Table needs recreation to remove columns",
				"table", tableName, "columns", columnsToRemove)
			if err := s.recreateTable(tableName, expectedCols, actualCols); err != nil {
				return fmt.Errorf("failed to recreate %s: %w", tableName, err)
			}
		} else {
			// Just add missing columns
			for colName := range expectedColMap {
				if !contains(actualCols, colName) {
					// Find the full column definition
					var colDef string
					for _, expected := range expectedCols {
						if strings.HasPrefix(strings.ToLower(expected), colName+" ") {
							colDef = expected
							break
						}
					}
					if colDef != "" {
						storageLogger.Info("Adding missing column", "table", tableName, "column", colName)
						_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s`, tableName, colDef))
						if err != nil && !strings.Contains(err.Error(), "duplicate column") {
							return fmt.Errorf("failed to add column %s to %s: %w", colName, tableName, err)
						}
					}
				}
			}
		}
	}

	return nil
}

// getTableColumns returns the list of column names for a table
func (s *SQLiteStore) getTableColumns(tableName string) ([]string, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name string
		var ctype string
		var notnull int
		var dfltValue interface{}
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, strings.ToLower(name))
	}
	return columns, nil
}

// recreateTable recreates a table with new schema, preserving data
func (s *SQLiteStore) recreateTable(tableName string, expectedCols, actualCols []string) error {
	// Build column list for data copy (intersection of old and new)
	var copyColumns []string
	expectedColMap := make(map[string]bool)
	for _, col := range expectedCols {
		parts := strings.Fields(col)
		if len(parts) > 0 {
			colName := strings.ToLower(parts[0])
			expectedColMap[colName] = true
			if contains(actualCols, colName) {
				copyColumns = append(copyColumns, colName)
			}
		}
	}

	if len(copyColumns) == 0 {
		return fmt.Errorf("no common columns found between old and new schema for %s", tableName)
	}

	// Build CREATE TABLE statement
	colDefs := strings.Join(expectedCols, ",\n\t\t")
	createSQL := fmt.Sprintf(`CREATE TABLE %s_new (\n\t\t%s\n\t)`, tableName, colDefs)

	// Execute recreation
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create new table
	if _, err := tx.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create new table: %w", err)
	}

	// Copy data
	copySQL := fmt.Sprintf(`INSERT INTO %s_new (%s) SELECT %s FROM %s`,
		tableName,
		strings.Join(copyColumns, ", "),
		strings.Join(copyColumns, ", "),
		tableName)
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Drop old table
	if _, err := tx.Exec(fmt.Sprintf(`DROP TABLE %s`, tableName)); err != nil {
		return fmt.Errorf("failed to drop old table: %w", err)
	}

	// Rename new table
	if _, err := tx.Exec(fmt.Sprintf(`ALTER TABLE %s_new RENAME TO %s`, tableName, tableName)); err != nil {
		return fmt.Errorf("failed to rename new table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	storageLogger.Info("Table recreated successfully", "table", tableName)
	return nil
}

// BackupAndReset backs up the current database and starts fresh (exported for API access)
func (s *SQLiteStore) BackupAndReset() error {
	// Close current connection
	if err := s.db.Close(); err != nil {
		storageLogger.Warn("Error closing database for backup", "error", err)
	}

	// Generate backup filename
	dbPath := s.dbPath
	if dbPath == ":memory:" {
		// Can't backup in-memory database, just reset
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("failed to open fresh database: %w", err)
		}
		s.db = db
		return s.initSchema()
	}

	backupPath := fmt.Sprintf("%s.backup_%s", dbPath, time.Now().Format("20060102_150405"))

	// Copy database file
	if err := copyFile(dbPath, backupPath); err != nil {
		storageLogger.Error("Failed to backup database", "error", err)
	} else {
		storageLogger.Info("Database backed up", "path", backupPath)
	}

	// Remove old database
	if err := os.Remove(dbPath); err != nil {
		return fmt.Errorf("failed to remove old database: %w", err)
	}

	// Open fresh database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open fresh database: %w", err)
	}
	s.db = db

	// Initialize fresh schema
	return s.initSchema()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// contains checks if a slice contains a string (case-insensitive)
func contains(slice []string, item string) bool {
	item = strings.ToLower(item)
	for _, s := range slice {
		if strings.ToLower(s) == item {
			return true
		}
	}
	return false
}

// seedDefaultMetrics creates initial metrics snapshots for devices that don't have any
func (s *SQLiteStore) seedDefaultMetrics() error {
	// Find devices without metrics
	query := `
		SELECT d.serial 
		FROM devices d 
		LEFT JOIN metrics_raw m ON d.serial = m.serial 
		WHERE m.serial IS NULL
	`

	rows, err := s.db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query devices without metrics: %w", err)
	}
	defer rows.Close()

	var serials []string
	for rows.Next() {
		var serial string
		if err := rows.Scan(&serial); err != nil {
			return err
		}
		serials = append(serials, serial)
	}

	// Create default metrics for each device
	ctx := context.Background()
	for _, serial := range serials {
		snapshot := &MetricsSnapshot{}
		snapshot.Serial = serial
		snapshot.Timestamp = time.Now()
		snapshot.PageCount = 0
		snapshot.TonerLevels = make(map[string]interface{})

		if err := s.SaveMetricsSnapshot(ctx, snapshot); err != nil {
			if storageLogger != nil {
				storageLogger.Warn("Failed to seed default metrics", "serial", serial, "error", err)
			}
		} else {
			if storageLogger != nil {
				storageLogger.Debug("Seeded default metrics", "serial", serial)
			}
		}
	}

	if len(serials) > 0 && storageLogger != nil {
		storageLogger.Info("Seeded default metrics for devices", "count", len(serials))
	}

	return nil
}
