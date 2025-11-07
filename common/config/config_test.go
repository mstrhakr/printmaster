package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfig is a simple config structure for testing
type TestConfig struct {
	Name  string `toml:"name"`
	Value int    `toml:"value"`
}

func TestWriteDefaultTOML(t *testing.T) {
	t.Parallel()

	t.Run("creates new config file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.toml")

		testCfg := TestConfig{
			Name:  "test",
			Value: 42,
		}

		err := WriteDefaultTOML(configPath, testCfg)
		if err != nil {
			t.Fatalf("WriteDefaultTOML() failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("Config file was not created")
		}

		// Verify content
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		contentStr := string(content)
		if !strings.Contains(contentStr, "name = \"test\"") {
			t.Error("Config file missing expected name value")
		}
		if !strings.Contains(contentStr, "value = 42") {
			t.Error("Config file missing expected value")
		}
	})

	t.Run("does not overwrite existing file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "existing.toml")

		// Create existing file
		existingContent := "# Existing config\nname = \"old\"\nvalue = 99\n"
		if err := os.WriteFile(configPath, []byte(existingContent), 0644); err != nil {
			t.Fatalf("Failed to create existing file: %v", err)
		}

		testCfg := TestConfig{
			Name:  "new",
			Value: 1,
		}

		// Attempt to write should fail
		err := WriteDefaultTOML(configPath, testCfg)
		if err == nil {
			t.Fatal("WriteDefaultTOML() should have failed for existing file")
		}

		// Verify error message
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("Error should mention 'already exists', got: %v", err)
		}

		// Verify original content unchanged
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}

		if string(content) != existingContent {
			t.Error("Existing file was modified")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "deep", "nested", "path", "config.toml")

		testCfg := TestConfig{
			Name:  "nested",
			Value: 123,
		}

		err := WriteDefaultTOML(configPath, testCfg)
		if err != nil {
			t.Fatalf("WriteDefaultTOML() failed: %v", err)
		}

		// Verify file exists
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			t.Fatal("Config file was not created in nested path")
		}

		// Verify parent directories exist
		parentDir := filepath.Dir(configPath)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			t.Fatal("Parent directories were not created")
		}
	})
}

func TestLoadTOML(t *testing.T) {
	t.Parallel()

	t.Run("loads valid config", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "valid.toml")

		// Create a valid config file
		configContent := "name = \"loaded\"\nvalue = 999\n"
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}

		var cfg TestConfig
		err := LoadTOML(configPath, &cfg)
		if err != nil {
			t.Fatalf("LoadTOML() failed: %v", err)
		}

		// Verify loaded values
		if cfg.Name != "loaded" {
			t.Errorf("Name = %s, want 'loaded'", cfg.Name)
		}
		if cfg.Value != 999 {
			t.Errorf("Value = %d, want 999", cfg.Value)
		}
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "missing.toml")

		var cfg TestConfig
		err := LoadTOML(configPath, &cfg)
		if err == nil {
			t.Fatal("LoadTOML() should fail for missing file")
		}

		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Error should mention 'not found', got: %v", err)
		}
	})

	t.Run("returns error for invalid TOML", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "invalid.toml")

		// Create invalid TOML
		if err := os.WriteFile(configPath, []byte("this is not valid TOML {{{}}}"), 0644); err != nil {
			t.Fatalf("Failed to create invalid config: %v", err)
		}

		var cfg TestConfig
		err := LoadTOML(configPath, &cfg)
		if err == nil {
			t.Fatal("LoadTOML() should fail for invalid TOML")
		}
	})
}
