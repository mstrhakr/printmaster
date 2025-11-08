package tests

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWebSocketProxy_BasicFlow tests the complete WebSocket proxy flow in-memory
func TestWebSocketProxy_BasicFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	// Create a mock device/agent web UI
	targetUI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<html><body>Test UI - Path: %s</body></html>", r.URL.Path)
	}))
	defer targetUI.Close()

	// Create mock agent that handles WebSocket proxy requests
	agentHandler := &mockAgentWebSocket{
		agentID:   "test-agent-123",
		targetURL: targetUI.URL,
	}

	agentServer := httptest.NewServer(http.HandlerFunc(agentHandler.handleWebSocket))
	defer agentServer.Close()

	t.Logf("Mock agent WebSocket server: %s", agentServer.URL)
	t.Logf("Target UI server: %s", targetUI.URL)

	// Test that we can connect
	wsURL := "ws" + strings.TrimPrefix(agentServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send a proxy request
	requestID := "test-request-1"
	proxyReq := map[string]interface{}{
		"type": "proxy_request",
		"data": map[string]interface{}{
			"request_id": requestID,
			"url":        targetUI.URL + "/test",
			"method":     "GET",
			"headers":    map[string]string{},
		},
		"timestamp": time.Now(),
	}

	if err := conn.WriteJSON(proxyReq); err != nil {
		t.Fatalf("Failed to send proxy request: %v", err)
	}

	// Read the response
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Verify response
	if response["type"] != "proxy_response" {
		t.Errorf("Expected type=proxy_response, got %v", response["type"])
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("Response data is not a map")
	}

	if data["request_id"] != requestID {
		t.Errorf("Expected request_id=%s, got %v", requestID, data["request_id"])
	}

	statusCode, ok := data["status_code"].(float64)
	if !ok || int(statusCode) != 200 {
		t.Errorf("Expected status_code=200, got %v", data["status_code"])
	}

	t.Log("✓ WebSocket proxy flow completed successfully")
}

// TestWebSocketProxy_UnreachableTarget tests error handling when target is unreachable
func TestWebSocketProxy_UnreachableTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	// Create mock agent that tries to connect to non-existent target
	agentHandler := &mockAgentWebSocket{
		agentID:   "test-agent-456",
		targetURL: "http://localhost:1", // Invalid target
	}

	agentServer := httptest.NewServer(http.HandlerFunc(agentHandler.handleWebSocket))
	defer agentServer.Close()

	// Connect to WebSocket
	wsURL := "ws" + strings.TrimPrefix(agentServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send proxy request to unreachable target
	requestID := "test-request-error"
	proxyReq := map[string]interface{}{
		"type": "proxy_request",
		"data": map[string]interface{}{
			"request_id": requestID,
			"url":        "http://localhost:1/test",
			"method":     "GET",
			"headers":    map[string]string{},
		},
		"timestamp": time.Now(),
	}

	if err := conn.WriteJSON(proxyReq); err != nil {
		t.Fatalf("Failed to send proxy request: %v", err)
	}

	// Read the error response
	var response map[string]interface{}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Verify error response
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("Response data is not a map")
	}

	statusCode, ok := data["status_code"].(float64)
	if !ok || int(statusCode) != 502 {
		t.Errorf("Expected status_code=502 (Bad Gateway), got %v", data["status_code"])
	}

	t.Log("✓ Correctly handled connection error with 502 status")
}

// TestWebSocketProxy_MultipleRequests tests handling multiple concurrent proxy requests
func TestWebSocketProxy_MultipleRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}
	t.Parallel()

	// Create a mock target
	targetUI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Response for: %s", r.URL.Path)
	}))
	defer targetUI.Close()

	agentHandler := &mockAgentWebSocket{
		agentID:   "test-agent-789",
		targetURL: targetUI.URL,
	}

	agentServer := httptest.NewServer(http.HandlerFunc(agentHandler.handleWebSocket))
	defer agentServer.Close()

	// Connect to WebSocket
	wsURL := "ws" + strings.TrimPrefix(agentServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send multiple requests
	requestIDs := []string{"req-1", "req-2", "req-3"}
	for _, reqID := range requestIDs {
		proxyReq := map[string]interface{}{
			"type": "proxy_request",
			"data": map[string]interface{}{
				"request_id": reqID,
				"url":        targetUI.URL + "/" + reqID,
				"method":     "GET",
				"headers":    map[string]string{},
			},
			"timestamp": time.Now(),
		}

		if err := conn.WriteJSON(proxyReq); err != nil {
			t.Fatalf("Failed to send proxy request %s: %v", reqID, err)
		}
	}

	// Read all responses
	receivedIDs := make(map[string]bool)
	for i := 0; i < len(requestIDs); i++ {
		var response map[string]interface{}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := conn.ReadJSON(&response); err != nil {
			t.Fatalf("Failed to read response %d: %v", i, err)
		}

		data, ok := response["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("Response %d data is not a map", i)
		}

		reqID, ok := data["request_id"].(string)
		if !ok {
			t.Fatalf("Response %d has no request_id", i)
		}

		receivedIDs[reqID] = true
	}

	// Verify all requests were handled
	for _, reqID := range requestIDs {
		if !receivedIDs[reqID] {
			t.Errorf("Did not receive response for request %s", reqID)
		}
	}

	t.Logf("✓ Successfully handled %d concurrent requests", len(requestIDs))
}

// mockAgentWebSocket simulates an agent's WebSocket handler with proxy capability
type mockAgentWebSocket struct {
	agentID   string
	targetURL string
}

func (m *mockAgentWebSocket) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Handle messages
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			break
		}

		msgType, ok := msg["type"].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "proxy_request":
			m.handleProxyRequest(conn, msg)
		}
	}
}

func (m *mockAgentWebSocket) handleProxyRequest(conn *websocket.Conn, msg map[string]interface{}) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		return
	}

	requestID, _ := data["request_id"].(string)
	url, _ := data["url"].(string)
	method, _ := data["method"].(string)

	// Make HTTP request to target
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		m.sendProxyError(conn, requestID, err.Error())
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		m.sendProxyError(conn, requestID, err.Error())
		return
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		m.sendProxyError(conn, requestID, err.Error())
		return
	}

	// Extract headers
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// Send successful response
	response := map[string]interface{}{
		"type": "proxy_response",
		"data": map[string]interface{}{
			"request_id":  requestID,
			"status_code": resp.StatusCode,
			"headers":     headers,
			"body":        string(body), // For simplicity, not base64 encoded in test
		},
		"timestamp": time.Now(),
	}

	conn.WriteJSON(response)
}

func (m *mockAgentWebSocket) sendProxyError(conn *websocket.Conn, requestID, errorMsg string) {
	response := map[string]interface{}{
		"type": "proxy_response",
		"data": map[string]interface{}{
			"request_id":  requestID,
			"status_code": 502,
			"headers": map[string]string{
				"Content-Type": "text/plain",
			},
			"body": errorMsg,
		},
		"timestamp": time.Now(),
	}
	conn.WriteJSON(response)
}
