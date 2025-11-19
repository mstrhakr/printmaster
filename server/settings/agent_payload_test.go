package settings

import (
	"context"
	"testing"
	"time"

	pmsettings "printmaster/common/settings"
	"printmaster/server/storage"
)

func TestBuildAgentSnapshotStripsAgentLocalFields(t *testing.T) {
	store := newFakeStore()
	cfg := pmsettings.DefaultSettings()
	cfg.Security.CustomCertPath = "/etc/custom.crt"
	cfg.Security.CustomKeyPath = "/etc/custom.key"
	cfg.Security.EnableHTTP = false
	cfg.Security.HTTPPort = "9000"
	cfg.Security.HTTPSPort = "9443"
	cfg.Developer.LogLevel = "debug"
	cfg.Developer.DumpParseDebug = true
	cfg.Developer.ShowLegacy = true
	store.global = &storage.SettingsRecord{
		SchemaVersion: "v9",
		Settings:      cfg,
		UpdatedAt:     time.Unix(100, 0),
	}

	resolver := NewResolver(store)
	snapshot, err := BuildAgentSnapshot(context.Background(), resolver, "")
	if err != nil {
		t.Fatalf("build snapshot failed: %v", err)
	}
	defaults := pmsettings.DefaultSettings()
	if snapshot.Settings.Security.CustomCertPath != defaults.Security.CustomCertPath {
		t.Fatalf("expected custom cert path stripped, got %s", snapshot.Settings.Security.CustomCertPath)
	}
	if snapshot.Settings.Security.CustomKeyPath != defaults.Security.CustomKeyPath {
		t.Fatalf("expected custom key path stripped, got %s", snapshot.Settings.Security.CustomKeyPath)
	}
	if snapshot.Settings.Security.EnableHTTP != defaults.Security.EnableHTTP {
		t.Fatalf("expected enable_http reset to %v, got %v", defaults.Security.EnableHTTP, snapshot.Settings.Security.EnableHTTP)
	}
	if snapshot.Settings.Developer.LogLevel != defaults.Developer.LogLevel {
		t.Fatalf("expected developer log level reset to %s, got %s", defaults.Developer.LogLevel, snapshot.Settings.Developer.LogLevel)
	}
	if snapshot.Version == "" {
		t.Fatalf("expected settings version to be set")
	}
	wantVersion, err := pmsettings.ComputeSettingsVersion(snapshot.SchemaVersion, snapshot.UpdatedAt, snapshot.Settings)
	if err != nil {
		t.Fatalf("compute expected version failed: %v", err)
	}
	if snapshot.Version != wantVersion {
		t.Fatalf("unexpected version: got %s want %s", snapshot.Version, wantVersion)
	}
}

func TestBuildAgentSnapshotResolvesTenantOverrides(t *testing.T) {
	store := newFakeStore()
	defaults := pmsettings.DefaultSettings()
	defaults.Discovery.SNMPEnabled = true
	store.global = &storage.SettingsRecord{SchemaVersion: "global", Settings: defaults, UpdatedAt: time.Unix(50, 0)}
	store.tenantRecords["tenant-1"] = &storage.TenantSettingsRecord{
		TenantID:      "tenant-1",
		SchemaVersion: "tenant-v2",
		Overrides: map[string]interface{}{
			"discovery": map[string]interface{}{"snmp_enabled": false},
		},
		UpdatedAt: time.Unix(200, 0),
	}

	resolver := NewResolver(store)
	snapshot, err := BuildAgentSnapshot(context.Background(), resolver, "tenant-1")
	if err != nil {
		t.Fatalf("build snapshot failed: %v", err)
	}
	if snapshot.SchemaVersion != "tenant-v2" {
		t.Fatalf("expected tenant schema version, got %s", snapshot.SchemaVersion)
	}
	if snapshot.Settings.Discovery.SNMPEnabled {
		t.Fatalf("expected tenant override to disable SNMP discovery")
	}
	if snapshot.UpdatedAt.IsZero() {
		t.Fatalf("expected updated_at from tenant overrides")
	}
	if snapshot.Version == "" {
		t.Fatalf("expected settings version to be set")
	}
}
