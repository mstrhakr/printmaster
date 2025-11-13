package tenancy

import "time"

// Tenant represents a customer/tenant
type Tenant struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// JoinToken represents a short-lived token used by agents to join a tenant
type JoinToken struct {
	Token     string    `json:"token"`
	TenantID  string    `json:"tenant_id"`
	ExpiresAt time.Time `json:"expires_at"`
	OneTime   bool      `json:"one_time"`
	CreatedAt time.Time `json:"created_at"`
}
