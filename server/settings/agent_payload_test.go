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
	cfg.Web.CustomCertPath = "/etc/custom.crt"
	cfg.Web.CustomKeyPath = "/etc/custom.key"
	cfg.Web.EnableHTTP = false
	cfg.Web.HTTPPort = "9000"
	cfg.Web.HTTPSPort = "9443"
	cfg.Logging.Level = "debug"
	cfg.Logging.DumpParseDebug = true
	store.global = &storage.SettingsRecord{
		SchemaVersion: "v9",
		Settings:      cfg,
		UpdatedAt:     time.Unix(100, 0),
	}

	resolver, err := NewResolver(store)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	snapshot, err := BuildAgentSnapshot(context.Background(), resolver, "", "")
	if err != nil {
		t.Fatalf("build snapshot failed: %v", err)
	}
	defaults := pmsettings.DefaultSettings()
	if snapshot.Settings.Web.CustomCertPath != defaults.Web.CustomCertPath {
		t.Fatalf("expected custom cert path stripped, got %s", snapshot.Settings.Web.CustomCertPath)
	}
	if snapshot.Settings.Web.CustomKeyPath != defaults.Web.CustomKeyPath {
		t.Fatalf("expected custom key path stripped, got %s", snapshot.Settings.Web.CustomKeyPath)
	}
	if snapshot.Settings.Web.EnableHTTP != defaults.Web.EnableHTTP {
		t.Fatalf("expected enable_http reset to %v, got %v", defaults.Web.EnableHTTP, snapshot.Settings.Web.EnableHTTP)
	}
	if snapshot.Settings.Logging.Level != defaults.Logging.Level {
		t.Fatalf("expected logging level reset to %s, got %s", defaults.Logging.Level, snapshot.Settings.Logging.Level)
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

	resolver, err := NewResolver(store)
	if err != nil {
		t.Fatalf("NewResolver failed: %v", err)
	}
	snapshot, err := BuildAgentSnapshot(context.Background(), resolver, "tenant-1", "")
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
