package reports

import (
	"context"
	"fmt"
	"printmaster/common/logger"
	"printmaster/server/storage"
	"sync"
	"time"
)

// SchedulerStore defines storage operations for the scheduler.
type SchedulerStore interface {
	GeneratorStore

	// Reports
	GetReport(ctx context.Context, id int64) (*storage.ReportDefinition, error)

	// Schedules
	GetDueSchedules(ctx context.Context, before time.Time) ([]*storage.ReportSchedule, error)
	UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, runID int64, nextRun time.Time, failed bool) error

	// Runs
	CreateReportRun(ctx context.Context, run *storage.ReportRun) error
	UpdateReportRun(ctx context.Context, run *storage.ReportRun) error
}

// Scheduler runs scheduled reports.
type Scheduler struct {
	store     SchedulerStore
	generator *Generator
	formatter *Formatter
	logger    *logger.Logger

	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewScheduler creates a new report scheduler.
func NewScheduler(store SchedulerStore, log *logger.Logger) *Scheduler {
	return &Scheduler{
		store:     store,
		generator: NewGenerator(store),
		formatter: NewFormatter(),
		logger:    log,
		interval:  1 * time.Minute,
		stopCh:    make(chan struct{}),
	}
}

// SetInterval sets the check interval for due schedules.
func (s *Scheduler) SetInterval(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = d
}

// Start begins the scheduler background loop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop()

	s.logger.Info("Report scheduler started", "interval", s.interval)
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	s.wg.Wait()
	s.logger.Info("Report scheduler stopped")
}

func (s *Scheduler) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run once immediately
	s.checkAndRun()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAndRun()
		}
	}
}

func (s *Scheduler) checkAndRun() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Get schedules that are due
	schedules, err := s.store.GetDueSchedules(ctx, time.Now())
	if err != nil {
		s.logger.Error("Failed to get due schedules", "error", err)
		return
	}

	for _, schedule := range schedules {
		if err := s.runSchedule(ctx, schedule); err != nil {
			s.logger.Error("Failed to run scheduled report",
				"schedule_id", schedule.ID,
				"report_id", schedule.ReportID,
				"error", err)
		}
	}
}

func (s *Scheduler) runSchedule(ctx context.Context, schedule *storage.ReportSchedule) error {
	// Get the report definition
	report, err := s.store.GetReport(ctx, schedule.ReportID)
	if err != nil {
		return fmt.Errorf("get report: %w", err)
	}
	if report == nil {
		return fmt.Errorf("report %d not found", schedule.ReportID)
	}

	// Create a run record
	run := &storage.ReportRun{
		ReportID:   report.ID,
		ScheduleID: &schedule.ID,
		Status:     storage.ReportStatusRunning,
		Format:     report.Format,
		StartedAt:  time.Now(),
		RunBy:      "scheduler",
	}

	if err := s.store.CreateReportRun(ctx, run); err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	// Calculate time range
	startTime, endTime := s.calculateTimeRange(report)

	// Generate the report
	params := GenerateParams{
		Report:    report,
		StartTime: startTime,
		EndTime:   endTime,
	}

	result, err := s.generator.Generate(ctx, params)
	if err != nil {
		// Mark as failed
		now := time.Now()
		run.Status = storage.ReportStatusFailed
		run.CompletedAt = &now
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
		run.ErrorMessage = err.Error()
		s.store.UpdateReportRun(ctx, run)

		// Update schedule
		nextRun := s.calculateNextRun(schedule)
		s.store.UpdateScheduleAfterRun(ctx, schedule.ID, run.ID, nextRun, true)

		return fmt.Errorf("generate report: %w", err)
	}

	// Format the result
	var data []byte
	switch report.Format {
	case storage.ReportFormatJSON:
		data, err = s.formatter.FormatJSON(result, true)
	case storage.ReportFormatCSV:
		data, err = s.formatter.FormatCSV(result)
	case storage.ReportFormatHTML:
		data, err = s.formatter.FormatHTML(result, report.Name)
	default:
		data, err = s.formatter.FormatJSON(result, true)
	}

	if err != nil {
		now := time.Now()
		run.Status = storage.ReportStatusFailed
		run.CompletedAt = &now
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
		run.ErrorMessage = fmt.Sprintf("format error: %v", err)
		s.store.UpdateReportRun(ctx, run)

		nextRun := s.calculateNextRun(schedule)
		s.store.UpdateScheduleAfterRun(ctx, schedule.ID, run.ID, nextRun, true)

		return fmt.Errorf("format report: %w", err)
	}

	// Update run with results
	now := time.Now()
	run.Status = storage.ReportStatusCompleted
	run.CompletedAt = &now
	run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
	run.RowCount = result.RowCount
	run.ResultSize = int64(len(data))
	run.ResultData = string(data) // Store inline for now

	if err := s.store.UpdateReportRun(ctx, run); err != nil {
		return fmt.Errorf("update run: %w", err)
	}

	// Update schedule
	nextRun := s.calculateNextRun(schedule)
	if err := s.store.UpdateScheduleAfterRun(ctx, schedule.ID, run.ID, nextRun, false); err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}

	s.logger.Info("Scheduled report completed",
		"report_id", report.ID,
		"report_name", report.Name,
		"schedule_id", schedule.ID,
		"run_id", run.ID,
		"row_count", result.RowCount,
		"duration_ms", run.DurationMS)

	// TODO: Send notifications (email, webhook) if configured
	// This would use the report.EmailRecipients and report.WebhookURL

	return nil
}

