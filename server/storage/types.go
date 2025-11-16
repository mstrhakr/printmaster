package storage

import (
	"context"
	commonstorage "printmaster/common/storage"
	"time"
)

// Agent represents a registered PrintMaster agent
type Agent struct {
	ID              int64     `json:"id"`
	AgentID         string    `json:"agent_id"` // Stable UUID identifier
	Name            string    `json:"name"`     // User-friendly name (defaults to hostname)
	Hostname        string    `json:"hostname"`
	IP              string    `json:"ip"`
	Platform        string    `json:"platform"`         // windows, linux, darwin
	Version         string    `json:"version"`          // Agent version
	ProtocolVersion string    `json:"protocol_version"` // Protocol compatibility
	Token           string    `json:"token"`            // Bearer token for authentication
	RegisteredAt    time.Time `json:"registered_at"`
	LastSeen        time.Time `json:"last_seen"`
	Status          string    `json:"status"` // active, inactive, offline

	// Additional metadata
	OSVersion       string    `json:"os_version,omitempty"`        // Detailed OS version
	GoVersion       string    `json:"go_version,omitempty"`        // Go runtime version
	Architecture    string    `json:"architecture,omitempty"`      // amd64, arm64, etc.
	NumCPU          int       `json:"num_cpu,omitempty"`           // Number of CPUs
	TotalMemoryMB   int64     `json:"total_memory_mb,omitempty"`   // Total system memory
	BuildType       string    `json:"build_type,omitempty"`        // dev, release
	GitCommit       string    `json:"git_commit,omitempty"`        // Git commit hash
	LastHeartbeat   time.Time `json:"last_heartbeat,omitempty"`    // Last heartbeat time
	DeviceCount     int       `json:"device_count,omitempty"`      // Number of devices managed
	LastDeviceSync  time.Time `json:"last_device_sync,omitempty"`  // Last device upload
	LastMetricsSync time.Time `json:"last_metrics_sync,omitempty"` // Last metrics upload
	TenantID        string    `json:"tenant_id,omitempty"`
}

// Device represents a printer device discovered by an agent (extends common Device)
type Device struct {
	commonstorage.Device // Embed common fields

	AgentID string `json:"agent_id"` // Which agent discovered this (server-specific field)
}

// MetricsSnapshot represents device metrics at a point in time (extends common MetricsSnapshot)
type MetricsSnapshot struct {
	commonstorage.MetricsSnapshot // Embed common fields

	AgentID string `json:"agent_id"` // Which agent reported this (server-specific field)
}

// Tenant represents a customer/tenant stored in server DB
type Tenant struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// JoinToken represents an opaque join token record stored in DB (token value not stored raw)
type JoinToken struct {
	ID        string    `json:"id"`
	TokenHash string    `json:"token_hash"`
	TenantID  string    `json:"tenant_id"`
	ExpiresAt time.Time `json:"expires_at"`
	OneTime   bool      `json:"one_time"`
	CreatedAt time.Time `json:"created_at"`
	UsedAt    time.Time `json:"used_at,omitempty"`
	Revoked   bool      `json:"revoked"`
}

// User represents a local user account for the server UI/API
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"` // admin, user
	TenantID     string    `json:"tenant_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session represents a short lived session/token for UI auth
type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditEntry represents an audit log entry for agent operations
type AuditEntry struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"` // Agent ID or "UNKNOWN" for failed auth
	Action    string    `json:"action"`   // register, heartbeat, upload_devices, upload_metrics, auth_blocked, auth_blocked_websocket
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
	// UpdateAgentName updates the user-friendly name for an agent
	UpdateAgentName(ctx context.Context, agentID string, name string) error
	DeleteAgent(ctx context.Context, agentID string) error

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

	// Tenancy
	CreateTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, id string) (*Tenant, error)
	ListTenants(ctx context.Context) ([]*Tenant, error)

	// Join tokens (opaque tokens): CreateJoinToken returns the created JoinToken
	// and the raw token string that should be returned to the caller (raw token
	// is not persisted in plain form).
	CreateJoinToken(ctx context.Context, tenantID string, ttlMinutes int, oneTime bool) (*JoinToken, string, error)
	ValidateJoinToken(ctx context.Context, token string) (*JoinToken, error)

	// Admin: list and revoke join tokens
	ListJoinTokens(ctx context.Context, tenantID string) ([]*JoinToken, error)
	RevokeJoinToken(ctx context.Context, id string) error

	// User & session management (local login)
	CreateUser(ctx context.Context, user *User, rawPassword string) error
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	AuthenticateUser(ctx context.Context, username, rawPassword string) (*User, error)

	// Sessions
	CreateSession(ctx context.Context, userID int64, ttlMinutes int) (*Session, error)
	GetSessionByToken(ctx context.Context, token string) (*Session, error)
	DeleteSession(ctx context.Context, token string) error
	GetUserByID(ctx context.Context, id int64) (*User, error)
	// ListUsers returns all local users (admin UI)
	ListUsers(ctx context.Context) ([]*User, error)
	DeleteUser(ctx context.Context, id int64) error
	UpdateUser(ctx context.Context, user *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)

	// Password reset tokens (opaque token stored hashed)
	CreatePasswordResetToken(ctx context.Context, userID int64, ttlMinutes int) (string, error)
	ValidatePasswordResetToken(ctx context.Context, token string) (int64, error)
	DeletePasswordResetToken(ctx context.Context, token string) error
	UpdateUserPassword(ctx context.Context, userID int64, rawPassword string) error
}
