// Package alerts provides HTTP API handlers for the alerting system.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	authz "printmaster/server/authz"
	"printmaster/server/storage"
)

// Store captures the persistence operations needed by the alerts API.
type Store interface {
	// Alert CRUD
	CreateAlert(context.Context, *storage.Alert) (int64, error)
	GetAlert(context.Context, int64) (*storage.Alert, error)
	ListActiveAlerts(context.Context, storage.AlertFilters) ([]storage.Alert, error)
	UpdateAlertStatus(context.Context, int64, storage.AlertStatus) error
	AcknowledgeAlert(context.Context, int64, string) error
	ResolveAlert(context.Context, int64) error

	// Alert Rules CRUD
	CreateAlertRule(context.Context, *storage.AlertRule) (int64, error)
	GetAlertRule(context.Context, int64) (*storage.AlertRule, error)
	ListAlertRules(context.Context) ([]storage.AlertRule, error)
	UpdateAlertRule(context.Context, *storage.AlertRule) error
	DeleteAlertRule(context.Context, int64) error

	// Notification Channels CRUD
	CreateNotificationChannel(context.Context, *storage.NotificationChannel) (int64, error)
	GetNotificationChannel(context.Context, int64) (*storage.NotificationChannel, error)
	ListNotificationChannels(context.Context) ([]storage.NotificationChannel, error)
	UpdateNotificationChannel(context.Context, *storage.NotificationChannel) error
	DeleteNotificationChannel(context.Context, int64) error

	// Escalation Policies CRUD
	CreateEscalationPolicy(context.Context, *storage.EscalationPolicy) (int64, error)
	GetEscalationPolicy(context.Context, int64) (*storage.EscalationPolicy, error)
	ListEscalationPolicies(context.Context) ([]storage.EscalationPolicy, error)
	UpdateEscalationPolicy(context.Context, *storage.EscalationPolicy) error
	DeleteEscalationPolicy(context.Context, int64) error

	// Maintenance Windows CRUD
	CreateAlertMaintenanceWindow(context.Context, *storage.AlertMaintenanceWindow) (int64, error)
	GetAlertMaintenanceWindow(context.Context, int64) (*storage.AlertMaintenanceWindow, error)
	ListAlertMaintenanceWindows(context.Context) ([]storage.AlertMaintenanceWindow, error)
	UpdateAlertMaintenanceWindow(context.Context, *storage.AlertMaintenanceWindow) error
	GetActiveAlertMaintenanceWindows(context.Context) ([]storage.AlertMaintenanceWindow, error)
	DeleteAlertMaintenanceWindow(context.Context, int64) error

	// Alert Settings
	GetAlertSettings(context.Context) (*storage.AlertSettings, error)
	SaveAlertSettings(context.Context, *storage.AlertSettings) error

	// Alert Summary
	GetAlertSummary(context.Context) (*storage.AlertSummary, error)
}

// APIOptions provides cross-cutting infrastructure for the HTTP layer.
type APIOptions struct {
	AuthMiddleware func(http.HandlerFunc) http.HandlerFunc
	Authorizer     func(*http.Request, authz.Action, authz.ResourceRef) error
	ActorResolver  func(*http.Request) string
	AuditLogger    func(*http.Request, *storage.AuditEntry)
	Notifier       *Notifier
}

// RouteConfig controls how HTTP handlers are registered.
type RouteConfig struct {
	Mux            *http.ServeMux
	FeatureEnabled bool
}

// API exposes HTTP handlers for the alerting system.
type API struct {
	store         Store
	notifier      *Notifier
	authWrap      func(http.HandlerFunc) http.HandlerFunc
	authorizer    func(*http.Request, authz.Action, authz.ResourceRef) error
	actorResolver func(*http.Request) string
	auditLogger   func(*http.Request, *storage.AuditEntry)
}

// NewAPI builds a new alerts API instance.
func NewAPI(store Store, opts APIOptions) (*API, error) {
	if store == nil {
		return nil, errors.New("alerts API requires a store")
	}
	return &API{
		store:         store,
		notifier:      opts.Notifier,
		authWrap:      opts.AuthMiddleware,
		authorizer:    opts.Authorizer,
		actorResolver: opts.ActorResolver,
		auditLogger:   opts.AuditLogger,
	}, nil
}

