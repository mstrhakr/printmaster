package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStore_InMemory(t *testing.T) {
	// Test in-memory database
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a test device
	device := newFullTestDevice("TEST001", "192.168.1.100", "HP", "LaserJet Pro", false, true)
	device.Consumables = []string{"Black Toner"}

	// Test Create
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create a metrics snapshot for this device
	snapshot := newTestMetrics("TEST001", 500)
	snapshot.TonerLevels = map[string]interface{}{"black": 75}
	err = store.SaveMetricsSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to save metrics snapshot: %v", err)
	}

	// Test Get
	retrieved, err := store.Get(ctx, "TEST001")
	if err != nil {
		t.Fatalf("Failed to get device: %v", err)
	}
	if retrieved.Serial != device.Serial {
		t.Errorf("Expected serial %s, got %s", device.Serial, retrieved.Serial)
	}

	// Test metrics retrieval
	metrics, err := store.GetLatestMetrics(ctx, "TEST001")
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	blackLevel, ok := metrics.TonerLevels["black"].(int)
	if !ok {
		// Try float64 (JSON unmarshaling might use this)
		if f, ok := metrics.TonerLevels["black"].(float64); ok {
			blackLevel = int(f)
		}
	}
	if blackLevel != 75 {
		t.Errorf("Expected toner level 75, got %d", blackLevel)
	}

	// Test duplicate create should fail
	err = store.Create(ctx, device)
	if err != ErrDuplicate {
		t.Errorf("Expected ErrDuplicate, got %v", err)
	}

	// Test Update
	device.IsSaved = true
	err = store.Update(ctx, device)
	if err != nil {
		t.Fatalf("Failed to update device: %v", err)
	}

	// Update metrics
	snapshot2 := newTestMetrics("TEST001", 1000)
	snapshot2.TonerLevels = map[string]interface{}{"black": 70}
	err = store.SaveMetricsSnapshot(ctx, snapshot2)
	if err != nil {
		t.Fatalf("Failed to save updated metrics: %v", err)
	}

	updated, err := store.Get(ctx, "TEST001")
	if err != nil {
		t.Fatalf("Failed to get updated device: %v", err)
	}
	if !updated.IsSaved {
		t.Error("Expected IsSaved to be true")
	}

	// Verify updated metrics
	updatedMetrics, err := store.GetLatestMetrics(ctx, "TEST001")
	if err != nil {
		t.Fatalf("Failed to get updated metrics: %v", err)
	}
	if updatedMetrics.PageCount != 1000 {
		t.Errorf("Expected page count 1000, got %d", updatedMetrics.PageCount)
	}

	// Test Delete
	err = store.Delete(ctx, "TEST001")
	if err != nil {
		t.Fatalf("Failed to delete device: %v", err)
	}

	_, err = store.Get(ctx, "TEST001")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound after delete, got %v", err)
	}
}

