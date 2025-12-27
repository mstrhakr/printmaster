package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ============================================================
// Alert Storage Methods (BaseStore)
// ============================================================

// isInQuietHours checks if the current time falls within quiet hours.
func isInQuietHours(qh QuietHours) bool {
	if !qh.Enabled {
		return false
	}

	now := time.Now()
	if qh.Timezone != "" && qh.Timezone != "local" {
		if loc, err := time.LoadLocation(qh.Timezone); err == nil {
			now = now.In(loc)
		}
	}

	// Parse start and end times (HH:MM format)
	startParts := strings.Split(qh.StartTime, ":")
	endParts := strings.Split(qh.EndTime, ":")
	if len(startParts) != 2 || len(endParts) != 2 {
		return false
	}

	startHour, _ := strconv.Atoi(startParts[0])
	startMin, _ := strconv.Atoi(startParts[1])
	endHour, _ := strconv.Atoi(endParts[0])
	endMin, _ := strconv.Atoi(endParts[1])

	currentMins := now.Hour()*60 + now.Minute()
	startMins := startHour*60 + startMin
	endMins := endHour*60 + endMin

	// Handle overnight quiet hours (e.g., 22:00 - 07:00)
	if startMins > endMins {
		return currentMins >= startMins || currentMins < endMins
	}

	return currentMins >= startMins && currentMins < endMins
}

// CreateAlert inserts a new alert.
func (s *BaseStore) CreateAlert(ctx context.Context, alert *Alert) (int64, error) {
	query := `
		INSERT INTO alerts (
			rule_id, type, severity, scope, status,
			tenant_id, site_id, agent_id, device_serial,
			title, message, details,
			triggered_at, expires_at,
			escalation_level, state_change_count, is_flapping,
			parent_alert_id, child_count,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	now := time.Now().UTC()
	id, err := s.insertReturningID(ctx, query,
		nullInt64(alert.RuleID),
		alert.Type,
		alert.Severity,
		alert.Scope,
		alert.Status,
		nullString(alert.TenantID),
		nullString(alert.SiteID),
		nullString(alert.AgentID),
		nullString(alert.DeviceSerial),
		alert.Title,
		alert.Message,
		nullString(alert.Details),
		alert.TriggeredAt,
		nullTimePtr(alert.ExpiresAt),
		alert.EscalationLevel,
		alert.StateChangeCount,
		alert.IsFlapping,
		nullInt64Ptr(alert.ParentAlertID),
		alert.ChildCount,
		now,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert alert: %w", err)
	}

	alert.ID = id
	alert.CreatedAt = now
	alert.UpdatedAt = now

	return id, nil
}

// GetAlert retrieves an alert by ID.
func (s *BaseStore) GetAlert(ctx context.Context, id int64) (*Alert, error) {
	query := `
		SELECT 
			id, rule_id, type, severity, scope, status,
			tenant_id, site_id, agent_id, device_serial,
			title, message, details,
			triggered_at, acknowledged_at, acknowledged_by, resolved_at,
			suppressed_until, expires_at,
			escalation_level, last_escalated_at,
			state_change_count, is_flapping,
			parent_alert_id, child_count,
			notifications_sent, last_notified_at,
			created_at, updated_at
		FROM alerts WHERE id = ?
	`

	var a Alert
	var ruleID, parentAlertID sql.NullInt64
	var tenantID, siteID, agentID, deviceSerial, details, acknowledgedBy sql.NullString
	var acknowledgedAt, resolvedAt, suppressedUntil, expiresAt, lastEscalatedAt, lastNotifiedAt sql.NullTime

	err := s.queryRowContext(ctx, query, id).Scan(
		&a.ID, &ruleID, &a.Type, &a.Severity, &a.Scope, &a.Status,
		&tenantID, &siteID, &agentID, &deviceSerial,
		&a.Title, &a.Message, &details,
		&a.TriggeredAt, &acknowledgedAt, &acknowledgedBy, &resolvedAt,
		&suppressedUntil, &expiresAt,
		&a.EscalationLevel, &lastEscalatedAt,
		&a.StateChangeCount, &a.IsFlapping,
		&parentAlertID, &a.ChildCount,
		&a.NotificationsSent, &lastNotifiedAt,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alert: %w", err)
	}

	if ruleID.Valid {
		a.RuleID = ruleID.Int64
	}
	if tenantID.Valid {
		a.TenantID = tenantID.String
	}
	if siteID.Valid {
		a.SiteID = siteID.String
	}
	if agentID.Valid {
		a.AgentID = agentID.String
	}
	if deviceSerial.Valid {
		a.DeviceSerial = deviceSerial.String
	}
	if details.Valid {
		a.Details = details.String
	}
	if acknowledgedBy.Valid {
		a.AcknowledgedBy = acknowledgedBy.String
	}
	if acknowledgedAt.Valid {
		a.AcknowledgedAt = &acknowledgedAt.Time
	}
	if resolvedAt.Valid {
		a.ResolvedAt = &resolvedAt.Time
	}
	if suppressedUntil.Valid {
		a.SuppressedUntil = &suppressedUntil.Time
	}
	if expiresAt.Valid {
		a.ExpiresAt = &expiresAt.Time
	}
	if lastEscalatedAt.Valid {
		a.LastEscalatedAt = &lastEscalatedAt.Time
	}
	if lastNotifiedAt.Valid {
		a.LastNotifiedAt = &lastNotifiedAt.Time
	}
	if parentAlertID.Valid {
		a.ParentAlertID = &parentAlertID.Int64
	}

	return &a, nil
}

// ListActiveAlerts returns all active alerts, optionally filtered.
func (s *BaseStore) ListActiveAlerts(ctx context.Context, filters AlertFilters) ([]Alert, error) {
	query := `
		SELECT 
			id, rule_id, type, severity, scope, status,
			tenant_id, site_id, agent_id, device_serial,
			title, message, details,
			triggered_at, acknowledged_at, acknowledged_by, resolved_at,
			suppressed_until, expires_at,
			escalation_level, last_escalated_at,
			state_change_count, is_flapping,
			parent_alert_id, child_count,
			notifications_sent, last_notified_at,
			created_at, updated_at
		FROM alerts
		WHERE status IN ('active', 'acknowledged')
	`
	args := []interface{}{}

	if filters.Severity != "" {
		query += " AND severity = ?"
		args = append(args, filters.Severity)
	}
	if filters.Scope != "" {
		query += " AND scope = ?"
		args = append(args, filters.Scope)
	}
	if filters.Type != "" {
		query += " AND type = ?"
		args = append(args, filters.Type)
	}
	if filters.TenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, filters.TenantID)
	}

	query += " ORDER BY severity DESC, triggered_at DESC"

	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)
		if filters.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filters.Offset)
		}
	}

	rows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active alerts: %w", err)
	}
	defer rows.Close()

	return s.scanAlerts(rows)
}

// ListAlerts returns alerts matching the filter (all statuses).
func (s *BaseStore) ListAlerts(ctx context.Context, filter AlertFilters) ([]*Alert, error) {
	query := `
		SELECT 
			id, rule_id, type, severity, scope, status,
			tenant_id, site_id, agent_id, device_serial,
			title, message, details,
			triggered_at, acknowledged_at, acknowledged_by, resolved_at,
			suppressed_until, expires_at,
			escalation_level, last_escalated_at,
			state_change_count, is_flapping,
			parent_alert_id, child_count,
			notifications_sent, last_notified_at,
			created_at, updated_at
		FROM alerts
		WHERE 1=1
	`
	args := []interface{}{}

	if filter.Severity != "" {
		query += " AND severity = ?"
		args = append(args, filter.Severity)
	}
	if filter.Scope != "" {
		query += " AND scope = ?"
		args = append(args, filter.Scope)
	}
	if filter.Type != "" {
		query += " AND type = ?"
		args = append(args, filter.Type)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.TenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, filter.TenantID)
	}
	if filter.StartTime != nil {
		query += " AND triggered_at >= ?"
		args = append(args, *filter.StartTime)
	}
	if filter.EndTime != nil {
		query += " AND triggered_at <= ?"
		args = append(args, *filter.EndTime)
	}

	query += " ORDER BY triggered_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	alerts, err := s.scanAlerts(rows)
	if err != nil {
		return nil, err
	}

	// Convert to pointer slice
	result := make([]*Alert, len(alerts))
	for i := range alerts {
		result[i] = &alerts[i]
	}
	return result, nil
}

// UpdateAlertStatus changes an alert's status.
func (s *BaseStore) UpdateAlertStatus(ctx context.Context, id int64, status AlertStatus) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alerts 
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)
	return err
}

// AcknowledgeAlert marks an alert as acknowledged.
func (s *BaseStore) AcknowledgeAlert(ctx context.Context, id int64, username string) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alerts 
		SET status = 'acknowledged', acknowledged_at = ?, acknowledged_by = ?, updated_at = ?
		WHERE id = ? AND status = 'active'
	`, now, username, now, id)
	return err
}

