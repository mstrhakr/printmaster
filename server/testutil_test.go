package main

import (
	"fmt"
	"net/http"
	"testing"

	"printmaster/server/storage"
)

// NewTestAdminUser returns a reusable admin user for tests.
func NewTestAdminUser() *storage.User {
	return &storage.User{ID: 1, Username: "test-admin", Role: storage.RoleAdmin}
}

// NewTestUser returns a user with the specified role and tenant scope for RBAC tests.
func NewTestUser(role storage.Role, tenantIDs ...string) *storage.User {
	ids := storage.SortTenantIDs(tenantIDs)
	primaryTenant := ""
	if len(ids) > 0 {
		primaryTenant = ids[0]
	}
	return &storage.User{
		ID:        100,
		Username:  fmt.Sprintf("test-%s", string(role)),
		Role:      role,
		TenantID:  primaryTenant,
		TenantIDs: ids,
	}
}

// InjectTestAdmin attaches a test admin user into the request context and returns
// the modified request.
func InjectTestAdmin(req *http.Request) *http.Request {
	return InjectTestUser(req, NewTestAdminUser())
}

// WrapWithAdmin wraps an http handler so that every incoming request has the
// test admin user injected into its context before the handler is invoked.
func WrapWithAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := contextWithPrincipal(r.Context(), NewTestAdminUser())
		h(w, r.WithContext(ctx))
	}
}

// InjectTestUser adds the provided test user/principal into the request context.
func InjectTestUser(req *http.Request, user *storage.User) *http.Request {
	ctx := contextWithPrincipal(req.Context(), user)
	return req.WithContext(ctx)
}

// SetupTestStore creates an in-memory SQLite store and assigns it to the global serverStore.
func SetupTestStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	serverStore = store
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}
