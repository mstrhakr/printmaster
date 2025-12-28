// Package handlers provides HTTP API handlers for the PrintMaster server.
// This package follows the same dependency injection pattern as alerts, settings,
// and updatepolicy packages.
package handlers

import (
	"net/http"

	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

// APIOptions provides cross-cutting infrastructure for the HTTP layer.
type APIOptions struct {
	// AuthMiddleware wraps handlers requiring authentication
	AuthMiddleware func(http.HandlerFunc) http.HandlerFunc

	// AgentAuthMiddleware wraps handlers requiring agent token authentication
	AgentAuthMiddleware func(http.HandlerFunc) http.HandlerFunc

	// Authorizer checks if the request has permission for the given action
	Authorizer func(*http.Request, authz.Action, authz.ResourceRef) error

	// ActorResolver returns the username of the authenticated user
	ActorResolver func(*http.Request) string

	// AuditLogger logs audit entries
	AuditLogger func(*http.Request, *storage.AuditEntry)

	// PrincipalGetter returns the authenticated principal from the request
	PrincipalGetter func(*http.Request) Principal

	// CredentialsKey is the encryption key for device credentials
	CredentialsKey []byte
}

// Principal represents the authenticated user with cached authorization helpers.
type Principal interface {
	IsAdmin() bool
	HasRole(min storage.Role) bool
	AllowedTenantIDs() []string
	CanAccessTenant(tenantID string) bool
	GetUser() *storage.User
	GetRole() storage.Role
}

// SSEBroadcaster broadcasts server-sent events to connected clients.
type SSEBroadcaster interface {
	Broadcast(event SSEEvent)
}

// SSEEvent represents a server-sent event.
type SSEEvent struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// WebSocketAgentChecker checks if an agent is connected via WebSocket.
type WebSocketAgentChecker interface {
	IsAgentConnected(agentID string) bool
	SendCommand(agentID string, cmd interface{}) error
}

// Logger provides logging capabilities.
type Logger interface {
	Debug(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}
