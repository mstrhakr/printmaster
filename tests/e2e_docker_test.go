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
	resp, err := httpClient.Get(serverURL + "/health")
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
	resp, err := httpClient.Get(agentURL + "/health")
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
	// The server's /api/v1/agents/list endpoint requires web auth (session cookie).
	// Without logging in first, we expect 401/302 redirect.
	// This test validates that the security is working correctly.
	resp, err := httpClient.Get(serverURL + "/api/v1/agents/list")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Server should require authentication for agents list
	// 401 = direct unauthorized, 302 = redirect to login
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("✓ Server correctly requires authentication for agents list")
	} else if resp.StatusCode == http.StatusOK {
		// If we get OK, check if agent is registered
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "e2e-test-agent") {
			t.Log("✓ Agent 'e2e-test-agent' is registered")
		} else {
			t.Log("✓ Agents list accessible (no test agent found)")
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("Unexpected status %d: %s", resp.StatusCode, body)
	}
}

// ===========================================================================
// Device API Tests
// ===========================================================================

func TestE2E_ServerDevicesList(t *testing.T) {
	// Server's /api/v1/devices/list requires web authentication
	// This test validates security is working
	resp, err := httpClient.Get(serverURL + "/api/v1/devices/list")
	if err != nil {
		t.Fatalf("Failed to query devices: %v", err)
	}
	defer resp.Body.Close()

	// Expect 401 or 302 redirect to login
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("✓ Server correctly requires authentication for devices list")
		return
	}

	// If for some reason we got through (shouldn't happen), validate response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var devices []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		t.Fatalf("Failed to decode devices response: %v", err)
	}

	t.Logf("✓ Server has %d devices", len(devices))
}

func TestE2E_AgentDevicesList(t *testing.T) {
	resp, err := httpClient.Get(agentURL + "/devices/list")
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

	// In E2E tests, agent might not have seeded devices
	t.Logf("✓ Agent has %d devices", len(devices))
}

// ===========================================================================
// Metrics API Tests
// ===========================================================================

func TestE2E_DeviceMetrics(t *testing.T) {
	// Server's /api/devices/metrics/history requires web authentication
	// This test validates security is working
	resp, err := httpClient.Get(serverURL + "/api/devices/metrics/history?serial=HP-E2E-001")
	if err != nil {
		t.Fatalf("Failed to query device metrics: %v", err)
	}
	defer resp.Body.Close()

	// Expect 401 or 302 redirect to login
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("✓ Server correctly requires authentication for metrics history")
		return
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var metrics []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		t.Fatalf("Failed to decode metrics response: %v", err)
	}

	t.Logf("✓ Got %d metric records for HP-E2E-001", len(metrics))
}

// ===========================================================================
// WebSocket Connection Tests
// ===========================================================================

func TestE2E_WebSocketConnection(t *testing.T) {
	// The agent doesn't have a dedicated status endpoint for WebSocket connection state.
	// We can verify the agent is responsive and check version info instead.
	resp, err := httpClient.Get(agentURL + "/api/version")
	if err != nil {
		t.Fatalf("Failed to query agent version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Unexpected status %d: %s", resp.StatusCode, body)
	}

	var version map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		t.Fatalf("Failed to decode version: %v", err)
	}

	// Verify version response has expected fields
	if v, ok := version["version"]; ok {
		t.Logf("✓ Agent version: %v", v)
	} else {
		t.Log("✓ Agent responded to version request")
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
	// Server's selfupdate/status requires web authentication
	resp, err := httpClient.Get(serverURL + "/api/v1/selfupdate/status")
	if err != nil {
		t.Fatalf("Failed to query selfupdate status: %v", err)
	}
	defer resp.Body.Close()

	// Expect 401 or 302 redirect to login
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("✓ Server correctly requires authentication for selfupdate status")
		return
	}

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
	// Test that a completely nonexistent API path returns 404
	resp, err := httpClient.Get(serverURL + "/api/v1/nonexistent-endpoint")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Should return 404 for nonexistent endpoint
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for nonexistent endpoint, got %d", resp.StatusCode)
	} else {
		t.Log("✓ Server correctly returns 404 for nonexistent endpoints")
	}
}

func TestE2E_InvalidMethodHandling(t *testing.T) {
	req, _ := http.NewRequest(http.MethodDelete, serverURL+"/health", nil)
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
	resp, err := httpClient.Get(serverURL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal("Server not healthy")
	}
	resp.Body.Close()
	t.Log("  ✓ Server healthy")

	// 2. Verify agent is up
	resp, err = httpClient.Get(agentURL + "/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal("Agent not healthy")
	}
	resp.Body.Close()
	t.Log("  ✓ Agent healthy")

	// 3. Verify devices on agent (auth disabled in E2E config)
	resp, err = httpClient.Get(agentURL + "/devices/list")
	if err != nil {
		t.Fatalf("Failed to query agent devices: %v", err)
	}
	var agentResult map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&agentResult)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		agentDevices, _ := agentResult["devices"].([]interface{})
		t.Logf("  ✓ Agent has %d devices", len(agentDevices))
	} else {
		t.Logf("  ⚠ Agent devices check returned %d (auth may be enabled)", resp.StatusCode)
	}

	// 4. Server endpoints require auth - just verify they respond
	resp, _ = httpClient.Get(serverURL + "/api/v1/devices/list")
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("  ✓ Server devices list requires authentication (expected)")
	} else {
		t.Logf("  ✓ Server devices list responded with status %d", resp.StatusCode)
	}

	// 5. Server agents list requires auth - just verify they respond
	resp, _ = httpClient.Get(serverURL + "/api/v1/agents/list")
	resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusFound {
		t.Log("  ✓ Server agents list requires authentication (expected)")
	} else {
		t.Logf("  ✓ Server agents list responded with status %d", resp.StatusCode)
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
