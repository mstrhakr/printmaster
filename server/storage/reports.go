package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ReportType defines the type of report.
type ReportType string

const (
	// Inventory reports
	ReportTypeDeviceInventory ReportType = "inventory.devices"
	ReportTypeAgentInventory  ReportType = "inventory.agents"
	ReportTypeSiteInventory   ReportType = "inventory.sites"

	// Usage reports
	ReportTypeUsageSummary  ReportType = "usage.summary"
	ReportTypeUsageByDevice ReportType = "usage.by_device"
	ReportTypeUsageByAgent  ReportType = "usage.by_agent"
	ReportTypeUsageBySite   ReportType = "usage.by_site"
	ReportTypeUsageByTenant ReportType = "usage.by_tenant"
	ReportTypeUsageTrends   ReportType = "usage.trends"
	ReportTypeTopPrinters   ReportType = "usage.top_printers"

	// Supplies reports
	ReportTypeSuppliesStatus   ReportType = "supplies.status"
	ReportTypeSuppliesLow      ReportType = "supplies.low"
	ReportTypeSuppliesCritical ReportType = "supplies.critical"
	ReportTypeSuppliesHistory  ReportType = "supplies.history"

	// Compliance/Health reports
	ReportTypeHealthSummary  ReportType = "health.summary"
	ReportTypeOfflineDevices ReportType = "health.offline"
	ReportTypeErrorDevices   ReportType = "health.errors"
	ReportTypeAgentHealth    ReportType = "health.agents"

	// Alert reports
	ReportTypeAlertSummary ReportType = "alerts.summary"
	ReportTypeAlertHistory ReportType = "alerts.history"
	ReportTypeAlertsByType ReportType = "alerts.by_type"

	// Custom/Ad-hoc
	ReportTypeCustom ReportType = "custom"
)

// ReportFormat defines the output format.
type ReportFormat string

const (
	ReportFormatJSON ReportFormat = "json"
	ReportFormatCSV  ReportFormat = "csv"
	ReportFormatPDF  ReportFormat = "pdf"
	ReportFormatHTML ReportFormat = "html"
)

// ReportStatus defines the status of a report run.
type ReportStatus string

const (
	ReportStatusPending   ReportStatus = "pending"
	ReportStatusRunning   ReportStatus = "running"
	ReportStatusCompleted ReportStatus = "completed"
	ReportStatusFailed    ReportStatus = "failed"
	ReportStatusCancelled ReportStatus = "cancelled"
)

// ReportScope defines the scope level of a report.
type ReportScope string

const (
	ReportScopeFleet  ReportScope = "fleet"
	ReportScopeTenant ReportScope = "tenant"
	ReportScopeSite   ReportScope = "site"
	ReportScopeAgent  ReportScope = "agent"
	ReportScopeDevice ReportScope = "device"
)

// ScheduleFrequency defines how often a scheduled report runs.
type ScheduleFrequency string

const (
	ScheduleFrequencyDaily   ScheduleFrequency = "daily"
	ScheduleFrequencyWeekly  ScheduleFrequency = "weekly"
	ScheduleFrequencyMonthly ScheduleFrequency = "monthly"
)

