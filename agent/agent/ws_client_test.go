package agent

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// TestWSClientConnection tests basic WebSocket client connection
func TestWSClientConnection(t *testing.T) {
	t.Parallel()

	// Create a test WebSocket server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check token
		token := r.URL.Query().Get("token")
		if token != "test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Simple echo server
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Echo back
			err = conn.WriteMessage(websocket.TextMessage, message)
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	// Create WebSocket client
	serverURL := "http" + strings.TrimPrefix(server.URL, "http")
	logger := log.New(log.Writer(), "[TEST] ", log.LstdFlags)
	client := NewWSClient(serverURL, "test-token", logger)

	// Start client
	err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start WebSocket client: %v", err)
	}
	defer client.Stop()

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Check if connected
	if !client.IsConnected() {
		t.Error("Expected client to be connected")
	}

	t.Log("WebSocket client connected successfully")
}

// TestWSClientHeartbeat tests sending heartbeat messages
func TestWSClientHeartbeat(t *testing.T) {
	t.Parallel()

	receivedHeartbeat := false

	// Create a test WebSocket server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check token
		token := r.URL.Query().Get("token")
		if token != "test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Read heartbeat messages
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			// Parse message
			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				t.Logf("Failed to unmarshal message: %v", err)
				continue
			}

			if msg.Type == MessageTypeHeartbeat {
				receivedHeartbeat = true

				// Send pong response
				pongMsg := WSMessage{
					Type:      MessageTypePong,
					Timestamp: time.Now(),
				}
				payload, _ := json.Marshal(pongMsg)
				conn.WriteMessage(websocket.TextMessage, payload)
			}
		}
	}))
	defer server.Close()

	// Create WebSocket client
	serverURL := "http" + strings.TrimPrefix(server.URL, "http")
	logger := log.New(log.Writer(), "[TEST] ", log.LstdFlags)
	client := NewWSClient(serverURL, "test-token", logger)

	// Start client
	err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start WebSocket client: %v", err)
	}
	defer client.Stop()

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	// Send heartbeat
	heartbeatData := map[string]interface{}{
		"device_count": 10,
	}

	err = client.SendHeartbeat(heartbeatData)
	if err != nil {
		t.Fatalf("Failed to send heartbeat: %v", err)
	}

	// Wait for server to process
	time.Sleep(200 * time.Millisecond)

	if !receivedHeartbeat {
		t.Error("Server did not receive heartbeat")
	}

	t.Log("WebSocket heartbeat sent and received successfully")
}

// TestWSClientReconnection tests automatic reconnection
func TestWSClientReconnection(t *testing.T) {
	t.Parallel()

	connectionCount := 0

	// Create a test WebSocket server that closes connections
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectionCount++

		// Check token
		token := r.URL.Query().Get("token")
		if token != "test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("Upgrade error: %v", err)
			return
		}
		defer conn.Close()

		// Close connection immediately to trigger reconnection
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	// Create WebSocket client with short reconnect delay
	serverURL := "http" + strings.TrimPrefix(server.URL, "http")
	logger := log.New(log.Writer(), "[TEST] ", log.LstdFlags)
	client := NewWSClient(serverURL, "test-token", logger)
	client.reconnectDelay = 500 * time.Millisecond // Short delay for testing

	// Start client
	err := client.Start()
	if err != nil {
		t.Fatalf("Failed to start WebSocket client: %v", err)
	}
	defer client.Stop()

	// Wait for initial connection and reconnections
	time.Sleep(2 * time.Second)

	// Should have reconnected at least once
	if connectionCount < 2 {
		t.Errorf("Expected at least 2 connections (initial + reconnect), got %d", connectionCount)
	}

	t.Logf("WebSocket reconnected successfully (%d connections)", connectionCount)
}

// TestWSClientAuthenticationFailure tests handling of authentication failures
func TestWSClientAuthenticationFailure(t *testing.T) {
	t.Parallel()

	// Create a test WebSocket server that rejects connections
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	// Create WebSocket client with invalid token
	serverURL := "http" + strings.TrimPrefix(server.URL, "http")
	logger := log.New(log.Writer(), "[TEST] ", log.LstdFlags)
	client := NewWSClient(serverURL, "invalid-token", logger)

	// Start client
	err := client.Start()
	// Start doesn't return error immediately - connection happens asynchronously
	if err != nil {
		t.Fatalf("Unexpected error from Start: %v", err)
	}
	defer client.Stop()

	// Wait a bit for connection attempt
	time.Sleep(200 * time.Millisecond)

	// Should not be connected
	if client.IsConnected() {
		t.Error("Expected client to not be connected with invalid token")
	}

	t.Log("WebSocket authentication failure handled correctly")
}
