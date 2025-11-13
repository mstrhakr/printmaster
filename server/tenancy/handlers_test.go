package tenancy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleTenantsCreateAndList(t *testing.T) {
	// POST create
	in := map[string]string{"id": "httpt", "name": "HTTP Tenant"}
	b, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader(b))
	rw := httptest.NewRecorder()
	handleTenants(rw, req)
	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var out Tenant
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out.ID != "httpt" {
		t.Fatalf("unexpected tenant id: %s", out.ID)
	}

	// GET list
	req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	rw = httptest.NewRecorder()
	handleTenants(rw, req)
	res = rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var list []Tenant
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	found := false
	for _, t := range list {
		if t.ID == "httpt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created tenant not found in list")
	}
}

func TestCreateJoinTokenAndRegister(t *testing.T) {
	// Ensure tenant exists
	_, _ = store.CreateTenant("regt", "Reg Tenant", "")

	// Create join token via handler
	in := map[string]interface{}{"tenant_id": "regt", "ttl_minutes": 5, "one_time": false}
	b, _ := json.Marshal(in)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/join-token", bytes.NewReader(b))
	rw := httptest.NewRecorder()
	handleCreateJoinToken(rw, req)
	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var tokenResp map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("decode token resp failed: %v", err)
	}
	token, ok := tokenResp["token"].(string)
	if !ok || token == "" {
		t.Fatalf("token missing in response")
	}

	// Now register with token
	reg := map[string]string{"token": token, "agent_id": "agent-x", "name": "agent x"}
	rb, _ := json.Marshal(reg)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/agents/register-with-token", bytes.NewReader(rb))
	rw = httptest.NewRecorder()
	handleRegisterWithToken(rw, req)
	res = rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var regResp map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode register resp failed: %v", err)
	}
	if regResp["tenant_id"] != "regt" {
		t.Fatalf("unexpected tenant_id in register response: %v", regResp["tenant_id"])
	}
	if regResp["agent_token"] == nil {
		t.Fatalf("agent_token missing in response")
	}
}
