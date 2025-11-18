package tenancy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func enableTenancyForTest(t *testing.T) {
	SetEnabled(true)
	t.Cleanup(func() { SetEnabled(false) })
}

func TestHandleTenantsCreateAndList(t *testing.T) {
	enableTenancyForTest(t)
	// POST create
	in := map[string]string{
		"id":            "httpt",
		"name":          "HTTP Tenant",
		"contact_email": "owner@example.com",
		"business_unit": "PS",
	}
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
	for _, tenant := range list {
		if tenant.ID == "httpt" {
			if tenant.ContactEmail != "owner@example.com" {
				t.Fatalf("contact email not persisted")
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created tenant not found in list")
	}
}

func TestHandleTenantUpdate(t *testing.T) {
	enableTenancyForTest(t)
	_, _ = store.CreateTenant(Tenant{ID: "update1", Name: "Original", ContactPhone: "123"})
	payload := map[string]string{
		"name":          "Updated Tenant",
		"contact_phone": "+18005551234",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/update1", bytes.NewReader(body))
	rw := httptest.NewRecorder()
	handleTenantByID(rw, req)
	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var out Tenant
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if out.ContactPhone != "+18005551234" {
		t.Fatalf("contact phone not updated")
	}
}

func TestCreateJoinTokenAndRegister(t *testing.T) {
	enableTenancyForTest(t)
	// Ensure tenant exists
	_, _ = store.CreateTenant(Tenant{ID: "regt", Name: "Reg Tenant"})

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

func TestRegisterWithTokenEmitsEvent(t *testing.T) {
	enableTenancyForTest(t)
	defer SetAgentEventSink(nil)

	_, _ = store.CreateTenant(Tenant{ID: "event", Name: "Event Tenant"})
	jt, err := store.CreateJoinToken("event", 5, false)
	if err != nil {
		t.Fatalf("CreateJoinToken failed: %v", err)
	}

	events := make(chan map[string]interface{}, 1)
	SetAgentEventSink(func(eventType string, data map[string]interface{}) {
		if eventType == "agent_registered" {
			events <- data
		}
	})

	reg := map[string]interface{}{
		"token":            jt.Token,
		"agent_id":         "agent-event",
		"name":             "Event Agent",
		"hostname":         "evt-host",
		"ip":               "10.0.0.55",
		"platform":         "linux",
		"agent_version":    "1.2.3",
		"protocol_version": "1",
	}
	body, _ := json.Marshal(reg)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register-with-token", bytes.NewReader(body))
	rw := httptest.NewRecorder()
	handleRegisterWithToken(rw, req)
	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}

	select {
	case payload := <-events:
		if payload["agent_id"] != "agent-event" {
			t.Fatalf("unexpected agent_id payload: %v", payload["agent_id"])
		}
		if payload["connection_type"] != "none" {
			t.Fatalf("unexpected connection_type: %v", payload["connection_type"])
		}
	default:
		t.Fatalf("expected agent_registered event")
	}
}

func TestListAndRevokeJoinTokens(t *testing.T) {
	enableTenancyForTest(t)
	// Ensure tenant exists and create token
	_, _ = store.CreateTenant(Tenant{ID: "admint", Name: "Admin Tenant"})
	jt, err := store.CreateJoinToken("admint", 5, false)
	if err != nil {
		t.Fatalf("CreateJoinToken failed: %v", err)
	}
	if jt == (JoinToken{}) {
		t.Fatalf("empty join token returned")
	}

	// List via handler (in-memory path)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/join-tokens?tenant_id=admint", nil)
	rw := httptest.NewRecorder()
	handleListJoinTokens(rw, req)
	res := rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
	var list []JoinToken
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatalf("decode list failed: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected non-empty list")
	}

	// Revoke via handler (attempt by id)
	// in-memory store uses token value as key; revoke by token
	b, _ := json.Marshal(map[string]string{"id": jt.Token})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/join-token/revoke", bytes.NewReader(b))
	rw = httptest.NewRecorder()
	handleRevokeJoinToken(rw, req)
	res = rw.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}

	// no raw for in-memory path
}
