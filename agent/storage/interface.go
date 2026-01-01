package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a device doesn't exist
	ErrNotFound = errors.New("device not found")
	// ErrDuplicate is returned when trying to create a device that already exists
	ErrDuplicate = errors.New("device already exists")
	// ErrInvalidSerial is returned when serial is empty or invalid
	ErrInvalidSerial = errors.New("invalid or empty serial")
)

// ScanSnapshot represents a point-in-time snapshot of a device scan.
// Each scan creates a new history entry, allowing for change tracking over time.
// Note: Metrics data (page counts, toner levels) are stored separately in the tiered metrics system.
type ScanSnapshot struct {
	ID              int64           `json:"id"`
	Serial          string          `json:"serial"`
	CreatedAt       time.Time       `json:"created_at"`
	IP              string          `json:"ip"`
	Hostname        string          `json:"hostname"`
	Firmware        string          `json:"firmware"`
	Consumables     []string        `json:"consumables,omitempty"`
	StatusMessages  []string        `json:"status_messages,omitempty"`
	DiscoveryMethod string          `json:"discovery_method"`
	WalkFilename    string          `json:"walk_filename,omitempty"`
	RawData         json.RawMessage `json:"raw_data,omitempty"` // Full snapshot including extended fields
}

// DeviceStore is the interface for device storage operations.
// Implementations can be SQLite (disk-based), in-memory, or remote server-based.
type DeviceStore interface {
	// Create adds a new device. Returns ErrDuplicate if serial already exists.
	Create(ctx context.Context, device *Device) error

	// Get retrieves a device by serial. Returns ErrNotFound if not found.
	Get(ctx context.Context, serial string) (*Device, error)

	// Update modifies an existing device. Returns ErrNotFound if not found.
	Update(ctx context.Context, device *Device) error

	// Upsert creates or updates a device (insert or update)
	Upsert(ctx context.Context, device *Device) error

	// StoreDiscoveryAtomic persists a discovery update (device upsert + scan history + metrics snapshot)
	// as a single atomic unit.
	StoreDiscoveryAtomic(ctx context.Context, device *Device, scan *ScanSnapshot, metrics *MetricsSnapshot) error

	// Delete removes a device by serial. Returns ErrNotFound if not found.
	Delete(ctx context.Context, serial string) error

	// List returns devices matching the filter criteria
	List(ctx context.Context, filter DeviceFilter) ([]*Device, error)

	// MarkSaved sets is_saved=true for a device
	MarkSaved(ctx context.Context, serial string) error

	// MarkAllSaved sets is_saved=true for all visible, unsaved devices
	MarkAllSaved(ctx context.Context) (int, error)

	// MarkDiscovered sets is_saved=false for a device
	MarkDiscovered(ctx context.Context, serial string) error

	// DeleteAll removes all devices matching the filter
	DeleteAll(ctx context.Context, filter DeviceFilter) (int, error)

	// Scan History Management

	// AddScanHistory records a new scan snapshot for a device
	AddScanHistory(ctx context.Context, scan *ScanSnapshot) error

	// GetScanHistory returns the last N scan snapshots for a device, newest first
	GetScanHistory(ctx context.Context, serial string, limit int) ([]*ScanSnapshot, error)

	// DeleteOldScans removes scan history older than the given timestamp
	DeleteOldScans(ctx context.Context, olderThan int64) (int, error)

	// Visibility Management (for hide/show instead of hard delete)

	// HideDiscovered sets visible=false for all devices where is_saved=false
	HideDiscovered(ctx context.Context) (int, error)

	// ShowAll sets visible=true for all devices
	ShowAll(ctx context.Context) (int, error)

	// DeleteOldHiddenDevices removes devices that are hidden and older than timestamp
	DeleteOldHiddenDevices(ctx context.Context, olderThan int64) (int, error)

	// Close closes the storage connection (for cleanup)
	Close() error

	// Stats returns storage statistics (device count, etc)
	Stats(ctx context.Context) (map[string]interface{}, error)

	// Metrics History Management

	// SaveMetricsSnapshot stores a metrics snapshot for a device
	SaveMetricsSnapshot(ctx context.Context, snapshot *MetricsSnapshot) error

	// GetMetricsHistory retrieves metrics history for a device within a time range
	GetMetricsHistory(ctx context.Context, serial string, since time.Time, until time.Time) ([]*MetricsSnapshot, error)

	// GetLatestMetrics retrieves the most recent metrics snapshot for a device
	GetLatestMetrics(ctx context.Context, serial string) (*MetricsSnapshot, error)

	// DeleteOldMetrics removes metrics history older than the given timestamp
	DeleteOldMetrics(ctx context.Context, olderThan time.Time) (int, error)

	// Tiered Metrics Downsampling (Netdata-style)

	// PerformFullDownsampling runs all downsampling operations: raw→hourly, hourly→daily, daily→monthly, cleanup
	PerformFullDownsampling(ctx context.Context) error

	// DownsampleRawToHourly aggregates raw 5-minute metrics into hourly buckets
	DownsampleRawToHourly(ctx context.Context, olderThan time.Time) (int, error)

	// DownsampleHourlyToDaily aggregates hourly metrics into daily buckets
	DownsampleHourlyToDaily(ctx context.Context, olderThan time.Time) (int, error)

	// DownsampleDailyToMonthly aggregates daily metrics into monthly buckets
	DownsampleDailyToMonthly(ctx context.Context, olderThan time.Time) (int, error)

	// CleanupOldTieredMetrics removes metrics from all tiers based on retention policies
	CleanupOldTieredMetrics(ctx context.Context, rawRetentionDays, hourlyRetentionDays, dailyRetentionDays int) (map[string]int, error)

	// GetTieredMetricsHistory retrieves metrics from appropriate tiers based on time range
	GetTieredMetricsHistory(ctx context.Context, serial string, since time.Time, until time.Time) ([]*MetricsSnapshot, error)

	// DeleteMetricByID removes a single metrics row by id from a specified tier/table.
	// If tier is empty, implementations should attempt to find and delete the id from known metric tables.
	DeleteMetricByID(ctx context.Context, tier string, id int64) error

	// Page Count Audit Trail

	// AddPageCountAudit records a page count change in the audit trail
	AddPageCountAudit(ctx context.Context, audit *PageCountAudit) error

	// GetPageCountAudit retrieves page count audit history for a device
	GetPageCountAudit(ctx context.Context, serial string, limit int) ([]*PageCountAudit, error)

	// GetPageCountAuditSince retrieves audit entries after a specific time
	GetPageCountAuditSince(ctx context.Context, serial string, since time.Time) ([]*PageCountAudit, error)

	// DeleteOldPageCountAudit removes audit entries older than the specified time
	DeleteOldPageCountAudit(ctx context.Context, olderThan time.Time) (int, error)

	// SetInitialPageCount sets the initial page count for a device and records it in the audit trail
	SetInitialPageCount(ctx context.Context, serial string, initialCount int, changedBy string, reason string) error

	// GetPageCountUsage calculates the page count usage since the initial baseline
	GetPageCountUsage(ctx context.Context, serial string) (usage int, initial int, current int, err error)
}