func TestSQLiteStore_StoreDiscoveryAtomic_RollsBackOnError(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("ROLLBACK001", "192.168.1.250", "HP", "LaserJet", false, true)
	device.Visible = true

	scan := &ScanSnapshot{
		Serial:          device.Serial,
		CreatedAt:       time.Now(),
		IP:              device.IP,
		Hostname:        device.Hostname,
		Firmware:        device.Firmware,
		DiscoveryMethod: "test",
	}

	metrics := newTestMetrics(device.Serial, 123)
	metrics.Serial = "" // force SaveMetricsSnapshot to fail

	err = store.StoreDiscoveryAtomic(ctx, device, scan, metrics)
	if err == nil {
		t.Fatalf("Expected error, got nil")
	}

	// No device row should exist
	if _, err := store.Get(ctx, device.Serial); err != ErrNotFound {
		t.Fatalf("Expected ErrNotFound after rollback, got %v", err)
	}

	// No scan history rows should exist
	history, err := store.GetScanHistory(ctx, device.Serial, 10)
	if err != nil {
		t.Fatalf("GetScanHistory returned error: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("Expected no scan history after rollback, got %d", len(history))
	}

	// No metrics rows should exist
	if _, err := store.GetLatestMetrics(ctx, device.Serial); err != ErrNotFound {
		t.Fatalf("Expected no metrics after rollback (ErrNotFound), got %v", err)
	}
}

func TestSQLiteStore_Upsert(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("TEST002", "192.168.1.101", "Canon", "ImageRunner", false, true)

	// First upsert should create
	err = store.Upsert(ctx, device)
	if err != nil {
		t.Fatalf("Failed to upsert (create): %v", err)
	}

	// Save initial metrics
	snapshot1 := newTestMetrics("TEST002", 500)
	err = store.SaveMetricsSnapshot(ctx, snapshot1)
	if err != nil {
		t.Fatalf("Failed to save initial metrics: %v", err)
	}

	createdAt := device.CreatedAt

	// Second upsert should update
	time.Sleep(10 * time.Millisecond) // Ensure time difference
	err = store.Upsert(ctx, device)
	if err != nil {
		t.Fatalf("Failed to upsert (update): %v", err)
	}

	// Update metrics
	snapshot2 := newTestMetrics("TEST002", 1500)
	err = store.SaveMetricsSnapshot(ctx, snapshot2)
	if err != nil {
		t.Fatalf("Failed to save updated metrics: %v", err)
	}

	retrieved, err := store.Get(ctx, "TEST002")
	if err != nil {
		t.Fatalf("Failed to get device: %v", err)
	}

	// Verify updated metrics
	metrics, err := store.GetLatestMetrics(ctx, "TEST002")
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	if metrics.PageCount != 1500 {
		t.Errorf("Expected page count 1500, got %d", metrics.PageCount)
	}

	// created_at should be preserved
	if !retrieved.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt was modified during upsert")
	}
}

