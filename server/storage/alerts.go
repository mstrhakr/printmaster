package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// AlertSeverity defines alert severity levels.
type AlertSeverity string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertScope defines the scope level of an alert.
type AlertScope string

const (
	AlertScopeDevice AlertScope = "device"
	AlertScopeAgent  AlertScope = "agent"
	AlertScopeSite   AlertScope = "site"
	AlertScopeTenant AlertScope = "tenant"
	AlertScopeFleet  AlertScope = "fleet"
)

// AlertStatus defines alert lifecycle states.
type AlertStatus string

const (
	AlertStatusActive       AlertStatus = "active"
	AlertStatusAcknowledged AlertStatus = "acknowledged"
	AlertStatusResolved     AlertStatus = "resolved"
	AlertStatusSuppressed   AlertStatus = "suppressed"
	AlertStatusExpired      AlertStatus = "expired"
)

// AlertType defines the type of alert condition.
type AlertType string

const (
	// Device alerts
	AlertTypeSupplyLow      AlertType = "device.supply.low"
	AlertTypeSupplyCritical AlertType = "device.supply.critical"
	AlertTypeDeviceOffline  AlertType = "device.offline"
	AlertTypeDeviceError    AlertType = "device.error"
	AlertTypeUsageHigh      AlertType = "device.usage.high"
	AlertTypeUsageSpike     AlertType = "device.usage.spike"

	// Agent alerts
	AlertTypeAgentOffline      AlertType = "agent.offline"
	AlertTypeAgentUnhealthy    AlertType = "agent.unhealthy"
	AlertTypeAgentUpdateFailed AlertType = "agent.update.failed"
	AlertTypeAgentOutdated     AlertType = "agent.version.outdated"
	AlertTypeAgentStorageFull  AlertType = "agent.storage.full"

	// Site alerts
	AlertTypeSiteOutage        AlertType = "site.outage"
	AlertTypeSitePartialOutage AlertType = "site.partial_outage"
	AlertTypeSiteDegraded      AlertType = "site.degraded"

	// Tenant alerts
	AlertTypeTenantOutage        AlertType = "tenant.outage"
	AlertTypeTenantPartialOutage AlertType = "tenant.partial_outage"
	AlertTypeTenantNoData        AlertType = "tenant.no_data"

	// Fleet alerts
	AlertTypeFleetMassOutage  AlertType = "fleet.mass_outage"
	AlertTypeFleetUpdateStall AlertType = "fleet.update.stalled"
)

