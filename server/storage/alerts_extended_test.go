package storage

import (
	"context"
	"testing"
	"time"
)

func TestIsInQuietHours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		qh     QuietHours
		adjust func() time.Time // Function to create a test time
		want   bool
	}{
		{
			name: "disabled returns false",
			qh:   QuietHours{Enabled: false, StartTime: "22:00", EndTime: "06:00"},
			want: false,
		},
		{
			name: "invalid start time format returns false",
			qh:   QuietHours{Enabled: true, StartTime: "invalid", EndTime: "06:00"},
			want: false,
		},
		{
			name: "invalid end time format returns false",
			qh:   QuietHours{Enabled: true, StartTime: "22:00", EndTime: "invalid"},
			want: false,
		},
		{
			name: "single colon start returns false",
			qh:   QuietHours{Enabled: true, StartTime: "22", EndTime: "06:00"},
			want: false,
		},
		{
			name: "single colon end returns false",
			qh:   QuietHours{Enabled: true, StartTime: "22:00", EndTime: "06"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInQuietHours(tt.qh)
			if got != tt.want {
				t.Errorf("isInQuietHours(%+v) = %v, want %v", tt.qh, got, tt.want)
			}
		})
	}
}

func TestIsInQuietHours_TimeZone(t *testing.T) {
	t.Parallel()

	// Test with a valid timezone
	qh := QuietHours{
		Enabled:   true,
		StartTime: "00:00",
		EndTime:   "23:59",
		Timezone:  "America/New_York",
	}

	// This should always be in quiet hours (covers entire day)
	got := isInQuietHours(qh)
	if !got {
		t.Error("isInQuietHours should return true for 00:00-23:59")
	}

	// Test with "local" timezone
	qhLocal := QuietHours{
		Enabled:   true,
		StartTime: "00:00",
		EndTime:   "23:59",
		Timezone:  "local",
	}
	got = isInQuietHours(qhLocal)
	if !got {
		t.Error("isInQuietHours with local timezone should return true for 00:00-23:59")
	}

	// Test with invalid timezone (should fall back to local)
	qhInvalid := QuietHours{
		Enabled:   true,
		StartTime: "00:00",
		EndTime:   "23:59",
		Timezone:  "Invalid/Timezone",
	}
	got = isInQuietHours(qhInvalid)
	if !got {
		t.Error("isInQuietHours with invalid timezone should still work")
	}
}

func TestListAlerts(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create test alerts with different severities, types, and statuses
	alerts := []*Alert{
		{
			Type:         AlertTypeDeviceOffline,
			Severity:     AlertSeverityWarning,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive,
			DeviceSerial: "DEV001",
			Title:        "Device Offline 1",
			TriggeredAt:  time.Now().UTC().Add(-time.Hour),
		},
		{
			Type:         AlertTypeSupplyCritical,
			Severity:     AlertSeverityCritical,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusAcknowledged,
			DeviceSerial: "DEV002",
			Title:        "Critical Toner",
			TenantID:     "tenant-1",
			TriggeredAt:  time.Now().UTC(),
		},
		{
			Type:        AlertTypeAgentOffline,
			Severity:    AlertSeverityCritical,
			Scope:       AlertScopeAgent,
			Status:      AlertStatusResolved,
			AgentID:     "agent-1",
			Title:       "Agent Offline",
			TriggeredAt: time.Now().UTC().Add(-2 * time.Hour),
		},
	}

	for _, a := range alerts {
		if _, err := s.CreateAlert(ctx, a); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
	}

	// Test listing all alerts
	all, err := s.ListAlerts(ctx, AlertFilters{})
	if err != nil {
		t.Fatalf("ListAlerts: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 alerts, got %d", len(all))
	}

	// Test filter by severity
	critical, err := s.ListAlerts(ctx, AlertFilters{Severity: AlertSeverityCritical})
	if err != nil {
		t.Fatalf("ListAlerts by severity: %v", err)
	}
	if len(critical) != 2 {
		t.Errorf("expected 2 critical alerts, got %d", len(critical))
	}

	// Test filter by scope
	deviceAlerts, err := s.ListAlerts(ctx, AlertFilters{Scope: AlertScopeDevice})
	if err != nil {
		t.Fatalf("ListAlerts by scope: %v", err)
	}
	if len(deviceAlerts) != 2 {
		t.Errorf("expected 2 device alerts, got %d", len(deviceAlerts))
	}

	// Test filter by type
	offlineAlerts, err := s.ListAlerts(ctx, AlertFilters{Type: AlertTypeDeviceOffline})
	if err != nil {
		t.Fatalf("ListAlerts by type: %v", err)
	}
	if len(offlineAlerts) != 1 {
		t.Errorf("expected 1 offline alert, got %d", len(offlineAlerts))
	}

	// Test filter by status
	activeAlerts, err := s.ListAlerts(ctx, AlertFilters{Status: AlertStatusActive})
	if err != nil {
		t.Fatalf("ListAlerts by status: %v", err)
	}
	if len(activeAlerts) != 1 {
		t.Errorf("expected 1 active alert, got %d", len(activeAlerts))
	}

	// Test filter by tenant
	tenantAlerts, err := s.ListAlerts(ctx, AlertFilters{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("ListAlerts by tenant: %v", err)
	}
	if len(tenantAlerts) != 1 {
		t.Errorf("expected 1 tenant-1 alert, got %d", len(tenantAlerts))
	}

	// Test filter by time range
	startTime := time.Now().UTC().Add(-90 * time.Minute)
	endTime := time.Now().UTC().Add(-30 * time.Minute)
	timeRangeAlerts, err := s.ListAlerts(ctx, AlertFilters{StartTime: &startTime, EndTime: &endTime})
	if err != nil {
		t.Fatalf("ListAlerts by time range: %v", err)
	}
	if len(timeRangeAlerts) != 1 {
		t.Errorf("expected 1 alert in time range, got %d", len(timeRangeAlerts))
	}

	// Test with limit
	limitedAlerts, err := s.ListAlerts(ctx, AlertFilters{Limit: 2})
	if err != nil {
		t.Fatalf("ListAlerts with limit: %v", err)
	}
	if len(limitedAlerts) != 2 {
		t.Errorf("expected 2 alerts with limit, got %d", len(limitedAlerts))
	}
}

func TestUpdateAlertStatus(t *testing.T) {
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
		DeviceSerial: "DEV001",
		Title:        "Test Alert",
		TriggeredAt:  time.Now().UTC(),
	}

	id, err := s.CreateAlert(ctx, alert)
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	// Update to acknowledged
	err = s.UpdateAlertStatus(ctx, id, AlertStatusAcknowledged)
	if err != nil {
		t.Fatalf("UpdateAlertStatus: %v", err)
	}

	got, err := s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if got.Status != AlertStatusAcknowledged {
		t.Errorf("expected status %q, got %q", AlertStatusAcknowledged, got.Status)
	}

	// Update to resolved
	err = s.UpdateAlertStatus(ctx, id, AlertStatusResolved)
	if err != nil {
		t.Fatalf("UpdateAlertStatus to resolved: %v", err)
	}

	got, err = s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert after resolve: %v", err)
	}
	if got.Status != AlertStatusResolved {
		t.Errorf("expected status %q, got %q", AlertStatusResolved, got.Status)
	}
}

