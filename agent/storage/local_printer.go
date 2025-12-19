package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// LocalPrinterStore defines operations for local printer storage
type LocalPrinterStore interface {
	// UpsertLocalPrinter creates or updates a local printer
	UpsertLocalPrinter(ctx context.Context, printer *LocalPrinter) error

	// GetLocalPrinter retrieves a local printer by name
	GetLocalPrinter(ctx context.Context, name string) (*LocalPrinter, error)

	// ListLocalPrinters returns local printers matching the filter
	ListLocalPrinters(ctx context.Context, filter LocalPrinterFilter) ([]*LocalPrinter, error)

	// UpdateLocalPrinterPages updates the page count for a local printer
	UpdateLocalPrinterPages(ctx context.Context, name string, pages, colorPages, monoPages int64) error

	// SetLocalPrinterBaseline sets the baseline page count for a printer
	SetLocalPrinterBaseline(ctx context.Context, name string, baseline int64) error

	// SetLocalPrinterTracking enables/disables tracking for a printer
	SetLocalPrinterTracking(ctx context.Context, name string, enabled bool) error

	// UpdateLocalPrinterInfo updates user-editable fields for a printer
	UpdateLocalPrinterInfo(ctx context.Context, name string, updates map[string]interface{}) error

	// DeleteLocalPrinter removes a local printer
	DeleteLocalPrinter(ctx context.Context, name string) error

	// AddLocalPrintJob records a completed print job
	AddLocalPrintJob(ctx context.Context, job *LocalPrintJob) error

	// GetLocalPrintJobs returns print jobs for a printer, newest first
	GetLocalPrintJobs(ctx context.Context, printerName string, limit int) ([]*LocalPrintJob, error)

	// DeleteOldLocalPrintJobs removes jobs older than the given timestamp
	DeleteOldLocalPrintJobs(ctx context.Context, olderThan time.Time) (int, error)

	// GetLocalPrinterStats returns statistics for a printer
	GetLocalPrinterStats(ctx context.Context, name string, since time.Time) (map[string]interface{}, error)
}

// LocalPrinter represents a printer discovered through the local print spooler
type LocalPrinter struct {
	Name            string    `json:"name"`
	PortName        string    `json:"port_name"`
	DriverName      string    `json:"driver_name,omitempty"`
	PrinterType     string    `json:"printer_type"`
	IsDefault       bool      `json:"is_default"`
	IsShared        bool      `json:"is_shared"`
	Manufacturer    string    `json:"manufacturer,omitempty"`
	Model           string    `json:"model,omitempty"`
	SerialNumber    string    `json:"serial_number,omitempty"`
	Status          string    `json:"status"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	TotalPages      int64     `json:"total_pages"`
	TotalColorPages int64     `json:"total_color_pages"`
	TotalMonoPages  int64     `json:"total_mono_pages"`
	BaselinePages   int64     `json:"baseline_pages"`
	LastPageUpdate  time.Time `json:"last_page_update,omitempty"`
	TrackingEnabled bool      `json:"tracking_enabled"`
	AssetNumber     string    `json:"asset_number,omitempty"`
	Location        string    `json:"location,omitempty"`
	Description     string    `json:"description,omitempty"`
}

// TotalPageCount returns the effective page count (baseline + tracked)
func (p *LocalPrinter) TotalPageCount() int64 {
	return p.BaselinePages + p.TotalPages
}

// LocalPrintJob represents a completed print job from a local printer
type LocalPrintJob struct {
	ID           int64     `json:"id"`
	PrinterName  string    `json:"printer_name"`
	JobID        uint32    `json:"job_id"`
	DocumentName string    `json:"document_name,omitempty"`
	UserName     string    `json:"user_name,omitempty"`
	MachineName  string    `json:"machine_name,omitempty"`
	TotalPages   int32     `json:"total_pages"`
	PagesPrinted int32     `json:"pages_printed"`
	IsColor      bool      `json:"is_color"`
	SizeBytes    int64     `json:"size_bytes"`
	SubmittedAt  time.Time `json:"submitted_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Status       string    `json:"status"`
}

