// Package storage provides shared data structures for PrintMaster agent and server
package storage

import (
	"time"
)

// FieldLock represents a locked field that should not be auto-updated from scans
type FieldLock struct {
	Field    string    `json:"field"`               // Field name (e.g., "model", "hostname", "ip")
	Reason   string    `json:"reason,omitempty"`    // Why it was locked: "manually_entered", "user_locked", "user_preference"
	LockedAt time.Time `json:"locked_at"`           // When the field was locked
	LockedBy string    `json:"locked_by,omitempty"` // User/system that locked it (for future multi-user support)
}

// PaperTray represents the status of a single paper input tray
type PaperTray struct {
	Index        int    `json:"index"`                   // Tray index (1, 2, 3...)
	Name         string `json:"name,omitempty"`          // Tray name ("Tray 1", "Manual Feed")
	MediaType    string `json:"media_type,omitempty"`    // Paper type ("Letter", "A4", "Legal")
	CurrentLevel int    `json:"current_level"`           // Current sheets (-3=someRemaining, -2=unknown, -1=unavailable, 0+=actual)
	MaxCapacity  int    `json:"max_capacity"`            // Max capacity (-2=unknown, -1=unlimited, 0+=actual)
	LevelPercent int    `json:"level_percent,omitempty"` // Calculated percentage (0-100, -1 if unknown)
	Status       string `json:"status,omitempty"`        // "ok", "low", "empty", "unknown"
}

// Device represents a printer device with all its properties (base struct shared by agent and server)
type Device struct {
	Serial          string                 `json:"serial"`
	IP              string                 `json:"ip"`
	Manufacturer    string                 `json:"manufacturer,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Hostname        string                 `json:"hostname,omitempty"`
	Firmware        string                 `json:"firmware,omitempty"`
	MACAddress      string                 `json:"mac_address,omitempty"`
	SubnetMask      string                 `json:"subnet_mask,omitempty"`
	Gateway         string                 `json:"gateway,omitempty"`
	Consumables     []string               `json:"consumables,omitempty"`
	StatusMessages  []string               `json:"status_messages,omitempty"`
	LastSeen        time.Time              `json:"last_seen"`
	CreatedAt       time.Time              `json:"created_at"`
	FirstSeen       time.Time              `json:"first_seen"`
	DiscoveryMethod string                 `json:"discovery_method,omitempty"`
	AssetNumber     string                 `json:"asset_number,omitempty"` // User-defined asset tag/number
	Location        string                 `json:"location,omitempty"`     // Physical location (room, building, etc)
	Description     string                 `json:"description,omitempty"`  // Device description/notes
	WebUIURL        string                 `json:"web_ui_url,omitempty"`   // HTTP/HTTPS URL to device web interface
	RawData         map[string]interface{} `json:"raw_data,omitempty"`     // Additional fields as JSON for extensibility
}

// MetricsSnapshot represents a point-in-time snapshot of device metrics (base struct)
type MetricsSnapshot struct {
	ID          int64                  `json:"id"`
	Serial      string                 `json:"serial"`
	Timestamp   time.Time              `json:"timestamp"`
	PageCount   int                    `json:"page_count,omitempty"`
	ColorPages  int                    `json:"color_pages,omitempty"`
	MonoPages   int                    `json:"mono_pages,omitempty"`
	ScanCount   int                    `json:"scan_count,omitempty"`
	TonerLevels map[string]interface{} `json:"toner_levels,omitempty"`
	PaperTrays  []PaperTray            `json:"paper_trays,omitempty"` // Current paper tray status
}

// DeviceFilter allows filtering devices by various criteria
type DeviceFilter struct {
	IsSaved       *bool      // nil = all, true = saved only, false = discovered only (agent-specific)
	Visible       *bool      // nil = all, true = visible only, false = hidden only (agent-specific)
	IP            string     // Filter by IP (exact match)
	Serial        string     // Filter by serial (exact match)
	Manufacturer  string     // Filter by manufacturer (case-insensitive contains)
	LastSeenAfter *time.Time // Only devices seen after this time
	Limit         int        // Max results (0 = no limit)
}
