package storage

import (
	"context"
	commonstorage "printmaster/common/storage"
	"sort"
	"strings"
	"time"
)

// Role represents the authorization level granted to a user.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// NormalizeRole ensures any persisted or user-provided role maps to a known value.
func NormalizeRole(value string) Role {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(RoleAdmin):
		return RoleAdmin
	case string(RoleOperator):
		return RoleOperator
	case "user":
		// Legacy value from pre-RBAC builds maps to operator by default.
		return RoleOperator
	default:
		return RoleViewer
	}
}

// DefaultRoles returns a deterministic list of built-in roles ordered by privilege.
func DefaultRoles() []Role {
	return []Role{RoleAdmin, RoleOperator, RoleViewer}
}

// SortTenantIDs normalizes tenant slices for stable storage/JSON.
func SortTenantIDs(ids []string) []string {
	res := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		res = append(res, id)
	}
	sort.Strings(res)
	return res
}

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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	ContactName  string    `json:"contact_name,omitempty"`
	ContactEmail string    `json:"contact_email,omitempty"`
	ContactPhone string    `json:"contact_phone,omitempty"`
	BusinessUnit string    `json:"business_unit,omitempty"`
	BillingCode  string    `json:"billing_code,omitempty"`
	Address      string    `json:"address,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
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
	Role         Role      `json:"role"`
	TenantID     string    `json:"tenant_id,omitempty"`
	TenantIDs    []string  `json:"tenant_ids,omitempty"`
	Email        string    `json:"email,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Session represents a short lived session/token for UI auth
type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Username  string    `json:"username,omitempty"`
}

// OIDCProvider represents a configured OIDC identity provider
type OIDCProvider struct {
	ID           int64     `json:"id"`
	Slug         string    `json:"slug"`
	DisplayName  string    `json:"display_name"`
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	Scopes       []string  `json:"scopes"`
	Icon         string    `json:"icon,omitempty"`
	ButtonText   string    `json:"button_text,omitempty"`
	ButtonStyle  string    `json:"button_style,omitempty"`
	AutoLogin    bool      `json:"auto_login"`
	TenantID     string    `json:"tenant_id,omitempty"`
	DefaultRole  Role      `json:"default_role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OIDCSession represents a pending login attempt (state/nonce)
type OIDCSession struct {
	ID           string    `json:"id"`
	ProviderSlug string    `json:"provider_slug"`
	TenantID     string    `json:"tenant_id,omitempty"`
	Nonce        string    `json:"nonce"`
	State        string    `json:"state"`
	RedirectURL  string    `json:"redirect_url"`
	CreatedAt    time.Time `json:"created_at"`
}

// OIDCLink ties an OIDC subject to a local user
type OIDCLink struct {
	ID           int64     `json:"id"`
	ProviderSlug string    `json:"provider_slug"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email,omitempty"`
	UserID       int64     `json:"user_id"`
	CreatedAt    time.Time `json:"created_at"`
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
	UpdateTenant(ctx context.Context, tenant *Tenant) error
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
	// ListSessions returns all sessions (admin) including username when available
	ListSessions(ctx context.Context) ([]*Session, error)
	// DeleteSessionByHash deletes a session by its stored token hash (DB-side key)
	DeleteSessionByHash(ctx context.Context, tokenHash string) error
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

	// OIDC / SSO
	CreateOIDCProvider(ctx context.Context, provider *OIDCProvider) error
	UpdateOIDCProvider(ctx context.Context, provider *OIDCProvider) error
	DeleteOIDCProvider(ctx context.Context, slug string) error
	GetOIDCProvider(ctx context.Context, slug string) (*OIDCProvider, error)
	ListOIDCProviders(ctx context.Context, tenantID string) ([]*OIDCProvider, error)
	CreateOIDCSession(ctx context.Context, sess *OIDCSession) error
	GetOIDCSession(ctx context.Context, id string) (*OIDCSession, error)
	DeleteOIDCSession(ctx context.Context, id string) error
	CreateOIDCLink(ctx context.Context, link *OIDCLink) error
	GetOIDCLink(ctx context.Context, providerSlug, subject string) (*OIDCLink, error)
	ListOIDCLinksForUser(ctx context.Context, userID int64) ([]*OIDCLink, error)
	DeleteOIDCLink(ctx context.Context, providerSlug, subject string) error
}
