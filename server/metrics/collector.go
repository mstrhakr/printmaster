// Package metrics provides the server metrics collection background worker.
package metrics

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"printmaster/server/storage"
)

// CollectorConfig configures the metrics collector.
type CollectorConfig struct {
	// Interval between raw metric collections (default 10s)
	CollectionInterval time.Duration

	// Interval between aggregation runs (default 5m)
	AggregationInterval time.Duration

	// Interval between prune runs (default 1h)
	PruneInterval time.Duration

	// Logger for collector events
	Logger *slog.Logger
}

// CollectorStore defines the storage operations needed by the collector.
type CollectorStore interface {
	// Fleet data collection
	ListAgents(ctx context.Context) ([]*storage.Agent, error)
	ListAllDevices(ctx context.Context) ([]*storage.Device, error)
	GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error)

	// Database stats
	GetDatabaseStats(ctx context.Context) (*storage.DatabaseStats, error)

	// Server metrics storage
	InsertServerMetrics(ctx context.Context, snapshot *storage.ServerMetricsSnapshot) error
	AggregateServerMetrics(ctx context.Context) error
	PruneServerMetrics(ctx context.Context) error

	// Alert counts for dashboard
	ListActiveAlerts(ctx context.Context, filters storage.AlertFilters) ([]storage.Alert, error)
}

// WSConnectionCounter allows injecting WebSocket connection counts.
type WSConnectionCounter interface {
	GetConnectionCount() int
	GetAgentCount() int
	IsAgentConnected(agentID string) bool // Check if specific agent has WS connection
}

// Collector periodically collects and stores server metrics.
type Collector struct {
	store     CollectorStore
	config    CollectorConfig
	logger    *slog.Logger
	wsCounter WSConnectionCounter
	dbPath    string // For DB file size calculation

	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}

	// Cached latest snapshot for quick access
	latestSnapshot *storage.ServerMetricsSnapshot
}

// NewCollector creates a new metrics collector.
func NewCollector(store CollectorStore, config CollectorConfig) *Collector {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if config.CollectionInterval == 0 {
		config.CollectionInterval = 10 * time.Second
	}
	if config.AggregationInterval == 0 {
		config.AggregationInterval = 5 * time.Minute
	}
	if config.PruneInterval == 0 {
		config.PruneInterval = 1 * time.Hour
	}

	return &Collector{
		store:    store,
		config:   config,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// SetWSCounter injects the WebSocket connection counter.
func (c *Collector) SetWSCounter(counter WSConnectionCounter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wsCounter = counter
}

// SetDBPath sets the database file path for size calculation.
func (c *Collector) SetDBPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dbPath = path
}

// Start begins periodic metrics collection.
func (c *Collector) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopChan = make(chan struct{})
	c.mu.Unlock()

	go c.runLoop()
	c.logger.Info("server metrics collector started",
		"collection_interval", c.config.CollectionInterval,
		"aggregation_interval", c.config.AggregationInterval)
}

// Stop halts the collector.
func (c *Collector) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopChan)
	c.mu.Unlock()
	c.logger.Info("server metrics collector stopped")
}

// GetLatest returns the most recent snapshot without hitting the database.
func (c *Collector) GetLatest() *storage.ServerMetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestSnapshot
}

func (c *Collector) runLoop() {
	collectionTicker := time.NewTicker(c.config.CollectionInterval)
	aggregationTicker := time.NewTicker(c.config.AggregationInterval)
	pruneTicker := time.NewTicker(c.config.PruneInterval)

	defer collectionTicker.Stop()
	defer aggregationTicker.Stop()
	defer pruneTicker.Stop()

	// Collect immediately on start
	c.collect()

	for {
		select {
		case <-c.stopChan:
			return
		case <-collectionTicker.C:
			c.collect()
		case <-aggregationTicker.C:
			c.aggregate()
		case <-pruneTicker.C:
			c.prune()
		}
	}
}

func (c *Collector) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	snapshot := c.buildSnapshot(ctx)
	if snapshot == nil {
		return
	}

	// Cache for quick access
	c.mu.Lock()
	c.latestSnapshot = snapshot
	c.mu.Unlock()

	// Store in database
	if err := c.store.InsertServerMetrics(ctx, snapshot); err != nil {
		c.logger.Error("failed to store server metrics", "error", err)
	}
}

func (c *Collector) buildSnapshot(ctx context.Context) *storage.ServerMetricsSnapshot {
	snapshot := &storage.ServerMetricsSnapshot{
		Timestamp: time.Now().UTC(),
		Tier:      "raw",
	}

	// Collect fleet metrics
	c.collectFleetMetrics(ctx, &snapshot.Fleet)

	// Collect server metrics
	c.collectServerMetrics(ctx, &snapshot.Server)

	return snapshot
}