// LocalPrinterFilter allows filtering local printers
type LocalPrinterFilter struct {
	PrinterType     *string // Filter by type (nil = all)
	TrackingEnabled *bool   // Filter by tracking status (nil = all)
	Name            string  // Filter by name (contains, case-insensitive)
	Limit           int     // Max results (0 = no limit)
}

// UpsertLocalPrinter creates or updates a local printer
func (s *SQLiteStore) UpsertLocalPrinter(ctx context.Context, printer *LocalPrinter) error {
	if printer == nil || printer.Name == "" {
		return fmt.Errorf("printer name is required")
	}

	now := time.Now()
	if printer.FirstSeen.IsZero() {
		printer.FirstSeen = now
	}
	if printer.LastSeen.IsZero() {
		printer.LastSeen = now
	}

	query := `
		INSERT INTO local_printers (
			name, port_name, driver_name, printer_type, is_default, is_shared,
			manufacturer, model, serial_number, status, first_seen, last_seen,
			total_pages, total_color_pages, total_mono_pages, baseline_pages,
			last_page_update, tracking_enabled, asset_number, location, description
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			port_name = excluded.port_name,
			driver_name = excluded.driver_name,
			printer_type = excluded.printer_type,
			is_default = excluded.is_default,
			is_shared = excluded.is_shared,
			manufacturer = COALESCE(NULLIF(excluded.manufacturer, ''), local_printers.manufacturer),
			model = COALESCE(NULLIF(excluded.model, ''), local_printers.model),
			serial_number = COALESCE(NULLIF(excluded.serial_number, ''), local_printers.serial_number),
			status = excluded.status,
			last_seen = excluded.last_seen
	`

	var lastPageUpdate interface{}
	if !printer.LastPageUpdate.IsZero() {
		lastPageUpdate = printer.LastPageUpdate
	}

	_, err := s.db.ExecContext(ctx, query,
		printer.Name, printer.PortName, printer.DriverName, printer.PrinterType,
		printer.IsDefault, printer.IsShared, printer.Manufacturer, printer.Model,
		printer.SerialNumber, printer.Status, printer.FirstSeen, printer.LastSeen,
		printer.TotalPages, printer.TotalColorPages, printer.TotalMonoPages,
		printer.BaselinePages, lastPageUpdate, printer.TrackingEnabled,
		printer.AssetNumber, printer.Location, printer.Description,
	)
	return err
}

// GetLocalPrinter retrieves a local printer by name
func (s *SQLiteStore) GetLocalPrinter(ctx context.Context, name string) (*LocalPrinter, error) {
	query := `
		SELECT name, port_name, driver_name, printer_type, is_default, is_shared,
			manufacturer, model, serial_number, status, first_seen, last_seen,
			total_pages, total_color_pages, total_mono_pages, baseline_pages,
			last_page_update, tracking_enabled, asset_number, location, description
		FROM local_printers
		WHERE name = ?
	`

	printer := &LocalPrinter{}
	var driverName, manufacturer, model, serialNumber, assetNumber, location, description sql.NullString
	var lastPageUpdate sql.NullTime

	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&printer.Name, &printer.PortName, &driverName, &printer.PrinterType,
		&printer.IsDefault, &printer.IsShared, &manufacturer, &model,
		&serialNumber, &printer.Status, &printer.FirstSeen, &printer.LastSeen,
		&printer.TotalPages, &printer.TotalColorPages, &printer.TotalMonoPages,
		&printer.BaselinePages, &lastPageUpdate, &printer.TrackingEnabled,
		&assetNumber, &location, &description,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	printer.DriverName = driverName.String
	printer.Manufacturer = manufacturer.String
	printer.Model = model.String
	printer.SerialNumber = serialNumber.String
	printer.AssetNumber = assetNumber.String
	printer.Location = location.String
	printer.Description = description.String
	if lastPageUpdate.Valid {
		printer.LastPageUpdate = lastPageUpdate.Time
	}

	return printer, nil
}