// Alert represents an active or historical alert instance.
type Alert struct {
	ID       int64         `json:"id"`
	RuleID   int64         `json:"rule_id,omitempty"`
	Type     AlertType     `json:"type"`
	Severity AlertSeverity `json:"severity"`
	Scope    AlertScope    `json:"scope"`
	Status   AlertStatus   `json:"status"`

	// Scope identifiers (one will be set based on scope)
	TenantID     string `json:"tenant_id,omitempty"`
	SiteID       string `json:"site_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	DeviceSerial string `json:"device_serial,omitempty"`

	Title   string `json:"title"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"` // JSON blob with type-specific details

	// Timestamps
	TriggeredAt     time.Time  `json:"triggered_at"`
	AcknowledgedAt  *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy  string     `json:"acknowledged_by,omitempty"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`

	// Escalation tracking
	EscalationLevel int        `json:"escalation_level"`
	LastEscalatedAt *time.Time `json:"last_escalated_at,omitempty"`

	// Flapping detection
	StateChangeCount int  `json:"state_change_count"`
	IsFlapping       bool `json:"is_flapping"`

	// Parent alert (for grouped alerts)
	ParentAlertID *int64 `json:"parent_alert_id,omitempty"`
	ChildCount    int    `json:"child_count,omitempty"`

	// Notification tracking
	NotificationsSent int        `json:"notifications_sent"`
	LastNotifiedAt    *time.Time `json:"last_notified_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AlertRule defines conditions that trigger alerts.
type AlertRule struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`

	Type     AlertType     `json:"type"`
	Severity AlertSeverity `json:"severity"`
	Scope    AlertScope    `json:"scope"`

	// Scope filter (empty = all)
	TenantIDs []string `json:"tenant_ids,omitempty"`
	SiteIDs   []string `json:"site_ids,omitempty"`
	AgentIDs  []string `json:"agent_ids,omitempty"`

	// Condition configuration (type-specific)
	ConditionJSON string `json:"condition_json,omitempty"`

	// Thresholds
	Threshold       float64 `json:"threshold,omitempty"`
	ThresholdUnit   string  `json:"threshold_unit,omitempty"`
	DurationMinutes int     `json:"duration_minutes,omitempty"`

	// Associated notification channels
	ChannelIDs []int64 `json:"channel_ids,omitempty"`

	// Escalation policy
	EscalationPolicyID *int64 `json:"escalation_policy_id,omitempty"`

	// Rate limiting
	CooldownMinutes int `json:"cooldown_minutes"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// NotificationChannelType defines the channel delivery method.
type NotificationChannelType string

const (
	ChannelTypeEmail     NotificationChannelType = "email"
	ChannelTypeWebhook   NotificationChannelType = "webhook"
	ChannelTypeSlack     NotificationChannelType = "slack"
	ChannelTypeTeams     NotificationChannelType = "teams"
	ChannelTypePagerDuty NotificationChannelType = "pagerduty"
)

// NotificationChannel configures where alerts are sent.
type NotificationChannel struct {
	ID      int64                   `json:"id"`
	Name    string                  `json:"name"`
	Type    NotificationChannelType `json:"type"`
	Enabled bool                    `json:"enabled"`

	// Configuration (type-specific, stored as JSON)
	ConfigJSON string `json:"config_json,omitempty"`

	// Filters
	MinSeverity AlertSeverity `json:"min_severity"`
	TenantIDs   []string      `json:"tenant_ids,omitempty"` // Empty = all tenants

	// Rate limiting
	RateLimitPerHour int        `json:"rate_limit_per_hour"`
	LastSentAt       *time.Time `json:"last_sent_at,omitempty"`
	SentThisHour     int        `json:"sent_this_hour"`

	// Quiet hours
	UseQuietHours bool `json:"use_quiet_hours"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EscalationPolicy defines how alerts escalate over time.
type EscalationPolicy struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`

	// Escalation steps (JSON array of steps)
	StepsJSON string `json:"steps_json"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EscalationStep defines a single escalation step.
type EscalationStep struct {
	DelayMinutes     int            `json:"delay_minutes"`
	ChannelIDs       []int64        `json:"channel_ids"`
	EscalateSeverity *AlertSeverity `json:"escalate_severity,omitempty"`
	NotifyAgain      bool           `json:"notify_again"`
}

// AlertMaintenanceWindow defines a period of alert suppression.
type AlertMaintenanceWindow struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Scope
	Scope        AlertScope `json:"scope"`
	TenantID     string     `json:"tenant_id,omitempty"`
	SiteID       string     `json:"site_id,omitempty"`
	AgentID      string     `json:"agent_id,omitempty"`
	DeviceSerial string     `json:"device_serial,omitempty"`

	// Time window
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Timezone  string    `json:"timezone"`

	// Recurring options
	Recurring    bool   `json:"recurring"`
	RecurPattern string `json:"recur_pattern,omitempty"` // weekly, monthly
	RecurDays    []int  `json:"recur_days,omitempty"`    // 0=Sun, 1=Mon, etc.

	// Alert types to suppress (empty = all)
	AlertTypes []AlertType `json:"alert_types,omitempty"`

	// Allow critical alerts through
	AllowCritical bool `json:"allow_critical"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// QuietHoursConfig defines global quiet hours settings.
type QuietHoursConfig struct {
	Enabled       bool   `json:"enabled"`
	StartTime     string `json:"start_time"` // HH:MM format
	EndTime       string `json:"end_time"`   // HH:MM format
	Timezone      string `json:"timezone"`
	AllowCritical bool   `json:"allow_critical"`
}

// AlertSettings holds global alerting configuration.
type AlertSettings struct {
	// Quiet hours
	QuietHours QuietHoursConfig `json:"quiet_hours"`

	// Flapping detection
	FlappingEnabled    bool `json:"flapping_enabled"`
	FlappingThreshold  int  `json:"flapping_threshold"` // State changes
	FlappingWindowMins int  `json:"flapping_window_mins"`

	// Alert grouping
	GroupingEnabled   bool `json:"grouping_enabled"`
	GroupingThreshold int  `json:"grouping_threshold"` // Percentage

	// Dependencies
	DependenciesEnabled bool `json:"dependencies_enabled"`
}

// AlertSummary provides dashboard statistics.
type AlertSummary struct {
	HealthyCounts struct {
		Devices int `json:"devices"`
		Agents  int `json:"agents"`
		Sites   int `json:"sites"`
		Tenants int `json:"tenants"`
	} `json:"healthy_counts"`

	WarningCounts struct {
		Devices int `json:"devices"`
		Agents  int `json:"agents"`
		Sites   int `json:"sites"`
		Tenants int `json:"tenants"`
	} `json:"warning_counts"`

	CriticalCounts struct {
		Devices int `json:"devices"`
		Agents  int `json:"agents"`
		Sites   int `json:"sites"`
		Tenants int `json:"tenants"`
	} `json:"critical_counts"`

	OfflineCounts struct {
		Devices int `json:"devices"`
		Agents  int `json:"agents"`
	} `json:"offline_counts"`

	AlertsByType map[string]int `json:"alerts_by_type"`

	ActiveRules       int `json:"active_rules"`
	ActiveChannels    int `json:"active_channels"`
	SuppressedCount   int `json:"suppressed_count"`
	AcknowledgedCount int `json:"acknowledged_count"`

	IsQuietHours   bool `json:"is_quiet_hours"`
	HasMaintenance bool `json:"has_maintenance"`
}

// ============================================================
// Alert Storage Methods
// ============================================================

// CreateAlert inserts a new alert.
func (s *SQLiteStore) CreateAlert(ctx context.Context, alert *Alert) (int64, error) {
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
	result, err := s.db.ExecContext(ctx, query,
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

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get alert id: %w", err)
	}
	alert.ID = id
	alert.CreatedAt = now
	alert.UpdatedAt = now

	return id, nil
}

// GetAlert retrieves an alert by ID.
func (s *SQLiteStore) GetAlert(ctx context.Context, id int64) (*Alert, error) {
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

	err := s.db.QueryRowContext(ctx, query, id).Scan(
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
func (s *SQLiteStore) ListActiveAlerts(ctx context.Context, filters AlertFilters) ([]Alert, error) {
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
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active alerts: %w", err)
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// AlertFilters defines query filters for alerts.
type AlertFilters struct {
	Severity  AlertSeverity
	Scope     AlertScope
	Type      AlertType
	Status    AlertStatus
	TenantID  string
	SiteID    string
	AgentID   string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

// UpdateAlertStatus changes an alert's status.
func (s *SQLiteStore) UpdateAlertStatus(ctx context.Context, id int64, status AlertStatus) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE alerts 
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, now, id)
	return err
}

// AcknowledgeAlert marks an alert as acknowledged.
func (s *SQLiteStore) AcknowledgeAlert(ctx context.Context, id int64, username string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE alerts 
		SET status = 'acknowledged', acknowledged_at = ?, acknowledged_by = ?, updated_at = ?
		WHERE id = ? AND status = 'active'
	`, now, username, now, id)
	return err
}

