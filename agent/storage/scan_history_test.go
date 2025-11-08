package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSQLiteStore_AddScanHistory(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a device first
	device := newFullTestDevice("TEST123", "10.0.0.1", "HP", "LaserJet", false, true)
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("failed to create device: %v", err)
	}

	// Add scan history
	scan := &ScanSnapshot{
		Serial:          "TEST123",
		CreatedAt:       time.Now(),
		IP:              "10.0.0.1",
		Hostname:        "test-printer",
		Firmware:        "1.2.3",
		Consumables:     []string{"Toner Cartridge"},
		StatusMessages:  []string{"Ready"},
		DiscoveryMethod: "snmp",
		WalkFilename:    "mib_walk_test.json",
	}

	err = store.AddScanHistory(ctx, scan)
	if err != nil {
		t.Fatalf("failed to add scan history: %v", err)
	}

	// Verify scan was added
	scans, err := store.GetScanHistory(ctx, "TEST123", 10)
	if err != nil {
		t.Fatalf("failed to get scan history: %v", err)
	}

	if len(scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(scans))
	}

	if scans[0].IP != "10.0.0.1" {
		t.Errorf("expected IP 10.0.0.1, got %s", scans[0].IP)
	}
	if scans[0].Hostname != "test-printer" {
		t.Errorf("expected Hostname test-printer, got %s", scans[0].Hostname)
	}
	if scans[0].Firmware != "1.2.3" {
		t.Errorf("expected Firmware 1.2.3, got %s", scans[0].Firmware)
	}
}

