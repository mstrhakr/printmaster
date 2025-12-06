package storage

import (
	"context"
	"testing"
	"time"
)

func TestReportLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a report
	report := &ReportDefinition{
		Name:          "Test Report",
		Description:   "A test report for unit testing",
		Type:          ReportTypeDeviceInventory,
		Format:        ReportFormatJSON,
		Scope:         ReportScopeFleet,
		TimeRangeType: "last_30d",
		TimeRangeDays: 30,
		CreatedBy:     "test-user",
	}

	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	if report.ID == 0 {
		t.Fatal("expected non-zero report ID")
	}

	// Get the report
	got, err := s.GetReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}
	if got == nil {
		t.Fatal("expected report, got nil")
	}
	if got.Name != report.Name {
		t.Errorf("name mismatch: got=%q want=%q", got.Name, report.Name)
	}
	if got.Type != ReportTypeDeviceInventory {
		t.Errorf("type mismatch: got=%q want=%q", got.Type, ReportTypeDeviceInventory)
	}

	// Update the report
	report.Description = "Updated description"
	report.Limit = 100
	err = s.UpdateReport(ctx, report)
	if err != nil {
		t.Fatalf("UpdateReport: %v", err)
	}

	got, err = s.GetReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("GetReport after update: %v", err)
	}
	if got.Description != "Updated description" {
		t.Errorf("description not updated: got=%q", got.Description)
	}
	if got.Limit != 100 {
		t.Errorf("limit not updated: got=%d want=100", got.Limit)
	}

	// List reports
	reports, err := s.ListReports(ctx, ReportFilter{})
	if err != nil {
		t.Fatalf("ListReports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got %d", len(reports))
	}

	// List with type filter
	reports, err = s.ListReports(ctx, ReportFilter{Type: ReportTypeDeviceInventory})
	if err != nil {
		t.Fatalf("ListReports with type filter: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("expected 1 report with type filter, got %d", len(reports))
	}

	// List with non-matching filter
	reports, err = s.ListReports(ctx, ReportFilter{Type: ReportTypeUsageSummary})
	if err != nil {
		t.Fatalf("ListReports with non-matching filter: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("expected 0 reports with non-matching filter, got %d", len(reports))
	}

	// Delete the report
	err = s.DeleteReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("DeleteReport: %v", err)
	}

	got, err = s.GetReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("GetReport after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestReportWithArrayFields(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	report := &ReportDefinition{
		Name:            "Array Fields Report",
		Type:            ReportTypeUsageSummary,
		Format:          ReportFormatCSV,
		Scope:           ReportScopeTenant,
		TenantIDs:       []string{"tenant-1", "tenant-2"},
		SiteIDs:         []string{"site-a", "site-b"},
		AgentIDs:        []string{"agent-x", "agent-y"},
		Columns:         []string{"device_name", "page_count", "last_seen"},
		GroupBy:         []string{"agent_id", "site_id"},
		EmailRecipients: []string{"user@example.com", "admin@example.com"},
	}

	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	got, err := s.GetReport(ctx, report.ID)
	if err != nil {
		t.Fatalf("GetReport: %v", err)
	}

	// Verify array fields are preserved
	if len(got.TenantIDs) != 2 || got.TenantIDs[0] != "tenant-1" {
		t.Errorf("TenantIDs not preserved: got %v", got.TenantIDs)
	}
	if len(got.SiteIDs) != 2 || got.SiteIDs[0] != "site-a" {
		t.Errorf("SiteIDs not preserved: got %v", got.SiteIDs)
	}
	if len(got.AgentIDs) != 2 {
		t.Errorf("AgentIDs not preserved: got %v", got.AgentIDs)
	}
	if len(got.Columns) != 3 {
		t.Errorf("Columns not preserved: got %v", got.Columns)
	}
	if len(got.GroupBy) != 2 {
		t.Errorf("GroupBy not preserved: got %v", got.GroupBy)
	}
	if len(got.EmailRecipients) != 2 {
		t.Errorf("EmailRecipients not preserved: got %v", got.EmailRecipients)
	}
}

func TestReportScheduleLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a report first
	report := &ReportDefinition{
		Name:   "Scheduled Report",
		Type:   ReportTypeFleetHealth,
		Format: ReportFormatPDF,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create a schedule
	nextRun := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	schedule := &ReportSchedule{
		ReportID:   report.ID,
		Name:       "Daily Schedule",
		Enabled:    true,
		Frequency:  ReportFrequencyDaily,
		TimeOfDay:  "08:00",
		Timezone:   "America/New_York",
		NextRunAt:  nextRun,
		DayOfWeek:  1, // Monday
		DayOfMonth: 15,
	}

	err = s.CreateReportSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule: %v", err)
	}
	if schedule.ID == 0 {
		t.Fatal("expected non-zero schedule ID")
	}

	// Get the schedule
	got, err := s.GetReportSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("GetReportSchedule: %v", err)
	}
	if got.Name != "Daily Schedule" {
		t.Errorf("name mismatch: got=%q", got.Name)
	}
	if got.Frequency != ReportFrequencyDaily {
		t.Errorf("frequency mismatch: got=%q", got.Frequency)
	}
	if !got.Enabled {
		t.Error("expected enabled=true")
	}

	// Update the schedule
	schedule.Enabled = false
	schedule.Frequency = ReportFrequencyWeekly
	err = s.UpdateReportSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("UpdateReportSchedule: %v", err)
	}

	got, err = s.GetReportSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("GetReportSchedule after update: %v", err)
	}
	if got.Enabled {
		t.Error("expected enabled=false after update")
	}
	if got.Frequency != ReportFrequencyWeekly {
		t.Errorf("frequency not updated: got=%q", got.Frequency)
	}

	// List schedules for report
	schedules, err := s.ListReportSchedules(ctx, report.ID)
	if err != nil {
		t.Fatalf("ListReportSchedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(schedules))
	}

	// List all schedules (reportID=0)
	allSchedules, err := s.ListReportSchedules(ctx, 0)
	if err != nil {
		t.Fatalf("ListReportSchedules(all): %v", err)
	}
	if len(allSchedules) != 1 {
		t.Fatalf("expected 1 schedule in all, got %d", len(allSchedules))
	}

	// Delete the schedule
	err = s.DeleteReportSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("DeleteReportSchedule: %v", err)
	}

	got, err = s.GetReportSchedule(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("GetReportSchedule after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDueSchedules(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a report
	report := &ReportDefinition{
		Name:   "Due Test Report",
		Type:   ReportTypeAgentStatus,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create an overdue schedule (should be returned)
	overdueSchedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Overdue Schedule",
		Enabled:   true,
		Frequency: ReportFrequencyDaily,
		TimeOfDay: "08:00",
		Timezone:  "UTC",
		NextRunAt: time.Now().Add(-1 * time.Hour), // 1 hour ago
	}
	err = s.CreateReportSchedule(ctx, overdueSchedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule (overdue): %v", err)
	}

	// Create a future schedule (should not be returned)
	futureSchedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Future Schedule",
		Enabled:   true,
		Frequency: ReportFrequencyWeekly,
		TimeOfDay: "09:00",
		Timezone:  "UTC",
		NextRunAt: time.Now().Add(24 * time.Hour), // 24 hours from now
	}
	err = s.CreateReportSchedule(ctx, futureSchedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule (future): %v", err)
	}

	// Create a disabled schedule (should not be returned)
	disabledSchedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Disabled Schedule",
		Enabled:   false,
		Frequency: ReportFrequencyMonthly,
		TimeOfDay: "10:00",
		Timezone:  "UTC",
		NextRunAt: time.Now().Add(-2 * time.Hour), // 2 hours ago but disabled
	}
	err = s.CreateReportSchedule(ctx, disabledSchedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule (disabled): %v", err)
	}

	// Get due schedules
	due, err := s.GetDueSchedules(ctx, time.Now())
	if err != nil {
		t.Fatalf("GetDueSchedules: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due schedule, got %d", len(due))
	}
	if due[0].Name != "Overdue Schedule" {
		t.Errorf("wrong schedule returned: got %q", due[0].Name)
	}
}

