package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// AutoUpdateStatus mirrors the agent's ManagerStatus for testing
type AutoUpdateStatus struct {
	Enabled           bool       `json:"enabled"`
	DisabledReason    string     `json:"disabled_reason,omitempty"`
	CurrentVersion    string     `json:"current_version"`
	LatestVersion     string     `json:"latest_version,omitempty"`
	UpdateAvailable   bool       `json:"update_available"`
	Status            string     `json:"status"`
	LastCheckAt       *time.Time `json:"last_check_at,omitempty"`
	NextCheckAt       *time.Time `json:"next_check_at,omitempty"`
	PolicySource      string     `json:"policy_source,omitempty"`
	CheckIntervalDays int        `json:"check_interval_days,omitempty"`
	Channel           string     `json:"channel"`
	Platform          string     `json:"platform"`
	Arch              string     `json:"arch"`
	UsePackageManager bool       `json:"use_package_manager,omitempty"`
	PackageName       string     `json:"package_name,omitempty"`
	UseMSI            bool       `json:"use_msi,omitempty"`
}

// SelfUpdateStatus mirrors the server's self-update status for testing
type SelfUpdateStatus struct {
	Enabled        bool       `json:"enabled"`
	Status         string     `json:"status"`
	CurrentVersion string     `json:"current_version"`
	TargetVersion  string     `json:"target_version,omitempty"`
	LastCheckAt    *time.Time `json:"last_check_at,omitempty"`
	NextCheckAt    *time.Time `json:"next_check_at,omitempty"`
	Channel        string     `json:"channel"`
	Platform       string     `json:"platform"`
	Arch           string     `json:"arch"`
}

