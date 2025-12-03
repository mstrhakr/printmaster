package storage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeviceJSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	device := Device{
		Serial:          "SN123456",
		IP:              "192.168.1.100",
		Manufacturer:    "Epson",
		Model:           "WorkForce Pro",
		Hostname:        "printer1",
		Firmware:        "1.2.3",
		MACAddress:      "00:11:22:33:44:55",
		SubnetMask:      "255.255.255.0",
		Gateway:         "192.168.1.1",
		Consumables:     []string{"Black Toner", "Cyan Toner"},
		StatusMessages:  []string{"Ready"},
		LastSeen:        now,
		CreatedAt:       now,
		FirstSeen:       now,
		DiscoveryMethod: "snmp",
		AssetNumber:     "ASSET001",
		Location:        "Office A",
		Description:     "Main office printer",
		WebUIURL:        "http://192.168.1.100",
		RawData:         map[string]interface{}{"custom": "value"},
	}

	// Marshal to JSON
	data, err := json.Marshal(device)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded Device
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify key fields
	if decoded.Serial != device.Serial {
		t.Errorf("Serial = %q, want %q", decoded.Serial, device.Serial)
	}
	if decoded.IP != device.IP {
		t.Errorf("IP = %q, want %q", decoded.IP, device.IP)
	}
	if decoded.Manufacturer != device.Manufacturer {
		t.Errorf("Manufacturer = %q, want %q", decoded.Manufacturer, device.Manufacturer)
	}
	if decoded.Model != device.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, device.Model)
	}
	if len(decoded.Consumables) != len(device.Consumables) {
		t.Errorf("Consumables length = %d, want %d", len(decoded.Consumables), len(device.Consumables))
	}
}

func TestMetricsSnapshotJSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	metrics := MetricsSnapshot{
		ID:         1,
		Serial:     "SN123456",
		Timestamp:  now,
		PageCount:  1000,
		ColorPages: 300,
		MonoPages:  700,
		ScanCount:  50,
		TonerLevels: map[string]interface{}{
			"black":   75,
			"cyan":    50,
			"magenta": 60,
			"yellow":  80,
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded MetricsSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// Verify key fields
	if decoded.Serial != metrics.Serial {
		t.Errorf("Serial = %q, want %q", decoded.Serial, metrics.Serial)
	}
	if decoded.PageCount != metrics.PageCount {
		t.Errorf("PageCount = %d, want %d", decoded.PageCount, metrics.PageCount)
	}
	if decoded.ColorPages != metrics.ColorPages {
		t.Errorf("ColorPages = %d, want %d", decoded.ColorPages, metrics.ColorPages)
	}
	if decoded.MonoPages != metrics.MonoPages {
		t.Errorf("MonoPages = %d, want %d", decoded.MonoPages, metrics.MonoPages)
	}
	if len(decoded.TonerLevels) != len(metrics.TonerLevels) {
		t.Errorf("TonerLevels length = %d, want %d", len(decoded.TonerLevels), len(metrics.TonerLevels))
	}
}

func TestFieldLockJSONRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().Truncate(time.Second)
	lock := FieldLock{
		Field:    "model",
		Reason:   "manually_entered",
		LockedAt: now,
		LockedBy: "admin",
	}

	// Marshal to JSON
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	// Unmarshal back
	var decoded FieldLock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Field != lock.Field {
		t.Errorf("Field = %q, want %q", decoded.Field, lock.Field)
	}
	if decoded.Reason != lock.Reason {
		t.Errorf("Reason = %q, want %q", decoded.Reason, lock.Reason)
	}
	if decoded.LockedBy != lock.LockedBy {
		t.Errorf("LockedBy = %q, want %q", decoded.LockedBy, lock.LockedBy)
	}
}

func TestDeviceFilterDefaults(t *testing.T) {
	t.Parallel()

	filter := DeviceFilter{}

	// All pointers should be nil (meaning "all")
	if filter.IsSaved != nil {
		t.Error("IsSaved should default to nil")
	}
	if filter.Visible != nil {
		t.Error("Visible should default to nil")
	}
	if filter.LastSeenAfter != nil {
		t.Error("LastSeenAfter should default to nil")
	}
	if filter.Limit != 0 {
		t.Error("Limit should default to 0")
	}
}

func TestDeviceFilterWithValues(t *testing.T) {
	t.Parallel()

	saved := true
	visible := false
	now := time.Now()

	filter := DeviceFilter{
		IsSaved:       &saved,
		Visible:       &visible,
		IP:            "192.168.1.1",
		Serial:        "SN123",
		Manufacturer:  "Epson",
		LastSeenAfter: &now,
		Limit:         100,
	}

	if filter.IsSaved == nil || *filter.IsSaved != true {
		t.Error("IsSaved should be true")
	}
	if filter.Visible == nil || *filter.Visible != false {
		t.Error("Visible should be false")
	}
	if filter.IP != "192.168.1.1" {
		t.Errorf("IP = %q, want %q", filter.IP, "192.168.1.1")
	}
	if filter.Limit != 100 {
		t.Errorf("Limit = %d, want %d", filter.Limit, 100)
	}
}