// RegisterRoutes wires all alert endpoints.
func (api *API) RegisterRoutes(cfg RouteConfig) {
	if !cfg.FeatureEnabled {
		return
	}

	mux := cfg.Mux
	if mux == nil {
		mux = http.DefaultServeMux
	}

	wrap := api.wrap

	// Alert summary (dashboard)
	mux.HandleFunc("/api/v1/alerts/summary", wrap(api.handleAlertSummary))

	// Alerts CRUD
	mux.HandleFunc("/api/v1/alerts", wrap(api.handleAlerts))
	mux.HandleFunc("/api/v1/alerts/", wrap(api.handleAlertRoute))

	// Alert Rules CRUD
	mux.HandleFunc("/api/v1/alert-rules", wrap(api.handleAlertRules))
	mux.HandleFunc("/api/v1/alert-rules/", wrap(api.handleAlertRuleRoute))

	// Notification Channels CRUD
	mux.HandleFunc("/api/v1/notification-channels", wrap(api.handleNotificationChannels))
	mux.HandleFunc("/api/v1/notification-channels/test", wrap(api.handleTestNotificationChannel))
	mux.HandleFunc("/api/v1/notification-channels/", wrap(api.handleNotificationChannelRoute))

	// Escalation Policies CRUD
	mux.HandleFunc("/api/v1/escalation-policies", wrap(api.handleEscalationPolicies))
	mux.HandleFunc("/api/v1/escalation-policies/", wrap(api.handleEscalationPolicyRoute))

	// Maintenance Windows CRUD
	mux.HandleFunc("/api/v1/maintenance-windows", wrap(api.handleMaintenanceWindows))
	mux.HandleFunc("/api/v1/maintenance-windows/", wrap(api.handleMaintenanceWindowRoute))

	// Alert Settings
	mux.HandleFunc("/api/v1/alert-settings", wrap(api.handleAlertSettings))
}

func (api *API) wrap(handler http.HandlerFunc) http.HandlerFunc {
	if api.authWrap == nil {
		return handler
	}
	return api.authWrap(handler)
}

func (api *API) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceRef) bool {
	if api.authorizer == nil {
		http.Error(w, "authorization not configured", http.StatusInternalServerError)
		return false
	}
	if err := api.authorizer(r, action, resource); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, authz.ErrUnauthorized) {
			status = http.StatusUnauthorized
		}
		http.Error(w, http.StatusText(status), status)
		return false
	}
	return true
}

func (api *API) actorLabel(r *http.Request) string {
	if api.actorResolver == nil {
		return "system"
	}
	if name := strings.TrimSpace(api.actorResolver(r)); name != "" {
		return name
	}
	return "system"
}

func (api *API) audit(r *http.Request, entry *storage.AuditEntry) {
	if api.auditLogger == nil || entry == nil {
		return
	}
	api.auditLogger(r, entry)
}

// ============================================================================
// Alert Summary Handler
// ============================================================================

func (api *API) handleAlertSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	summary, err := api.store.GetAlertSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get alert summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// ============================================================================
// Alerts Handlers
// ============================================================================

func (api *API) handleAlerts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListAlerts(w, r)
	case http.MethodPost:
		api.handleCreateAlert(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	filters := storage.AlertFilters{}

	// Parse query parameters
	if status := r.URL.Query().Get("status"); status != "" {
		filters.Status = storage.AlertStatus(status)
	}
	if severity := r.URL.Query().Get("severity"); severity != "" {
		filters.Severity = storage.AlertSeverity(severity)
	}
	if scope := r.URL.Query().Get("scope"); scope != "" {
		filters.Scope = storage.AlertScope(scope)
	}
	if alertType := r.URL.Query().Get("type"); alertType != "" {
		filters.Type = storage.AlertType(alertType)
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" {
		filters.TenantID = tenantID
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil && l > 0 {
			filters.Limit = l
		}
	}
	if offset := r.URL.Query().Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil && o >= 0 {
			filters.Offset = o
		}
	}

	// Get total count for pagination (without limit/offset)
	countFilters := filters
	countFilters.Limit = 0
	countFilters.Offset = 0
	allAlerts, err := api.store.ListActiveAlerts(r.Context(), countFilters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count alerts")
		return
	}
	totalCount := len(allAlerts)

	alerts, err := api.store.ListActiveAlerts(r.Context(), filters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alerts")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"alerts":      alerts,
		"count":       len(alerts),
		"total_count": totalCount,
		"offset":      filters.Offset,
		"limit":       filters.Limit,
		"has_more":    filters.Limit > 0 && filters.Offset+len(alerts) < totalCount,
	})
}

