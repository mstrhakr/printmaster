// Package alerts provides notification dispatch functionality for alerts.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"printmaster/server/storage"
)

// NotifierConfig configures the notification dispatcher.
type NotifierConfig struct {
	// Logger for notification events
	Logger *slog.Logger

	// HTTP client for webhook/Slack notifications
	HTTPClient *http.Client

	// Max retries for failed notifications
	MaxRetries int

	// Retry delay between attempts
	RetryDelay time.Duration
}

// NotifierStore defines the storage operations needed by the notifier.
type NotifierStore interface {
	// Get notification channels
	GetNotificationChannel(ctx context.Context, id int64) (*storage.NotificationChannel, error)
	ListNotificationChannels(ctx context.Context) ([]storage.NotificationChannel, error)

	// Get alert rules to find associated channels
	GetAlertRule(ctx context.Context, id int64) (*storage.AlertRule, error)

	// Update notification tracking on alerts
	UpdateAlertNotificationStatus(ctx context.Context, id int64, sent int, lastNotified time.Time) error

	// Get settings for rate limits, quiet hours, etc.
	GetAlertSettings(ctx context.Context) (*storage.AlertSettings, error)
}

// Notifier dispatches notifications for triggered alerts.
type Notifier struct {
	store  NotifierStore
	config NotifierConfig
	logger *slog.Logger
	client *http.Client

	mu           sync.Mutex
	lastNotified map[string]time.Time // Track last notification per channel+alert combo
}

// NewNotifier creates a new notification dispatcher.
func NewNotifier(store NotifierStore, config NotifierConfig) *Notifier {
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 5 * time.Second
	}

	return &Notifier{
		store:        store,
		config:       config,
		logger:       logger,
		client:       client,
		lastNotified: make(map[string]time.Time),
	}
}

// NotifyForAlert sends notifications for a triggered alert.
// It looks up the associated rule's channels and dispatches to each.
func (n *Notifier) NotifyForAlert(ctx context.Context, alert *storage.Alert) error {
	// Skip if alert has no associated rule
	if alert.RuleID == 0 {
		return nil
	}

	// Check quiet hours
	settings, _ := n.store.GetAlertSettings(ctx)
	if isInQuietHours(settings) && !isCriticalAlertType(alert.Type) {
		n.logger.Debug("skipping notification during quiet hours", "alert_id", alert.ID, "type", alert.Type)
		return nil
	}

	// Get the rule to find associated channels
	rule, err := n.store.GetAlertRule(ctx, alert.RuleID)
	if err != nil {
		return fmt.Errorf("failed to get alert rule: %w", err)
	}

	if len(rule.ChannelIDs) == 0 {
		n.logger.Debug("no notification channels configured for rule", "rule_id", rule.ID)
		return nil
	}

	// Send to each channel
	var notificationsSent int
	var lastErr error

	for _, channelID := range rule.ChannelIDs {
		channel, err := n.store.GetNotificationChannel(ctx, channelID)
		if err != nil {
			n.logger.Warn("failed to get notification channel", "channel_id", channelID, "error", err)
			continue
		}

		if !channel.Enabled {
			continue
		}

		// Check rate limiting (convert hourly limit to minutes: 60/limit)
		rateLimitMins := 0
		if channel.RateLimitPerHour > 0 {
			rateLimitMins = 60 / channel.RateLimitPerHour
		}
		if !n.shouldNotify(channel.ID, alert.ID, rateLimitMins) {
			n.logger.Debug("rate limited notification", "channel_id", channel.ID, "alert_id", alert.ID)
			continue
		}

		// Dispatch based on channel type
		err = n.dispatchToChannel(ctx, channel, alert)
		if err != nil {
			n.logger.Error("failed to send notification", "channel", channel.Name, "type", channel.Type, "error", err)
			lastErr = err
			continue
		}

		notificationsSent++
		n.recordNotification(channel.ID, alert.ID)
		n.logger.Info("notification sent", "channel", channel.Name, "type", channel.Type, "alert_id", alert.ID)
	}

	// Update alert notification tracking
	if notificationsSent > 0 {
		now := time.Now().UTC()
		_ = n.store.UpdateAlertNotificationStatus(ctx, alert.ID, alert.NotificationsSent+notificationsSent, now)
	}

	return lastErr
}

