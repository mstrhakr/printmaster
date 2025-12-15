package reports

import (
	"context"
	"testing"
	"time"

	commonstorage "printmaster/common/storage"
	"printmaster/server/storage"
)

// mockGeneratorStore implements GeneratorStore for testing.
type mockGeneratorStore struct {
	devices      []*storage.Device
	metrics      map[string]*storage.MetricsSnapshot
	metricsHist  map[string][]*storage.MetricsSnapshot
	agents       []*storage.Agent
	tenants      []*storage.Tenant
	sites        map[string][]*storage.Site
	alerts       []*storage.Alert
	alertSummary *storage.AlertSummary
}

func newMockGeneratorStore() *mockGeneratorStore {
	return &mockGeneratorStore{
		devices:     []*storage.Device{},
		metrics:     make(map[string]*storage.MetricsSnapshot),
		metricsHist: make(map[string][]*storage.MetricsSnapshot),
		agents:      []*storage.Agent{},
		tenants:     []*storage.Tenant{},
		sites:       make(map[string][]*storage.Site),
		alerts:      []*storage.Alert{},
		alertSummary: &storage.AlertSummary{
			AlertsByType: make(map[string]int),
		},
	}
}

func (m *mockGeneratorStore) ListAllDevices(ctx context.Context) ([]*storage.Device, error) {
	return m.devices, nil
}

func (m *mockGeneratorStore) GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error) {
	return m.metrics[serial], nil
}

func (m *mockGeneratorStore) GetMetricsAtOrBefore(ctx context.Context, serial string, at time.Time) (*storage.MetricsSnapshot, error) {
	h := m.metricsHist[serial]
	var best *storage.MetricsSnapshot
	for _, snap := range h {
		if snap == nil {
			continue
		}
		if snap.Timestamp.After(at) {
			continue
		}
		if best == nil || snap.Timestamp.After(best.Timestamp) {
			best = snap
		}
	}
	return best, nil
}

func (m *mockGeneratorStore) GetMetricsHistory(ctx context.Context, serial string, since time.Time) ([]*storage.MetricsSnapshot, error) {
	h := m.metricsHist[serial]
	if len(h) == 0 {
		return h, nil
	}
	filtered := make([]*storage.MetricsSnapshot, 0, len(h))
	for _, snap := range h {
		if snap == nil {
			continue
		}
		if !snap.Timestamp.Before(since) {
			filtered = append(filtered, snap)
		}
	}
	return filtered, nil
}

func (m *mockGeneratorStore) ListAgents(ctx context.Context) ([]*storage.Agent, error) {
	return m.agents, nil
}

func (m *mockGeneratorStore) GetAgent(ctx context.Context, agentID string) (*storage.Agent, error) {
	for _, a := range m.agents {
		if a.AgentID == agentID {
			return a, nil
		}
	}
	return nil, nil
}

func (m *mockGeneratorStore) ListTenants(ctx context.Context) ([]*storage.Tenant, error) {
	return m.tenants, nil
}