func TestSQLiteStore_List(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create multiple devices
	devices := []*Device{
		newFullTestDevice("HP001", "192.168.1.10", "HP", "LaserJet 1", true, true),
		newFullTestDevice("HP002", "192.168.1.11", "HP", "LaserJet 2", false, true),
		newFullTestDevice("CANON001", "192.168.1.20", "Canon", "ImageRunner", true, true),
		newFullTestDevice("EPSON001", "192.168.1.30", "Epson", "WorkForce", false, true),
	}

	for _, d := range devices {
		err := store.Create(ctx, d)
		if err != nil {
			t.Fatalf("Failed to create device %s: %v", d.Serial, err)
		}
	}

	// Test list all
	all, err := store.List(ctx, DeviceFilter{})
	if err != nil {
		t.Fatalf("Failed to list all devices: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("Expected 4 devices, got %d", len(all))
	}

	// Test list saved only
	saved := true
	savedDevices, err := store.List(ctx, DeviceFilter{IsSaved: &saved})
	if err != nil {
		t.Fatalf("Failed to list saved devices: %v", err)
	}
	if len(savedDevices) != 2 {
		t.Errorf("Expected 2 saved devices, got %d", len(savedDevices))
	}

	// Test list discovered only
	discovered := false
	discoveredDevices, err := store.List(ctx, DeviceFilter{IsSaved: &discovered})
	if err != nil {
		t.Fatalf("Failed to list discovered devices: %v", err)
	}
	if len(discoveredDevices) != 2 {
		t.Errorf("Expected 2 discovered devices, got %d", len(discoveredDevices))
	}

	// Test filter by manufacturer
	hpDevices, err := store.List(ctx, DeviceFilter{Manufacturer: "HP"})
	if err != nil {
		t.Fatalf("Failed to list HP devices: %v", err)
	}
	if len(hpDevices) != 2 {
		t.Errorf("Expected 2 HP devices, got %d", len(hpDevices))
	}

	// Test filter by IP
	ipDevices, err := store.List(ctx, DeviceFilter{IP: "192.168.1.10"})
	if err != nil {
		t.Fatalf("Failed to list devices by IP: %v", err)
	}
	if len(ipDevices) != 1 {
		t.Errorf("Expected 1 device with IP, got %d", len(ipDevices))
	}

	// Test limit
	limited, err := store.List(ctx, DeviceFilter{Limit: 2})
	if err != nil {
		t.Fatalf("Failed to list with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("Expected 2 devices with limit, got %d", len(limited))
	}
}

func TestSQLiteStore_MarkSavedDiscovered(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newTestDevice("TEST003", "192.168.1.102", false, true)

	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Mark as saved
	err = store.MarkSaved(ctx, "TEST003")
	if err != nil {
		t.Fatalf("Failed to mark as saved: %v", err)
	}

	retrieved, _ := store.Get(ctx, "TEST003")
	if !retrieved.IsSaved {
		t.Error("Expected IsSaved to be true")
	}

	// Mark as discovered
	err = store.MarkDiscovered(ctx, "TEST003")
	if err != nil {
		t.Fatalf("Failed to mark as discovered: %v", err)
	}

	retrieved, _ = store.Get(ctx, "TEST003")
	if retrieved.IsSaved {
		t.Error("Expected IsSaved to be false")
	}

	// Test non-existent device
	err = store.MarkSaved(ctx, "NONEXISTENT")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestSQLiteStore_DeleteAll(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create test devices
	devices := []*Device{
		newFullTestDevice("DEL001", "192.168.1.40", "HP", "", true, true),
		newFullTestDevice("DEL002", "192.168.1.41", "HP", "", false, true),
		newFullTestDevice("DEL003", "192.168.1.42", "Canon", "", true, true),
	}

	for _, d := range devices {
		store.Create(ctx, d)
	}

	// Delete all HP devices
	deleted, err := store.DeleteAll(ctx, DeviceFilter{Manufacturer: "HP"})
	if err != nil {
		t.Fatalf("Failed to delete HP devices: %v", err)
	}
	if deleted != 2 {
		t.Errorf("Expected 2 devices deleted, got %d", deleted)
	}

	// Verify only Canon device remains
	remaining, _ := store.List(ctx, DeviceFilter{})
	if len(remaining) != 1 {
		t.Errorf("Expected 1 device remaining, got %d", len(remaining))
	}
	if remaining[0].Manufacturer != "Canon" {
		t.Errorf("Expected Canon device, got %s", remaining[0].Manufacturer)
	}

	// Delete all saved devices
	saved := true
	deleted, err = store.DeleteAll(ctx, DeviceFilter{IsSaved: &saved})
	if err != nil {
		t.Fatalf("Failed to delete saved devices: %v", err)
	}
	if deleted != 1 {
		t.Errorf("Expected 1 device deleted, got %d", deleted)
	}

	// Verify no devices remain
	remaining, _ = store.List(ctx, DeviceFilter{})
	if len(remaining) != 0 {
		t.Errorf("Expected 0 devices remaining, got %d", len(remaining))
	}
}

func TestSQLiteStore_Stats(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create test devices
	store.Create(ctx, newTestDevice("STAT001", "192.168.1.50", true, true))
	store.Create(ctx, newTestDevice("STAT002", "192.168.1.51", true, true))
	store.Create(ctx, newTestDevice("STAT003", "192.168.1.52", false, true))

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if stats["total_devices"] != 3 {
		t.Errorf("Expected 3 total devices, got %v", stats["total_devices"])
	}
	if stats["saved_devices"] != 2 {
		t.Errorf("Expected 2 saved devices, got %v", stats["saved_devices"])
	}
	if stats["discovered_devices"] != 1 {
		t.Errorf("Expected 1 discovered device, got %v", stats["discovered_devices"])
	}
}

func TestSQLiteStore_ComplexData(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("COMPLEX001", "192.168.1.100", "HP", "LaserJet M1536dnf", true, true)
	device.Hostname = "printer-01"
	device.Firmware = "v2.0.1"
	device.MACAddress = "c4:34:6b:1a:50:5c"
	device.SubnetMask = "255.255.0.0"
	device.Gateway = "192.168.1.1"
	device.DNSServers = []string{"8.8.8.8", "8.8.4.4"}
	device.DHCPServer = "192.168.1.1"
	device.Consumables = []string{"Black Toner CE278A", "Cyan Toner", "Magenta Toner", "Yellow Toner"}
	device.StatusMessages = []string{"Ready", "Toner Low"}
	device.DiscoveryMethod = "snmp"
	device.WalkFilename = "mib_walk_192_168_1_100_20251101.json"
	device.RawData = map[string]interface{}{
		"uptime_seconds": 1234567,
		"duplex":         true,
		"color":          true,
	}

	// Create
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create complex device: %v", err)
	}

	// Save metrics separately
	snapshot := newTestMetrics("COMPLEX001", 50000)
	snapshot.TonerLevels = map[string]interface{}{
		"black":   85,
		"cyan":    60,
		"magenta": 70,
		"yellow":  55,
	}
	err = store.SaveMetricsSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to save metrics: %v", err)
	}

	// Retrieve and verify all fields
	retrieved, err := store.Get(ctx, "COMPLEX001")
	if err != nil {
		t.Fatalf("Failed to get complex device: %v", err)
	}

	if len(retrieved.DNSServers) != 2 {
		t.Errorf("Expected 2 DNS servers, got %d", len(retrieved.DNSServers))
	}
	if len(retrieved.Consumables) != 4 {
		t.Errorf("Expected 4 consumables, got %d", len(retrieved.Consumables))
	}
	if len(retrieved.StatusMessages) != 2 {
		t.Errorf("Expected 2 status messages, got %d", len(retrieved.StatusMessages))
	}

	// Verify metrics
	metrics, err := store.GetLatestMetrics(ctx, "COMPLEX001")
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	if len(metrics.TonerLevels) != 4 {
		t.Errorf("Expected 4 toner levels, got %d", len(metrics.TonerLevels))
	}
	if retrieved.RawData["duplex"] != true {
		t.Error("Expected duplex to be true in raw data")
	}
}

func TestSQLiteStore_ErrorCases(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Test operations with empty serial
	emptyDevice := newTestDevice("", "192.168.1.1", false, true)
	err = store.Create(ctx, emptyDevice)
	if err != ErrInvalidSerial {
		t.Errorf("Expected ErrInvalidSerial for empty serial, got %v", err)
	}

	err = store.Update(ctx, emptyDevice)
	if err != ErrInvalidSerial {
		t.Errorf("Expected ErrInvalidSerial for update with empty serial, got %v", err)
	}

	_, err = store.Get(ctx, "")
	if err != ErrInvalidSerial {
		t.Errorf("Expected ErrInvalidSerial for get with empty serial, got %v", err)
	}

	err = store.Delete(ctx, "")
	if err != ErrInvalidSerial {
		t.Errorf("Expected ErrInvalidSerial for delete with empty serial, got %v", err)
	}

	// Test get non-existent device
	_, err = store.Get(ctx, "NONEXISTENT")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	// Test update non-existent device
	nonExistentDevice := newTestDevice("NONEXISTENT", "192.168.1.1", false, true)
	err = store.Update(ctx, nonExistentDevice)
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound for update, got %v", err)
	}

	// Test delete non-existent device
	err = store.Delete(ctx, "NONEXISTENT")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound for delete, got %v", err)
	}
}

