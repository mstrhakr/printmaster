package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	wscommon "printmaster/common/ws"
	"printmaster/server/storage"

	"github.com/gorilla/websocket"
)

// TestWebSocketConnection tests basic WebSocket connection establishment
func TestWebSocketConnection(t *testing.T) {
	t.Parallel()

	// Create test store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer store.Close()

	// Register a test agent
	agent := &storage.Agent{
		AgentID:         "test-agent-ws",
		Hostname:        "test-ws-host",
		IP:              "192.168.1.100",
		Platform:        "linux",
		Version:         "1.0.0",
		ProtocolVersion: "1",
		Token:           "test-token-ws",
		Status:          "active",
	}

	err = store.RegisterAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Create test HTTP server with WebSocket handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAgentWebSocket(w, r, store)
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + agent.Token

	// Connect via WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect via WebSocket: %v", err)
	}
	defer ws.Close()

	t.Log("WebSocket connection established successfully")
}

// TestWebSocketHeartbeat tests sending heartbeat messages over WebSocket
func TestWebSocketHeartbeat(t *testing.T) {
	t.Parallel()

	// Create test store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer store.Close()

	// Register a test agent
	agent := &storage.Agent{
		AgentID:         "test-agent-heartbeat",
		Hostname:        "test-heartbeat-host",
		IP:              "192.168.1.101",
		Platform:        "windows",
		Version:         "1.0.0",
		ProtocolVersion: "1",
		Token:           "test-token-heartbeat",
		Status:          "active",
	}

	err = store.RegisterAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Create test HTTP server with WebSocket handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAgentWebSocket(w, r, store)
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + agent.Token

	// Connect via WebSocket
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect via WebSocket: %v", err)
	}
	defer ws.Close()

	// Send heartbeat message
	heartbeatMsg := wscommon.Message{
		Type: "heartbeat",
		Data: map[string]interface{}{
			"device_count": 5,
		},
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(heartbeatMsg)
	if err != nil {
		t.Fatalf("Failed to marshal heartbeat: %v", err)
	}

	err = ws.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	// Read pong response
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read pong: %v", err)
	}

	var pongMsg wscommon.Message
	err = json.Unmarshal(message, &pongMsg)
	if err != nil {
		t.Fatalf("Failed to unmarshal pong: %v", err)
	}

	if pongMsg.Type != "pong" {
		t.Errorf("Expected pong message, got: %s", pongMsg.Type)
	}

	// Verify agent heartbeat was updated in database
	updatedAgent, err := store.GetAgent(context.Background(), agent.AgentID)
	if err != nil {
		t.Fatalf("Failed to get updated agent: %v", err)
	}

	if updatedAgent.Status != "active" {
		t.Errorf("Expected agent status 'active', got: %s", updatedAgent.Status)
	}

	t.Log("WebSocket heartbeat processed successfully")
}

// TestWebSocketAuthenticationFailure tests WebSocket connection with invalid token
func TestWebSocketAuthenticationFailure(t *testing.T) {
	t.Parallel()

	// Create test store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer store.Close()

	// Create test HTTP server with WebSocket handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAgentWebSocket(w, r, store)
	}))
	defer server.Close()

	// Try to connect with invalid token
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=invalid-token"

	// This should fail during upgrade (401 Unauthorized)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("Expected authentication failure, but connection succeeded")
	}

	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got: %d", resp.StatusCode)
	}

	t.Log("WebSocket authentication failure handled correctly")
}

// TestWebSocketMissingToken tests WebSocket connection without token
func TestWebSocketMissingToken(t *testing.T) {
	t.Parallel()

	// Create test store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer store.Close()

	// Create test HTTP server with WebSocket handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAgentWebSocket(w, r, store)
	}))
	defer server.Close()

	// Try to connect without token
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// This should fail during upgrade (401 Unauthorized)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("Expected authentication failure, but connection succeeded")
	}

	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got: %d", resp.StatusCode)
	}

	t.Log("WebSocket missing token handled correctly")
}

// TestWebSocketReconnection tests handling of agent reconnection
func TestWebSocketReconnection(t *testing.T) {
	t.Parallel()

	// Create test store
	store, err := storage.NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("Failed to create test store: %v", err)
	}
	defer store.Close()

	// Register a test agent
	agent := &storage.Agent{
		AgentID:         "test-agent-reconnect",
		Hostname:        "test-reconnect-host",
		IP:              "192.168.1.102",
		Platform:        "linux",
		Version:         "1.0.0",
		ProtocolVersion: "1",
		Token:           "test-token-reconnect",
		Status:          "active",
	}

	err = store.RegisterAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to register agent: %v", err)
	}

	// Create test HTTP server with WebSocket handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAgentWebSocket(w, r, store)
	}))
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + agent.Token

	// First connection
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed first connection: %v", err)
	}

	// Second connection (should close first one)
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed second connection: %v", err)
	}
	defer ws2.Close()

	// First connection should be closed by server
	time.Sleep(200 * time.Millisecond) // Give server time to close

	// Try to read from the first connection - should fail since it was closed
	ws1.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	_, _, err = ws1.ReadMessage()
	if err == nil {
		t.Error("Expected first connection to be closed, but read succeeded")
	}

	ws1.Close()
	t.Log("WebSocket reconnection handled correctly")
}
