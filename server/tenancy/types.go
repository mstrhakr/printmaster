package tenancy

import "time"

// Tenant represents a customer/tenant
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

// JoinToken represents a short-lived token used by agents to join a tenant
type JoinToken struct {
	Token     string    `json:"token"`
	TenantID  string    `json:"tenant_id"`
	ExpiresAt time.Time `json:"expires_at"`
	OneTime   bool      `json:"one_time"`
	CreatedAt time.Time `json:"created_at"`
}
