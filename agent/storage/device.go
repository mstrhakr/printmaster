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

// Device represents a printer device with all its properties
type Device struct {
	Serial       string   `json:"serial"`
	IP           string   `json:"ip"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	Hostname     string   `json:"hostname,omitempty"`
	Firmware     string   `json:"firmware,omitempty"`
	MACAddress   string   `json:"mac_address,omitempty"`
	SubnetMask   string   `json:"subnet_mask,omitempty"`
	Gateway      string   `json:"gateway,omitempty"`
	DNSServers   []string `json:"dns_servers,omitempty"`
	DHCPServer   string   `json:"dhcp_server,omitempty"`
	// These are time-series data that belong in metrics_history table
	Consumables     []string    `json:"consumables,omitempty"`
	StatusMessages  []string    `json:"status_messages,omitempty"`
	LastSeen        time.Time   `json:"last_seen"`
	CreatedAt       time.Time   `json:"created_at"`
	FirstSeen       time.Time   `json:"first_seen"`
	IsSaved         bool        `json:"is_saved"`
	Visible         bool        `json:"visible"` // false = hidden from UI (soft delete)
	DiscoveryMethod string      `json:"discovery_method,omitempty"`
	WalkFilename    string      `json:"walk_filename,omitempty"`
	LastScanID      int64       `json:"last_scan_id,omitempty"`  // FK to most recent scan_history entry
	AssetNumber     string      `json:"asset_number,omitempty"`  // User-defined asset tag/number
	Location        string      `json:"location,omitempty"`      // Physical location (room, building, etc)
	Description     string      `json:"description,omitempty"`   // Device description/notes (UUID, identifier, etc.)
	WebUIURL        string      `json:"web_ui_url,omitempty"`    // HTTP/HTTPS URL to device web interface
	LockedFields    []FieldLock `json:"locked_fields,omitempty"` // Fields that should not be auto-updated
	// RawData stores additional fields as JSON for extensibility
	RawData map[string]interface{} `json:"raw_data,omitempty"`
}

// DeviceFilter allows filtering devices by various criteria
type DeviceFilter struct {
	IsSaved       *bool      // nil = all, true = saved only, false = discovered only
	Visible       *bool      // nil = all, true = visible only, false = hidden only
	IP            string     // Filter by IP (exact match)
	Serial        string     // Filter by serial (exact match)
	Manufacturer  string     // Filter by manufacturer (case-insensitive contains)
	LastSeenAfter *time.Time // Only devices seen after this time
	Limit         int        // Max results (0 = no limit)
}