// ReportDefinition defines a report configuration (template).
type ReportDefinition struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Type        ReportType   `json:"type"`
	Format      ReportFormat `json:"format"`
	Scope       ReportScope  `json:"scope"`

	// Scope filters (narrow down what's included)
	TenantIDs    []string `json:"tenant_ids,omitempty"`
	SiteIDs      []string `json:"site_ids,omitempty"`
	AgentIDs     []string `json:"agent_ids,omitempty"`
	DeviceFilter string   `json:"device_filter,omitempty"` // JSON filter criteria

	// Time range (for usage/history reports)
	TimeRangeType  string `json:"time_range_type,omitempty"`  // "last_24h", "last_7d", "last_30d", "custom"
	TimeRangeDays  int    `json:"time_range_days,omitempty"`  // For custom range
	TimeRangeStart string `json:"time_range_start,omitempty"` // ISO date for absolute range
	TimeRangeEnd   string `json:"time_range_end,omitempty"`   // ISO date for absolute range

	// Report options (type-specific)
	OptionsJSON string `json:"options_json,omitempty"`

	// Columns to include (for tabular reports)
	Columns []string `json:"columns,omitempty"`

	// Grouping/sorting
	GroupBy []string `json:"group_by,omitempty"`
	OrderBy string   `json:"order_by,omitempty"`
	Limit   int      `json:"limit,omitempty"`

	// Distribution
	EmailRecipients []string `json:"email_recipients,omitempty"`
	WebhookURL      string   `json:"webhook_url,omitempty"`

	// Ownership
	CreatedBy string    `json:"created_by,omitempty"`
	IsBuiltIn bool      `json:"is_built_in"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReportSchedule defines when a report runs automatically.
type ReportSchedule struct {
	ID           int64             `json:"id"`
	ReportID     int64             `json:"report_id"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Frequency    ScheduleFrequency `json:"frequency"`
	DayOfWeek    int               `json:"day_of_week,omitempty"`  // 0=Sun, 1=Mon, etc. (for weekly)
	DayOfMonth   int               `json:"day_of_month,omitempty"` // 1-31 (for monthly)
	TimeOfDay    string            `json:"time_of_day"`            // "HH:MM" in 24h format
	Timezone     string            `json:"timezone"`
	NextRunAt    time.Time         `json:"next_run_at"`
	LastRunAt    *time.Time        `json:"last_run_at,omitempty"`
	LastRunID    *int64            `json:"last_run_id,omitempty"`
	FailureCount int               `json:"failure_count"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ReportRun represents a single execution of a report.
type ReportRun struct {
	ID         int64        `json:"id"`
	ReportID   int64        `json:"report_id"`
	ScheduleID *int64       `json:"schedule_id,omitempty"` // nil for ad-hoc runs
	Status     ReportStatus `json:"status"`
	Format     ReportFormat `json:"format"`

	// Execution details
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DurationMS  int64      `json:"duration_ms,omitempty"`

	// Parameters used (snapshot of filters at run time)
	ParametersJSON string `json:"parameters_json,omitempty"`

	// Results
	RowCount   int    `json:"row_count,omitempty"`
	ResultSize int64  `json:"result_size_bytes,omitempty"`
	ResultPath string `json:"result_path,omitempty"` // File path or blob reference
	ResultData string `json:"result_data,omitempty"` // For small reports, store inline

	// Error tracking
	ErrorMessage string `json:"error_message,omitempty"`

	// Who ran it
	RunBy     string    `json:"run_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ReportFilter defines criteria for listing reports.
type ReportFilter struct {
	Type      ReportType
	Scope     ReportScope
	TenantID  string
	CreatedBy string
	IsBuiltIn *bool
	Limit     int
	Offset    int
}

// ReportRunFilter defines criteria for listing report runs.
type ReportRunFilter struct {
	ReportID   int64
	ScheduleID *int64
	Status     ReportStatus
	Since      time.Time
	Limit      int
	Offset     int
}

// ReportSummary provides dashboard-level report statistics.
type ReportSummary struct {
	TotalReports      int   `json:"total_reports"`
	TotalSchedules    int   `json:"total_schedules"`
	ActiveSchedules   int   `json:"active_schedules"`
	TotalRuns         int   `json:"total_runs"`
	RunsLast24h       int   `json:"runs_last_24h"`
	FailedRunsLast24h int   `json:"failed_runs_last_24h"`
	SuccessfulRuns    int   `json:"successful_runs"`
	AverageRunTimeMS  int64 `json:"average_run_time_ms"`
	StorageUsedBytes  int64 `json:"storage_used_bytes"`
}

// initReportsSchema creates the reports tables.
func (s *SQLiteStore) initReportsSchema() error {
	schema := `
	-- Report definitions (templates)
	CREATE TABLE IF NOT EXISTS reports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		description TEXT,
		type TEXT NOT NULL,
		format TEXT NOT NULL DEFAULT 'json',
		scope TEXT NOT NULL DEFAULT 'fleet',
		tenant_ids TEXT,
		site_ids TEXT,
		agent_ids TEXT,
		device_filter TEXT,
		time_range_type TEXT,
		time_range_days INTEGER,
		time_range_start TEXT,
		time_range_end TEXT,
		options_json TEXT,
		columns TEXT,
		group_by TEXT,
		order_by TEXT,
		report_limit INTEGER,
		email_recipients TEXT,
		webhook_url TEXT,
		created_by TEXT,
		is_built_in INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_reports_type ON reports(type);
	CREATE INDEX IF NOT EXISTS idx_reports_scope ON reports(scope);
	CREATE INDEX IF NOT EXISTS idx_reports_created_by ON reports(created_by);

	-- Report schedules
	CREATE TABLE IF NOT EXISTS report_schedules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		frequency TEXT NOT NULL,
		day_of_week INTEGER,
		day_of_month INTEGER,
		time_of_day TEXT NOT NULL,
		timezone TEXT NOT NULL DEFAULT 'UTC',
		next_run_at DATETIME NOT NULL,
		last_run_at DATETIME,
		last_run_id INTEGER REFERENCES report_runs(id),
		failure_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_report_schedules_report ON report_schedules(report_id);
	CREATE INDEX IF NOT EXISTS idx_report_schedules_next_run ON report_schedules(next_run_at) WHERE enabled = 1;

	-- Report runs (execution history)
	CREATE TABLE IF NOT EXISTS report_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_id INTEGER NOT NULL REFERENCES reports(id) ON DELETE CASCADE,
		schedule_id INTEGER REFERENCES report_schedules(id) ON DELETE SET NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		format TEXT NOT NULL,
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME,
		duration_ms INTEGER,
		parameters_json TEXT,
		row_count INTEGER,
		result_size_bytes INTEGER,
		result_path TEXT,
		result_data TEXT,
		error_message TEXT,
		run_by TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_report_runs_report ON report_runs(report_id);
	CREATE INDEX IF NOT EXISTS idx_report_runs_schedule ON report_runs(schedule_id);
	CREATE INDEX IF NOT EXISTS idx_report_runs_status ON report_runs(status);
	CREATE INDEX IF NOT EXISTS idx_report_runs_started ON report_runs(started_at);
	`

	_, err := s.db.Exec(schema)
	return err
}

