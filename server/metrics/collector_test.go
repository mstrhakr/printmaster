package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	commonstorage "printmaster/common/storage"
	"printmaster/server/storage"
)

// makeDevice is a test helper to create a storage.Device with common fields
func makeDevice(serial string, lastSeen time.Time, statusMessages []string) *storage.Device {
	return &storage.Device{
		Device: commonstorage.Device{
			Serial:         serial,
			LastSeen:       lastSeen,
			StatusMessages: statusMessages,
		},
	}
}

// mockCollectorStore implements CollectorStore for testing.
type mockCollectorStore struct {
	mu sync.Mutex

	agents          []*storage.Agent
	devices         []*storage.Device
	metrics         map[string]*storage.MetricsSnapshot
	dbStats         *storage.DatabaseStats
	alerts          []storage.Alert
	insertedMetrics []*storage.ServerMetricsSnapshot

	// Error injection
	listAgentsErr    error
	listDevicesErr   error
	getMetricsErr    error
	getDBStatsErr    error
	insertMetricsErr error
	aggregateErr     error
	pruneErr         error
}

func newMockCollectorStore() *mockCollectorStore {
	return &mockCollectorStore{
		agents:          []*storage.Agent{},
		devices:         []*storage.Device{},
		metrics:         make(map[string]*storage.MetricsSnapshot),
		dbStats:         &storage.DatabaseStats{},
		alerts:          []storage.Alert{},
		insertedMetrics: []*storage.ServerMetricsSnapshot{},
	}
}

func (m *mockCollectorStore) ListAgents(ctx context.Context) ([]*storage.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listAgentsErr != nil {
		return nil, m.listAgentsErr
	}
	return m.agents, nil
}

func (m *mockCollectorStore) ListAllDevices(ctx context.Context) ([]*storage.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listDevicesErr != nil {
		return nil, m.listDevicesErr
	}
	return m.devices, nil
}

func (m *mockCollectorStore) GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getMetricsErr != nil {
		return nil, m.getMetricsErr
	}
	return m.metrics[serial], nil
}

func (m *mockCollectorStore) GetDatabaseStats(ctx context.Context) (*storage.DatabaseStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getDBStatsErr != nil {
		return nil, m.getDBStatsErr
	}
	return m.dbStats, nil
}

func (m *mockCollectorStore) InsertServerMetrics(ctx context.Context, snapshot *storage.ServerMetricsSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.insertMetricsErr != nil {
		return m.insertMetricsErr
	}
	m.insertedMetrics = append(m.insertedMetrics, snapshot)
	return nil
}

func (m *mockCollectorStore) AggregateServerMetrics(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.aggregateErr
}

func (m *mockCollectorStore) PruneServerMetrics(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruneErr
}

func (m *mockCollectorStore) ListActiveAlerts(ctx context.Context, filters storage.AlertFilters) ([]storage.Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alerts, nil
}

func (m *mockCollectorStore) getInsertedMetrics() []*storage.ServerMetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*storage.ServerMetricsSnapshot{}, m.insertedMetrics...)
}

// mockWSCounter implements WSConnectionCounter for testing.
type mockWSCounter struct {
	connections int
	agents      int
}

func (m *mockWSCounter) GetConnectionCount() int {
	return m.connections
}

func (m *mockWSCounter) GetAgentCount() int {
	return m.agents
}

// --- Tests ---

func TestNewCollector_DefaultConfig(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{}

	c := NewCollector(store, config)

	if c == nil {
		t.Fatal("NewCollector returned nil")
	}

	// Verify defaults are applied
	if c.config.CollectionInterval != 10*time.Second {
		t.Errorf("default CollectionInterval = %v, want 10s", c.config.CollectionInterval)
	}
	if c.config.AggregationInterval != 5*time.Minute {
		t.Errorf("default AggregationInterval = %v, want 5m", c.config.AggregationInterval)
	}
	if c.config.PruneInterval != 1*time.Hour {
		t.Errorf("default PruneInterval = %v, want 1h", c.config.PruneInterval)
	}
}

func TestNewCollector_CustomConfig(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval:  30 * time.Second,
		AggregationInterval: 10 * time.Minute,
		PruneInterval:       2 * time.Hour,
	}

	c := NewCollector(store, config)

	if c.config.CollectionInterval != 30*time.Second {
		t.Errorf("CollectionInterval = %v, want 30s", c.config.CollectionInterval)
	}
	if c.config.AggregationInterval != 10*time.Minute {
		t.Errorf("AggregationInterval = %v, want 10m", c.config.AggregationInterval)
	}
	if c.config.PruneInterval != 2*time.Hour {
		t.Errorf("PruneInterval = %v, want 2h", c.config.PruneInterval)
	}
}

