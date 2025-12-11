package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"printmaster/server/reports"
	"printmaster/server/storage"
	"strconv"
	"strings"
	"time"
)

// ReportStore wraps the storage.Store to implement report interfaces.
type ReportStore struct {
	store storage.Store
}

func (rs *ReportStore) ListAllDevices(ctx context.Context) ([]*storage.Device, error) {
	return rs.store.ListAllDevices(ctx)
}

func (rs *ReportStore) GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error) {
	return rs.store.GetLatestMetrics(ctx, serial)
}

func (rs *ReportStore) GetMetricsHistory(ctx context.Context, serial string, since time.Time) ([]*storage.MetricsSnapshot, error) {
	return rs.store.GetMetricsHistory(ctx, serial, since)
}

func (rs *ReportStore) ListAgents(ctx context.Context) ([]*storage.Agent, error) {
	return rs.store.ListAgents(ctx)
}

func (rs *ReportStore) GetAgent(ctx context.Context, agentID string) (*storage.Agent, error) {
	return rs.store.GetAgent(ctx, agentID)
}

func (rs *ReportStore) ListTenants(ctx context.Context) ([]*storage.Tenant, error) {
	return rs.store.ListTenants(ctx)
}

func (rs *ReportStore) GetTenant(ctx context.Context, id string) (*storage.Tenant, error) {
	return rs.store.GetTenant(ctx, id)
}

func (rs *ReportStore) ListSitesByTenant(ctx context.Context, tenantID string) ([]*storage.Site, error) {
	return rs.store.ListSitesByTenant(ctx, tenantID)
}

func (rs *ReportStore) ListAlerts(ctx context.Context, filter storage.AlertFilter) ([]*storage.Alert, error) {
	return rs.store.ListAlerts(ctx, filter)
}

func (rs *ReportStore) GetAlertSummary(ctx context.Context) (*storage.AlertSummary, error) {
	return rs.store.GetAlertSummary(ctx)
}

func (rs *ReportStore) GetReport(ctx context.Context, id int64) (*storage.ReportDefinition, error) {
	return rs.store.GetReport(ctx, id)
}

func (rs *ReportStore) GetDueSchedules(ctx context.Context, before time.Time) ([]*storage.ReportSchedule, error) {
	return rs.store.GetDueSchedules(ctx, before)
}

func (rs *ReportStore) UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, runID int64, nextRun time.Time, failed bool) error {
	return rs.store.UpdateScheduleAfterRun(ctx, scheduleID, runID, nextRun, failed)
}

func (rs *ReportStore) CreateReportRun(ctx context.Context, run *storage.ReportRun) error {
	return rs.store.CreateReportRun(ctx, run)
}

func (rs *ReportStore) UpdateReportRun(ctx context.Context, run *storage.ReportRun) error {
	return rs.store.UpdateReportRun(ctx, run)
}