// ListLocalPrinters returns local printers matching the filter
func (s *SQLiteStore) ListLocalPrinters(ctx context.Context, filter LocalPrinterFilter) ([]*LocalPrinter, error) {
	query := `
		SELECT name, port_name, driver_name, printer_type, is_default, is_shared,
			manufacturer, model, serial_number, status, first_seen, last_seen,
			total_pages, total_color_pages, total_mono_pages, baseline_pages,
			last_page_update, tracking_enabled, asset_number, location, description
		FROM local_printers
		WHERE 1=1
	`
	var args []interface{}

	if filter.PrinterType != nil {
		query += " AND printer_type = ?"
		args = append(args, *filter.PrinterType)
	}
	if filter.TrackingEnabled != nil {
		query += " AND tracking_enabled = ?"
		args = append(args, *filter.TrackingEnabled)
	}
	if filter.Name != "" {
		query += " AND name LIKE ?"
		args = append(args, "%"+filter.Name+"%")
	}

	query += " ORDER BY name"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var printers []*LocalPrinter
	for rows.Next() {
		printer := &LocalPrinter{}
		var driverName, manufacturer, model, serialNumber, assetNumber, location, description sql.NullString
		var lastPageUpdate sql.NullTime

		err := rows.Scan(
			&printer.Name, &printer.PortName, &driverName, &printer.PrinterType,
			&printer.IsDefault, &printer.IsShared, &manufacturer, &model,
			&serialNumber, &printer.Status, &printer.FirstSeen, &printer.LastSeen,
			&printer.TotalPages, &printer.TotalColorPages, &printer.TotalMonoPages,
			&printer.BaselinePages, &lastPageUpdate, &printer.TrackingEnabled,
			&assetNumber, &location, &description,
		)
		if err != nil {
			return nil, err
		}

		printer.DriverName = driverName.String
		printer.Manufacturer = manufacturer.String
		printer.Model = model.String
		printer.SerialNumber = serialNumber.String
		printer.AssetNumber = assetNumber.String
		printer.Location = location.String
		printer.Description = description.String
		if lastPageUpdate.Valid {
			printer.LastPageUpdate = lastPageUpdate.Time
		}

		printers = append(printers, printer)
	}

	return printers, nil
}