// ResolveAlert marks an alert as resolved.
func (s *BaseStore) ResolveAlert(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alerts 
		SET status = 'resolved', resolved_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('active', 'acknowledged')
	`, now, now, id)
	return err
}

// SuppressAlert suppresses an alert until a given time.
func (s *BaseStore) SuppressAlert(ctx context.Context, id int64, until time.Time) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alerts 
		SET status = 'suppressed', suppressed_until = ?, updated_at = ?
		WHERE id = ?
	`, until, now, id)
	return err
}

// UpdateAlertNotificationStatus updates the notification tracking fields on an alert.
func (s *BaseStore) UpdateAlertNotificationStatus(ctx context.Context, id int64, sent int, lastNotified time.Time) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alerts 
		SET notifications_sent = ?, last_notified_at = ?, updated_at = ?
		WHERE id = ?
	`, sent, lastNotified, now, id)
	return err
}

// ListAlertHistory returns resolved/expired alerts within a time range.
func (s *BaseStore) ListAlertHistory(ctx context.Context, filters AlertFilters) ([]Alert, error) {
	query := `
		SELECT 
			id, rule_id, type, severity, scope, status,
			tenant_id, site_id, agent_id, device_serial,
			title, message, details,
			triggered_at, acknowledged_at, acknowledged_by, resolved_at,
			suppressed_until, expires_at,
			escalation_level, last_escalated_at,
			state_change_count, is_flapping,
			parent_alert_id, child_count,
			notifications_sent, last_notified_at,
			created_at, updated_at
		FROM alerts
		WHERE status NOT IN ('active')
	`
	args := []interface{}{}

	if filters.StartTime != nil {
		query += " AND triggered_at >= ?"
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		query += " AND triggered_at <= ?"
		args = append(args, *filters.EndTime)
	}
	if filters.Status != "" {
		query += " AND status = ?"
		args = append(args, filters.Status)
	}
	if filters.Scope != "" {
		query += " AND scope = ?"
		args = append(args, filters.Scope)
	}
	if filters.TenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, filters.TenantID)
	}

	query += " ORDER BY triggered_at DESC"

	if filters.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filters.Limit)
	}

	rows, err := s.queryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert history: %w", err)
	}
	defer rows.Close()

	return s.scanAlerts(rows)
}

// scanAlerts scans rows into a slice of Alert structs.
func (s *BaseStore) scanAlerts(rows *sql.Rows) ([]Alert, error) {
	var alerts []Alert
	for rows.Next() {
		var a Alert
		var ruleID, parentAlertID sql.NullInt64
		var tenantID, siteID, agentID, deviceSerial, details, acknowledgedBy sql.NullString
		var acknowledgedAt, resolvedAt, suppressedUntil, expiresAt, lastEscalatedAt, lastNotifiedAt sql.NullTime

		err := rows.Scan(
			&a.ID, &ruleID, &a.Type, &a.Severity, &a.Scope, &a.Status,
			&tenantID, &siteID, &agentID, &deviceSerial,
			&a.Title, &a.Message, &details,
			&a.TriggeredAt, &acknowledgedAt, &acknowledgedBy, &resolvedAt,
			&suppressedUntil, &expiresAt,
			&a.EscalationLevel, &lastEscalatedAt,
			&a.StateChangeCount, &a.IsFlapping,
			&parentAlertID, &a.ChildCount,
			&a.NotificationsSent, &lastNotifiedAt,
			&a.CreatedAt, &a.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}

		if ruleID.Valid {
			a.RuleID = ruleID.Int64
		}
		if tenantID.Valid {
			a.TenantID = tenantID.String
		}
		if siteID.Valid {
			a.SiteID = siteID.String
		}
		if agentID.Valid {
			a.AgentID = agentID.String
		}
		if deviceSerial.Valid {
			a.DeviceSerial = deviceSerial.String
		}
		if details.Valid {
			a.Details = details.String
		}
		if acknowledgedBy.Valid {
			a.AcknowledgedBy = acknowledgedBy.String
		}
		if acknowledgedAt.Valid {
			a.AcknowledgedAt = &acknowledgedAt.Time
		}
		if resolvedAt.Valid {
			a.ResolvedAt = &resolvedAt.Time
		}
		if suppressedUntil.Valid {
			a.SuppressedUntil = &suppressedUntil.Time
		}
		if expiresAt.Valid {
			a.ExpiresAt = &expiresAt.Time
		}
		if lastEscalatedAt.Valid {
			a.LastEscalatedAt = &lastEscalatedAt.Time
		}
		if lastNotifiedAt.Valid {
			a.LastNotifiedAt = &lastNotifiedAt.Time
		}
		if parentAlertID.Valid {
			a.ParentAlertID = &parentAlertID.Int64
		}

		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// ============================================================
// Alert Rule Storage Methods (BaseStore)
// ============================================================

// CreateAlertRule creates a new alert rule.
func (s *BaseStore) CreateAlertRule(ctx context.Context, rule *AlertRule) (int64, error) {
	channelsJSON, _ := json.Marshal(rule.ChannelIDs)
	tenantIDsJSON, _ := json.Marshal(rule.TenantIDs)
	siteIDsJSON, _ := json.Marshal(rule.SiteIDs)
	agentIDsJSON, _ := json.Marshal(rule.AgentIDs)

	now := time.Now().UTC()
	id, err := s.insertReturningID(ctx, `
		INSERT INTO alert_rules (
			name, description, enabled,
			type, severity, scope,
			tenant_ids, site_ids, agent_ids,
			condition_json, threshold, threshold_unit, duration_minutes,
			channel_ids, escalation_policy_id, cooldown_minutes,
			created_at, updated_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.Name, rule.Description, rule.Enabled,
		rule.Type, rule.Severity, rule.Scope,
		string(tenantIDsJSON), string(siteIDsJSON), string(agentIDsJSON),
		rule.ConditionJSON, rule.Threshold, rule.ThresholdUnit, rule.DurationMinutes,
		string(channelsJSON), nullInt64Ptr(rule.EscalationPolicyID), rule.CooldownMinutes,
		now, now, rule.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert alert rule: %w", err)
	}

	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now

	return id, nil
}

