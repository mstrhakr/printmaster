package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// ServerClient handles uploading agent data to the central PrintMaster server
// This is the agent's HTTP client for server communication
type ServerClient struct {
	BaseURL           string
	AgentID           string
	Token             string
	HTTPClient        *http.Client
	mu                sync.RWMutex
	lastHeartbeat     time.Time
	lastDeviceUpload  time.Time
	lastMetricsUpload time.Time
}

// NewServerClient creates a new server uploader for this agent
func NewServerClient(baseURL, agentID, token string) *ServerClient {
	return &ServerClient{
		BaseURL: baseURL,
		AgentID: agentID,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetToken updates the authentication token
func (c *ServerClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Token = token
}

// GetToken retrieves the current authentication token
func (c *ServerClient) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Token
}

// Register performs initial agent registration with the server
// Returns the authentication token on success
func (c *ServerClient) Register(ctx context.Context, version string) (string, error) {
	type RegisterRequest struct {
		AgentID         string `json:"agent_id"`
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion string `json:"protocol_version"`
		Hostname        string `json:"hostname"`
		IP              string `json:"ip"`
		Platform        string `json:"platform"`
	}

	type RegisterResponse struct {
		Success bool   `json:"success"`
		AgentID string `json:"agent_id"`
		Token   string `json:"token"`
		Message string `json:"message"`
	}

	hostname, _ := getHostname()
	localIP, _ := getLocalIP()

	req := RegisterRequest{
		AgentID:         c.AgentID,
		AgentVersion:    version,
		ProtocolVersion: "1",
		Hostname:        hostname,
		IP:              localIP,
		Platform:        runtime.GOOS,
	}

	var resp RegisterResponse
	if err := c.doRequest(ctx, "POST", "/api/v1/agents/register", req, &resp, false); err != nil {
		return "", fmt.Errorf("registration failed: %w", err)
	}

	if !resp.Success {
		return "", fmt.Errorf("registration failed: %s", resp.Message)
	}

	// Store token for future requests
	if resp.Token != "" {
		c.SetToken(resp.Token)
	}

	return resp.Token, nil
}

// Heartbeat sends a keep-alive signal to the server
func (c *ServerClient) Heartbeat(ctx context.Context) error {
	type HeartbeatRequest struct {
		AgentID   string    `json:"agent_id"`
		Timestamp time.Time `json:"timestamp"`
		Status    string    `json:"status"`
	}

	req := HeartbeatRequest{
		AgentID:   c.AgentID,
		Timestamp: time.Now(),
		Status:    "active",
	}

	var resp map[string]interface{}
	if err := c.doRequest(ctx, "POST", "/api/v1/agents/heartbeat", req, &resp, true); err != nil {
		return fmt.Errorf("heartbeat failed: %w", err)
	}

	c.mu.Lock()
	c.lastHeartbeat = time.Now()
	c.mu.Unlock()

	return nil
}

// UploadDevices sends discovered devices to the server
func (c *ServerClient) UploadDevices(ctx context.Context, devices []interface{}) error {
	type DevicesBatchRequest struct {
		AgentID   string        `json:"agent_id"`
		Timestamp time.Time     `json:"timestamp"`
		Devices   []interface{} `json:"devices"`
	}

	req := DevicesBatchRequest{
		AgentID:   c.AgentID,
		Timestamp: time.Now(),
		Devices:   devices,
	}

	var resp map[string]interface{}
	if err := c.doRequest(ctx, "POST", "/api/v1/devices/batch", req, &resp, true); err != nil {
		return fmt.Errorf("device upload failed: %w", err)
	}

	c.mu.Lock()
	c.lastDeviceUpload = time.Now()
	c.mu.Unlock()

	return nil
}

// UploadMetrics sends device metrics to the server
func (c *ServerClient) UploadMetrics(ctx context.Context, metrics []interface{}) error {
	type MetricsBatchRequest struct {
		AgentID   string        `json:"agent_id"`
		Timestamp time.Time     `json:"timestamp"`
		Metrics   []interface{} `json:"metrics"`
	}

	req := MetricsBatchRequest{
		AgentID:   c.AgentID,
		Timestamp: time.Now(),
		Metrics:   metrics,
	}

	var resp map[string]interface{}
	if err := c.doRequest(ctx, "POST", "/api/v1/metrics/batch", req, &resp, true); err != nil {
		return fmt.Errorf("metrics upload failed: %w", err)
	}

	c.mu.Lock()
	c.lastMetricsUpload = time.Now()
	c.mu.Unlock()

	return nil
}

// LogAuditEvent sends an audit log entry to the server
func (c *ServerClient) LogAuditEvent(ctx context.Context, action, resourceType, resourceID string, details map[string]interface{}) error {
	type AuditRequest struct {
		AgentID      string                 `json:"agent_id"`
		Timestamp    time.Time              `json:"timestamp"`
		Action       string                 `json:"action"`
		ResourceType string                 `json:"resource_type"`
		ResourceID   string                 `json:"resource_id"`
		Details      map[string]interface{} `json:"details"`
	}

	req := AuditRequest{
		AgentID:      c.AgentID,
		Timestamp:    time.Now(),
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Details:      details,
	}

	var resp map[string]interface{}
	if err := c.doRequest(ctx, "POST", "/api/v1/audit/log", req, &resp, true); err != nil {
		// Don't fail the operation if audit logging fails, just log it
		return fmt.Errorf("audit log failed: %w", err)
	}

	return nil
}

// GetStats returns client statistics
func (c *ServerClient) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"last_heartbeat":      c.lastHeartbeat,
		"last_device_upload":  c.lastDeviceUpload,
		"last_metrics_upload": c.lastMetricsUpload,
		"has_token":           c.Token != "",
	}
}

// doRequest performs an HTTP request with optional authentication
func (c *ServerClient) doRequest(ctx context.Context, method, path string, reqBody, respBody interface{}, requireAuth bool) error {
	url := c.BaseURL + path

	// Encode request body
	var bodyReader io.Reader
	if reqBody != nil {
		jsonData, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "PrintMaster-Agent/1.0")

	// Add authentication if required and token available
	if requireAuth {
		token := c.GetToken()
		if token == "" {
			return fmt.Errorf("authentication required but no token available")
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	// Perform request
	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("server returned status %d: %s", httpResp.StatusCode, string(respData))
	}

	// Decode response if needed
	if respBody != nil {
		if err := json.Unmarshal(respData, respBody); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}

// Helper functions

func getHostname() (string, error) {
	// Try to import os package
	hostname, err := getHostnameInternal()
	if err != nil || hostname == "" {
		return "unknown", err
	}
	return hostname, nil
}

func getLocalIP() (string, error) {
	// Try to get local IP
	ip, err := getLocalIPInternal()
	if err != nil || ip == "" {
		return "unknown", err
	}
	return ip, nil
}

// These will be implemented in helpers.go or can be simple stubs
var getHostnameInternal = func() (string, error) {
	// Implementation will use os.Hostname()
	return "agent-host", nil
}

var getLocalIPInternal = func() (string, error) {
	// Implementation will use net.InterfaceAddrs()
	return "192.168.1.100", nil
}