func TestCollector_StartStop(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval:  100 * time.Millisecond,
		AggregationInterval: 1 * time.Hour, // Long to avoid triggering
		PruneInterval:       1 * time.Hour,
	}

	c := NewCollector(store, config)

	// Start collector
	c.Start()

	// Wait for at least one collection
	time.Sleep(150 * time.Millisecond)

	// Stop collector
	c.Stop()

	// Verify at least one metric was collected
	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Error("expected at least one metrics snapshot to be inserted")
	}
}

func TestCollector_DoubleStart(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval: 10 * time.Second,
	}

	c := NewCollector(store, config)

	c.Start()
	c.Start() // Should be no-op

	c.Stop()
}

func TestCollector_DoubleStop(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval: 10 * time.Second,
	}

	c := NewCollector(store, config)

	c.Start()
	c.Stop()
	c.Stop() // Should be no-op
}

func TestCollector_StopWithoutStart(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{}

	c := NewCollector(store, config)

	// Should not panic
	c.Stop()
}

func TestCollector_SetWSCounter(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval: 100 * time.Millisecond,
	}

	c := NewCollector(store, config)

	wsCounter := &mockWSCounter{connections: 5, agents: 3}
	c.SetWSCounter(wsCounter)

	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics to be inserted")
	}

	// Check WS metrics were collected
	last := inserted[len(inserted)-1]
	if last.Server.WSConnections != 5 {
		t.Errorf("WSConnections = %d, want 5", last.Server.WSConnections)
	}
	if last.Server.WSAgents != 3 {
		t.Errorf("WSAgents = %d, want 3", last.Server.WSAgents)
	}
}

func TestCollector_GetLatest(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{
		CollectionInterval: 100 * time.Millisecond,
	}

	c := NewCollector(store, config)

	// Before start, should be nil
	if c.GetLatest() != nil {
		t.Error("GetLatest should return nil before any collection")
	}

	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	// After collection, should have a snapshot
	latest := c.GetLatest()
	if latest == nil {
		t.Error("GetLatest should return a snapshot after collection")
	}
}

func TestCollector_FleetMetrics(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	store.agents = []*storage.Agent{
		{AgentID: "agent-1", Name: "Site A"},
		{AgentID: "agent-2", Name: "Site B"},
	}
	store.devices = []*storage.Device{
		makeDevice("DEV001", time.Now(), nil),
		makeDevice("DEV002", time.Now(), nil),
		makeDevice("DEV003", time.Now().Add(-30*time.Minute), nil), // Offline
	}
	store.metrics["DEV001"] = &storage.MetricsSnapshot{
		PageCount:   1000,
		ColorPages:  200,
		MonoPages:   800,
		TonerLevels: map[string]interface{}{"black": 75, "cyan": 50},
	}
	store.metrics["DEV002"] = &storage.MetricsSnapshot{
		PageCount:   500,
		ColorPages:  100,
		MonoPages:   400,
		TonerLevels: map[string]interface{}{"black": 5}, // Critical
	}

	config := CollectorConfig{
		CollectionInterval: 100 * time.Millisecond,
	}

	c := NewCollector(store, config)
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics to be inserted")
	}

	latest := inserted[len(inserted)-1]

	// Check fleet counts
	if latest.Fleet.TotalAgents != 2 {
		t.Errorf("TotalAgents = %d, want 2", latest.Fleet.TotalAgents)
	}
	if latest.Fleet.TotalDevices != 3 {
		t.Errorf("TotalDevices = %d, want 3", latest.Fleet.TotalDevices)
	}
	if latest.Fleet.DevicesOnline != 2 {
		t.Errorf("DevicesOnline = %d, want 2", latest.Fleet.DevicesOnline)
	}
	if latest.Fleet.DevicesOffline != 1 {
		t.Errorf("DevicesOffline = %d, want 1", latest.Fleet.DevicesOffline)
	}

	// Check page counts
	if latest.Fleet.TotalPages != 1500 {
		t.Errorf("TotalPages = %d, want 1500", latest.Fleet.TotalPages)
	}
	if latest.Fleet.ColorPages != 300 {
		t.Errorf("ColorPages = %d, want 300", latest.Fleet.ColorPages)
	}
	if latest.Fleet.MonoPages != 1200 {
		t.Errorf("MonoPages = %d, want 1200", latest.Fleet.MonoPages)
	}
}

