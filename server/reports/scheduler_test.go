package reports

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"printmaster/common/logger"
	"printmaster/server/storage"
)

// mockSchedulerStore implements SchedulerStore for testing.
type mockSchedulerStore struct {
	mu                 sync.Mutex
	mockGeneratorStore // Embed generator store for report generation

	reports       map[int64]*storage.ReportDefinition
	schedules     []*storage.ReportSchedule
	runs          []*storage.ReportRun
	scheduleCalls []scheduleCall

	// Error injection
	getReportErr           error
	getDueSchedulesErr     error
	createRunErr           error
	updateRunErr           error
	updateScheduleAfterErr error
}

type scheduleCall struct {
	scheduleID int64
	runID      int64
	nextRun    time.Time
	failed     bool
}

func newMockSchedulerStore() *mockSchedulerStore {
	return &mockSchedulerStore{
		mockGeneratorStore: *newMockGeneratorStore(),
		reports:            make(map[int64]*storage.ReportDefinition),
		schedules:          []*storage.ReportSchedule{},
		runs:               []*storage.ReportRun{},
		scheduleCalls:      []scheduleCall{},
	}
}

func (m *mockSchedulerStore) GetReport(ctx context.Context, id int64) (*storage.ReportDefinition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getReportErr != nil {
		return nil, m.getReportErr
	}
	return m.reports[id], nil
}

func (m *mockSchedulerStore) GetDueSchedules(ctx context.Context, before time.Time) ([]*storage.ReportSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getDueSchedulesErr != nil {
		return nil, m.getDueSchedulesErr
	}
	var due []*storage.ReportSchedule
	for _, s := range m.schedules {
		if s.NextRunAt.Before(before) || s.NextRunAt.Equal(before) {
			due = append(due, s)
		}
	}
	return due, nil
}

func (m *mockSchedulerStore) CreateReportRun(ctx context.Context, run *storage.ReportRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createRunErr != nil {
		return m.createRunErr
	}
	run.ID = int64(len(m.runs) + 1)
	m.runs = append(m.runs, run)
	return nil
}

func (m *mockSchedulerStore) UpdateReportRun(ctx context.Context, run *storage.ReportRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateRunErr != nil {
		return m.updateRunErr
	}
	for i, r := range m.runs {
		if r.ID == run.ID {
			m.runs[i] = run
			return nil
		}
	}
	return nil
}

func (m *mockSchedulerStore) UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, runID int64, nextRun time.Time, failed bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateScheduleAfterErr != nil {
		return m.updateScheduleAfterErr
	}
	m.scheduleCalls = append(m.scheduleCalls, scheduleCall{
		scheduleID: scheduleID,
		runID:      runID,
		nextRun:    nextRun,
		failed:     failed,
	})
	// Update the schedule's NextRunAt
	for i, s := range m.schedules {
		if s.ID == scheduleID {
			m.schedules[i].NextRunAt = nextRun
			break
		}
	}
	return nil
}

func (m *mockSchedulerStore) getRuns() []*storage.ReportRun {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*storage.ReportRun{}, m.runs...)
}

func (m *mockSchedulerStore) getScheduleCalls() []scheduleCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]scheduleCall{}, m.scheduleCalls...)
}

// testLogger creates a logger for testing (writes to temp dir, minimal buffer)
func testLogger() *logger.Logger {
	return logger.New(logger.INFO, "", 100)
}

// --- Tests ---

func TestNewScheduler(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()

	s := NewScheduler(store, log)

	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
	if s.interval != 1*time.Minute {
		t.Errorf("default interval = %v, want 1m", s.interval)
	}
}

func TestScheduler_SetInterval(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	s.SetInterval(30 * time.Second)

	if s.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", s.interval)
	}
}

func TestScheduler_StartStop(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)
	s.SetInterval(100 * time.Millisecond)

	s.Start()
	time.Sleep(50 * time.Millisecond)
	s.Stop()
}

func TestScheduler_DoubleStart(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	s.Start()
	s.Start() // Should be no-op
	s.Stop()
}

func TestScheduler_DoubleStop(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	s.Start()
	s.Stop()
	s.Stop() // Should be no-op
}

func TestScheduler_StopWithoutStart(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	// Should not panic
	s.Stop()
}

func TestScheduler_RunsDueSchedules(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	store.reports[1] = &storage.ReportDefinition{
		ID:            1,
		Name:          "Test Report",
		Type:          storage.ReportTypeDeviceInventory,
		Format:        storage.ReportFormatJSON,
		TimeRangeType: "last_30d",
	}
	store.schedules = []*storage.ReportSchedule{
		{
			ID:        1,
			ReportID:  1,
			Frequency: storage.ScheduleFrequencyDaily,
			TimeOfDay: "00:00",
			NextRunAt: time.Now().Add(-1 * time.Hour), // Due
		},
	}

	log := testLogger()
	s := NewScheduler(store, log)
	s.SetInterval(100 * time.Millisecond)

	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	runs := store.getRuns()
	if len(runs) == 0 {
		t.Error("expected at least one run to be created")
	}

	calls := store.getScheduleCalls()
	if len(calls) == 0 {
		t.Error("expected UpdateScheduleAfterRun to be called")
	}
}

