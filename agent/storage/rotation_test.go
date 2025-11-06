package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotateDatabase(t *testing.T) {
	t.Parallel()

	t.Run("rotate existing database", func(t *testing.T) {
		t.Parallel()

		// Create a temporary database file
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create a dummy database file
		if err := os.WriteFile(dbPath, []byte("dummy data"), 0644); err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}

		// Rotate the database (no config store)
		backupPath, err := RotateDatabase(dbPath, nil)
		if err != nil {
			t.Fatalf("RotateDatabase failed: %v", err)
		}

		// Verify backup was created
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Errorf("Backup file was not created: %s", backupPath)
		}

		// Verify original database was removed
		if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
			t.Errorf("Original database still exists: %s", dbPath)
		}

		// Verify backup path has correct format
		if !strings.Contains(backupPath, ".backup.") {
			t.Errorf("Backup path doesn't contain .backup.: %s", backupPath)
		}
	})

	t.Run("rotate with WAL files", func(t *testing.T) {
		t.Parallel()

		// Create a temporary database file with WAL files
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")
		walPath := dbPath + "-wal"
		shmPath := dbPath + "-shm"

		// Create dummy files
		if err := os.WriteFile(dbPath, []byte("dummy data"), 0644); err != nil {
			t.Fatalf("Failed to create test database: %v", err)
		}
		if err := os.WriteFile(walPath, []byte("wal data"), 0644); err != nil {
			t.Fatalf("Failed to create WAL file: %v", err)
		}
		if err := os.WriteFile(shmPath, []byte("shm data"), 0644); err != nil {
			t.Fatalf("Failed to create SHM file: %v", err)
		}

		// Rotate the database (no config store)
		backupPath, err := RotateDatabase(dbPath, nil)
		if err != nil {
			t.Fatalf("RotateDatabase failed: %v", err)
		}

		// Verify backup was created
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			t.Errorf("Backup file was not created: %s", backupPath)
		}

		// Verify WAL backup was created (optional, so just log if missing)
		walBackup := strings.Replace(backupPath, ".db.backup.", ".db-wal.backup.", 1)
		if _, err := os.Stat(walBackup); os.IsNotExist(err) {
			t.Logf("WAL backup not found (expected): %s", walBackup)
		}
	})

	t.Run("in-memory database fails", func(t *testing.T) {
		t.Parallel()

		_, err := RotateDatabase(":memory:", nil)
		if err == nil {
			t.Error("Expected error when rotating in-memory database")
		}
	})

	t.Run("non-existent database fails", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "nonexistent.db")

		_, err := RotateDatabase(dbPath, nil)
		if err == nil {
			t.Error("Expected error when rotating non-existent database")
		}
	})
}

func TestCleanupOldBackups(t *testing.T) {
	t.Parallel()

	t.Run("remove old backups beyond keepCount", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create current database
		if err := os.WriteFile(dbPath, []byte("current"), 0644); err != nil {
			t.Fatalf("Failed to create database: %v", err)
		}

		// Create 5 backup files with different ages
		backupFiles := []string{
			filepath.Join(tmpDir, "test.db.backup.2025-11-01T10-00-00"),
			filepath.Join(tmpDir, "test.db.backup.2025-11-02T10-00-00"),
			filepath.Join(tmpDir, "test.db.backup.2025-11-03T10-00-00"),
			filepath.Join(tmpDir, "test.db.backup.2025-11-04T10-00-00"),
			filepath.Join(tmpDir, "test.db.backup.2025-11-05T10-00-00"),
		}

		// Create backups and set modification times
		for i, backup := range backupFiles {
			if err := os.WriteFile(backup, []byte("backup"), 0644); err != nil {
				t.Fatalf("Failed to create backup: %v", err)
			}
			// Set mod time - oldest first, newest last
			modTime := time.Now().Add(time.Duration(-len(backupFiles)+i) * 24 * time.Hour)
			if err := os.Chtimes(backup, modTime, modTime); err != nil {
				t.Fatalf("Failed to set backup time: %v", err)
			}
		}

		// Keep only 2 most recent backups
		if err := CleanupOldBackups(dbPath, 2); err != nil {
			t.Fatalf("CleanupOldBackups failed: %v", err)
		}

		// Verify oldest 3 backups were removed
		for i := 0; i < 3; i++ {
			if _, err := os.Stat(backupFiles[i]); !os.IsNotExist(err) {
				t.Errorf("Old backup was not removed: %s", backupFiles[i])
			}
		}

		// Verify 2 newest backups still exist
		for i := 3; i < 5; i++ {
			if _, err := os.Stat(backupFiles[i]); os.IsNotExist(err) {
				t.Errorf("Recent backup was removed: %s", backupFiles[i])
			}
		}

		// Verify current database still exists
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			t.Errorf("Current database was removed: %s", dbPath)
		}
	})

	t.Run("keep all when under keepCount", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		// Create 2 backups
		backup1 := filepath.Join(tmpDir, "test.db.backup.2025-11-01T10-00-00")
		backup2 := filepath.Join(tmpDir, "test.db.backup.2025-11-02T10-00-00")

		if err := os.WriteFile(backup1, []byte("backup"), 0644); err != nil {
			t.Fatalf("Failed to create backup1: %v", err)
		}
		if err := os.WriteFile(backup2, []byte("backup"), 0644); err != nil {
			t.Fatalf("Failed to create backup2: %v", err)
		}

		// Keep 10 backups (more than we have)
		if err := CleanupOldBackups(dbPath, 10); err != nil {
			t.Fatalf("CleanupOldBackups failed: %v", err)
		}

		// Verify both backups still exist
		if _, err := os.Stat(backup1); os.IsNotExist(err) {
			t.Errorf("Backup1 was removed: %s", backup1)
		}
		if _, err := os.Stat(backup2); os.IsNotExist(err) {
			t.Errorf("Backup2 was removed: %s", backup2)
		}
	})

	t.Run("in-memory database no-op", func(t *testing.T) {
		t.Parallel()

		err := CleanupOldBackups(":memory:", 10)
		if err != nil {
			t.Errorf("Expected no error for in-memory database: %v", err)
		}
	})
}
