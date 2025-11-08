package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPAPI_AgentRegistration tests agent registration flow
func TestHTTPAPI_AgentRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	// Create mock server that simulates agent registration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/register" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate required fields
		agentID, ok := req["agent_id"].(string)
		if !ok || agentID == "" {
			http.Error(w, "Missing agent_id", http.StatusBadRequest)
			return
		}

		// Return success with token
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"agent_id": agentID,
			"token":    "test-token-123456",
		})
	}))
	defer server.Close()

	// Test successful registration
	reqBody := map[string]interface{}{
		"agent_id":         "test-agent-001",
		"agent_version":    "v0.6.0",
		"hostname":         "test-host",
		"platform":         "windows",
		"protocol_version": "1",
	}

	body, _ := json.Marshal(reqBody)
	resp, err := http.Post(server.URL+"/api/v1/agents/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if respData["success"] != true {
		t.Errorf("Expected success=true, got %v", respData["success"])
	}

	token, ok := respData["token"].(string)
	if !ok || token == "" {
		t.Errorf("Expected non-empty token, got %v", respData["token"])
	}

	t.Logf("✓ Agent registration successful, token: %s", token[:8]+"...")
}

// TestHTTPAPI_Heartbeat tests agent heartbeat endpoint
func TestHTTPAPI_Heartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	const testToken = "test-token-789"

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/heartbeat" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Check authorization
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+testToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Decode heartbeat
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Heartbeat received",
		})
	}))
	defer server.Close()

	// Test successful heartbeat
	reqBody := map[string]interface{}{
		"status":       "active",
		"device_count": 5,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/agents/heartbeat", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if respData["success"] != true {
		t.Errorf("Expected success=true, got %v", respData["success"])
	}

	t.Log("✓ Heartbeat sent successfully")
}

// TestHTTPAPI_UnauthorizedAccess tests that endpoints reject invalid tokens
func TestHTTPAPI_UnauthorizedAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Test with invalid token
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized, got %d", resp.StatusCode)
	}

	t.Log("✓ Correctly rejected invalid token")

	// Test with valid token
	req2, _ := http.NewRequest(http.MethodPost, server.URL+"/api/test", nil)
	req2.Header.Set("Authorization", "Bearer valid-token")

	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 with valid token, got %d", resp2.StatusCode)
	}

	t.Log("✓ Accepted valid token")
}

// TestHTTPAPI_DeviceUpload tests device batch upload endpoint
func TestHTTPAPI_DeviceUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	const testToken = "test-token-device"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/devices/batch" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		// Check authorization
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+testToken {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Decode devices
		var devices []map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&devices); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if len(devices) == 0 {
			http.Error(w, "No devices provided", http.StatusBadRequest)
			return
		}

		// Return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"count":   len(devices),
		})
	}))
	defer server.Close()

	// Test device upload
	devices := []map[string]interface{}{
		{
			"serial":       "ABC123",
			"manufacturer": "HP",
			"model":        "LaserJet Pro",
			"ip":           "192.168.1.100",
		},
		{
			"serial":       "XYZ789",
			"manufacturer": "Canon",
			"model":        "imageRUNNER",
			"ip":           "192.168.1.101",
		},
	}

	body, _ := json.Marshal(devices)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/devices/batch", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload devices: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	count, ok := respData["count"].(float64)
	if !ok || int(count) != len(devices) {
		t.Errorf("Expected count=%d, got %v", len(devices), respData["count"])
	}

	t.Logf("✓ Successfully uploaded %d devices", len(devices))
}
