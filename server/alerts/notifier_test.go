package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"printmaster/server/storage"
)

func init() {
	// Allow localhost URLs for testing webhook functionality
	SetAllowTestWebhooks(true)
}

// mockNotifierStore implements NotifierStore for testing.
type mockNotifierStore struct {
	channels map[int64]*storage.NotificationChannel
	rules    map[int64]*storage.AlertRule
	settings *storage.AlertSettings
	mu       sync.Mutex
}

func newMockNotifierStore() *mockNotifierStore {
	return &mockNotifierStore{
		channels: make(map[int64]*storage.NotificationChannel),
		rules:    make(map[int64]*storage.AlertRule),
		settings: &storage.AlertSettings{},
	}
}

func (m *mockNotifierStore) GetNotificationChannel(ctx context.Context, id int64) (*storage.NotificationChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.channels[id]; ok {
		return ch, nil
	}
	return nil, nil
}

func (m *mockNotifierStore) ListNotificationChannels(ctx context.Context) ([]storage.NotificationChannel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]storage.NotificationChannel, 0, len(m.channels))
	for _, ch := range m.channels {
		result = append(result, *ch)
	}
	return result, nil
}

func (m *mockNotifierStore) GetAlertRule(ctx context.Context, id int64) (*storage.AlertRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rules[id]; ok {
		return r, nil
	}
	return nil, nil
}

func (m *mockNotifierStore) UpdateAlertNotificationStatus(ctx context.Context, id int64, sent int, lastNotified time.Time) error {
	return nil
}

func (m *mockNotifierStore) GetAlertSettings(ctx context.Context) (*storage.AlertSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings, nil
}

