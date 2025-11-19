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
	Version       string              `json:"version"`
	SchemaVersion string              `json:"schema_version"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Settings      pmsettings.Settings `json:"settings"`
}

// BuildAgentSnapshot resolves the appropriate settings for an agent and rewrites the
// payload so agent-local fields are reset to defaults. Tenants receive their overrides,
// while standalone agents fall back to the global snapshot.
func BuildAgentSnapshot(ctx context.Context, resolver *Resolver, tenantID string) (AgentSnapshot, error) {
	if resolver == nil {
		return AgentSnapshot{}, fmt.Errorf("resolver required")
	}
	var (
		snapshot Snapshot
		err      error
	)
	if strings.TrimSpace(tenantID) == "" {
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
		Version:       version,
		SchemaVersion: schemaVersion,
		UpdatedAt:     snapshot.UpdatedAt,
		Settings:      settingsCopy,
	}, nil
}
