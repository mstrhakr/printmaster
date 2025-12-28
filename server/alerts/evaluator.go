// Package alerts provides alert evaluation and notification functionality.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"printmaster/server/storage"
)

// EvaluatorConfig configures the alert evaluator.
type EvaluatorConfig struct {
	// Interval between evaluation runs
	Interval time.Duration

	// Logger for evaluation events
	Logger *slog.Logger
}

// EvaluatorStore defines the storage operations needed by the evaluator.
type EvaluatorStore interface {
	// Device and agent data
	ListAllDevices(ctx context.Context) ([]*storage.Device, error)
	ListAgents(ctx context.Context) ([]*storage.Agent, error)
	GetLatestMetrics(ctx context.Context, serial string) (*storage.MetricsSnapshot, error)

	// Alert rules
	ListAlertRules(ctx context.Context) ([]storage.AlertRule, error)

	// Alert CRUD
	CreateAlert(ctx context.Context, alert *storage.Alert) (int64, error)
	ListActiveAlerts(ctx context.Context, filters storage.AlertFilters) ([]storage.Alert, error)
	ResolveAlert(ctx context.Context, id int64) error

	// Maintenance windows
	GetActiveAlertMaintenanceWindows(ctx context.Context) ([]storage.AlertMaintenanceWindow, error)

	// Settings
	GetAlertSettings(ctx context.Context) (*storage.AlertSettings, error)
}

// Evaluator periodically evaluates alert rules against device/agent state.
type Evaluator struct {
	store  EvaluatorStore
	config EvaluatorConfig
	logger *slog.Logger

	mu       sync.RWMutex
	running  bool
	stopChan chan struct{}
}

// NewEvaluator creates a new alert evaluator.
func NewEvaluator(store EvaluatorStore, config EvaluatorConfig) *Evaluator {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if config.Interval == 0 {
		config.Interval = 60 * time.Second
	}

	return &Evaluator{
		store:    store,
		config:   config,
		logger:   logger,
		stopChan: make(chan struct{}),
	}
}

// Start begins periodic alert evaluation.
func (e *Evaluator) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.stopChan = make(chan struct{})
	e.mu.Unlock()

	go e.runLoop()
	e.logger.Info("alert evaluator started", "interval", e.config.Interval)
}

// Stop halts the evaluator.
func (e *Evaluator) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopChan)
	e.mu.Unlock()
	e.logger.Info("alert evaluator stopped")
}

func (e *Evaluator) runLoop() {
	ticker := time.NewTicker(e.config.Interval)
	defer ticker.Stop()

	// Run immediately on start
	e.evaluate()

	for {
		select {
		case <-e.stopChan:
			return
		case <-ticker.C:
			e.evaluate()
		}
	}
}

func (e *Evaluator) evaluate() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if we're in a maintenance window
	windows, err := e.store.GetActiveAlertMaintenanceWindows(ctx)
	if err != nil {
		e.logger.Error("failed to get maintenance windows", "error", err)
	}
	inMaintenance := len(windows) > 0 && hasGlobalMaintenanceWindow(windows)

	// Check quiet hours
	settings, _ := e.store.GetAlertSettings(ctx)
	inQuietHours := isInQuietHours(settings)

	// Load all enabled rules
	rules, err := e.store.ListAlertRules(ctx)
	if err != nil {
		e.logger.Error("failed to list alert rules", "error", err)
		return
	}

	enabledRules := make([]storage.AlertRule, 0)
	for _, r := range rules {
		if r.Enabled {
			enabledRules = append(enabledRules, r)
		}
	}

	if len(enabledRules) == 0 {
		return // No rules to evaluate
	}

	// Load existing active alerts to avoid duplicates
	activeAlerts, err := e.store.ListActiveAlerts(ctx, storage.AlertFilters{})
	if err != nil {
		e.logger.Error("failed to list active alerts", "error", err)
		return
	}
	existingAlertKeys := make(map[string]storage.Alert)
	for _, a := range activeAlerts {
		key := alertKey(a.Type, a.Scope, a.DeviceSerial, a.AgentID, a.SiteID, a.TenantID)
		existingAlertKeys[key] = a
	}

	// Evaluate device rules
	if hasDeviceRules(enabledRules) {
		e.evaluateDeviceRules(ctx, enabledRules, existingAlertKeys, inMaintenance, inQuietHours)
	}

	// Evaluate agent rules
	if hasAgentRules(enabledRules) {
		e.evaluateAgentRules(ctx, enabledRules, existingAlertKeys, inMaintenance, inQuietHours)
	}

	// Auto-resolve cleared alerts
	e.autoResolveCleared(ctx, activeAlerts)
}

