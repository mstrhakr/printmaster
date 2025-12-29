// Package alerts provides notification dispatch functionality for alerts.
package alerts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"printmaster/server/storage"
)

// allowTestWebhooks allows localhost webhook URLs for testing.
// Set to true in test initialization code.
var allowTestWebhooks bool

// SetAllowTestWebhooks sets whether localhost URLs are allowed for webhook testing.
// This should only be called during test initialization.
func SetAllowTestWebhooks(allow bool) {
	allowTestWebhooks = allow
}

// isAllowedWebhookURL validates webhook URLs to prevent SSRF attacks.
// Only allows http/https schemes and blocks requests to private/internal networks.
func isAllowedWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow http and https schemes
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", parsed.Scheme)
	}

	// Get the hostname
	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Block localhost and loopback addresses (unless in test mode)
	if !allowTestWebhooks {
		if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
			return fmt.Errorf("localhost URLs are not allowed for webhooks")
		}

		// Resolve hostname to check for private IPs
		ips, err := net.LookupIP(hostname)
		if err != nil {
			// If DNS resolution fails, allow it (could be a valid external service)
			// The HTTP request will fail anyway if unreachable
			return nil
		}

		for _, ip := range ips {
			if isPrivateIP(ip) {
				return fmt.Errorf("webhook URLs cannot target private/internal networks")
			}
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private/internal range.
func isPrivateIP(ip net.IP) bool {
	// Check for loopback
	if ip.IsLoopback() {
		return true
	}

	// Check for link-local
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// Check for private ranges (RFC 1918 and RFC 4193)
	privateRanges := []string{
		"10.0.0.0/8",     // Class A private
		"172.16.0.0/12",  // Class B private
		"192.168.0.0/16", // Class C private
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
		"169.254.0.0/16", // Link-local (APIPA)
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// sanitizeEmailHeader removes CR and LF characters from email header values
// to prevent email header injection attacks.
func sanitizeEmailHeader(s string) string {
	// Replace all CR and LF characters with spaces
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	// Also handle null bytes which could be used for attacks
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.TrimSpace(s)
}

// validateEmailAddress performs basic email address validation.
func validateEmailAddress(email string) error {
	// Check for header injection attempts
	if strings.ContainsAny(email, "\r\n") {
		return fmt.Errorf("email address contains invalid characters")
	}
	// Basic format check
	if !strings.Contains(email, "@") || len(email) < 3 {
		return fmt.Errorf("invalid email address format")
	}
	return nil
}

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

// TestChannel sends a test notification to a channel without any rate limiting or quiet hours checks.
func (n *Notifier) TestChannel(ctx context.Context, channel *storage.NotificationChannel, alert *storage.Alert) error {
	return n.dispatchToChannel(ctx, channel, alert)
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
	case storage.ChannelTypeDiscord:
		return n.sendDiscord(ctx, config, alert)
	case storage.ChannelTypeTelegram:
		return n.sendTelegram(ctx, config, alert)
	case storage.ChannelTypePushover:
		return n.sendPushover(ctx, config, alert)
	case storage.ChannelTypeNtfy:
		return n.sendNtfy(ctx, config, alert)
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

	// Validate and sanitize from address to prevent header injection
	if err := validateEmailAddress(from); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	from = sanitizeEmailHeader(from)

	// Build recipient list with validation
	var to []string
	for _, addr := range toList {
		if s, ok := addr.(string); ok {
			if err := validateEmailAddress(s); err != nil {
				n.logger.Warn("skipping invalid recipient address", "address", s, "error", err)
				continue
			}
			to = append(to, sanitizeEmailHeader(s))
		}
	}

	if len(to) == 0 {
		return fmt.Errorf("no valid recipient addresses")
	}

	// Build message with sanitized headers to prevent header injection
	subject := sanitizeEmailHeader(fmt.Sprintf("[%s] %s", strings.ToUpper(string(alert.Severity)), alert.Title))
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

	// Allow custom API URL for testing
	apiURL := "https://events.pagerduty.com/v2/enqueue"
	if customURL, ok := config["api_url"].(string); ok && customURL != "" {
		apiURL = customURL
	}

	return n.postJSON(ctx, apiURL, nil, payload)
}

// postJSON posts a JSON payload to the given URL with retry logic.
func (n *Notifier) postJSON(ctx context.Context, url string, headers map[string]string, payload interface{}) error {
	// Validate URL to prevent SSRF attacks
	if err := isAllowedWebhookURL(url); err != nil {
		return fmt.Errorf("webhook URL validation failed: %w", err)
	}

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

// Discord notification via webhook
func (n *Notifier) sendDiscord(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("discord channel has no configuration")
	}

	webhookURL, _ := config["webhook_url"].(string)
	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL not configured")
	}

	username, _ := config["username"].(string)
	if username == "" {
		username = "PrintMaster"
	}

	// Pick color based on severity (Discord uses decimal color)
	color := 1752220 // info - blue
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		color = 15158332 // red
	case storage.AlertSeverityWarning:
		color = 16776960 // yellow
	}

	// Build Discord embed payload
	payload := map[string]interface{}{
		"username": username,
		"embeds": []map[string]interface{}{
			{
				"title":       alert.Title,
				"description": alert.Message,
				"color":       color,
				"timestamp":   alert.TriggeredAt.Format(time.RFC3339),
				"footer": map[string]string{
					"text": fmt.Sprintf("PrintMaster | %s", alert.Scope),
				},
				"fields": []map[string]interface{}{
					{"name": "Severity", "value": string(alert.Severity), "inline": true},
					{"name": "Type", "value": string(alert.Type), "inline": true},
				},
			},
		},
	}

	return n.postJSON(ctx, webhookURL, nil, payload)
}

// Telegram notification via Bot API
func (n *Notifier) sendTelegram(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("telegram channel has no configuration")
	}

	botToken, _ := config["bot_token"].(string)
	chatID, _ := config["chat_id"].(string)
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token and chat_id are required")
	}

	// Build message text with formatting
	severityEmoji := "ℹ️"
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		severityEmoji = "🚨"
	case storage.AlertSeverityWarning:
		severityEmoji = "⚠️"
	}

	text := fmt.Sprintf("%s *%s*\n\n%s\n\n*Severity:* %s\n*Type:* %s\n*Scope:* %s\n*Time:* %s",
		severityEmoji, escapeMarkdownV2(alert.Title),
		escapeMarkdownV2(alert.Message),
		escapeMarkdownV2(string(alert.Severity)),
		escapeMarkdownV2(string(alert.Type)),
		escapeMarkdownV2(alert.Scope),
		escapeMarkdownV2(alert.TriggeredAt.Format(time.RFC3339)))

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "MarkdownV2",
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	if customURL, ok := config["api_url"].(string); ok && customURL != "" {
		apiURL = customURL
	}

	return n.postJSON(ctx, apiURL, nil, payload)
}

