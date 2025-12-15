package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pmsettings "printmaster/common/settings"
	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

type fakeStore struct {
	global        *storage.SettingsRecord
	tenants       map[string]*storage.Tenant
	tenantRecords map[string]*storage.TenantSettingsRecord
	agents        map[string]*storage.Agent
	agentRecords  map[string]*storage.AgentSettingsRecord
	lastGlobal    *storage.SettingsRecord
	lastTenant    *storage.TenantSettingsRecord
	deleteCalls   []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tenants:       make(map[string]*storage.Tenant),
		tenantRecords: make(map[string]*storage.TenantSettingsRecord),
		agents:        make(map[string]*storage.Agent),
		agentRecords:  make(map[string]*storage.AgentSettingsRecord),
	}
}

func (s *fakeStore) GetGlobalSettings(context.Context) (*storage.SettingsRecord, error) {
	if s.global == nil {
		return nil, nil
	}
	copy := *s.global
	copy.Settings = s.global.Settings
	return &copy, nil
}

func (s *fakeStore) UpsertGlobalSettings(ctx context.Context, rec *storage.SettingsRecord) error {
	copy := *rec
	copy.Settings = rec.Settings
	s.global = &copy
	s.lastGlobal = rec
	return nil
}

func (s *fakeStore) GetTenantSettings(ctx context.Context, tenantID string) (*storage.TenantSettingsRecord, error) {
	if rec, ok := s.tenantRecords[tenantID]; ok {
		copy := *rec
		copy.Overrides = cloneMap(rec.Overrides)
		return &copy, nil
	}
	return nil, nil
}

func (s *fakeStore) UpsertTenantSettings(ctx context.Context, rec *storage.TenantSettingsRecord) error {
	copy := *rec
	copy.Overrides = cloneMap(rec.Overrides)
	s.tenantRecords[rec.TenantID] = &copy
	s.lastTenant = rec
	return nil
}

func (s *fakeStore) DeleteTenantSettings(ctx context.Context, tenantID string) error {
	delete(s.tenantRecords, tenantID)
	s.deleteCalls = append(s.deleteCalls, tenantID)
	return nil
}

func (s *fakeStore) GetTenant(ctx context.Context, tenantID string) (*storage.Tenant, error) {
	if tenant, ok := s.tenants[tenantID]; ok {
		copy := *tenant
		return &copy, nil
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) GetAgent(ctx context.Context, agentID string) (*storage.Agent, error) {
	if agent, ok := s.agents[agentID]; ok {
		copy := *agent
		return &copy, nil
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) GetAgentSettings(ctx context.Context, agentID string) (*storage.AgentSettingsRecord, error) {
	if rec, ok := s.agentRecords[agentID]; ok {
		copy := *rec
		copy.Overrides = cloneMap(rec.Overrides)
		return &copy, nil
	}
	return nil, nil
}

func (s *fakeStore) UpsertAgentSettings(ctx context.Context, rec *storage.AgentSettingsRecord) error {
	copy := *rec
	copy.Overrides = cloneMap(rec.Overrides)
	s.agentRecords[rec.AgentID] = &copy
	return nil
}

func (s *fakeStore) DeleteAgentSettings(ctx context.Context, agentID string) error {
	delete(s.agentRecords, agentID)
	return nil
}

func allowAllAuthorizer(_ *http.Request, _ authz.Action, _ authz.ResourceRef) error {
	return nil
}

func TestResolverResolveGlobalDefaults(t *testing.T) {
	store := newFakeStore()
	resolver := NewResolver(store)
	snap, err := resolver.ResolveGlobal(context.Background())
	if err != nil {
		t.Fatalf("resolve global failed: %v", err)
	}
	if snap.SchemaVersion != pmsettings.SchemaVersion {
		t.Fatalf("unexpected schema version: %s", snap.SchemaVersion)
	}
	defaults := pmsettings.DefaultSettings()
	if snap.Settings.Discovery.SNMPEnabled != defaults.Discovery.SNMPEnabled {
		t.Fatalf("expected default discovery settings")
	}
}

func TestResolverResolveForTenantOverrides(t *testing.T) {
	store := newFakeStore()
	store.global = &storage.SettingsRecord{SchemaVersion: "v1", Settings: pmsettings.DefaultSettings()}
	store.tenantRecords["tenant-a"] = &storage.TenantSettingsRecord{
		TenantID:      "tenant-a",
		SchemaVersion: "v1",
		Overrides: map[string]interface{}{
			"discovery": map[string]interface{}{"snmp_enabled": false},
		},
		UpdatedAt: time.Unix(200, 0),
		UpdatedBy: "tester",
	}
	resolver := NewResolver(store)
	snap, err := resolver.ResolveForTenant(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("resolve tenant failed: %v", err)
	}
	if snap.Settings.Discovery.SNMPEnabled {
		t.Fatalf("override not applied")
	}
	if len(snap.OverridePaths) != 1 || snap.OverridePaths[0] != "discovery.snmp_enabled" {
		t.Fatalf("unexpected override paths: %+v", snap.OverridePaths)
	}
	if snap.OverridesUpdatedBy != "tester" {
		t.Fatalf("override metadata missing")
	}
}

func TestAPIHandleGlobalPutPersistsSettings(t *testing.T) {
	store := newFakeStore()
	api := NewAPI(store, nil, APIOptions{
		Authorizer:    allowAllAuthorizer,
		ActorResolver: func(*http.Request) string { return "alice" },
	})
	payload := pmsettings.DefaultSettings()
	payload.SNMP.TimeoutMS = 1234
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/global", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.handleGlobal(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if store.lastGlobal == nil {
		t.Fatalf("global settings were not persisted")
	}
	if store.lastGlobal.Settings.SNMP.TimeoutMS != 1234 {
		t.Fatalf("override not saved")
	}
	if store.lastGlobal.UpdatedBy != "alice" {
		t.Fatalf("updated by not recorded")
	}
}

func TestAPIHandleTenantPutStoresOverrides(t *testing.T) {
	store := newFakeStore()
	store.global = &storage.SettingsRecord{SchemaVersion: "v1", Settings: pmsettings.DefaultSettings()}
	store.tenants["tenant-a"] = &storage.Tenant{ID: "tenant-a", Name: "Tenant A"}
	api := NewAPI(store, nil, APIOptions{
		Authorizer:    allowAllAuthorizer,
		ActorResolver: func(*http.Request) string { return "bob" },
	})
	patch := map[string]interface{}{
		"discovery": map[string]interface{}{"snmp_enabled": false},
	}
	body, _ := json.Marshal(patch)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings/tenants/tenant-a", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.handleTenantSettings(rr, req, "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if store.lastTenant == nil {
		t.Fatalf("tenant overrides not saved")
	}
	if store.lastTenant.UpdatedBy != "bob" {
		t.Fatalf("actor not recorded")
	}
	if _, ok := store.lastTenant.Overrides["discovery"].(map[string]interface{})["snmp_enabled"]; !ok {
		t.Fatalf("override missing from stored record: %+v", store.lastTenant.Overrides)
	}
}