func TestSQLiteStore_BackupAndReset_InMemory(t *testing.T) {
	// Test BackupAndReset with in-memory database
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a test device
	device := newFullTestDevice("BACKUP001", "192.168.1.200", "Canon", "imageRUNNER", true, true)

	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Verify device exists
	_, err = store.Get(ctx, "BACKUP001")
	if err != nil {
		t.Fatalf("Failed to get device before reset: %v", err)
	}

	// Perform BackupAndReset (should work with sqlite driver)
	err = store.BackupAndReset()
	if err != nil {
		t.Fatalf("BackupAndReset failed: %v", err)
	}

	// Verify device is gone after reset
	_, err = store.Get(ctx, "BACKUP001")
	if err != ErrNotFound {
		t.Errorf("Expected device to be gone after reset, but got: %v", err)
	}

	// Verify we can still use the database after reset
	newDevice := newFullTestDevice("RESET001", "192.168.1.201", "Brother", "MFC", false, true)

	err = store.Create(ctx, newDevice)
	if err != nil {
		t.Fatalf("Failed to create device after reset: %v", err)
	}

	// Verify new device exists
	retrieved, err := store.Get(ctx, "RESET001")
	if err != nil {
		t.Fatalf("Failed to get device after reset: %v", err)
	}
	if retrieved.Serial != "RESET001" {
		t.Errorf("Retrieved wrong device after reset: got %s, want RESET001", retrieved.Serial)
	}
}