func TestUpdateScheduleAfterRun(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create report and schedule
	report := &ReportDefinition{
		Name:   "After Run Test Report",
		Type:   ReportTypeSuppliesStatus,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	schedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Update Test Schedule",
		Enabled:   true,
		Frequency: ReportFrequencyDaily,
		TimeOfDay: "08:00",
		Timezone:  "UTC",
		NextRunAt: time.Now(),
	}
	err = s.CreateReportSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule: %v", err)
	}

	// Update after successful run
	nextRun := time.Now().Add(24 * time.Hour)
	err = s.UpdateScheduleAfterRun(ctx, schedule.ID, 123, nextRun, false)
	if err != nil {
		t.Fatalf("UpdateScheduleAfterRun (success): %v", err)
	}

	got, _ := s.GetReportSchedule(ctx, schedule.ID)
	if got.FailureCount != 0 {
		t.Errorf("failure_count should be 0 after success, got %d", got.FailureCount)
	}

	// Update after failed run
	err = s.UpdateScheduleAfterRun(ctx, schedule.ID, 124, nextRun, true)
	if err != nil {
		t.Fatalf("UpdateScheduleAfterRun (failure): %v", err)
	}

	got, _ = s.GetReportSchedule(ctx, schedule.ID)
	if got.FailureCount != 1 {
		t.Errorf("failure_count should be 1 after failure, got %d", got.FailureCount)
	}
}

func TestReportRunLifecycle(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create a report
	report := &ReportDefinition{
		Name:   "Run Test Report",
		Type:   ReportTypeTopPrinters,
		Format: ReportFormatCSV,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create a run
	run := &ReportRun{
		ReportID:       report.ID,
		Status:         ReportRunStatusPending,
		Format:         ReportFormatCSV,
		StartedAt:      time.Now(),
		ParametersJSON: `{"limit": 10}`,
		RunBy:          "test-user",
	}

	err = s.CreateReportRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateReportRun: %v", err)
	}
	if run.ID == 0 {
		t.Fatal("expected non-zero run ID")
	}

	// Get the run
	got, err := s.GetReportRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetReportRun: %v", err)
	}
	if got.Status != ReportRunStatusPending {
		t.Errorf("status mismatch: got=%q", got.Status)
	}
	if got.RunBy != "test-user" {
		t.Errorf("run_by mismatch: got=%q", got.RunBy)
	}

	// Update the run (complete it)
	completedAt := time.Now()
	run.Status = ReportRunStatusCompleted
	run.CompletedAt = &completedAt
	run.DurationMS = 1500
	run.RowCount = 50
	run.ResultSize = 1024
	run.ResultPath = "/reports/output/test.csv"
	run.ResultData = "col1,col2\nval1,val2"

	err = s.UpdateReportRun(ctx, run)
	if err != nil {
		t.Fatalf("UpdateReportRun: %v", err)
	}

	got, err = s.GetReportRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetReportRun after update: %v", err)
	}
	if got.Status != ReportRunStatusCompleted {
		t.Errorf("status not updated: got=%q", got.Status)
	}
	if got.RowCount != 50 {
		t.Errorf("row_count not updated: got=%d", got.RowCount)
	}
	if got.ResultPath != "/reports/output/test.csv" {
		t.Errorf("result_path not updated: got=%q", got.ResultPath)
	}

	// List runs
	runs, err := s.ListReportRuns(ctx, ReportRunFilter{ReportID: report.ID})
	if err != nil {
		t.Fatalf("ListReportRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	// List with status filter
	runs, err = s.ListReportRuns(ctx, ReportRunFilter{Status: ReportRunStatusCompleted})
	if err != nil {
		t.Fatalf("ListReportRuns with status filter: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 completed run, got %d", len(runs))
	}

	// Delete the run
	err = s.DeleteReportRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("DeleteReportRun: %v", err)
	}

	got, err = s.GetReportRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetReportRun after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestReportRunWithSchedule(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create report and schedule
	report := &ReportDefinition{
		Name:   "Scheduled Run Report",
		Type:   ReportTypeAlertHistory,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	schedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Test Schedule",
		Enabled:   true,
		Frequency: ReportFrequencyDaily,
		TimeOfDay: "08:00",
		Timezone:  "UTC",
		NextRunAt: time.Now(),
	}
	err = s.CreateReportSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule: %v", err)
	}

	// Create a run linked to the schedule
	scheduleID := schedule.ID
	run := &ReportRun{
		ReportID:   report.ID,
		ScheduleID: &scheduleID,
		Status:     ReportRunStatusRunning,
		Format:     ReportFormatJSON,
		StartedAt:  time.Now(),
	}

	err = s.CreateReportRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateReportRun: %v", err)
	}

	// Get and verify schedule link
	got, err := s.GetReportRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetReportRun: %v", err)
	}
	if got.ScheduleID == nil || *got.ScheduleID != schedule.ID {
		t.Error("schedule_id not preserved")
	}

	// Filter by schedule ID
	runs, err := s.ListReportRuns(ctx, ReportRunFilter{ScheduleID: &scheduleID})
	if err != nil {
		t.Fatalf("ListReportRuns by schedule: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run for schedule, got %d", len(runs))
	}
}

