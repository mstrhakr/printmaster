package alerts

import (
	"context"
	"testing"
	"time"

	commonstorage "printmaster/common/storage"
	"printmaster/server/storage"
)

// mockEvaluatorStore implements EvaluatorStore for testing.
type mockEvaluatorStore struct {
	devices            []*storage.Device
	agents             []*storage.Agent
	metrics            map[string]*storage.MetricsSnapshot
	rules              []storage.AlertRule
	alerts             []storage.Alert
	maintenanceWindows []storage.AlertMaintenanceWindow
	settings           *storage.AlertSettings
	createdAlerts      []*storage.Alert
}

func newMockEvaluatorStore() *mockEvaluatorStore {
	return &mockEvaluatorStore{
		metrics:       make(map[string]*storage.MetricsSnapshot),
		createdAlerts: make([]*storage.Alert, 0),
		settings:      &storage.AlertSettings{},
	}
}

func (m *mockEvaluatorStore) ListAllDevices(ctx context.Context) ([]*storage.Device, error) {
	return m.devices, nil
}

func (m *mockEvaluatorStore) ListAgents(ctx context.Context) ([]*storage.Agent, error) {
	return m.agents, nil
}

func (m *mockEvaluatorStore) GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error) {
	if metrics, ok := m.metrics[serial]; ok {
		return metrics, nil
	}
	return nil, nil
}

func (m *mockEvaluatorStore) ListAlertRules(ctx context.Context) ([]storage.AlertRule, error) {
	return m.rules, nil
}

func (m *mockEvaluatorStore) CreateAlert(ctx context.Context, alert *storage.Alert) (int64, error) {
	alert.ID = int64(len(m.createdAlerts) + 1)
	m.createdAlerts = append(m.createdAlerts, alert)
	return alert.ID, nil
}

func (m *mockEvaluatorStore) ListActiveAlerts(ctx context.Context, filters storage.AlertFilters) ([]storage.Alert, error) {
	return m.alerts, nil
}

func (m *mockEvaluatorStore) ResolveAlert(ctx context.Context, id int64) error {
	for i := range m.alerts {
		if m.alerts[i].ID == id {
			m.alerts[i].Status = storage.AlertStatusResolved
		}
	}
	return nil
}

func (m *mockEvaluatorStore) GetActiveAlertMaintenanceWindows(ctx context.Context) ([]storage.AlertMaintenanceWindow, error) {
	return m.maintenanceWindows, nil
}

func (m *mockEvaluatorStore) GetAlertSettings(ctx context.Context) (*storage.AlertSettings, error) {
	return m.settings, nil
}

func TestEvaluator_StartStop(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()
	evaluator := NewEvaluator(store, EvaluatorConfig{
		Interval: 100 * time.Millisecond,
	})

	// Start should not panic
	evaluator.Start()

	// Wait a bit for the evaluator to run
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	evaluator.Stop()

	// Double stop should be safe
	evaluator.Stop()
}

func TestEvaluator_DeviceOfflineAlert(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	// Add a device with no recent metrics
	store.devices = []*storage.Device{
		{
			AgentID: "agent-1",
		},
	}
	store.devices[0].Serial = "DEV001"
	store.devices[0].Model = "Test Printer"
	store.devices[0].LastSeen = time.Now().Add(-1 * time.Hour)

	// No metrics = device appears offline
	store.metrics = make(map[string]*storage.MetricsSnapshot)

	// Add a rule for device offline
	store.rules = []storage.AlertRule{
		{
			ID:       1,
			Name:     "Device Offline",
			Enabled:  true,
			Type:     storage.AlertTypeDeviceOffline,
			Severity: storage.AlertSeverityWarning,
			Scope:    storage.AlertScopeDevice,
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{
		Interval: 1 * time.Hour, // Long interval so we control when it runs
	})

	// Manually trigger evaluation
	evaluator.evaluate()

	// Should have created an alert
	if len(store.createdAlerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(store.createdAlerts))
	}

	alert := store.createdAlerts[0]
	if alert.Type != storage.AlertTypeDeviceOffline {
		t.Errorf("expected device offline alert, got %s", alert.Type)
	}
	if alert.DeviceSerial != "DEV001" {
		t.Errorf("expected device serial DEV001, got %s", alert.DeviceSerial)
	}
}