func TestCollector_TonerLevelClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tonerLevels  map[string]interface{}
		wantCritical bool
		wantLow      bool
		wantMedium   bool
		wantHigh     bool
	}{
		{
			name:        "High (75%)",
			tonerLevels: map[string]interface{}{"black": 75},
			wantHigh:    true,
		},
		{
			name:        "Medium (40%)",
			tonerLevels: map[string]interface{}{"black": 40},
			wantMedium:  true,
		},
		{
			name:        "Low (20%)",
			tonerLevels: map[string]interface{}{"black": 20},
			wantLow:     true,
		},
		{
			name:         "Critical (5%)",
			tonerLevels:  map[string]interface{}{"black": 5},
			wantCritical: true,
		},
		{
			name:         "Min of multiple (cyan low)",
			tonerLevels:  map[string]interface{}{"black": 80, "cyan": 8, "magenta": 90},
			wantCritical: true, // 8% is critical
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMockCollectorStore()
			store.devices = []*storage.Device{makeDevice("DEV001", time.Now(), nil)}
			store.metrics["DEV001"] = &storage.MetricsSnapshot{TonerLevels: tt.tonerLevels}

			config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}
			c := NewCollector(store, config)
			c.Start()
			time.Sleep(150 * time.Millisecond)
			c.Stop()

			inserted := store.getInsertedMetrics()
			if len(inserted) == 0 {
				t.Fatal("expected metrics")
			}

			fleet := inserted[len(inserted)-1].Fleet
			if tt.wantCritical && fleet.TonerCritical != 1 {
				t.Errorf("TonerCritical = %d, want 1", fleet.TonerCritical)
			}
			if tt.wantLow && fleet.TonerLow != 1 {
				t.Errorf("TonerLow = %d, want 1", fleet.TonerLow)
			}
			if tt.wantMedium && fleet.TonerMedium != 1 {
				t.Errorf("TonerMedium = %d, want 1", fleet.TonerMedium)
			}
			if tt.wantHigh && fleet.TonerHigh != 1 {
				t.Errorf("TonerHigh = %d, want 1", fleet.TonerHigh)
			}
		})
	}
}

func TestCollector_ServerMetrics(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	store.dbStats = &storage.DatabaseStats{
		Agents:           5,
		Devices:          20,
		MetricsSnapshots: 1000,
		Sessions:         10,
		Users:            3,
		AuditEntries:     500,
	}
	store.alerts = []storage.Alert{
		{ID: 1, Status: "active"},
		{ID: 2, Status: "active"},
	}

	config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}
	c := NewCollector(store, config)
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics")
	}

	server := inserted[len(inserted)-1].Server

	// Check DB stats were collected
	if server.DBAgents != 5 {
		t.Errorf("DBAgents = %d, want 5", server.DBAgents)
	}
	if server.DBDevices != 20 {
		t.Errorf("DBDevices = %d, want 20", server.DBDevices)
	}
	if server.DBMetricsRows != 1000 {
		t.Errorf("DBMetricsRows = %d, want 1000", server.DBMetricsRows)
	}

	// Check runtime metrics are populated (they'll be non-zero)
	if server.Goroutines == 0 {
		t.Error("Goroutines should be non-zero")
	}
}

func TestCollector_StatusMessageClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		messages    []string
		wantError   int
		wantWarning int
		wantJam     int
	}{
		{
			name:     "No messages",
			messages: []string{},
		},
		{
			name:      "Error message",
			messages:  []string{"Device error"},
			wantError: 1,
		},
		{
			name:        "Warning message",
			messages:    []string{"Toner low"},
			wantWarning: 1,
		},
		{
			name:      "Paper jam",
			messages:  []string{"Paper jammed"},
			wantJam:   1,
			wantError: 1, // Jam also counts as error
		},
		{
			name:      "Fault message",
			messages:  []string{"Communication fault"},
			wantError: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := newMockCollectorStore()
			store.devices = []*storage.Device{makeDevice("DEV001", time.Now(), tt.messages)}

			config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}
			c := NewCollector(store, config)
			c.Start()
			time.Sleep(150 * time.Millisecond)
			c.Stop()

			inserted := store.getInsertedMetrics()
			if len(inserted) == 0 {
				t.Fatal("expected metrics")
			}

			fleet := inserted[len(inserted)-1].Fleet
			if fleet.DevicesError != tt.wantError {
				t.Errorf("DevicesError = %d, want %d", fleet.DevicesError, tt.wantError)
			}
			if fleet.DevicesWarning != tt.wantWarning {
				t.Errorf("DevicesWarning = %d, want %d", fleet.DevicesWarning, tt.wantWarning)
			}
			if fleet.DevicesJam != tt.wantJam {
				t.Errorf("DevicesJam = %d, want %d", fleet.DevicesJam, tt.wantJam)
			}
		})
	}
}