func (e *Evaluator) evaluateDeviceRules(ctx context.Context, rules []storage.AlertRule, existing map[string]storage.Alert, inMaint, inQuiet bool) {
	devices, err := e.store.ListAllDevices(ctx)
	if err != nil {
		e.logger.Error("failed to list devices", "error", err)
		return
	}

	for _, device := range devices {
		// Get latest metrics for the device
		metrics, _ := e.store.GetLatestMetrics(ctx, device.Serial)

		for _, rule := range rules {
			if rule.Scope != storage.AlertScopeDevice {
				continue
			}

			// Check scope filters - device uses AgentID for now
			if !matchesScope(rule, "", "", device.AgentID) {
				continue
			}

			triggered, title, message := evaluateDeviceRule(rule, device, metrics)
			if !triggered {
				continue
			}

			// Check if alert already exists
			// Key must match what's stored: includes AgentID for device alerts
			key := alertKey(rule.Type, rule.Scope, device.Serial, device.AgentID, "", "")
			if _, exists := existing[key]; exists {
				continue // Already alerting
			}

			// Check maintenance window for this device/scope
			if inMaint && !isCriticalAlertType(rule.Type) {
				continue
			}

			// Create the alert
			alert := &storage.Alert{
				RuleID:       rule.ID,
				Type:         rule.Type,
				Severity:     rule.Severity,
				Scope:        rule.Scope,
				Status:       storage.AlertStatusActive,
				AgentID:      device.AgentID,
				DeviceSerial: device.Serial,
				Title:        title,
				Message:      message,
				TriggeredAt:  time.Now().UTC(),
			}

			id, err := e.store.CreateAlert(ctx, alert)
			if err != nil {
				e.logger.Error("failed to create alert", "error", err, "rule", rule.Name, "device", device.Serial)
				continue
			}
			e.logger.Info("alert created", "id", id, "type", rule.Type, "device", device.Serial)
		}
	}
}

func (e *Evaluator) evaluateAgentRules(ctx context.Context, rules []storage.AlertRule, existing map[string]storage.Alert, inMaint, inQuiet bool) {
	agents, err := e.store.ListAgents(ctx)
	if err != nil {
		e.logger.Error("failed to list agents", "error", err)
		return
	}

	for _, agent := range agents {
		for _, rule := range rules {
			if rule.Scope != storage.AlertScopeAgent {
				continue
			}

			// Check scope filters
			if !matchesAgentScope(rule, agent) {
				continue
			}

			triggered, title, message := evaluateAgentRule(rule, agent)
			if !triggered {
				continue
			}

			// Check if alert already exists - use AgentID (stable UUID string)
			// Include TenantID to match the key format used when loading existing alerts
			key := alertKey(rule.Type, rule.Scope, "", agent.AgentID, "", agent.TenantID)
			if _, exists := existing[key]; exists {
				continue
			}

			if inMaint && !isCriticalAlertType(rule.Type) {
				continue
			}

			alert := &storage.Alert{
				RuleID:      rule.ID,
				Type:        rule.Type,
				Severity:    rule.Severity,
				Scope:       rule.Scope,
				Status:      storage.AlertStatusActive,
				TenantID:    agent.TenantID,
				AgentID:     agent.AgentID,
				Title:       title,
				Message:     message,
				TriggeredAt: time.Now().UTC(),
			}

			id, err := e.store.CreateAlert(ctx, alert)
			if err != nil {
				e.logger.Error("failed to create alert", "error", err, "rule", rule.Name, "agent", agent.AgentID)
				continue
			}
			e.logger.Info("alert created", "id", id, "type", rule.Type, "agent", agent.AgentID)
		}
	}
}

func (e *Evaluator) autoResolveCleared(ctx context.Context, activeAlerts []storage.Alert) {
	// For each active alert, check if the condition is still true
	// If not, auto-resolve it
	for _, alert := range activeAlerts {
		if alert.Status != storage.AlertStatusActive {
			continue
		}

		cleared := false

		switch alert.Scope {
		case storage.AlertScopeDevice:
			if alert.DeviceSerial != "" {
				cleared = e.isDeviceAlertCleared(ctx, alert)
			}
		case storage.AlertScopeAgent:
			if alert.AgentID != "" {
				cleared = e.isAgentAlertCleared(ctx, alert)
			}
		}

		if cleared {
			if err := e.store.ResolveAlert(ctx, alert.ID); err != nil {
				e.logger.Error("failed to auto-resolve alert", "error", err, "id", alert.ID)
			} else {
				e.logger.Info("alert auto-resolved", "id", alert.ID, "type", alert.Type)
			}
		}
	}
}

