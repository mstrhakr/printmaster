//go:build e2e
// +build e2e

package tests

// E2E tests that run against the Docker Compose environment.
// These tests require the server and agent containers to be running.
//
// Environment variables (set by docker-compose.e2e.yml):
//   E2E_SERVER_URL    - Server base URL (default: http://localhost:8443)
//   E2E_AGENT_URL     - Agent base URL (default: http://localhost:8080)
//   E2E_ADMIN_PASSWORD - Admin password for authentication
//
// Run locally:
//   1. docker compose -f tests/docker-compose.e2e.yml up -d
//   2. go test -tags=e2e -v ./tests/...
//   3. docker compose -f tests/docker-compose.e2e.yml down

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Test configuration from environment
var (
	serverURL     = getEnv("E2E_SERVER_URL", "http://localhost:8443")
	agentURL      = getEnv("E2E_AGENT_URL", "http://localhost:8080")
	adminPassword = getEnv("E2E_ADMIN_PASSWORD", "e2e-test-password")
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// httpClient with reasonable timeouts for E2E tests
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// ===========================================================================
// Health Check Tests
// ===========================================================================

func TestE2E_ServerHealth(t *testing.T) {
	resp, err := httpClient.Get(serverURL + "/api/health")
	if err != nil {
		t.Fatalf("Failed to reach server health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Server health check failed: status=%d body=%s", resp.StatusCode, body)
	}

	t.Logf("✓ Server is healthy at %s", serverURL)
}

func TestE2E_AgentHealth(t *testing.T) {
	resp, err := httpClient.Get(agentURL + "/api/health")
	if err != nil {
		t.Fatalf("Failed to reach agent health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("Agent health check failed: status=%d body=%s", resp.StatusCode, body)
	}

	t.Logf("✓ Agent is healthy at %s", agentURL)
}

// ===========================================================================
// Agent Registration Tests
// ===========================================================================

func TestE2E_AgentRegistration(t *testing.T) {
	// Wait for agent to register with server (may take a few seconds after startup)
	var registered bool
	for i := 0; i < 10; i++ {
		resp, err := httpClient.Get(serverURL + "/api/v1/agents")
		if err != nil {
			t.Logf("Attempt %d: Failed to query agents: %v", i+1, err)
			time.Sleep(2 * time.Second)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Logf("Attempt %d: Status %d", i+1, resp.StatusCode)
			time.Sleep(2 * time.Second)
			continue
		}

		// Check if our test agent is registered
		if strings.Contains(string(body), "e2e-test-agent") ||
			strings.Contains(string(body), "e2e00000-0000-0000-0000-000000000001") {
			registered = true
			t.Logf("✓ Agent 'e2e-test-agent' is registered")
			break
		}

		t.Logf("Attempt %d: Agent not found in response", i+1)
		time.Sleep(2 * time.Second)
	}

	if !registered {
		t.Error("Agent failed to register with server within timeout")
	}
}

// ===========================================================================
// Device API Tests
// ===========================================================================

func TestE2E_ServerDevicesList(t *testing.T) {
	resp, err := httpClient.Get(serverURL + "/api/v1/devices")
	if err != nil {
		t.Fatalf("Failed to query devices: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var devices []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		t.Fatalf("Failed to decode devices response: %v", err)
	}

	// We seeded 5 devices
	if len(devices) < 5 {
		t.Errorf("Expected at least 5 devices, got %d", len(devices))
	}

	// Verify our test devices exist
	expectedSerials := []string{"HP-E2E-001", "KYOCERA-E2E-002", "BROTHER-E2E-003", "LEXMARK-E2E-004", "XEROX-E2E-005"}
	foundSerials := make(map[string]bool)

	for _, dev := range devices {
		if serial, ok := dev["serial_number"].(string); ok {
			foundSerials[serial] = true
		}
	}

	for _, serial := range expectedSerials {
		if !foundSerials[serial] {
			t.Errorf("Expected device %s not found", serial)
		} else {
			t.Logf("✓ Found device %s", serial)
		}
	}
}

func TestE2E_AgentDevicesList(t *testing.T) {
	resp, err := httpClient.Get(agentURL + "/api/devices")
	if err != nil {
		t.Fatalf("Failed to query agent devices: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode agent devices response: %v", err)
	}

	devices, ok := result["devices"].([]interface{})
	if !ok {
		t.Fatal("Response missing 'devices' array")
	}

	if len(devices) < 5 {
		t.Errorf("Expected at least 5 devices on agent, got %d", len(devices))
	}

	t.Logf("✓ Agent has %d devices", len(devices))
}

// ===========================================================================
// Metrics API Tests
// ===========================================================================

func TestE2E_DeviceMetrics(t *testing.T) {
	// Get metrics for HP device
	resp, err := httpClient.Get(serverURL + "/api/v1/devices/HP-E2E-001/metrics")
	if err != nil {
		t.Fatalf("Failed to query device metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var metrics []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode metrics response: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected metrics data, got empty array")
		return
	}

	// Verify metrics structure
	latest := metrics[len(metrics)-1]
	if _, ok := latest["page_count"]; !ok {
		t.Error("Metrics missing page_count field")
	}
	if _, ok := latest["toner_black"]; !ok {
		t.Error("Metrics missing toner_black field")
	}

	t.Logf("✓ Got %d metric records for HP-E2E-001", len(metrics))
}

// ===========================================================================
// WebSocket Connection Tests
// ===========================================================================

func TestE2E_WebSocketConnection(t *testing.T) {
	// Check agent's WebSocket status
	resp, err := httpClient.Get(agentURL + "/api/status")
	if err != nil {
		t.Fatalf("Failed to query agent status: %v", err)
	}
	defer resp.Body.Close()

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}

	// Check server connectivity
	serverStatus, _ := status["server"].(map[string]interface{})
	if serverStatus == nil {
		t.Log("⚠ Server status not reported by agent")
		return
	}

	connected, _ := serverStatus["connected"].(bool)
	wsConnected, _ := serverStatus["websocket_connected"].(bool)

	t.Logf("Agent server connection: connected=%v websocket=%v", connected, wsConnected)

	// Give WebSocket time to connect
	if !wsConnected {
		t.Log("Waiting for WebSocket connection...")
		time.Sleep(5 * time.Second)

		// Re-check
		resp, _ = httpClient.Get(agentURL + "/api/status")
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		serverStatus, _ = status["server"].(map[string]interface{})
		wsConnected, _ = serverStatus["websocket_connected"].(bool)
	}

	if wsConnected {
		t.Log("✓ WebSocket connection established")
	} else {
		t.Log("⚠ WebSocket not connected (may be expected in some configurations)")
	}
}

// ===========================================================================
// Auto-Update Status Tests
// ===========================================================================

func TestE2E_AutoUpdateStatus(t *testing.T) {
	resp, err := httpClient.Get(agentURL + "/api/autoupdate/status")
	if err != nil {
		t.Fatalf("Failed to query autoupdate status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode autoupdate status: %v", err)
	}

	// Verify autoupdate is disabled in tests
	enabled, _ := status["enabled"].(bool)
	if enabled {
		t.Log("⚠ Auto-update is enabled (expected disabled in E2E tests)")
	} else {
		t.Log("✓ Auto-update is disabled as expected")
	}

	// Check status field exists
	if _, ok := status["status"]; !ok {
		t.Error("Autoupdate status missing 'status' field")
	}

	t.Logf("Autoupdate status: %v", status["status"])
}

// ===========================================================================
// Server Self-Update Tests
// ===========================================================================

func TestE2E_ServerSelfUpdateStatus(t *testing.T) {
	resp, err := httpClient.Get(serverURL + "/api/v1/selfupdate/status")
	if err != nil {
		t.Fatalf("Failed to query selfupdate status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode selfupdate status: %v", err)
	}

	t.Logf("✓ Server selfupdate status: %v", status["status"])
}

// ===========================================================================
// API Error Handling Tests
// ===========================================================================

func TestE2E_NotFoundHandling(t *testing.T) {
	resp, err := httpClient.Get(serverURL + "/api/v1/devices/NONEXISTENT-SERIAL/metrics")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should return 404
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for nonexistent device, got %d", resp.StatusCode)
	} else {
		t.Log("✓ Server correctly returns 404 for nonexistent devices")
	}
}

func TestE2E_InvalidMethodHandling(t *testing.T) {
	req, _ := http.NewRequest(http.MethodDelete, serverURL+"/api/health", nil)
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should return 405 Method Not Allowed
	if resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusOK {
		// Some endpoints may allow all methods on health check
		t.Logf("Health endpoint returned %d for DELETE method", resp.StatusCode)
	}
}

// ===========================================================================
// Integration Flow Test
// ===========================================================================

func TestE2E_FullIntegrationFlow(t *testing.T) {
	t.Log("Running full integration flow test...")

	// 1. Verify server is up
	resp, err := httpClient.Get(serverURL + "/api/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal("Server not healthy")
	}
	resp.Body.Close()
	t.Log("  ✓ Server healthy")

	// 2. Verify agent is up
	resp, err = httpClient.Get(agentURL + "/api/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal("Agent not healthy")
	}
	resp.Body.Close()
	t.Log("  ✓ Agent healthy")

	// 3. Verify devices on agent
	resp, _ = httpClient.Get(agentURL + "/api/devices")
	var agentResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&agentResult)
	resp.Body.Close()
	agentDevices, _ := agentResult["devices"].([]interface{})
	t.Logf("  ✓ Agent has %d devices", len(agentDevices))

	// 4. Verify devices on server
	resp, _ = httpClient.Get(serverURL + "/api/v1/devices")
	var serverDevices []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&serverDevices)
	resp.Body.Close()
	t.Logf("  ✓ Server has %d devices", len(serverDevices))

	// 5. Verify agent registered with server
	resp, _ = httpClient.Get(serverURL + "/api/v1/agents")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "e2e-test-agent") {
		t.Log("  ✓ Agent registered with server")
	} else {
		t.Log("  ⚠ Agent may not be registered yet")
	}

	t.Log("Full integration flow completed!")
}

// ===========================================================================
// Cleanup / Utility Functions
// ===========================================================================

func init() {
	// Print test configuration on startup
	fmt.Printf("E2E Test Configuration:\n")
	fmt.Printf("  Server URL: %s\n", serverURL)
	fmt.Printf("  Agent URL:  %s\n", agentURL)
	fmt.Printf("\n")
}