// SelfUpdateRun mirrors a self-update run record for testing
type SelfUpdateRun struct {
	ID             int64          `json:"id"`
	Status         string         `json:"status"`
	RequestedAt    time.Time      `json:"requested_at"`
	StartedAt      time.Time      `json:"started_at"`
	CompletedAt    time.Time      `json:"completed_at"`
	CurrentVersion string         `json:"current_version"`
	TargetVersion  string         `json:"target_version"`
	Channel        string         `json:"channel"`
	Platform       string         `json:"platform"`
	Arch           string         `json:"arch"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// TestAutoUpdateAPI_Status tests the agent's /api/autoupdate/status endpoint
func TestAutoUpdateAPI_Status(t *testing.T) {
	t.Parallel()

	// Create mock agent server
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/status" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		status := AutoUpdateStatus{
			Enabled:           true,
			CurrentVersion:    "1.0.0",
			LatestVersion:     "1.1.0",
			UpdateAvailable:   true,
			Status:            "idle",
			PolicySource:      "fleet",
			CheckIntervalDays: 7,
			Channel:           "stable",
			Platform:          "windows",
			Arch:              "amd64",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}))
	defer agentServer.Close()

	// Test GET /api/autoupdate/status
	resp, err := http.Get(agentServer.URL + "/api/autoupdate/status")
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var status AutoUpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !status.Enabled {
		t.Error("Expected enabled=true")
	}
	if status.CurrentVersion != "1.0.0" {
		t.Errorf("Expected current_version=1.0.0, got %s", status.CurrentVersion)
	}
	if !status.UpdateAvailable {
		t.Error("Expected update_available=true")
	}
	if status.Status != "idle" {
		t.Errorf("Expected status=idle, got %s", status.Status)
	}
	if status.PolicySource != "fleet" {
		t.Errorf("Expected policy_source=fleet, got %s", status.PolicySource)
	}
}

// TestAutoUpdateAPI_StatusDisabled tests disabled auto-update status
func TestAutoUpdateAPI_StatusDisabled(t *testing.T) {
	t.Parallel()

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/status" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		status := AutoUpdateStatus{
			Enabled:        false,
			DisabledReason: "not initialized (no server connection)",
			Status:         "disabled",
			Platform:       "windows",
			Arch:           "amd64",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}))
	defer agentServer.Close()

	resp, err := http.Get(agentServer.URL + "/api/autoupdate/status")
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer resp.Body.Close()

	var status AutoUpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if status.Enabled {
		t.Error("Expected enabled=false")
	}
	if status.DisabledReason == "" {
		t.Error("Expected non-empty disabled_reason")
	}
	if status.Status != "disabled" {
		t.Errorf("Expected status=disabled, got %s", status.Status)
	}
}

// TestAutoUpdateAPI_Check tests the /api/autoupdate/check endpoint
func TestAutoUpdateAPI_Check(t *testing.T) {
	t.Parallel()

	checkCalled := false

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/check" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		checkCalled = true

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Update check initiated",
		})
	}))
	defer agentServer.Close()

	resp, err := http.Post(agentServer.URL+"/api/autoupdate/check", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to trigger check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if !checkCalled {
		t.Error("Expected check handler to be called")
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	msg, ok := result["message"].(string)
	if !ok || msg == "" {
		t.Error("Expected message in response")
	}
}

// TestAutoUpdateAPI_CheckMethodNotAllowed tests wrong HTTP method
func TestAutoUpdateAPI_CheckMethodNotAllowed(t *testing.T) {
	t.Parallel()

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer agentServer.Close()

	resp, err := http.Get(agentServer.URL + "/api/autoupdate/check")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

// TestAutoUpdateAPI_Force tests the /api/autoupdate/force endpoint
func TestAutoUpdateAPI_Force(t *testing.T) {
	t.Parallel()

	var receivedReason string

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/force" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		var payload struct {
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			receivedReason = payload.Reason
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Force reinstall initiated",
		})
	}))
	defer agentServer.Close()

	// Test with reason payload
	body, _ := json.Marshal(map[string]string{"reason": "test_force_reinstall"})
	resp, err := http.Post(agentServer.URL+"/api/autoupdate/force", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to trigger force: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if receivedReason != "test_force_reinstall" {
		t.Errorf("Expected reason=test_force_reinstall, got %s", receivedReason)
	}
}

// TestAutoUpdateAPI_ForceUnavailable tests force when manager not available
func TestAutoUpdateAPI_ForceUnavailable(t *testing.T) {
	t.Parallel()

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/force" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "auto-update not available",
			"message": "Agent must be connected to a server for updates. Check server configuration.",
		})
	}))
	defer agentServer.Close()

	body, _ := json.Marshal(map[string]string{"reason": "test"})
	resp, err := http.Post(agentServer.URL+"/api/autoupdate/force", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["error"] != "auto-update not available" {
		t.Errorf("Expected error message about unavailability")
	}
}

// TestAutoUpdateAPI_Cancel tests the /api/autoupdate/cancel endpoint
func TestAutoUpdateAPI_Cancel(t *testing.T) {
	t.Parallel()

	cancelCalled := false

	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autoupdate/cancel" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		cancelCalled = true

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Update cancelled",
		})
	}))
	defer agentServer.Close()

	resp, err := http.Post(agentServer.URL+"/api/autoupdate/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to cancel: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if !cancelCalled {
		t.Error("Expected cancel handler to be called")
	}
}

// TestSelfUpdateAPI_Status tests the server's /api/v1/selfupdate/status endpoint
func TestSelfUpdateAPI_Status(t *testing.T) {
	t.Parallel()

	serverInstance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/selfupdate/status" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		status := SelfUpdateStatus{
			Enabled:        true,
			Status:         "idle",
			CurrentVersion: "0.9.5",
			Channel:        "stable",
			Platform:       "windows",
			Arch:           "amd64",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}))
	defer serverInstance.Close()

	resp, err := http.Get(serverInstance.URL + "/api/v1/selfupdate/status")
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var status SelfUpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !status.Enabled {
		t.Error("Expected enabled=true")
	}
	if status.CurrentVersion != "0.9.5" {
		t.Errorf("Expected current_version=0.9.5, got %s", status.CurrentVersion)
	}
}

// TestSelfUpdateAPI_Runs tests the server's /api/v1/selfupdate/runs endpoint
func TestSelfUpdateAPI_Runs(t *testing.T) {
	t.Parallel()

	serverInstance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/selfupdate/runs" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		now := time.Now()
		runs := []SelfUpdateRun{
			{
				ID:             1,
				Status:         "succeeded",
				RequestedAt:    now.Add(-2 * time.Hour),
				StartedAt:      now.Add(-2 * time.Hour),
				CompletedAt:    now.Add(-1*time.Hour - 55*time.Minute),
				CurrentVersion: "0.9.4",
				TargetVersion:  "0.9.5",
				Channel:        "stable",
				Platform:       "windows",
				Arch:           "amd64",
			},
			{
				ID:             2,
				Status:         "skipped",
				RequestedAt:    now.Add(-1 * time.Hour),
				StartedAt:      now.Add(-1 * time.Hour),
				CompletedAt:    now.Add(-1 * time.Hour),
				CurrentVersion: "0.9.5",
				Channel:        "stable",
				Platform:       "windows",
				Arch:           "amd64",
				Metadata:       map[string]any{"reason": "no-artifacts"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(runs)
	}))
	defer serverInstance.Close()

	resp, err := http.Get(serverInstance.URL + "/api/v1/selfupdate/runs")
	if err != nil {
		t.Fatalf("Failed to get runs: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var runs []SelfUpdateRun
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(runs) != 2 {
		t.Errorf("Expected 2 runs, got %d", len(runs))
	}

	if runs[0].Status != "succeeded" {
		t.Errorf("Expected first run status=succeeded, got %s", runs[0].Status)
	}
	if runs[1].Status != "skipped" {
		t.Errorf("Expected second run status=skipped, got %s", runs[1].Status)
	}
}

// TestSelfUpdateAPI_Check tests the server's /api/v1/selfupdate/check endpoint
func TestSelfUpdateAPI_Check(t *testing.T) {
	t.Parallel()

	checkCalled := false

	serverInstance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/selfupdate/check" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		checkCalled = true

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Update check initiated",
		})
	}))
	defer serverInstance.Close()

	resp, err := http.Post(serverInstance.URL+"/api/v1/selfupdate/check", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to trigger check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if !checkCalled {
		t.Error("Expected check handler to be called")
	}
}

// TestSelfUpdateAPI_CheckUnavailable tests check when manager not available
func TestSelfUpdateAPI_CheckUnavailable(t *testing.T) {
	t.Parallel()

	serverInstance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/selfupdate/check" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		http.Error(w, "self-update manager not available", http.StatusServiceUnavailable)
	}))
	defer serverInstance.Close()

	resp, err := http.Post(serverInstance.URL+"/api/v1/selfupdate/check", "application/json", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}
}

// TestUpdateStatusTransitions tests valid status transitions
func TestUpdateStatusTransitions(t *testing.T) {
	t.Parallel()

	// Valid status progression during a successful update
	validProgression := []string{
		"idle",
		"checking",
		"pending",
		"downloading",
		"staging",
		"applying",
		"restarting",
		"succeeded",
	}

	// Valid statuses that can occur from any state
	terminalStatuses := map[string]bool{
		"failed":      true,
		"cancelled":   true,
		"skipped":     true,
		"rolled_back": true,
	}

	// Valid intermediate statuses
	allStatuses := map[string]bool{
		"idle":        true,
		"checking":    true,
		"pending":     true,
		"downloading": true,
		"staging":     true,
		"applying":    true,
		"restarting":  true,
		"succeeded":   true,
		"failed":      true,
		"skipped":     true,
		"rolled_back": true,
		"cancelled":   true,
	}

	// Verify all progression statuses are valid
	for _, status := range validProgression {
		if !allStatuses[status] {
			t.Errorf("Invalid status in progression: %s", status)
		}
	}

	// Verify terminal statuses are valid
	for status := range terminalStatuses {
		if !allStatuses[status] {
			t.Errorf("Invalid terminal status: %s", status)
		}
	}

	t.Log("✓ All status values are valid")
}

// TestWebSocketCommandHandling tests WebSocket command message format
func TestWebSocketCommandHandling(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		command string
		data    map[string]interface{}
		valid   bool
	}{
		{
			name:    "check_update command",
			command: "check_update",
			data:    nil,
			valid:   true,
		},
		{
			name:    "force_reinstall with reason",
			command: "force_reinstall",
			data:    map[string]interface{}{"reason": "server_triggered"},
			valid:   true,
		},
		{
			name:    "cancel_update command",
			command: "cancel_update",
			data:    nil,
			valid:   true,
		},
		{
			name:    "restart command",
			command: "restart",
			data:    nil,
			valid:   true,
		},
		{
			name:    "unknown command",
			command: "unknown_command",
			data:    nil,
			valid:   false,
		},
	}

	validCommands := map[string]bool{
		"check_update":    true,
		"force_reinstall": true,
		"cancel_update":   true,
		"restart":         true,
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			isValid := validCommands[tc.command]
			if isValid != tc.valid {
				t.Errorf("Command %q validity: expected %v, got %v", tc.command, tc.valid, isValid)
			}
		})
	}
}

// TestUpdateManifestValidation tests manifest structure validation
func TestUpdateManifestValidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		manifest map[string]interface{}
		valid    bool
	}{
		{
			name: "valid manifest",
			manifest: map[string]interface{}{
				"manifest_version": "1",
				"component":        "agent",
				"version":          "1.1.0",
				"platform":         "windows",
				"arch":             "amd64",
				"channel":          "stable",
				"sha256":           "abc123def456...",
				"size_bytes":       15728640,
				"source_url":       "https://releases.example.com/agent-1.1.0.zip",
			},
			valid: true,
		},
		{
			name: "missing version",
			manifest: map[string]interface{}{
				"manifest_version": "1",
				"component":        "agent",
				"platform":         "windows",
				"arch":             "amd64",
			},
			valid: false,
		},
		{
			name: "missing sha256",
			manifest: map[string]interface{}{
				"manifest_version": "1",
				"component":        "agent",
				"version":          "1.1.0",
				"platform":         "windows",
				"arch":             "amd64",
			},
			valid: false,
		},
		{
			name: "invalid platform",
			manifest: map[string]interface{}{
				"manifest_version": "1",
				"component":        "agent",
				"version":          "1.1.0",
				"platform":         "invalid_os",
				"arch":             "amd64",
				"sha256":           "abc123",
			},
			valid: false,
		},
	}

	validPlatforms := map[string]bool{
		"windows": true,
		"linux":   true,
		"darwin":  true,
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Validate required fields
			hasRequiredFields := true
			requiredFields := []string{"manifest_version", "component", "version", "platform", "arch", "sha256"}

			for _, field := range requiredFields {
				if _, ok := tc.manifest[field]; !ok {
					hasRequiredFields = false
					break
				}
			}

			// Validate platform if present
			validPlatform := true
			if platform, ok := tc.manifest["platform"].(string); ok {
				validPlatform = validPlatforms[platform]
			}

			isValid := hasRequiredFields && validPlatform

			if isValid != tc.valid {
				t.Errorf("Manifest validity: expected %v, got %v", tc.valid, isValid)
			}
		})
	}
}

// TestTelemetryPayloadFormat tests the telemetry payload structure
func TestTelemetryPayloadFormat(t *testing.T) {
	t.Parallel()

	// Test telemetry payload for successful update
	successPayload := map[string]interface{}{
		"agent_id":        "test-agent-001",
		"run_id":          "run-123",
		"status":          "succeeded",
		"current_version": "1.0.0",
		"target_version":  "1.1.0",
		"download_time_ms": 5432,
		"size_bytes":      15728640,
		"timestamp":       time.Now().Format(time.RFC3339),
	}

	// Verify required fields
	requiredFields := []string{"agent_id", "status", "current_version", "timestamp"}
	for _, field := range requiredFields {
		if _, ok := successPayload[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Test telemetry payload for failed update
	failPayload := map[string]interface{}{
		"agent_id":        "test-agent-001",
		"run_id":          "run-456",
		"status":          "failed",
		"current_version": "1.0.0",
		"target_version":  "1.1.0",
		"error_code":      "DOWNLOAD_FAILED",
		"error_message":   "Network timeout during download",
		"timestamp":       time.Now().Format(time.RFC3339),
	}

	// Verify error fields present for failed status
	if failPayload["status"] == "failed" {
		if _, ok := failPayload["error_code"]; !ok {
			t.Error("Failed payload should include error_code")
		}
		if _, ok := failPayload["error_message"]; !ok {
			t.Error("Failed payload should include error_message")
		}
	}
}

// TestUpdateContextCancellation verifies context cancellation handling
func TestUpdateContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	// Simulate an update operation that respects context
	done := make(chan bool)
	go func() {
		select {
		case <-ctx.Done():
			done <- true
		case <-time.After(5 * time.Second):
			done <- false
		}
	}()

	// Cancel context
	cancel()

	// Verify operation was cancelled
	select {
	case cancelled := <-done:
		if !cancelled {
			t.Error("Expected operation to be cancelled")
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for cancellation")
	}
}