func TestSuppressAlert(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create an alert
	alert := &Alert{
		Type:         AlertTypeSupplyLow,
		Severity:     AlertSeverityWarning,
		Scope:        AlertScopeDevice,
		Status:       AlertStatusActive,
		DeviceSerial: "DEV001",
		Title:        "Low Toner",
		TriggeredAt:  time.Now().UTC(),
	}

	id, err := s.CreateAlert(ctx, alert)
	if err != nil {
		t.Fatalf("CreateAlert: %v", err)
	}

	// Suppress until tomorrow
	until := time.Now().UTC().Add(24 * time.Hour)
	err = s.SuppressAlert(ctx, id, until)
	if err != nil {
		t.Fatalf("SuppressAlert: %v", err)
	}

	got, err := s.GetAlert(ctx, id)
	if err != nil {
		t.Fatalf("GetAlert: %v", err)
	}
	if got.Status != AlertStatusSuppressed {
		t.Errorf("expected status %q, got %q", AlertStatusSuppressed, got.Status)
	}
	if got.SuppressedUntil == nil {
		t.Error("expected suppressed_until to be set")
	}
	if got.SuppressedUntil.Before(time.Now()) {
		t.Error("suppressed_until should be in the future")
	}
}

func TestListAlertHistory(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create alerts with different statuses
	alerts := []*Alert{
		{
			Type:         AlertTypeDeviceOffline,
			Severity:     AlertSeverityWarning,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusActive, // Should NOT appear in history
			DeviceSerial: "DEV001",
			Title:        "Active Alert",
			TriggeredAt:  time.Now().UTC(),
		},
		{
			Type:         AlertTypeSupplyLow,
			Severity:     AlertSeverityWarning,
			Scope:        AlertScopeDevice,
			Status:       AlertStatusResolved,
			DeviceSerial: "DEV002",
			Title:        "Resolved Alert",
			TriggeredAt:  time.Now().UTC().Add(-1 * time.Hour),
		},
		{
			Type:        AlertTypeAgentOffline,
			Severity:    AlertSeverityCritical,
			Scope:       AlertScopeAgent,
			Status:      AlertStatusSuppressed, // Should appear in history (not active)
			AgentID:     "agent-1",
			Title:       "Suppressed Alert",
			TriggeredAt: time.Now().UTC().Add(-30 * time.Minute),
		},
	}

	for _, a := range alerts {
		if _, err := s.CreateAlert(ctx, a); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
	}

	// List history (non-active alerts)
	history, err := s.ListAlertHistory(ctx, AlertFilters{})
	if err != nil {
		t.Fatalf("ListAlertHistory: %v", err)
	}
	// Should have resolved and suppressed alerts, not the active one
	if len(history) != 2 {
		t.Errorf("expected 2 history alerts, got %d", len(history))
	}

	// Test with time range filter
	startTime := time.Now().UTC().Add(-2 * time.Hour)
	endTime := time.Now().UTC().Add(-20 * time.Minute)
	historyWithRange, err := s.ListAlertHistory(ctx, AlertFilters{StartTime: &startTime, EndTime: &endTime})
	if err != nil {
		t.Fatalf("ListAlertHistory with range: %v", err)
	}
	if len(historyWithRange) != 2 {
		t.Errorf("expected 2 history alerts in range, got %d", len(historyWithRange))
	}

	// Test with tenant filter
	historyWithTenant, err := s.ListAlertHistory(ctx, AlertFilters{TenantID: "nonexistent"})
	if err != nil {
		t.Fatalf("ListAlertHistory with tenant: %v", err)
	}
	if len(historyWithTenant) != 0 {
		t.Errorf("expected 0 history alerts for nonexistent tenant, got %d", len(historyWithTenant))
	}

	// Test with limit
	historyWithLimit, err := s.ListAlertHistory(ctx, AlertFilters{Limit: 1})
	if err != nil {
		t.Fatalf("ListAlertHistory with limit: %v", err)
	}
	if len(historyWithLimit) != 1 {
		t.Errorf("expected 1 history alert with limit, got %d", len(historyWithLimit))
	}
}
