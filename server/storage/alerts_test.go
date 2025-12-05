package storage

import (
	"context"
	"testing"
	"time"
)

func TestAlertLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create an alert
	alert := &Alert{
		Type:         AlertTypeDeviceOffline,
		Severity:     AlertSeverityWarning,
		Scope:        AlertScopeDevice,
		Status:       AlertStatusActive,
		DeviceSerial: "SN12345",
		AgentID:      "agent-uuid-1",
		Title:        "Device Offline",
		Message:      "Device SN12345 has not reported in 10 minutes",
		TriggeredAt:  time.Now().UTC(),
	}

	id, err := s.CreateAlert(ctx, alert)
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero alert ID")
	}

	// Retrieve the alert
	got, err := s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if got.Title != alert.Title {
		t.Errorf("title mismatch: got=%q want=%q", got.Title, alert.Title)
	}
	if got.Status != AlertStatusActive {
		t.Errorf("status mismatch: got=%q want=%q", got.Status, AlertStatusActive)
	}

	// List active alerts
	alerts, err := s.ListActiveAlerts(ctx, AlertFilters{})
	if err != nil {
		t.Fatalf("ListActiveAlerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 active alert, got %d", len(alerts))
	}

	// Acknowledge the alert
	err = s.AcknowledgeAlert(ctx, id, "test-user")
	if err != nil {
		t.Fatalf("AcknowledgeAlert: %v", err)
	}

	got, err = s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert after acknowledge: %v", err)
	}
	if got.Status != AlertStatusAcknowledged {
		t.Errorf("expected acknowledged status, got %q", got.Status)
	}
	if got.AcknowledgedBy != "test-user" {
		t.Errorf("acknowledged_by mismatch: got=%q want=%q", got.AcknowledgedBy, "test-user")
	}

	// Resolve the alert
	err = s.ResolveAlert(ctx, id)
	if err != nil {
		t.Fatalf("ResolveAlert: %v", err)
	}

	got, err = s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert after resolve: %v", err)
	}
	if got.Status != AlertStatusResolved {
		t.Errorf("expected resolved status, got %q", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Error("expected resolved_at to be set")
	}

	// Active alerts should now be empty
	alerts, err = s.ListActiveAlerts(ctx, AlertFilters{})
	if err != nil {
		t.Fatalf("ListActiveAlerts after resolve: %v", err)
	}
	if len(alerts) != 0 {
		t.Errorf("expected 0 active alerts, got %d", len(alerts))
	}
}

func TestAlertFilters(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create alerts with different severities and scopes
	alerts := []*Alert{
		{
			Type:         AlertTypeSupplyLow,
			Severity:     AlertSeverityWarning,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive,
			DeviceSerial: "DEV001",
			Title:        "Low Toner",
		},
		{
			Type:         AlertTypeSupplyCritical,
			Severity:     AlertSeverityCritical,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive,
			DeviceSerial: "DEV002",
			Title:        "Critical Toner",
		},
		{
			Type:     AlertTypeAgentOffline,
			Severity: AlertSeverityCritical,
			Scope:    AlertScopeAgent,
			Status:   AlertStatusActive,
			AgentID:  "agent-1",
			Title:    "Agent Offline",
		},
	}

	for _, a := range alerts {
		a.TriggeredAt = time.Now().UTC()
		if _, err := s.CreateAlert(ctx, a); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
	}

	// Filter by severity
	critical, err := s.ListActiveAlerts(ctx, AlertFilters{Severity: AlertSeverityCritical})
	if err != nil {
		t.Fatalf("ListActiveAlerts critical: %v", err)
	}
	if len(critical) != 2 {
		t.Errorf("expected 2 critical alerts, got %d", len(critical))
	}

	// Filter by scope
	deviceAlerts, err := s.ListActiveAlerts(ctx, AlertFilters{Scope: AlertScopeDevice})
	if err != nil {
		t.Fatalf("ListActiveAlerts device: %v", err)
	}
	if len(deviceAlerts) != 2 {
		t.Errorf("expected 2 device alerts, got %d", len(deviceAlerts))
	}

	// Filter by type
	supplyAlerts, err := s.ListActiveAlerts(ctx, AlertFilters{Type: AlertTypeSupplyLow})
	if err != nil {
		t.Fatalf("ListActiveAlerts supply low: %v", err)
	}
	if len(supplyAlerts) != 1 {
		t.Errorf("expected 1 supply low alert, got %d", len(supplyAlerts))
	}
}

func TestAlertRuleLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	rule := &AlertRule{
		Name:            "Low Toner Alert",
		Description:     "Alert when toner drops below 20%",
		Enabled:         true,
		Type:            AlertTypeSupplyLow,
		Severity:        AlertSeverityWarning,
		Scope:           AlertScopeDevice,
		Threshold:       20.0,
		ThresholdUnit:   "percent",
		DurationMinutes: 0,
	}

	id, err := s.CreateAlertRule(ctx, rule)
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero rule ID")
	}

	// Retrieve the rule
	got, err := s.GetAlertRule(ctx, id)
	if err != nil {
		t.Fatalf("GetAlertRule: %v", err)
	}
	if got.Name != rule.Name {
		t.Errorf("name mismatch: got=%q want=%q", got.Name, rule.Name)
	}
	if got.Threshold != 20.0 {
		t.Errorf("threshold mismatch: got=%f want=%f", got.Threshold, 20.0)
	}

	// Update the rule
	got.Threshold = 15.0
	got.Enabled = false
	err = s.UpdateAlertRule(ctx, got)
	if err != nil {
		t.Fatalf("UpdateAlertRule: %v", err)
	}

	updated, err := s.GetAlertRule(ctx, id)
	if err != nil {
		t.Fatalf("GetAlertRule after update: %v", err)
	}
	if updated.Threshold != 15.0 {
		t.Errorf("threshold not updated: got=%f want=%f", updated.Threshold, 15.0)
	}
	if updated.Enabled != false {
		t.Error("enabled not updated to false")
	}

	// List rules
	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("ListAlertRules: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}

	// Delete the rule
	err = s.DeleteAlertRule(ctx, id)
	if err != nil {
		t.Fatalf("DeleteAlertRule: %v", err)
	}

	rules, err = s.ListAlertRules(ctx)
	if err != nil {
		t.Fatalf("ListAlertRules after delete: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestNotificationChannelLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	channel := &NotificationChannel{
		Name:             "Slack Alerts",
		Type:             ChannelTypeSlack,
		Enabled:          true,
		ConfigJSON:       `{"webhook_url": "https://hooks.slack.com/xxx"}`,
		MinSeverity:      AlertSeverityWarning,
		RateLimitPerHour: 10,
		UseQuietHours:    true,
	}

	id, err := s.CreateNotificationChannel(ctx, channel)
	if err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero channel ID")
	}

	// Retrieve the channel
	got, err := s.GetNotificationChannel(ctx, id)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if got.Name != channel.Name {
		t.Errorf("name mismatch: got=%q want=%q", got.Name, channel.Name)
	}
	if got.Type != ChannelTypeSlack {
		t.Errorf("type mismatch: got=%q want=%q", got.Type, ChannelTypeSlack)
	}

	// List channels
	channels, err := s.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}

	// Delete the channel
	err = s.DeleteNotificationChannel(ctx, id)
	if err != nil {
		t.Fatalf("DeleteNotificationChannel: %v", err)
	}

	channels, err = s.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("ListNotificationChannels after delete: %v", err)
	}
	if len(channels) != 0 {
		t.Errorf("expected 0 channels after delete, got %d", len(channels))
	}
}

func TestMaintenanceWindowLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	now := time.Now().UTC()
	window := &AlertMaintenanceWindow{
		Name:        "Weekend Maintenance",
		Description: "Suppress alerts during weekend maintenance",
		Scope:       AlertScopeFleet,
		StartTime:   now.Add(-1 * time.Hour),
		EndTime:     now.Add(2 * time.Hour),
		CreatedBy:   "admin",
	}

	id, err := s.CreateAlertMaintenanceWindow(ctx, window)
	if err != nil {
		t.Fatalf("CreateAlertMaintenanceWindow: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero window ID")
	}

	// List all windows
	windows, err := s.ListAlertMaintenanceWindows(ctx)
	if err != nil {
		t.Fatalf("ListAlertMaintenanceWindows: %v", err)
	}
	if len(windows) != 1 {
		t.Errorf("expected 1 window, got %d", len(windows))
	}

	// Get active windows (should include our window)
	active, err := s.GetActiveAlertMaintenanceWindows(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlertMaintenanceWindows: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active window, got %d", len(active))
	}

	// Create a past window
	pastWindow := &AlertMaintenanceWindow{
		Name:      "Past Maintenance",
		Scope:     AlertScopeFleet,
		StartTime: now.Add(-5 * time.Hour),
		EndTime:   now.Add(-4 * time.Hour),
		CreatedBy: "admin",
	}
	_, err = s.CreateAlertMaintenanceWindow(ctx, pastWindow)
	if err != nil {
		t.Fatalf("CreateAlertMaintenanceWindow (past): %v", err)
	}

	// Active windows should still be 1
	active, err = s.GetActiveAlertMaintenanceWindows(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlertMaintenanceWindows: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active window (past excluded), got %d", len(active))
	}

	// Delete the active window
	err = s.DeleteAlertMaintenanceWindow(ctx, id)
	if err != nil {
		t.Fatalf("DeleteAlertMaintenanceWindow: %v", err)
	}

	active, err = s.GetActiveAlertMaintenanceWindows(ctx)
	if err != nil {
		t.Fatalf("GetActiveAlertMaintenanceWindows after delete: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active windows after delete, got %d", len(active))
	}
}

