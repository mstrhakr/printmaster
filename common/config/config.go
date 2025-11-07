// Package config provides shared configuration utilities for PrintMaster components
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// FindConfigFile searches for a config file in multiple platform-appropriate locations
// Returns the path and data if found, or an error if not found in any location
func FindConfigFile(filename string, component string) (string, []byte, error) {
	searchPaths := GetConfigSearchPaths(filename, component)

	for _, path := range searchPaths {
		if data, err := os.ReadFile(path); err == nil {
			return path, data, nil
		}
	}

	return "", nil, fmt.Errorf("%s not found in any search path", filename)
}

// GetConfigSearchPaths returns an ordered list of paths to search for config files
// component should be "agent" or "server"
func GetConfigSearchPaths(filename string, component string) []string {
	var searchPaths []string

	// 1. Component-specific system directory (highest priority for services)
	switch runtime.GOOS {
	case "windows":
		searchPaths = append(searchPaths, filepath.Join(os.Getenv("ProgramData"), "PrintMaster", component, filename))
	case "darwin":
		searchPaths = append(searchPaths, filepath.Join("/Library/Application Support", "PrintMaster", component, filename))
	default: // Linux and other Unix-like
		searchPaths = append(searchPaths, filepath.Join("/etc/printmaster", component, filename))
	}

	// 2. User-specific config directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		switch runtime.GOOS {
		case "windows":
			searchPaths = append(searchPaths, filepath.Join(homeDir, "AppData", "Local", "PrintMaster", component, filename))
		case "darwin":
			searchPaths = append(searchPaths, filepath.Join(homeDir, "Library", "Application Support", "PrintMaster", component, filename))
		default:
			searchPaths = append(searchPaths, filepath.Join(homeDir, ".config", "printmaster", component, filename))
		}
	}

	// 3. Executable directory
	if exePath, err := os.Executable(); err == nil {
		searchPaths = append(searchPaths, filepath.Join(filepath.Dir(exePath), filename))
	}

	// 4. Current working directory (lowest priority)
	searchPaths = append(searchPaths, filepath.Join(".", filename))

	return searchPaths
}

// GetDataDirectory returns the appropriate directory for storing application data
// When running as service, returns system-wide directory
// When running interactively, returns user-specific directory
func GetDataDirectory(component string, isService bool) (string, error) {
	var dataDir string

	if isService {
		// Service mode - use system-wide directory with component subdirectory
		switch runtime.GOOS {
		case "windows":
			dataDir = filepath.Join(os.Getenv("ProgramData"), "PrintMaster", component)
		case "darwin":
			dataDir = filepath.Join("/var/lib/printmaster", component)
		default: // Linux
			dataDir = filepath.Join("/var/lib/printmaster", component)
		}
	} else {
		// Interactive mode - use user directory with component subdirectory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not get user home directory: %w", err)
		}

		switch runtime.GOOS {
		case "windows":
			dataDir = filepath.Join(homeDir, "AppData", "Local", "PrintMaster", component)
		case "darwin":
			dataDir = filepath.Join(homeDir, "Library", "Application Support", "PrintMaster", component)
		default: // Linux and other Unix-like
			dataDir = filepath.Join(homeDir, ".local", "share", "printmaster", component)
		}
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create data directory: %w", err)
	}

	return dataDir, nil
}

// GetLogDirectory returns the appropriate directory for storing logs
func GetLogDirectory(component string, isService bool) (string, error) {
	var logDir string

	if isService {
		// Service mode - use system log directory with component subdirectory
		switch runtime.GOOS {
		case "windows":
			logDir = filepath.Join(os.Getenv("ProgramData"), "PrintMaster", component, "logs")
		case "darwin":
			logDir = filepath.Join("/var/log/printmaster", component)
		default: // Linux
			logDir = filepath.Join("/var/log/printmaster", component)
		}
	} else {
		// Interactive mode - use current directory
		logDir = "logs"
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create log directory: %w", err)
	}

	return logDir, nil
}

// WriteDefaultTOML writes a default TOML configuration file with the provided structure
func WriteDefaultTOML(configPath string, config interface{}) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadTOML loads a TOML configuration file into the provided structure
func LoadTOML(configPath string, config interface{}) error {
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	if _, err := toml.DecodeFile(configPath, config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Common configuration structs that both agent and server use

// DatabaseConfig holds database settings
type DatabaseConfig struct {
	Path string `toml:"path"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level string `toml:"level"`
}

// ApplyEnvOverrides applies common environment variable overrides
func ApplyDatabaseEnvOverrides(cfg *DatabaseConfig) {
	if val := os.Getenv("DB_PATH"); val != "" {
		cfg.Path = val
	}
}

func ApplyLoggingEnvOverrides(cfg *LoggingConfig) {
	if val := os.Getenv("LOG_LEVEL"); val != "" {
		cfg.Level = val
	}
}
