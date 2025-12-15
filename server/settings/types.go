package settings

import (
	pmsettings "printmaster/common/settings"
	"time"
)

// Snapshot represents a resolved settings payload and its metadata.
type Snapshot struct {
	SchemaVersion   string              `json:"schema_version"`
	Settings        pmsettings.Settings `json:"settings"`
	ManagedSections []string            `json:"managed_sections,omitempty"` // e.g. ["discovery", "snmp", "features"]
	UpdatedAt       time.Time           `json:"updated_at"`
	UpdatedBy       string              `json:"updated_by,omitempty"`
}

// TenantSnapshot extends Snapshot with tenant-specific override metadata.
type TenantSnapshot struct {
	TenantID string `json:"tenant_id"`
	Snapshot
	Overrides          map[string]interface{} `json:"overrides"`
	OverridePaths      []string               `json:"override_paths"`
	EnforcedSections   []string               `json:"enforced_sections,omitempty"`
	OverridesUpdatedAt time.Time              `json:"overrides_updated_at,omitempty"`
	OverridesUpdatedBy string                 `json:"overrides_updated_by,omitempty"`
}

// AgentSettingsSnapshot extends Snapshot with agent-specific override metadata.
type AgentSettingsSnapshot struct {
	AgentID  string `json:"agent_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Snapshot
	Overrides          map[string]interface{} `json:"overrides"`
	OverridePaths      []string               `json:"override_paths"`
	EnforcedSections   []string               `json:"enforced_sections,omitempty"`
	OverridesUpdatedAt time.Time              `json:"overrides_updated_at,omitempty"`
	OverridesUpdatedBy string                 `json:"overrides_updated_by,omitempty"`
}
