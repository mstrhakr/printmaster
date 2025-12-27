package settings

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	pmsettings "printmaster/common/settings"
	"printmaster/server/storage"
)

// Store captures the storage operations required by the settings API.
type Store interface {
	GetGlobalSettings(context.Context) (*storage.SettingsRecord, error)
	UpsertGlobalSettings(context.Context, *storage.SettingsRecord) error
	GetTenantSettings(context.Context, string) (*storage.TenantSettingsRecord, error)
	UpsertTenantSettings(context.Context, *storage.TenantSettingsRecord) error
	DeleteTenantSettings(context.Context, string) error
	GetAgentSettings(context.Context, string) (*storage.AgentSettingsRecord, error)
	UpsertAgentSettings(context.Context, *storage.AgentSettingsRecord) error
	DeleteAgentSettings(context.Context, string) error
	GetAgent(context.Context, string) (*storage.Agent, error)
	GetTenant(context.Context, string) (*storage.Tenant, error)
}

// Resolver merges settings layers (defaults → global → tenant overrides).
type Resolver struct {
	store Store
}

// NewResolver builds a resolver using the provided store.
// Returns an error if store is nil.
func NewResolver(store Store) (*Resolver, error) {
	if store == nil {
		return nil, errors.New("settings resolver requires a store")
	}
	return &Resolver{store: store}, nil
}

// ResolveGlobal returns the canonical global settings snapshot.
func (r *Resolver) ResolveGlobal(ctx context.Context) (Snapshot, error) {
	rec, err := r.store.GetGlobalSettings(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if rec == nil {
		defaults := pmsettings.DefaultSettings()
		pmsettings.Sanitize(&defaults)
		// Default: all sections managed when no record exists
		return Snapshot{
			SchemaVersion:   pmsettings.SchemaVersion,
			Settings:        defaults,
			ManagedSections: []string{"discovery", "snmp", "features"},
		}, nil
	}
	settings := rec.Settings
	pmsettings.Sanitize(&settings)
	version := rec.SchemaVersion
	if strings.TrimSpace(version) == "" {
		version = pmsettings.SchemaVersion
	}
	// Use stored managed sections or default to all sections
	managedSections := rec.ManagedSections
	if len(managedSections) == 0 {
		managedSections = []string{"discovery", "snmp", "features"}
	}
	return Snapshot{
		SchemaVersion:   version,
		Settings:        settings,
		ManagedSections: managedSections,
		UpdatedAt:       rec.UpdatedAt,
		UpdatedBy:       rec.UpdatedBy,
	}, nil
}

// ResolveForTenant returns the resolved settings for a tenant, including override metadata.
func (r *Resolver) ResolveForTenant(ctx context.Context, tenantID string) (TenantSnapshot, error) {
	if strings.TrimSpace(tenantID) == "" {
		return TenantSnapshot{}, fmt.Errorf("tenant id required")
	}
	globalSnap, err := r.ResolveGlobal(ctx)
	if err != nil {
		return TenantSnapshot{}, err
	}
	snapshot := TenantSnapshot{
		TenantID:      tenantID,
		Snapshot:      globalSnap,
		Overrides:     map[string]interface{}{},
		OverridePaths: []string{},
	}
	rec, err := r.store.GetTenantSettings(ctx, tenantID)
	if err != nil {
		return TenantSnapshot{}, err
	}
	if rec == nil {
		snapshot.EnforcedSections = normalizeSectionList(nil)
		return snapshot, nil
	}
	snapshot.EnforcedSections = normalizeSectionList(rec.EnforcedSections)
	if len(rec.Overrides) == 0 {
		return snapshot, nil
	}
	merged, err := ApplyPatch(globalSnap.Settings, rec.Overrides)
	if err != nil {
		return TenantSnapshot{}, err
	}
	snapshot.Settings = merged
	if strings.TrimSpace(rec.SchemaVersion) != "" {
		snapshot.SchemaVersion = rec.SchemaVersion
	}
	snapshot.Overrides = cloneMap(rec.Overrides)
	snapshot.OverridePaths = collectOverridePaths(rec.Overrides)
	if !rec.UpdatedAt.IsZero() {
		snapshot.OverridesUpdatedAt = rec.UpdatedAt
		snapshot.UpdatedAt = rec.UpdatedAt
	}
	if strings.TrimSpace(rec.UpdatedBy) != "" {
		snapshot.OverridesUpdatedBy = rec.UpdatedBy
		snapshot.UpdatedBy = rec.UpdatedBy
	}
	return snapshot, nil
}

// ResolveForAgent returns the resolved settings for an agent, including agent override metadata.
func (r *Resolver) ResolveForAgent(ctx context.Context, agentID string) (AgentSettingsSnapshot, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return AgentSettingsSnapshot{}, fmt.Errorf("agent id required")
	}
	agent, err := r.store.GetAgent(ctx, agentID)
	if err != nil {
		return AgentSettingsSnapshot{}, err
	}
	if agent == nil {
		return AgentSettingsSnapshot{}, fmt.Errorf("agent not found")
	}

	var (
		baseSnap Snapshot
		tenantID string
		enforced []string
	)
	tenantID = strings.TrimSpace(agent.TenantID)
	if tenantID == "" {
		baseSnap, err = r.ResolveGlobal(ctx)
	} else {
		tenantSnap, terr := r.ResolveForTenant(ctx, tenantID)
		if terr != nil {
			return AgentSettingsSnapshot{}, terr
		}
		baseSnap = tenantSnap.Snapshot
		enforced = normalizeSectionList(tenantSnap.EnforcedSections)
	}
	if err != nil {
		return AgentSettingsSnapshot{}, err
	}

	snap := AgentSettingsSnapshot{
		AgentID:          agentID,
		TenantID:         tenantID,
		Snapshot:         baseSnap,
		Overrides:        map[string]interface{}{},
		OverridePaths:    []string{},
		EnforcedSections: enforced,
	}

	rec, err := r.store.GetAgentSettings(ctx, agentID)
	if err != nil {
		return AgentSettingsSnapshot{}, err
	}
	if rec == nil || len(rec.Overrides) == 0 {
		return snap, nil
	}

	allowed := allowedAgentOverrideSections(baseSnap.ManagedSections, enforced)
	filtered, blocked := filterOverrideSections(rec.Overrides, allowed)
	// Ignore any blocked sections at resolve-time; writes should prevent them.
	_ = blocked
	if len(filtered) == 0 {
		return snap, nil
	}

	merged, err := ApplyPatch(baseSnap.Settings, filtered)
	if err != nil {
		return AgentSettingsSnapshot{}, err
	}
	snap.Settings = merged
	if strings.TrimSpace(rec.SchemaVersion) != "" {
		snap.SchemaVersion = rec.SchemaVersion
	}
	snap.Overrides = cloneMap(filtered)
	snap.OverridePaths = collectOverridePaths(filtered)
	if !rec.UpdatedAt.IsZero() {
		snap.OverridesUpdatedAt = rec.UpdatedAt
		if snap.UpdatedAt.IsZero() || rec.UpdatedAt.After(snap.UpdatedAt) {
			snap.UpdatedAt = rec.UpdatedAt
		}
	}
	if strings.TrimSpace(rec.UpdatedBy) != "" {
		snap.OverridesUpdatedBy = rec.UpdatedBy
		if snap.UpdatedBy == "" || snap.UpdatedAt.Equal(rec.UpdatedAt) || rec.UpdatedAt.After(baseSnap.UpdatedAt) {
			snap.UpdatedBy = rec.UpdatedBy
		}
	}
	return snap, nil
}

