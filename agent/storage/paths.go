package storage

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetDataDir returns the appropriate data directory for the current OS
func GetDataDir(appName string) (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case "windows":
		// Use ProgramData for system-wide or LOCALAPPDATA for user-specific
		baseDir = os.Getenv("LOCALAPPDATA")
		if baseDir == "" {
			baseDir = os.Getenv("PROGRAMDATA")
		}
		if baseDir == "" {
			return "", os.ErrNotExist
		}

	case "darwin": // macOS
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(homeDir, "Library", "Application Support")

	default: // Linux and other Unix-like systems
		// Try XDG_DATA_HOME first, fallback to ~/.local/share
		baseDir = os.Getenv("XDG_DATA_HOME")
		if baseDir == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			baseDir = filepath.Join(homeDir, ".local", "share")
		}
	}

	dataDir := filepath.Join(baseDir, appName)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", err
	}

	return dataDir, nil
}

// GetDefaultDBPath returns the default SQLite database path
func GetDefaultDBPath() (string, error) {
	dataDir, err := GetDataDir("PrintMaster")
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "devices.db"), nil
}
