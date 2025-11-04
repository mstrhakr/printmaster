package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServerClient_Register(t *testing.T) {
	t.Parallel()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/register" {
			t.Errorf("Expected path /api/v1/agents/register, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}

		// Decode request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Verify fields
		if req["agent_id"] != "test-agent" {
			t.Errorf("Expected agent_id=test-agent, got %v", req["agent_id"])
		}

		// Return token
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"agent_id": "test-agent",
			"token":    "test-token-123",
		})
	}))
	defer server.Close()

	// Create client
	client := NewServerClient(server.URL, "test-agent", "")

	// Register
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := client.Register(ctx, "v0.2.0")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if token != "test-token-123" {
		t.Errorf("Expected token=test-token-123, got %s", token)
	}

	// Verify token was stored
	if client.GetToken() != "test-token-123" {
		t.Errorf("Token not stored in client")
	}
}

func TestServerClient_Heartbeat(t *testing.T) {
	t.Parallel()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agents/heartbeat" {
			t.Errorf("Expected path /api/v1/agents/heartbeat, got %s", r.URL.Path)
		}

		// Verify Bearer token
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-456" {
			t.Errorf("Expected Bearer test-token-456, got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})
	}))
	defer server.Close()

	// Create client with token
	client := NewServerClient(server.URL, "test-agent", "test-token-456")

	// Send heartbeat
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Heartbeat(ctx)
	if err != nil {
		t.Fatalf("Heartbeat failed: %v", err)
	}
}

func TestServerClient_UploadDevices(t *testing.T) {
	t.Parallel()

	// Create mock server
	receivedDevices := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/devices/batch" {
			t.Errorf("Expected path /api/v1/devices/batch, got %s", r.URL.Path)
		}

		// Verify Bearer token
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-789" {
			t.Errorf("Expected Bearer test-token-789, got %s", auth)
		}

		// Decode request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Verify devices
		devices, ok := req["devices"].([]interface{})
		if !ok {
			t.Error("Missing or invalid devices field")
		}
		receivedDevices = len(devices)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"received": len(devices),
		})
	}))
	defer server.Close()

	// Create client
	client := NewServerClient(server.URL, "test-agent", "test-token-789")

	// Upload devices
	devices := []interface{}{
		map[string]interface{}{
			"serial":       "DEV001",
			"ip":           "192.168.1.50",
			"manufacturer": "HP",
		},
		map[string]interface{}{
			"serial":       "DEV002",
			"ip":           "192.168.1.51",
			"manufacturer": "Canon",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.UploadDevices(ctx, devices)
	if err != nil {
		t.Fatalf("UploadDevices failed: %v", err)
	}

	if receivedDevices != 2 {
		t.Errorf("Expected 2 devices uploaded, got %d", receivedDevices)
	}
}

func TestServerClient_UploadMetrics(t *testing.T) {
	t.Parallel()

	// Create mock server
	receivedMetrics := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/metrics/batch" {
			t.Errorf("Expected path /api/v1/metrics/batch, got %s", r.URL.Path)
		}

		// Decode request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Failed to decode request: %v", err)
		}

		// Verify metrics
		metrics, ok := req["metrics"].([]interface{})
		if !ok {
			t.Error("Missing or invalid metrics field")
		}
		receivedMetrics = len(metrics)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"received": len(metrics),
		})
	}))
	defer server.Close()

	// Create client
	client := NewServerClient(server.URL, "test-agent", "test-token-abc")

	// Upload metrics
	metrics := []interface{}{
		map[string]interface{}{
			"serial":     "DEV001",
			"page_count": 1000,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.UploadMetrics(ctx, metrics)
	if err != nil {
		t.Fatalf("UploadMetrics failed: %v", err)
	}

	if receivedMetrics != 1 {
		t.Errorf("Expected 1 metric uploaded, got %d", receivedMetrics)
	}
}

func TestServerClient_Unauthorized(t *testing.T) {
	t.Parallel()

	// Create mock server that returns 401
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Invalid token"))
	}))
	defer server.Close()

	// Create client with bad token
	client := NewServerClient(server.URL, "test-agent", "bad-token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Heartbeat should fail
	err := client.Heartbeat(ctx)
	if err == nil {
		t.Error("Expected heartbeat to fail with bad token")
	}
}

func TestServerClient_Timeout(t *testing.T) {
	t.Parallel()

	// Create mock server that hangs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Longer than test timeout
	}))
	defer server.Close()

	// Create client
	client := NewServerClient(server.URL, "test-agent", "test-token")

	// Use short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should timeout
	err := client.Heartbeat(ctx)
	if err == nil {
		t.Error("Expected heartbeat to timeout")
	}
}

func TestServerClient_SetGetToken(t *testing.T) {
	t.Parallel()

	client := NewServerClient("http://localhost:9090", "test-agent", "initial-token")

	// Verify initial token
	if client.GetToken() != "initial-token" {
		t.Errorf("Expected initial-token, got %s", client.GetToken())
	}

	// Set new token
	client.SetToken("new-token")

	// Verify new token
	if client.GetToken() != "new-token" {
		t.Errorf("Expected new-token, got %s", client.GetToken())
	}

	// Test thread safety with concurrent gets/sets
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				client.SetToken("token-" + string(rune(id)))
				_ = client.GetToken()
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic (thread safety test)
}