func (n *Notifier) shouldNotify(channelID, alertID int64, rateLimitMins int) bool {
	if rateLimitMins <= 0 {
		return true
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	key := fmt.Sprintf("%d:%d", channelID, alertID)
	lastTime, exists := n.lastNotified[key]
	if !exists {
		return true
	}

	return time.Since(lastTime) >= time.Duration(rateLimitMins)*time.Minute
}

func (n *Notifier) recordNotification(channelID, alertID int64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	key := fmt.Sprintf("%d:%d", channelID, alertID)
	n.lastNotified[key] = time.Now()
}

func (n *Notifier) dispatchToChannel(ctx context.Context, channel *storage.NotificationChannel, alert *storage.Alert) error {
	// Parse config from JSON
	var config map[string]interface{}
	if channel.ConfigJSON != "" {
		if err := json.Unmarshal([]byte(channel.ConfigJSON), &config); err != nil {
			return fmt.Errorf("failed to parse channel config: %w", err)
		}
	}

	switch channel.Type {
	case storage.ChannelTypeEmail:
		return n.sendEmail(ctx, config, alert)
	case storage.ChannelTypeWebhook:
		return n.sendWebhook(ctx, config, alert)
	case storage.ChannelTypeSlack:
		return n.sendSlack(ctx, config, alert)
	case storage.ChannelTypeTeams:
		return n.sendMSTeams(ctx, config, alert)
	case storage.ChannelTypePagerDuty:
		return n.sendPagerDuty(ctx, config, alert)
	default:
		return fmt.Errorf("unsupported notification type: %s", channel.Type)
	}
}

// Email notification
func (n *Notifier) sendEmail(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("email channel has no configuration")
	}

	host, _ := config["smtp_host"].(string)
	portFloat, _ := config["smtp_port"].(float64)
	port := int(portFloat)
	username, _ := config["smtp_username"].(string)
	password, _ := config["smtp_password"].(string)
	from, _ := config["from_address"].(string)
	toList, _ := config["to_addresses"].([]interface{})

	if host == "" || from == "" || len(toList) == 0 {
		return fmt.Errorf("incomplete email configuration")
	}

	// Build recipient list
	var to []string
	for _, addr := range toList {
		if s, ok := addr.(string); ok {
			to = append(to, s)
		}
	}

	// Build message
	subject := fmt.Sprintf("[%s] %s", strings.ToUpper(string(alert.Severity)), alert.Title)
	body := fmt.Sprintf(`Alert: %s
Severity: %s
Scope: %s
Time: %s

%s

---
This is an automated alert from PrintMaster.
`, alert.Title, alert.Severity, alert.Scope, alert.TriggeredAt.Format(time.RFC3339), alert.Message)

	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, strings.Join(to, ", "), subject, body))

	addr := fmt.Sprintf("%s:%d", host, port)

	// Send with retry
	var lastErr error
	for i := 0; i < n.config.MaxRetries; i++ {
		var auth smtp.Auth
		if username != "" {
			auth = smtp.PlainAuth("", username, password, host)
		}

		err := smtp.SendMail(addr, auth, from, to, msg)
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(n.config.RetryDelay)
	}

	return fmt.Errorf("email send failed after %d attempts: %w", n.config.MaxRetries, lastErr)
}

// Webhook notification
func (n *Notifier) sendWebhook(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("webhook channel has no configuration")
	}

	url, _ := config["url"].(string)
	if url == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	// Get optional headers
	headers := make(map[string]string)
	if h, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	}

	// Build payload
	payload := map[string]interface{}{
		"alert_id":      alert.ID,
		"type":          alert.Type,
		"severity":      alert.Severity,
		"scope":         alert.Scope,
		"status":        alert.Status,
		"title":         alert.Title,
		"message":       alert.Message,
		"triggered_at":  alert.TriggeredAt.Format(time.RFC3339),
		"device_serial": alert.DeviceSerial,
		"agent_id":      alert.AgentID,
		"site_id":       alert.SiteID,
		"tenant_id":     alert.TenantID,
	}

	return n.postJSON(ctx, url, headers, payload)
}

