package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"printmaster/common/config"
	"printmaster/common/updatepolicy"
)

func TestDefaultAgentConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultAgentConfig()

	// Test top-level settings
	if cfg.AssetIDRegex != `\b\d{5}\b` {
		t.Errorf("expected default AssetIDRegex to be '\\b\\d{5}\\b', got %s", cfg.AssetIDRegex)
	}
	if cfg.Concurrency != 50 {
		t.Errorf("expected default Concurrency to be 50, got %d", cfg.Concurrency)
	}
	if cfg.EpsonRemoteModeEnabled {
		t.Error("expected Epson remote mode to be disabled by default")
	}

	// Test SNMP settings
	if cfg.SNMP.Community != "public" {
		t.Errorf("expected default SNMP community to be 'public', got %s", cfg.SNMP.Community)
	}
	if cfg.SNMP.TimeoutMs != 2000 {
		t.Errorf("expected default SNMP timeout to be 2000ms, got %d", cfg.SNMP.TimeoutMs)
	}
	if cfg.SNMP.Retries != 1 {
		t.Errorf("expected default SNMP retries to be 1, got %d", cfg.SNMP.Retries)
	}

	// Test Server settings
	if cfg.Server.Enabled {
		t.Error("expected server to be disabled by default")
	}
	if cfg.Server.UploadInterval != 300 {
		t.Errorf("expected default upload interval to be 300s, got %d", cfg.Server.UploadInterval)
	}
	if cfg.Server.HeartbeatInterval != 60 {
		t.Errorf("expected default heartbeat interval to be 60s, got %d", cfg.Server.HeartbeatInterval)
	}

	// Test Web settings
	if cfg.Web.HTTPPort != 8080 {
		t.Errorf("expected default HTTP port to be 8080, got %d", cfg.Web.HTTPPort)
	}
	if cfg.Web.HTTPSPort != 8443 {
		t.Errorf("expected default HTTPS port to be 8443, got %d", cfg.Web.HTTPSPort)
	}
	if cfg.Web.EnableTLS {
		t.Error("expected TLS to be disabled by default")
	}

	// Test Logging settings
	if cfg.Logging.Level != "info" {
		t.Errorf("expected default log level to be 'info', got %s", cfg.Logging.Level)
	}

	if cfg.AutoUpdate.Mode != updatepolicy.AgentOverrideInherit {
		t.Errorf("expected default auto-update mode inherit, got %s", cfg.AutoUpdate.Mode)
	}
	if cfg.AutoUpdate.LocalPolicy.UpdateCheckDays != 7 {
		t.Errorf("expected default local update cadence 7 days, got %d", cfg.AutoUpdate.LocalPolicy.UpdateCheckDays)
	}
}

