package storage

import (
	"encoding/json"
	"time"
)

// ============================================================
// Alert Types
// ============================================================

// AlertStatus represents the status of an alert (string type for backwards compatibility)
type AlertStatus = string

const (
	AlertStatusActive       AlertStatus = "active"
	AlertStatusAcknowledged AlertStatus = "acknowledged"
	AlertStatusResolved     AlertStatus = "resolved"
	AlertStatusSuppressed   AlertStatus = "suppressed"
	AlertStatusExpired      AlertStatus = "expired"
)

// AlertSeverity represents the severity level of an alert (string type for backwards compatibility)
type AlertSeverity = string

const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertScope represents the scope of an alert (string type for backwards compatibility)
type AlertScope = string

const (
	AlertScopeFleet  AlertScope = "fleet"
	AlertScopeTenant AlertScope = "tenant"
	AlertScopeSite   AlertScope = "site"
	AlertScopeAgent  AlertScope = "agent"
	AlertScopeDevice AlertScope = "device"
)

// AlertType represents the type of alert (string type for backwards compatibility)
type AlertType = string

const (
	AlertTypeDeviceOffline     AlertType = "device_offline"
	AlertTypeAgentOffline      AlertType = "agent_offline"
	AlertTypeTonerLow          AlertType = "toner_low"
	AlertTypeTonerCritical     AlertType = "toner_critical"
	AlertTypePaperLow          AlertType = "paper_low"
	AlertTypePaperJam          AlertType = "paper_jam"
	AlertTypeServiceRequired   AlertType = "service_required"
	AlertTypeError             AlertType = "error"
	AlertTypeUsageThreshold    AlertType = "usage_threshold"
	AlertTypeMaintenanceDue    AlertType = "maintenance_due"
	AlertTypeConnectionFailure AlertType = "connection_failure"
	AlertTypeSupplyLow         AlertType = "supply_low"
	AlertTypeSupplyCritical    AlertType = "supply_critical"
	AlertTypeDeviceError       AlertType = "device_error"
	AlertTypeAgentOutdated     AlertType = "agent_outdated"
	AlertTypeAgentStorageFull  AlertType = "agent_storage_full"
	AlertTypeUsageHigh         AlertType = "usage_high"
	AlertTypeSiteOutage        AlertType = "site_outage"
	AlertTypeFleetMassOutage   AlertType = "fleet_mass_outage"
	AlertTypeCustom            AlertType = "custom"
)

// ChannelType represents the type of notification channel (string type for backwards compatibility)
type ChannelType = string

const (
	ChannelTypeEmail     ChannelType = "email"
	ChannelTypeWebhook   ChannelType = "webhook"
	ChannelTypeSlack     ChannelType = "slack"
	ChannelTypeTeams     ChannelType = "teams"
	ChannelTypePagerDuty ChannelType = "pagerduty"
	ChannelTypeSMS       ChannelType = "sms"
	ChannelTypePush      ChannelType = "push"
)

