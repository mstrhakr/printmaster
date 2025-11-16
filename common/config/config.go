// Package config provides shared configuration utilities for PrintMaster components
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

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
// If the file already exists, it returns nil without overwriting
func WriteDefaultTOML(configPath string, config interface{}) error {
	// Check if file already exists - don't overwrite
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("config file already exists at %s (will not overwrite)", configPath)
	}

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

// WriteTOML writes the provided config structure to the given path, overwriting
// any existing file. It ensures the containing directory exists.
func WriteTOML(configPath string, config interface{}) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("failed to encode config to TOML: %w", err)
	}

	// Write atomically: write to temp file then rename
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := os.Rename(tmp, configPath); err != nil {
		return fmt.Errorf("failed to rename temp config file: %w", err)
	}
	return nil
}

// LoadTOML loads a TOML configuration file into the provided structure
func LoadTOML(configPath string, config interface{}) error {
	if _, err := os.Stat(configPath); err != nil {
		return fmt.Errorf("config file not found: %w", err)
	}

	if _, err := toml.DecodeFile(configPath, config); err != nil {
		// If the TOML decoder failed due to invalid escape sequences (common when
		// Windows paths are placed in double-quoted TOML strings like
		// path = "C:\\path\to\db"), attempt a targeted fallback: read the
		// file and convert path assignments that contain backslashes into
		// single-quoted literal strings (TOML single quotes are literal and do
		// not process escape sequences). This keeps the change minimal and
		// localized to path assignments.
		if strings.Contains(err.Error(), "invalid escape") || strings.Contains(err.Error(), "invalid escape in string") {
			data, rerr := os.ReadFile(configPath)
			if rerr != nil {
				return fmt.Errorf("failed to parse config file: %w", err)
			}
			content := string(data)

			// Replace lines like: path = "C:\..."  -> path = 'C:\...'
			re := regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"\\]*\\[^\"]*)"`)
			transformed := re.ReplaceAllString(content, "path = '$1'")

			// Try decoding the transformed content
			if _, derr := toml.Decode(transformed, config); derr == nil {
				return nil
			}
			// Fall through to return original error if retry failed
		}
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
// ApplyDatabaseEnvOverrides applies database path overrides from environment.
// It supports a component-specific override via <PREFIX>_DB_PATH (e.g. SERVER_DB_PATH,
// AGENT_DB_PATH). If that is not set, it falls back to the generic DB_PATH.
func ApplyDatabaseEnvOverrides(cfg *DatabaseConfig, prefix string) {
	// Check component-specific env var first
	if prefix != "" {
		key := strings.ToUpper(prefix) + "_DB_PATH"
		if val := os.Getenv(key); val != "" {
			cfg.Path = val
			return
		}
	}

	// Fallback to generic DB_PATH
	if val := os.Getenv("DB_PATH"); val != "" {
		cfg.Path = val
	}
}

// ResolveConfigPath returns a configuration file path by checking environment
// variables with an optional component prefix, then falling back to the
// provided flagValue. Order of precedence:
// 1. <PREFIX>_CONFIG
// 2. <PREFIX>_CONFIG_PATH
// 3. CONFIG
// 4. CONFIG_PATH
// 5. flagValue (if non-empty)
// Returns empty string if none set.
func ResolveConfigPath(prefix string, flagValue string) string {
	// 1/2: component-specific
	if prefix != "" {
		key := strings.ToUpper(prefix) + "_CONFIG"
		if val := os.Getenv(key); val != "" {
			return val
		}
		key2 := strings.ToUpper(prefix) + "_CONFIG_PATH"
		if val := os.Getenv(key2); val != "" {
			return val
		}
	}

	// 3/4: generic
	if val := os.Getenv("CONFIG"); val != "" {
		return val
	}
	if val := os.Getenv("CONFIG_PATH"); val != "" {
		return val
	}

	// 5: CLI flag
	if flagValue != "" {
		return flagValue
	}

	return ""
}

// GetEnvPrefixed returns the value of an environment variable by checking
// the prefixed form first (<PREFIX>_<KEY>) and then falling back to the
// unprefixed KEY. Keys are upper-cased when combined with the prefix.
func GetEnvPrefixed(prefix, key string) string {
	if prefix != "" {
		full := strings.ToUpper(prefix) + "_" + strings.ToUpper(key)
		if val := os.Getenv(full); val != "" {
			return val
		}
	}
	return os.Getenv(strings.ToUpper(key))
}

func ApplyLoggingEnvOverrides(cfg *LoggingConfig) {
	if val := os.Getenv("LOG_LEVEL"); val != "" {
		cfg.Level = val
	}
}