func (e *Evaluator) isDeviceAlertCleared(ctx context.Context, alert storage.Alert) bool {
	devices, _ := e.store.ListAllDevices(ctx)
	var device *storage.Device
	for _, d := range devices {
		if d.Serial == alert.DeviceSerial {
			device = d
			break
		}
	}
	if device == nil {
		return true // Device gone, clear the alert
	}

	metrics, _ := e.store.GetLatestMetrics(ctx, device.Serial)

	switch alert.Type {
	case storage.AlertTypeDeviceOffline:
		// Device is online if we have recent metrics
		if metrics != nil && time.Since(metrics.Timestamp) < 10*time.Minute {
			return true
		}
	case storage.AlertTypeSupplyLow, storage.AlertTypeSupplyCritical:
		// Check toner levels - if all above threshold, clear
		if metrics != nil {
			allOK := true
			for _, levelVal := range metrics.TonerLevels {
				level := tonerLevelToInt(levelVal)
				if level >= 0 && level < 20 { // Still low
					allOK = false
					break
				}
			}
			return allOK
		}
	case storage.AlertTypeDeviceError:
		// Device has no explicit Status field in common storage
		// Check if last seen is recent as proxy for "OK"
		if time.Since(device.LastSeen) < 10*time.Minute {
			return true
		}
	}

	return false
}

func (e *Evaluator) isAgentAlertCleared(ctx context.Context, alert storage.Alert) bool {
	agents, _ := e.store.ListAgents(ctx)
	for _, a := range agents {
		if a.AgentID == alert.AgentID {
			switch alert.Type {
			case storage.AlertTypeAgentOffline:
				if a.Status == "active" && time.Since(a.LastHeartbeat) < 5*time.Minute {
					return true
				}
			case storage.AlertTypeAgentOutdated:
				// Clear when version updated (would need version comparison)
				return false
			}
			return false
		}
	}
	return true // Agent gone, clear alert
}

// Helper functions

func alertKey(alertType storage.AlertType, scope storage.AlertScope, deviceSerial, agentID, siteID, tenantID string) string {
	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", alertType, scope, deviceSerial, agentID, siteID, tenantID)
}

func hasDeviceRules(rules []storage.AlertRule) bool {
	for _, r := range rules {
		if r.Scope == storage.AlertScopeDevice {
			return true
		}
	}
	return false
}

func hasAgentRules(rules []storage.AlertRule) bool {
	for _, r := range rules {
		if r.Scope == storage.AlertScopeAgent {
			return true
		}
	}
	return false
}

func hasGlobalMaintenanceWindow(windows []storage.AlertMaintenanceWindow) bool {
	for _, w := range windows {
		if w.Scope == storage.AlertScopeFleet || w.Scope == "" {
			return true
		}
	}
	return false
}

func isInQuietHours(settings *storage.AlertSettings) bool {
	if settings == nil || !settings.QuietHours.Enabled {
		return false
	}

	now := time.Now()

	// Parse start and end times (HH:MM format)
	startHour, startMin := parseTimeHHMM(settings.QuietHours.StartTime)
	endHour, endMin := parseTimeHHMM(settings.QuietHours.EndTime)

	currentMins := now.Hour()*60 + now.Minute()
	startMins := startHour*60 + startMin
	endMins := endHour*60 + endMin

	if startMins <= endMins {
		// Same day window (e.g., 09:00 - 17:00)
		return currentMins >= startMins && currentMins < endMins
	}
	// Wraps around midnight (e.g., 22:00 - 06:00)
	return currentMins >= startMins || currentMins < endMins
}

// parseTimeHHMM parses a time string in HH:MM format to hour and minute.
func parseTimeHHMM(s string) (hour, min int) {
	if s == "" {
		return 0, 0
	}
	fmt.Sscanf(s, "%d:%d", &hour, &min)
	return
}