// Alert represents an active or historical alert
type Alert struct {
	ID                int64      `json:"id"`
	RuleID            int64      `json:"rule_id,omitempty"`
	Type              string     `json:"type"`
	Severity          string     `json:"severity"`
	Scope             string     `json:"scope"`
	Status            string     `json:"status"`
	TenantID          string     `json:"tenant_id,omitempty"`
	SiteID            string     `json:"site_id,omitempty"`
	AgentID           string     `json:"agent_id,omitempty"`
	DeviceSerial      string     `json:"device_serial,omitempty"`
	Title             string     `json:"title"`
	Message           string     `json:"message"`
	Details           string     `json:"details,omitempty"`
	TriggeredAt       time.Time  `json:"triggered_at"`
	AcknowledgedAt    *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy    string     `json:"acknowledged_by,omitempty"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
	SuppressedUntil   *time.Time `json:"suppressed_until,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	EscalationLevel   int        `json:"escalation_level"`
	LastEscalatedAt   *time.Time `json:"last_escalated_at,omitempty"`
	StateChangeCount  int        `json:"state_change_count"`
	IsFlapping        bool       `json:"is_flapping"`
	ParentAlertID     *int64     `json:"parent_alert_id,omitempty"`
	ChildCount        int        `json:"child_count"`
	NotificationsSent int        `json:"notifications_sent"`
	LastNotifiedAt    *time.Time `json:"last_notified_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// AlertFilters defines filters for listing alerts
type AlertFilters struct {
	Status    string     `json:"status,omitempty"`
	Severity  string     `json:"severity,omitempty"`
	Scope     string     `json:"scope,omitempty"`
	Type      string     `json:"type,omitempty"`
	TenantID  string     `json:"tenant_id,omitempty"`
	SiteID    string     `json:"site_id,omitempty"`
	AgentID   string     `json:"agent_id,omitempty"`
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Since     *time.Time `json:"since,omitempty"` // Alias for StartTime
	Until     *time.Time `json:"until,omitempty"` // Alias for EndTime
	Limit     int        `json:"limit,omitempty"`
	Offset    int        `json:"offset,omitempty"`
}

// AlertFilter is an alias for AlertFilters for backwards compatibility
type AlertFilter = AlertFilters

// AlertRule defines an alert rule configuration
type AlertRule struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description,omitempty"`
	Enabled            bool      `json:"enabled"`
	Type               string    `json:"type"`
	Severity           string    `json:"severity"`
	Scope              string    `json:"scope"`
	TenantIDs          []string  `json:"tenant_ids,omitempty"`
	SiteIDs            []string  `json:"site_ids,omitempty"`
	AgentIDs           []string  `json:"agent_ids,omitempty"`
	ConditionJSON      string    `json:"condition_json,omitempty"`
	Threshold          float64   `json:"threshold,omitempty"`
	ThresholdUnit      string    `json:"threshold_unit,omitempty"`
	DurationMinutes    int       `json:"duration_minutes"`
	ChannelIDs         []int64   `json:"channel_ids,omitempty"`
	EscalationPolicyID *int64    `json:"escalation_policy_id,omitempty"`
	CooldownMinutes    int       `json:"cooldown_minutes"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	CreatedBy          string    `json:"created_by,omitempty"`
}

// NotificationChannelType represents the type of notification channel
type NotificationChannelType string

const (
	NotificationChannelEmail   NotificationChannelType = "email"
	NotificationChannelWebhook NotificationChannelType = "webhook"
	NotificationChannelSlack   NotificationChannelType = "slack"
	NotificationChannelTeams   NotificationChannelType = "teams"
	NotificationChannelPager   NotificationChannelType = "pager"
)

// NotificationChannel defines a notification channel configuration
type NotificationChannel struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Type             string     `json:"type"`
	Enabled          bool       `json:"enabled"`
	ConfigJSON       string     `json:"config_json,omitempty"`
	MinSeverity      string     `json:"min_severity"`
	TenantIDs        []string   `json:"tenant_ids,omitempty"`
	RateLimitPerHour int        `json:"rate_limit_per_hour"`
	LastSentAt       *time.Time `json:"last_sent_at,omitempty"`
	SentThisHour     int        `json:"sent_this_hour"`
	UseQuietHours    bool       `json:"use_quiet_hours"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// EscalationStep defines a single step in an escalation policy
type EscalationStep struct {
	DelayMinutes int     `json:"delay_minutes"`
	ChannelIDs   []int64 `json:"channel_ids"`
	Repeat       int     `json:"repeat,omitempty"`
}

// EscalationPolicy defines an alert escalation policy
type EscalationPolicy struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Enabled     bool             `json:"enabled"`
	Steps       []EscalationStep `json:"steps"`
	StepsJSON   string           `json:"-"` // Internal JSON representation
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// MarshalSteps converts the Steps array to StepsJSON string for database storage.
func (p *EscalationPolicy) MarshalSteps() {
	if len(p.Steps) > 0 {
		data, _ := json.Marshal(p.Steps)
		p.StepsJSON = string(data)
	} else if p.StepsJSON == "" {
		p.StepsJSON = "[]"
	}
}

// UnmarshalSteps converts the StepsJSON string to Steps array.
func (p *EscalationPolicy) UnmarshalSteps() {
	if p.StepsJSON != "" {
		json.Unmarshal([]byte(p.StepsJSON), &p.Steps)
	}
}

// AlertMaintenanceWindow defines a maintenance window for alert suppression
type AlertMaintenanceWindow struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Scope         string    `json:"scope"`
	TenantID      string    `json:"tenant_id,omitempty"`
	SiteID        string    `json:"site_id,omitempty"`
	AgentID       string    `json:"agent_id,omitempty"`
	DeviceSerial  string    `json:"device_serial,omitempty"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	Timezone      string    `json:"timezone"`
	Recurring     bool      `json:"recurring"`
	RecurPattern  string    `json:"recur_pattern,omitempty"`
	RecurDays     []string  `json:"recur_days,omitempty"`
	AlertTypes    []string  `json:"alert_types,omitempty"`
	AllowCritical bool      `json:"allow_critical"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CreatedBy     string    `json:"created_by,omitempty"`
}