// ListAlertRules returns all alert rules.
func (s *BaseStore) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.queryContext(ctx, `
		SELECT 
			id, name, description, enabled,
			type, severity, scope,
			tenant_ids, site_ids, agent_ids,
			condition_json, threshold, threshold_unit, duration_minutes,
			channel_ids, escalation_policy_id, cooldown_minutes,
			created_at, updated_at, created_by
		FROM alert_rules
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	var rules []AlertRule
	for rows.Next() {
		var r AlertRule
		var tenantIDsJSON, siteIDsJSON, agentIDsJSON, channelsJSON, conditionJSON, thresholdUnit, createdBy sql.NullString
		var escalationPolicyID sql.NullInt64
		var description sql.NullString

		err := rows.Scan(
			&r.ID, &r.Name, &description, &r.Enabled,
			&r.Type, &r.Severity, &r.Scope,
			&tenantIDsJSON, &siteIDsJSON, &agentIDsJSON,
			&conditionJSON, &r.Threshold, &thresholdUnit, &r.DurationMinutes,
			&channelsJSON, &escalationPolicyID, &r.CooldownMinutes,
			&r.CreatedAt, &r.UpdatedAt, &createdBy,
		)
		if err != nil {
			return nil, fmt.Errorf("scan alert rule: %w", err)
		}

		if description.Valid {
			r.Description = description.String
		}
		if conditionJSON.Valid {
			r.ConditionJSON = conditionJSON.String
		}
		if thresholdUnit.Valid {
			r.ThresholdUnit = thresholdUnit.String
		}
		if createdBy.Valid {
			r.CreatedBy = createdBy.String
		}
		if escalationPolicyID.Valid {
			r.EscalationPolicyID = &escalationPolicyID.Int64
		}

		// Parse JSON arrays
		if tenantIDsJSON.Valid && tenantIDsJSON.String != "" {
			json.Unmarshal([]byte(tenantIDsJSON.String), &r.TenantIDs)
		}
		if siteIDsJSON.Valid && siteIDsJSON.String != "" {
			json.Unmarshal([]byte(siteIDsJSON.String), &r.SiteIDs)
		}
		if agentIDsJSON.Valid && agentIDsJSON.String != "" {
			json.Unmarshal([]byte(agentIDsJSON.String), &r.AgentIDs)
		}
		if channelsJSON.Valid && channelsJSON.String != "" {
			json.Unmarshal([]byte(channelsJSON.String), &r.ChannelIDs)
		}

		rules = append(rules, r)
	}

	return rules, rows.Err()
}

// GetAlertRule retrieves a single alert rule by ID.
func (s *BaseStore) GetAlertRule(ctx context.Context, id int64) (*AlertRule, error) {
	var r AlertRule
	var tenantIDsJSON, siteIDsJSON, agentIDsJSON, channelsJSON, conditionJSON, thresholdUnit, createdBy sql.NullString
	var escalationPolicyID sql.NullInt64
	var description sql.NullString

	err := s.queryRowContext(ctx, `
		SELECT 
			id, name, description, enabled,
			type, severity, scope,
			tenant_ids, site_ids, agent_ids,
			condition_json, threshold, threshold_unit, duration_minutes,
			channel_ids, escalation_policy_id, cooldown_minutes,
			created_at, updated_at, created_by
		FROM alert_rules
		WHERE id = ?
	`, id).Scan(
		&r.ID, &r.Name, &description, &r.Enabled,
		&r.Type, &r.Severity, &r.Scope,
		&tenantIDsJSON, &siteIDsJSON, &agentIDsJSON,
		&conditionJSON, &r.Threshold, &thresholdUnit, &r.DurationMinutes,
		&channelsJSON, &escalationPolicyID, &r.CooldownMinutes,
		&r.CreatedAt, &r.UpdatedAt, &createdBy,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alert rule: %w", err)
	}

	if description.Valid {
		r.Description = description.String
	}
	if conditionJSON.Valid {
		r.ConditionJSON = conditionJSON.String
	}
	if thresholdUnit.Valid {
		r.ThresholdUnit = thresholdUnit.String
	}
	if createdBy.Valid {
		r.CreatedBy = createdBy.String
	}
	if escalationPolicyID.Valid {
		r.EscalationPolicyID = &escalationPolicyID.Int64
	}

	// Parse JSON arrays
	if tenantIDsJSON.Valid && tenantIDsJSON.String != "" {
		json.Unmarshal([]byte(tenantIDsJSON.String), &r.TenantIDs)
	}
	if siteIDsJSON.Valid && siteIDsJSON.String != "" {
		json.Unmarshal([]byte(siteIDsJSON.String), &r.SiteIDs)
	}
	if agentIDsJSON.Valid && agentIDsJSON.String != "" {
		json.Unmarshal([]byte(agentIDsJSON.String), &r.AgentIDs)
	}
	if channelsJSON.Valid && channelsJSON.String != "" {
		json.Unmarshal([]byte(channelsJSON.String), &r.ChannelIDs)
	}

	return &r, nil
}

// UpdateAlertRule updates an existing alert rule.
func (s *BaseStore) UpdateAlertRule(ctx context.Context, rule *AlertRule) error {
	channelsJSON, _ := json.Marshal(rule.ChannelIDs)
	tenantIDsJSON, _ := json.Marshal(rule.TenantIDs)
	siteIDsJSON, _ := json.Marshal(rule.SiteIDs)
	agentIDsJSON, _ := json.Marshal(rule.AgentIDs)

	now := time.Now().UTC()
	_, err := s.execContext(ctx, `
		UPDATE alert_rules SET
			name = ?, description = ?, enabled = ?,
			type = ?, severity = ?, scope = ?,
			tenant_ids = ?, site_ids = ?, agent_ids = ?,
			condition_json = ?, threshold = ?, threshold_unit = ?, duration_minutes = ?,
			channel_ids = ?, escalation_policy_id = ?, cooldown_minutes = ?,
			updated_at = ?
		WHERE id = ?
	`,
		rule.Name, rule.Description, rule.Enabled,
		rule.Type, rule.Severity, rule.Scope,
		string(tenantIDsJSON), string(siteIDsJSON), string(agentIDsJSON),
		rule.ConditionJSON, rule.Threshold, rule.ThresholdUnit, rule.DurationMinutes,
		string(channelsJSON), nullInt64Ptr(rule.EscalationPolicyID), rule.CooldownMinutes,
		now, rule.ID,
	)
	if err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	rule.UpdatedAt = now
	return nil
}

// DeleteAlertRule removes an alert rule by ID.
func (s *BaseStore) DeleteAlertRule(ctx context.Context, id int64) error {
	_, err := s.execContext(ctx, "DELETE FROM alert_rules WHERE id = ?", id)
	return err
}

// ============================================================
// Notification Channel Storage Methods (BaseStore)
// ============================================================

// CreateNotificationChannel creates a new notification channel.
func (s *BaseStore) CreateNotificationChannel(ctx context.Context, ch *NotificationChannel) (int64, error) {
	tenantIDsJSON, _ := json.Marshal(ch.TenantIDs)
	now := time.Now().UTC()

	id, err := s.insertReturningID(ctx, `
		INSERT INTO notification_channels (
			name, type, enabled, config_json,
			min_severity, tenant_ids,
			rate_limit_per_hour, use_quiet_hours,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		ch.Name, ch.Type, ch.Enabled, ch.ConfigJSON,
		ch.MinSeverity, string(tenantIDsJSON),
		ch.RateLimitPerHour, ch.UseQuietHours,
		now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert notification channel: %w", err)
	}

	ch.ID = id
	ch.CreatedAt = now
	ch.UpdatedAt = now

	return id, nil
}

