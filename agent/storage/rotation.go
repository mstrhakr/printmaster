package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RotateDatabase renames the existing database file with a timestamp suffix
// and returns true if rotation was successful. This allows the application
// to create a fresh database when migrations fail.
//
// Example: devices.db -> devices.db.backup.2025-11-06T14-59-31
//
// The backup file is left in place for manual recovery if needed.
//
// If configStore is provided, sets a "database_rotated" flag to true so the UI
// can warn users on next page load.
func RotateDatabase(dbPath string, configStore AgentConfigStore) (string, error) {
	// Don't rotate in-memory databases
	if dbPath == "" || dbPath == ":memory:" {
		return "", fmt.Errorf("cannot rotate in-memory database")
	}

	// Check if database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", fmt.Errorf("database file does not exist: %s", dbPath)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	backupPath := fmt.Sprintf("%s.backup.%s", dbPath, timestamp)

	// Also rotate the WAL files if they exist
	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"

	// Rename the database file
	if err := os.Rename(dbPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to rename database: %w", err)
	}

	// Try to rotate WAL and SHM files (non-fatal if they don't exist)
	if _, err := os.Stat(walPath); err == nil {
		walBackup := fmt.Sprintf("%s-wal.backup.%s", dbPath, timestamp)
		_ = os.Rename(walPath, walBackup)
	}
	if _, err := os.Stat(shmPath); err == nil {
		shmBackup := fmt.Sprintf("%s-shm.backup.%s", dbPath, timestamp)
		_ = os.Rename(shmPath, shmBackup)
	}

	// Set rotation flag in config store if provided
	if configStore != nil {
		rotationInfo := map[string]interface{}{
			"rotated_at":  timestamp,
			"backup_path": backupPath,
			"original_db": dbPath,
		}
		if err := configStore.SetConfigValue("database_rotation", rotationInfo); err != nil {
			if storageLogger != nil {
				storageLogger.Warn("Failed to set rotation flag in config", "error", err)
			}
			// Non-fatal - rotation still succeeded
		}
	}

	return backupPath, nil
}

// CleanupOldBackups removes old database backup files, keeping only the N most recent.
// This helps prevent disk space accumulation from repeated rotation events.
//
// keepCount specifies how many backup files to retain (e.g., 10).
// Older backups beyond this count are deleted.
func CleanupOldBackups(dbPath string, keepCount int) error {
	// Don't cleanup for in-memory databases
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}

	if keepCount < 0 {
		keepCount = 0
	}

	dir := filepath.Dir(dbPath)
	baseName := filepath.Base(dbPath)

	// Find all backup files for this database
	pattern := fmt.Sprintf("%s.backup.*", baseName)
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return fmt.Errorf("failed to find backup files: %w", err)
	}

	// If we have fewer backups than keepCount, nothing to clean up
	if len(matches) <= keepCount {
		return nil
	}

	// Sort by modification time (newest first)
	type backupFile struct {
		path    string
		modTime time.Time
	}

	backups := make([]backupFile, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		backups = append(backups, backupFile{
			path:    match,
			modTime: info.ModTime(),
		})
	}

	// Sort by modification time (newest first)
	for i := 0; i < len(backups); i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[j].modTime.After(backups[i].modTime) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// Remove backups beyond keepCount
	var removed int
	for i := keepCount; i < len(backups); i++ {
		if err := os.Remove(backups[i].path); err != nil {
			if storageLogger != nil {
				storageLogger.Warn("Failed to remove old backup", "path", backups[i].path, "error", err)
			}
		} else {
			removed++
		}
	}

	if storageLogger != nil && removed > 0 {
		storageLogger.Info("Cleaned up old database backups", "removed", removed, "kept", keepCount)
	}

	return nil
}