// Slack notification
func (n *Notifier) sendSlack(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("slack channel has no configuration")
	}

	webhookURL, _ := config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("slack webhook URL not configured")
	}

	slackChannel, _ := config["channel"].(string)
	username, _ := config["username"].(string)
	if username == "" {
		username = "PrintMaster"
	}

	// Pick color based on severity
	color := "#17a2b8" // info - blue
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		color = "#dc3545" // red
	case storage.AlertSeverityWarning:
		color = "#ffc107" // yellow
	}

	// Build Slack message
	payload := map[string]interface{}{
		"username": username,
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"title":  alert.Title,
				"text":   alert.Message,
				"footer": fmt.Sprintf("PrintMaster Alert | %s", alert.Scope),
				"ts":     alert.TriggeredAt.Unix(),
				"fields": []map[string]interface{}{
					{"title": "Severity", "value": string(alert.Severity), "short": true},
					{"title": "Type", "value": string(alert.Type), "short": true},
				},
			},
		},
	}

	if slackChannel != "" {
		payload["channel"] = slackChannel
	}

	return n.postJSON(ctx, webhookURL, nil, payload)
}

// MS Teams notification
func (n *Notifier) sendMSTeams(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("teams channel has no configuration")
	}

	webhookURL, _ := config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("teams webhook URL not configured")
	}

	// Pick theme color based on severity
	themeColor := "0078D7" // info - blue
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		themeColor = "DC3545" // red
	case storage.AlertSeverityWarning:
		themeColor = "FFC107" // yellow
	}

	// Build Teams Adaptive Card
	payload := map[string]interface{}{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"themeColor": themeColor,
		"summary":    alert.Title,
		"sections": []map[string]interface{}{
			{
				"activityTitle":    alert.Title,
				"activitySubtitle": fmt.Sprintf("%s | %s", alert.Scope, alert.Severity),
				"text":             alert.Message,
				"facts": []map[string]interface{}{
					{"name": "Severity", "value": string(alert.Severity)},
					{"name": "Type", "value": string(alert.Type)},
					{"name": "Time", "value": alert.TriggeredAt.Format(time.RFC3339)},
				},
			},
		},
	}

	return n.postJSON(ctx, webhookURL, nil, payload)
}

// PagerDuty notification
func (n *Notifier) sendPagerDuty(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("pagerduty channel has no configuration")
	}

	routingKey, _ := config["routing_key"].(string)
	if routingKey == "" {
		return fmt.Errorf("pagerduty routing key not configured")
	}

	// Map severity to PagerDuty severity
	pdSeverity := "info"
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		pdSeverity = "critical"
	case storage.AlertSeverityWarning:
		pdSeverity = "warning"
	}

	// Build PagerDuty Events API v2 payload
	payload := map[string]interface{}{
		"routing_key":  routingKey,
		"event_action": "trigger",
		"dedup_key":    fmt.Sprintf("printmaster-%d", alert.ID),
		"payload": map[string]interface{}{
			"summary":   alert.Title,
			"severity":  pdSeverity,
			"source":    "PrintMaster",
			"component": string(alert.Scope),
			"group":     string(alert.Type),
			"class":     "alert",
			"custom_details": map[string]interface{}{
				"message":       alert.Message,
				"device_serial": alert.DeviceSerial,
				"agent_id":      alert.AgentID,
				"triggered_at":  alert.TriggeredAt.Format(time.RFC3339),
			},
		},
	}

	return n.postJSON(ctx, "https://events.pagerduty.com/v2/enqueue", nil, payload)
}

// postJSON posts a JSON payload to the given URL with retry logic.
func (n *Notifier) postJSON(ctx context.Context, url string, headers map[string]string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	var lastErr error
	for i := 0; i < n.config.MaxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := n.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(n.config.RetryDelay)
			continue
		}

		// Read and discard body
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		time.Sleep(n.config.RetryDelay)
	}

	return fmt.Errorf("request failed after %d attempts: %w", n.config.MaxRetries, lastErr)
}