// GetNotificationChannel returns a notification channel by ID.
func (s *BaseStore) GetNotificationChannel(ctx context.Context, id int64) (*NotificationChannel, error) {
	var ch NotificationChannel
	var configJSON, tenantIDsJSON sql.NullString
	var lastSentAt sql.NullTime

	err := s.queryRowContext(ctx, `
		SELECT 
			id, name, type, enabled, config_json,
			min_severity, tenant_ids,
			rate_limit_per_hour, last_sent_at, sent_this_hour,
			use_quiet_hours, created_at, updated_at
		FROM notification_channels
		WHERE id = ?
	`, id).Scan(
		&ch.ID, &ch.Name, &ch.Type, &ch.Enabled, &configJSON,
		&ch.MinSeverity, &tenantIDsJSON,
		&ch.RateLimitPerHour, &lastSentAt, &ch.SentThisHour,
		&ch.UseQuietHours, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("notification channel not found: %d", id)
		}
		return nil, fmt.Errorf("get notification channel: %w", err)
	}

	if configJSON.Valid {
		ch.ConfigJSON = configJSON.String
	}
	if lastSentAt.Valid {
		ch.LastSentAt = &lastSentAt.Time
	}
	if tenantIDsJSON.Valid && tenantIDsJSON.String != "" {
		json.Unmarshal([]byte(tenantIDsJSON.String), &ch.TenantIDs)
	}

	return &ch, nil
}