// TestSQLiteStore_MonoOnlyFlipDetection tests that the storage layer correctly
// rejects metrics snapshots that exhibit the "mono-only flip" anomaly.
// This happens when SNMP intermittently fails to return the color page count OID,
// causing a color device to temporarily appear as mono-only.
//
// Pattern:
// - Previous snapshot: total=423995, color=200404, mono=223591 (valid color device)
// - Anomalous snapshot: total=423995, color=0, mono=423995 (mono jumped to match total!)
//
// Rule 1: Counts only go up - this includes going from non-zero to zero
func TestSQLiteStore_MetricsRule1_CountsOnlyGoUp(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("TEST001", "192.168.1.100", "HP", "LaserJet", true, true)
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Initial metrics
	snapshot1 := newTestMetrics("TEST001", 10000)
	snapshot1.Timestamp = time.Now().Add(-10 * time.Minute)
	snapshot1.ColorPages = 5000
	snapshot1.MonoPages = 5000
	err = store.SaveMetricsSnapshot(ctx, snapshot1)
	if err != nil {
		t.Fatalf("Failed to save initial: %v", err)
	}

	// Test case 1: page_count decreased significantly
	badSnapshot1 := newTestMetrics("TEST001", 8000) // Decreased by 2000 (20%)
	badSnapshot1.Timestamp = time.Now().Add(-8 * time.Minute)
	badSnapshot1.ColorPages = 4000
	badSnapshot1.MonoPages = 4000
	err = store.SaveMetricsSnapshot(ctx, badSnapshot1)
	if err != nil {
		t.Fatalf("SaveMetricsSnapshot returned error: %v", err)
	}

	latest, _ := store.GetLatestMetrics(ctx, "TEST001")
	if latest.PageCount != 10000 {
		t.Errorf("Rule 1 failed for page_count! decreased to %d (expected 10000)", latest.PageCount)
	}

	// Test case 2: color_pages went to zero (5000 -> 0 is a decrease!)
	badSnapshot2 := newTestMetrics("TEST001", 10000)
	badSnapshot2.Timestamp = time.Now().Add(-6 * time.Minute)
	badSnapshot2.ColorPages = 0    // Went to zero!
	badSnapshot2.MonoPages = 10000 // Jumped to match total
	err = store.SaveMetricsSnapshot(ctx, badSnapshot2)
	if err != nil {
		t.Fatalf("SaveMetricsSnapshot returned error: %v", err)
	}

	latest, _ = store.GetLatestMetrics(ctx, "TEST001")
	if latest.ColorPages != 5000 {
		t.Errorf("Rule 1 failed for color_pages! went to %d (expected 5000)", latest.ColorPages)
	}

	// Test case 3: legitimate update should work
	goodSnapshot := newTestMetrics("TEST001", 10100)
	goodSnapshot.Timestamp = time.Now()
	goodSnapshot.ColorPages = 5050
	goodSnapshot.MonoPages = 5050
	err = store.SaveMetricsSnapshot(ctx, goodSnapshot)
	if err != nil {
		t.Fatalf("Failed to save legitimate: %v", err)
	}

	latest, _ = store.GetLatestMetrics(ctx, "TEST001")
	if latest.ColorPages != 5050 {
		t.Errorf("Legitimate update was rejected! color_pages=%d (expected 5050)", latest.ColorPages)
	}
}