func TestScheduler_SkipsNotDueSchedules(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	store.reports[1] = &storage.ReportDefinition{
		ID:            1,
		Name:          "Future Report",
		Type:          storage.ReportTypeDeviceInventory,
		Format:        storage.ReportFormatJSON,
		TimeRangeType: "last_30d",
	}
	store.schedules = []*storage.ReportSchedule{
		{
			ID:        1,
			ReportID:  1,
			Frequency: storage.ScheduleFrequencyDaily,
			TimeOfDay: "00:00",
			NextRunAt: time.Now().Add(24 * time.Hour), // Not due
		},
	}

	log := testLogger()
	s := NewScheduler(store, log)
	s.SetInterval(100 * time.Millisecond)

	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	runs := store.getRuns()
	if len(runs) != 0 {
		t.Errorf("expected no runs, got %d", len(runs))
	}
}

func TestScheduler_GetDueSchedulesError(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	store.getDueSchedulesErr = errors.New("database error")

	log := testLogger()
	s := NewScheduler(store, log)
	s.SetInterval(100 * time.Millisecond)

	// Should not panic
	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()
}

func TestScheduler_ReportNotFound(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	// No report with ID 1
	store.schedules = []*storage.ReportSchedule{
		{
			ID:        1,
			ReportID:  1, // Doesn't exist
			NextRunAt: time.Now().Add(-1 * time.Hour),
		},
	}

	log := testLogger()
	s := NewScheduler(store, log)
	s.SetInterval(100 * time.Millisecond)

	// Should handle gracefully
	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()
}

func TestScheduler_CalculateTimeRange(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	tests := []struct {
		name          string
		timeRangeType string
		timeRangeDays int
		checkStart    func(start, end time.Time) bool
	}{
		{
			name:          "current",
			timeRangeType: "current",
			checkStart: func(start, end time.Time) bool {
				// Start should be close to end
				return end.Sub(start) < time.Second
			},
		},
		{
			name:          "last_24h",
			timeRangeType: "last_24h",
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 23*time.Hour && diff <= 25*time.Hour
			},
		},
		{
			name:          "last_7d",
			timeRangeType: "last_7d",
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 6*24*time.Hour && diff <= 8*24*time.Hour
			},
		},
		{
			name:          "last_30d",
			timeRangeType: "last_30d",
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 29*24*time.Hour && diff <= 31*24*time.Hour
			},
		},
		{
			name:          "last_90d",
			timeRangeType: "last_90d",
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 89*24*time.Hour && diff <= 91*24*time.Hour
			},
		},
		{
			name:          "custom_days",
			timeRangeType: "custom",
			timeRangeDays: 14,
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 13*24*time.Hour && diff <= 15*24*time.Hour
			},
		},
		{
			name:          "unknown_defaults_to_30d",
			timeRangeType: "unknown",
			checkStart: func(start, end time.Time) bool {
				diff := end.Sub(start)
				return diff >= 29*24*time.Hour && diff <= 31*24*time.Hour
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			report := &storage.ReportDefinition{
				TimeRangeType: tt.timeRangeType,
				TimeRangeDays: tt.timeRangeDays,
			}

			start, end := s.calculateTimeRange(report)

			if !tt.checkStart(start, end) {
				t.Errorf("unexpected time range: start=%v, end=%v, diff=%v",
					start, end, end.Sub(start))
			}
		})
	}
}

func TestScheduler_CalculateNextRun_Daily(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	schedule := &storage.ReportSchedule{
		Frequency: storage.ScheduleFrequencyDaily,
		TimeOfDay: "09:00",
		Timezone:  "UTC",
	}

	next := s.calculateNextRun(schedule)

	// Should be within 24 hours from now
	if next.Before(time.Now()) {
		t.Error("next run should be in the future")
	}
	if next.After(time.Now().Add(25 * time.Hour)) {
		t.Error("next run should be within ~24 hours")
	}

	// Should be at 09:00
	if next.Hour() != 9 || next.Minute() != 0 {
		t.Errorf("expected time 09:00, got %02d:%02d", next.Hour(), next.Minute())
	}
}

func TestScheduler_CalculateNextRun_Weekly(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	schedule := &storage.ReportSchedule{
		Frequency: storage.ScheduleFrequencyWeekly,
		TimeOfDay: "10:00",
		DayOfWeek: 1, // Monday
		Timezone:  "UTC",
	}

	next := s.calculateNextRun(schedule)

	// Should be in the future
	if next.Before(time.Now()) {
		t.Error("next run should be in the future")
	}

	// Should be on Monday
	if next.Weekday() != time.Monday {
		t.Errorf("expected Monday, got %v", next.Weekday())
	}
}

