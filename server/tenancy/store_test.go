package tenancy

import (
	"testing"
	"time"
)

func TestCreateAndValidateJoinToken(t *testing.T) {
	// Ensure default tenant exists
	if _, ok := store.tenants["default"]; !ok {
		t.Fatalf("default tenant missing")
	}

	// Create a tenant
	tnt, err := store.CreateTenant("testt", "Test Tenant", "")
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}
	if tnt.ID != "testt" {
		t.Fatalf("unexpected tenant id: %s", tnt.ID)
	}

	// Create token
	jt, err := store.CreateJoinToken(tnt.ID, 1, true)
	if err != nil {
		t.Fatalf("CreateJoinToken failed: %v", err)
	}
	if jt.TenantID != tnt.ID {
		t.Fatalf("join token tenant mismatch: %s != %s", jt.TenantID, tnt.ID)
	}

	// Validate token
	v, err := store.ValidateToken(jt.Token)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if v.Token != jt.Token {
		t.Fatalf("validated token mismatch")
	}

	// Since it's one-time, second validation should fail
	_, err = store.ValidateToken(jt.Token)
	if err == nil {
		t.Fatalf("expected error for consumed one-time token")
	}
}

func TestListTenants(t *testing.T) {
	// Create a couple tenants
	_, err := store.CreateTenant("t1", "One", "")
	if err != nil {
		t.Fatalf("CreateTenant t1: %v", err)
	}
	_, err = store.CreateTenant("t2", "Two", "")
	if err != nil {
		t.Fatalf("CreateTenant t2: %v", err)
	}

	list := store.ListTenants()
	if len(list) < 2 {
		t.Fatalf("expected at least 2 tenants, got %d", len(list))
	}

	// Ensure created times are sensible
	for _, tnt := range list {
		if time.Since(tnt.CreatedAt) < 0 {
			t.Fatalf("tenant has future created_at")
		}
	}
}
