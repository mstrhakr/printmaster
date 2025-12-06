package storage

import (
	"context"
	"testing"
)

func TestUserLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user
	user := &User{
		Username: "testuser",
		Role:     RoleAdmin,
		Email:    "test@example.com",
	}

	err = s.CreateUser(ctx, user, "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == 0 {
		t.Fatal("expected non-zero user ID")
	}

	// Get user by username
	got, err := s.GetUserByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Role != RoleAdmin {
		t.Errorf("role mismatch: got=%v", got.Role)
	}
	if got.Email != "test@example.com" {
		t.Errorf("email mismatch: got=%q", got.Email)
	}

	// Get user by ID
	gotByID, err := s.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if gotByID == nil {
		t.Fatal("expected user by ID, got nil")
	}
	if gotByID.Username != "testuser" {
		t.Errorf("username mismatch: got=%q", gotByID.Username)
	}

	// Update user
	user.Role = RoleOperator
	user.Email = "newemail@example.com"
	err = s.UpdateUser(ctx, user)
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	got, _ = s.GetUserByUsername(ctx, "testuser")
	if got.Role != RoleOperator {
		t.Errorf("role not updated: got=%v", got.Role)
	}
	if got.Email != "newemail@example.com" {
		t.Errorf("email not updated: got=%q", got.Email)
	}

	// List users
	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user, got %d", len(users))
	}

	// Delete user
	err = s.DeleteUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	got, err = s.GetUserByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("GetUserByUsername after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestUserNotFound(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Get non-existent user by username
	got, err := s.GetUserByUsername(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent username")
	}

	// Get non-existent user by ID
	gotByID, err := s.GetUserByID(ctx, 999999)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if gotByID != nil {
		t.Error("expected nil for non-existent ID")
	}
}

func TestAuthenticateUser(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user with password
	user := &User{
		Username: "authuser",
		Role:     RoleAdmin,
	}
	err = s.CreateUser(ctx, user, "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Authenticate with correct password
	authed, err := s.AuthenticateUser(ctx, "authuser", "password123")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if authed == nil {
		t.Fatal("expected authenticated user, got nil")
	}
	if authed.Username != "authuser" {
		t.Errorf("username mismatch: got=%q", authed.Username)
	}

	// Authenticate with wrong password - should return error for audit logging
	authed, err = s.AuthenticateUser(ctx, "authuser", "wrongpassword")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	if authed != nil {
		t.Error("expected nil user for wrong password")
	}

	// Authenticate non-existent user - should return error for audit logging
	authed, err = s.AuthenticateUser(ctx, "nouser", "password123")
	if err == nil {
		t.Fatal("expected error for non-existent user")
	}
	if authed != nil {
		t.Error("expected nil user for non-existent user")
	}
}

func TestUpdatePassword(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user
	user := &User{
		Username: "pwduser",
		Role:     RoleAdmin,
	}
	err = s.CreateUser(ctx, user, "oldpassword")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Update password
	err = s.UpdateUserPassword(ctx, user.ID, "newpassword")
	if err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}

	// Old password should fail
	authed, _ := s.AuthenticateUser(ctx, "pwduser", "oldpassword")
	if authed != nil {
		t.Error("old password should not work")
	}

	// New password should work
	authed, _ = s.AuthenticateUser(ctx, "pwduser", "newpassword")
	if authed == nil {
		t.Error("new password should work")
	}
}

func TestMultipleUsers(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	roles := []Role{RoleAdmin, RoleOperator, RoleViewer}
	for i, role := range roles {
		user := &User{
			Username: "user" + string(rune('a'+i)),
			Role:     role,
		}
		err := s.CreateUser(ctx, user, "password")
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Errorf("expected 3 users, got %d", len(users))
	}

	// Verify roles
	roleMap := make(map[string]Role)
	for _, u := range users {
		roleMap[u.Username] = u.Role
	}
	if roleMap["usera"] != RoleAdmin {
		t.Error("usera should be admin")
	}
	if roleMap["userb"] != RoleOperator {
		t.Error("userb should be operator")
	}
	if roleMap["userc"] != RoleViewer {
		t.Error("userc should be viewer")
	}
}

func TestUserWithTenant(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create tenant first
	tenant := &Tenant{
		ID:   "tenant-user-test",
		Name: "User Test Tenant",
	}
	err = s.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Create user with tenant
	user := &User{
		Username: "tenantuser",
		Role:     RoleAdmin,
		TenantID: "tenant-user-test",
	}
	err = s.CreateUser(ctx, user, "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Get user and verify tenant
	got, _ := s.GetUserByUsername(ctx, "tenantuser")
	if got.TenantID != "tenant-user-test" {
		t.Errorf("tenant_id mismatch: got=%q", got.TenantID)
	}
}

func TestGetUserByEmail(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user with email
	user := &User{
		Username: "emailuser",
		Role:     RoleAdmin,
		Email:    "unique@example.com",
	}
	err = s.CreateUser(ctx, user, "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Get by email
	got, err := s.GetUserByEmail(ctx, "unique@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got == nil {
		t.Fatal("expected user, got nil")
	}
	if got.Username != "emailuser" {
		t.Errorf("username mismatch: got=%q", got.Username)
	}

	// Non-existent email
	got, err = s.GetUserByEmail(ctx, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail (nonexistent): %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent email")
	}
}

func TestTokenHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
	}{
		{"simple", "abc123"},
		{"long", "verylongtokenthatexceeds32characters"},
		{"special", "token+with/special=chars"},
		{"uuid", "550e8400-e29b-41d4-a716-446655440000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := TokenHash(tt.token)
			if hash == "" {
				t.Fatal("expected non-empty hash")
			}
			if hash == tt.token {
				t.Fatal("hash should not equal token")
			}

			// Same input should produce same hash
			hash2 := TokenHash(tt.token)
			if hash != hash2 {
				t.Error("same token should produce same hash")
			}

			// Different input should produce different hash
			hash3 := TokenHash(tt.token + "x")
			if hash == hash3 {
				t.Error("different tokens should produce different hashes")
			}
		})
	}
}

func TestGetDefaultDBPath(t *testing.T) {
	t.Parallel()

	path := GetDefaultDBPath()
	if path == "" {
		t.Fatal("expected non-empty default DB path")
	}
	// Path should contain expected components
	t.Logf("Default DB path: %s", path)
}
