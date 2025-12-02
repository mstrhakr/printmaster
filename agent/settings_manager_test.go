package main

import (
	"testing"
	"time"

	agentpkg "printmaster/agent/agent"
	pmsettings "printmaster/common/settings"
)

func TestSettingsManagerReloadsPersistedSnapshot(t *testing.T) {
	store := newFakeConfigStore()
	seeded := serverManagedSettings{
		Version:       "ver-1",
		SchemaVersion: "schema-1",
		UpdatedAt:     time.Unix(100, 0),
		Settings:      pmsettings.DefaultSettings(),
	}
	seeded.Settings.SNMP.TimeoutMS = 4242
	if err := store.SetConfigValue(serverManagedSettingsKey, seeded); err != nil {
		t.Fatalf("failed to seed store: %v", err)
	}

	mgr := NewSettingsManager(store)
	prev := settingsManager
	settingsManager = mgr
	t.Cleanup(func() { settingsManager = prev })

	if !mgr.HasManagedSnapshot() {
		t.Fatalf("expected manager to load persisted snapshot")
	}
	if mgr.CurrentVersion() != "ver-1" {
		t.Fatalf("unexpected version: %s", mgr.CurrentVersion())
	}
	base, managed := mgr.baseSettings()
	if !managed {
		t.Fatalf("expected managed flag to be true")
	}
	if base.SNMP.TimeoutMS != 4242 {
		t.Fatalf("expected loaded settings to reflect persisted snapshot")
	}
}

func TestSettingsManagerApplyServerSnapshotPersistsAndReturnsUnified(t *testing.T) {
	store := newFakeConfigStore()
	mgr := NewSettingsManager(store)
	prev := settingsManager
	settingsManager = mgr
	t.Cleanup(func() { settingsManager = prev })

	snap := &agentpkg.SettingsSnapshot{
		Version:       "hash-123",
		SchemaVersion: "schema-1",
		UpdatedAt:     time.Unix(200, 0),
		Settings:      pmsettings.DefaultSettings(),
	}
	snap.Settings.SNMP.TimeoutMS = 9001

	result, err := mgr.ApplyServerSnapshot(snap)
	if err != nil {
		t.Fatalf("apply snapshot failed: %v", err)
	}
	if mgr.CurrentVersion() != "hash-123" {
		t.Fatalf("manager version not updated")
	}
	if store.setCount(serverManagedSettingsKey) != 1 {
		t.Fatalf("expected snapshot persisted once, got %d", store.setCount(serverManagedSettingsKey))
	}
	if result.SNMP.TimeoutMS != 9001 {
		t.Fatalf("expected unified settings to include server-managed values")
	}
}
