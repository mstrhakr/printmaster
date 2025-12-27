package updatepolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"printmaster/common/updatepolicy"
	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

type fakeStore struct {
	policies   map[string]*storage.FleetUpdatePolicy
	deletes    []string
	lastUpsert *storage.FleetUpdatePolicy
}

func newFakeStore() *fakeStore {
	return &fakeStore{policies: make(map[string]*storage.FleetUpdatePolicy)}
}

func (s *fakeStore) GetFleetUpdatePolicy(ctx context.Context, tenantID string) (*storage.FleetUpdatePolicy, error) {
	if rec, ok := s.policies[tenantID]; ok {
		clone := *rec
		clone.PolicySpec = clonePolicySpec(rec.PolicySpec)
		return &clone, nil
	}
	return nil, nil
}

func (s *fakeStore) UpsertFleetUpdatePolicy(ctx context.Context, policy *storage.FleetUpdatePolicy) error {
	stored := &storage.FleetUpdatePolicy{
		TenantID:   policy.TenantID,
		PolicySpec: clonePolicySpec(policy.PolicySpec),
		UpdatedAt:  time.Unix(1700000000, 0).UTC(),
	}
	s.policies[policy.TenantID] = stored
	clone := *stored
	clone.PolicySpec = clonePolicySpec(stored.PolicySpec)
	s.lastUpsert = &clone
	return nil
}

func (s *fakeStore) DeleteFleetUpdatePolicy(ctx context.Context, tenantID string) error {
	delete(s.policies, tenantID)
	s.deletes = append(s.deletes, tenantID)
	return nil
}

func (s *fakeStore) ListFleetUpdatePolicies(ctx context.Context) ([]*storage.FleetUpdatePolicy, error) {
	out := make([]*storage.FleetUpdatePolicy, 0, len(s.policies))
	for _, rec := range s.policies {
		clone := *rec
		clone.PolicySpec = clonePolicySpec(rec.PolicySpec)
		out = append(out, &clone)
	}
	return out, nil
}

func allowAllAuthorizer(_ *http.Request, _ authz.Action, _ authz.ResourceRef) error {
	return nil
}