func TestLoadAgentConfig(t *testing.T) {
	t.Parallel()

	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	configContent := `asset_id_regex = "\\b\\d{6}\\b"
discovery_concurrency = 100
	epson_remote_mode_enabled = true

[snmp]
community = "private"
timeout_ms = 3000
retries = 2

[server]
enabled = true
url = "https://test.example.com:9443"
agent_id = "test-agent-001"
ca_path = "/path/to/ca.crt"
upload_interval_seconds = 600
heartbeat_interval_seconds = 120

	[auto_update]
	mode = "local"

		[auto_update.local_policy]
		update_check_days = 3
		version_pin_strategy = "patch"
		allow_major_upgrade = true
		target_version = "0.9.42"
		collect_telemetry = false

[web]
http_port = 9090
https_port = 9443
enable_tls = true

[logging]
level = "debug"
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Load config
	cfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify loaded values
	if cfg.AssetIDRegex != `\b\d{6}\b` {
		t.Errorf("expected AssetIDRegex to be '\\b\\d{6}\\b', got %s", cfg.AssetIDRegex)
	}
	if cfg.Concurrency != 100 {
		t.Errorf("expected Concurrency to be 100, got %d", cfg.Concurrency)
	}
	if cfg.SNMP.Community != "private" {
		t.Errorf("expected SNMP community to be 'private', got %s", cfg.SNMP.Community)
	}
	if cfg.SNMP.TimeoutMs != 3000 {
		t.Errorf("expected SNMP timeout to be 3000ms, got %d", cfg.SNMP.TimeoutMs)
	}
	if cfg.SNMP.Retries != 2 {
		t.Errorf("expected SNMP retries to be 2, got %d", cfg.SNMP.Retries)
	}
	if !cfg.Server.Enabled {
		t.Error("expected server to be enabled")
	}
	if cfg.Server.URL != "https://test.example.com:9443" {
		t.Errorf("expected server URL to be 'https://test.example.com:9443', got %s", cfg.Server.URL)
	}
	if cfg.Server.AgentID != "test-agent-001" {
		t.Errorf("expected agent ID to be 'test-agent-001', got %s", cfg.Server.AgentID)
	}
	if cfg.Server.UploadInterval != 600 {
		t.Errorf("expected upload interval to be 600s, got %d", cfg.Server.UploadInterval)
	}
	if cfg.Web.HTTPPort != 9090 {
		t.Errorf("expected HTTP port to be 9090, got %d", cfg.Web.HTTPPort)
	}
	if !cfg.Web.EnableTLS {
		t.Error("expected TLS to be enabled")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected log level to be 'debug', got %s", cfg.Logging.Level)
	}
	if !cfg.EpsonRemoteModeEnabled {
		t.Error("expected Epson remote mode to be enabled when set in config")
	}
	if cfg.AutoUpdate.Mode != updatepolicy.AgentOverrideLocal {
		t.Errorf("expected auto-update mode local, got %s", cfg.AutoUpdate.Mode)
	}
	if cfg.AutoUpdate.LocalPolicy.UpdateCheckDays != 3 {
		t.Errorf("expected local update cadence 3 days, got %d", cfg.AutoUpdate.LocalPolicy.UpdateCheckDays)
	}
	if cfg.AutoUpdate.LocalPolicy.VersionPinStrategy != updatepolicy.VersionPinPatch {
		t.Errorf("expected local pin strategy patch, got %s", cfg.AutoUpdate.LocalPolicy.VersionPinStrategy)
	}
	if !cfg.AutoUpdate.LocalPolicy.AllowMajorUpgrade {
		t.Error("expected local policy to allow major upgrades")
	}
	if cfg.AutoUpdate.LocalPolicy.TargetVersion != "0.9.42" {
		t.Errorf("expected target version 0.9.42, got %s", cfg.AutoUpdate.LocalPolicy.TargetVersion)
	}
	if cfg.AutoUpdate.LocalPolicy.CollectTelemetry {
		t.Error("expected telemetry disabled in local policy")
	}
}

func TestLoadAgentConfigWithEnvOverrides(t *testing.T) {
	// Note: Cannot use t.Parallel() here because we modify global environment

	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	configContent := `asset_id_regex = "\\b\\d{5}\\b"

[snmp]
community = "public"
timeout_ms = 2000
`

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Save original environment
	envVars := []string{"ASSET_ID_REGEX", "SNMP_COMMUNITY", "SNMP_TIMEOUT_MS", "SNMP_RETRIES", "SERVER_ENABLED", "SERVER_URL", "WEB_HTTP_PORT", "EPSON_REMOTE_MODE_ENABLED", "AUTO_UPDATE_MODE"}
	originalEnv := make(map[string]string)
	for _, key := range envVars {
		originalEnv[key] = os.Getenv(key)
	}

	// Set environment variables
	os.Setenv("ASSET_ID_REGEX", `\b\d{7}\b`)
	os.Setenv("SNMP_COMMUNITY", "secret")
	os.Setenv("SNMP_TIMEOUT_MS", "5000")
	os.Setenv("SNMP_RETRIES", "3")
	os.Setenv("SERVER_ENABLED", "true")
	os.Setenv("SERVER_URL", "https://env.example.com")
	os.Setenv("WEB_HTTP_PORT", "7070")
	os.Setenv("EPSON_REMOTE_MODE_ENABLED", "true")
	os.Setenv("AUTO_UPDATE_MODE", "disabled")

	// Restore original environment
	defer func() {
		for key, val := range originalEnv {
			if val == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, val)
			}
		}
	}()

	// Load config
	cfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify environment overrides
	if cfg.AssetIDRegex != `\b\d{7}\b` {
		t.Errorf("expected AssetIDRegex to be overridden to '\\b\\d{7}\\b', got %s", cfg.AssetIDRegex)
	}
	if cfg.SNMP.Community != "secret" {
		t.Errorf("expected SNMP community to be overridden to 'secret', got %s", cfg.SNMP.Community)
	}
	if cfg.SNMP.TimeoutMs != 5000 {
		t.Errorf("expected SNMP timeout to be overridden to 5000ms, got %d", cfg.SNMP.TimeoutMs)
	}
	if cfg.SNMP.Retries != 3 {
		t.Errorf("expected SNMP retries to be overridden to 3, got %d", cfg.SNMP.Retries)
	}
	if !cfg.Server.Enabled {
		t.Error("expected server to be enabled via env override")
	}
	if cfg.Server.URL != "https://env.example.com" {
		t.Errorf("expected server URL to be overridden to 'https://env.example.com', got %s", cfg.Server.URL)
	}
	if cfg.Web.HTTPPort != 7070 {
		t.Errorf("expected HTTP port to be overridden to 7070, got %d", cfg.Web.HTTPPort)
	}
	if !cfg.EpsonRemoteModeEnabled {
		t.Error("expected Epson remote mode to be enabled via env override")
	}
	if cfg.AutoUpdate.Mode != updatepolicy.AgentOverrideNever {
		t.Errorf("expected auto-update mode disabled via env override, got %s", cfg.AutoUpdate.Mode)
	}
}

func TestLoadAgentConfigNonExistent(t *testing.T) {
	t.Parallel()

	// Load config from non-existent file (should return error)
	cfg, err := LoadAgentConfig("/nonexistent/config.toml")
	if err == nil {
		t.Fatalf("expected error when config file doesn't exist, got nil")
	}
	if cfg != nil {
		t.Errorf("expected nil config when file doesn't exist, got %+v", cfg)
	}
}

func TestWriteDefaultAgentConfig(t *testing.T) {
	// Note: Cannot use t.Parallel() here because we need clean environment

	// Save and clear any environment variables that might affect the test
	envVars := []string{
		"ASSET_ID_REGEX", "DISCOVERY_CONCURRENCY", "SNMP_COMMUNITY",
		"SNMP_TIMEOUT_MS", "SNMP_RETRIES", "SERVER_ENABLED", "SERVER_URL",
		"AGENT_ID", "SERVER_CA_PATH", "WEB_HTTP_PORT", "WEB_HTTPS_PORT",
		"EPSON_REMOTE_MODE_ENABLED",
	}
	originalValues := make(map[string]string)
	for _, key := range envVars {
		originalValues[key] = os.Getenv(key)
		os.Unsetenv(key)
	}
	defer func() {
		for key, val := range originalValues {
			if val == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, val)
			}
		}
	}()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "generated-config.toml")

	// Write default config
	if err := WriteDefaultAgentConfig(configPath); err != nil {
		t.Fatalf("failed to write default config: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load the generated config
	cfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load generated config: %v", err)
	}

	// Verify it matches defaults
	defaultCfg := DefaultAgentConfig()
	if cfg.AssetIDRegex != defaultCfg.AssetIDRegex {
		t.Errorf("generated config AssetIDRegex mismatch: got %s, want %s", cfg.AssetIDRegex, defaultCfg.AssetIDRegex)
	}
	if cfg.SNMP.Community != defaultCfg.SNMP.Community {
		t.Errorf("generated config SNMP community mismatch: got %s, want %s", cfg.SNMP.Community, defaultCfg.SNMP.Community)
	}
}

func TestWriteDefaultAgentConfigDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "existing-config.toml")

	// Create an existing config file with custom content
	existingContent := "# Custom agent config\n[snmp]\ncommunity = \"custom-community\"\n"
	if err := os.WriteFile(configPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Try to write default config - should fail
	err := WriteDefaultAgentConfig(configPath)
	if err == nil {
		t.Fatal("WriteDefaultAgentConfig() should have failed when file exists")
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
}

func TestLoadServerToken(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Test with no token file
	token := LoadServerToken(tempDir)
	if token != "" {
		t.Errorf("expected empty token when file doesn't exist, got %s", token)
	}

	// Test with token file
	expectedToken := "test-token-12345"
	tokenPath := filepath.Join(tempDir, "agent_token")
	if err := os.WriteFile(tokenPath, []byte(expectedToken+"\n"), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	token = LoadServerToken(tempDir)
	if token != expectedToken {
		t.Errorf("expected token %s, got %s", expectedToken, token)
	}

	// Test token is trimmed
	if err := os.WriteFile(tokenPath, []byte("  "+expectedToken+"  \n  "), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	token = LoadServerToken(tempDir)
	if token != expectedToken {
		t.Errorf("expected trimmed token %s, got %s", expectedToken, token)
	}
}

func TestSaveServerToken(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	token := "test-token-67890"

	// Save token
	if err := SaveServerToken(tempDir, token); err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Verify file exists
	tokenPath := filepath.Join(tempDir, "agent_token")
	if _, err := os.Stat(tokenPath); os.IsNotExist(err) {
		t.Fatal("token file was not created")
	}

	// Verify file permissions (on Unix-like systems)
	if info, err := os.Stat(tokenPath); err == nil {
		// Check that file is not world-readable
		mode := info.Mode()
		if mode.Perm()&0077 != 0 {
			t.Logf("Warning: token file has loose permissions: %v", mode.Perm())
		}
	}

	// Verify content
	loadedToken := LoadServerToken(tempDir)
	if loadedToken != token {
		t.Errorf("expected loaded token %s, got %s", token, loadedToken)
	}

	// Test empty token (should not save)
	if err := SaveServerToken(tempDir, ""); err != nil {
		t.Errorf("expected no error for empty token, got: %v", err)
	}
}

func TestSaveServerTokenCreatesDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested", "path")
	token := "test-token-nested"

	// Save token to non-existent directory
	if err := SaveServerToken(nestedDir, token); err != nil {
		t.Fatalf("failed to save token to nested directory: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Fatal("nested directory was not created")
	}

	// Verify token was saved
	loadedToken := LoadServerToken(nestedDir)
	if loadedToken != token {
		t.Errorf("expected loaded token %s, got %s", token, loadedToken)
	}
}

func TestDeleteServerToken(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	tokenPath := filepath.Join(tempDir, "agent_token")
	if err := os.WriteFile(tokenPath, []byte("token"), 0600); err != nil {
		t.Fatalf("failed to seed token file: %v", err)
	}

	if err := DeleteServerToken(tempDir); err != nil {
		t.Fatalf("expected delete to succeed: %v", err)
	}

	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("expected token file to be removed, got err=%v", err)
	}

	// Second delete should be a no-op
	if err := DeleteServerToken(tempDir); err != nil {
		t.Fatalf("expected idempotent delete, got %v", err)
	}

	if err := DeleteServerToken(""); err == nil {
		t.Fatalf("expected error when data directory missing")
	}
}

func TestLoadServerJoinToken(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	if token := LoadServerJoinToken(tempDir); token != "" {
		t.Fatalf("expected empty token for missing file, got %s", token)
	}

	expected := "join-token-abc"
	path := filepath.Join(tempDir, "server_join_token")
	if err := os.WriteFile(path, []byte("  "+expected+"  \n"), 0600); err != nil {
		t.Fatalf("failed to write join token: %v", err)
	}

	if token := LoadServerJoinToken(tempDir); token != expected {
		t.Fatalf("expected %s, got %s", expected, token)
	}
}

func TestSaveServerJoinToken(t *testing.T) {
	t.Parallel()

	if err := SaveServerJoinToken("", "abc"); err == nil {
		t.Fatal("expected error when data directory is empty")
	}

	tempDir := t.TempDir()
	joinToken := "join-token-xyz"
	if err := SaveServerJoinToken(tempDir, joinToken); err != nil {
		t.Fatalf("failed to save join token: %v", err)
	}

	if token := LoadServerJoinToken(tempDir); token != joinToken {
		t.Fatalf("expected %s, got %s", joinToken, token)
	}

	if err := SaveServerJoinToken(tempDir, ""); err != nil {
		t.Fatalf("expected nil error when clearing join token, got %v", err)
	}

	path := filepath.Join(tempDir, "server_join_token")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected join token file to be removed, err=%v", err)
	}
}

func TestAgentConfigTOMLRoundTrip(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "roundtrip.toml")

	// Create a config with custom values
	originalCfg := &AgentConfig{
		AssetIDRegex: `\b\d{8}\b`,
		Concurrency:  75,
		SNMP: SNMPConfig{
			Community: "test-community",
			TimeoutMs: 4500,
			Retries:   5,
		},
		Server: ServerConnectionConfig{
			Enabled:           true,
			URL:               "https://roundtrip.example.com:9999",
			AgentID:           "roundtrip-agent",
			CAPath:            "/roundtrip/ca.crt",
			UploadInterval:    900,
			HeartbeatInterval: 180,
		},
		Database: config.DatabaseConfig{
			Path: "/custom/db/path",
		},
		Logging: config.LoggingConfig{
			Level: "trace",
		},
		Web: WebConfig{
			HTTPPort:  7777,
			HTTPSPort: 8888,
			EnableTLS: true,
		},
	}

	// Write config
	if err := config.WriteDefaultTOML(configPath, originalCfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load config back
	loadedCfg, err := LoadAgentConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Compare values
	if loadedCfg.AssetIDRegex != originalCfg.AssetIDRegex {
		t.Errorf("AssetIDRegex mismatch: got %s, want %s", loadedCfg.AssetIDRegex, originalCfg.AssetIDRegex)
	}
	if loadedCfg.Concurrency != originalCfg.Concurrency {
		t.Errorf("Concurrency mismatch: got %d, want %d", loadedCfg.Concurrency, originalCfg.Concurrency)
	}
	if loadedCfg.SNMP.Community != originalCfg.SNMP.Community {
		t.Errorf("SNMP Community mismatch: got %s, want %s", loadedCfg.SNMP.Community, originalCfg.SNMP.Community)
	}
	if loadedCfg.Server.URL != originalCfg.Server.URL {
		t.Errorf("Server URL mismatch: got %s, want %s", loadedCfg.Server.URL, originalCfg.Server.URL)
	}
	if loadedCfg.Web.HTTPPort != originalCfg.Web.HTTPPort {
		t.Errorf("Web HTTPPort mismatch: got %d, want %d", loadedCfg.Web.HTTPPort, originalCfg.Web.HTTPPort)
	}
}
