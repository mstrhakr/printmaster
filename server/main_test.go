package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"printmaster/server/storage"
	"printmaster/server/tenancy"
	"testing"
	"time"
)

// setupTestServer creates a test server with in-memory storage
func setupTestServer(t *testing.T) (*httptest.Server, storage.Store) {
	t.Helper()

	// Create in-memory store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}

	// Set globals for handlers
	serverStore = store
	// Note: serverLogger is nil in tests, handlers should handle gracefully

	// Create a dedicated mux for this test server to avoid races when tests run
	// in parallel. Register the core handlers on that mux and register tenancy
	// routes onto the same mux using the mux-aware registration function.
	mux := http.NewServeMux()

	// Register core handlers onto the new mux
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/version", handleVersion)
	// Keep /api/v1/agents/register for compatibility (it will return 403)
	mux.HandleFunc("/api/v1/agents/register", handleAgentRegister)
	mux.HandleFunc("/api/v1/agents/heartbeat", requireAuth(handleAgentHeartbeat))
	mux.HandleFunc("/api/v1/agents/list", handleAgentsList)
	mux.HandleFunc("/api/v1/agents/", handleAgentDetails)
	mux.HandleFunc("/api/v1/devices/batch", requireAuth(handleDevicesBatch))
	mux.HandleFunc("/api/v1/metrics/batch", requireAuth(handleMetricsBatch))

	// Register tenancy routes onto this mux (avoids global DefaultServeMux)
	tenancy.RegisterRoutesOnMux(mux, store)

	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})

	return server, store
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("Failed to call /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "healthy" {
		t.Errorf("Expected status=healthy, got %v", result["status"])
	}
}

