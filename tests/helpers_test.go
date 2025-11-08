package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

// Helper: Get a free TCP port
func getFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Small delay to ensure port is released
	time.Sleep(100 * time.Millisecond)

	return port
}

// Helper: Wait for HTTP server to be ready
func waitForHTTPServer(t *testing.T, url string, timeout time.Duration) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			resp, err := http.Get(url + "/api/health")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true
				}
			}
		}
	}
}

// Helper: Check if agent is registered and active
func isAgentActive(t *testing.T, serverURL, agentID string) bool {
	t.Helper()

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/agents/%s", serverURL, agentID))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var agent map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		return false
	}

	status, ok := agent["status"].(string)
	return ok && status == "active"
}

// Helper: Make authenticated request to server
func makeAuthRequest(t *testing.T, method, url, token string, body io.Reader) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return http.DefaultClient.Do(req)
}