func TestEvaluator_SupplyLowAlert(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.devices = []*storage.Device{
		{
			Device: commonstorage.Device{
				Serial:   "DEV002",
				Model:    "Color Printer",
				LastSeen: time.Now(),
			},
			AgentID: "agent-1",
		},
	}

	// Metrics with low toner
	store.metrics["DEV002"] = &storage.MetricsSnapshot{
		Serial:    "DEV002",
		Timestamp: time.Now(),
		TonerLevels: map[string]interface{}{
			"black":   15, // Below 20% threshold
			"cyan":    80,
			"magenta": 75,
			"yellow":  90,
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:        1,
			Name:      "Low Toner",
			Enabled:   true,
			Type:      storage.AlertTypeSupplyLow,
			Severity:  storage.AlertSeverityWarning,
			Scope:     storage.AlertScopeDevice,
			Threshold: 20, // 20%
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	if len(store.createdAlerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(store.createdAlerts))
	}

	alert := store.createdAlerts[0]
	if alert.Type != storage.AlertTypeSupplyLow {
		t.Errorf("expected supply low alert, got %s", alert.Type)
	}
}

func TestEvaluator_AgentOfflineAlert(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.agents = []*storage.Agent{
		{
			ID:            1,
			AgentID:       "agent-offline",
			Name:          "Offline Agent",
			Status:        "offline",
			LastHeartbeat: time.Now().Add(-30 * time.Minute),
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:              1,
			Name:            "Agent Offline",
			Enabled:         true,
			Type:            storage.AlertTypeAgentOffline,
			Severity:        storage.AlertSeverityCritical,
			Scope:           storage.AlertScopeAgent,
			DurationMinutes: 5,
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	if len(store.createdAlerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(store.createdAlerts))
	}

	alert := store.createdAlerts[0]
	if alert.Type != storage.AlertTypeAgentOffline {
		t.Errorf("expected agent offline alert, got %s", alert.Type)
	}
	if alert.AgentID != "agent-offline" {
		t.Errorf("expected agent ID agent-offline, got %s", alert.AgentID)
	}
}

func TestEvaluator_NoDuplicateAlerts(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.devices = []*storage.Device{
		{
			Device: commonstorage.Device{
				Serial: "DEV001",
			},
			AgentID: "agent-1",
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:       1,
			Name:     "Device Offline",
			Enabled:  true,
			Type:     storage.AlertTypeDeviceOffline,
			Severity: storage.AlertSeverityWarning,
			Scope:    storage.AlertScopeDevice,
		},
	}

	// Simulate existing active alert (with AgentID to match key format)
	store.alerts = []storage.Alert{
		{
			ID:           1,
			Type:         storage.AlertTypeDeviceOffline,
			Scope:        storage.AlertScopeDevice,
			Status:       storage.AlertStatusActive,
			DeviceSerial: "DEV001",
			AgentID:      "agent-1",
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	// Should not create a duplicate
	if len(store.createdAlerts) != 0 {
		t.Errorf("expected 0 new alerts (existing alert), got %d", len(store.createdAlerts))
	}
}

func TestEvaluator_DisabledRulesIgnored(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.devices = []*storage.Device{
		{
			Device: commonstorage.Device{
				Serial: "DEV001",
			},
			AgentID: "agent-1",
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:       1,
			Name:     "Device Offline",
			Enabled:  false, // Disabled
			Type:     storage.AlertTypeDeviceOffline,
			Severity: storage.AlertSeverityWarning,
			Scope:    storage.AlertScopeDevice,
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	if len(store.createdAlerts) != 0 {
		t.Errorf("expected 0 alerts (rule disabled), got %d", len(store.createdAlerts))
	}
}

func TestEvaluator_MaintenanceWindowSuppresses(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.devices = []*storage.Device{
		{
			Device: commonstorage.Device{
				Serial: "DEV001",
			},
			AgentID: "agent-1",
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:       1,
			Name:     "Device Offline",
			Enabled:  true,
			Type:     storage.AlertTypeDeviceOffline,
			Severity: storage.AlertSeverityWarning, // Non-critical
			Scope:    storage.AlertScopeDevice,
		},
	}

	// Global maintenance window
	now := time.Now()
	store.maintenanceWindows = []storage.AlertMaintenanceWindow{
		{
			ID:        1,
			Name:      "Maintenance",
			Scope:     storage.AlertScopeFleet,
			StartTime: now.Add(-1 * time.Hour),
			EndTime:   now.Add(1 * time.Hour),
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	// Non-critical alerts should be suppressed during maintenance
	if len(store.createdAlerts) != 0 {
		t.Errorf("expected 0 alerts (maintenance window), got %d", len(store.createdAlerts))
	}
}

func TestEvaluator_CriticalBypassesMaintenance(t *testing.T) {
	t.Parallel()

	store := newMockEvaluatorStore()

	store.devices = []*storage.Device{
		{
			Device: commonstorage.Device{
				Serial:         "DEV001",
				StatusMessages: []string{"Paper Jam Error"},
			},
			AgentID: "agent-1",
		},
	}

	store.rules = []storage.AlertRule{
		{
			ID:       1,
			Name:     "Device Error",
			Enabled:  true,
			Type:     storage.AlertTypeDeviceError, // Critical type
			Severity: storage.AlertSeverityCritical,
			Scope:    storage.AlertScopeDevice,
		},
	}

	// Add recent metrics so device isn't considered offline
	store.metrics["DEV001"] = &storage.MetricsSnapshot{
		Serial:    "DEV001",
		Timestamp: time.Now(),
	}

	// Global maintenance window
	now := time.Now()
	store.maintenanceWindows = []storage.AlertMaintenanceWindow{
		{
			ID:        1,
			Name:      "Maintenance",
			Scope:     storage.AlertScopeFleet,
			StartTime: now.Add(-1 * time.Hour),
			EndTime:   now.Add(1 * time.Hour),
		},
	}

	evaluator := NewEvaluator(store, EvaluatorConfig{})
	evaluator.evaluate()

	// Critical alerts should bypass maintenance window
	if len(store.createdAlerts) != 1 {
		t.Errorf("expected 1 alert (critical bypasses maintenance), got %d", len(store.createdAlerts))
	}
}

func TestHelpers_AlertKey(t *testing.T) {
	t.Parallel()

	key1 := alertKey(storage.AlertTypeDeviceOffline, storage.AlertScopeDevice, "DEV001", "", "", "")
	key2 := alertKey(storage.AlertTypeDeviceOffline, storage.AlertScopeDevice, "DEV002", "", "", "")
	key3 := alertKey(storage.AlertTypeSupplyLow, storage.AlertScopeDevice, "DEV001", "", "", "")

	if key1 == key2 {
		t.Error("different devices should have different keys")
	}
	if key1 == key3 {
		t.Error("different alert types should have different keys")
	}
}

func TestHelpers_IsCriticalAlertType(t *testing.T) {
	t.Parallel()

	criticalTypes := []storage.AlertType{
		storage.AlertTypeSupplyCritical,
		storage.AlertTypeDeviceError,
		storage.AlertTypeSiteOutage,
		storage.AlertTypeFleetMassOutage,
	}

	nonCriticalTypes := []storage.AlertType{
		storage.AlertTypeSupplyLow,
		storage.AlertTypeDeviceOffline,
		storage.AlertTypeAgentOffline,
		storage.AlertTypeUsageHigh,
	}

	for _, at := range criticalTypes {
		if !isCriticalAlertType(at) {
			t.Errorf("expected %s to be critical", at)
		}
	}

	for _, at := range nonCriticalTypes {
		if isCriticalAlertType(at) {
			t.Errorf("expected %s to NOT be critical", at)
		}
	}
}

func TestHelpers_TonerLevelToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    interface{}
		expected int
	}{
		{50, 50},
		{int64(75), 75},
		{float64(25.5), 25},
		{"invalid", -1},
		{nil, -1},
	}

	for _, tc := range tests {
		result := tonerLevelToInt(tc.input)
		if result != tc.expected {
			t.Errorf("tonerLevelToInt(%v) = %d, want %d", tc.input, result, tc.expected)
		}
	}
}

func TestHelpers_GetDeviceDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		device   *storage.Device
		expected string
	}{
		{
			device: &storage.Device{
				Device: commonstorage.Device{
					Serial:   "SN123",
					Hostname: "printer-1",
					Model:    "HP LaserJet",
				},
			},
			expected: "printer-1", // Hostname takes priority
		},
		{
			device: &storage.Device{
				Device: commonstorage.Device{
					Serial: "SN123",
					Model:  "HP LaserJet",
				},
			},
			expected: "HP LaserJet", // Model when no hostname
		},
		{
			device: &storage.Device{
				Device: commonstorage.Device{
					Serial: "SN123",
				},
			},
			expected: "SN123", // Falls back to serial
		},
	}

	for _, tc := range tests {
		result := getDeviceDisplayName(tc.device)
		if result != tc.expected {
			t.Errorf("getDeviceDisplayName() = %q, want %q", result, tc.expected)
		}
	}
}
