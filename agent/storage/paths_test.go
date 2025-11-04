package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGetDataDir(t *testing.T) {
	dataDir, err := GetDataDir("PrintMaster")
	if err != nil {
		t.Fatalf("Failed to get data dir: %v", err)
	}

	if dataDir == "" {
		t.Error("Data dir should not be empty")
	}

	// Verify directory was created
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Errorf("Data directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("Data path is not a directory")
	}

	// Verify path contains app name
	if !filepath.IsAbs(dataDir) {
		t.Error("Data dir should be an absolute path")
	}

	// Check platform-specific expectations
	switch runtime.GOOS {
	case "windows":
		if os.Getenv("LOCALAPPDATA") != "" {
			expectedBase := os.Getenv("LOCALAPPDATA")
			if !filepath.HasPrefix(dataDir, expectedBase) {
				t.Errorf("Expected Windows data dir to start with %s, got %s", expectedBase, dataDir)
			}
		}
	case "darwin":
		homeDir, _ := os.UserHomeDir()
		expectedBase := filepath.Join(homeDir, "Library", "Application Support")
		if !filepath.HasPrefix(dataDir, expectedBase) {
			t.Errorf("Expected macOS data dir to start with %s, got %s", expectedBase, dataDir)
		}
	default: // Linux
		homeDir, _ := os.UserHomeDir()
		expectedBase := filepath.Join(homeDir, ".local", "share")
		if !filepath.HasPrefix(dataDir, expectedBase) {
			t.Errorf("Expected Linux data dir to start with %s, got %s", expectedBase, dataDir)
		}
	}

	t.Logf("Data directory for %s: %s", runtime.GOOS, dataDir)
}

func TestGetDefaultDBPath(t *testing.T) {
	dbPath, err := GetDefaultDBPath()
	if err != nil {
		t.Fatalf("Failed to get default DB path: %v", err)
	}

	if dbPath == "" {
		t.Error("DB path should not be empty")
	}

	if !filepath.IsAbs(dbPath) {
		t.Error("DB path should be absolute")
	}

	if filepath.Ext(dbPath) != ".db" {
		t.Errorf("Expected .db extension, got %s", filepath.Ext(dbPath))
	}

	// Verify parent directory exists
	parentDir := filepath.Dir(dbPath)
	info, err := os.Stat(parentDir)
	if err != nil {
		t.Errorf("Parent directory does not exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("Parent is not a directory")
	}

	t.Logf("Default DB path for %s: %s", runtime.GOOS, dbPath)
}

func TestGetDataDir_CustomAppName(t *testing.T) {
	appNames := []string{"TestApp", "MyPrinterAgent", "Agent-v2"}

	for _, appName := range appNames {
		dataDir, err := GetDataDir(appName)
		if err != nil {
			t.Errorf("Failed to get data dir for %s: %v", appName, err)
			continue
		}

		// Verify directory contains app name
		if !filepath.HasPrefix(filepath.Base(dataDir), appName) {
			t.Errorf("Expected data dir to contain app name %s, got %s", appName, dataDir)
		}

		// Cleanup test directory
		os.RemoveAll(dataDir)
	}
}
