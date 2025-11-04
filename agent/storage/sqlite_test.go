package storage

import (
	"context"
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
	device := &Device{
		Serial:       "TEST001",
		IP:           "192.168.1.100",
		Manufacturer: "HP",
		Model:        "LaserJet Pro",
		IsSaved:      false,
		Consumables:  []string{"Black Toner"},
	}

	// Test Create
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create a metrics snapshot for this device
	snapshot := &MetricsSnapshot{
		Serial:      "TEST001",
		Timestamp:   time.Now(),
		PageCount:   500,
		TonerLevels: map[string]interface{}{"black": 75},
	}
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
	snapshot2 := &MetricsSnapshot{
		Serial:      "TEST001",
		Timestamp:   time.Now(),
		PageCount:   1000,
		TonerLevels: map[string]interface{}{"black": 70},
	}
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

func TestSQLiteStore_Upsert(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	device := &Device{
		Serial:       "TEST002",
		IP:           "192.168.1.101",
		Manufacturer: "Canon",
		Model:        "ImageRunner",
	}

	// First upsert should create
	err = store.Upsert(ctx, device)
	if err != nil {
		t.Fatalf("Failed to upsert (create): %v", err)
	}

	// Save initial metrics
	snapshot1 := &MetricsSnapshot{
		Serial:    "TEST002",
		Timestamp: time.Now(),
		PageCount: 500,
	}
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
	snapshot2 := &MetricsSnapshot{
		Serial:    "TEST002",
		Timestamp: time.Now(),
		PageCount: 1500,
	}
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
		{Serial: "HP001", IP: "192.168.1.10", Manufacturer: "HP", Model: "LaserJet 1", IsSaved: true},
		{Serial: "HP002", IP: "192.168.1.11", Manufacturer: "HP", Model: "LaserJet 2", IsSaved: false},
		{Serial: "CANON001", IP: "192.168.1.20", Manufacturer: "Canon", Model: "ImageRunner", IsSaved: true},
		{Serial: "EPSON001", IP: "192.168.1.30", Manufacturer: "Epson", Model: "WorkForce", IsSaved: false},
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

	device := &Device{
		Serial:  "TEST003",
		IP:      "192.168.1.102",
		IsSaved: false,
	}

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
		{Serial: "DEL001", IP: "192.168.1.40", Manufacturer: "HP", IsSaved: true},
		{Serial: "DEL002", IP: "192.168.1.41", Manufacturer: "HP", IsSaved: false},
		{Serial: "DEL003", IP: "192.168.1.42", Manufacturer: "Canon", IsSaved: true},
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
	store.Create(ctx, &Device{Serial: "STAT001", IP: "192.168.1.50", IsSaved: true})
	store.Create(ctx, &Device{Serial: "STAT002", IP: "192.168.1.51", IsSaved: true})
	store.Create(ctx, &Device{Serial: "STAT003", IP: "192.168.1.52", IsSaved: false})

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

	device := &Device{
		Serial:          "COMPLEX001",
		IP:              "192.168.1.100",
		Manufacturer:    "HP",
		Model:           "LaserJet M1536dnf",
		Hostname:        "printer-01",
		Firmware:        "v2.0.1",
		MACAddress:      "c4:34:6b:1a:50:5c",
		SubnetMask:      "255.255.0.0",
		Gateway:         "192.168.1.1",
		DNSServers:      []string{"8.8.8.8", "8.8.4.4"},
		DHCPServer:      "192.168.1.1",
		Consumables:     []string{"Black Toner CE278A", "Cyan Toner", "Magenta Toner", "Yellow Toner"},
		StatusMessages:  []string{"Ready", "Toner Low"},
		IsSaved:         true,
		DiscoveryMethod: "snmp",
		WalkFilename:    "mib_walk_192_168_1_100_20251101.json",
		RawData: map[string]interface{}{
			"uptime_seconds": 1234567,
			"duplex":         true,
			"color":          true,
		},
	}

	// Create
	err = store.Create(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create complex device: %v", err)
	}

	// Save metrics separately
	snapshot := &MetricsSnapshot{
		Serial:    "COMPLEX001",
		Timestamp: time.Now(),
		PageCount: 50000,
		TonerLevels: map[string]interface{}{
			"black":   85,
			"cyan":    60,
			"magenta": 70,
			"yellow":  55,
		},
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
	err = store.Create(ctx, &Device{IP: "192.168.1.1"})
	if err != ErrInvalidSerial {
		t.Errorf("Expected ErrInvalidSerial for empty serial, got %v", err)
	}

	err = store.Update(ctx, &Device{IP: "192.168.1.1"})
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
	err = store.Update(ctx, &Device{Serial: "NONEXISTENT", IP: "192.168.1.1"})
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
	device := &Device{
		Serial:       "BACKUP001",
		IP:           "192.168.1.200",
		Manufacturer: "Canon",
		Model:        "imageRUNNER",
		IsSaved:      true,
	}

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
	newDevice := &Device{
		Serial:       "RESET001",
		IP:           "192.168.1.201",
		Manufacturer: "Brother",
		Model:        "MFC",
		IsSaved:      false,
	}

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