func TestHandleTenantPolicyGetNotFound(t *testing.T) {
	store := newFakeStore()
	api, err := NewAPI(store, APIOptions{Authorizer: allowAllAuthorizer})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/update-policies/tenant-a", nil)
	rr := httptest.NewRecorder()
	api.handlePolicyRoute(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleTenantPolicyPutPersists(t *testing.T) {
	store := newFakeStore()
	api, err := NewAPI(store, APIOptions{
		Authorizer:    allowAllAuthorizer,
		ActorResolver: func(*http.Request) string { return "alice" },
	})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	payload := map[string]interface{}{
		"policy": map[string]interface{}{
			"update_check_days":    7,
			"version_pin_strategy": "minor",
			"allow_major_upgrade":  true,
			"collect_telemetry":    true,
			"maintenance_window": map[string]interface{}{
				"enabled":      true,
				"timezone":     "UTC",
				"start_hour":   1,
				"start_min":    0,
				"end_hour":     3,
				"end_min":      0,
				"days_of_week": []int{1, 3, 5},
			},
			"rollout_control": map[string]interface{}{
				"staggered":      true,
				"max_concurrent": 5,
				"batch_size":     2,
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/update-policies/tenant-a", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.handlePolicyRoute(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if store.lastUpsert == nil {
		t.Fatalf("upsert was not recorded")
	}
	var resp policyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.Policy.UpdateCheckDays != 7 {
		t.Fatalf("policy not returned")
	}
	if resp.UpdatedAt == nil {
		t.Fatalf("expected updated timestamp")
	}
}

func TestHandleTenantPolicyPutValidation(t *testing.T) {
	store := newFakeStore()
	api, err := NewAPI(store, APIOptions{Authorizer: allowAllAuthorizer})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	payload := map[string]interface{}{
		"policy": map[string]interface{}{
			"update_check_days": 7,
			"maintenance_window": map[string]interface{}{
				"enabled": true,
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/update-policies/tenant-a", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	api.handlePolicyRoute(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid policy") {
		t.Fatalf("expected invalid policy message")
	}
}

func TestHandleTenantPolicyDelete(t *testing.T) {
	store := newFakeStore()
	store.policies["tenant-a"] = &storage.FleetUpdatePolicy{TenantID: "tenant-a"}
	api, err := NewAPI(store, APIOptions{Authorizer: allowAllAuthorizer})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/update-policies/tenant-a", nil)
	rr := httptest.NewRecorder()
	api.handlePolicyRoute(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if len(store.deletes) != 1 || store.deletes[0] != "tenant-a" {
		t.Fatalf("expected delete to be recorded")
	}
}

func TestHandleListPolicies(t *testing.T) {
	store := newFakeStore()
	store.policies["tenant-b"] = &storage.FleetUpdatePolicy{TenantID: "tenant-b", PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 3}}
	store.policies["tenant-a"] = &storage.FleetUpdatePolicy{TenantID: "tenant-a", PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 5}}
	api, err := NewAPI(store, APIOptions{Authorizer: allowAllAuthorizer})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/update-policies", nil)
	rr := httptest.NewRecorder()
	api.handleListPolicies(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp []policyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected two policies, got %d", len(resp))
	}
	if resp[0].TenantID != "tenant-a" {
		t.Fatalf("expected sorted response, got %+v", resp)
	}
}

func TestHandleGlobalPolicyRoundTrip(t *testing.T) {
	store := newFakeStore()
	api, err := NewAPI(store, APIOptions{
		Authorizer:    allowAllAuthorizer,
		ActorResolver: func(*http.Request) string { return "carol" },
	})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}

	// GET before configured should 404
	req := httptest.NewRequest(http.MethodGet, "/api/v1/update-policies/global", nil)
	rr := httptest.NewRecorder()
	api.handlePolicyRoute(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing global policy, got %d", rr.Code)
	}

	payload := map[string]interface{}{
		"policy": map[string]interface{}{
			"update_check_days":    14,
			"version_pin_strategy": "major",
		},
	}
	body, _ := json.Marshal(payload)
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/update-policies/global", bytes.NewReader(body))
	putRR := httptest.NewRecorder()
	api.handlePolicyRoute(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("expected 200 saving global policy, got %d", putRR.Code)
	}
	if store.lastUpsert == nil || store.lastUpsert.TenantID != storage.GlobalFleetPolicyTenantID {
		t.Fatalf("expected global tenant id to be persisted, got %+v", store.lastUpsert)
	}
	var resp policyResponse
	if err := json.NewDecoder(putRR.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if resp.TenantID != "global" {
		t.Fatalf("expected tenant_id 'global', got %s", resp.TenantID)
	}

	// Delete should succeed and remove record
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/update-policies/global", nil)
	delRR := httptest.NewRecorder()
	api.handlePolicyRoute(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("expected 204 deleting global policy, got %d", delRR.Code)
	}
	if _, ok := store.policies[storage.GlobalFleetPolicyTenantID]; ok {
		t.Fatalf("global policy was not deleted")
	}
}

func TestHandleListPoliciesIncludesGlobal(t *testing.T) {
	store := newFakeStore()
	store.policies[storage.GlobalFleetPolicyTenantID] = &storage.FleetUpdatePolicy{TenantID: storage.GlobalFleetPolicyTenantID, PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 10}}
	store.policies["tenant-a"] = &storage.FleetUpdatePolicy{TenantID: "tenant-a", PolicySpec: updatepolicy.PolicySpec{UpdateCheckDays: 5}}
	api, err := NewAPI(store, APIOptions{Authorizer: allowAllAuthorizer})
	if err != nil {
		t.Fatalf("NewAPI failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/update-policies", nil)
	rr := httptest.NewRecorder()
	api.handleListPolicies(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp []policyResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected two policies, got %d", len(resp))
	}
	if resp[0].TenantID != "global" {
		t.Fatalf("expected global policy first, got %+v", resp)
	}
}
