package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	pmsettings "printmaster/common/settings"
)

// AgentSnapshot captures the subset of settings sent to agents plus a change token.
type AgentSnapshot struct {
	Version         string              `json:"version"`
	SchemaVersion   string              `json:"schema_version"`
	UpdatedAt       time.Time           `json:"updated_at"`
	Settings        pmsettings.Settings `json:"settings"`
	ManagedSections []string            `json:"managed_sections,omitempty"` // e.g. ["discovery", "snmp", "features"]
}

// BuildAgentSnapshot resolves the appropriate settings for an agent and rewrites the
// payload so agent-local fields are reset to defaults. Tenants receive their overrides,
// while standalone agents fall back to the global snapshot.
func BuildAgentSnapshot(ctx context.Context, resolver *Resolver, tenantID string, agentID string) (AgentSnapshot, error) {
	if resolver == nil {
		return AgentSnapshot{}, fmt.Errorf("resolver required")
	}
	var (
		snapshot Snapshot
		err      error
	)
	agentID = strings.TrimSpace(agentID)
	if agentID != "" {
		if agentSnap, aerr := resolver.ResolveForAgent(ctx, agentID); aerr == nil {
			snapshot = agentSnap.Snapshot
		} else {
			// Fall back to the tenant/global resolution path (e.g., during in-memory join-token flows)
			if strings.TrimSpace(tenantID) == "" {
				snapshot, err = resolver.ResolveGlobal(ctx)
			} else {
				var tenantSnap TenantSnapshot
				tenantSnap, err = resolver.ResolveForTenant(ctx, tenantID)
				snapshot = tenantSnap.Snapshot
			}
		}
	} else if strings.TrimSpace(tenantID) == "" {
		snapshot, err = resolver.ResolveGlobal(ctx)
	} else {
		var tenantSnap TenantSnapshot
		tenantSnap, err = resolver.ResolveForTenant(ctx, tenantID)
		snapshot = tenantSnap.Snapshot
	}
	if err != nil {
		return AgentSnapshot{}, err
	}
	return agentSnapshotFromSnapshot(snapshot)
}

func agentSnapshotFromSnapshot(snapshot Snapshot) (AgentSnapshot, error) {
	settingsCopy := snapshot.Settings
	pmsettings.Sanitize(&settingsCopy)
	pmsettings.StripAgentLocalFields(&settingsCopy)
	schemaVersion := strings.TrimSpace(snapshot.SchemaVersion)
	if schemaVersion == "" {
		schemaVersion = pmsettings.SchemaVersion
	}
	version, err := pmsettings.ComputeSettingsVersion(schemaVersion, snapshot.UpdatedAt, settingsCopy)
	if err != nil {
		return AgentSnapshot{}, err
	}
	return AgentSnapshot{
		Version:         version,
		SchemaVersion:   schemaVersion,
		UpdatedAt:       snapshot.UpdatedAt,
		Settings:        settingsCopy,
		ManagedSections: snapshot.ManagedSections,
	}, nil
}