func (api *API) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var alert storage.Alert
	if err := decodeJSON(r.Body, &alert); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if alert.Type == "" {
		writeError(w, http.StatusBadRequest, "alert type is required")
		return
	}
	if alert.Severity == "" {
		alert.Severity = storage.AlertSeverityWarning
	}
	if alert.Scope == "" {
		alert.Scope = storage.AlertScopeDevice
	}
	if alert.Status == "" {
		alert.Status = storage.AlertStatusActive
	}

	id, err := api.store.CreateAlert(r.Context(), &alert)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create alert")
		return
	}

	alert.ID = id
	api.audit(r, &storage.AuditEntry{
		Action:     "alert.create",
		TargetType: "alert",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Created alert: %s (%s)", alert.Title, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusCreated, alert)
}

func (api *API) handleAlertRoute(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/v1/alerts/{id}[/action]
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid alert id")
		return
	}

	// Check for action subpath
	if len(parts) == 2 {
		action := parts[1]
		switch action {
		case "acknowledge":
			api.handleAcknowledgeAlert(w, r, id)
		case "resolve":
			api.handleResolveAlert(w, r, id)
		default:
			http.NotFound(w, r)
		}
		return
	}

	// Direct alert operations
	switch r.Method {
	case http.MethodGet:
		api.handleGetAlert(w, r, id)
	case http.MethodDelete:
		api.handleDeleteAlert(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetAlert(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	alert, err := api.store.GetAlert(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get alert")
		return
	}
	if alert == nil {
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, alert)
}

func (api *API) handleDeleteAlert(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	// Resolve the alert (soft delete - mark as resolved)
	if err := api.store.ResolveAlert(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete alert")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert.delete",
		TargetType: "alert",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Deleted alert (%s)", api.actorLabel(r)),
	})

	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handleAcknowledgeAlert(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	acknowledgedBy := api.actorLabel(r)
	if err := api.store.AcknowledgeAlert(r.Context(), id, acknowledgedBy); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to acknowledge alert")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert.acknowledge",
		TargetType: "alert",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Acknowledged alert (%s)", acknowledgedBy),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

func (api *API) handleResolveAlert(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if err := api.store.ResolveAlert(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve alert")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert.resolve",
		TargetType: "alert",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Resolved alert (%s)", api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

// ============================================================================
// Alert Rules Handlers
// ============================================================================

func (api *API) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListAlertRules(w, r)
	case http.MethodPost:
		api.handleCreateAlertRule(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleListAlertRules(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	// Note: filtering is done client-side for now since ListAlertRules doesn't take options
	rules, err := api.store.ListAlertRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alert rules")
		return
	}

	// Optional client-side filtering
	enabledOnly := r.URL.Query().Get("enabled") == "true"
	alertType := r.URL.Query().Get("type")

	filtered := make([]storage.AlertRule, 0, len(rules))
	for _, rule := range rules {
		if enabledOnly && !rule.Enabled {
			continue
		}
		if alertType != "" && string(rule.Type) != alertType {
			continue
		}
		filtered = append(filtered, rule)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rules": filtered,
		"count": len(filtered),
	})
}

func (api *API) handleCreateAlertRule(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var rule storage.AlertRule
	if err := decodeJSON(r.Body, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if rule.Name == "" {
		writeError(w, http.StatusBadRequest, "rule name is required")
		return
	}
	if rule.Type == "" {
		writeError(w, http.StatusBadRequest, "rule type is required")
		return
	}
	if rule.Severity == "" {
		rule.Severity = storage.AlertSeverityWarning
	}
	if rule.Scope == "" {
		rule.Scope = storage.AlertScopeDevice
	}

	rule.CreatedBy = api.actorLabel(r)

	id, err := api.store.CreateAlertRule(r.Context(), &rule)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create alert rule")
		return
	}

	rule.ID = id
	api.audit(r, &storage.AuditEntry{
		Action:     "alert_rule.create",
		TargetType: "alert_rule",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Created alert rule: %s (%s)", rule.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusCreated, rule)
}

func (api *API) handleAlertRuleRoute(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/alert-rules/")
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.handleGetAlertRule(w, r, id)
	case http.MethodPut:
		api.handleUpdateAlertRule(w, r, id)
	case http.MethodDelete:
		api.handleDeleteAlertRule(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetAlertRule(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	rule, err := api.store.GetAlertRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get alert rule")
		return
	}
	if rule == nil {
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, rule)
}

func (api *API) handleUpdateAlertRule(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var rule storage.AlertRule
	if err := decodeJSON(r.Body, &rule); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rule.ID = id
	if err := api.store.UpdateAlertRule(r.Context(), &rule); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update alert rule")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert_rule.update",
		TargetType: "alert_rule",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Updated alert rule: %s (%s)", rule.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, rule)
}

func (api *API) handleDeleteAlertRule(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if err := api.store.DeleteAlertRule(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete alert rule")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert_rule.delete",
		TargetType: "alert_rule",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Deleted alert rule (%s)", api.actorLabel(r)),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Notification Channels Handlers
// ============================================================================

func (api *API) handleNotificationChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListNotificationChannels(w, r)
	case http.MethodPost:
		api.handleCreateNotificationChannel(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleListNotificationChannels(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	channels, err := api.store.ListNotificationChannels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list notification channels")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"channels": channels,
		"count":    len(channels),
	})
}

func (api *API) handleCreateNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var channel storage.NotificationChannel
	if err := decodeJSON(r.Body, &channel); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if channel.Name == "" {
		writeError(w, http.StatusBadRequest, "channel name is required")
		return
	}
	if channel.Type == "" {
		writeError(w, http.StatusBadRequest, "channel type is required")
		return
	}

	id, err := api.store.CreateNotificationChannel(r.Context(), &channel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create notification channel")
		return
	}

	channel.ID = id
	api.audit(r, &storage.AuditEntry{
		Action:     "notification_channel.create",
		TargetType: "notification_channel",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Created notification channel: %s (%s)", channel.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusCreated, channel)
}

func (api *API) handleNotificationChannelRoute(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/notification-channels/")
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	// Handle /api/v1/notification-channels/{id}/test route
	if strings.HasSuffix(idStr, "/test") {
		idStr = strings.TrimSuffix(idStr, "/test")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid channel id")
			return
		}
		api.handleTestExistingChannel(w, r, id)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.handleGetNotificationChannel(w, r, id)
	case http.MethodPut:
		api.handleUpdateNotificationChannel(w, r, id)
	case http.MethodDelete:
		api.handleDeleteNotificationChannel(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetNotificationChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	channel, err := api.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}

	writeJSON(w, http.StatusOK, channel)
}

func (api *API) handleUpdateNotificationChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var req storage.NotificationChannel
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.ID = id

	if err := api.store.UpdateNotificationChannel(r.Context(), &req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update notification channel")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "notification_channel.update",
		TargetType: "notification_channel",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Updated notification channel: %s (%s)", req.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, &req)
}

func (api *API) handleDeleteNotificationChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if err := api.store.DeleteNotificationChannel(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete notification channel")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "notification_channel.delete",
		TargetType: "notification_channel",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Deleted notification channel (%s)", api.actorLabel(r)),
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleTestNotificationChannel tests a notification channel configuration (without saving).
// POST /api/v1/notification-channels/test
func (api *API) handleTestNotificationChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if api.notifier == nil {
		writeError(w, http.StatusServiceUnavailable, "notification service not available")
		return
	}

	var req struct {
		Type       string `json:"type"`
		Name       string `json:"name"`
		ConfigJSON string `json:"config_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Create a temporary channel for testing
	channel := &storage.NotificationChannel{
		Type:       storage.ChannelType(req.Type),
		Name:       req.Name,
		ConfigJSON: req.ConfigJSON,
	}

	// Create test alert
	testAlert := &storage.Alert{
		Type:     "test",
		Severity: "info",
		Title:    "Test Notification",
		Message:  "This is a test notification from PrintMaster to verify your channel configuration.",
	}

	if err := api.notifier.TestChannel(r.Context(), channel, testAlert); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("test failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "test notification sent"})
}

// handleTestExistingChannel tests an existing notification channel by ID.
// POST /api/v1/notification-channels/{id}/test
func (api *API) handleTestExistingChannel(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if api.notifier == nil {
		writeError(w, http.StatusServiceUnavailable, "notification service not available")
		return
	}

	channel, err := api.store.GetNotificationChannel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "notification channel not found")
		return
	}

	// Create test alert
	testAlert := &storage.Alert{
		Type:     "test",
		Severity: "info",
		Title:    "Test Notification",
		Message:  "This is a test notification from PrintMaster to verify your channel configuration.",
	}

	if err := api.notifier.TestChannel(r.Context(), channel, testAlert); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("test failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "test notification sent"})
}

// ============================================================================
// Escalation Policies Handlers
// ============================================================================

func (api *API) handleEscalationPolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListEscalationPolicies(w, r)
	case http.MethodPost:
		api.handleCreateEscalationPolicy(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleListEscalationPolicies(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	policies, err := api.store.ListEscalationPolicies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list escalation policies")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"policies": policies,
		"count":    len(policies),
	})
}

func (api *API) handleCreateEscalationPolicy(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var policy storage.EscalationPolicy
	if err := decodeJSON(r.Body, &policy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if policy.Name == "" {
		writeError(w, http.StatusBadRequest, "policy name is required")
		return
	}

	id, err := api.store.CreateEscalationPolicy(r.Context(), &policy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create escalation policy")
		return
	}

	policy.ID = id
	api.audit(r, &storage.AuditEntry{
		Action:     "escalation_policy.create",
		TargetType: "escalation_policy",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Created escalation policy: %s (%s)", policy.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusCreated, policy)
}

func (api *API) handleEscalationPolicyRoute(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/escalation-policies/")
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid policy id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.handleGetEscalationPolicy(w, r, id)
	case http.MethodPut:
		api.handleUpdateEscalationPolicy(w, r, id)
	case http.MethodDelete:
		api.handleDeleteEscalationPolicy(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetEscalationPolicy(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	policy, err := api.store.GetEscalationPolicy(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "escalation policy not found")
		return
	}

	writeJSON(w, http.StatusOK, policy)
}

func (api *API) handleUpdateEscalationPolicy(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var req storage.EscalationPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.ID = id

	if err := api.store.UpdateEscalationPolicy(r.Context(), &req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update escalation policy")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "escalation_policy.update",
		TargetType: "escalation_policy",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Updated escalation policy: %s (%s)", req.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, &req)
}

func (api *API) handleDeleteEscalationPolicy(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if err := api.store.DeleteEscalationPolicy(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete escalation policy")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "escalation_policy.delete",
		TargetType: "escalation_policy",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Deleted escalation policy (%s)", api.actorLabel(r)),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Maintenance Windows Handlers
// ============================================================================

func (api *API) handleMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleListMaintenanceWindows(w, r)
	case http.MethodPost:
		api.handleCreateMaintenanceWindow(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleListMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	// Check if only active windows requested
	activeOnly := r.URL.Query().Get("active") == "true"

	var windows []storage.AlertMaintenanceWindow
	var err error

	if activeOnly {
		windows, err = api.store.GetActiveAlertMaintenanceWindows(r.Context())
	} else {
		windows, err = api.store.ListAlertMaintenanceWindows(r.Context())
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list maintenance windows")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"windows": windows,
		"count":   len(windows),
	})
}

func (api *API) handleCreateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var window storage.AlertMaintenanceWindow
	if err := decodeJSON(r.Body, &window); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate required fields
	if window.Name == "" {
		writeError(w, http.StatusBadRequest, "window name is required")
		return
	}
	if window.StartTime.IsZero() {
		writeError(w, http.StatusBadRequest, "start time is required")
		return
	}
	if window.EndTime.IsZero() {
		writeError(w, http.StatusBadRequest, "end time is required")
		return
	}
	if window.EndTime.Before(window.StartTime) {
		writeError(w, http.StatusBadRequest, "end time must be after start time")
		return
	}

	window.CreatedBy = api.actorLabel(r)

	id, err := api.store.CreateAlertMaintenanceWindow(r.Context(), &window)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create maintenance window")
		return
	}

	window.ID = id
	api.audit(r, &storage.AuditEntry{
		Action:     "maintenance_window.create",
		TargetType: "maintenance_window",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Created maintenance window: %s (%s)", window.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusCreated, window)
}

func (api *API) handleMaintenanceWindowRoute(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/maintenance-windows/")
	idStr = strings.Trim(idStr, "/")
	if idStr == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid window id")
		return
	}

	switch r.Method {
	case http.MethodGet:
		api.handleGetMaintenanceWindow(w, r, id)
	case http.MethodPut:
		api.handleUpdateMaintenanceWindow(w, r, id)
	case http.MethodDelete:
		api.handleDeleteMaintenanceWindow(w, r, id)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetMaintenanceWindow(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	window, err := api.store.GetAlertMaintenanceWindow(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "maintenance window not found")
		return
	}

	writeJSON(w, http.StatusOK, window)
}

func (api *API) handleUpdateMaintenanceWindow(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var req storage.AlertMaintenanceWindow
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.ID = id

	if err := api.store.UpdateAlertMaintenanceWindow(r.Context(), &req); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update maintenance window")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "maintenance_window.update",
		TargetType: "maintenance_window",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Updated maintenance window: %s (%s)", req.Name, api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, &req)
}

func (api *API) handleDeleteMaintenanceWindow(w http.ResponseWriter, r *http.Request, id int64) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	if err := api.store.DeleteAlertMaintenanceWindow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete maintenance window")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "maintenance_window.delete",
		TargetType: "maintenance_window",
		TargetID:   fmt.Sprintf("%d", id),
		Details:    fmt.Sprintf("Deleted maintenance window (%s)", api.actorLabel(r)),
	})

	w.WriteHeader(http.StatusNoContent)
}

// ============================================================================
// Alert Settings Handlers
// ============================================================================

func (api *API) handleAlertSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		api.handleGetAlertSettings(w, r)
	case http.MethodPut:
		api.handleSaveAlertSettings(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (api *API) handleGetAlertSettings(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsRead, authz.ResourceRef{}) {
		return
	}

	settings, err := api.store.GetAlertSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get alert settings")
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (api *API) handleSaveAlertSettings(w http.ResponseWriter, r *http.Request) {
	if !api.authorize(w, r, authz.ActionSettingsAlertsWrite, authz.ResourceRef{}) {
		return
	}

	var settings storage.AlertSettings
	if err := decodeJSON(r.Body, &settings); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := api.store.SaveAlertSettings(r.Context(), &settings); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save alert settings")
		return
	}

	api.audit(r, &storage.AuditEntry{
		Action:     "alert_settings.update",
		TargetType: "alert_settings",
		TargetID:   "global",
		Details:    fmt.Sprintf("Updated alert settings (%s)", api.actorLabel(r)),
	})

	writeJSON(w, http.StatusOK, settings)
}

// ============================================================================
// Helper Functions
// ============================================================================

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		// Log error but can't do much at this point
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func decodeJSON(body io.Reader, v interface{}) error {
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20)) // 1MB limit
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

// Ensure time.Time is imported for maintenance window validation
var _ = time.Now