func (c *Collector) collectFleetMetrics(ctx context.Context, fleet *storage.FleetSnapshot) {
	// Get wsCounter for agent connection checks
	c.mu.RLock()
	wsCounter := c.wsCounter
	c.mu.RUnlock()

	// Count agents and their connection types
	agents, err := c.store.ListAgents(ctx)
	if err == nil {
		fleet.TotalAgents = len(agents)

		now := time.Now()
		for _, a := range agents {
			// Check agent status
			isActive := a.Status == "active" && now.Sub(a.LastSeen) < 15*time.Minute

			if !isActive {
				fleet.AgentsOffline++
			} else if wsCounter != nil && wsCounter.IsAgentConnected(a.AgentID) {
				fleet.AgentsWS++
			} else {
				fleet.AgentsHTTP++
			}
		}
	}

	// Count devices and their statuses
	devices, err := c.store.ListAllDevices(ctx)
	if err == nil {
		fleet.TotalDevices = len(devices)

		now := time.Now()
		for _, d := range devices {
			// Online/offline based on last seen
			if now.Sub(d.LastSeen) < 15*time.Minute {
				fleet.DevicesOnline++
			} else {
				fleet.DevicesOffline++
			}

			// Status classification
			hasError, hasWarning, hasJam := classifyStatusMessages(d.StatusMessages)
			if hasJam {
				fleet.DevicesJam++
				fleet.DevicesError++
			} else if hasError {
				fleet.DevicesError++
			} else if hasWarning {
				fleet.DevicesWarning++
			}
		}

		// Collect consumable/toner levels and page counts
		for _, d := range devices {
			metrics, err := c.store.GetLatestMetrics(ctx, d.Serial)
			if err != nil || metrics == nil {
				fleet.TonerUnknown++
				continue
			}

			// Accumulate page counts
			fleet.TotalPages += int64(metrics.PageCount)
			fleet.ColorPages += int64(metrics.ColorPages)
			fleet.MonoPages += int64(metrics.MonoPages)
			fleet.ScanCount += int64(metrics.ScanCount)

			// Classify toner levels
			minLevel := 100
			for _, levelVal := range metrics.TonerLevels {
				level := tonerLevelToInt(levelVal)
				if level >= 0 && level < minLevel {
					minLevel = level
				}
			}

			switch {
			case minLevel < 0:
				fleet.TonerUnknown++
			case minLevel < 10:
				fleet.TonerCritical++
			case minLevel < 25:
				fleet.TonerLow++
			case minLevel < 50:
				fleet.TonerMedium++
			default:
				fleet.TonerHigh++
			}
		}
	}
}

func (c *Collector) collectServerMetrics(ctx context.Context, server *storage.ServerSnapshot) {
	// Runtime metrics
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	server.Goroutines = runtime.NumGoroutine()
	server.HeapAllocMB = int(mem.HeapAlloc / (1024 * 1024))
	server.HeapSysMB = int(mem.HeapSys / (1024 * 1024))
	server.StackInUseMB = int(mem.StackInuse / (1024 * 1024))
	server.TotalAllocMB = int(mem.TotalAlloc / (1024 * 1024))
	server.SysMB = int(mem.Sys / (1024 * 1024))
	server.NumGC = mem.NumGC
	if len(mem.PauseNs) > 0 {
		server.GCPauseNs = mem.PauseNs[(mem.NumGC+255)%256]
	}

	// Database stats (includes size for PostgreSQL)
	dbStats, err := c.store.GetDatabaseStats(ctx)
	if err == nil && dbStats != nil {
		server.DBAgents = dbStats.Agents
		server.DBDevices = dbStats.Devices
		server.DBMetricsRows = dbStats.MetricsSnapshots
		server.DBSessions = dbStats.Sessions
		server.DBUsers = dbStats.Users
		server.DBAuditEntries = dbStats.AuditEntries
		server.DBCacheBytes = dbStats.ReleaseBytes
		// PostgreSQL returns size via GetDatabaseStats
		if dbStats.SizeBytes > 0 {
			server.DBSizeBytes = dbStats.SizeBytes
		}
	}

	// SQLite database file size (fallback for SQLite)
	c.mu.RLock()
	dbPath := c.dbPath
	wsCounter := c.wsCounter
	c.mu.RUnlock()

	if dbPath != "" && server.DBSizeBytes == 0 {
		if info, err := os.Stat(dbPath); err == nil {
			server.DBSizeBytes = info.Size()
		}
	}

	// WebSocket connections
	if wsCounter != nil {
		server.WSConnections = wsCounter.GetConnectionCount()
		server.WSAgents = wsCounter.GetAgentCount()
	}

	// Alert counts
	alerts, err := c.store.ListActiveAlerts(ctx, storage.AlertFilters{})
	if err == nil {
		server.DBActiveAlerts = int64(len(alerts))
	}
	// Total alerts would need a different query, but active is most useful
	server.DBAlerts = server.DBActiveAlerts
}

func (c *Collector) aggregate() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.store.AggregateServerMetrics(ctx); err != nil {
		c.logger.Error("failed to aggregate server metrics", "error", err)
	}
}

func (c *Collector) prune() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := c.store.PruneServerMetrics(ctx); err != nil {
		c.logger.Error("failed to prune server metrics", "error", err)
	}
}

// Helper functions

func classifyStatusMessages(messages []string) (hasError, hasWarning, hasJam bool) {
	for _, msg := range messages {
		msgLower := stringToLower(msg)
		if containsAny(msgLower, "jam", "jammed") {
			hasJam = true
		}
		if containsAny(msgLower, "error", "fault", "failure") {
			hasError = true
		}
		if containsAny(msgLower, "warning", "low", "empty") {
			hasWarning = true
		}
	}
	return
}

func tonerLevelToInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return int(i)
		}
	}
	return -1
}

func stringToLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(sub) == 0 {
			continue
		}
		for i := 0; i <= len(s)-len(sub); i++ {
			match := true
			for j := 0; j < len(sub); j++ {
				if s[i+j] != sub[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}
