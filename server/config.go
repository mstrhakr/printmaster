package main

import (
	"fmt"
	"os"

	"printmaster/common/config"
)

// Config represents the server configuration
type Config struct {
	Server   ServerConfig          `toml:"server"`
	TLS      TLSConfigTOML         `toml:"tls"`
	Database config.DatabaseConfig `toml:"database"`
	Logging  config.LoggingConfig  `toml:"logging"`
}

// ServerConfig holds server-specific settings
type ServerConfig struct {
	HTTPPort    int  `toml:"http_port"`
	HTTPSPort   int  `toml:"https_port"`
	BehindProxy bool `toml:"behind_proxy"`
}

// TLSConfigTOML holds TLS configuration from TOML
type TLSConfigTOML struct {
	Mode        string            `toml:"mode"`
	Domain      string            `toml:"domain"`
	CertPath    string            `toml:"cert_path"`
	KeyPath     string            `toml:"key_path"`
	LetsEncrypt LetsEncryptConfig `toml:"letsencrypt"`
}

// LetsEncryptConfig holds Let's Encrypt specific settings
type LetsEncryptConfig struct {
	Domain    string `toml:"domain"`
	Email     string `toml:"email"`
	CacheDir  string `toml:"cache_dir"`
	AcceptTOS bool   `toml:"accept_tos"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort:    9090,
			HTTPSPort:   9443,
			BehindProxy: false,
		},
		TLS: TLSConfigTOML{
			Mode:   "self-signed",
			Domain: "localhost",
			LetsEncrypt: LetsEncryptConfig{
				CacheDir:  "letsencrypt-cache",
				AcceptTOS: false,
			},
		},
		Database: config.DatabaseConfig{
			Path: "", // Empty = use platform default (ProgramData on Windows)
		},
		Logging: config.LoggingConfig{
			Level: "info",
		},
	}
}

// LoadConfig loads configuration from TOML file with environment variable overrides
func LoadConfig(configPath string) (*Config, error) {
	cfg := DefaultConfig()

	// Load from file if it exists
	if _, err := os.Stat(configPath); err == nil {
		if err := config.LoadTOML(configPath, cfg); err != nil {
			return nil, err
		}
	}

	// Override with environment variables
	if val := os.Getenv("SERVER_HTTP_PORT"); val != "" {
		var port int
		if _, err := fmt.Sscanf(val, "%d", &port); err == nil {
			cfg.Server.HTTPPort = port
		}
	}
	if val := os.Getenv("SERVER_HTTPS_PORT"); val != "" {
		var port int
		if _, err := fmt.Sscanf(val, "%d", &port); err == nil {
			cfg.Server.HTTPSPort = port
		}
	}
	if val := os.Getenv("BEHIND_PROXY"); val != "" {
		cfg.Server.BehindProxy = val == "true" || val == "1"
	}
	if val := os.Getenv("TLS_MODE"); val != "" {
		cfg.TLS.Mode = val
	}
	if val := os.Getenv("TLS_CERT_PATH"); val != "" {
		cfg.TLS.CertPath = val
	}
	if val := os.Getenv("TLS_KEY_PATH"); val != "" {
		cfg.TLS.KeyPath = val
	}
	if val := os.Getenv("LETSENCRYPT_DOMAIN"); val != "" {
		cfg.TLS.LetsEncrypt.Domain = val
	}
	if val := os.Getenv("LETSENCRYPT_EMAIL"); val != "" {
		cfg.TLS.LetsEncrypt.Email = val
	}
	if val := os.Getenv("LETSENCRYPT_ACCEPT_TOS"); val != "" {
		cfg.TLS.LetsEncrypt.AcceptTOS = val == "true" || val == "1"
	}

	// Apply common environment variable overrides
	config.ApplyDatabaseEnvOverrides(&cfg.Database)
	config.ApplyLoggingEnvOverrides(&cfg.Logging)

	return cfg, nil
}

// ToTLSConfig converts YAML TLS config to TLSConfig
func (c *Config) ToTLSConfig() *TLSConfig {
	mode := TLSModeSelfSigned
	switch c.TLS.Mode {
	case "letsencrypt":
		mode = TLSModeLetsEncrypt
	case "custom":
		mode = TLSModeCustom
	case "self-signed":
		mode = TLSModeSelfSigned
	}

	return &TLSConfig{
		Mode:              mode,
		Domain:            c.TLS.Domain,
		CertPath:          c.TLS.CertPath,
		KeyPath:           c.TLS.KeyPath,
		LetsEncryptDomain: c.TLS.LetsEncrypt.Domain,
		LetsEncryptEmail:  c.TLS.LetsEncrypt.Email,
		LetsEncryptCache:  c.TLS.LetsEncrypt.CacheDir,
		AcceptTOS:         c.TLS.LetsEncrypt.AcceptTOS,
		HTTPPort:          c.Server.HTTPPort,
		HTTPSPort:         c.Server.HTTPSPort,
		BehindProxy:       c.Server.BehindProxy,
	}
}

// WriteDefaultConfig writes a default configuration file
func WriteDefaultConfig(configPath string) error {
	cfg := DefaultConfig()
	return config.WriteDefaultTOML(configPath, cfg)
}