// ListNotificationChannels returns all notification channels.
func (s *BaseStore) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.queryContext(ctx, `
		SELECT 
			id, name, type, enabled, config_json,
			min_severity, tenant_ids,
			rate_limit_per_hour, last_sent_at, sent_this_hour,
			use_quiet_hours, created_at, updated_at
		FROM notification_channels
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list notification channels: %w", err)
	}
	defer rows.Close()

	var channels []NotificationChannel
	for rows.Next() {
		var ch NotificationChannel
		var configJSON, tenantIDsJSON sql.NullString
		var lastSentAt sql.NullTime

		err := rows.Scan(
			&ch.ID, &ch.Name, &ch.Type, &ch.Enabled, &configJSON,
			&ch.MinSeverity, &tenantIDsJSON,
			&ch.RateLimitPerHour, &lastSentAt, &ch.SentThisHour,
			&ch.UseQuietHours, &ch.CreatedAt, &ch.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan notification channel: %w", err)
		}

		if configJSON.Valid {
			ch.ConfigJSON = configJSON.String
		}
		if lastSentAt.Valid {
			ch.LastSentAt = &lastSentAt.Time
		}
		if tenantIDsJSON.Valid && tenantIDsJSON.String != "" {
			json.Unmarshal([]byte(tenantIDsJSON.String), &ch.TenantIDs)
		}

		channels = append(channels, ch)
	}

	return channels, rows.Err()
}

// DeleteNotificationChannel removes a notification channel by ID.
func (s *BaseStore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	_, err := s.execContext(ctx, "DELETE FROM notification_channels WHERE id = ?", id)
	return err
}

// UpdateNotificationChannel updates an existing notification channel.
func (s *BaseStore) UpdateNotificationChannel(ctx context.Context, ch *NotificationChannel) error {
	tenantIDsJSON, _ := json.Marshal(ch.TenantIDs)
	now := time.Now().UTC()

	_, err := s.execContext(ctx, `
		UPDATE notification_channels SET
			name = ?, type = ?, enabled = ?, config_json = ?,
			min_severity = ?, tenant_ids = ?,
			rate_limit_per_hour = ?, use_quiet_hours = ?,
			updated_at = ?
		WHERE id = ?
	`,
		ch.Name, ch.Type, ch.Enabled, ch.ConfigJSON,
		ch.MinSeverity, string(tenantIDsJSON),
		ch.RateLimitPerHour, ch.UseQuietHours,
		now, ch.ID,
	)
	if err != nil {
		return fmt.Errorf("update notification channel: %w", err)
	}
	ch.UpdatedAt = now
	return nil
}

// ============================================================
// Escalation Policy Storage Methods (BaseStore)
// ============================================================

// CreateEscalationPolicy creates a new escalation policy.
func (s *BaseStore) CreateEscalationPolicy(ctx context.Context, policy *EscalationPolicy) (int64, error) {
	now := time.Now().UTC()
	policy.MarshalSteps() // Convert Steps array to StepsJSON

	id, err := s.insertReturningID(ctx, `
		INSERT INTO escalation_policies (name, description, enabled, steps_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, policy.Name, policy.Description, policy.Enabled, policy.StepsJSON, now, now)
	if err != nil {
		return 0, fmt.Errorf("insert escalation policy: %w", err)
	}

	policy.ID = id
	policy.CreatedAt = now
	policy.UpdatedAt = now

	return id, nil
}

// ListEscalationPolicies returns all escalation policies.
func (s *BaseStore) ListEscalationPolicies(ctx context.Context) ([]EscalationPolicy, error) {
	rows, err := s.queryContext(ctx, `
		SELECT id, name, description, enabled, steps_json, created_at, updated_at
		FROM escalation_policies
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("list escalation policies: %w", err)
	}
	defer rows.Close()

	var policies []EscalationPolicy
	for rows.Next() {
		var p EscalationPolicy
		var description sql.NullString

		err := rows.Scan(&p.ID, &p.Name, &description, &p.Enabled, &p.StepsJSON, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan escalation policy: %w", err)
		}

		if description.Valid {
			p.Description = description.String
		}
		p.UnmarshalSteps() // Convert StepsJSON to Steps array

		policies = append(policies, p)
	}

	return policies, rows.Err()
}

// DeleteEscalationPolicy removes an escalation policy by ID.
func (s *BaseStore) DeleteEscalationPolicy(ctx context.Context, id int64) error {
	_, err := s.execContext(ctx, "DELETE FROM escalation_policies WHERE id = ?", id)
	return err
}

// GetEscalationPolicy retrieves a single escalation policy by ID.
func (s *BaseStore) GetEscalationPolicy(ctx context.Context, id int64) (*EscalationPolicy, error) {
	var p EscalationPolicy
	var description sql.NullString

	err := s.queryRowContext(ctx, `
		SELECT id, name, description, enabled, steps_json, created_at, updated_at
		FROM escalation_policies
		WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &description, &p.Enabled, &p.StepsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("escalation policy %d not found", id)
		}
		return nil, fmt.Errorf("get escalation policy: %w", err)
	}

	if description.Valid {
		p.Description = description.String
	}
	p.UnmarshalSteps() // Convert StepsJSON to Steps array

	return &p, nil
}

