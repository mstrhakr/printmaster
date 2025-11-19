package main

import (
	"testing"
	"time"

	agentpkg "printmaster/agent/agent"
	pmsettings "printmaster/common/settings"
)

func TestLoadUnifiedSettingsRetainsDiscoveryToggles(t *testing.T) {
	store := newFakeConfigStore()
	store.values["discovery_settings"] = map[string]interface{}{
		"auto_discover_enabled":          true,
		"autosave_discovered_devices":    true,
		"show_discover_button_anyway":    true,
		"show_discovered_devices_anyway": true,
		"passive_discovery_enabled":      true,
	}

	snapshot := loadUnifiedSettings(store)
	discMap := structToMap(snapshot.Discovery)

	expectations := map[string]bool{
		"auto_discover_enabled":          true,
		"autosave_discovered_devices":    true,
		"show_discover_button_anyway":    true,
		"show_discovered_devices_anyway": true,
		"passive_discovery_enabled":      true,
	}

	for key, want := range expectations {
		got, ok := discMap[key]
		if !ok {
			t.Fatalf("expected discovery key %s to be present", key)
		}
		gotBool, _ := got.(bool)
		if gotBool != want {
			t.Fatalf("unexpected value for %s: got %v want %v", key, gotBool, want)
		}
	}
}

func TestLoadUnifiedSettingsUsesManagedSnapshotButAllowsLocalDeveloperOverrides(t *testing.T) {
	store := newFakeConfigStore()
	mgr := NewSettingsManager(store)
	prev := settingsManager
	settingsManager = mgr
	t.Cleanup(func() { settingsManager = prev })

	snap := &agentpkg.SettingsSnapshot{
		Version:       "abc",
		SchemaVersion: "schema-1",
		UpdatedAt:     time.Unix(300, 0),
		Settings:      pmsettings.DefaultSettings(),
	}
	snap.Settings.Developer.SNMPTimeoutMS = 3333
	snap.Settings.Developer.LogLevel = "warn"

	if _, err := mgr.ApplyServerSnapshot(snap); err != nil {
		t.Fatalf("apply snapshot failed: %v", err)
	}

	store.values["settings"] = map[string]interface{}{
		"developer": map[string]interface{}{
			"log_level":       "debug",
			"snmp_timeout_ms": 1234,
		},
	}

	result := loadUnifiedSettings(store)
	if result.Developer.SNMPTimeoutMS != 3333 {
		t.Fatalf("expected server-managed value to persist, got %d", result.Developer.SNMPTimeoutMS)
	}
	if result.Developer.LogLevel != "debug" {
		t.Fatalf("expected local developer overrides to apply, got %s", result.Developer.LogLevel)
	}
}
