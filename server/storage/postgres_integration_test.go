//go:build integration

package storage

import (
	"context"
	"testing"
	"time"

	commonstorage "printmaster/common/storage"
	"printmaster/common/updatepolicy"
)

// TestPostgresStore_Integration runs the full store test suite against Postgres
func TestPostgresStore_Integration(t *testing.T) {
	WithPostgresStore(t, func(t *testing.T, store *PostgresStore) {
		ctx := context.Background()

		// Test basic connectivity
		t.Run("Path", func(t *testing.T) {
			// PostgresStore.Path() returns empty string by design since
			// PostgreSQL doesn't use file paths like SQLite
			path := store.Path()
			// Just log it - empty is expected for Postgres
			t.Logf("Postgres Path (expected empty): %q", path)
		})

		// Test agent operations
		t.Run("AgentLifecycle", func(t *testing.T) {
			agent := &Agent{
				AgentID: "test-agent-pg",
				Name:    "Test Agent Postgres",
				Token:   "test-token-pg-12345",
				Version: "1.0.0",
				Status:  "online",
			}

			// Register
			err := store.RegisterAgent(ctx, agent)
			if err != nil {
				t.Fatalf("RegisterAgent: %v", err)
			}

			// Get by AgentID
			got, err := store.GetAgent(ctx, "test-agent-pg")
			if err != nil {
				t.Fatalf("GetAgent: %v", err)
			}
			if got.Name != "Test Agent Postgres" {
				t.Errorf("Name = %q, want %q", got.Name, "Test Agent Postgres")
			}

			// Update heartbeat
			err = store.UpdateAgentHeartbeat(ctx, "test-agent-pg", "online")
			if err != nil {
				t.Fatalf("UpdateAgentHeartbeat: %v", err)
			}

			// Update info
			updatedAgent := &Agent{
				AgentID:       "test-agent-pg",
				Version:       "1.1.0",
				OSVersion:     "Windows 11",
				GoVersion:     "go1.24",
				Architecture:  "amd64",
				NumCPU:        8,
				TotalMemoryMB: 16384,
				BuildType:     "dev",
				GitCommit:     "abc123",
			}
			err = store.UpdateAgentInfo(ctx, updatedAgent)
			if err != nil {
				t.Fatalf("UpdateAgentInfo: %v", err)
			}

			// List agents
			agents, err := store.ListAgents(ctx)
			if err != nil {
				t.Fatalf("ListAgents: %v", err)
			}
			if len(agents) == 0 {
				t.Error("ListAgents returned empty list")
			}

			// Delete
			err = store.DeleteAgent(ctx, "test-agent-pg")
			if err != nil {
				t.Fatalf("DeleteAgent: %v", err)
			}
		})

		// Test device operations
		t.Run("DeviceLifecycle", func(t *testing.T) {
			// First create an agent to own the device
			agent := &Agent{
				AgentID: "device-test-agent",
				Name:    "Device Test Agent",
				Token:   "device-test-token",
				Status:  "online",
			}
			if err := store.RegisterAgent(ctx, agent); err != nil {
				t.Fatalf("RegisterAgent for device test: %v", err)
			}

			device := &Device{
				Device: commonstorage.Device{
					Serial:       "SN-PG-001",
					IP:           "192.168.1.100",
					Manufacturer: "HP",
					Model:        "LaserJet Pro",
				},
				AgentID: "device-test-agent",
			}

			// Upsert
			err := store.UpsertDevice(ctx, device)
			if err != nil {
				t.Fatalf("UpsertDevice: %v", err)
			}

			// Get by serial
			got, err := store.GetDevice(ctx, "SN-PG-001")
			if err != nil {
				t.Fatalf("GetDevice: %v", err)
			}
			if got.Serial != "SN-PG-001" {
				t.Errorf("Serial = %q, want %q", got.Serial, "SN-PG-001")
			}
			if got.Manufacturer != "HP" {
				t.Errorf("Manufacturer = %q, want %q", got.Manufacturer, "HP")
			}

			// List
			devices, err := store.ListDevices(ctx, "device-test-agent")
			if err != nil {
				t.Fatalf("ListDevices: %v", err)
			}
			if len(devices) != 1 {
				t.Errorf("ListDevices returned %d devices, want 1", len(devices))
			}
		})

		// Test metrics
		t.Run("MetricsLifecycle", func(t *testing.T) {
			metrics := &MetricsSnapshot{
				Serial:    "SN-PG-001",
				AgentID:   "device-test-agent",
				Timestamp: time.Now().UTC(),
				PageCount: 1000,
			}

			err := store.SaveMetrics(ctx, metrics)
			if err != nil {
				t.Fatalf("SaveMetrics: %v", err)
			}

			got, err := store.GetLatestMetrics(ctx, "SN-PG-001")
			if err != nil {
				t.Fatalf("GetLatestMetrics: %v", err)
			}
			if got == nil {
				t.Fatal("GetLatestMetrics returned nil")
			}
			if got.PageCount != 1000 {
				t.Errorf("PageCount = %d, want 1000", got.PageCount)
			}
		})

		// Test tenant operations
		t.Run("TenantLifecycle", func(t *testing.T) {
			tenant := &Tenant{
				ID:   "tenant-pg-1",
				Name: "Test Tenant PG",
			}

			err := store.CreateTenant(ctx, tenant)
			if err != nil {
				t.Fatalf("CreateTenant: %v", err)
			}

			got, err := store.GetTenant(ctx, "tenant-pg-1")
			if err != nil {
				t.Fatalf("GetTenant: %v", err)
			}
			if got.Name != "Test Tenant PG" {
				t.Errorf("Name = %q, want %q", got.Name, "Test Tenant PG")
			}

			// Update
			tenant.Name = "Updated Tenant PG"
			err = store.UpdateTenant(ctx, tenant)
			if err != nil {
				t.Fatalf("UpdateTenant: %v", err)
			}

			// List
			tenants, err := store.ListTenants(ctx)
			if err != nil {
				t.Fatalf("ListTenants: %v", err)
			}
			if len(tenants) == 0 {
				t.Error("ListTenants returned empty")
			}
		})

		// Test user operations
		t.Run("UserLifecycle", func(t *testing.T) {
			user := &User{
				Username: "testuser_pg",
				Role:     RoleAdmin,
				Email:    "test@example.com",
			}

			err := store.CreateUser(ctx, user, "testpassword123")
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}

			got, err := store.GetUserByUsername(ctx, "testuser_pg")
			if err != nil {
				t.Fatalf("GetUserByUsername: %v", err)
			}
			if got.Email != "test@example.com" {
				t.Errorf("Email = %q, want %q", got.Email, "test@example.com")
			}

			// Update password (use the ID from the created user)
			err = store.UpdateUserPassword(ctx, got.ID, "newpassword456")
			if err != nil {
				t.Fatalf("UpdateUserPassword: %v", err)
			}

			// Delete
			err = store.DeleteUser(ctx, got.ID)
			if err != nil {
				t.Fatalf("DeleteUser: %v", err)
			}
		})

		// Test session operations
		t.Run("SessionLifecycle", func(t *testing.T) {
			// Create user first
			user := &User{
				Username: "sessionuser_pg",
				Role:     RoleViewer, // Use RoleViewer instead of RoleUser
			}
			if err := store.CreateUser(ctx, user, "sessionpass"); err != nil {
				t.Fatalf("CreateUser for session test: %v", err)
			}

			// Get the user to get their ID
			createdUser, err := store.GetUserByUsername(ctx, "sessionuser_pg")
			if err != nil {
				t.Fatalf("GetUserByUsername: %v", err)
			}

			// Create session (returns session with token)
			session, err := store.CreateSession(ctx, createdUser.ID, 60) // 60 minutes
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			if session.Token == "" {
				t.Error("CreateSession returned empty token")
			}

			got, err := store.GetSessionByToken(ctx, session.Token)
			if err != nil {
				t.Fatalf("GetSessionByToken: %v", err)
			}
			if got.UserID != createdUser.ID {
				t.Errorf("UserID = %d, want %d", got.UserID, createdUser.ID)
			}

			// Delete session by token
			err = store.DeleteSession(ctx, session.Token)
			if err != nil {
				t.Fatalf("DeleteSession: %v", err)
			}
		})

		// Test fleet update policy
		t.Run("FleetUpdatePolicy", func(t *testing.T) {
			// Create tenant first
			tenant := &Tenant{ID: "policy-tenant-pg", Name: "Policy Tenant"}
			if err := store.CreateTenant(ctx, tenant); err != nil {
				t.Fatalf("CreateTenant: %v", err)
			}

			policy := &FleetUpdatePolicy{
				TenantID: "policy-tenant-pg",
				PolicySpec: updatepolicy.PolicySpec{
					UpdateCheckDays:    7,
					VersionPinStrategy: updatepolicy.VersionPinMinor,
				},
			}

			err := store.UpsertFleetUpdatePolicy(ctx, policy)
			if err != nil {
				t.Fatalf("UpsertFleetUpdatePolicy: %v", err)
			}

			got, err := store.GetFleetUpdatePolicy(ctx, "policy-tenant-pg")
			if err != nil {
				t.Fatalf("GetFleetUpdatePolicy: %v", err)
			}
			if got.UpdateCheckDays != 7 {
				t.Errorf("UpdateCheckDays = %d, want 7", got.UpdateCheckDays)
			}

			// Delete
			err = store.DeleteFleetUpdatePolicy(ctx, "policy-tenant-pg")
			if err != nil {
				t.Fatalf("DeleteFleetUpdatePolicy: %v", err)
			}
		})

		// Test global settings
		t.Run("GlobalSettings", func(t *testing.T) {
			settings, err := store.GetGlobalSettings(ctx)
			if err != nil {
				t.Fatalf("GetGlobalSettings: %v", err)
			}
			// Should have default settings
			if settings == nil {
				t.Error("GetGlobalSettings returned nil")
			}
		})

		// Test reports
		t.Run("ReportLifecycle", func(t *testing.T) {
			report := &ReportDefinition{
				Name:        "Test Report PG",
				Description: "A test report",
				Type:        string(ReportTypeDeviceInventory),
				Format:      string(ReportFormatJSON),
				Scope:       string(ReportScopeFleet),
			}

			err := store.CreateReport(ctx, report)
			if err != nil {
				t.Fatalf("CreateReport: %v", err)
			}
			if report.ID == 0 {
				t.Error("CreateReport did not set ID")
			}

			got, err := store.GetReport(ctx, report.ID)
			if err != nil {
				t.Fatalf("GetReport: %v", err)
			}
			if got.Name != "Test Report PG" {
				t.Errorf("Name = %q, want %q", got.Name, "Test Report PG")
			}

			// Delete
			err = store.DeleteReport(ctx, report.ID)
			if err != nil {
				t.Fatalf("DeleteReport: %v", err)
			}
		})

		// Test alerts
		t.Run("AlertLifecycle", func(t *testing.T) {
			alert := &Alert{
				DeviceSerial: "SN-PG-001",
				Type:         "low_toner",
				Severity:     "warning",
				Message:      "Toner low",
				Status:       "active",
			}

			id, err := store.CreateAlert(ctx, alert)
			if err != nil {
				t.Fatalf("CreateAlert: %v", err)
			}
			if id == 0 {
				t.Error("CreateAlert returned ID 0")
			}

			got, err := store.GetAlert(ctx, id)
			if err != nil {
				t.Fatalf("GetAlert: %v", err)
			}
			if got.Type != "low_toner" {
				t.Errorf("Type = %q, want %q", got.Type, "low_toner")
			}

			// Update status
			err = store.UpdateAlertStatus(ctx, id, "acknowledged")
			if err != nil {
				t.Fatalf("UpdateAlertStatus: %v", err)
			}

			// Resolve
			err = store.ResolveAlert(ctx, id)
			if err != nil {
				t.Fatalf("ResolveAlert: %v", err)
			}
		})
	})
}

// TestPostgresStore_NewPostgresStore tests the constructor
func TestPostgresStore_NewPostgresStore(t *testing.T) {
	WithPostgresStore(t, func(t *testing.T, store *PostgresStore) {
		// Just verify the store was created successfully
		if store == nil {
			t.Error("NewPostgresStore returned nil")
		}

		// Verify we can query the database
		ctx := context.Background()
		_, err := store.ListTenants(ctx)
		if err != nil {
			t.Errorf("ListTenants failed: %v", err)
		}
	})
}

// TestPostgresStore_Close tests the Close method
func TestPostgresStore_Close(t *testing.T) {
	SkipIfNoDocker(t)

	container, cleanup := NewPostgresTestContainer(t)
	defer cleanup()

	store := NewPostgresStoreFromContainer(t, container)

	// Close should succeed
	err := store.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	_, err = store.ListTenants(ctx)
	if err == nil {
		t.Error("Expected error after Close(), got nil")
	}
}