// UpdateEscalationPolicy updates an existing escalation policy.
func (s *BaseStore) UpdateEscalationPolicy(ctx context.Context, policy *EscalationPolicy) error {
	now := time.Now().UTC()
	policy.MarshalSteps() // Convert Steps array to StepsJSON

	_, err := s.execContext(ctx, `
		UPDATE escalation_policies
		SET name = ?, description = ?, enabled = ?, steps_json = ?, updated_at = ?
		WHERE id = ?
	`, policy.Name, policy.Description, policy.Enabled, policy.StepsJSON, now, policy.ID)
	if err != nil {
		return fmt.Errorf("update escalation policy: %w", err)
	}
	policy.UpdatedAt = now
	return nil
}

// ============================================================
// Alert Maintenance Window Storage Methods (BaseStore)
// ============================================================

// CreateAlertMaintenanceWindow creates a new maintenance window.
func (s *BaseStore) CreateAlertMaintenanceWindow(ctx context.Context, mw *AlertMaintenanceWindow) (int64, error) {
	alertTypesJSON, _ := json.Marshal(mw.AlertTypes)
	recurDaysJSON, _ := json.Marshal(mw.RecurDays)
	now := time.Now().UTC()

	id, err := s.insertReturningID(ctx, `
		INSERT INTO maintenance_windows (
			name, description, scope,
			tenant_id, site_id, agent_id, device_serial,
			start_time, end_time, timezone,
			recurring, recur_pattern, recur_days,
			alert_types, allow_critical,
			created_at, updated_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		mw.Name, mw.Description, mw.Scope,
		nullString(mw.TenantID), nullString(mw.SiteID), nullString(mw.AgentID), nullString(mw.DeviceSerial),
		mw.StartTime, mw.EndTime, mw.Timezone,
		mw.Recurring, nullString(mw.RecurPattern), string(recurDaysJSON),
		string(alertTypesJSON), mw.AllowCritical,
		now, now, mw.CreatedBy,
	)
	if err != nil {
		return 0, fmt.Errorf("insert maintenance window: %w", err)
	}

	mw.ID = id
	mw.CreatedAt = now
	mw.UpdatedAt = now

	return id, nil
}

// ListAlertMaintenanceWindows returns all maintenance windows.
func (s *BaseStore) ListAlertMaintenanceWindows(ctx context.Context) ([]AlertMaintenanceWindow, error) {
	rows, err := s.queryContext(ctx, `
		SELECT 
			id, name, description, scope,
			tenant_id, site_id, agent_id, device_serial,
			start_time, end_time, timezone,
			recurring, recur_pattern, recur_days,
			alert_types, allow_critical,
			created_at, updated_at, created_by
		FROM maintenance_windows
		ORDER BY start_time DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	defer rows.Close()

	return s.scanMaintenanceWindows(rows)
}

// GetActiveAlertMaintenanceWindows returns currently active maintenance windows.
func (s *BaseStore) GetActiveAlertMaintenanceWindows(ctx context.Context) ([]AlertMaintenanceWindow, error) {
	now := time.Now().UTC()
	rows, err := s.queryContext(ctx, `
		SELECT 
			id, name, description, scope,
			tenant_id, site_id, agent_id, device_serial,
			start_time, end_time, timezone,
			recurring, recur_pattern, recur_days,
			alert_types, allow_critical,
			created_at, updated_at, created_by
		FROM maintenance_windows
		WHERE start_time <= ? AND end_time >= ?
		ORDER BY start_time
	`, now, now)
	if err != nil {
		return nil, fmt.Errorf("get active maintenance windows: %w", err)
	}
	defer rows.Close()

	return s.scanMaintenanceWindows(rows)
}

// scanMaintenanceWindows scans rows into maintenance window structs.
func (s *BaseStore) scanMaintenanceWindows(rows *sql.Rows) ([]AlertMaintenanceWindow, error) {
	var windows []AlertMaintenanceWindow
	for rows.Next() {
		var mw AlertMaintenanceWindow
		var description, tenantID, siteID, agentID, deviceSerial, recurPattern, createdBy sql.NullString
		var alertTypesJSON, recurDaysJSON sql.NullString

		err := rows.Scan(
			&mw.ID, &mw.Name, &description, &mw.Scope,
			&tenantID, &siteID, &agentID, &deviceSerial,
			&mw.StartTime, &mw.EndTime, &mw.Timezone,
			&mw.Recurring, &recurPattern, &recurDaysJSON,
			&alertTypesJSON, &mw.AllowCritical,
			&mw.CreatedAt, &mw.UpdatedAt, &createdBy,
		)
		if err != nil {
			return nil, fmt.Errorf("scan maintenance window: %w", err)
		}

		if description.Valid {
			mw.Description = description.String
		}
		if tenantID.Valid {
			mw.TenantID = tenantID.String
		}
		if siteID.Valid {
			mw.SiteID = siteID.String
		}
		if agentID.Valid {
			mw.AgentID = agentID.String
		}
		if deviceSerial.Valid {
			mw.DeviceSerial = deviceSerial.String
		}
		if recurPattern.Valid {
			mw.RecurPattern = recurPattern.String
		}
		if createdBy.Valid {
			mw.CreatedBy = createdBy.String
		}
		if alertTypesJSON.Valid && alertTypesJSON.String != "" {
			json.Unmarshal([]byte(alertTypesJSON.String), &mw.AlertTypes)
		}
		if recurDaysJSON.Valid && recurDaysJSON.String != "" {
			json.Unmarshal([]byte(recurDaysJSON.String), &mw.RecurDays)
		}

		windows = append(windows, mw)
	}

	return windows, rows.Err()
}

// DeleteAlertMaintenanceWindow removes a maintenance window by ID.
func (s *BaseStore) DeleteAlertMaintenanceWindow(ctx context.Context, id int64) error {
	_, err := s.execContext(ctx, "DELETE FROM maintenance_windows WHERE id = ?", id)
	return err
}

// GetAlertMaintenanceWindow retrieves a single maintenance window by ID.
func (s *BaseStore) GetAlertMaintenanceWindow(ctx context.Context, id int64) (*AlertMaintenanceWindow, error) {
	row := s.queryRowContext(ctx, `
		SELECT 
			id, name, description, scope,
			tenant_id, site_id, agent_id, device_serial,
			start_time, end_time, timezone,
			recurring, recur_pattern, recur_days,
			alert_types, allow_critical,
			created_at, updated_at, created_by
		FROM maintenance_windows
		WHERE id = ?
	`, id)

	var mw AlertMaintenanceWindow
	var description, tenantID, siteID, agentID, deviceSerial, recurPattern, createdBy sql.NullString
	var alertTypesJSON, recurDaysJSON sql.NullString

	err := row.Scan(
		&mw.ID, &mw.Name, &description, &mw.Scope,
		&tenantID, &siteID, &agentID, &deviceSerial,
		&mw.StartTime, &mw.EndTime, &mw.Timezone,
		&mw.Recurring, &recurPattern, &recurDaysJSON,
		&alertTypesJSON, &mw.AllowCritical,
		&mw.CreatedAt, &mw.UpdatedAt, &createdBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("maintenance window %d not found", id)
		}
		return nil, fmt.Errorf("get maintenance window: %w", err)
	}

	if description.Valid {
		mw.Description = description.String
	}
	if tenantID.Valid {
		mw.TenantID = tenantID.String
	}
	if siteID.Valid {
		mw.SiteID = siteID.String
	}
	if agentID.Valid {
		mw.AgentID = agentID.String
	}
	if deviceSerial.Valid {
		mw.DeviceSerial = deviceSerial.String
	}
	if recurPattern.Valid {
		mw.RecurPattern = recurPattern.String
	}
	if createdBy.Valid {
		mw.CreatedBy = createdBy.String
	}
	if alertTypesJSON.Valid && alertTypesJSON.String != "" {
		json.Unmarshal([]byte(alertTypesJSON.String), &mw.AlertTypes)
	}
	if recurDaysJSON.Valid && recurDaysJSON.String != "" {
		json.Unmarshal([]byte(recurDaysJSON.String), &mw.RecurDays)
	}

	return &mw, nil
}