func TestSQLiteStore_GetScanHistory(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create device
	device := newTestDevice("TEST123", "10.0.0.1", false, true)
	store.Create(ctx, device)

	// Add multiple scans with different firmware versions to test ordering
	for i := 0; i < 5; i++ {
		scan := &ScanSnapshot{
			Serial:      "TEST123",
			CreatedAt:   time.Now().Add(time.Duration(i) * time.Hour),
			IP:          "10.0.0.1",
			Hostname:    "test-printer",
			Firmware:    fmt.Sprintf("1.0.%d", i), // Different firmware versions
			Consumables: []string{fmt.Sprintf("Toner %d", i)},
		}
		store.AddScanHistory(ctx, scan)
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Get all scans
	scans, err := store.GetScanHistory(ctx, "TEST123", 10)
	if err != nil {
		t.Fatalf("failed to get scan history: %v", err)
	}

	if len(scans) != 5 {
		t.Fatalf("expected 5 scans, got %d", len(scans))
	}

	// Verify newest first (descending order by checking CreatedAt)
	if !scans[0].CreatedAt.After(scans[1].CreatedAt) {
		t.Errorf("scans not in descending order")
	}

	// Test limit
	limited, err := store.GetScanHistory(ctx, "TEST123", 3)
	if err != nil {
		t.Fatalf("failed to get limited scan history: %v", err)
	}

	if len(limited) != 3 {
		t.Errorf("expected 3 scans with limit, got %d", len(limited))
	}
}

func TestSQLiteStore_DeleteOldScans(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create device
	device := newTestDevice("TEST123", "10.0.0.1", false, true)
	store.Create(ctx, device)

	// Add old and new scans
	oldTime := time.Now().Add(-40 * 24 * time.Hour) // 40 days ago
	newTime := time.Now().Add(-10 * 24 * time.Hour) // 10 days ago

	oldScan := &ScanSnapshot{
		Serial:    "TEST123",
		CreatedAt: oldTime,
		IP:        "10.0.0.1",
		Firmware:  "1.0.0",
	}
	store.AddScanHistory(ctx, oldScan)

	newScan := &ScanSnapshot{
		Serial:    "TEST123",
		CreatedAt: newTime,
		IP:        "10.0.0.1",
		Firmware:  "2.0.0",
	}
	store.AddScanHistory(ctx, newScan)

	// Delete scans older than 30 days
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	count, err := store.DeleteOldScans(ctx, cutoff.Unix())
	if err != nil {
		t.Fatalf("failed to delete old scans: %v", err)
	}

	if count != 1 {
		t.Errorf("expected to delete 1 scan, deleted %d", count)
	}

	// Verify only new scan remains
	scans, _ := store.GetScanHistory(ctx, "TEST123", 10)
	if len(scans) != 1 {
		t.Errorf("expected 1 remaining scan, got %d", len(scans))
	}
	if scans[0].Firmware != "2.0.0" {
		t.Errorf("wrong scan remained, Firmware %s", scans[0].Firmware)
	}
}

func TestSQLiteStore_HideDiscovered(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create discovered and saved devices
	discovered := newTestDevice("DISC123", "10.0.0.1", false, true)
	store.Create(ctx, discovered)

	saved := newTestDevice("SAVED123", "10.0.0.2", true, true)
	store.Create(ctx, saved)

	// Hide discovered devices
	count, err := store.HideDiscovered(ctx)
	if err != nil {
		t.Fatalf("failed to hide discovered: %v", err)
	}

	if count != 1 {
		t.Errorf("expected to hide 1 device, hid %d", count)
	}

	// Verify discovered is hidden
	disc, _ := store.Get(ctx, "DISC123")
	if disc.Visible {
		t.Error("discovered device should be hidden")
	}

	// Verify saved is still visible
	sav, _ := store.Get(ctx, "SAVED123")
	if !sav.Visible {
		t.Error("saved device should still be visible")
	}
}

func TestSQLiteStore_ShowAll(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create hidden devices
	device1 := newTestDevice("TEST1", "10.0.0.1", false, false)
	store.Create(ctx, device1)

	device2 := newTestDevice("TEST2", "10.0.0.2", false, false)
	store.Create(ctx, device2)

	// Show all
	count, err := store.ShowAll(ctx)
	if err != nil {
		t.Fatalf("failed to show all: %v", err)
	}

	if count != 2 {
		t.Errorf("expected to show 2 devices, showed %d", count)
	}

	// Verify both visible
	d1, _ := store.Get(ctx, "TEST1")
	d2, _ := store.Get(ctx, "TEST2")

	if !d1.Visible || !d2.Visible {
		t.Error("devices should be visible")
	}
}

func TestSQLiteStore_DeleteOldHiddenDevices(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create old hidden device
	oldDevice := newTestDevice("OLD123", "10.0.0.1", false, false)
	oldDevice.LastSeen = time.Now().Add(-40 * 24 * time.Hour)
	store.Create(ctx, oldDevice)

	// Create recent hidden device
	recentDevice := newTestDevice("RECENT123", "10.0.0.2", false, false)
	recentDevice.LastSeen = time.Now().Add(-10 * 24 * time.Hour)
	store.Create(ctx, recentDevice)

	// Create visible device (should not be deleted)
	visibleDevice := newTestDevice("VISIBLE123", "10.0.0.3", false, true)
	visibleDevice.LastSeen = time.Now().Add(-40 * 24 * time.Hour)
	store.Create(ctx, visibleDevice)

	// Delete hidden devices older than 30 days
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	count, err := store.DeleteOldHiddenDevices(ctx, cutoff.Unix())
	if err != nil {
		t.Fatalf("failed to delete old hidden devices: %v", err)
	}

	if count != 1 {
		t.Errorf("expected to delete 1 device, deleted %d", count)
	}

	// Verify old hidden device is gone
	_, err = store.Get(ctx, "OLD123")
	if err != ErrNotFound {
		t.Error("old hidden device should be deleted")
	}

	// Verify recent hidden device remains
	_, err = store.Get(ctx, "RECENT123")
	if err != nil {
		t.Error("recent hidden device should remain")
	}

	// Verify visible device remains (even though old)
	_, err = store.Get(ctx, "VISIBLE123")
	if err != nil {
		t.Error("visible device should remain regardless of age")
	}
}

func TestSQLiteStore_VisibleFilter(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create mix of visible and hidden devices
	visible1 := newTestDevice("VIS1", "10.0.0.1", false, true)
	visible2 := newTestDevice("VIS2", "10.0.0.2", true, true)
	hidden1 := newTestDevice("HID1", "10.0.0.3", false, false)
	hidden2 := newTestDevice("HID2", "10.0.0.4", true, false)

	store.Create(ctx, visible1)
	store.Create(ctx, visible2)
	store.Create(ctx, hidden1)
	store.Create(ctx, hidden2)

	// Test visible filter
	visibleTrue := true
	devices, err := store.List(ctx, DeviceFilter{Visible: &visibleTrue})
	if err != nil {
		t.Fatalf("failed to list visible devices: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("expected 2 visible devices, got %d", len(devices))
	}

	// Test hidden filter
	visibleFalse := false
	devices, err = store.List(ctx, DeviceFilter{Visible: &visibleFalse})
	if err != nil {
		t.Fatalf("failed to list hidden devices: %v", err)
	}
	if len(devices) != 2 {
		t.Errorf("expected 2 hidden devices, got %d", len(devices))
	}

	// Test combined filters (visible + discovered)
	isSavedFalse := false
	devices, err = store.List(ctx, DeviceFilter{Visible: &visibleTrue, IsSaved: &isSavedFalse})
	if err != nil {
		t.Fatalf("failed to list visible discovered devices: %v", err)
	}
	if len(devices) != 1 {
		t.Errorf("expected 1 visible discovered device, got %d", len(devices))
	}
	if devices[0].Serial != "VIS1" {
		t.Errorf("expected VIS1, got %s", devices[0].Serial)
	}
}

func TestSQLiteStore_Stats_WithScanHistory(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create devices
	saved := newTestDevice("SAVED1", "10.0.0.1", true, true)
	discovered := newTestDevice("DISC1", "10.0.0.2", false, true)
	hidden := newTestDevice("HID1", "10.0.0.3", false, false)

	store.Create(ctx, saved)
	store.Create(ctx, discovered)
	store.Create(ctx, hidden)

	// Add scan history
	scan1 := &ScanSnapshot{Serial: "SAVED1", CreatedAt: time.Now(), IP: "10.0.0.1"}
	scan2 := &ScanSnapshot{Serial: "DISC1", CreatedAt: time.Now(), IP: "10.0.0.2"}
	scan3 := &ScanSnapshot{Serial: "DISC1", CreatedAt: time.Now(), IP: "10.0.0.2"}

	store.AddScanHistory(ctx, scan1)
	store.AddScanHistory(ctx, scan2)
	store.AddScanHistory(ctx, scan3)

	// Get stats
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats["total_devices"] != 3 {
		t.Errorf("expected 3 total devices, got %v", stats["total_devices"])
	}
	if stats["saved_devices"] != 1 {
		t.Errorf("expected 1 saved device, got %v", stats["saved_devices"])
	}
	if stats["discovered_devices"] != 2 {
		t.Errorf("expected 2 discovered devices, got %v", stats["discovered_devices"])
	}
	if stats["visible_devices"] != 2 {
		t.Errorf("expected 2 visible devices, got %v", stats["visible_devices"])
	}
	if stats["hidden_devices"] != 1 {
		t.Errorf("expected 1 hidden device, got %v", stats["hidden_devices"])
	}
	if stats["total_scans"] != 3 {
		t.Errorf("expected 3 total scans, got %v", stats["total_scans"])
	}
}