func TestAlertSettings(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Get default settings (should return defaults)
	settings, err := s.GetAlertSettings(ctx)
	if err != nil {
		t.Fatalf("GetAlertSettings: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil settings")
	}

	// Update settings
	settings.QuietHours.Enabled = true
	settings.QuietHours.StartTime = "22:00"
	settings.QuietHours.EndTime = "06:00"
	settings.FlappingEnabled = true
	settings.FlappingThreshold = 5

	err = s.SaveAlertSettings(ctx, settings)
	if err != nil {
		t.Fatalf("SaveAlertSettings: %v", err)
	}

	// Retrieve and verify
	updated, err := s.GetAlertSettings(ctx)
	if err != nil {
		t.Fatalf("GetAlertSettings after save: %v", err)
	}
	if !updated.QuietHours.Enabled {
		t.Error("quiet hours not enabled")
	}
	if updated.QuietHours.StartTime != "22:00" {
		t.Errorf("quiet hours start mismatch: got=%q want=%q", updated.QuietHours.StartTime, "22:00")
	}
	if !updated.FlappingEnabled {
		t.Error("flapping not enabled")
	}
}

func TestAlertNotificationStatusUpdate(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	alert := &Alert{
		Type:         AlertTypeDeviceError,
		Severity:     AlertSeverityCritical,
		Scope:        AlertScopeDevice,
		Status:       AlertStatusActive,
		DeviceSerial: "ERR001",
		Title:        "Device Error",
		TriggeredAt:  time.Now().UTC(),
	}

	id, err := s.CreateAlert(ctx, alert)
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	// Update notification status
	now := time.Now().UTC()
	err = s.UpdateAlertNotificationStatus(ctx, id, 3, now)
	if err != nil {
		t.Fatalf("UpdateAlertNotificationStatus: %v", err)
	}

	// Verify update
	got, err := s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if got.NotificationsSent != 3 {
		t.Errorf("notifications_sent mismatch: got=%d want=%d", got.NotificationsSent, 3)
	}
	if got.LastNotifiedAt == nil {
		t.Error("expected last_notified_at to be set")
	}
}

func TestEscalationPolicyLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	policy := &EscalationPolicy{
		Name:        "Default Escalation",
		Description: "Escalate to management after 30 minutes",
		Enabled:     true,
		StepsJSON:   `[{"delay_minutes": 30, "channel_ids": [1, 2]}]`,
	}

	id, err := s.CreateEscalationPolicy(ctx, policy)
	if err != nil {
		t.Fatalf("CreateEscalationPolicy: %v", err)
	}
	if id == 0 {
		t.Fatalf("expected non-zero policy ID")
	}

	// List policies
	policies, err := s.ListEscalationPolicies(ctx)
	if err != nil {
		t.Fatalf("ListEscalationPolicies: %v", err)
	}
	if len(policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(policies))
	}
	if policies[0].Name != policy.Name {
		t.Errorf("name mismatch: got=%q want=%q", policies[0].Name, policy.Name)
	}

	// Delete the policy
	err = s.DeleteEscalationPolicy(ctx, id)
	if err != nil {
		t.Fatalf("DeleteEscalationPolicy: %v", err)
	}

	policies, err = s.ListEscalationPolicies(ctx)
	if err != nil {
		t.Fatalf("ListEscalationPolicies after delete: %v", err)
	}
	if len(policies) != 0 {
		t.Errorf("expected 0 policies after delete, got %d", len(policies))
	}
}

func TestAlertSummary(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create some alerts for summary
	alerts := []*Alert{
		{
			Type:         AlertTypeDeviceOffline,
			Severity:     AlertSeverityWarning,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive,
			DeviceSerial: "DEV001",
			Title:        "Device Offline 1",
			TriggeredAt:  time.Now().UTC(),
		},
		{
			Type:         AlertTypeSupplyCritical,
			Severity:     AlertSeverityCritical,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive,
			DeviceSerial: "DEV002",
			Title:        "Critical Toner",
			TriggeredAt:  time.Now().UTC(),
		},
		{
			Type:        AlertTypeAgentOffline,
			Severity:    AlertSeverityCritical,
			Scope:       AlertScopeAgent,
			Status:      AlertStatusActive,
			AgentID:     "agent-1",
			Title:       "Agent Offline",
			TriggeredAt: time.Now().UTC(),
		},
	}

	for _, a := range alerts {
		if _, err := s.CreateAlert(ctx, a); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
	}

	summary, err := s.GetAlertSummary(ctx)
	if err != nil {
		t.Fatalf("GetAlertSummary: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}

	// Check alert counts by type
	if summary.AlertsByType == nil {
		t.Error("expected alerts_by_type to be populated")
	}
}