// escapeMarkdownV2 escapes special characters for Telegram MarkdownV2
func escapeMarkdownV2(s string) string {
	specialChars := []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"}
	result := s
	for _, char := range specialChars {
		result = strings.ReplaceAll(result, char, "\\"+char)
	}
	return result
}

// Pushover notification for iOS/Android push
func (n *Notifier) sendPushover(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("pushover channel has no configuration")
	}

	userKey, _ := config["user_key"].(string)
	apiToken, _ := config["api_token"].(string)
	if userKey == "" || apiToken == "" {
		return fmt.Errorf("pushover user_key and api_token are required")
	}

	// Map severity to Pushover priority (-2 to 2)
	priority := 0 // normal
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		priority = 1 // high priority
	case storage.AlertSeverityWarning:
		priority = 0 // normal
	case storage.AlertSeverityInfo:
		priority = -1 // low priority
	}

	payload := map[string]interface{}{
		"token":    apiToken,
		"user":     userKey,
		"title":    alert.Title,
		"message":  alert.Message,
		"priority": priority,
		"url":      "", // Could add a link to the alert in UI
	}

	// Optional device targeting
	if device, ok := config["device"].(string); ok && device != "" {
		payload["device"] = device
	}

	// Optional sound
	if sound, ok := config["sound"].(string); ok && sound != "" {
		payload["sound"] = sound
	}

	apiURL := "https://api.pushover.net/1/messages.json"
	if customURL, ok := config["api_url"].(string); ok && customURL != "" {
		apiURL = customURL
	}

	return n.postJSON(ctx, apiURL, nil, payload)
}

// ntfy notification (self-hosted or ntfy.sh)
func (n *Notifier) sendNtfy(ctx context.Context, config map[string]interface{}, alert *storage.Alert) error {
	if config == nil {
		return fmt.Errorf("ntfy channel has no configuration")
	}

	topic, _ := config["topic"].(string)
	if topic == "" {
		return fmt.Errorf("ntfy topic is required")
	}

	serverURL, _ := config["server_url"].(string)
	if serverURL == "" {
		serverURL = "https://ntfy.sh" // default to public ntfy.sh
	}

	// Map severity to ntfy priority
	priority := "default"
	tags := "printer"
	switch alert.Severity {
	case storage.AlertSeverityCritical:
		priority = "urgent"
		tags = "rotating_light,printer"
	case storage.AlertSeverityWarning:
		priority = "high"
		tags = "warning,printer"
	}

	// Build the ntfy publish URL
	publishURL := fmt.Sprintf("%s/%s", strings.TrimSuffix(serverURL, "/"), topic)

	headers := map[string]string{
		"Title":    alert.Title,
		"Priority": priority,
		"Tags":     tags,
	}

	// Add optional click action
	if clickURL, ok := config["click_url"].(string); ok && clickURL != "" {
		headers["Click"] = clickURL
	}

	// Add optional authentication
	if token, ok := config["access_token"].(string); ok && token != "" {
		headers["Authorization"] = "Bearer " + token
	} else if user, ok := config["username"].(string); ok && user != "" {
		if pass, ok := config["password"].(string); ok {
			headers["Authorization"] = "Basic " + basicAuth(user, pass)
		}
	}

	// ntfy accepts plain text body
	message := fmt.Sprintf("%s\n\nSeverity: %s | Type: %s | Scope: %s",
		alert.Message, alert.Severity, alert.Type, alert.Scope)

	return n.postText(ctx, publishURL, headers, message)
}

// basicAuth returns base64-encoded basic auth string
func basicAuth(user, pass string) string {
	auth := user + ":" + pass
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// postText posts plain text to the given URL with retry logic.
func (n *Notifier) postText(ctx context.Context, url string, headers map[string]string, body string) error {
	// Validate URL to prevent SSRF attacks
	if err := isAllowedWebhookURL(url); err != nil {
		return fmt.Errorf("webhook URL validation failed: %w", err)
	}

	var lastErr error
	for i := 0; i < n.config.MaxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "text/plain")
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