func TestNotifier_SendWebhook(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte
	var receivedContentType string

	// Create a test HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	// Add a webhook channel with JSON config
	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Test Webhook",
		Type:             storage.ChannelTypeWebhook,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"url": "` + server.URL + `"}`,
	}

	// Add a rule that references the channel
	store.rules[1] = &storage.AlertRule{
		ID:         1,
		Name:       "Test Rule",
		Enabled:    true,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1, // Link to the rule
		Type:     storage.AlertTypeDeviceOffline,
		Severity: storage.AlertSeverityWarning,
		Scope:    storage.AlertScopeDevice,
		Title:    "Printer Offline",
		Message:  "Device DEV001 is offline",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}

	if len(receivedPayload) == 0 {
		t.Error("expected payload to be sent")
	}
}

func TestNotifier_NoRuleID(t *testing.T) {
	t.Parallel()

	store := newMockNotifierStore()
	notifier := NewNotifier(store, NotifierConfig{})

	// Alert with no RuleID should be skipped
	alert := &storage.Alert{
		ID:       1,
		RuleID:   0, // No rule
		Type:     storage.AlertTypeDeviceOffline,
		Severity: storage.AlertSeverityWarning,
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}
}

func TestNotifier_DisabledChannel(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	// Add a disabled channel
	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Disabled Webhook",
		Type:             storage.ChannelTypeWebhook,
		Enabled:          false, // Disabled
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Severity: storage.AlertSeverityWarning,
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected 0 calls (channel disabled), got %d", callCount)
	}
}

func TestNotifier_MultipleChannels(t *testing.T) {
	t.Parallel()

	callCount := 0
	mu := sync.Mutex{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	// Add multiple channels
	for i := int64(1); i <= 3; i++ {
		store.channels[i] = &storage.NotificationChannel{
			ID:               i,
			Name:             "Channel",
			Type:             storage.ChannelTypeWebhook,
			Enabled:          true,
			MinSeverity:      storage.AlertSeverityInfo,
			RateLimitPerHour: 100,
			ConfigJSON:       `{"url": "` + server.URL + `"}`,
		}
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1, 2, 3},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Severity: storage.AlertSeverityWarning,
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	if callCount != 3 {
		t.Errorf("expected 3 calls (one per channel), got %d", callCount)
	}
}

func TestNotifier_SlackPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Slack Test",
		Type:             storage.ChannelTypeSlack,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"webhook_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeDeviceOffline,
		Severity: storage.AlertSeverityCritical,
		Title:    "Critical Alert",
		Message:  "Something critical happened",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected Slack payload to be sent")
	}

	// Check that payload contains Slack-specific fields
	if !contains(payload, "attachments") {
		t.Error("expected Slack payload to contain 'attachments'")
	}
}

func TestNotifier_TeamsPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Teams Test",
		Type:             storage.ChannelTypeTeams,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"webhook_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeAgentOffline,
		Severity: storage.AlertSeverityWarning,
		Title:    "Agent Offline",
		Message:  "Agent went offline",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected Teams payload to be sent")
	}

	// Check that payload contains Teams-specific fields
	if !contains(payload, "@type") {
		t.Error("expected Teams payload to contain '@type'")
	}
}

func TestNotifier_PagerDutyPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "PagerDuty Test",
		Type:             storage.ChannelTypePagerDuty,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"routing_key": "test-routing-key", "api_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:           1,
		RuleID:       1,
		Type:         storage.AlertTypeDeviceError,
		Severity:     storage.AlertSeverityCritical,
		Title:        "Device Error",
		Message:      "Device has critical error",
		DeviceSerial: "SN12345",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected PagerDuty payload to be sent")
	}

	// Check that payload contains PagerDuty-specific fields
	if !contains(payload, "routing_key") {
		t.Error("expected PagerDuty payload to contain 'routing_key'")
	}
	if !contains(payload, "event_action") {
		t.Error("expected PagerDuty payload to contain 'event_action'")
	}
}

func TestNotifier_DiscordPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Discord Test",
		Type:             storage.ChannelTypeDiscord,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"webhook_url": "` + server.URL + `", "username": "PrintMaster"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeDeviceOffline,
		Severity: storage.AlertSeverityWarning,
		Title:    "Device Offline",
		Message:  "Device went offline",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected Discord payload to be sent")
	}

	// Check that payload contains Discord-specific fields
	if !contains(payload, "embeds") {
		t.Error("expected Discord payload to contain 'embeds'")
	}
	if !contains(payload, "username") {
		t.Error("expected Discord payload to contain 'username'")
	}
}

func TestNotifier_TelegramPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Telegram Test",
		Type:             storage.ChannelTypeTelegram,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"bot_token": "test-token", "chat_id": "-1001234567890", "api_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeTonerLow,
		Severity: storage.AlertSeverityInfo,
		Title:    "Low Toner",
		Message:  "Toner is running low",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected Telegram payload to be sent")
	}

	// Check that payload contains Telegram-specific fields
	if !contains(payload, "chat_id") {
		t.Error("expected Telegram payload to contain 'chat_id'")
	}
	if !contains(payload, "parse_mode") {
		t.Error("expected Telegram payload to contain 'parse_mode'")
	}
}

func TestNotifier_PushoverPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedPayload = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Pushover Test",
		Type:             storage.ChannelTypePushover,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"user_key": "test-user-key", "api_token": "test-api-token", "api_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeDeviceError,
		Severity: storage.AlertSeverityCritical,
		Title:    "Device Error",
		Message:  "Critical device error",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	payload := string(receivedPayload)
	if len(payload) == 0 {
		t.Error("expected Pushover payload to be sent")
	}

	// Check that payload contains Pushover-specific fields
	if !contains(payload, "token") {
		t.Error("expected Pushover payload to contain 'token'")
	}
	if !contains(payload, "user") {
		t.Error("expected Pushover payload to contain 'user'")
	}
	if !contains(payload, "priority") {
		t.Error("expected Pushover payload to contain 'priority'")
	}
}

func TestNotifier_NtfyPayload(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	var receivedBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "ntfy Test",
		Type:             storage.ChannelTypeNtfy,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		ConfigJSON:       `{"topic": "test-topic", "server_url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	alert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeAgentOffline,
		Severity: storage.AlertSeverityWarning,
		Title:    "Agent Offline",
		Message:  "Agent went offline",
	}

	err := notifier.NotifyForAlert(context.Background(), alert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	if len(receivedBody) == 0 {
		t.Error("expected ntfy payload to be sent")
	}

	// Check that headers contain ntfy-specific fields
	if receivedHeaders.Get("Title") == "" {
		t.Error("expected ntfy Title header to be set")
	}
	if receivedHeaders.Get("Priority") == "" {
		t.Error("expected ntfy Priority header to be set")
	}
	if receivedHeaders.Get("Tags") == "" {
		t.Error("expected ntfy Tags header to be set")
	}
}

func TestNotifier_QuietHours(t *testing.T) {
	t.Parallel()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := newMockNotifierStore()

	// Enable quiet hours using start == end to indicate "always active"
	// This special case is handled in isInQuietHours() for full 24h coverage
	store.settings = &storage.AlertSettings{
		QuietHours: storage.QuietHoursConfig{
			Enabled:       true,
			StartTime:     "00:00",
			EndTime:       "00:00",
			Timezone:      "UTC",
			AllowCritical: true,
		},
	}

	store.channels[1] = &storage.NotificationChannel{
		ID:               1,
		Name:             "Test",
		Type:             storage.ChannelTypeWebhook,
		Enabled:          true,
		MinSeverity:      storage.AlertSeverityInfo,
		RateLimitPerHour: 100,
		UseQuietHours:    true,
		ConfigJSON:       `{"url": "` + server.URL + `"}`,
	}

	store.rules[1] = &storage.AlertRule{
		ID:         1,
		ChannelIDs: []int64{1},
	}

	notifier := NewNotifier(store, NotifierConfig{})

	// Non-critical alert should be blocked
	warningAlert := &storage.Alert{
		ID:       1,
		RuleID:   1,
		Type:     storage.AlertTypeDeviceOffline,
		Severity: storage.AlertSeverityWarning,
	}

	err := notifier.NotifyForAlert(context.Background(), warningAlert)
	if err != nil {
		t.Fatalf("NotifyForAlert() error = %v", err)
	}

	if callCount != 0 {
		t.Errorf("expected 0 calls (quiet hours), got %d", callCount)
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