func TestVersionEndpoint(t *testing.T) {
	t.Parallel()

	server, _ := setupTestServer(t)

	resp, err := http.Get(server.URL + "/api/version")
	if err != nil {
		t.Fatalf("Failed to call /api/version: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if _, ok := result["version"]; !ok {
		t.Error("Response missing 'version' field")
	}
	if _, ok := result["protocol_version"]; !ok {
		t.Error("Response missing 'protocol_version' field")
	}
}

func TestAgentRegistration(t *testing.T) {
	// Note: Not parallel due to shared global serverStore
	server, store := setupTestServer(t)

	// Create tenant and join token, then register agent using register-with-token
	ctx := context.Background()
	tn := &storage.Tenant{ID: "test-tenant-01", Name: "TestTenant"}
	if err := store.CreateTenant(ctx, tn); err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}
	_, rawToken, err := store.CreateJoinToken(ctx, tn.ID, 60, true)
	if err != nil {
		t.Fatalf("Failed to create join token: %v", err)
	}

	reqBody := map[string]interface{}{
		"token":            rawToken,
		"agent_id":         "test-agent-01",
		"agent_version":    "v0.2.0",
		"protocol_version": "1",
		"hostname":         "test-host",
		"ip":               "192.168.1.100",
		"platform":         "windows",
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL+"/api/v1/agents/register-with-token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response
	if result["success"] != true {
		t.Errorf("Expected success=true, got %v", result["success"])
	}
	agentToken, ok := result["agent_token"].(string)
	if !ok || agentToken == "" {
		t.Error("Response missing or empty agent_token")
	}

	// Verify agent in database
	agent, err := store.GetAgent(ctx, "test-agent-01")
	if err != nil {
		t.Fatalf("Failed to retrieve agent from store: %v", err)
	}

	if agent.AgentID != "test-agent-01" {
		t.Errorf("Expected AgentID=test-agent-01, got %s", agent.AgentID)
	}
	if agent.Token != agentToken {
		t.Errorf("Token mismatch: response=%s, db=%s", agentToken, agent.Token)
	}
	if agent.Status != "active" {
		t.Errorf("Expected status=active, got %s", agent.Status)
	}
}

func TestHeartbeatRequiresAuth(t *testing.T) {
	t.Parallel()

	server, _ := setupTestServer(t)

	// Try heartbeat without token
	reqBody := map[string]interface{}{
		"agent_id":  "test-agent-01",
		"timestamp": time.Now(),
		"status":    "active",
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL+"/api/v1/agents/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to call heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestHeartbeatWithValidToken(t *testing.T) {
	// Note: Not parallel due to shared global serverStore
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Register agent first
	agent := &storage.Agent{
		AgentID:         "test-agent-02",
		Hostname:        "test-host",
		IP:              "192.168.1.100",
		Platform:        "windows",
		Version:         "v0.2.0",
		ProtocolVersion: "1",
		Token:           "test-token-12345",
		RegisteredAt:    time.Now(),
		LastSeen:        time.Now(),
		Status:          "active",
	}
	if err := store.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Send heartbeat with token
	reqBody := map[string]interface{}{
		"agent_id":  "test-agent-02",
		"timestamp": time.Now(),
		"status":    "active",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/agents/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token-12345")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to call heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("Expected success=true, got %v", result["success"])
	}
}

func TestHeartbeatWithInvalidToken(t *testing.T) {
	t.Parallel()

	server, _ := setupTestServer(t)

	reqBody := map[string]interface{}{
		"agent_id":  "test-agent-02",
		"timestamp": time.Now(),
		"status":    "active",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/agents/heartbeat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to call heartbeat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestDevicesBatchUpload(t *testing.T) {
	// Note: Not parallel due to shared global serverStore
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Register agent
	agent := &storage.Agent{
		AgentID:         "test-agent-03",
		Hostname:        "test-host",
		IP:              "192.168.1.100",
		Platform:        "windows",
		Version:         "v0.2.0",
		ProtocolVersion: "1",
		Token:           "test-token-67890",
		RegisteredAt:    time.Now(),
		LastSeen:        time.Now(),
		Status:          "active",
	}
	if err := store.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Upload devices
	devices := []map[string]interface{}{
		{
			"serial":       "ABC123",
			"ip":           "192.168.1.50",
			"manufacturer": "HP",
			"model":        "LaserJet Pro",
			"hostname":     "printer-01",
		},
		{
			"serial":       "XYZ789",
			"ip":           "192.168.1.51",
			"manufacturer": "Canon",
			"model":        "PIXMA",
			"hostname":     "printer-02",
		},
	}

	reqBody := map[string]interface{}{
		"agent_id":  "test-agent-03",
		"timestamp": time.Now(),
		"devices":   devices,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/devices/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token-67890")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload devices: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("Expected success=true, got %v", result["success"])
	}

	// Verify devices in database
	device1, err := store.GetDevice(ctx, "ABC123")
	if err != nil {
		t.Errorf("Failed to retrieve device ABC123: %v", err)
	} else if device1.Manufacturer != "HP" {
		t.Errorf("Expected manufacturer=HP, got %s", device1.Manufacturer)
	}

	device2, err := store.GetDevice(ctx, "XYZ789")
	if err != nil {
		t.Errorf("Failed to retrieve device XYZ789: %v", err)
	} else if device2.Manufacturer != "Canon" {
		t.Errorf("Expected manufacturer=Canon, got %s", device2.Manufacturer)
	}
}

func TestMetricsBatchUpload(t *testing.T) {
	// Note: Not parallel due to shared global serverStore
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Register agent
	agent := &storage.Agent{
		AgentID:         "test-agent-04",
		Hostname:        "test-host",
		IP:              "192.168.1.100",
		Platform:        "windows",
		Version:         "v0.2.0",
		ProtocolVersion: "1",
		Token:           "test-token-abcde",
		RegisteredAt:    time.Now(),
		LastSeen:        time.Now(),
		Status:          "active",
	}
	if err := store.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Create device first
	device := &storage.Device{}
	device.Serial = "METRICS-TEST-01"
	device.AgentID = "test-agent-04"
	device.IP = "192.168.1.60"
	device.LastSeen = time.Now()
	device.FirstSeen = time.Now()
	device.CreatedAt = time.Now()

	if err := store.UpsertDevice(ctx, device); err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Upload metrics
	metrics := []map[string]interface{}{
		{
			"serial":     "METRICS-TEST-01",
			"timestamp":  time.Now(),
			"page_count": 1234,
			"toner_levels": map[string]interface{}{
				"black": 75,
				"cyan":  80,
			},
		},
	}

	reqBody := map[string]interface{}{
		"agent_id":  "test-agent-04",
		"timestamp": time.Now(),
		"metrics":   metrics,
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", server.URL+"/api/v1/metrics/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token-abcde")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("Expected success=true, got %v", result["success"])
	}

	// Verify metrics in database
	retrieved, err := store.GetLatestMetrics(ctx, "METRICS-TEST-01")
	if err != nil {
		t.Fatalf("Failed to retrieve metrics: %v", err)
	}
	if retrieved.PageCount != 1234 {
		t.Errorf("Expected page_count=1234, got %d", retrieved.PageCount)
	}
}

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	// Generate multiple tokens
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateToken()
		if err != nil {
			t.Fatalf("Failed to generate token: %v", err)
		}

		// Check uniqueness
		if tokens[token] {
			t.Errorf("Duplicate token generated: %s", token)
		}
		tokens[token] = true

		// Check length (32 bytes base64-encoded = 44 chars)
		if len(token) < 40 {
			t.Errorf("Token too short: %s (len=%d)", token, len(token))
		}
	}
}

func TestExtractClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expectedIP    string
	}{
		{
			name:       "Direct connection",
			remoteAddr: "192.168.1.100:54321",
			expectedIP: "192.168.1.100",
		},
		{
			name:          "Behind proxy with X-Forwarded-For",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.1, 192.168.1.1",
			expectedIP:    "203.0.113.1",
		},
		{
			name:       "Behind proxy with X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.2",
			expectedIP: "203.0.113.2",
		},
		{
			name:          "X-Forwarded-For takes precedence",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.3",
			xRealIP:       "203.0.113.4",
			expectedIP:    "203.0.113.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			ip := extractClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("Expected IP %s, got %s", tt.expectedIP, ip)
			}
		})
	}
}