func normalizeSectionList(sections []string) []string {
	if len(sections) == 0 {
		return []string{}
	}
	valid := map[string]bool{"discovery": true, "snmp": true, "features": true}
	seen := make(map[string]bool)
	out := make([]string, 0, len(sections))
	for _, s := range sections {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" || !valid[s] || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func allowedAgentOverrideSections(managedSections, enforcedSections []string) map[string]bool {
	allowed := make(map[string]bool)
	managed := normalizeSectionList(managedSections)
	if len(managed) == 0 {
		managed = []string{"discovery", "snmp", "features"}
	}
	for _, s := range managed {
		allowed[s] = true
	}
	for _, s := range normalizeSectionList(enforcedSections) {
		delete(allowed, s)
	}
	return allowed
}

func filterOverrideSections(overrides map[string]interface{}, allowed map[string]bool) (map[string]interface{}, []string) {
	if len(overrides) == 0 {
		return map[string]interface{}{}, nil
	}
	filtered := make(map[string]interface{})
	blockedSet := make(map[string]bool)
	for k, v := range overrides {
		key := strings.TrimSpace(strings.ToLower(k))
		if allowed[key] {
			filtered[k] = v
		} else {
			blockedSet[key] = true
		}
	}
	blocked := make([]string, 0, len(blockedSet))
	for k := range blockedSet {
		blocked = append(blocked, k)
	}
	sort.Strings(blocked)
	return filtered, blocked
}

func collectOverridePaths(overrides map[string]interface{}) []string {
	if len(overrides) == 0 {
		return []string{}
	}
	var out []string
	var walk func(path string, value interface{})
	walk = func(path string, value interface{}) {
		if value == nil {
			out = append(out, path)
			return
		}
		if nested, ok := value.(map[string]interface{}); ok {
			for key, child := range nested {
				next := key
				if path != "" {
					next = path + "." + key
				}
				walk(next, child)
			}
			return
		}
		out = append(out, path)
	}
	for key, value := range overrides {
		walk(key, value)
	}
	sort.Strings(out)
	return out
}