// QuietHours defines quiet hours settings
type QuietHours struct {
	Enabled       bool   `json:"enabled"`
	StartTime     string `json:"start_time"` // HH:MM format
	EndTime       string `json:"end_time"`   // HH:MM format
	Timezone      string `json:"timezone"`
	DaysOfWeek    []int  `json:"days_of_week,omitempty"` // 0=Sunday, 6=Saturday
	AllowCritical bool   `json:"allow_critical"`
}

// QuietHoursConfig is an alias for QuietHours for backwards compatibility
type QuietHoursConfig = QuietHours

// AlertSettings defines global alert settings
type AlertSettings struct {
	Enabled               bool       `json:"enabled"`
	DefaultCooldownMins   int        `json:"default_cooldown_mins"`
	MaxAlertsPerHour      int        `json:"max_alerts_per_hour"`
	AlertRetentionDays    int        `json:"alert_retention_days"`
	AggregationEnabled    bool       `json:"aggregation_enabled"`
	AggregationWindowMins int        `json:"aggregation_window_mins"`
	QuietHours            QuietHours `json:"quiet_hours"`
	FlappingEnabled       bool       `json:"flapping_enabled"`
	FlappingThreshold     int        `json:"flapping_threshold"`
	FlappingWindowMins    int        `json:"flapping_window_mins"`
	GroupingEnabled       bool       `json:"grouping_enabled"`
	GroupingThreshold     int        `json:"grouping_threshold"`
	DependenciesEnabled   bool       `json:"dependencies_enabled"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// AlertCountsByScope holds counts broken down by scope level
type AlertCountsByScope struct {
	Devices int `json:"devices"`
	Agents  int `json:"agents"`
	Sites   int `json:"sites"`
	Tenants int `json:"tenants"`
}

// AlertSummary provides a summary of the alert state
type AlertSummary struct {
	ActiveCount        int                `json:"active_count"`
	AcknowledgedCount  int                `json:"acknowledged_count"`
	CriticalCount      int                `json:"critical_count"`
	WarningCount       int                `json:"warning_count"`
	InfoCount          int                `json:"info_count"`
	ResolvedTodayCount int                `json:"resolved_today_count"`
	SuppressedCount    int                `json:"suppressed_count"`
	ActiveRules        int                `json:"active_rules"`
	ActiveChannels     int                `json:"active_channels"`
	CriticalCounts     AlertCountsByScope `json:"critical_counts"`
	WarningCounts      AlertCountsByScope `json:"warning_counts"`
	HealthyCounts      AlertCountsByScope `json:"healthy_counts"`
	OfflineCounts      AlertCountsByScope `json:"offline_counts"`
	AlertsByType       map[string]int     `json:"alerts_by_type"`
	AlertsByScope      map[string]int     `json:"alerts_by_scope"`
	IsQuietHours       bool               `json:"is_quiet_hours"`
	HasMaintenance     bool               `json:"has_maintenance"`
}

// AlertHistoryEntry represents an entry in the alert history
type AlertHistoryEntry struct {
	ID        int64     `json:"id"`
	AlertID   int64     `json:"alert_id"`
	Action    string    `json:"action"`
	OldStatus string    `json:"old_status,omitempty"`
	NewStatus string    `json:"new_status,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	Details   string    `json:"details,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
