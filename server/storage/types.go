package storage

import (
	"context"
	"time"
)

// Agent represents a registered PrintMaster agent
type Agent struct {
	ID              int64     `json:"id"`
	AgentID         string    `json:"agent_id"` // Unique agent identifier
	Hostname        string    `json:"hostname"`
	IP              string    `json:"ip"`
	Platform        string    `json:"platform"`         // windows, linux, darwin
	Version         string    `json:"version"`          // Agent version
	ProtocolVersion string    `json:"protocol_version"` // Protocol compatibility
	Token           string    `json:"token"`            // Bearer token for authentication
	RegisteredAt    time.Time `json:"registered_at"`
	LastSeen        time.Time `json:"last_seen"`
	Status          string    `json:"status"` // active, inactive, offline
}

// Device represents a printer device discovered by an agent
type Device struct {
	Serial          string                 `json:"serial"`
	AgentID         string                 `json:"agent_id"` // Which agent discovered this
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
	FirstSeen       time.Time              `json:"first_seen"`
	CreatedAt       time.Time              `json:"created_at"`
	DiscoveryMethod string                 `json:"discovery_method,omitempty"`
	AssetNumber     string                 `json:"asset_number,omitempty"`
	Location        string                 `json:"location,omitempty"`
	Description     string                 `json:"description,omitempty"`
	WebUIURL        string                 `json:"web_ui_url,omitempty"`
	RawData         map[string]interface{} `json:"raw_data,omitempty"`
}

// MetricsSnapshot represents device metrics at a point in time
type MetricsSnapshot struct {
	ID          int64                  `json:"id"`
	Serial      string                 `json:"serial"`
	AgentID     string                 `json:"agent_id"`
	Timestamp   time.Time              `json:"timestamp"`
	PageCount   int                    `json:"page_count,omitempty"`
	ColorPages  int                    `json:"color_pages,omitempty"`
	MonoPages   int                    `json:"mono_pages,omitempty"`
	ScanCount   int                    `json:"scan_count,omitempty"`
	TonerLevels map[string]interface{} `json:"toner_levels,omitempty"`
}

// AuditEntry represents an audit log entry for agent operations
type AuditEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"`
	Action    string    `json:"action"` // register, heartbeat, upload_devices, upload_metrics, etc.
	Details   string    `json:"details,omitempty"`
	IPAddress string    `json:"ip_address,omitempty"`
}

// Store defines the interface for server data storage
type Store interface {
	// Agent management
	RegisterAgent(ctx context.Context, agent *Agent) error
	GetAgent(ctx context.Context, agentID string) (*Agent, error)
	GetAgentByToken(ctx context.Context, token string) (*Agent, error)
	ListAgents(ctx context.Context) ([]*Agent, error)
	UpdateAgentHeartbeat(ctx context.Context, agentID string, status string) error

	// Device management
	UpsertDevice(ctx context.Context, device *Device) error
	GetDevice(ctx context.Context, serial string) (*Device, error)
	ListDevices(ctx context.Context, agentID string) ([]*Device, error)
	ListAllDevices(ctx context.Context) ([]*Device, error)

	// Metrics
	SaveMetrics(ctx context.Context, metrics *MetricsSnapshot) error
	GetLatestMetrics(ctx context.Context, serial string) (*MetricsSnapshot, error)
	GetMetricsHistory(ctx context.Context, serial string, since time.Time) ([]*MetricsSnapshot, error)

	// Audit logging
	SaveAuditEntry(ctx context.Context, entry *AuditEntry) error
	GetAuditLog(ctx context.Context, agentID string, since time.Time) ([]*AuditEntry, error)

	// Utility
	Close() error
}
