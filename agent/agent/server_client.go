package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

// ServerClient handles uploading agent data to the central PrintMaster server
// This is the agent's HTTP client for server communication
type ServerClient struct {
	BaseURL            string
	AgentID            string
	AgentName          string // User-friendly agent name
	Token              string
	HTTPClient         *http.Client
	InsecureSkipVerify bool
	mu                 sync.RWMutex
	lastHeartbeat      time.Time
	lastDeviceUpload   time.Time
	lastMetricsUpload  time.Time
}

// NewServerClient creates a new server uploader for this agent
// If caCertPath is provided, uses it to validate server certificate (for self-signed certs)
// If caCertPath is empty, uses system CA pool (works with Let's Encrypt)
func NewServerClient(baseURL, agentID, token string) *ServerClient {
	return NewServerClientWithName(baseURL, agentID, "", token, "", false)
}

// NewServerClientWithName creates a new server client with agent name
func NewServerClientWithName(baseURL, agentID, agentName, token, caCertPath string, insecureSkipVerify bool) *ServerClient {
	// Use agent package logger for structured logging when available
	Info(fmt.Sprintf("NewServerClientWithName baseURL=%s insecureSkipVerify=%v caCertPath=%s", baseURL, insecureSkipVerify, caCertPath))
	var tlsConfig *tls.Config

	if caCertPath != "" {
		// Custom CA (self-signed server certificate)
		caCert, err := os.ReadFile(caCertPath)
		if err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig = &tls.Config{
					RootCAs:            caCertPool,
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: insecureSkipVerify,
				}
			}
		}
	}

	if tlsConfig == nil {
		// Use system CA pool (works with Let's Encrypt and other public CAs)
		tlsConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify,
		}
	}

	return &ServerClient{
		BaseURL:            baseURL,
		AgentID:            agentID,
		AgentName:          agentName,
		Token:              token,
		InsecureSkipVerify: insecureSkipVerify,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

// NewServerClientWithCA creates a new server client with optional custom CA certificate
func NewServerClientWithCA(baseURL, agentID, token, caCertPath string) *ServerClient {
	return NewServerClientWithName(baseURL, agentID, "", token, caCertPath, false)
}

// NewServerClientWithCAAndSkipVerify creates a new server client with optional custom CA and skip verify option
func NewServerClientWithCAAndSkipVerify(baseURL, agentID, token, caCertPath string, insecureSkipVerify bool) *ServerClient {
	return NewServerClientWithName(baseURL, agentID, "", token, caCertPath, insecureSkipVerify)
}

// IsInsecureSkipVerify returns whether this client was configured to skip TLS verification.
func (c *ServerClient) IsInsecureSkipVerify() bool {
	return c.InsecureSkipVerify
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

// GetServerURL retrieves the base server URL
func (c *ServerClient) GetServerURL() string {
	return c.BaseURL
}

// Register performs initial agent registration with the server
// Returns the authentication token on success
func (c *ServerClient) Register(ctx context.Context, version string) (string, error) {
	type RegisterRequest struct {
		AgentID         string `json:"agent_id"`
		Name            string `json:"name,omitempty"` // User-friendly name
		AgentVersion    string `json:"agent_version"`
		ProtocolVersion string `json:"protocol_version"`
		Hostname        string `json:"hostname"`
		IP              string `json:"ip"`
		Platform        string `json:"platform"`
		// Additional metadata
		OSVersion     string `json:"os_version,omitempty"`
		GoVersion     string `json:"go_version,omitempty"`
		Architecture  string `json:"architecture,omitempty"`
		NumCPU        int    `json:"num_cpu,omitempty"`
		TotalMemoryMB int64  `json:"total_memory_mb,omitempty"`
		BuildType     string `json:"build_type,omitempty"`
		GitCommit     string `json:"git_commit,omitempty"`
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
		Name:            c.AgentName, // Use client's agent name
		AgentVersion:    version,
		ProtocolVersion: "1",
		Hostname:        hostname,
		IP:              localIP,
		Platform:        runtime.GOOS,
		OSVersion:       getOSVersion(),
		GoVersion:       runtime.Version(),
		Architecture:    runtime.GOARCH,
		NumCPU:          runtime.NumCPU(),
		TotalMemoryMB:   getTotalMemoryMB(),
		BuildType:       getBuildType(),
		GitCommit:       getGitCommit(),
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

// RegisterWithToken registers the agent using a join token issued by the server.
// On success it returns the agent-scoped token issued by the server and the
// tenant ID the agent was assigned to. The returned agent token is also stored
// on the client instance for future requests.
func (c *ServerClient) RegisterWithToken(ctx context.Context, joinToken string, version string) (string, string, error) {
	type JoinRequest struct {
		Token         string `json:"token"`
		AgentID       string `json:"agent_id"`
		Name          string `json:"name,omitempty"`
		AgentVersion  string `json:"agent_version,omitempty"`
		ProtocolVersion string `json:"protocol_version,omitempty"`
	}

	type JoinResponse struct {
		Success   bool   `json:"success"`
		TenantID  string `json:"tenant_id"`
		AgentToken string `json:"agent_token"`
		Message   string `json:"message,omitempty"`
	}

	hostname, _ := getHostname()

	req := JoinRequest{
		Token:           joinToken,
		AgentID:         c.AgentID,
		Name:            c.AgentName,
		AgentVersion:    version,
		ProtocolVersion: "1",
	}

	var resp JoinResponse
	if err := c.doRequest(ctx, "POST", "/api/v1/agents/register-with-token", req, &resp, false); err != nil {
		return "", "", fmt.Errorf("register-with-token failed: %w", err)
	}

	if !resp.Success {
		return "", "", fmt.Errorf("register-with-token failed: %s", resp.Message)
	}

	// Store token for future authenticated requests
	if resp.AgentToken != "" {
		c.SetToken(resp.AgentToken)
	}

	// If Name wasn't set, and server returned success, ensure we have hostname set
	if c.AgentName == "" {
		c.AgentName = hostname
	}

	return resp.AgentToken, resp.TenantID, nil
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

	// Perform request (log for debugging)
	tokenPresent := false
	if requireAuth {
		token := c.GetToken()
		tokenPresent = token != ""
	}
	Debug(fmt.Sprintf("HTTP request: method=%s url=%s requireAuth=%v tokenPresent=%v", method, url, requireAuth, tokenPresent))

	// Perform request
	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		Error(fmt.Sprintf("HTTP request failed: %v", err))
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
		Error(fmt.Sprintf("Server returned non-2xx status %d for %s %s: %s", httpResp.StatusCode, method, url, string(respData)))
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

// getOSVersion returns the operating system version
func getOSVersion() string {
	// This is a placeholder - actual implementation would use platform-specific APIs
	// For now, just return the OS name
	return runtime.GOOS
}

// getTotalMemoryMB returns the total system memory in MB
func getTotalMemoryMB() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// This returns allocated memory, not total system memory
	// For actual total system memory, would need platform-specific syscalls
	return int64(m.Sys / 1024 / 1024)
}

// These variables will be set at build time via -ldflags
var (
	buildType = "dev"
	gitCommit = "unknown"
)

// getBuildType returns the build type (dev or release)
func getBuildType() string {
	return buildType
}

// getGitCommit returns the git commit hash
func getGitCommit() string {
	return gitCommit
}
