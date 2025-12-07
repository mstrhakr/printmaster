package storage

import (
	"context"
	"testing"
)

func TestNewBaseStore(t *testing.T) {
	t.Parallel()

	// Create a SQLite store to get a valid DB connection
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	// Create a BaseStore using the SQLite DB
	bs := NewBaseStore(s.DB(), s.Dialect(), ":memory:")
	if bs == nil {
		t.Fatal("NewBaseStore returned nil")
	}

	// Verify DB() method
	if bs.DB() == nil {
		t.Error("DB() returned nil")
	}
	if bs.DB() != s.DB() {
		t.Error("DB() returned different database than expected")
	}

	// Verify Dialect() method
	if bs.Dialect() == nil {
		t.Error("Dialect() returned nil")
	}
	if bs.Dialect().Name() != "sqlite" {
		t.Errorf("Dialect().Name() = %q, want %q", bs.Dialect().Name(), "sqlite")
	}
}

func TestBaseStoreQueryMethod(t *testing.T) {
	t.Parallel()

	// Create a SQLite store
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	// SQLite dialect should not modify placeholders
	sqliteQuery := s.BaseStore.query("SELECT * FROM users WHERE id = ?")
	if sqliteQuery != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("SQLite query() modified query: %q", sqliteQuery)
	}
}

func TestUserInvitationLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create an invitation
	inv := &UserInvitation{
		Email:     "newuser@example.com",
		Username:  "newuser",
		Role:      RoleOperator,
		TenantID:  "tenant-1",
		CreatedBy: "admin",
	}

	token, err := s.CreateUserInvitation(ctx, inv, 60) // 60 minute TTL
	if err != nil {
		t.Fatalf("CreateUserInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if inv.ID == 0 {
		t.Fatal("expected non-zero invitation ID")
	}

	// Verify the invitation was created correctly
	if inv.TokenHash == "" {
		t.Error("expected TokenHash to be set")
	}
	if inv.ExpiresAt.IsZero() {
		t.Error("expected ExpiresAt to be set")
	}
	if inv.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if inv.Role != RoleOperator {
		t.Errorf("role mismatch: got %v, want %v", inv.Role, RoleOperator)
	}

	// Get the invitation using the token
	got, err := s.GetUserInvitation(ctx, token)
	if err != nil {
		t.Fatalf("GetUserInvitation: %v", err)
	}
	if got == nil {
		t.Fatal("expected invitation, got nil")
	}
	if got.Email != inv.Email {
		t.Errorf("email mismatch: got %q, want %q", got.Email, inv.Email)
	}
	if got.Username != inv.Username {
		t.Errorf("username mismatch: got %q, want %q", got.Username, inv.Username)
	}
	if got.TenantID != inv.TenantID {
		t.Errorf("tenant_id mismatch: got %q, want %q", got.TenantID, inv.TenantID)
	}

	// List invitations
	invitations, err := s.ListUserInvitations(ctx)
	if err != nil {
		t.Fatalf("ListUserInvitations: %v", err)
	}
	if len(invitations) != 1 {
		t.Errorf("expected 1 invitation, got %d", len(invitations))
	}

	// Mark invitation as used
	err = s.MarkInvitationUsed(ctx, inv.ID)
	if err != nil {
		t.Fatalf("MarkInvitationUsed: %v", err)
	}

	// Token should now be invalid (used)
	_, err = s.GetUserInvitation(ctx, token)
	if err == nil {
		t.Error("expected error for used invitation")
	}

	// Delete invitation
	err = s.DeleteUserInvitation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("DeleteUserInvitation: %v", err)
	}

	// List should be empty now
	invitations, err = s.ListUserInvitations(ctx)
	if err != nil {
		t.Fatalf("ListUserInvitations after delete: %v", err)
	}
	if len(invitations) != 0 {
		t.Errorf("expected 0 invitations after delete, got %d", len(invitations))
	}
}

func TestUserInvitation_InvalidToken(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Try to get invitation with invalid token
	_, err = s.GetUserInvitation(ctx, "invalid-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestUserInvitation_EmptyEmail(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Try to create invitation with empty email
	inv := &UserInvitation{
		Email: "",
		Role:  RoleViewer,
	}
	_, err = s.CreateUserInvitation(ctx, inv, 60)
	if err == nil {
		t.Error("expected error for empty email")
	}
}

func TestUserInvitation_NilInvitation(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Try to create nil invitation
	_, err = s.CreateUserInvitation(ctx, nil, 60)
	if err == nil {
		t.Error("expected error for nil invitation")
	}
}

func TestUserInvitation_OptionalFields(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create invitation with minimal fields
	inv := &UserInvitation{
		Email: "minimal@example.com",
		Role:  RoleViewer,
		// Username, TenantID, CreatedBy are optional
	}

	token, err := s.CreateUserInvitation(ctx, inv, 60)
	if err != nil {
		t.Fatalf("CreateUserInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	got, err := s.GetUserInvitation(ctx, token)
	if err != nil {
		t.Fatalf("GetUserInvitation: %v", err)
	}
	if got.Username != "" {
		t.Errorf("expected empty username, got %q", got.Username)
	}
	if got.TenantID != "" {
		t.Errorf("expected empty tenant_id, got %q", got.TenantID)
	}
}

func TestGetSigningKey(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a signing key
	key := &SigningKey{
		ID:         "test-key-id",
		Algorithm:  "ed25519",
		PublicKey:  "public-key-data",
		PrivateKey: "private-key-data",
		Active:     true,
	}

	err = s.CreateSigningKey(ctx, key)
	if err != nil {
		t.Fatalf("CreateSigningKey: %v", err)
	}

	// Get the signing key by ID
	got, err := s.GetSigningKey(ctx, "test-key-id")
	if err != nil {
		t.Fatalf("GetSigningKey: %v", err)
	}
	if got == nil {
		t.Fatal("expected signing key, got nil")
	}
	if got.ID != "test-key-id" {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, "test-key-id")
	}
	if got.Algorithm != "ed25519" {
		t.Errorf("Algorithm mismatch: got %q, want %q", got.Algorithm, "ed25519")
	}

	// Get non-existent key - returns sql.ErrNoRows
	notFound, err := s.GetSigningKey(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent key")
	}
}

func TestSetLogging(t *testing.T) {
	t.Parallel()

	// Test that SetLogger doesn't panic with nil
	SetLogger(nil)

	// SetLogger with nil should be safe
	// Note: we can't really test the logging output, but we can verify it doesn't crash
}

func TestLogWithLevel(t *testing.T) {
	t.Parallel()

	// These tests just ensure the functions don't panic
	// Since we set logger to nil above, these should just return silently

	// Test logInfo
	logInfo("test message", "key1", "value1")

	// Test logDebug
	logDebug("test debug", "key2", "value2")

	// Test logWarn - this one is also at 0% coverage
	logWarn("test warning", "key3", "value3")
}