func TestAgentRegistrationWithMetadata(t *testing.T) {
	// Test that new metadata fields are properly stored
	server, store := setupTestServer(t)

	// Create tenant and join token, then register agent with metadata using register-with-token
	ctx := context.Background()
	tn := &storage.Tenant{ID: "test-tenant-02", Name: "MetaTenant"}
	if err := store.CreateTenant(ctx, tn); err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}
	_, rawToken, err := store.CreateJoinToken(ctx, tn.ID, 60, true)
	if err != nil {
		t.Fatalf("Failed to create join token: %v", err)
	}

	// Register agent with extended metadata
	reqBody := map[string]interface{}{
		"token":            rawToken,
		"agent_id":         "test-agent-metadata",
		"agent_version":    "v0.3.0",
		"protocol_version": "1",
		"hostname":         "test-metadata-host",
		"ip":               "192.168.1.200",
		"platform":         "linux",
		"os_version":       "Ubuntu 22.04",
		"go_version":       "go1.21.0",
		"architecture":     "amd64",
		"num_cpu":          8,
		"total_memory_mb":  16384,
		"build_type":       "release",
		"git_commit":       "abc123def456",
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL+"/api/v1/agents/register-with-token", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("Expected success=true, got %v", result["success"])
	}

	// Verify all metadata is stored
	agent, err := store.GetAgent(ctx, "test-agent-metadata")
	if err != nil {
		t.Fatalf("Failed to retrieve agent from store: %v", err)
	}

	// Check basic fields
	if agent.AgentID != "test-agent-metadata" {
		t.Errorf("Expected AgentID=test-agent-metadata, got %s", agent.AgentID)
	}
	if agent.Hostname != "test-metadata-host" {
		t.Errorf("Expected Hostname=test-metadata-host, got %s", agent.Hostname)
	}

	// Check metadata fields
	if agent.OSVersion != "Ubuntu 22.04" {
		t.Errorf("Expected OSVersion=Ubuntu 22.04, got %s", agent.OSVersion)
	}
	if agent.GoVersion != "go1.21.0" {
		t.Errorf("Expected GoVersion=go1.21.0, got %s", agent.GoVersion)
	}
	if agent.Architecture != "amd64" {
		t.Errorf("Expected Architecture=amd64, got %s", agent.Architecture)
	}
	if agent.NumCPU != 8 {
		t.Errorf("Expected NumCPU=8, got %d", agent.NumCPU)
	}
	if agent.TotalMemoryMB != 16384 {
		t.Errorf("Expected TotalMemoryMB=16384, got %d", agent.TotalMemoryMB)
	}
	if agent.BuildType != "release" {
		t.Errorf("Expected BuildType=release, got %s", agent.BuildType)
	}
	if agent.GitCommit != "abc123def456" {
		t.Errorf("Expected GitCommit=abc123def456, got %s", agent.GitCommit)
	}
}