func (m *mockGeneratorStore) GetTenant(ctx context.Context, id string) (*storage.Tenant, error) {
	for _, t := range m.tenants {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockGeneratorStore) ListSitesByTenant(ctx context.Context, tenantID string) ([]*storage.Site, error) {
	return m.sites[tenantID], nil
}

func (m *mockGeneratorStore) ListAlerts(ctx context.Context, filter storage.AlertFilter) ([]*storage.Alert, error) {
	return m.alerts, nil
}

func (m *mockGeneratorStore) GetAlertSummary(ctx context.Context) (*storage.AlertSummary, error) {
	return m.alertSummary, nil
}

// helper to create a test device
func newTestDevice(serial, model, ip string, agentID string) *storage.Device {
	return &storage.Device{
		Device: commonstorage.Device{
			Serial:   serial,
			Model:    model,
			IP:       ip,
			Hostname: "host-" + serial,
			LastSeen: time.Now(),
		},
		AgentID: agentID,
	}
}

// helper to create a test agent
func newTestAgent(id int64, agentID, name, version string) *storage.Agent {
	return &storage.Agent{
		ID:            id,
		AgentID:       agentID,
		Name:          name,
		Version:       version,
		Status:        "active",
		LastHeartbeat: time.Now(),
	}
}

func TestGenerator_DeviceInventory(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "HP LaserJet Pro", "192.168.1.10", "agent-1"),
		newTestDevice("SN002", "Canon ImageRunner", "192.168.1.11", "agent-1"),
	}
	store.agents = []*storage.Agent{
		newTestAgent(1, "agent-1", "Main Office", "1.0.0"),
	}
	store.metrics["SN001"] = &storage.MetricsSnapshot{
		PageCount: 1000,
		Timestamp: time.Now(),
	}
	store.metrics["SN002"] = &storage.MetricsSnapshot{
		PageCount: 500,
		Timestamp: time.Now(),
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportTypeDeviceInventory,
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}

	// Check columns exist
	if len(result.Columns) == 0 {
		t.Error("expected columns to be populated")
	}
}

func TestGenerator_AgentInventory(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	now := time.Now()
	store.agents = []*storage.Agent{
		{
			ID:            1,
			AgentID:       "agent-1",
			Name:          "Main Office",
			Version:       "1.0.0",
			OSVersion:     "Windows 11",
			LastHeartbeat: now,
			DeviceCount:   5,
			Status:        "active",
		},
		{
			ID:            2,
			AgentID:       "agent-2",
			Name:          "Remote Office",
			Version:       "0.9.0",
			OSVersion:     "Ubuntu 22.04",
			LastHeartbeat: now.Add(-10 * time.Minute),
			DeviceCount:   3,
			Status:        "offline",
		},
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportTypeAgentInventory,
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result.Rows))
	}
}

func TestGenerator_SuppliesStatus(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "HP LaserJet Pro", "192.168.1.10", "agent-1"),
	}
	store.metrics["SN001"] = &storage.MetricsSnapshot{
		TonerLevels: map[string]interface{}{
			"black":   50,
			"cyan":    75,
			"magenta": 80,
			"yellow":  25,
		},
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportTypeSuppliesStatus,
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(result.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(result.Rows))
	}
}

func TestGenerator_AlertSummary(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.alertSummary = &storage.AlertSummary{
		WarningCounts: struct {
			Devices int `json:"devices"`
			Agents  int `json:"agents"`
			Sites   int `json:"sites"`
			Tenants int `json:"tenants"`
		}{
			Devices: 2,
			Agents:  0,
		},
		CriticalCounts: struct {
			Devices int `json:"devices"`
			Agents  int `json:"agents"`
			Sites   int `json:"sites"`
			Tenants int `json:"tenants"`
		}{
			Devices: 1,
			Agents:  0,
		},
		AlertsByType: map[string]int{
			"toner_low":      2,
			"device_offline": 1,
		},
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportTypeAlertSummary,
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Alert summary should have summary data
	if result.Summary == nil {
		t.Error("expected summary to be populated")
	}
}

func TestGenerator_UsageSummary_AsUsageAuditWithDeltas(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "HP LaserJet Pro", "192.168.1.10", "agent-1"),
	}

	baselineAt := time.Now().Add(-10 * 24 * time.Hour)
	currentAt := time.Now()
	store.metrics["SN001"] = &storage.MetricsSnapshot{
		Serial:     "SN001",
		Timestamp:  currentAt,
		PageCount:  200,
		ColorPages: 50,
		MonoPages:  150,
		ScanCount:  20,
	}
	store.metricsHist["SN001"] = []*storage.MetricsSnapshot{
		{
			Serial:     "SN001",
			Timestamp:  baselineAt,
			PageCount:  120,
			ColorPages: 30,
			MonoPages:  90,
			ScanCount:  10,
		},
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type:          storage.ReportTypeUsageSummary,
			TimeRangeType: "last_30d",
		},
		StartTime: time.Now().Add(-30 * 24 * time.Hour),
		EndTime:   time.Now(),
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result == nil || len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	row := result.Rows[0]
	if row["serial"] != "SN001" {
		t.Errorf("expected serial SN001, got %v", row["serial"])
	}

	// Current values
	if row["page_count_current"] != 200 {
		t.Errorf("expected page_count_current 200, got %v", row["page_count_current"])
	}

	// Baseline + delta values
	if row["page_count_then"] != 120 {
		t.Errorf("expected page_count_then 120, got %v", row["page_count_then"])
	}
	if row["page_count_delta"].(int64) != 80 {
		t.Errorf("expected page_count_delta 80, got %v", row["page_count_delta"])
	}
}