// CreateReport creates a new report definition.
func (s *SQLiteStore) CreateReport(ctx context.Context, report *ReportDefinition) error {
	tenantIDs := strings.Join(report.TenantIDs, ",")
	siteIDs := strings.Join(report.SiteIDs, ",")
	agentIDs := strings.Join(report.AgentIDs, ",")
	columns := strings.Join(report.Columns, ",")
	groupBy := strings.Join(report.GroupBy, ",")
	emailRecipients := strings.Join(report.EmailRecipients, ",")

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO reports (
			name, description, type, format, scope,
			tenant_ids, site_ids, agent_ids, device_filter,
			time_range_type, time_range_days, time_range_start, time_range_end,
			options_json, columns, group_by, order_by, report_limit,
			email_recipients, webhook_url, created_by, is_built_in
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		report.Name, report.Description, report.Type, report.Format, report.Scope,
		tenantIDs, siteIDs, agentIDs, report.DeviceFilter,
		report.TimeRangeType, report.TimeRangeDays, report.TimeRangeStart, report.TimeRangeEnd,
		report.OptionsJSON, columns, groupBy, report.OrderBy, report.Limit,
		emailRecipients, report.WebhookURL, report.CreatedBy, report.IsBuiltIn,
	)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get report id: %w", err)
	}
	report.ID = id
	report.CreatedAt = time.Now()
	report.UpdatedAt = report.CreatedAt
	return nil
}

// UpdateReport updates an existing report definition.
func (s *SQLiteStore) UpdateReport(ctx context.Context, report *ReportDefinition) error {
	tenantIDs := strings.Join(report.TenantIDs, ",")
	siteIDs := strings.Join(report.SiteIDs, ",")
	agentIDs := strings.Join(report.AgentIDs, ",")
	columns := strings.Join(report.Columns, ",")
	groupBy := strings.Join(report.GroupBy, ",")
	emailRecipients := strings.Join(report.EmailRecipients, ",")

	_, err := s.db.ExecContext(ctx, `
		UPDATE reports SET
			name = ?, description = ?, type = ?, format = ?, scope = ?,
			tenant_ids = ?, site_ids = ?, agent_ids = ?, device_filter = ?,
			time_range_type = ?, time_range_days = ?, time_range_start = ?, time_range_end = ?,
			options_json = ?, columns = ?, group_by = ?, order_by = ?, report_limit = ?,
			email_recipients = ?, webhook_url = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`,
		report.Name, report.Description, report.Type, report.Format, report.Scope,
		tenantIDs, siteIDs, agentIDs, report.DeviceFilter,
		report.TimeRangeType, report.TimeRangeDays, report.TimeRangeStart, report.TimeRangeEnd,
		report.OptionsJSON, columns, groupBy, report.OrderBy, report.Limit,
		emailRecipients, report.WebhookURL, report.ID,
	)
	if err != nil {
		return fmt.Errorf("update report: %w", err)
	}
	report.UpdatedAt = time.Now()
	return nil
}

// GetReport retrieves a report by ID.
func (s *SQLiteStore) GetReport(ctx context.Context, id int64) (*ReportDefinition, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, type, format, scope,
			tenant_ids, site_ids, agent_ids, device_filter,
			time_range_type, time_range_days, time_range_start, time_range_end,
			options_json, columns, group_by, order_by, report_limit,
			email_recipients, webhook_url, created_by, is_built_in,
			created_at, updated_at
		FROM reports WHERE id = ?
	`, id)
	return s.scanReport(row)
}

// DeleteReport deletes a report and its schedules/runs.
func (s *SQLiteStore) DeleteReport(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM reports WHERE id = ?`, id)
	return err
}

