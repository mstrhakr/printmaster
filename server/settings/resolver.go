package settings

import (
	"context"
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
	GetTenant(context.Context, string) (*storage.Tenant, error)
}

// Resolver merges settings layers (defaults → global → tenant overrides).
type Resolver struct {
	store Store
}

// NewResolver builds a resolver using the provided store.
func NewResolver(store Store) *Resolver {
	if store == nil {
		panic("settings resolver requires a store")
	}
	return &Resolver{store: store}
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
		return Snapshot{SchemaVersion: pmsettings.SchemaVersion, Settings: defaults}, nil
	}
	settings := rec.Settings
	pmsettings.Sanitize(&settings)
	version := rec.SchemaVersion
	if strings.TrimSpace(version) == "" {
		version = pmsettings.SchemaVersion
	}
	return Snapshot{
		SchemaVersion: version,
		Settings:      settings,
		UpdatedAt:     rec.UpdatedAt,
		UpdatedBy:     rec.UpdatedBy,
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
	if rec == nil || len(rec.Overrides) == 0 {
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
