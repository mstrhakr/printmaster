package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthAPI_HandleHealth(t *testing.T) {
	t.Parallel()

	api := NewHealthAPI(HealthAPIOptions{
		Version:   "1.0.0",
		BuildTime: "2024-01-01",
		GitCommit: "abc123",
		BuildType: "test",
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	api.HandleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if status, ok := resp["status"].(string); !ok || status != "healthy" {
		t.Errorf("expected status=healthy, got %v", resp["status"])
	}

	if _, ok := resp["timestamp"]; !ok {
		t.Error("expected timestamp in response")
	}
}

func TestHealthAPI_HandleVersion(t *testing.T) {
	t.Parallel()

	tenancyEnabled := true
	api := NewHealthAPI(HealthAPIOptions{
		Version:         "1.0.0",
		BuildTime:       "2024-01-01",
		GitCommit:       "abc123",
		BuildType:       "test",
		ProtocolVersion: "1",
		ProcessStart:    time.Now(),
		TenancyChecker:  func() bool { return tenancyEnabled },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()

	api.HandleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify all expected fields are present
	expectedFields := []string{
		"version", "build_time", "git_commit", "build_type",
		"protocol_version", "go_version", "os", "arch",
		"tenancy_enabled", "uptime",
	}
	for _, field := range expectedFields {
		if _, ok := resp[field]; !ok {
			t.Errorf("expected %s in response", field)
		}
	}

	if resp["version"] != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %v", resp["version"])
	}

	if resp["tenancy_enabled"] != true {
		t.Errorf("expected tenancy_enabled=true, got %v", resp["tenancy_enabled"])
	}
}

func TestHealthAPI_RegisterRoutes(t *testing.T) {
	t.Parallel()

	api := NewHealthAPI(HealthAPIOptions{
		Version: "1.0.0",
	})

	mux := http.NewServeMux()
	api.RegisterRoutes(mux)

	// Test /health route is registered
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /health to return 200, got %d", w.Code)
	}

	// Test /api/version route is registered
	req = httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected /api/version to return 200, got %d", w.Code)
	}
}

func TestRunHealthCheck_NoEndpoints(t *testing.T) {
	t.Parallel()

	// Test with invalid port (will fail to connect)
	err := RunHealthCheck(HealthCheckConfig{
		HTTPPort:  0,
		HTTPSPort: 0,
	})

	// Should fail because no endpoints are available to connect to
	if err == nil {
		t.Error("expected error when no valid endpoints configured")
	}
}