// ListReports lists reports matching the filter.
func (s *SQLiteStore) ListReports(ctx context.Context, filter ReportFilter) ([]*ReportDefinition, error) {
	query := `
		SELECT id, name, description, type, format, scope,
			tenant_ids, site_ids, agent_ids, device_filter,
			time_range_type, time_range_days, time_range_start, time_range_end,
			options_json, columns, group_by, order_by, report_limit,
			email_recipients, webhook_url, created_by, is_built_in,
			created_at, updated_at
		FROM reports WHERE 1=1
	`
	var args []interface{}

	if filter.Type != "" {
		query += " AND type = ?"
		args = append(args, filter.Type)
	}
	if filter.Scope != "" {
		query += " AND scope = ?"
		args = append(args, filter.Scope)
	}
	if filter.TenantID != "" {
		query += " AND (tenant_ids = '' OR tenant_ids LIKE ?)"
		args = append(args, "%"+filter.TenantID+"%")
	}
	if filter.CreatedBy != "" {
		query += " AND created_by = ?"
		args = append(args, filter.CreatedBy)
	}
	if filter.IsBuiltIn != nil {
		query += " AND is_built_in = ?"
		if *filter.IsBuiltIn {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	query += " ORDER BY name"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	defer rows.Close()

	var reports []*ReportDefinition
	for rows.Next() {
		r, err := s.scanReportRow(rows)
		if err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// scanReport scans a single report row.
func (s *SQLiteStore) scanReport(row *sql.Row) (*ReportDefinition, error) {
	var r ReportDefinition
	var tenantIDs, siteIDs, agentIDs, columns, groupBy, emailRecipients sql.NullString
	var description, deviceFilter, timeRangeType, timeRangeStart, timeRangeEnd sql.NullString
	var optionsJSON, orderBy, webhookURL, createdBy sql.NullString
	var timeRangeDays, limit sql.NullInt64
	var isBuiltIn int

	err := row.Scan(
		&r.ID, &r.Name, &description, &r.Type, &r.Format, &r.Scope,
		&tenantIDs, &siteIDs, &agentIDs, &deviceFilter,
		&timeRangeType, &timeRangeDays, &timeRangeStart, &timeRangeEnd,
		&optionsJSON, &columns, &groupBy, &orderBy, &limit,
		&emailRecipients, &webhookURL, &createdBy, &isBuiltIn,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan report: %w", err)
	}

	r.Description = description.String
	r.DeviceFilter = deviceFilter.String
	r.TimeRangeType = timeRangeType.String
	r.TimeRangeDays = int(timeRangeDays.Int64)
	r.TimeRangeStart = timeRangeStart.String
	r.TimeRangeEnd = timeRangeEnd.String
	r.OptionsJSON = optionsJSON.String
	r.OrderBy = orderBy.String
	r.Limit = int(limit.Int64)
	r.WebhookURL = webhookURL.String
	r.CreatedBy = createdBy.String
	r.IsBuiltIn = isBuiltIn != 0

	if tenantIDs.String != "" {
		r.TenantIDs = strings.Split(tenantIDs.String, ",")
	}
	if siteIDs.String != "" {
		r.SiteIDs = strings.Split(siteIDs.String, ",")
	}
	if agentIDs.String != "" {
		r.AgentIDs = strings.Split(agentIDs.String, ",")
	}
	if columns.String != "" {
		r.Columns = strings.Split(columns.String, ",")
	}
	if groupBy.String != "" {
		r.GroupBy = strings.Split(groupBy.String, ",")
	}
	if emailRecipients.String != "" {
		r.EmailRecipients = strings.Split(emailRecipients.String, ",")
	}

	return &r, nil
}

// scanReportRow scans from rows.
func (s *SQLiteStore) scanReportRow(rows *sql.Rows) (*ReportDefinition, error) {
	var r ReportDefinition
	var tenantIDs, siteIDs, agentIDs, columns, groupBy, emailRecipients sql.NullString
	var description, deviceFilter, timeRangeType, timeRangeStart, timeRangeEnd sql.NullString
	var optionsJSON, orderBy, webhookURL, createdBy sql.NullString
	var timeRangeDays, limit sql.NullInt64
	var isBuiltIn int

	err := rows.Scan(
		&r.ID, &r.Name, &description, &r.Type, &r.Format, &r.Scope,
		&tenantIDs, &siteIDs, &agentIDs, &deviceFilter,
		&timeRangeType, &timeRangeDays, &timeRangeStart, &timeRangeEnd,
		&optionsJSON, &columns, &groupBy, &orderBy, &limit,
		&emailRecipients, &webhookURL, &createdBy, &isBuiltIn,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan report row: %w", err)
	}

	r.Description = description.String
	r.DeviceFilter = deviceFilter.String
	r.TimeRangeType = timeRangeType.String
	r.TimeRangeDays = int(timeRangeDays.Int64)
	r.TimeRangeStart = timeRangeStart.String
	r.TimeRangeEnd = timeRangeEnd.String
	r.OptionsJSON = optionsJSON.String
	r.OrderBy = orderBy.String
	r.Limit = int(limit.Int64)
	r.WebhookURL = webhookURL.String
	r.CreatedBy = createdBy.String
	r.IsBuiltIn = isBuiltIn != 0

	if tenantIDs.String != "" {
		r.TenantIDs = strings.Split(tenantIDs.String, ",")
	}
	if siteIDs.String != "" {
		r.SiteIDs = strings.Split(siteIDs.String, ",")
	}
	if agentIDs.String != "" {
		r.AgentIDs = strings.Split(agentIDs.String, ",")
	}
	if columns.String != "" {
		r.Columns = strings.Split(columns.String, ",")
	}
	if groupBy.String != "" {
		r.GroupBy = strings.Split(groupBy.String, ",")
	}
	if emailRecipients.String != "" {
		r.EmailRecipients = strings.Split(emailRecipients.String, ",")
	}

	return &r, nil
}

// CreateReportSchedule creates a new schedule.
func (s *SQLiteStore) CreateReportSchedule(ctx context.Context, schedule *ReportSchedule) error {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO report_schedules (
			report_id, name, enabled, frequency,
			day_of_week, day_of_month, time_of_day, timezone,
			next_run_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		schedule.ReportID, schedule.Name, schedule.Enabled, schedule.Frequency,
		schedule.DayOfWeek, schedule.DayOfMonth, schedule.TimeOfDay, schedule.Timezone,
		schedule.NextRunAt,
	)
	if err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get schedule id: %w", err)
	}
	schedule.ID = id
	schedule.CreatedAt = time.Now()
	schedule.UpdatedAt = schedule.CreatedAt
	return nil
}

// UpdateReportSchedule updates a schedule.
func (s *SQLiteStore) UpdateReportSchedule(ctx context.Context, schedule *ReportSchedule) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE report_schedules SET
			name = ?, enabled = ?, frequency = ?,
			day_of_week = ?, day_of_month = ?, time_of_day = ?, timezone = ?,
			next_run_at = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`,
		schedule.Name, schedule.Enabled, schedule.Frequency,
		schedule.DayOfWeek, schedule.DayOfMonth, schedule.TimeOfDay, schedule.Timezone,
		schedule.NextRunAt, schedule.ID,
	)
	return err
}

// GetReportSchedule retrieves a schedule by ID.
func (s *SQLiteStore) GetReportSchedule(ctx context.Context, id int64) (*ReportSchedule, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, report_id, name, enabled, frequency,
			day_of_week, day_of_month, time_of_day, timezone,
			next_run_at, last_run_at, last_run_id, failure_count,
			created_at, updated_at
		FROM report_schedules WHERE id = ?
	`, id)
	return s.scanSchedule(row)
}

// DeleteReportSchedule deletes a schedule.
func (s *SQLiteStore) DeleteReportSchedule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM report_schedules WHERE id = ?`, id)
	return err
}

// ListReportSchedules lists schedules for a report. If reportID is 0, lists all schedules.
func (s *SQLiteStore) ListReportSchedules(ctx context.Context, reportID int64) ([]*ReportSchedule, error) {
	var rows *sql.Rows
	var err error

	if reportID == 0 {
		// List all schedules
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, report_id, name, enabled, frequency,
				day_of_week, day_of_month, time_of_day, timezone,
				next_run_at, last_run_at, last_run_id, failure_count,
				created_at, updated_at
			FROM report_schedules
			ORDER BY name
		`)
	} else {
		// List schedules for specific report
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, report_id, name, enabled, frequency,
				day_of_week, day_of_month, time_of_day, timezone,
				next_run_at, last_run_at, last_run_id, failure_count,
				created_at, updated_at
			FROM report_schedules WHERE report_id = ?
			ORDER BY name
		`, reportID)
	}
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*ReportSchedule
	for rows.Next() {
		s, err := s.scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

// GetDueSchedules returns schedules that need to run.
func (s *SQLiteStore) GetDueSchedules(ctx context.Context, before time.Time) ([]*ReportSchedule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, report_id, name, enabled, frequency,
			day_of_week, day_of_month, time_of_day, timezone,
			next_run_at, last_run_at, last_run_id, failure_count,
			created_at, updated_at
		FROM report_schedules 
		WHERE enabled = 1 AND next_run_at <= ?
		ORDER BY next_run_at
	`, before)
	if err != nil {
		return nil, fmt.Errorf("get due schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*ReportSchedule
	for rows.Next() {
		s, err := s.scanScheduleRow(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	return schedules, rows.Err()
}

// UpdateScheduleAfterRun updates schedule after a run completes.
func (s *SQLiteStore) UpdateScheduleAfterRun(ctx context.Context, scheduleID int64, runID int64, nextRun time.Time, failed bool) error {
	failureIncr := 0
	if failed {
		failureIncr = 1
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE report_schedules SET
			last_run_at = CURRENT_TIMESTAMP,
			last_run_id = ?,
			next_run_at = ?,
			failure_count = CASE WHEN ? = 1 THEN failure_count + 1 ELSE 0 END,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, runID, nextRun, failureIncr, scheduleID)
	return err
}

func (s *SQLiteStore) scanSchedule(row *sql.Row) (*ReportSchedule, error) {
	var sched ReportSchedule
	var dayOfWeek, dayOfMonth sql.NullInt64
	var lastRunAt sql.NullTime
	var lastRunID sql.NullInt64

	err := row.Scan(
		&sched.ID, &sched.ReportID, &sched.Name, &sched.Enabled, &sched.Frequency,
		&dayOfWeek, &dayOfMonth, &sched.TimeOfDay, &sched.Timezone,
		&sched.NextRunAt, &lastRunAt, &lastRunID, &sched.FailureCount,
		&sched.CreatedAt, &sched.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan schedule: %w", err)
	}

	sched.DayOfWeek = int(dayOfWeek.Int64)
	sched.DayOfMonth = int(dayOfMonth.Int64)
	if lastRunAt.Valid {
		sched.LastRunAt = &lastRunAt.Time
	}
	if lastRunID.Valid {
		sched.LastRunID = &lastRunID.Int64
	}

	return &sched, nil
}

func (s *SQLiteStore) scanScheduleRow(rows *sql.Rows) (*ReportSchedule, error) {
	var sched ReportSchedule
	var dayOfWeek, dayOfMonth sql.NullInt64
	var lastRunAt sql.NullTime
	var lastRunID sql.NullInt64

	err := rows.Scan(
		&sched.ID, &sched.ReportID, &sched.Name, &sched.Enabled, &sched.Frequency,
		&dayOfWeek, &dayOfMonth, &sched.TimeOfDay, &sched.Timezone,
		&sched.NextRunAt, &lastRunAt, &lastRunID, &sched.FailureCount,
		&sched.CreatedAt, &sched.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan schedule row: %w", err)
	}

	sched.DayOfWeek = int(dayOfWeek.Int64)
	sched.DayOfMonth = int(dayOfMonth.Int64)
	if lastRunAt.Valid {
		sched.LastRunAt = &lastRunAt.Time
	}
	if lastRunID.Valid {
		sched.LastRunID = &lastRunID.Int64
	}

	return &sched, nil
}

// CreateReportRun creates a new report run.
func (s *SQLiteStore) CreateReportRun(ctx context.Context, run *ReportRun) error {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO report_runs (
			report_id, schedule_id, status, format,
			started_at, parameters_json, run_by
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		run.ReportID, run.ScheduleID, run.Status, run.Format,
		run.StartedAt, run.ParametersJSON, run.RunBy,
	)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get run id: %w", err)
	}
	run.ID = id
	run.CreatedAt = time.Now()
	return nil
}

// UpdateReportRun updates a run (typically to set completion status).
func (s *SQLiteStore) UpdateReportRun(ctx context.Context, run *ReportRun) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE report_runs SET
			status = ?, completed_at = ?, duration_ms = ?,
			row_count = ?, result_size_bytes = ?, result_path = ?, result_data = ?,
			error_message = ?
		WHERE id = ?
	`,
		run.Status, run.CompletedAt, run.DurationMS,
		run.RowCount, run.ResultSize, run.ResultPath, run.ResultData,
		run.ErrorMessage, run.ID,
	)
	return err
}

// GetReportRun retrieves a run by ID.
func (s *SQLiteStore) GetReportRun(ctx context.Context, id int64) (*ReportRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, report_id, schedule_id, status, format,
			started_at, completed_at, duration_ms,
			parameters_json, row_count, result_size_bytes, result_path, result_data,
			error_message, run_by, created_at
		FROM report_runs WHERE id = ?
	`, id)
	return s.scanRun(row)
}

