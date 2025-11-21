package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDefaultConfig(t *testing.T) {
	t.Parallel()

	t.Run("creates new config file", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		// Write default config
		err := WriteDefaultConfig(configPath)
		if err != nil {
			t.Fatalf("WriteDefaultConfig() failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("Config file was not created")
		}

		// Verify file content is valid TOML and contains expected sections
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		contentStr := string(content)
		expectedSections := []string{"[server]", "[tls]", "[database]", "[logging]"}
		for _, section := range expectedSections {
			if !strings.Contains(contentStr, section) {
				t.Errorf("Config file missing expected section: %s", section)
			}
		}

		// Verify default values
		if !strings.Contains(contentStr, "http_port = 9090") {
			t.Error("Config file missing default http_port value")
		}
		if !strings.Contains(contentStr, "https_port = 9443") {
			t.Error("Config file missing default https_port value")
		}
	})

	t.Run("does not overwrite existing config", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		// Create an existing config file with custom content
		existingContent := "# Custom config\n[server]\nhttp_port = 8888\n"
		if err := os.WriteFile(configPath, []byte(existingContent), 0644); err != nil {
			t.Fatalf("Failed to write existing config: %v", err)
		}

		// Try to write default config - should fail
		err := WriteDefaultConfig(configPath)
		if err == nil {
			t.Fatal("WriteDefaultConfig() should have failed when file exists")
		}

		// Verify error message indicates file exists
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("Error should mention 'already exists', got: %v", err)
		}

		// Verify original content was not modified
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		if string(content) != existingContent {
			t.Error("Existing config file was modified")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "nested", "dir", "config.toml")

		// Write default config - should create parent directories
		err := WriteDefaultConfig(configPath)
		if err != nil {
			t.Fatalf("WriteDefaultConfig() failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("Config file was not created in nested directory")
		}

		// Verify parent directories exist
		parentDir := filepath.Dir(configPath)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			t.Fatal("Parent directories were not created")
		}
	})
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	t.Run("loads valid config file", func(t *testing.T) {
		t.Parallel()

		// Create temp directory
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		// Write a valid config
		configContent := `
[server]
http_port = 8080
https_port = 8443
behind_proxy = true

[tls]
mode = "custom"
domain = "example.com"

[database]
path = "/custom/path/db.sqlite"

[logging]
level = "debug"
`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}

		// Load config
		cfg, _, err := LoadConfig(configPath)
		if err != nil {
			t.Fatalf("LoadConfig() failed: %v", err)
		}

		// Verify loaded values
		if cfg.Server.HTTPPort != 8080 {
			t.Errorf("HTTPPort = %d, want 8080", cfg.Server.HTTPPort)
		}
		if cfg.Server.HTTPSPort != 8443 {
			t.Errorf("HTTPSPort = %d, want 8443", cfg.Server.HTTPSPort)
		}
		if !cfg.Server.BehindProxy {
			t.Error("BehindProxy should be true")
		}
		if cfg.TLS.Mode != "custom" {
			t.Errorf("TLS.Mode = %s, want 'custom'", cfg.TLS.Mode)
		}
		if cfg.TLS.Domain != "example.com" {
			t.Errorf("TLS.Domain = %s, want 'example.com'", cfg.TLS.Domain)
		}
		if cfg.Database.Path != "/custom/path/db.sqlite" {
			t.Errorf("Database.Path = %s, want '/custom/path/db.sqlite'", cfg.Database.Path)
		}
		if cfg.Logging.Level != "debug" {
			t.Errorf("Logging.Level = %s, want 'debug'", cfg.Logging.Level)
		}
	})

	t.Run("returns defaults when file does not exist", func(t *testing.T) {
		t.Parallel()

		// Use non-existent path
		configPath := filepath.Join(t.TempDir(), "nonexistent.toml")

		// Load config - should use defaults
		cfg, _, err := LoadConfig(configPath)
		if err != nil {
			t.Fatalf("LoadConfig() failed: %v", err)
		}

		// Verify default values
		if cfg.Server.HTTPPort != 9090 {
			t.Errorf("HTTPPort = %d, want 9090 (default)", cfg.Server.HTTPPort)
		}
		if cfg.Server.HTTPSPort != 9443 {
			t.Errorf("HTTPSPort = %d, want 9443 (default)", cfg.Server.HTTPSPort)
		}
		if cfg.Server.BehindProxy {
			t.Error("BehindProxy should be false (default)")
		}
		if cfg.TLS.Mode != "self-signed" {
			t.Errorf("TLS.Mode = %s, want 'self-signed' (default)", cfg.TLS.Mode)
		}
		if cfg.Logging.Level != "info" {
			t.Errorf("Logging.Level = %s, want 'info' (default)", cfg.Logging.Level)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	// Verify server defaults
	if cfg.Server.HTTPPort != 9090 {
		t.Errorf("Server.HTTPPort = %d, want 9090", cfg.Server.HTTPPort)
	}
	if cfg.Server.HTTPSPort != 9443 {
		t.Errorf("Server.HTTPSPort = %d, want 9443", cfg.Server.HTTPSPort)
	}
	if cfg.Server.BehindProxy {
		t.Error("Server.BehindProxy should be false")
	}

	// Verify TLS defaults
	if cfg.TLS.Mode != "self-signed" {
		t.Errorf("TLS.Mode = %s, want 'self-signed'", cfg.TLS.Mode)
	}
	if cfg.TLS.Domain != "localhost" {
		t.Errorf("TLS.Domain = %s, want 'localhost'", cfg.TLS.Domain)
	}

	// Verify database defaults
	if cfg.Database.Path != "" {
		t.Errorf("Database.Path should be empty (use platform default), got %s", cfg.Database.Path)
	}

	// Verify logging defaults
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %s, want 'info'", cfg.Logging.Level)
	}
}
