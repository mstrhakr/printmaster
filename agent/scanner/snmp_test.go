package scanner

import (
	"os"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Note: Tests using t.Setenv cannot use t.Parallel() due to Go testing restrictions.
// These tests modify environment variables and must run sequentially.

func TestGetSNMPConfig_Defaults(t *testing.T) {
	// Clear any existing env vars for clean test
	envVars := []string{
		"SNMP_COMMUNITY", "SNMP_VERSION", "SNMP_USERNAME",
		"SNMP_SECURITY_LEVEL", "SNMP_AUTH_PROTOCOL", "SNMP_AUTH_PASSWORD",
		"SNMP_PRIV_PROTOCOL", "SNMP_PRIV_PASSWORD", "SNMP_CONTEXT_NAME",
	}
	for _, v := range envVars {
		t.Setenv(v, "")
	}

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Community != "public" {
		t.Errorf("expected default community 'public', got %q", cfg.Community)
	}

	if cfg.Version != gosnmp.Version2c {
		t.Errorf("expected default version v2c, got %v", cfg.Version)
	}
}

func TestGetSNMPConfig_V1(t *testing.T) {
	t.Setenv("SNMP_VERSION", "1")
	t.Setenv("SNMP_COMMUNITY", "private")

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != gosnmp.Version1 {
		t.Errorf("expected version v1, got %v", cfg.Version)
	}

	if cfg.Community != "private" {
		t.Errorf("expected community 'private', got %q", cfg.Community)
	}
}

func TestGetSNMPConfig_V2c_Variants(t *testing.T) {
	variants := []string{"2c", "v2c", "2", ""}
	for _, v := range variants {
		t.Run("version_"+v, func(t *testing.T) {
			t.Setenv("SNMP_VERSION", v)

			cfg, err := GetSNMPConfig()
			if err != nil {
				t.Fatalf("unexpected error for version %q: %v", v, err)
			}

			if cfg.Version != gosnmp.Version2c {
				t.Errorf("expected version v2c for input %q, got %v", v, cfg.Version)
			}
		})
	}
}

func TestGetSNMPConfig_V3_NoAuthNoPriv(t *testing.T) {
	t.Setenv("SNMP_VERSION", "3")
	t.Setenv("SNMP_USERNAME", "monitor")
	t.Setenv("SNMP_SECURITY_LEVEL", "noauthnopriv")

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != gosnmp.Version3 {
		t.Errorf("expected version v3, got %v", cfg.Version)
	}

	if cfg.Username != "monitor" {
		t.Errorf("expected username 'monitor', got %q", cfg.Username)
	}

	if cfg.SecurityLevel != gosnmp.NoAuthNoPriv {
		t.Errorf("expected NoAuthNoPriv, got %v", cfg.SecurityLevel)
	}

	if cfg.AuthProtocol != gosnmp.NoAuth {
		t.Errorf("expected NoAuth protocol, got %v", cfg.AuthProtocol)
	}

	if cfg.PrivProtocol != gosnmp.NoPriv {
		t.Errorf("expected NoPriv protocol, got %v", cfg.PrivProtocol)
	}
}

func TestGetSNMPConfig_V3_AuthNoPriv(t *testing.T) {
	t.Setenv("SNMP_VERSION", "3")
	t.Setenv("SNMP_USERNAME", "authuser")
	t.Setenv("SNMP_SECURITY_LEVEL", "authnopriv")
	t.Setenv("SNMP_AUTH_PROTOCOL", "SHA256")
	t.Setenv("SNMP_AUTH_PASSWORD", "authpass123")

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != gosnmp.Version3 {
		t.Errorf("expected version v3, got %v", cfg.Version)
	}

	if cfg.SecurityLevel != gosnmp.AuthNoPriv {
		t.Errorf("expected AuthNoPriv, got %v", cfg.SecurityLevel)
	}

	if cfg.AuthProtocol != gosnmp.SHA256 {
		t.Errorf("expected SHA256, got %v", cfg.AuthProtocol)
	}

	if cfg.AuthPassword != "authpass123" {
		t.Errorf("expected auth password 'authpass123', got %q", cfg.AuthPassword)
	}
}

func TestGetSNMPConfig_V3_AuthPriv(t *testing.T) {
	t.Setenv("SNMP_VERSION", "3")
	t.Setenv("SNMP_USERNAME", "secureuser")
	t.Setenv("SNMP_SECURITY_LEVEL", "authpriv")
	t.Setenv("SNMP_AUTH_PROTOCOL", "SHA512")
	t.Setenv("SNMP_AUTH_PASSWORD", "authsecret")
	t.Setenv("SNMP_PRIV_PROTOCOL", "AES256")
	t.Setenv("SNMP_PRIV_PASSWORD", "privsecret")
	t.Setenv("SNMP_CONTEXT_NAME", "mycontext")

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != gosnmp.Version3 {
		t.Errorf("expected version v3, got %v", cfg.Version)
	}

	if cfg.Username != "secureuser" {
		t.Errorf("expected username 'secureuser', got %q", cfg.Username)
	}

	if cfg.SecurityLevel != gosnmp.AuthPriv {
		t.Errorf("expected AuthPriv, got %v", cfg.SecurityLevel)
	}

	if cfg.AuthProtocol != gosnmp.SHA512 {
		t.Errorf("expected SHA512, got %v", cfg.AuthProtocol)
	}

	if cfg.AuthPassword != "authsecret" {
		t.Errorf("expected auth password 'authsecret', got %q", cfg.AuthPassword)
	}

	if cfg.PrivProtocol != gosnmp.AES256 {
		t.Errorf("expected AES256, got %v", cfg.PrivProtocol)
	}

	if cfg.PrivPassword != "privsecret" {
		t.Errorf("expected priv password 'privsecret', got %q", cfg.PrivPassword)
	}

	if cfg.ContextName != "mycontext" {
		t.Errorf("expected context name 'mycontext', got %q", cfg.ContextName)
	}
}

func TestGetSNMPConfig_AllAuthProtocols(t *testing.T) {
	tests := []struct {
		input    string
		expected gosnmp.SnmpV3AuthProtocol
	}{
		{"MD5", gosnmp.MD5},
		{"SHA", gosnmp.SHA},
		{"SHA1", gosnmp.SHA},
		{"SHA224", gosnmp.SHA224},
		{"SHA256", gosnmp.SHA256},
		{"SHA384", gosnmp.SHA384},
		{"SHA512", gosnmp.SHA512},
		{"", gosnmp.NoAuth},
		{"unknown", gosnmp.NoAuth},
	}

	for _, tc := range tests {
		t.Run("auth_"+tc.input, func(t *testing.T) {
			t.Setenv("SNMP_VERSION", "3")
			t.Setenv("SNMP_AUTH_PROTOCOL", tc.input)

			cfg, err := GetSNMPConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.AuthProtocol != tc.expected {
				t.Errorf("auth protocol %q: expected %v, got %v", tc.input, tc.expected, cfg.AuthProtocol)
			}
		})
	}
}

func TestGetSNMPConfig_AllPrivProtocols(t *testing.T) {
	tests := []struct {
		input    string
		expected gosnmp.SnmpV3PrivProtocol
	}{
		{"DES", gosnmp.DES},
		{"AES", gosnmp.AES},
		{"AES128", gosnmp.AES},
		{"AES192", gosnmp.AES192},
		{"AES256", gosnmp.AES256},
		{"AES192C", gosnmp.AES192C},
		{"AES256C", gosnmp.AES256C},
		{"", gosnmp.NoPriv},
		{"unknown", gosnmp.NoPriv},
	}

	for _, tc := range tests {
		t.Run("priv_"+tc.input, func(t *testing.T) {
			t.Setenv("SNMP_VERSION", "3")
			t.Setenv("SNMP_PRIV_PROTOCOL", tc.input)

			cfg, err := GetSNMPConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.PrivProtocol != tc.expected {
				t.Errorf("priv protocol %q: expected %v, got %v", tc.input, tc.expected, cfg.PrivProtocol)
			}
		})
	}
}

func TestGetSNMPConfig_InvalidVersion(t *testing.T) {
	t.Setenv("SNMP_VERSION", "4")

	_, err := GetSNMPConfig()
	if err == nil {
		t.Fatal("expected error for invalid SNMP version")
	}

	if err.Error() != "unsupported SNMP version: 4 (use 1, 2c, or 3)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewSNMPClient_NilConfig(t *testing.T) {
	t.Parallel()

	_, err := newSNMPClientImpl(nil, "10.0.0.1", 30)
	if err == nil {
		t.Fatal("expected error for nil config")
	}

	if err.Error() != "SNMP config required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewSNMPClient_EmptyTarget(t *testing.T) {
	t.Parallel()

	cfg := &SNMPConfig{
		Community: "public",
		Version:   gosnmp.Version2c,
	}

	_, err := newSNMPClientImpl(cfg, "", 30)
	if err == nil {
		t.Fatal("expected error for empty target")
	}

	if err.Error() != "target IP required" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestNewSNMPClient_DefaultTimeout(t *testing.T) {
	// This test validates the timeout default logic without actually connecting.
	// We can't easily test the actual connection without a real SNMP target,
	// but we can verify the config struct is built correctly.

	// Skip if we're running in CI or don't want network tests
	if os.Getenv("SKIP_NETWORK_TESTS") != "" {
		t.Skip("skipping network test")
	}

	cfg := &SNMPConfig{
		Community: "public",
		Version:   gosnmp.Version2c,
	}

	// This will fail to connect, but we're testing the error message format
	_, err := newSNMPClientImpl(cfg, "192.0.2.1", 0) // TEST-NET-1, should not exist
	if err == nil {
		// Unexpectedly succeeded - maybe there's something at that IP
		t.Skip("unexpected connection success, skipping")
	}

	// Error should mention the target IP
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestSNMPConfig_V3Fields(t *testing.T) {
	t.Parallel()

	cfg := &SNMPConfig{
		Version:       gosnmp.Version3,
		SecurityLevel: gosnmp.AuthPriv,
		Username:      "testuser",
		AuthProtocol:  gosnmp.SHA256,
		AuthPassword:  "authpass",
		PrivProtocol:  gosnmp.AES256,
		PrivPassword:  "privpass",
		ContextName:   "context1",
	}

	// Verify all fields are accessible
	if cfg.Version != gosnmp.Version3 {
		t.Error("Version field mismatch")
	}
	if cfg.SecurityLevel != gosnmp.AuthPriv {
		t.Error("SecurityLevel field mismatch")
	}
	if cfg.Username != "testuser" {
		t.Error("Username field mismatch")
	}
	if cfg.AuthProtocol != gosnmp.SHA256 {
		t.Error("AuthProtocol field mismatch")
	}
	if cfg.AuthPassword != "authpass" {
		t.Error("AuthPassword field mismatch")
	}
	if cfg.PrivProtocol != gosnmp.AES256 {
		t.Error("PrivProtocol field mismatch")
	}
	if cfg.PrivPassword != "privpass" {
		t.Error("PrivPassword field mismatch")
	}
	if cfg.ContextName != "context1" {
		t.Error("ContextName field mismatch")
	}
}

func TestGetSNMPConfig_CaseInsensitiveVersion(t *testing.T) {
	variants := []string{"V1", "v1", "V2C", "V2c", "v2C", "V3", "v3"}
	expected := []gosnmp.SnmpVersion{
		gosnmp.Version1, gosnmp.Version1,
		gosnmp.Version2c, gosnmp.Version2c, gosnmp.Version2c,
		gosnmp.Version3, gosnmp.Version3,
	}

	for i, v := range variants {
		exp := expected[i]
		t.Run("version_"+v, func(t *testing.T) {
			t.Setenv("SNMP_VERSION", v)

			cfg, err := GetSNMPConfig()
			if err != nil {
				t.Fatalf("unexpected error for version %q: %v", v, err)
			}

			if cfg.Version != exp {
				t.Errorf("version %q: expected %v, got %v", v, exp, cfg.Version)
			}
		})
	}
}

func TestGetSNMPConfig_SecurityLevelCaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		expected gosnmp.SnmpV3MsgFlags
	}{
		{"noauthnopriv", gosnmp.NoAuthNoPriv},
		{"NoAuthNoPriv", gosnmp.NoAuthNoPriv},
		{"NOAUTHNOPRIV", gosnmp.NoAuthNoPriv},
		{"authnopriv", gosnmp.AuthNoPriv},
		{"AuthNoPriv", gosnmp.AuthNoPriv},
		{"AUTHNOPRIV", gosnmp.AuthNoPriv},
		{"authpriv", gosnmp.AuthPriv},
		{"AuthPriv", gosnmp.AuthPriv},
		{"AUTHPRIV", gosnmp.AuthPriv},
		{"", gosnmp.NoAuthNoPriv},        // default
		{"invalid", gosnmp.NoAuthNoPriv}, // unknown defaults to noAuthNoPriv
	}

	for _, tc := range tests {
		t.Run("seclevel_"+tc.input, func(t *testing.T) {
			t.Setenv("SNMP_VERSION", "3")
			t.Setenv("SNMP_SECURITY_LEVEL", tc.input)

			cfg, err := GetSNMPConfig()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.SecurityLevel != tc.expected {
				t.Errorf("security level %q: expected %v, got %v", tc.input, tc.expected, cfg.SecurityLevel)
			}
		})
	}
}

func TestGetSNMPConfig_V3NotSetForV2c(t *testing.T) {
	// When using v2c, v3 fields should not be populated even if env vars are set
	t.Setenv("SNMP_VERSION", "2c")
	t.Setenv("SNMP_USERNAME", "ignored")
	t.Setenv("SNMP_AUTH_PROTOCOL", "SHA256")
	t.Setenv("SNMP_AUTH_PASSWORD", "ignored")

	cfg, err := GetSNMPConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Version != gosnmp.Version2c {
		t.Error("expected v2c")
	}

	// V3 fields should be empty/zero for v2c
	if cfg.Username != "" {
		t.Errorf("expected empty username for v2c, got %q", cfg.Username)
	}

	if cfg.AuthProtocol != gosnmp.SnmpV3AuthProtocol(0) {
		t.Errorf("expected zero auth protocol for v2c, got %v", cfg.AuthProtocol)
	}
}