// DeleteReportRun deletes a run.
func (s *SQLiteStore) DeleteReportRun(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM report_runs WHERE id = ?`, id)
	return err
}

// ListReportRuns lists runs matching the filter.
func (s *SQLiteStore) ListReportRuns(ctx context.Context, filter ReportRunFilter) ([]*ReportRun, error) {
	query := `
		SELECT id, report_id, schedule_id, status, format,
			started_at, completed_at, duration_ms,
			parameters_json, row_count, result_size_bytes, result_path, result_data,
			error_message, run_by, created_at
		FROM report_runs WHERE 1=1
	`
	var args []interface{}

	if filter.ReportID > 0 {
		query += " AND report_id = ?"
		args = append(args, filter.ReportID)
	}
	if filter.ScheduleID != nil {
		query += " AND schedule_id = ?"
		args = append(args, *filter.ScheduleID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if !filter.Since.IsZero() {
		query += " AND started_at >= ?"
		args = append(args, filter.Since)
	}

	query += " ORDER BY started_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []*ReportRun
	for rows.Next() {
		r, err := s.scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (s *SQLiteStore) scanRun(row *sql.Row) (*ReportRun, error) {
	var run ReportRun
	var scheduleID sql.NullInt64
	var completedAt sql.NullTime
	var durationMS, rowCount, resultSize sql.NullInt64
	var parametersJSON, resultPath, resultData, errorMessage, runBy sql.NullString

	err := row.Scan(
		&run.ID, &run.ReportID, &scheduleID, &run.Status, &run.Format,
		&run.StartedAt, &completedAt, &durationMS,
		&parametersJSON, &rowCount, &resultSize, &resultPath, &resultData,
		&errorMessage, &runBy, &run.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}

	if scheduleID.Valid {
		run.ScheduleID = &scheduleID.Int64
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	run.DurationMS = durationMS.Int64
	run.RowCount = int(rowCount.Int64)
	run.ResultSize = resultSize.Int64
	run.ParametersJSON = parametersJSON.String
	run.ResultPath = resultPath.String
	run.ResultData = resultData.String
	run.ErrorMessage = errorMessage.String
	run.RunBy = runBy.String

	return &run, nil
}

func (s *SQLiteStore) scanRunRow(rows *sql.Rows) (*ReportRun, error) {
	var run ReportRun
	var scheduleID sql.NullInt64
	var completedAt sql.NullTime
	var durationMS, rowCount, resultSize sql.NullInt64
	var parametersJSON, resultPath, resultData, errorMessage, runBy sql.NullString

	err := rows.Scan(
		&run.ID, &run.ReportID, &scheduleID, &run.Status, &run.Format,
		&run.StartedAt, &completedAt, &durationMS,
		&parametersJSON, &rowCount, &resultSize, &resultPath, &resultData,
		&errorMessage, &runBy, &run.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan run row: %w", err)
	}

	if scheduleID.Valid {
		run.ScheduleID = &scheduleID.Int64
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	run.DurationMS = durationMS.Int64
	run.RowCount = int(rowCount.Int64)
	run.ResultSize = resultSize.Int64
	run.ParametersJSON = parametersJSON.String
	run.ResultPath = resultPath.String
	run.ResultData = resultData.String
	run.ErrorMessage = errorMessage.String
	run.RunBy = runBy.String

	return &run, nil
}

// GetReportSummary returns dashboard statistics for reports.
func (s *SQLiteStore) GetReportSummary(ctx context.Context) (*ReportSummary, error) {
	var summary ReportSummary

	// Total reports
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports`).Scan(&summary.TotalReports)
	if err != nil {
		return nil, fmt.Errorf("count reports: %w", err)
	}

	// Total and active schedules
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_schedules`).Scan(&summary.TotalSchedules)
	if err != nil {
		return nil, fmt.Errorf("count schedules: %w", err)
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_schedules WHERE enabled = 1`).Scan(&summary.ActiveSchedules)
	if err != nil {
		return nil, fmt.Errorf("count active schedules: %w", err)
	}

	// Run statistics
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_runs`).Scan(&summary.TotalRuns)
	if err != nil {
		return nil, fmt.Errorf("count runs: %w", err)
	}

	since24h := time.Now().Add(-24 * time.Hour)
	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM report_runs WHERE started_at >= ?
	`, since24h).Scan(&summary.RunsLast24h)
	if err != nil {
		return nil, fmt.Errorf("count runs 24h: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM report_runs WHERE started_at >= ? AND status = 'failed'
	`, since24h).Scan(&summary.FailedRunsLast24h)
	if err != nil {
		return nil, fmt.Errorf("count failed runs: %w", err)
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM report_runs WHERE status = 'completed'
	`).Scan(&summary.SuccessfulRuns)
	if err != nil {
		return nil, fmt.Errorf("count successful runs: %w", err)
	}

	// Average run time
	var avgTime sql.NullFloat64
	err = s.db.QueryRowContext(ctx, `
		SELECT AVG(duration_ms) FROM report_runs WHERE status = 'completed' AND duration_ms > 0
	`).Scan(&avgTime)
	if err != nil {
		return nil, fmt.Errorf("avg run time: %w", err)
	}
	summary.AverageRunTimeMS = int64(avgTime.Float64)

	// Storage used
	var storageUsed sql.NullInt64
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(result_size_bytes), 0) FROM report_runs
	`).Scan(&storageUsed)
	if err != nil {
		return nil, fmt.Errorf("storage used: %w", err)
	}
	summary.StorageUsedBytes = storageUsed.Int64

	return &summary, nil
}

// CleanupOldReportRuns deletes runs older than the retention period.
func (s *SQLiteStore) CleanupOldReportRuns(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM report_runs WHERE created_at < ?
	`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("cleanup runs: %w", err)
	}
	return result.RowsAffected()
}