func TestCollector_InsertError(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	store.insertMetricsErr = errors.New("insert failed")

	config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}
	c := NewCollector(store, config)

	// Should not panic on insert error
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	// GetLatest should still return cached snapshot
	if c.GetLatest() == nil {
		t.Error("GetLatest should return cached snapshot even if insert failed")
	}
}

func TestClassifyStatusMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		messages    []string
		wantError   bool
		wantWarning bool
		wantJam     bool
	}{
		{"empty", []string{}, false, false, false},
		{"error", []string{"Error occurred"}, true, false, false},
		{"fault", []string{"Device fault"}, true, false, false},
		{"failure", []string{"Network failure"}, true, false, false},
		{"warning", []string{"Toner warning"}, false, true, false},
		{"low", []string{"Paper low"}, false, true, false},
		{"empty supply", []string{"Toner empty"}, false, true, false},
		{"jam", []string{"Paper jam"}, false, false, true},
		{"jammed", []string{"Printer jammed"}, false, false, true},
		{"multiple", []string{"Jam", "Error", "Low"}, true, true, true},
		{"case insensitive", []string{"ERROR"}, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hasError, hasWarning, hasJam := classifyStatusMessages(tt.messages)
			if hasError != tt.wantError {
				t.Errorf("hasError = %v, want %v", hasError, tt.wantError)
			}
			if hasWarning != tt.wantWarning {
				t.Errorf("hasWarning = %v, want %v", hasWarning, tt.wantWarning)
			}
			if hasJam != tt.wantJam {
				t.Errorf("hasJam = %v, want %v", hasJam, tt.wantJam)
			}
		})
	}
}

func TestTonerLevelToInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input interface{}
		want  int
	}{
		{"int", 50, 50},
		{"int64", int64(75), 75},
		{"float64", float64(25.5), 25},
		{"json.Number", json.Number("80"), 80},
		{"string (invalid)", "50%", -1},
		{"nil", nil, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tonerLevelToInt(tt.input)
			if got != tt.want {
				t.Errorf("tonerLevelToInt(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestStringToLower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Hello", "hello"},
		{"UPPERCASE", "uppercase"},
		{"lowercase", "lowercase"},
		{"MiXeD", "mixed"},
		{"", ""},
		{"123ABC", "123abc"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got := stringToLower(tt.input)
			if got != tt.want {
				t.Errorf("stringToLower(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		s       string
		substrs []string
		want    bool
	}{
		{"empty string", "", []string{"a", "b"}, false},
		{"empty substrs", "hello", []string{}, false},
		{"contains first", "hello world", []string{"hello", "foo"}, true},
		{"contains second", "hello world", []string{"foo", "world"}, true},
		{"contains none", "hello world", []string{"foo", "bar"}, false},
		{"empty substr in list", "hello", []string{""}, false},
		{"exact match", "hello", []string{"hello"}, true},
		{"partial match", "hello", []string{"ell"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := containsAny(tt.s, tt.substrs...)
			if got != tt.want {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.want)
			}
		})
	}
}

func TestCollector_MetricsWithNoDevices(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	// Empty store - no agents, no devices

	config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}
	c := NewCollector(store, config)
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics even with empty fleet")
	}

	fleet := inserted[len(inserted)-1].Fleet
	if fleet.TotalAgents != 0 {
		t.Errorf("TotalAgents = %d, want 0", fleet.TotalAgents)
	}
	if fleet.TotalDevices != 0 {
		t.Errorf("TotalDevices = %d, want 0", fleet.TotalDevices)
	}
}

func TestCollector_MetricsTimestamp(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}

	before := time.Now().UTC()
	c := NewCollector(store, config)
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()
	after := time.Now().UTC()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics")
	}

	ts := inserted[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp %v not between %v and %v", ts, before, after)
	}
}

func TestCollector_MetricsTier(t *testing.T) {
	t.Parallel()

	store := newMockCollectorStore()
	config := CollectorConfig{CollectionInterval: 100 * time.Millisecond}

	c := NewCollector(store, config)
	c.Start()
	time.Sleep(150 * time.Millisecond)
	c.Stop()

	inserted := store.getInsertedMetrics()
	if len(inserted) == 0 {
		t.Fatal("expected metrics")
	}

	// Raw collection should have "raw" tier
	if inserted[0].Tier != "raw" {
		t.Errorf("Tier = %q, want 'raw'", inserted[0].Tier)
	}
}