func matchesScope(rule storage.AlertRule, tenantID, siteID, agentID string) bool {
	// Check tenant filter
	if len(rule.TenantIDs) > 0 {
		found := false
		for _, t := range rule.TenantIDs {
			if t == tenantID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check site filter
	if len(rule.SiteIDs) > 0 {
		found := false
		for _, s := range rule.SiteIDs {
			if s == siteID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check agent filter
	if len(rule.AgentIDs) > 0 {
		found := false
		for _, a := range rule.AgentIDs {
			if a == agentID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func matchesAgentScope(rule storage.AlertRule, agent *storage.Agent) bool {
	if len(rule.TenantIDs) > 0 {
		found := false
		for _, t := range rule.TenantIDs {
			if t == agent.TenantID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(rule.AgentIDs) > 0 {
		found := false
		for _, a := range rule.AgentIDs {
			if a == agent.AgentID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// tonerLevelToInt converts a toner level from interface{} to int.
// TonerLevels is map[string]interface{} so we need to handle type conversion.
func tonerLevelToInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return int(i)
		}
	}
	return -1 // Unknown
}

// getDeviceDisplayName returns a display name for the device.
// Falls back to serial if other identifiers are not available.
func getDeviceDisplayName(device *storage.Device) string {
	if device.Hostname != "" {
		return device.Hostname
	}
	if device.Model != "" {
		return device.Model
	}
	return device.Serial
}

func evaluateDeviceRule(rule storage.AlertRule, device *storage.Device, metrics *storage.MetricsSnapshot) (triggered bool, title, message string) {
	name := getDeviceDisplayName(device)

	switch rule.Type {
	case storage.AlertTypeDeviceOffline:
		// Device is offline if no recent metrics
		if metrics == nil || time.Since(metrics.Timestamp) > 10*time.Minute {
			return true,
				fmt.Sprintf("Device Offline: %s", name),
				fmt.Sprintf("Device %s (%s) has not reported metrics recently", name, device.Serial)
		}

	case storage.AlertTypeSupplyLow:
		if metrics != nil {
			for color, levelVal := range metrics.TonerLevels {
				level := tonerLevelToInt(levelVal)
				threshold := rule.Threshold
				if threshold == 0 {
					threshold = 20 // Default 20%
				}
				if level >= 0 && float64(level) <= threshold {
					return true,
						fmt.Sprintf("Low %s Toner: %s", strings.ToTitle(color), name),
						fmt.Sprintf("%s toner is at %d%% on %s", strings.ToTitle(color), level, name)
				}
			}
		}

	case storage.AlertTypeSupplyCritical:
		if metrics != nil {
			for color, levelVal := range metrics.TonerLevels {
				level := tonerLevelToInt(levelVal)
				threshold := rule.Threshold
				if threshold == 0 {
					threshold = 5 // Default 5%
				}
				if level >= 0 && float64(level) <= threshold {
					return true,
						fmt.Sprintf("Critical %s Toner: %s", strings.ToTitle(color), name),
						fmt.Sprintf("%s toner is critically low at %d%% on %s", strings.ToTitle(color), level, name)
				}
			}
		}

	case storage.AlertTypeDeviceError:
		// Check status messages for errors
		if len(device.StatusMessages) > 0 {
			for _, msg := range device.StatusMessages {
				// Check for error-like status messages
				lowerMsg := strings.ToLower(msg)
				if strings.Contains(lowerMsg, "error") || strings.Contains(lowerMsg, "jam") || strings.Contains(lowerMsg, "fault") {
					return true,
						fmt.Sprintf("Device Error: %s", name),
						fmt.Sprintf("Device %s is reporting: %s", name, msg)
				}
			}
		}

	case storage.AlertTypeUsageHigh:
		if metrics != nil {
			threshold := rule.Threshold
			if threshold == 0 {
				threshold = 1000 // Default 1000 pages/day
			}
			// This would need historical data to calculate daily usage
			// For now, check if page count is high
			if float64(metrics.PageCount) > threshold*30 { // Monthly proxy
				return true,
					fmt.Sprintf("High Usage: %s", name),
					fmt.Sprintf("Device %s has high page count: %d", name, metrics.PageCount)
			}
		}
	}

	return false, "", ""
}

func evaluateAgentRule(rule storage.AlertRule, agent *storage.Agent) (triggered bool, title, message string) {
	switch rule.Type {
	case storage.AlertTypeAgentOffline:
		duration := rule.DurationMinutes
		if duration == 0 {
			duration = 5 // Default 5 minutes
		}
		if agent.Status != "active" || time.Since(agent.LastHeartbeat) > time.Duration(duration)*time.Minute {
			return true,
				fmt.Sprintf("Agent Offline: %s", agent.Name),
				fmt.Sprintf("Agent %s has not sent a heartbeat in %d minutes", agent.Name, int(time.Since(agent.LastHeartbeat).Minutes()))
		}

	case storage.AlertTypeAgentOutdated:
		// This would need version comparison logic
		// For now, just a placeholder
		return false, "", ""

	case storage.AlertTypeAgentStorageFull:
		// This would need disk space metrics from the agent
		return false, "", ""
	}

	return false, "", ""
}

// isCriticalAlertType returns true if the alert type should bypass maintenance windows.
func isCriticalAlertType(t storage.AlertType) bool {
	switch t {
	case storage.AlertTypeSupplyCritical, storage.AlertTypeDeviceError, storage.AlertTypeSiteOutage, storage.AlertTypeFleetMassOutage:
		return true
	}
	return false
}