// GetBuiltInReportTypes returns a list of available built-in report types with descriptions.
func GetBuiltInReportTypes() []struct {
	Type        ReportType
	Name        string
	Description string
	Category    string
} {
	return []struct {
		Type        ReportType
		Name        string
		Description string
		Category    string
	}{
		// Inventory
		{ReportTypeDeviceInventory, "Device Inventory", "Complete list of all discovered printers with details", "Inventory"},
		{ReportTypeAgentInventory, "Agent Inventory", "List of all agents with status and configuration", "Inventory"},
		{ReportTypeSiteInventory, "Site Inventory", "Sites and their assigned agents/devices", "Inventory"},

		// Usage
		{ReportTypeUsageSummary, "Usage Summary", "Fleet-wide usage statistics for the period", "Usage"},
		{ReportTypeUsageByDevice, "Usage by Device", "Page counts and usage metrics per device", "Usage"},
		{ReportTypeUsageByAgent, "Usage by Agent", "Aggregated usage for devices per agent", "Usage"},
		{ReportTypeUsageBySite, "Usage by Site", "Usage aggregated by site/location", "Usage"},
		{ReportTypeUsageByTenant, "Usage by Tenant", "Usage aggregated by tenant/customer", "Usage"},
		{ReportTypeUsageTrends, "Usage Trends", "Usage trends over time with charts", "Usage"},
		{ReportTypeTopPrinters, "Top Printers", "Most heavily used devices", "Usage"},

		// Supplies
		{ReportTypeSuppliesStatus, "Supplies Status", "Current toner/ink levels for all devices", "Supplies"},
		{ReportTypeSuppliesLow, "Low Supplies", "Devices with supplies below warning threshold", "Supplies"},
		{ReportTypeSuppliesCritical, "Critical Supplies", "Devices with critically low supplies", "Supplies"},
		{ReportTypeSuppliesHistory, "Supplies History", "Toner/ink level changes over time", "Supplies"},

		// Health
		{ReportTypeHealthSummary, "Health Summary", "Fleet health overview with status breakdown", "Health"},
		{ReportTypeOfflineDevices, "Offline Devices", "Devices that haven't reported recently", "Health"},
		{ReportTypeErrorDevices, "Error Devices", "Devices with active errors or warnings", "Health"},
		{ReportTypeAgentHealth, "Agent Health", "Agent connectivity and performance status", "Health"},

		// Alerts
		{ReportTypeAlertSummary, "Alert Summary", "Active and recent alerts summary", "Alerts"},
		{ReportTypeAlertHistory, "Alert History", "Historical alert data for the period", "Alerts"},
		{ReportTypeAlertsByType, "Alerts by Type", "Alerts grouped by type/category", "Alerts"},
	}
}

