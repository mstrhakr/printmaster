package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"printmaster/common/config"
)

// AgentConfig represents the agent configuration
type AgentConfig struct {
	AssetIDRegex string                 `toml:"asset_id_regex"`
	Concurrency  int                    `toml:"discovery_concurrency"`
	SNMP         SNMPConfig             `toml:"snmp"`
	Server       ServerConnectionConfig `toml:"server"`
	Database     config.DatabaseConfig  `toml:"database"`
	Logging      config.LoggingConfig   `toml:"logging"`
	Web          WebConfig              `toml:"web"`
}

// SNMPConfig holds SNMP client settings
type SNMPConfig struct {
	Community string `toml:"community"`
	TimeoutMs int    `toml:"timeout_ms"`
	Retries   int    `toml:"retries"`
}

// ServerConnectionConfig holds server registration and upload settings
type ServerConnectionConfig struct {
	Enabled              bool   `toml:"enabled"`
	URL                  string `toml:"url"`
	AgentID              string `toml:"agent_id"`
	CAPath               string `toml:"ca_path"`
	InsecureSkipVerify   bool   `toml:"insecure_skip_verify"` // Skip TLS verification (dev/testing only)
	UploadInterval       int    `toml:"upload_interval_seconds"`
	HeartbeatInterval    int    `toml:"heartbeat_interval_seconds"`
	Token                string `toml:"token"` // Stored after registration
}

// WebConfig holds web UI settings
type WebConfig struct {
	HTTPPort  int  `toml:"http_port"`
	HTTPSPort int  `toml:"https_port"`
	EnableTLS bool `toml:"enable_tls"`
}

// DefaultAgentConfig returns agent configuration with sensible defaults
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		AssetIDRegex: `\b\d{5}\b`,
		Concurrency:  50,
		SNMP: SNMPConfig{
			Community: "public",
			TimeoutMs: 2000,
			Retries:   1,
		},
		Server: ServerConnectionConfig{
			Enabled:            false,
			URL:                "",
			AgentID:            "",
			CAPath:             "",
			InsecureSkipVerify: false,
			UploadInterval:     300,
			HeartbeatInterval:  60,
			Token:              "",
		},
		Database: config.DatabaseConfig{
			Path: "", // Will use default platform-specific path
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
		Web: WebConfig{
			HTTPPort:  8080,
			HTTPSPort: 8443,
			EnableTLS: false,
		},
	}
}

// LoadAgentConfig loads configuration from TOML file with environment variable overrides
func LoadAgentConfig(configPath string) (*AgentConfig, error) {
	cfg := DefaultAgentConfig()

	// Load from file if it exists
	if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadTOML(configPath, cfg); err != nil {
			return nil, err
		}
	}

	// Override with environment variables
	if val := os.Getenv("ASSET_ID_REGEX"); val != "" {
		cfg.AssetIDRegex = val
	}
	if val := os.Getenv("DISCOVERY_CONCURRENCY"); val != "" {
		if concurrency, err := strconv.Atoi(val); err == nil {
			cfg.Concurrency = concurrency
		}
	}
	if val := os.Getenv("SNMP_COMMUNITY"); val != "" {
		cfg.SNMP.Community = val
	}
	if val := os.Getenv("SNMP_TIMEOUT_MS"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil {
			cfg.SNMP.TimeoutMs = timeout
		}
	}
	if val := os.Getenv("SNMP_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			cfg.SNMP.Retries = retries
		}
	}
	if val := os.Getenv("SERVER_ENABLED"); val != "" {
		cfg.Server.Enabled = val == "true" || val == "1"
	}
	if val := os.Getenv("SERVER_URL"); val != "" {
		cfg.Server.URL = val
	}
	if val := os.Getenv("AGENT_ID"); val != "" {
		cfg.Server.AgentID = val
	}
	if val := os.Getenv("SERVER_CA_PATH"); val != "" {
		cfg.Server.CAPath = val
	}
	if val := os.Getenv("WEB_HTTP_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Web.HTTPPort = port
		}
	}
	if val := os.Getenv("WEB_HTTPS_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			cfg.Web.HTTPSPort = port
		}
	}

	// Apply common environment variable overrides
	config.ApplyDatabaseEnvOverrides(&cfg.Database)
	config.ApplyLoggingEnvOverrides(&cfg.Logging)

	return cfg, nil
}

// WriteDefaultAgentConfig writes a default agent configuration file
func WriteDefaultAgentConfig(configPath string) error {
	cfg := DefaultAgentConfig()
	return config.WriteDefaultTOML(configPath, cfg)
}

// LoadServerToken loads the server authentication token from file
func LoadServerToken(dataDir string) string {
	tokenPath := filepath.Join(dataDir, "agent_token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "" // No token file = not registered yet
	}
	return strings.TrimSpace(string(data))
}

// SaveServerToken saves the server authentication token to file
func SaveServerToken(dataDir, token string) error {
	if token == "" {
		return nil // Don't save empty tokens
	}
	tokenPath := filepath.Join(dataDir, "agent_token")
	// Create directory if needed
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	// Write with restrictive permissions (owner read/write only)
	return os.WriteFile(tokenPath, []byte(token), 0600)
}