func TestReportRunFailure(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	report := &ReportDefinition{
		Name:   "Failed Run Report",
		Type:   ReportTypeUsageByDevice,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create a failed run
	run := &ReportRun{
		ReportID:  report.ID,
		Status:    ReportRunStatusFailed,
		Format:    ReportFormatJSON,
		StartedAt: time.Now(),
	}
	err = s.CreateReportRun(ctx, run)
	if err != nil {
		t.Fatalf("CreateReportRun: %v", err)
	}

	// Update with error
	completedAt := time.Now()
	run.CompletedAt = &completedAt
	run.ErrorMessage = "database connection timeout"
	err = s.UpdateReportRun(ctx, run)
	if err != nil {
		t.Fatalf("UpdateReportRun: %v", err)
	}

	got, _ := s.GetReportRun(ctx, run.ID)
	if got.ErrorMessage != "database connection timeout" {
		t.Errorf("error_message not preserved: got=%q", got.ErrorMessage)
	}
}

func TestCleanupOldReportRuns(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	report := &ReportDefinition{
		Name:   "Cleanup Test Report",
		Type:   ReportTypeUsageTrends,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create runs - we can't easily backdate created_at, but we can test the function works
	for i := 0; i < 3; i++ {
		run := &ReportRun{
			ReportID:  report.ID,
			Status:    ReportRunStatusCompleted,
			Format:    ReportFormatJSON,
			StartedAt: time.Now(),
		}
		err = s.CreateReportRun(ctx, run)
		if err != nil {
			t.Fatalf("CreateReportRun %d: %v", i, err)
		}
	}

	// Cleanup runs older than now (should delete none since they were just created)
	deleted, err := s.CleanupOldReportRuns(ctx, time.Now().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("CleanupOldReportRuns: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted (all recent), got %d", deleted)
	}

	// Cleanup runs older than future (should delete all)
	deleted, err = s.CleanupOldReportRuns(ctx, time.Now().Add(1*time.Hour))
	if err != nil {
		t.Fatalf("CleanupOldReportRuns (future): %v", err)
	}
	if deleted != 3 {
		t.Errorf("expected 3 deleted, got %d", deleted)
	}
}

func TestGetReportSummary(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Get summary with empty database
	summary, err := s.GetReportSummary(ctx)
	if err != nil {
		t.Fatalf("GetReportSummary (empty): %v", err)
	}
	if summary.TotalReports != 0 {
		t.Errorf("expected 0 reports, got %d", summary.TotalReports)
	}

	// Create a report
	report := &ReportDefinition{
		Name:   "Summary Test Report",
		Type:   ReportTypeFleetHealth,
		Format: ReportFormatJSON,
		Scope:  ReportScopeFleet,
	}
	err = s.CreateReport(ctx, report)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}

	// Create an enabled schedule
	schedule := &ReportSchedule{
		ReportID:  report.ID,
		Name:      "Enabled Schedule",
		Enabled:   true,
		Frequency: ReportFrequencyDaily,
		TimeOfDay: "08:00",
		Timezone:  "UTC",
		NextRunAt: time.Now(),
	}
	err = s.CreateReportSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("CreateReportSchedule: %v", err)
	}

	// Create some runs
	for i := 0; i < 2; i++ {
		run := &ReportRun{
			ReportID:  report.ID,
			Status:    ReportRunStatusCompleted,
			Format:    ReportFormatJSON,
			StartedAt: time.Now(),
		}
		completedAt := time.Now()
		run.CompletedAt = &completedAt
		run.DurationMS = 100
		run.ResultSize = 512
		err = s.CreateReportRun(ctx, run)
		if err != nil {
			t.Fatalf("CreateReportRun %d: %v", i, err)
		}
	}

	// Get updated summary
	summary, err = s.GetReportSummary(ctx)
	if err != nil {
		t.Fatalf("GetReportSummary: %v", err)
	}
	if summary.TotalReports != 1 {
		t.Errorf("expected 1 report, got %d", summary.TotalReports)
	}
	if summary.TotalSchedules != 1 {
		t.Errorf("expected 1 schedule, got %d", summary.TotalSchedules)
	}
	if summary.ActiveSchedules != 1 {
		t.Errorf("expected 1 active schedule, got %d", summary.ActiveSchedules)
	}
	if summary.TotalRuns != 2 {
		t.Errorf("expected 2 runs, got %d", summary.TotalRuns)
	}
	if summary.SuccessfulRuns != 2 {
		t.Errorf("expected 2 successful runs, got %d", summary.SuccessfulRuns)
	}
}