// SeedBuiltInReports creates default built-in reports.
func (s *SQLiteStore) SeedBuiltInReports(ctx context.Context) error {
	// Check if already seeded
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reports WHERE is_built_in = 1`).Scan(&count)
	if err != nil {
		return fmt.Errorf("check built-in reports: %w", err)
	}
	if count > 0 {
		return nil // Already seeded
	}

	types := GetBuiltInReportTypes()
	for _, t := range types {
		report := &ReportDefinition{
			Name:        t.Name,
			Description: t.Description,
			Type:        t.Type,
			Format:      ReportFormatJSON,
			Scope:       ReportScopeFleet,
			IsBuiltIn:   true,
		}

		// Set sensible defaults based on type
		switch t.Type {
		case ReportTypeUsageSummary, ReportTypeUsageByDevice, ReportTypeUsageByAgent,
			ReportTypeUsageBySite, ReportTypeUsageByTenant, ReportTypeUsageTrends:
			report.TimeRangeType = "last_30d"
		case ReportTypeTopPrinters:
			report.TimeRangeType = "last_30d"
			report.Limit = 10
			report.OrderBy = "page_count DESC"
		case ReportTypeSuppliesLow:
			opts := map[string]interface{}{"threshold": 20}
			data, _ := json.Marshal(opts)
			report.OptionsJSON = string(data)
		case ReportTypeSuppliesCritical:
			opts := map[string]interface{}{"threshold": 10}
			data, _ := json.Marshal(opts)
			report.OptionsJSON = string(data)
		case ReportTypeAlertHistory:
			report.TimeRangeType = "last_7d"
		}

		if err := s.CreateReport(ctx, report); err != nil {
			return fmt.Errorf("seed report %s: %w", t.Name, err)
		}
	}

	return nil
}
