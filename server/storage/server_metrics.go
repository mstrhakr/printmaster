// Package storage provides server metrics time-series storage for Netdata-style dashboards.
package storage

import (
	"context"
	"encoding/json"
	"time"
)

// ServerMetricsSnapshot represents a single point-in-time collection of server and fleet metrics.
// These are collected periodically (every 10-30 seconds) and stored for time-series visualization.
type ServerMetricsSnapshot struct {
	ID        int64     `json:"id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Tier      string    `json:"tier"` // "raw", "hourly", "daily"

	// Fleet metrics - aggregated across all devices
	Fleet FleetSnapshot `json:"fleet"`

	// Server runtime metrics
	Server ServerSnapshot `json:"server"`
}

// FleetSnapshot captures fleet-wide metrics at a point in time.
type FleetSnapshot struct {
	// Counts
	TotalAgents  int `json:"total_agents"`
	TotalDevices int `json:"total_devices"`

	// Agent connection breakdown
	AgentsWS      int `json:"agents_ws"`      // Agents connected via WebSocket
	AgentsHTTP    int `json:"agents_http"`    // Agents using HTTP polling (fallback)
	AgentsOffline int `json:"agents_offline"` // Agents not connected

	// Page counters (cumulative totals across fleet)
	TotalPages int64 `json:"total_pages"`
	ColorPages int64 `json:"color_pages"`
	MonoPages  int64 `json:"mono_pages"`
	ScanCount  int64 `json:"scan_count"`

	// Consumable tiers (device counts per tier)
	TonerHigh     int `json:"toner_high"`
	TonerMedium   int `json:"toner_medium"`
	TonerLow      int `json:"toner_low"`
	TonerCritical int `json:"toner_critical"`
	TonerUnknown  int `json:"toner_unknown"`

	// Status counts
	DevicesOnline  int `json:"devices_online"`
	DevicesOffline int `json:"devices_offline"`
	DevicesError   int `json:"devices_error"`
	DevicesWarning int `json:"devices_warning"`
	DevicesJam     int `json:"devices_jam"`
}

// ServerSnapshot captures server runtime metrics at a point in time.
type ServerSnapshot struct {
	// Runtime
	Goroutines   int    `json:"goroutines"`
	HeapAllocMB  int    `json:"heap_alloc_mb"`
	HeapSysMB    int    `json:"heap_sys_mb"`
	StackInUseMB int    `json:"stack_in_use_mb"`
	TotalAllocMB int    `json:"total_alloc_mb"`
	SysMB        int    `json:"sys_mb"`
	NumGC        uint32 `json:"num_gc"`
	GCPauseNs    uint64 `json:"gc_pause_ns"` // Last GC pause

	// Database
	DBSizeBytes    int64 `json:"db_size_bytes"`
	DBAgents       int64 `json:"db_agents"`
	DBDevices      int64 `json:"db_devices"`
	DBMetricsRows  int64 `json:"db_metrics_rows"`
	DBSessions     int64 `json:"db_sessions"`
	DBUsers        int64 `json:"db_users"`
	DBAuditEntries int64 `json:"db_audit_entries"`
	DBAlerts       int64 `json:"db_alerts"`
	DBActiveAlerts int64 `json:"db_active_alerts"`
	DBCacheBytes   int64 `json:"db_cache_bytes"` // Release artifacts + installer bundles

	// WebSocket
	WSConnections int `json:"ws_connections"`
	WSAgents      int `json:"ws_agents"` // Active agent WS connections
}

// ServerMetricsTimeSeries represents a time-series of server metrics for charting.
type ServerMetricsTimeSeries struct {
	StartTime   time.Time                    `json:"start_time"`
	EndTime     time.Time                    `json:"end_time"`
	Resolution  string                       `json:"resolution"` // "raw", "hourly", "daily"
	PointCount  int                          `json:"point_count"`
	Snapshots   []ServerMetricsSnapshot      `json:"snapshots,omitempty"`
	ChartSeries map[string][]TimeSeriesPoint `json:"chart_series,omitempty"` // Pre-computed series for specific charts
}

// TimeSeriesPoint is a single data point for charting.
type TimeSeriesPoint struct {
	Timestamp int64   `json:"t"` // Unix milliseconds for compact JSON
	Value     float64 `json:"v"`
}

// ServerMetricsQuery defines parameters for querying server metrics time-series.
type ServerMetricsQuery struct {
	StartTime  time.Time
	EndTime    time.Time
	Resolution string   // "auto", "raw", "hourly", "daily" - auto picks best resolution
	Series     []string // Which series to include (empty = all)
	MaxPoints  int      // Maximum points to return (for decimation)
}

// Available metric series names for charting
const (
	SeriesGoroutines    = "goroutines"
	SeriesHeapAlloc     = "heap_alloc"
	SeriesTotalAlloc    = "total_alloc"
	SeriesDBSize        = "db_size"
	SeriesWSConnections = "ws_connections"
	SeriesTotalPages    = "total_pages"
	SeriesColorPages    = "color_pages"
	SeriesMonoPages     = "mono_pages"
	SeriesScanCount     = "scan_count"
	SeriesTonerHigh     = "toner_high"
	SeriesTonerMedium   = "toner_medium"
	SeriesTonerLow      = "toner_low"
	SeriesTonerCritical = "toner_critical"
	SeriesDevicesOnline = "devices_online"
	SeriesDevicesError  = "devices_error"
	SeriesAgents        = "agents"
	SeriesDevices       = "devices"
	SeriesAgentsWS      = "agents_ws"
	SeriesAgentsHTTP    = "agents_http"
	SeriesAgentsOffline = "agents_offline"
)

// AllServerMetricsSeries returns all available series names.
func AllServerMetricsSeries() []string {
	return []string{
		SeriesGoroutines,
		SeriesHeapAlloc,
		SeriesTotalAlloc,
		SeriesDBSize,
		SeriesWSConnections,
		SeriesTotalPages,
		SeriesColorPages,
		SeriesMonoPages,
		SeriesScanCount,
		SeriesTonerHigh,
		SeriesTonerMedium,
		SeriesTonerLow,
		SeriesTonerCritical,
		SeriesDevicesOnline,
		SeriesDevicesError,
		SeriesAgents,
		SeriesDevices,
		SeriesAgentsWS,
		SeriesAgentsHTTP,
		SeriesAgentsOffline,
	}
}

// ServerMetricsStore defines the interface for server metrics persistence.
type ServerMetricsStore interface {
	// InsertServerMetrics stores a new metrics snapshot.
	InsertServerMetrics(ctx context.Context, snapshot *ServerMetricsSnapshot) error

	// GetServerMetrics retrieves time-series data based on query parameters.
	GetServerMetrics(ctx context.Context, query ServerMetricsQuery) (*ServerMetricsTimeSeries, error)

	// GetLatestServerMetrics returns the most recent raw snapshot.
	GetLatestServerMetrics(ctx context.Context) (*ServerMetricsSnapshot, error)

	// AggregateServerMetrics computes hourly/daily aggregates from raw data.
	// Called periodically to downsample old raw data.
	AggregateServerMetrics(ctx context.Context) error

	// PruneServerMetrics removes old data based on retention policy.
	// Default: raw=7d, hourly=90d, daily=365d
	PruneServerMetrics(ctx context.Context) error
}

// ToJSON serializes the snapshot for storage.
func (s *ServerMetricsSnapshot) ToJSON() (string, error) {
	data, err := json.Marshal(s)
	return string(data), err
}

// FromJSON deserializes a snapshot from storage.
func (s *ServerMetricsSnapshot) FromJSON(data string) error {
	return json.Unmarshal([]byte(data), s)
}

// ExtractSeries extracts a specific series from a snapshot.
func (s *ServerMetricsSnapshot) ExtractSeries(seriesName string) float64 {
	switch seriesName {
	case SeriesGoroutines:
		return float64(s.Server.Goroutines)
	case SeriesHeapAlloc:
		return float64(s.Server.HeapAllocMB)
	case SeriesTotalAlloc:
		return float64(s.Server.TotalAllocMB)
	case SeriesDBSize:
		return float64(s.Server.DBSizeBytes)
	case SeriesWSConnections:
		return float64(s.Server.WSConnections)
	case SeriesTotalPages:
		return float64(s.Fleet.TotalPages)
	case SeriesColorPages:
		return float64(s.Fleet.ColorPages)
	case SeriesMonoPages:
		return float64(s.Fleet.MonoPages)
	case SeriesScanCount:
		return float64(s.Fleet.ScanCount)
	case SeriesTonerHigh:
		return float64(s.Fleet.TonerHigh)
	case SeriesTonerMedium:
		return float64(s.Fleet.TonerMedium)
	case SeriesTonerLow:
		return float64(s.Fleet.TonerLow)
	case SeriesTonerCritical:
		return float64(s.Fleet.TonerCritical)
	case SeriesDevicesOnline:
		return float64(s.Fleet.DevicesOnline)
	case SeriesDevicesError:
		return float64(s.Fleet.DevicesError)
	case SeriesAgents:
		return float64(s.Fleet.TotalAgents)
	case SeriesDevices:
		return float64(s.Fleet.TotalDevices)
	case SeriesAgentsWS:
		return float64(s.Fleet.AgentsWS)
	case SeriesAgentsHTTP:
		return float64(s.Fleet.AgentsHTTP)
	case SeriesAgentsOffline:
		return float64(s.Fleet.AgentsOffline)
	default:
		return 0
	}
}

// BuildChartSeries converts snapshots into chart-ready series data.
func BuildChartSeries(snapshots []ServerMetricsSnapshot, seriesNames []string) map[string][]TimeSeriesPoint {
	if len(seriesNames) == 0 {
		seriesNames = AllServerMetricsSeries()
	}

	result := make(map[string][]TimeSeriesPoint, len(seriesNames))
	for _, name := range seriesNames {
		points := make([]TimeSeriesPoint, 0, len(snapshots))
		for _, snap := range snapshots {
			points = append(points, TimeSeriesPoint{
				Timestamp: snap.Timestamp.UnixMilli(),
				Value:     snap.ExtractSeries(name),
			})
		}
		result[name] = points
	}
	return result
}

// PickResolution selects the appropriate data tier based on time range.
func PickResolution(startTime, endTime time.Time) string {
	duration := endTime.Sub(startTime)
	switch {
	case duration <= 6*time.Hour:
		return "raw" // Show raw 10-second data for short ranges
	case duration <= 7*24*time.Hour:
		return "hourly" // Hourly for up to a week
	default:
		return "daily" // Daily for longer ranges
	}
}
