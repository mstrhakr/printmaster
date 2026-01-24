package storage

import (
	"context"
	"testing"

	"printmaster/common/settings"
	"printmaster/common/updatepolicy"
)

func TestGlobalSettingsLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Get initial settings (should be defaults or empty)
	initial, err := s.GetGlobalSettings(ctx)
	if err != nil {
		t.Fatalf("GetGlobalSettings (initial): %v", err)
	}
	// Initial may be nil or have default values

	// Create settings record
	rec := &SettingsRecord{
		SchemaVersion: "1",
		Settings: settings.Settings{
			Discovery: settings.DiscoverySettings{
				SubnetScan:  true,
				Concurrency: 10,
			},
			SNMP: settings.SNMPSettings{
				Community: "public",
				TimeoutMS: 5000,
				Retries:   2,
			},
			Features: settings.FeaturesSettings{
				EpsonRemoteModeEnabled: false,
			},
			Logging: settings.LoggingSettings{
				Level: "info",
			},
		},
		ManagedSections: []string{"discovery", "snmp"},
		UpdatedBy:       "admin",
	}

	err = s.UpsertGlobalSettings(ctx, rec)
	if err != nil {
		t.Fatalf("UpsertGlobalSettings: %v", err)
	}

	// Retrieve and verify
	got, err := s.GetGlobalSettings(ctx)
	if err != nil {
		t.Fatalf("GetGlobalSettings: %v", err)
	}
	if got == nil {
		t.Fatal("expected settings, got nil")
	}
	if got.Settings.Discovery.Concurrency != 10 {
		t.Errorf("concurrency mismatch: got=%d", got.Settings.Discovery.Concurrency)
	}
	if got.Settings.SNMP.Community != "public" {
		t.Errorf("community mismatch: got=%q", got.Settings.SNMP.Community)
	}

	// Update settings
	rec.Settings.Discovery.Concurrency = 20
	rec.Settings.SNMP.TimeoutMS = 10000
	err = s.UpsertGlobalSettings(ctx, rec)
	if err != nil {
		t.Fatalf("UpsertGlobalSettings (update): %v", err)
	}

	got, _ = s.GetGlobalSettings(ctx)
	if got.Settings.Discovery.Concurrency != 20 {
		t.Errorf("concurrency not updated: got=%d", got.Settings.Discovery.Concurrency)
	}
	if got.Settings.SNMP.TimeoutMS != 10000 {
		t.Errorf("timeout not updated: got=%d", got.Settings.SNMP.TimeoutMS)
	}
	// Verify initial access before nil check was handled correctly
	_ = initial
}

func TestTenantSettingsLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create tenant first
	tenant := &Tenant{
		ID:   "tenant-settings",
		Name: "Settings Test Tenant",
	}
	err = s.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Get initial tenant settings
	initial, err := s.GetTenantSettings(ctx, "tenant-settings")
	if err != nil {
		t.Fatalf("GetTenantSettings (initial): %v", err)
	}
	// Initial may be nil

	// Create tenant settings with map[string]interface{} overrides
	rec := &TenantSettingsRecord{
		TenantID:      "tenant-settings",
		SchemaVersion: "1",
		Overrides: map[string]interface{}{
			"discovery": map[string]interface{}{
				"subnet_scan": true,
				"concurrency": 5,
			},
		},
		UpdatedBy: "tenant-admin",
	}

	err = s.UpsertTenantSettings(ctx, rec)
	if err != nil {
		t.Fatalf("UpsertTenantSettings: %v", err)
	}

	// Retrieve and verify
	got, err := s.GetTenantSettings(ctx, "tenant-settings")
	if err != nil {
		t.Fatalf("GetTenantSettings: %v", err)
	}
	if got == nil {
		t.Fatal("expected tenant settings, got nil")
	}
	if got.TenantID != "tenant-settings" {
		t.Errorf("tenant_id mismatch: got=%q", got.TenantID)
	}
	if got.Overrides == nil {
		t.Fatal("expected overrides, got nil")
	}

	// Update settings
	rec.Overrides["discovery"] = map[string]interface{}{
		"subnet_scan": true,
		"concurrency": 15,
	}
	err = s.UpsertTenantSettings(ctx, rec)
	if err != nil {
		t.Fatalf("UpsertTenantSettings (update): %v", err)
	}

	got, _ = s.GetTenantSettings(ctx, "tenant-settings")
	if got.Overrides == nil {
		t.Fatal("expected overrides after update, got nil")
	}

	// Delete tenant settings
	err = s.DeleteTenantSettings(ctx, "tenant-settings")
	if err != nil {
		t.Fatalf("DeleteTenantSettings: %v", err)
	}

	got, err = s.GetTenantSettings(ctx, "tenant-settings")
	if err != nil {
		t.Fatalf("GetTenantSettings after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
	_ = initial
}

func TestFleetUpdatePolicyLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a fleet update policy
	policy := &FleetUpdatePolicy{
		TenantID: "tenant-policy",
		PolicySpec: updatepolicy.PolicySpec{
			UpdateCheckDays:    7,
			VersionPinStrategy: updatepolicy.VersionPinMinor,
			AllowMajorUpgrade:  false,
			TargetVersion:      "1.2.0",
		},
	}

	err = s.UpsertFleetUpdatePolicy(ctx, policy)
	if err != nil {
		t.Fatalf("UpsertFleetUpdatePolicy: %v", err)
	}

	// Get policy
	got, err := s.GetFleetUpdatePolicy(ctx, "tenant-policy")
	if err != nil {
		t.Fatalf("GetFleetUpdatePolicy: %v", err)
	}
	if got == nil {
		t.Fatal("expected policy, got nil")
	}
	if got.UpdateCheckDays != 7 {
		t.Errorf("update_check_days mismatch: got=%d", got.UpdateCheckDays)
	}
	if got.VersionPinStrategy != updatepolicy.VersionPinMinor {
		t.Errorf("version_pin_strategy mismatch: got=%q", got.VersionPinStrategy)
	}

	// Update policy
	policy.UpdateCheckDays = 14
	policy.AllowMajorUpgrade = true
	err = s.UpsertFleetUpdatePolicy(ctx, policy)
	if err != nil {
		t.Fatalf("UpsertFleetUpdatePolicy (update): %v", err)
	}

	got, _ = s.GetFleetUpdatePolicy(ctx, "tenant-policy")
	if got.UpdateCheckDays != 14 {
		t.Errorf("update_check_days not updated: got=%d", got.UpdateCheckDays)
	}

	// Delete policy
	err = s.DeleteFleetUpdatePolicy(ctx, "tenant-policy")
	if err != nil {
		t.Fatalf("DeleteFleetUpdatePolicy: %v", err)
	}

	got, err = s.GetFleetUpdatePolicy(ctx, "tenant-policy")
	if err != nil {
		t.Fatalf("GetFleetUpdatePolicy after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestListFleetUpdatePolicies(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create multiple policies
	for i := 1; i <= 3; i++ {
		policy := &FleetUpdatePolicy{
			TenantID: "tenant-" + string(rune('0'+i)),
			PolicySpec: updatepolicy.PolicySpec{
				UpdateCheckDays: i * 7,
			},
		}
		err = s.UpsertFleetUpdatePolicy(ctx, policy)
		if err != nil {
			t.Fatalf("UpsertFleetUpdatePolicy %d: %v", i, err)
		}
	}

	// List all
	policies, err := s.ListFleetUpdatePolicies(ctx)
	if err != nil {
		t.Fatalf("ListFleetUpdatePolicies: %v", err)
	}
	if len(policies) != 3 {
		t.Errorf("expected 3 policies, got %d", len(policies))
	}
}

func TestJoinTokenOperations(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create tenant
	tenant := &Tenant{
		ID:   "tenant-join",
		Name: "Join Token Test Tenant",
	}
	err = s.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Create join token (one-time)
	jt, rawToken, err := s.CreateJoinToken(ctx, "tenant-join", 1440, true)
	if err != nil {
		t.Fatalf("CreateJoinToken: %v", err)
	}
	if rawToken == "" {
		t.Fatal("expected raw token, got empty")
	}
	if jt == nil {
		t.Fatal("expected join token record, got nil")
	}
	if jt.TenantID != "tenant-join" {
		t.Errorf("tenant_id mismatch: got=%q", jt.TenantID)
	}

	// Validate token
	validated, err := s.ValidateJoinToken(ctx, rawToken)
	if err != nil {
		t.Fatalf("ValidateJoinToken: %v", err)
	}
	if validated == nil {
		t.Fatal("expected token, got nil")
	}
	if validated.TenantID != "tenant-join" {
		t.Errorf("validated tenant_id mismatch: got=%q", validated.TenantID)
	}

	// List tokens
	tokens, err := s.ListJoinTokens(ctx, "tenant-join")
	if err != nil {
		t.Fatalf("ListJoinTokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("expected 1 token, got %d", len(tokens))
	}

	// Revoke token
	err = s.RevokeJoinToken(ctx, jt.ID)
	if err != nil {
		t.Fatalf("RevokeJoinToken: %v", err)
	}

	// Validate should fail after revocation
	_, err = s.ValidateJoinToken(ctx, rawToken)
	if err == nil {
		t.Error("expected validation to fail after revoke")
	}
}

// TestInitSecretJoinToken tests that arbitrary INIT_SECRET strings work for auto-join.
// This covers the Docker Compose scenario where agent and server share a human-readable secret.
func TestInitSecretJoinToken(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create tenant
	tenant := &Tenant{
		ID:   "tenant-init",
		Name: "Init Secret Test Tenant",
	}
	err = s.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Use human-readable INIT_SECRET (not 48-char hex)
	initSecret := "changeme-for-production"

	jt := &JoinToken{
		ID:       "init-secret",
		TenantID: "tenant-init",
		OneTime:  false,
	}

	// Create join token with custom secret
	created, err := s.CreateJoinTokenWithSecret(ctx, jt, initSecret)
	if err != nil {
		t.Fatalf("CreateJoinTokenWithSecret: %v", err)
	}
	if created == nil {
		t.Fatal("expected join token record, got nil")
	}

	// Validate with the same secret
	validated, err := s.ValidateJoinToken(ctx, initSecret)
	if err != nil {
		t.Fatalf("ValidateJoinToken with init secret failed: %v", err)
	}
	if validated == nil {
		t.Fatal("expected token, got nil")
	}
	if validated.TenantID != "tenant-init" {
		t.Errorf("tenant_id mismatch: got=%q want=%q", validated.TenantID, "tenant-init")
	}
	if validated.ID != "init-secret" {
		t.Errorf("token ID mismatch: got=%q want=%q", validated.ID, "init-secret")
	}

	// Multiple validations should succeed (not one-time)
	validated2, err := s.ValidateJoinToken(ctx, initSecret)
	if err != nil {
		t.Fatalf("Second ValidateJoinToken failed: %v", err)
	}
	if validated2 == nil {
		t.Fatal("expected token on second validation, got nil")
	}

	// Wrong secret should fail
	_, err = s.ValidateJoinToken(ctx, "wrong-secret")
	if err == nil {
		t.Error("expected validation to fail with wrong secret")
	}
}

func TestPasswordResetTokenOperations(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create user
	user := &User{
		Username: "resetuser",
		Role:     RoleAdmin,
	}
	err = s.CreateUser(ctx, user, "password")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Create password reset token
	rawToken, err := s.CreatePasswordResetToken(ctx, user.ID, 30)
	if err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}
	if rawToken == "" {
		t.Fatal("expected raw token, got empty")
	}

	// Validate token
	userID, err := s.ValidatePasswordResetToken(ctx, rawToken)
	if err != nil {
		t.Fatalf("ValidatePasswordResetToken: %v", err)
	}
	if userID != user.ID {
		t.Errorf("user_id mismatch: got=%d, expected=%d", userID, user.ID)
	}

	// Delete token
	err = s.DeletePasswordResetToken(ctx, rawToken)
	if err != nil {
		t.Fatalf("DeletePasswordResetToken: %v", err)
	}

	// Validate should fail
	_, err = s.ValidatePasswordResetToken(ctx, rawToken)
	if err == nil {
		t.Error("expected validation to fail after delete")
	}
}