// UpdateAlertMaintenanceWindow updates an existing maintenance window.
func (s *BaseStore) UpdateAlertMaintenanceWindow(ctx context.Context, mw *AlertMaintenanceWindow) error {
	alertTypesJSON, _ := json.Marshal(mw.AlertTypes)
	recurDaysJSON, _ := json.Marshal(mw.RecurDays)
	now := time.Now().UTC()

	_, err := s.execContext(ctx, `
		UPDATE maintenance_windows SET
			name = ?, description = ?, scope = ?,
			tenant_id = ?, site_id = ?, agent_id = ?, device_serial = ?,
			start_time = ?, end_time = ?, timezone = ?,
			recurring = ?, recur_pattern = ?, recur_days = ?,
			alert_types = ?, allow_critical = ?,
			updated_at = ?
		WHERE id = ?
	`,
		mw.Name, mw.Description, mw.Scope,
		nullString(mw.TenantID), nullString(mw.SiteID), nullString(mw.AgentID), nullString(mw.DeviceSerial),
		mw.StartTime, mw.EndTime, mw.Timezone,
		mw.Recurring, nullString(mw.RecurPattern), string(recurDaysJSON),
		string(alertTypesJSON), mw.AllowCritical,
		now, mw.ID,
	)
	if err != nil {
		return fmt.Errorf("update maintenance window: %w", err)
	}
	mw.UpdatedAt = now
	return nil
}

// ============================================================
// Alert Settings Storage Methods (BaseStore)
// ============================================================

// GetAlertSettings retrieves the global alert settings.
func (s *BaseStore) GetAlertSettings(ctx context.Context) (*AlertSettings, error) {
	var settingsJSON sql.NullString
	err := s.queryRowContext(ctx, `
		SELECT value FROM alert_settings WHERE key = 'alert_settings'
	`).Scan(&settingsJSON)

	if err == sql.ErrNoRows || !settingsJSON.Valid {
		// Return defaults
		return &AlertSettings{
			QuietHours: QuietHoursConfig{
				Enabled:       false,
				StartTime:     "22:00",
				EndTime:       "07:00",
				Timezone:      "local",
				AllowCritical: true,
			},
			FlappingEnabled:     true,
			FlappingThreshold:   5,
			FlappingWindowMins:  10,
			GroupingEnabled:     true,
			GroupingThreshold:   50,
			DependenciesEnabled: true,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alert settings: %w", err)
	}

	var settings AlertSettings
	if err := json.Unmarshal([]byte(settingsJSON.String), &settings); err != nil {
		return nil, fmt.Errorf("parse alert settings: %w", err)
	}

	return &settings, nil
}

// SaveAlertSettings persists the global alert settings.
func (s *BaseStore) SaveAlertSettings(ctx context.Context, settings *AlertSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal alert settings: %w", err)
	}

	// Use dialect-specific upsert
	upsertQuery := `
		INSERT INTO alert_settings (key, value, updated_at) VALUES ('alert_settings', ?, CURRENT_TIMESTAMP)
	` + s.dialect.UpsertConflict([]string{"key"}) + ` value = EXCLUDED.value, updated_at = CURRENT_TIMESTAMP`

	_, err = s.execContext(ctx, upsertQuery, string(data))
	return err
}