func TestScheduler_CalculateNextRun_Monthly(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	schedule := &storage.ReportSchedule{
		Frequency:  storage.ScheduleFrequencyMonthly,
		TimeOfDay:  "08:00",
		DayOfMonth: 15,
		Timezone:   "UTC",
	}

	next := s.calculateNextRun(schedule)

	// Should be in the future
	if next.Before(time.Now()) {
		t.Error("next run should be in the future")
	}

	// Should be on day 15
	if next.Day() != 15 {
		t.Errorf("expected day 15, got %d", next.Day())
	}
}

func TestScheduler_RunNow_Success(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	store.reports[1] = &storage.ReportDefinition{
		ID:            1,
		Name:          "Ad-hoc Report",
		Type:          storage.ReportTypeDeviceInventory,
		Format:        storage.ReportFormatJSON,
		TimeRangeType: "last_30d",
	}

	log := testLogger()
	s := NewScheduler(store, log)

	ctx := context.Background()
	run, err := s.RunNow(ctx, 1, "admin")

	if err != nil {
		t.Fatalf("RunNow failed: %v", err)
	}
	if run == nil {
		t.Fatal("run should not be nil")
	}
	if run.Status != storage.ReportStatusCompleted {
		t.Errorf("run status = %v, want completed", run.Status)
	}
	if run.RunBy != "admin" {
		t.Errorf("run.RunBy = %v, want admin", run.RunBy)
	}
}

func TestScheduler_RunNow_ReportNotFound(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	// No reports

	log := testLogger()
	s := NewScheduler(store, log)

	ctx := context.Background()
	_, err := s.RunNow(ctx, 999, "admin")

	if err == nil {
		t.Error("expected error for missing report")
	}
}

func TestScheduler_RunNow_CreateRunError(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	store.reports[1] = &storage.ReportDefinition{
		ID:            1,
		Name:          "Test Report",
		Type:          storage.ReportTypeDeviceInventory,
		Format:        storage.ReportFormatJSON,
		TimeRangeType: "last_30d",
	}
	store.createRunErr = errors.New("db error")

	log := testLogger()
	s := NewScheduler(store, log)

	ctx := context.Background()
	_, err := s.RunNow(ctx, 1, "admin")

	if err == nil {
		t.Error("expected error when create run fails")
	}
}

func TestScheduler_RunNow_AllFormats(t *testing.T) {
	t.Parallel()

	formats := []string{
		storage.ReportFormatJSON,
		storage.ReportFormatCSV,
		storage.ReportFormatHTML,
	}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			store := newMockSchedulerStore()
			store.reports[1] = &storage.ReportDefinition{
				ID:            1,
				Name:          "Test Report",
				Type:          storage.ReportTypeDeviceInventory,
				Format:        format,
				TimeRangeType: "last_30d",
			}

			log := testLogger()
			s := NewScheduler(store, log)

			ctx := context.Background()
			run, err := s.RunNow(ctx, 1, "tester")

			if err != nil {
				t.Fatalf("RunNow(%s) failed: %v", format, err)
			}
			if run.Format != format {
				t.Errorf("run.Format = %v, want %v", run.Format, format)
			}
			if run.ResultSize == 0 {
				t.Error("ResultSize should be non-zero")
			}
		})
	}
}

func TestScheduler_CustomTimeRange_AbsoluteDates(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	report := &storage.ReportDefinition{
		TimeRangeType:  "custom",
		TimeRangeStart: "2025-01-01",
		TimeRangeEnd:   "2025-01-31",
	}

	start, end := s.calculateTimeRange(report)

	expectedStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	expectedEnd := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC)

	if !start.Equal(expectedStart) {
		t.Errorf("start = %v, want %v", start, expectedStart)
	}
	if !end.Equal(expectedEnd) {
		t.Errorf("end = %v, want %v", end, expectedEnd)
	}
}

func TestScheduler_InvalidTimezone(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	schedule := &storage.ReportSchedule{
		Frequency: storage.ScheduleFrequencyDaily,
		TimeOfDay: "09:00",
		Timezone:  "Invalid/Timezone",
	}

	// Should not panic, should fall back to UTC
	next := s.calculateNextRun(schedule)

	if next.Before(time.Now()) {
		t.Error("next run should be in the future")
	}
}

func TestScheduler_UnknownFrequency(t *testing.T) {
	t.Parallel()

	store := newMockSchedulerStore()
	log := testLogger()
	s := NewScheduler(store, log)

	schedule := &storage.ReportSchedule{
		Frequency: "unknown",
		TimeOfDay: "09:00",
		Timezone:  "UTC",
	}

	next := s.calculateNextRun(schedule)

	// Should default to 24 hours from now
	diff := time.Until(next)
	if diff < 23*time.Hour || diff > 25*time.Hour {
		t.Errorf("expected ~24 hours from now, got %v", diff)
	}
}
