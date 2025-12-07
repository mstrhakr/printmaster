// Package config provides shared configuration utilities for PrintMaster components
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
// When running in Docker (DOCKER env var set), returns the mounted volume directory
func GetDataDirectory(component string, isService bool) (string, error) {
	var dataDir string

	// Docker takes precedence - use mounted volume path
	if os.Getenv("DOCKER") != "" {
		dataDir = filepath.Join("/var/lib/printmaster", component)
	} else if isService {
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

	// Docker takes precedence - use mounted volume path
	if os.Getenv("DOCKER") != "" {
		logDir = filepath.Join("/var/log/printmaster", component)
	} else if isService {
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

// DatabaseConfig holds database settings supporting multiple backends.
// For SQLite: only Path is required.
// For PostgreSQL/MySQL: use Host, Port, User, Password, Name, and optionally SSLMode.
type DatabaseConfig struct {
	// Driver specifies the database backend: "sqlite" (default), "postgres", "mysql"
	Driver string `toml:"driver"`
	// Path is the SQLite database file path (only used for sqlite driver)
	Path string `toml:"path"`
	// DSN is a full connection string that overrides individual connection fields
	// Example for postgres: "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
	DSN string `toml:"dsn"`
	// Host is the database server hostname (for postgres/mysql)
	Host string `toml:"host"`
	// Port is the database server port (default: 5432 for postgres, 3306 for mysql)
	Port int `toml:"port"`
	// User is the database username
	User string `toml:"user"`
	// Password is the database password
	Password string `toml:"password"`
	// Name is the database name
	Name string `toml:"name"`
	// SSLMode controls SSL connection (postgres: disable, require, verify-ca, verify-full)
	SSLMode string `toml:"ssl_mode"`
	// MaxOpenConns limits the number of open connections (0 = unlimited, default: 25)
	MaxOpenConns int `toml:"max_open_conns"`
	// MaxIdleConns limits the number of idle connections (default: 5)
	MaxIdleConns int `toml:"max_idle_conns"`
	// ConnMaxLifetimeSecs is max connection lifetime in seconds (0 = no limit, default: 300)
	ConnMaxLifetimeSecs int `toml:"conn_max_lifetime_secs"`
}

// EffectiveDriver returns the database driver, defaulting to "sqlite" if not set.
func (c *DatabaseConfig) EffectiveDriver() string {
	if c.Driver == "" {
		return "sqlite"
	}
	return c.Driver
}

// BuildDSN constructs a connection string for the configured database driver.
// If DSN is explicitly set, it is returned directly. Otherwise, a DSN is
// built from the individual connection fields.
func (c *DatabaseConfig) BuildDSN() string {
	if c.DSN != "" {
		return c.DSN
	}

	switch c.EffectiveDriver() {
	case "postgres", "postgresql":
		// Build PostgreSQL connection string
		port := c.Port
		if port == 0 {
			port = 5432
		}
		sslMode := c.SSLMode
		if sslMode == "" {
			sslMode = "prefer"
		}
		dbName := c.Name
		if dbName == "" {
			dbName = "printmaster"
		}
		host := c.Host
		if host == "" {
			host = "localhost"
		}
		user := c.User
		if user == "" {
			user = "printmaster"
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			user, c.Password, host, port, dbName, sslMode)
		return dsn

	case "mysql", "mariadb":
		// Build MySQL/MariaDB connection string (DSN format: user:pass@tcp(host:port)/dbname)
		port := c.Port
		if port == 0 {
			port = 3306
		}
		dbName := c.Name
		if dbName == "" {
			dbName = "printmaster"
		}
		host := c.Host
		if host == "" {
			host = "localhost"
		}
		user := c.User
		if user == "" {
			user = "printmaster"
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
			user, c.Password, host, port, dbName)
		return dsn

	default:
		// SQLite: return path or default
		if c.Path != "" {
			return c.Path
		}
		return "printmaster.db"
	}
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level string `toml:"level"`
}

// ApplyEnvOverrides applies common environment variable overrides
// ApplyDatabaseEnvOverrides applies database config overrides from environment.
// It supports component-specific overrides via <PREFIX>_DB_* (e.g. SERVER_DB_DRIVER,
// AGENT_DB_PATH). If those are not set, it falls back to the generic DB_* variants.
func ApplyDatabaseEnvOverrides(cfg *DatabaseConfig, prefix string) {
	// Helper to get env with prefix fallback
	getEnv := func(key string) string {
		if prefix != "" {
			if val := os.Getenv(strings.ToUpper(prefix) + "_DB_" + key); val != "" {
				return val
			}
		}
		return os.Getenv("DB_" + key)
	}

	// Driver
	if val := getEnv("DRIVER"); val != "" {
		cfg.Driver = val
	}

	// Path (SQLite)
	if val := getEnv("PATH"); val != "" {
		cfg.Path = val
	}

	// DSN (full connection string)
	if val := getEnv("DSN"); val != "" {
		cfg.DSN = val
	}

	// Host
	if val := getEnv("HOST"); val != "" {
		cfg.Host = val
	}

	// Port
	if val := getEnv("PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Port = port
		}
	}

	// User
	if val := getEnv("USER"); val != "" {
		cfg.User = val
	}

	// Password (supports both DB_PASSWORD and DB_PASS)
	if val := getEnv("PASSWORD"); val != "" {
		cfg.Password = val
	} else if val := getEnv("PASS"); val != "" {
		cfg.Password = val
	}

	// Database name
	if val := getEnv("NAME"); val != "" {
		cfg.Name = val
	}

	// SSL Mode
	if val := getEnv("SSL_MODE"); val != "" {
		cfg.SSLMode = val
	}

	// Connection pool settings
	if val := getEnv("MAX_OPEN_CONNS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.MaxOpenConns = n
		}
	}
	if val := getEnv("MAX_IDLE_CONNS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.MaxIdleConns = n
		}
	}
	if val := getEnv("CONN_MAX_LIFETIME_SECS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			cfg.ConnMaxLifetimeSecs = n
		}
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