// TestSQLiteStore_MetricsRule2_PartsMustEqualWhole tests Rule 2: color + mono ≈ total
func TestSQLiteStore_MetricsRule2_PartsMustEqualWhole(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("TEST002", "192.168.1.101", "Canon", "MF", true, true)
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Try to save snapshot where parts don't match total (>10% off)
	badSnapshot := newTestMetrics("TEST002", 10000)
	badSnapshot.Timestamp = time.Now()
	badSnapshot.ColorPages = 3000 // 3000 + 3000 = 6000, but total is 10000 (40% off!)
	badSnapshot.MonoPages = 3000
	err = store.SaveMetricsSnapshot(ctx, badSnapshot)
	if err != nil {
		t.Fatalf("SaveMetricsSnapshot returned error: %v", err)
	}

	// Verify bad snapshot was dropped (no metrics saved)
	latest, err := store.GetLatestMetrics(ctx, "TEST002")
	if err != nil && !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Unexpected error: %v", err)
	}
	if latest != nil {
		t.Errorf("Rule 3 failed! Mismatched snapshot was saved (total=%d, color+mono=%d)",
			latest.PageCount, latest.ColorPages+latest.MonoPages)
	}

	// Now save a good snapshot where parts match
	goodSnapshot := newTestMetrics("TEST002", 10000)
	goodSnapshot.Timestamp = time.Now()
	goodSnapshot.ColorPages = 5000
	goodSnapshot.MonoPages = 5000 // 5000 + 5000 = 10000 ✓
	err = store.SaveMetricsSnapshot(ctx, goodSnapshot)
	if err != nil {
		t.Fatalf("Failed to save good snapshot: %v", err)
	}

	latest, err = store.GetLatestMetrics(ctx, "TEST002")
	if err != nil {
		t.Fatalf("Failed to get latest: %v", err)
	}
	if latest.PageCount != 10000 {
		t.Errorf("Good snapshot was rejected!")
	}
}

// TestSQLiteStore_MetricsAllowsLegitimateMonoDevice tests that mono-only devices work
func TestSQLiteStore_MetricsAllowsLegitimateMonoDevice(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := newFullTestDevice("MONO001", "192.168.1.101", "Brother", "HL-L2350DW", true, true)
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Mono device: color=0 from the start (legitimate)
	snapshot1 := newTestMetrics("MONO001", 10000)
	snapshot1.Timestamp = time.Now().Add(-10 * time.Minute)
	snapshot1.ColorPages = 0
	snapshot1.MonoPages = 10000
	err = store.SaveMetricsSnapshot(ctx, snapshot1)
	if err != nil {
		t.Fatalf("Failed to save mono metrics: %v", err)
	}

	// Update with more pages
	snapshot2 := newTestMetrics("MONO001", 10500)
	snapshot2.Timestamp = time.Now()
	snapshot2.ColorPages = 0
	snapshot2.MonoPages = 10500
	err = store.SaveMetricsSnapshot(ctx, snapshot2)
	if err != nil {
		t.Fatalf("Failed to save mono update: %v", err)
	}

	latest, err := store.GetLatestMetrics(ctx, "MONO001")
	if err != nil {
		t.Fatalf("Failed to get latest: %v", err)
	}
	if latest.PageCount != 10500 {
		t.Errorf("Mono device update was rejected! page_count=%d", latest.PageCount)
	}
}