func (s *Scheduler) calculateTimeRange(report *storage.ReportDefinition) (time.Time, time.Time) {
	now := time.Now().UTC()
	endTime := now

	switch report.TimeRangeType {
	case "last_24h":
		return now.Add(-24 * time.Hour), endTime
	case "last_7d":
		return now.Add(-7 * 24 * time.Hour), endTime
	case "last_30d":
		return now.Add(-30 * 24 * time.Hour), endTime
	case "last_90d":
		return now.Add(-90 * 24 * time.Hour), endTime
	case "custom":
		if report.TimeRangeDays > 0 {
			return now.Add(-time.Duration(report.TimeRangeDays) * 24 * time.Hour), endTime
		}
		// Try to parse absolute dates
		if report.TimeRangeStart != "" && report.TimeRangeEnd != "" {
			start, err1 := time.Parse("2006-01-02", report.TimeRangeStart)
			end, err2 := time.Parse("2006-01-02", report.TimeRangeEnd)
			if err1 == nil && err2 == nil {
				return start, end
			}
		}
	}

	// Default to last 30 days
	return now.Add(-30 * 24 * time.Hour), endTime
}

func (s *Scheduler) calculateNextRun(schedule *storage.ReportSchedule) time.Time {
	now := time.Now()

	// Parse time of day
	var hour, min int
	fmt.Sscanf(schedule.TimeOfDay, "%d:%d", &hour, &min)

	// Load timezone
	loc := time.UTC
	if schedule.Timezone != "" {
		if l, err := time.LoadLocation(schedule.Timezone); err == nil {
			loc = l
		}
	}

	// Start from now in the schedule's timezone
	nowLocal := now.In(loc)

	switch schedule.Frequency {
	case storage.ScheduleFrequencyDaily:
		// Next occurrence of the specified time
		next := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, min, 0, 0, loc)
		if next.Before(now) || next.Equal(now) {
			next = next.Add(24 * time.Hour)
		}
		return next.UTC()

	case storage.ScheduleFrequencyWeekly:
		// Find next occurrence of the specified day of week
		next := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, min, 0, 0, loc)
		daysUntil := (schedule.DayOfWeek - int(next.Weekday()) + 7) % 7
		if daysUntil == 0 && (next.Before(now) || next.Equal(now)) {
			daysUntil = 7
		}
		next = next.Add(time.Duration(daysUntil) * 24 * time.Hour)
		return next.UTC()

	case storage.ScheduleFrequencyMonthly:
		// Find next occurrence of the specified day of month
		next := time.Date(nowLocal.Year(), nowLocal.Month(), schedule.DayOfMonth, hour, min, 0, 0, loc)
		if next.Before(now) || next.Equal(now) {
			next = next.AddDate(0, 1, 0)
		}
		return next.UTC()
	}

	// Default: 24 hours from now
	return now.Add(24 * time.Hour)
}

// RunNow executes a report immediately (ad-hoc run).
func (s *Scheduler) RunNow(ctx context.Context, reportID int64, runBy string) (*storage.ReportRun, error) {
	report, err := s.store.GetReport(ctx, reportID)
	if err != nil {
		return nil, fmt.Errorf("get report: %w", err)
	}
	if report == nil {
		return nil, fmt.Errorf("report %d not found", reportID)
	}

	// Create run record
	run := &storage.ReportRun{
		ReportID:  report.ID,
		Status:    storage.ReportStatusRunning,
		Format:    report.Format,
		StartedAt: time.Now(),
		RunBy:     runBy,
	}

	if err := s.store.CreateReportRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Calculate time range
	startTime, endTime := s.calculateTimeRange(report)

	// Generate
	params := GenerateParams{
		Report:    report,
		StartTime: startTime,
		EndTime:   endTime,
	}

	result, err := s.generator.Generate(ctx, params)
	if err != nil {
		now := time.Now()
		run.Status = storage.ReportStatusFailed
		run.CompletedAt = &now
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
		run.ErrorMessage = err.Error()
		s.store.UpdateReportRun(ctx, run)
		return run, fmt.Errorf("generate: %w", err)
	}

	// Format
	var data []byte
	switch report.Format {
	case storage.ReportFormatJSON:
		data, err = s.formatter.FormatJSON(result, true)
	case storage.ReportFormatCSV:
		data, err = s.formatter.FormatCSV(result)
	case storage.ReportFormatHTML:
		data, err = s.formatter.FormatHTML(result, report.Name)
	default:
		data, err = s.formatter.FormatJSON(result, true)
	}

	if err != nil {
		now := time.Now()
		run.Status = storage.ReportStatusFailed
		run.CompletedAt = &now
		run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
		run.ErrorMessage = fmt.Sprintf("format error: %v", err)
		s.store.UpdateReportRun(ctx, run)
		return run, fmt.Errorf("format: %w", err)
	}

	// Update run
	now := time.Now()
	run.Status = storage.ReportStatusCompleted
	run.CompletedAt = &now
	run.DurationMS = now.Sub(run.StartedAt).Milliseconds()
	run.RowCount = result.RowCount
	run.ResultSize = int64(len(data))
	run.ResultData = string(data)

	if err := s.store.UpdateReportRun(ctx, run); err != nil {
		return run, fmt.Errorf("update run: %w", err)
	}

	return run, nil
}