func TestGenerator_UsageSummary_UsesBaselineAtOrBeforeStart(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "HP LaserJet Pro", "192.168.1.10", "agent-1"),
	}

	start := time.Now().Add(-30 * 24 * time.Hour)
	baselineBefore := start.Add(-10 * 24 * time.Hour)
	baselineAfter := start.Add(10 * 24 * time.Hour)
	currentAt := time.Now()

	store.metrics["SN001"] = &storage.MetricsSnapshot{
		Serial:     "SN001",
		Timestamp:  currentAt,
		PageCount:  200,
		ColorPages: 50,
		MonoPages:  150,
		ScanCount:  20,
	}
	store.metricsHist["SN001"] = []*storage.MetricsSnapshot{
		{
			Serial:    "SN001",
			Timestamp: baselineBefore,
			PageCount: 100,
		},
		{
			Serial:    "SN001",
			Timestamp: baselineAfter,
			PageCount: 150,
		},
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type:          storage.ReportTypeUsageSummary,
			TimeRangeType: "last_30d",
		},
		StartTime: start,
		EndTime:   time.Now(),
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result == nil || len(result.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(result.Rows))
	}

	row := result.Rows[0]
	if row["page_count_then"] != 100 {
		t.Errorf("expected page_count_then 100 (baseline before start), got %v", row["page_count_then"])
	}
	if row["page_count_delta"].(int64) != 100 {
		t.Errorf("expected page_count_delta 100, got %v", row["page_count_delta"])
	}
}

func TestGenerator_WithFilters(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "HP LaserJet Pro", "192.168.1.10", "agent-1"),
		newTestDevice("SN002", "Canon ImageRunner", "192.168.1.11", "agent-2"),
		newTestDevice("SN003", "HP OfficeJet", "192.168.1.12", "agent-1"),
	}

	gen := NewGenerator(store)

	// Filter by agent
	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type:     storage.ReportTypeDeviceInventory,
			AgentIDs: []string{"agent-1"},
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should only include agent-1 devices
	if len(result.Rows) != 2 {
		t.Errorf("expected 2 rows for agent-1, got %d", len(result.Rows))
	}
}

func TestGenerator_InvalidReportType(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	gen := NewGenerator(store)

	_, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportType("invalid-type"),
		},
	})
	if err == nil {
		t.Error("expected error for invalid report type")
	}
}

func TestGenerateResult_Metadata(t *testing.T) {
	t.Parallel()

	store := newMockGeneratorStore()
	store.devices = []*storage.Device{
		newTestDevice("SN001", "Test Printer", "192.168.1.10", "agent-1"),
	}

	gen := NewGenerator(store)

	result, err := gen.Generate(context.Background(), GenerateParams{
		Report: &storage.ReportDefinition{
			Type: storage.ReportTypeDeviceInventory,
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result.Metadata["generated"] == "" {
		t.Error("generated timestamp should be set in metadata")
	}

	if result.RowCount != 1 {
		t.Errorf("expected RowCount 1, got %d", result.RowCount)
	}
}