// ResolveAlert marks an alert as resolved.
func (s *SQLiteStore) ResolveAlert(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE alerts 
		SET status = 'resolved', resolved_at = ?, updated_at = ?
		WHERE id = ? AND status IN ('active', 'acknowledged')
	`, now, now, id)
	return err
}

// SuppressAlert suppresses an alert until a given time.
func (s *SQLiteStore) SuppressAlert(ctx context.Context, id int64, until time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE alerts 
		SET status = 'suppressed', suppressed_until = ?, updated_at = ?
		WHERE id = ?
	`, until, now, id)
	return err
}

// ListAlertHistory returns resolved/expired alerts within a time range.
func (s *SQLiteStore) ListAlertHistory(ctx context.Context, filters AlertFilters) ([]Alert, error) {
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

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert history: %w", err)
	}
	defer rows.Close()

	return scanAlerts(rows)
}

// scanAlerts scans rows into a slice of Alert structs.
func scanAlerts(rows *sql.Rows) ([]Alert, error) {
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
// Alert Rule Storage Methods
// ============================================================

// CreateAlertRule creates a new alert rule.
func (s *SQLiteStore) CreateAlertRule(ctx context.Context, rule *AlertRule) (int64, error) {
	channelsJSON, _ := json.Marshal(rule.ChannelIDs)
	tenantIDsJSON, _ := json.Marshal(rule.TenantIDs)
	siteIDsJSON, _ := json.Marshal(rule.SiteIDs)
	agentIDsJSON, _ := json.Marshal(rule.AgentIDs)

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
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

	id, _ := result.LastInsertId()
	rule.ID = id
	rule.CreatedAt = now
	rule.UpdatedAt = now

	return id, nil
}

// ListAlertRules returns all alert rules.
func (s *SQLiteStore) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `
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
func (s *SQLiteStore) GetAlertRule(ctx context.Context, id int64) (*AlertRule, error) {
	var r AlertRule
	var tenantIDsJSON, siteIDsJSON, agentIDsJSON, channelsJSON, conditionJSON, thresholdUnit, createdBy sql.NullString
	var escalationPolicyID sql.NullInt64
	var description sql.NullString

	err := s.db.QueryRowContext(ctx, `
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
func (s *SQLiteStore) UpdateAlertRule(ctx context.Context, rule *AlertRule) error {
	channelsJSON, _ := json.Marshal(rule.ChannelIDs)
	tenantIDsJSON, _ := json.Marshal(rule.TenantIDs)
	siteIDsJSON, _ := json.Marshal(rule.SiteIDs)
	agentIDsJSON, _ := json.Marshal(rule.AgentIDs)

	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
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
func (s *SQLiteStore) DeleteAlertRule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM alert_rules WHERE id = ?", id)
	return err
}

// ============================================================
// Notification Channel Storage Methods
// ============================================================

// CreateNotificationChannel creates a new notification channel.
func (s *SQLiteStore) CreateNotificationChannel(ctx context.Context, ch *NotificationChannel) (int64, error) {
	tenantIDsJSON, _ := json.Marshal(ch.TenantIDs)
	now := time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
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

	id, _ := result.LastInsertId()
	ch.ID = id
	ch.CreatedAt = now
	ch.UpdatedAt = now

	return id, nil
}

// ListNotificationChannels returns all notification channels.
func (s *SQLiteStore) ListNotificationChannels(ctx context.Context) ([]NotificationChannel, error) {
	rows, err := s.db.QueryContext(ctx, `
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
func (s *SQLiteStore) DeleteNotificationChannel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM notification_channels WHERE id = ?", id)
	return err
}

// ============================================================
// Escalation Policy Storage Methods
// ============================================================

// CreateEscalationPolicy creates a new escalation policy.
func (s *SQLiteStore) CreateEscalationPolicy(ctx context.Context, policy *EscalationPolicy) (int64, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO escalation_policies (name, description, enabled, steps_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, policy.Name, policy.Description, policy.Enabled, policy.StepsJSON, now, now)
	if err != nil {
		return 0, fmt.Errorf("insert escalation policy: %w", err)
	}

	id, _ := result.LastInsertId()
	policy.ID = id
	policy.CreatedAt = now
	policy.UpdatedAt = now

	return id, nil
}

// ListEscalationPolicies returns all escalation policies.
func (s *SQLiteStore) ListEscalationPolicies(ctx context.Context) ([]EscalationPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
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

		policies = append(policies, p)
	}

	return policies, rows.Err()
}

// DeleteEscalationPolicy removes an escalation policy by ID.
func (s *SQLiteStore) DeleteEscalationPolicy(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM escalation_policies WHERE id = ?", id)
	return err
}

// ============================================================
// Alert Maintenance Window Storage Methods
// ============================================================

// CreateAlertMaintenanceWindow creates a new maintenance window.
func (s *SQLiteStore) CreateAlertMaintenanceWindow(ctx context.Context, mw *AlertMaintenanceWindow) (int64, error) {
	alertTypesJSON, _ := json.Marshal(mw.AlertTypes)
	recurDaysJSON, _ := json.Marshal(mw.RecurDays)
	now := time.Now().UTC()

	result, err := s.db.ExecContext(ctx, `
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

	id, _ := result.LastInsertId()
	mw.ID = id
	mw.CreatedAt = now
	mw.UpdatedAt = now

	return id, nil
}

// ListAlertMaintenanceWindows returns all maintenance windows.
func (s *SQLiteStore) ListAlertMaintenanceWindows(ctx context.Context) ([]AlertMaintenanceWindow, error) {
	rows, err := s.db.QueryContext(ctx, `
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

// GetActiveAlertMaintenanceWindows returns currently active maintenance windows.
func (s *SQLiteStore) GetActiveAlertMaintenanceWindows(ctx context.Context) ([]AlertMaintenanceWindow, error) {
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `
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
func (s *SQLiteStore) DeleteAlertMaintenanceWindow(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM maintenance_windows WHERE id = ?", id)
	return err
}

// ============================================================
// Alert Settings Storage Methods
// ============================================================

// GetAlertSettings retrieves the global alert settings.
func (s *SQLiteStore) GetAlertSettings(ctx context.Context) (*AlertSettings, error) {
	var settingsJSON sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT value FROM settings_global WHERE key = 'alert_settings'
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
func (s *SQLiteStore) SaveAlertSettings(ctx context.Context, settings *AlertSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal alert settings: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings_global (key, value) VALUES ('alert_settings', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, string(data))

	return err
}

// GetAlertSummary computes dashboard statistics.
func (s *SQLiteStore) GetAlertSummary(ctx context.Context) (*AlertSummary, error) {
	summary := &AlertSummary{
		AlertsByType: make(map[string]int),
	}

	// Count active alerts by type
	rows, err := s.db.QueryContext(ctx, `
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

	// Count active rules
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alert_rules WHERE enabled = 1`).Scan(&summary.ActiveRules)
	if err != nil {
		return nil, err
	}

	// Count active channels
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_channels WHERE enabled = 1`).Scan(&summary.ActiveChannels)
	if err != nil {
		return nil, err
	}

	// Count suppressed
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'suppressed'`).Scan(&summary.SuppressedCount)
	if err != nil {
		return nil, err
	}

	// Count acknowledged
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE status = 'acknowledged'`).Scan(&summary.AcknowledgedCount)
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

// isInQuietHours checks if the current time is within quiet hours.
func isInQuietHours(config QuietHoursConfig) bool {
	if !config.Enabled {
		return false
	}

	// Parse start and end times
	now := time.Now()
	startParts := []int{22, 0}
	endParts := []int{7, 0}

	fmt.Sscanf(config.StartTime, "%d:%d", &startParts[0], &startParts[1])
	fmt.Sscanf(config.EndTime, "%d:%d", &endParts[0], &endParts[1])

	currentMins := now.Hour()*60 + now.Minute()
	startMins := startParts[0]*60 + startParts[1]
	endMins := endParts[0]*60 + endParts[1]

	// Handle overnight window (e.g., 22:00 to 07:00)
	if startMins > endMins {
		return currentMins >= startMins || currentMins < endMins
	}
	return currentMins >= startMins && currentMins < endMins
}

// nullInt64Ptr is a helper for optional int64 pointers
func nullInt64Ptr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

// nullTimePtr is a helper for optional time pointers
func nullTimePtr(v *time.Time) sql.NullTime {
	if v == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *v, Valid: true}
}