// UpdateLocalPrinterPages updates the page count for a local printer
func (s *SQLiteStore) UpdateLocalPrinterPages(ctx context.Context, name string, pages, colorPages, monoPages int64) error {
	query := `
		UPDATE local_printers
		SET total_pages = total_pages + ?,
			total_color_pages = total_color_pages + ?,
			total_mono_pages = total_mono_pages + ?,
			last_page_update = ?
		WHERE name = ? AND tracking_enabled = 1
	`
	result, err := s.db.ExecContext(ctx, query, pages, colorPages, monoPages, time.Now(), name)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetLocalPrinterBaseline sets the baseline page count for a printer
func (s *SQLiteStore) SetLocalPrinterBaseline(ctx context.Context, name string, baseline int64) error {
	query := `UPDATE local_printers SET baseline_pages = ? WHERE name = ?`
	result, err := s.db.ExecContext(ctx, query, baseline, name)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetLocalPrinterTracking enables/disables tracking for a printer
func (s *SQLiteStore) SetLocalPrinterTracking(ctx context.Context, name string, enabled bool) error {
	query := `UPDATE local_printers SET tracking_enabled = ? WHERE name = ?`
	result, err := s.db.ExecContext(ctx, query, enabled, name)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLocalPrinterInfo updates user-editable fields for a printer
func (s *SQLiteStore) UpdateLocalPrinterInfo(ctx context.Context, name string, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}

	// Only allow specific fields to be updated
	allowed := map[string]bool{
		"manufacturer":  true,
		"model":         true,
		"serial_number": true,
		"asset_number":  true,
		"location":      true,
		"description":   true,
	}

	var setClauses []string
	var args []interface{}
	for key, value := range updates {
		if !allowed[key] {
			continue
		}
		setClauses = append(setClauses, key+" = ?")
		args = append(args, value)
	}

	if len(setClauses) == 0 {
		return nil
	}

	args = append(args, name)
	query := "UPDATE local_printers SET " + joinStrings(setClauses, ", ") + " WHERE name = ?"
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteLocalPrinter removes a local printer
func (s *SQLiteStore) DeleteLocalPrinter(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM local_printers WHERE name = ?", name)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// AddLocalPrintJob records a completed print job
func (s *SQLiteStore) AddLocalPrintJob(ctx context.Context, job *LocalPrintJob) error {
	if job == nil || job.PrinterName == "" {
		return fmt.Errorf("printer name is required")
	}

	query := `
		INSERT INTO local_print_jobs (
			printer_name, job_id, document_name, user_name, machine_name,
			total_pages, pages_printed, is_color, size_bytes, submitted_at,
			completed_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var completedAt interface{}
	if !job.CompletedAt.IsZero() {
		completedAt = job.CompletedAt
	}

	result, err := s.db.ExecContext(ctx, query,
		job.PrinterName, job.JobID, job.DocumentName, job.UserName, job.MachineName,
		job.TotalPages, job.PagesPrinted, job.IsColor, job.SizeBytes, job.SubmittedAt,
		completedAt, job.Status,
	)
	if err != nil {
		return err
	}

	id, _ := result.LastInsertId()
	job.ID = id
	return nil
}

// GetLocalPrintJobs returns print jobs for a printer, newest first
func (s *SQLiteStore) GetLocalPrintJobs(ctx context.Context, printerName string, limit int) ([]*LocalPrintJob, error) {
	query := `
		SELECT id, printer_name, job_id, document_name, user_name, machine_name,
			total_pages, pages_printed, is_color, size_bytes, submitted_at,
			completed_at, status
		FROM local_print_jobs
		WHERE printer_name = ?
		ORDER BY submitted_at DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, printerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*LocalPrintJob
	for rows.Next() {
		job := &LocalPrintJob{}
		var docName, userName, machineName sql.NullString
		var completedAt sql.NullTime

		err := rows.Scan(
			&job.ID, &job.PrinterName, &job.JobID, &docName, &userName, &machineName,
			&job.TotalPages, &job.PagesPrinted, &job.IsColor, &job.SizeBytes,
			&job.SubmittedAt, &completedAt, &job.Status,
		)
		if err != nil {
			return nil, err
		}

		job.DocumentName = docName.String
		job.UserName = userName.String
		job.MachineName = machineName.String
		if completedAt.Valid {
			job.CompletedAt = completedAt.Time
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// DeleteOldLocalPrintJobs removes jobs older than the given timestamp
func (s *SQLiteStore) DeleteOldLocalPrintJobs(ctx context.Context, olderThan time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM local_print_jobs WHERE submitted_at < ?",
		olderThan,
	)
	if err != nil {
		return 0, err
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// GetLocalPrinterStats returns statistics for a printer
func (s *SQLiteStore) GetLocalPrinterStats(ctx context.Context, name string, since time.Time) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get job count and total pages
	var jobCount int
	var totalPages int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(total_pages), 0)
		FROM local_print_jobs
		WHERE printer_name = ? AND submitted_at >= ?
	`, name, since).Scan(&jobCount, &totalPages)
	if err != nil {
		return nil, err
	}

	stats["job_count"] = jobCount
	stats["pages_printed"] = totalPages
	stats["period_start"] = since

	// Get printer info
	printer, err := s.GetLocalPrinter(ctx, name)
	if err == nil {
		stats["total_pages_all_time"] = printer.TotalPageCount()
		stats["baseline_pages"] = printer.BaselinePages
	}

	return stats, nil
}

// helper to join strings (avoid importing strings just for this)
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