// GetAlertSummary computes dashboard statistics.
func (s *BaseStore) GetAlertSummary(ctx context.Context) (*AlertSummary, error) {
	summary := &AlertSummary{
		AlertsByType:  make(map[string]int),
		AlertsByScope: make(map[string]int),
	}

	// Count total active alerts
	err := s.queryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'active'`).Scan(&summary.ActiveCount)
	if err != nil {
		return nil, err
	}

	// Count active alerts by type
	rows, err := s.queryContext(ctx, `
		SELECT type, COUNT(*) FROM alerts 
		WHERE status = 'active' 
		GROUP BY type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var alertType string
		var count int
		if err := rows.Scan(&alertType, &count); err != nil {
			return nil, err
		}
		summary.AlertsByType[alertType] = count
	}

	// Count active alerts by severity
	severityRows, err := s.queryContext(ctx, `
		SELECT severity, COUNT(*) FROM alerts 
		WHERE status = 'active' 
		GROUP BY severity
	`)
	if err != nil {
		return nil, err
	}
	defer severityRows.Close()

	for severityRows.Next() {
		var severity string
		var count int
		if err := severityRows.Scan(&severity, &count); err != nil {
			return nil, err
		}
		switch severity {
		case "critical":
			summary.CriticalCount = count
		case "warning":
			summary.WarningCount = count
		case "info":
			summary.InfoCount = count
		}
	}

	// Count active alerts by scope
	scopeRows, err := s.queryContext(ctx, `
		SELECT scope, COUNT(*) FROM alerts 
		WHERE status = 'active' 
		GROUP BY scope
	`)
	if err != nil {
		return nil, err
	}
	defer scopeRows.Close()

	for scopeRows.Next() {
		var scope string
		var count int
		if err := scopeRows.Scan(&scope, &count); err != nil {
			return nil, err
		}
		summary.AlertsByScope[scope] = count
	}

	// Count resolved today
	today := time.Now().UTC().Truncate(24 * time.Hour)
	err = s.queryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'resolved' AND resolved_at >= ?`, today).Scan(&summary.ResolvedTodayCount)
	if err != nil {
		return nil, err
	}

	// Count active rules
	err = s.queryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules WHERE enabled = ?`, true).Scan(&summary.ActiveRules)
	if err != nil {
		return nil, err
	}

	// Count active channels
	err = s.queryRowContext(ctx, `SELECT COUNT(*) FROM notification_channels WHERE enabled = ?`, true).Scan(&summary.ActiveChannels)
	if err != nil {
		return nil, err
	}

	// Count suppressed
	err = s.queryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'suppressed'`).Scan(&summary.SuppressedCount)
	if err != nil {
		return nil, err
	}

	// Count acknowledged
	err = s.queryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'acknowledged'`).Scan(&summary.AcknowledgedCount)
	if err != nil {
		return nil, err
	}

	// Check if in quiet hours
	settings, err := s.GetAlertSettings(ctx)
	if err == nil && settings.QuietHours.Enabled {
		summary.IsQuietHours = isInQuietHours(settings.QuietHours)
	}

	// Check if any maintenance windows are active
	windows, err := s.GetActiveAlertMaintenanceWindows(ctx)
	if err == nil && len(windows) > 0 {
		summary.HasMaintenance = true
	}

	return summary, nil
}

// ============================================================
// Default Alert Rules Seed
// ============================================================

// DefaultAlertRules returns the default alert rules to seed on first initialization.
func DefaultAlertRules() []AlertRule {
	now := time.Now().UTC()
	return []AlertRule{
		{
			Name:            "Low Toner Warning",
			Description:     "Alerts when any toner level drops below 20%",
			Enabled:         true,
			Type:            AlertTypeSupplyLow,
			Severity:        AlertSeverityWarning,
			Scope:           AlertScopeDevice,
			Threshold:       20,
			ThresholdUnit:   "percent",
			DurationMinutes: 0,
			CooldownMinutes: 60,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
		{
			Name:            "Critical Toner Level",
			Description:     "Alerts when any toner level drops below 5%",
			Enabled:         true,
			Type:            AlertTypeSupplyCritical,
			Severity:        AlertSeverityCritical,
			Scope:           AlertScopeDevice,
			Threshold:       5,
			ThresholdUnit:   "percent",
			DurationMinutes: 0,
			CooldownMinutes: 30,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
		{
			Name:            "Printer Offline",
			Description:     "Alerts when a printer has been offline for 15 minutes",
			Enabled:         true,
			Type:            AlertTypeDeviceOffline,
			Severity:        AlertSeverityWarning,
			Scope:           AlertScopeDevice,
			DurationMinutes: 15,
			CooldownMinutes: 60,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
		{
			Name:            "Agent Disconnected",
			Description:     "Alerts when an agent has been disconnected for 10 minutes",
			Enabled:         true,
			Type:            AlertTypeAgentOffline,
			Severity:        AlertSeverityWarning,
			Scope:           AlertScopeAgent,
			DurationMinutes: 10,
			CooldownMinutes: 30,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
		{
			Name:            "Device Error",
			Description:     "Alerts when a device reports an error status (paper jam, fault, etc.)",
			Enabled:         true,
			Type:            AlertTypeDeviceError,
			Severity:        AlertSeverityWarning,
			Scope:           AlertScopeDevice,
			DurationMinutes: 0,
			CooldownMinutes: 15,
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
		{
			Name:            "High Page Volume",
			Description:     "Alerts when daily page count exceeds 1000 pages",
			Enabled:         false, // Disabled by default - user can enable
			Type:            AlertTypeUsageHigh,
			Severity:        AlertSeverityInfo,
			Scope:           AlertScopeDevice,
			Threshold:       1000,
			ThresholdUnit:   "pages",
			DurationMinutes: 0,
			CooldownMinutes: 1440, // Once per day
			CreatedAt:       now,
			UpdatedAt:       now,
			CreatedBy:       "system",
		},
	}
}

// SeedDefaultAlertRules inserts the default alert rules if none exist.
func (s *BaseStore) SeedDefaultAlertRules(ctx context.Context) error {
	// Check if any alert rules exist
	var count int
	err := s.queryRowContext(ctx, "SELECT COUNT(*) FROM alert_rules").Scan(&count)
	if err != nil {
		return fmt.Errorf("check alert rules count: %w", err)
	}

	// Only seed if no rules exist
	if count > 0 {
		return nil
	}

	defaults := DefaultAlertRules()
	for _, rule := range defaults {
		if _, err := s.CreateAlertRule(ctx, &rule); err != nil {
			return fmt.Errorf("seed alert rule %q: %w", rule.Name, err)
		}
	}

	logInfo("Seeded default alert rules", "count", len(defaults))
	return nil
}