// handleReports handles GET /api/v1/reports and POST /api/v1/reports
func handleReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		// List reports
		filter := storage.ReportFilter{}

		if t := r.URL.Query().Get("type"); t != "" {
			filter.Type = storage.ReportType(t)
		}
		if s := r.URL.Query().Get("scope"); s != "" {
			filter.Scope = storage.ReportScope(s)
		}
		if cb := r.URL.Query().Get("created_by"); cb != "" {
			filter.CreatedBy = cb
		}
		if b := r.URL.Query().Get("built_in"); b != "" {
			val := b == "true"
			filter.IsBuiltIn = &val
		}
		if l := r.URL.Query().Get("limit"); l != "" {
			if lv, err := strconv.Atoi(l); err == nil {
				filter.Limit = lv
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if ov, err := strconv.Atoi(o); err == nil {
				filter.Offset = ov
			}
		}

		reports, err := serverStore.ListReports(ctx, filter)
		if err != nil {
			serverLogger.Error("Failed to list reports", "error", err)
			http.Error(w, fmt.Sprintf("list reports: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"reports": reports,
			"count":   len(reports),
		})

	case http.MethodPost:
		// Create report
		var report storage.ReportDefinition
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}

		// Get current user for created_by
		if principal := getPrincipal(r); principal != nil {
			report.CreatedBy = principal.User.Username
		}

		if err := serverStore.CreateReport(ctx, &report); err != nil {
			serverLogger.Error("Failed to create report", "name", report.Name, "type", report.Type, "error", err)
			http.Error(w, fmt.Sprintf("create report: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(report)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleReport handles GET/PUT/DELETE /api/v1/reports/{id}
func handleReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract ID from path
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/reports/")
	// Handle sub-paths
	if idx := strings.Index(idStr, "/"); idx >= 0 {
		subPath := idStr[idx:]
		idStr = idStr[:idx]

		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.Error(w, "Invalid report ID", http.StatusBadRequest)
			return
		}

		switch {
		case strings.HasPrefix(subPath, "/run"):
			handleReportRun(w, r, id)
		case strings.HasPrefix(subPath, "/schedules"):
			handleReportSchedules(w, r, id)
		case strings.HasPrefix(subPath, "/runs"):
			handleReportRuns(w, r, id)
		default:
			http.Error(w, "Not found", http.StatusNotFound)
		}
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid report ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		report, err := serverStore.GetReport(ctx, id)
		if err != nil {
			serverLogger.Error("Failed to get report", "report_id", id, "error", err)
			http.Error(w, fmt.Sprintf("get report: %v", err), http.StatusInternalServerError)
			return
		}
		if report == nil {
			http.Error(w, "Report not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)

	case http.MethodPut:
		var report storage.ReportDefinition
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
		report.ID = id

		if err := serverStore.UpdateReport(ctx, &report); err != nil {
			serverLogger.Error("Failed to update report", "report_id", report.ID, "error", err)
			http.Error(w, fmt.Sprintf("update report: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)

	case http.MethodDelete:
		if err := serverStore.DeleteReport(ctx, id); err != nil {
			serverLogger.Error("Failed to delete report", "report_id", id, "error", err)
			http.Error(w, fmt.Sprintf("delete report: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleReportRun handles POST /api/v1/reports/{id}/run
func handleReportRun(w http.ResponseWriter, r *http.Request, reportID int64) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get username from session
	username := "api"
	if principal := getPrincipal(r); principal != nil {
		username = principal.User.Username
	}

	// Create report store wrapper
	reportStore := &ReportStore{store: serverStore}
	scheduler := reports.NewScheduler(reportStore, serverLogger)

	run, err := scheduler.RunNow(ctx, reportID, username)
	if err != nil {
		serverLogger.Error("Report run failed", "report_id", reportID, "username", username, "error", err)
		http.Error(w, fmt.Sprintf("run report: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// handleReportRuns handles GET /api/v1/reports/{id}/runs
func handleReportRuns(w http.ResponseWriter, r *http.Request, reportID int64) {
	ctx := r.Context()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filter := storage.ReportRunFilter{
		ReportID: reportID,
		Limit:    50,
	}

	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = storage.ReportStatus(s)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if lv, err := strconv.Atoi(l); err == nil {
			filter.Limit = lv
		}
	}

	runs, err := serverStore.ListReportRuns(ctx, filter)
	if err != nil {
		serverLogger.Error("Failed to list report runs", "report_id", reportID, "error", err)
		http.Error(w, fmt.Sprintf("list runs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
	})
}

// handleReportRunsCollection handles GET /api/v1/report-runs (list all runs)
func handleReportRunsCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filter := storage.ReportRunFilter{
		Limit: 50,
	}

	if s := r.URL.Query().Get("status"); s != "" {
		filter.Status = storage.ReportStatus(s)
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if lv, err := strconv.Atoi(l); err == nil {
			filter.Limit = lv
		}
	}
	if rid := r.URL.Query().Get("report_id"); rid != "" {
		if rv, err := strconv.ParseInt(rid, 10, 64); err == nil {
			filter.ReportID = rv
		}
	}

	runs, err := serverStore.ListReportRuns(ctx, filter)
	if err != nil {
		serverLogger.Error("Failed to list all report runs", "error", err)
		http.Error(w, fmt.Sprintf("list runs: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
	})
}

// handleReportSchedules handles GET/POST /api/v1/reports/{id}/schedules
func handleReportSchedules(w http.ResponseWriter, r *http.Request, reportID int64) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		schedules, err := serverStore.ListReportSchedules(ctx, reportID)
		if err != nil {
			serverLogger.Error("Failed to list report schedules", "report_id", reportID, "error", err)
			http.Error(w, fmt.Sprintf("list schedules: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"schedules": schedules,
			"count":     len(schedules),
		})

	case http.MethodPost:
		var schedule storage.ReportSchedule
		if err := json.NewDecoder(r.Body).Decode(&schedule); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
		schedule.ReportID = reportID

		// Calculate initial next_run_at if not set
		if schedule.NextRunAt.IsZero() {
			schedule.NextRunAt = calculateInitialNextRun(&schedule)
		}

		if err := serverStore.CreateReportSchedule(ctx, &schedule); err != nil {
			serverLogger.Error("Failed to create report schedule", "report_id", reportID, "error", err)
			http.Error(w, fmt.Sprintf("create schedule: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(schedule)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleReportSchedulesCollection handles GET /api/v1/report-schedules (list all schedules)
func handleReportSchedulesCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// List all schedules (no report filter)
	schedules, err := serverStore.ListReportSchedules(ctx, 0)
	if err != nil {
		serverLogger.Error("Failed to list all report schedules", "error", err)
		http.Error(w, fmt.Sprintf("list schedules: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"schedules": schedules,
		"count":     len(schedules),
	})
}

// handleSchedule handles GET/PUT/DELETE /api/v1/report-schedules/{id}
func handleSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract ID
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/report-schedules/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid schedule ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		schedule, err := serverStore.GetReportSchedule(ctx, id)
		if err != nil {
			serverLogger.Error("Failed to get report schedule", "schedule_id", id, "error", err)
			http.Error(w, fmt.Sprintf("get schedule: %v", err), http.StatusInternalServerError)
			return
		}
		if schedule == nil {
			http.Error(w, "Schedule not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schedule)

	case http.MethodPut:
		var schedule storage.ReportSchedule
		if err := json.NewDecoder(r.Body).Decode(&schedule); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}
		schedule.ID = id

		// Recalculate next run if schedule changed
		if schedule.NextRunAt.IsZero() {
			schedule.NextRunAt = calculateInitialNextRun(&schedule)
		}

		if err := serverStore.UpdateReportSchedule(ctx, &schedule); err != nil {
			serverLogger.Error("Failed to update report schedule", "schedule_id", id, "error", err)
			http.Error(w, fmt.Sprintf("update schedule: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(schedule)

	case http.MethodDelete:
		if err := serverStore.DeleteReportSchedule(ctx, id); err != nil {
			serverLogger.Error("Failed to delete report schedule", "schedule_id", id, "error", err)
			http.Error(w, fmt.Sprintf("delete schedule: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleReportRunResult handles GET /api/v1/report-runs/{id}
func handleReportRunResult(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/report-runs/")
	// Handle /download suffix
	download := false
	if strings.HasSuffix(idStr, "/download") {
		download = true
		idStr = strings.TrimSuffix(idStr, "/download")
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid run ID", http.StatusBadRequest)
		return
	}

	run, err := serverStore.GetReportRun(ctx, id)
	if err != nil {
		serverLogger.Error("Failed to get report run", "run_id", id, "error", err)
		http.Error(w, fmt.Sprintf("get run: %v", err), http.StatusInternalServerError)
		return
	}
	if run == nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	if download && run.ResultData != "" {
		// Serve the result as a file download
		var contentType, ext string
		switch run.Format {
		case storage.ReportFormatJSON:
			contentType = "application/json"
			ext = "json"
		case storage.ReportFormatCSV:
			contentType = "text/csv"
			ext = "csv"
		case storage.ReportFormatHTML:
			contentType = "text/html"
			ext = "html"
		default:
			contentType = "application/octet-stream"
			ext = "txt"
		}

		filename := fmt.Sprintf("report_%d_%s.%s", run.ReportID, run.StartedAt.Format("20060102_150405"), ext)

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Write([]byte(run.ResultData))
		return
	}

	// Return run metadata
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(run)
}

// handleReportTypes handles GET /api/v1/reports/types
func handleReportTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	types := storage.GetBuiltInReportTypes()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"types": types,
		"count": len(types),
	})
}

// handleReportSummary handles GET /api/v1/reports/summary
func handleReportSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	summary, err := serverStore.GetReportSummary(ctx)
	if err != nil {
		serverLogger.Error("Failed to get report summary", "error", err)
		http.Error(w, fmt.Sprintf("get summary: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// calculateInitialNextRun calculates the first run time for a new schedule.
func calculateInitialNextRun(schedule *storage.ReportSchedule) time.Time {
	now := time.Now()

	var hour, min int
	fmt.Sscanf(schedule.TimeOfDay, "%d:%d", &hour, &min)

	loc := time.UTC
	if schedule.Timezone != "" {
		if l, err := time.LoadLocation(schedule.Timezone); err == nil {
			loc = l
		}
	}

	nowLocal := now.In(loc)

	switch schedule.Frequency {
	case storage.ScheduleFrequencyDaily:
		next := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, min, 0, 0, loc)
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		return next.UTC()

	case storage.ScheduleFrequencyWeekly:
		next := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, min, 0, 0, loc)
		daysUntil := (schedule.DayOfWeek - int(next.Weekday()) + 7) % 7
		if daysUntil == 0 && next.Before(now) {
			daysUntil = 7
		}
		next = next.Add(time.Duration(daysUntil) * 24 * time.Hour)
		return next.UTC()

	case storage.ScheduleFrequencyMonthly:
		next := time.Date(nowLocal.Year(), nowLocal.Month(), schedule.DayOfMonth, hour, min, 0, 0, loc)
		if next.Before(now) {
			next = next.AddDate(0, 1, 0)
		}
		return next.UTC()
	}

	return now.Add(24 * time.Hour)
}