func TestAgentDetailsEndpoint(t *testing.T) {
	// Test the /api/v1/agents/{id} endpoint
	_, store := setupTestServer(t)
	ctx := context.Background()

	// Create test agent with full metadata
	agent := &storage.Agent{
		AgentID:         "test-agent-details",
		Hostname:        "details-host",
		IP:              "192.168.1.150",
		Platform:        "darwin",
		Version:         "v0.3.0",
		ProtocolVersion: "1",
		Token:           "details-token-123",
		RegisteredAt:    time.Now().Add(-24 * time.Hour),
		LastSeen:        time.Now(),
		Status:          "active",
		OSVersion:       "macOS 14.0",
		GoVersion:       "go1.21.0",
		Architecture:    "arm64",
		NumCPU:          10,
		TotalMemoryMB:   32768,
		BuildType:       "release",
		GitCommit:       "xyz789abc123",
		LastHeartbeat:   time.Now().Add(-5 * time.Minute),
		DeviceCount:     5,
		LastDeviceSync:  time.Now().Add(-10 * time.Minute),
		LastMetricsSync: time.Now().Add(-15 * time.Minute),
	}
	if err := store.RegisterAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Setup the agent details handler
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/", handleAgentDetails)
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Fetch agent details
	resp, err := http.Get(testServer.URL + "/api/v1/agents/test-agent-details")
	if err != nil {
		t.Fatalf("Failed to fetch agent details: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result storage.Agent
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify all fields are returned
	if result.AgentID != "test-agent-details" {
		t.Errorf("Expected AgentID=test-agent-details, got %s", result.AgentID)
	}
	if result.Hostname != "details-host" {
		t.Errorf("Expected Hostname=details-host, got %s", result.Hostname)
	}
	if result.OSVersion != "macOS 14.0" {
		t.Errorf("Expected OSVersion=macOS 14.0, got %s", result.OSVersion)
	}
	if result.Architecture != "arm64" {
		t.Errorf("Expected Architecture=arm64, got %s", result.Architecture)
	}
	if result.NumCPU != 10 {
		t.Errorf("Expected NumCPU=10, got %d", result.NumCPU)
	}
	if result.TotalMemoryMB != 32768 {
		t.Errorf("Expected TotalMemoryMB=32768, got %d", result.TotalMemoryMB)
	}

	// Verify token is not exposed
	if result.Token != "" {
		t.Error("Token should not be exposed in API response")
	}
}

func TestAgentDetailsNotFound(t *testing.T) {
	t.Parallel()

	_, _ = setupTestServer(t)

	// Setup the agent details handler
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/", handleAgentDetails)
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	// Try to fetch non-existent agent
	resp, err := http.Get(testServer.URL + "/api/v1/agents/non-existent-agent")
	if err != nil {
		t.Fatalf("Failed to fetch agent details: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

func TestAgentsListEndpoint(t *testing.T) {
	// Test the /api/v1/agents/list endpoint
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Create multiple test agents
	agents := []*storage.Agent{
		{
			AgentID:         "list-agent-01",
			Hostname:        "host-01",
			IP:              "192.168.1.10",
			Platform:        "windows",
			Version:         "v0.3.0",
			ProtocolVersion: "1",
			Token:           "token-01",
			RegisteredAt:    time.Now(),
			LastSeen:        time.Now(),
			Status:          "active",
		},
		{
			AgentID:         "list-agent-02",
			Hostname:        "host-02",
			IP:              "192.168.1.11",
			Platform:        "linux",
			Version:         "v0.3.0",
			ProtocolVersion: "1",
			Token:           "token-02",
			RegisteredAt:    time.Now(),
			LastSeen:        time.Now().Add(-30 * time.Minute),
			Status:          "inactive",
		},
	}

	for _, agent := range agents {
		if err := store.RegisterAgent(ctx, agent); err != nil {
			t.Fatalf("Failed to register agent: %v", err)
		}
	}

	// Fetch agents list
	resp, err := http.Get(server.URL + "/api/v1/agents/list")
	if err != nil {
		t.Fatalf("Failed to fetch agents list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result []*storage.Agent
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have at least 2 agents (may have more from other tests)
	if len(result) < 2 {
		t.Errorf("Expected at least 2 agents, got %d", len(result))
	}

	// Verify tokens are not exposed
	for _, agent := range result {
		if agent.Token != "" {
			t.Errorf("Token should not be exposed for agent %s", agent.AgentID)
		}
	}
}

func TestAgentsListConnectionType(t *testing.T) {
	// Not parallel due to global wsConnections map
	server, store := setupTestServer(t)
	ctx := context.Background()

	// Agent with active WS connection
	wsAgent := &storage.Agent{
		AgentID:      "ws-agent",
		Hostname:     "ws-host",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
		Status:       "active",
		Token:        "t-ws",
	}
	// Agent with recent HTTP heartbeat
	httpAgent := &storage.Agent{
		AgentID:      "http-agent",
		Hostname:     "http-host",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now().Add(-30 * time.Second),
		Status:       "active",
		Token:        "t-http",
	}
	// Agent offline
	offAgent := &storage.Agent{
		AgentID:      "off-agent",
		Hostname:     "off-host",
		RegisteredAt: time.Now(),
		LastSeen:     time.Now().Add(-10 * time.Minute),
		Status:       "offline",
		Token:        "t-off",
	}

	if err := store.RegisterAgent(ctx, wsAgent); err != nil {
		t.Fatalf("failed to register ws agent: %v", err)
	}
	if err := store.RegisterAgent(ctx, httpAgent); err != nil {
		t.Fatalf("failed to register http agent: %v", err)
	}
	if err := store.RegisterAgent(ctx, offAgent); err != nil {
		t.Fatalf("failed to register off agent: %v", err)
	}

	// Simulate ws connection by inserting key into wsConnections map
	wsConnectionsLock.Lock()
	wsConnections["ws-agent"] = nil // presence matters, value may be nil in tests
	wsConnectionsLock.Unlock()

	resp, err := http.Get(server.URL + "/api/v1/agents/list")
	if err != nil {
		t.Fatalf("Failed to fetch agents list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var list []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Build lookup by agent_id
	byID := map[string]map[string]interface{}{}
	for _, obj := range list {
		if id, ok := obj["agent_id"].(string); ok {
			byID[id] = obj
		}
	}

	// ws-agent should be 'ws'
	if obj, ok := byID["ws-agent"]; ok {
		if ct, _ := obj["connection_type"].(string); ct != "ws" {
			t.Errorf("ws-agent connection_type expected 'ws', got '%s'", ct)
		}
	} else {
		t.Errorf("ws-agent missing from list response")
	}

	// http-agent should be 'http'
	if obj, ok := byID["http-agent"]; ok {
		if ct, _ := obj["connection_type"].(string); ct != "http" {
			t.Errorf("http-agent connection_type expected 'http', got '%s'", ct)
		}
	} else {
		t.Errorf("http-agent missing from list response")
	}

	// off-agent should be 'none'
	if obj, ok := byID["off-agent"]; ok {
		if ct, _ := obj["connection_type"].(string); ct != "none" {
			t.Errorf("off-agent connection_type expected 'none', got '%s'", ct)
		}
	} else {
		t.Errorf("off-agent missing from list response")
	}
}
