package storage

import (
	"context"
	"testing"

	"printmaster/common/updatepolicy"
)

func TestListTenantSettings(t *testing.T) {
	t.Parallel()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Initially empty
	records, err := store.ListTenantSettings(ctx)
	if err != nil {
		t.Fatalf("ListTenantSettings() error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("Expected 0 records initially, got %d", len(records))
	}

	// Create a tenant
	tenant := &Tenant{ID: "tenant-1", Name: "Test Tenant"}
	if err := store.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Create settings for tenant
	rec := &TenantSettingsRecord{
		TenantID:      "tenant-1",
		SchemaVersion: "1.0",
		Overrides: map[string]interface{}{
			"theme": "dark",
			"limit": 100,
		},
		UpdatedBy: "admin",
	}
	if err := store.UpsertTenantSettings(ctx, rec); err != nil {
		t.Fatalf("UpsertTenantSettings: %v", err)
	}

	// Now list should return one record
	records, err = store.ListTenantSettings(ctx)
	if err != nil {
		t.Fatalf("ListTenantSettings() error after upsert: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	gotRec := records[0]
	if gotRec.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", gotRec.TenantID, "tenant-1")
	}
	if gotRec.UpdatedBy != "admin" {
		t.Errorf("UpdatedBy = %q, want %q", gotRec.UpdatedBy, "admin")
	}
	if val, ok := gotRec.Overrides["theme"]; !ok || val != "dark" {
		t.Errorf("Expected theme=dark in overrides, got %v", gotRec.Overrides)
	}

	// Add second tenant settings
	tenant2 := &Tenant{ID: "tenant-2", Name: "Test Tenant 2"}
	if err := store.CreateTenant(ctx, tenant2); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	rec2 := &TenantSettingsRecord{
		TenantID:      "tenant-2",
		SchemaVersion: "1.0",
		Overrides:     map[string]interface{}{"mode": "advanced"},
		UpdatedBy:     "user2",
	}
	if err := store.UpsertTenantSettings(ctx, rec2); err != nil {
		t.Fatalf("UpsertTenantSettings for tenant-2: %v", err)
	}

	// Now list should return two records
	records, err = store.ListTenantSettings(ctx)
	if err != nil {
		t.Fatalf("ListTenantSettings() error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("Expected 2 records, got %d", len(records))
	}
}

func TestUpsertGlobalFleetUpdatePolicy(t *testing.T) {
	t.Parallel()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Create a global policy using the GlobalFleetPolicyTenantID
	policy := &FleetUpdatePolicy{
		TenantID: GlobalFleetPolicyTenantID,
		PolicySpec: updatepolicy.PolicySpec{
			UpdateCheckDays:    7,
			VersionPinStrategy: updatepolicy.VersionPinMinor,
			AllowMajorUpgrade:  false,
			MaintenanceWindow: updatepolicy.MaintenanceWindow{
				Enabled:    true,
				StartHour:  2,
				StartMin:   0,
				EndHour:    5,
				EndMin:     0,
				Timezone:   "UTC",
				DaysOfWeek: []int{0, 6}, // Sunday=0, Saturday=6
			},
			RolloutControl: updatepolicy.RolloutControl{
				Staggered:         true,
				MaxConcurrent:     5,
				BatchSize:         10,
				DelayBetweenWaves: 300,
				JitterSeconds:     60,
				EmergencyAbort:    true,
			},
			CollectTelemetry: true,
		},
	}

	// UpsertFleetUpdatePolicy should call upsertGlobalFleetUpdatePolicy internally
	err = store.UpsertFleetUpdatePolicy(ctx, policy)
	if err != nil {
		t.Fatalf("UpsertFleetUpdatePolicy(global) error: %v", err)
	}

	// Retrieve it back
	got, err := store.GetFleetUpdatePolicy(ctx, GlobalFleetPolicyTenantID)
	if err != nil {
		t.Fatalf("GetFleetUpdatePolicy(global) error: %v", err)
	}

	if got.UpdateCheckDays != 7 {
		t.Errorf("UpdateCheckDays = %d, want 7", got.UpdateCheckDays)
	}
	if got.VersionPinStrategy != updatepolicy.VersionPinMinor {
		t.Errorf("VersionPinStrategy = %q, want %q", got.VersionPinStrategy, updatepolicy.VersionPinMinor)
	}
	if !got.MaintenanceWindow.Enabled {
		t.Error("MaintenanceWindow.Enabled should be true")
	}
	if got.MaintenanceWindow.StartHour != 2 {
		t.Errorf("MaintenanceWindow.StartHour = %d, want 2", got.MaintenanceWindow.StartHour)
	}
	if !got.RolloutControl.Staggered {
		t.Error("RolloutControl.Staggered should be true")
	}
	if !got.CollectTelemetry {
		t.Error("CollectTelemetry should be true")
	}

	// Update the policy (upsert)
	policy.UpdateCheckDays = 14
	policy.VersionPinStrategy = updatepolicy.VersionPinPatch
	err = store.UpsertFleetUpdatePolicy(ctx, policy)
	if err != nil {
		t.Fatalf("UpsertFleetUpdatePolicy(global) update error: %v", err)
	}

	got2, err := store.GetFleetUpdatePolicy(ctx, GlobalFleetPolicyTenantID)
	if err != nil {
		t.Fatalf("GetFleetUpdatePolicy(global) after update error: %v", err)
	}
	if got2.UpdateCheckDays != 14 {
		t.Errorf("After update, UpdateCheckDays = %d, want 14", got2.UpdateCheckDays)
	}
	if got2.VersionPinStrategy != updatepolicy.VersionPinPatch {
		t.Errorf("After update, VersionPinStrategy = %q, want %q", got2.VersionPinStrategy, updatepolicy.VersionPinPatch)
	}

	// Delete global policy
	err = store.DeleteFleetUpdatePolicy(ctx, GlobalFleetPolicyTenantID)
	if err != nil {
		t.Fatalf("DeleteFleetUpdatePolicy(global) error: %v", err)
	}

	// Get should now return nil (not found) or error, depending on behavior
	got3, err := store.GetFleetUpdatePolicy(ctx, GlobalFleetPolicyTenantID)
	// The global policy returns sql.ErrNoRows wrapped, so we check for nil or error
	if err == nil && got3 != nil {
		t.Error("Expected nil policy after deleting global policy")
	}
}

func TestUpsertGlobalFleetUpdatePolicy_NilPolicy(t *testing.T) {
	t.Parallel()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// UpsertFleetUpdatePolicy with nil should fail
	err = store.UpsertFleetUpdatePolicy(ctx, nil)
	if err == nil {
		t.Error("UpsertFleetUpdatePolicy(nil) should return error")
	}
}
