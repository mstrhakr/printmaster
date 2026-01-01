package storage

import (
	"context"
	"database/sql"
	"fmt"
	commonstorage "printmaster/common/storage"
	"time"
)

// PageCountAudit is a type alias for the common storage PageCountAudit
type PageCountAudit = commonstorage.PageCountAudit

// Audit change types
const (
	AuditChangeTypeManual     = "manual"     // User manually set page count
	AuditChangeTypePolled     = "polled"     // Page count updated from device polling
	AuditChangeTypeInitial    = "initial"    // Initial baseline page count set
	AuditChangeTypeAdjustment = "adjustment" // Administrative adjustment
)

// AddPageCountAudit records a page count change in the audit trail
func (s *SQLiteStore) AddPageCountAudit(ctx context.Context, audit *PageCountAudit) error {
	if audit.Serial == "" {
		return ErrInvalidSerial
	}

	if audit.Timestamp.IsZero() {
		audit.Timestamp = time.Now()
	}
	if audit.SourceMetric == "" {
		audit.SourceMetric = "page_count"
	}

	query := `
		INSERT INTO page_count_audit (
			serial, old_count, new_count, change_type, changed_by, reason, timestamp, source_metric
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		audit.Serial, audit.OldCount, audit.NewCount, audit.ChangeType,
		audit.ChangedBy, audit.Reason, audit.Timestamp, audit.SourceMetric,
	)
	if err != nil {
		return fmt.Errorf("failed to add page count audit: %w", err)
	}

	if storageLogger != nil {
		storageLogger.Debug("Page count audit recorded",
			"serial", audit.Serial,
			"old", audit.OldCount,
			"new", audit.NewCount,
			"type", audit.ChangeType,
			"by", audit.ChangedBy,
		)
	}

	return nil
}

// GetPageCountAudit retrieves page count audit history for a device
func (s *SQLiteStore) GetPageCountAudit(ctx context.Context, serial string, limit int) ([]*PageCountAudit, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}

	query := `
		SELECT id, serial, old_count, new_count, change_type, changed_by, reason, timestamp, source_metric
		FROM page_count_audit
		WHERE serial = ?
		ORDER BY timestamp DESC
	`
	args := []interface{}{serial}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query page count audit: %w", err)
	}
	defer rows.Close()

	var audits []*PageCountAudit
	for rows.Next() {
		audit := &PageCountAudit{}
		var changedBy, reason, sourceMetric sql.NullString

		err := rows.Scan(
			&audit.ID, &audit.Serial, &audit.OldCount, &audit.NewCount,
			&audit.ChangeType, &changedBy, &reason, &audit.Timestamp, &sourceMetric,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit row: %w", err)
		}

		if changedBy.Valid {
			audit.ChangedBy = changedBy.String
		}
		if reason.Valid {
			audit.Reason = reason.String
		}
		if sourceMetric.Valid {
			audit.SourceMetric = sourceMetric.String
		}

		audits = append(audits, audit)
	}

	return audits, rows.Err()
}

// GetPageCountAuditSince retrieves audit entries after a specific time
func (s *SQLiteStore) GetPageCountAuditSince(ctx context.Context, serial string, since time.Time) ([]*PageCountAudit, error) {
	if serial == "" {
		return nil, ErrInvalidSerial
	}

	query := `
		SELECT id, serial, old_count, new_count, change_type, changed_by, reason, timestamp, source_metric
		FROM page_count_audit
		WHERE serial = ? AND timestamp > ?
		ORDER BY timestamp ASC
	`

	rows, err := s.db.QueryContext(ctx, query, serial, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query page count audit: %w", err)
	}
	defer rows.Close()

	var audits []*PageCountAudit
	for rows.Next() {
		audit := &PageCountAudit{}
		var changedBy, reason, sourceMetric sql.NullString

		err := rows.Scan(
			&audit.ID, &audit.Serial, &audit.OldCount, &audit.NewCount,
			&audit.ChangeType, &changedBy, &reason, &audit.Timestamp, &sourceMetric,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit row: %w", err)
		}

		if changedBy.Valid {
			audit.ChangedBy = changedBy.String
		}
		if reason.Valid {
			audit.Reason = reason.String
		}
		if sourceMetric.Valid {
			audit.SourceMetric = sourceMetric.String
		}

		audits = append(audits, audit)
	}

	return audits, rows.Err()
}

// DeleteOldPageCountAudit removes audit entries older than the specified time
func (s *SQLiteStore) DeleteOldPageCountAudit(ctx context.Context, olderThan time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM page_count_audit WHERE timestamp < ?", olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old audit entries: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

// SetInitialPageCount sets the initial page count for a device and records it in the audit trail
func (s *SQLiteStore) SetInitialPageCount(ctx context.Context, serial string, initialCount int, changedBy string, reason string) error {
	if serial == "" {
		return ErrInvalidSerial
	}

	// Get current device to record old value
	device, err := s.Get(ctx, serial)
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	oldCount := device.InitialPageCount

	// Update device
	_, err = s.db.ExecContext(ctx,
		"UPDATE devices SET initial_page_count = ?, last_seen = ? WHERE serial = ?",
		initialCount, time.Now(), serial,
	)
	if err != nil {
		return fmt.Errorf("failed to update initial page count: %w", err)
	}

	// Record audit entry
	audit := &PageCountAudit{
		Serial:       serial,
		OldCount:     oldCount,
		NewCount:     initialCount,
		ChangeType:   AuditChangeTypeInitial,
		ChangedBy:    changedBy,
		Reason:       reason,
		Timestamp:    time.Now(),
		SourceMetric: "initial_page_count",
	}

	return s.AddPageCountAudit(ctx, audit)
}

// GetPageCountUsage calculates the page count usage since the initial baseline
func (s *SQLiteStore) GetPageCountUsage(ctx context.Context, serial string) (usage int, initial int, current int, err error) {
	if serial == "" {
		return 0, 0, 0, ErrInvalidSerial
	}

	// Get device's initial page count
	device, err := s.Get(ctx, serial)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get device: %w", err)
	}

	initial = device.InitialPageCount

	// Get latest metrics for current page count
	metrics, err := s.GetLatestMetrics(ctx, serial)
	if err != nil {
		if err == ErrNotFound {
			// No metrics yet, usage is 0
			return 0, initial, 0, nil
		}
		return 0, 0, 0, fmt.Errorf("failed to get latest metrics: %w", err)
	}

	current = metrics.PageCount
	usage = current - initial
	if usage < 0 {
		usage = 0 // Page count reset or device replaced
	}

	return usage, initial, current, nil
}