func TestReportFilters(t *testing.T) {
	t.Parallel()
	s, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create reports with different attributes
	reports := []*ReportDefinition{
		{Name: "Report 1", Type: ReportTypeDeviceInventory, Scope: ReportScopeFleet, CreatedBy: "user1"},
		{Name: "Report 2", Type: ReportTypeUsageSummary, Scope: ReportScopeTenant, CreatedBy: "user1"},
		{Name: "Report 3", Type: ReportTypeDeviceInventory, Scope: ReportScopeSite, CreatedBy: "user2"},
	}

	for _, r := range reports {
		r.Format = ReportFormatJSON
		err := s.CreateReport(ctx, r)
		if err != nil {
			t.Fatalf("CreateReport %s: %v", r.Name, err)
		}
	}

	// Filter by type
	got, err := s.ListReports(ctx, ReportFilter{Type: ReportTypeDeviceInventory})
	if err != nil {
		t.Fatalf("ListReports by type: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 device_inventory reports, got %d", len(got))
	}

	// Filter by scope
	got, err = s.ListReports(ctx, ReportFilter{Scope: ReportScopeFleet})
	if err != nil {
		t.Fatalf("ListReports by scope: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 fleet-scope report, got %d", len(got))
	}

	// Filter by created_by
	got, err = s.ListReports(ctx, ReportFilter{CreatedBy: "user1"})
	if err != nil {
		t.Fatalf("ListReports by created_by: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 reports by user1, got %d", len(got))
	}

	// Filter with limit
	got, err = s.ListReports(ctx, ReportFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListReports with limit: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 reports with limit, got %d", len(got))
	}
}

func TestBuiltInReportTypes(t *testing.T) {
	t.Parallel()

	types := GetBuiltInReportTypes()
	if len(types) == 0 {
		t.Error("expected non-empty list of built-in report types")
	}

	// Verify expected types are present
	expectedTypes := []ReportType{
		ReportTypeDeviceInventory,
		ReportTypeUsageSummary,
		ReportTypeSuppliesStatus,
		ReportTypeAlertHistory,
	}

	for _, expected := range expectedTypes {
		found := false
		for _, got := range types {
			if got == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected type %q not found in built-in types", expected)
		}
	}
}
