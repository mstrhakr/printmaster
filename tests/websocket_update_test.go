package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket message types for update commands
const (
	MessageTypeCommand        = "command"
	MessageTypeUpdateProgress = "update_progress"
)

// WSMessage represents a WebSocket message structure
type WSMessage struct {
	Type      string                 `json:"type"`
	Command   string                 `json:"command,omitempty"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// TestWebSocketUpdateCommands tests server-to-agent update commands via WebSocket
func TestWebSocketUpdateCommands(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		command        string
		data           map[string]interface{}
		expectResponse bool
	}{
		{
			name:           "check_update command",
			command:        "check_update",
			data:           nil,
			expectResponse: true,
		},
		{
			name:    "force_reinstall with reason",
			command: "force_reinstall",
			data: map[string]interface{}{
				"reason": "server_admin_triggered",
			},
			expectResponse: true,
		},
		{
			name:           "cancel_update command",
			command:        "cancel_update",
			data:           nil,
			expectResponse: true,
		},
		{
			name:           "restart command",
			command:        "restart",
			data:           nil,
			expectResponse: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var receivedCommand string
			var receivedData map[string]interface{}
			var mu sync.Mutex
			responseSent := false

			// Create mock agent WebSocket server
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool { return true },
			}

			agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Logf("Upgrade error: %v", err)
					return
				}
				defer conn.Close()

				for {
					_, message, err := conn.ReadMessage()
					if err != nil {
						break
					}

					var msg WSMessage
					if err := json.Unmarshal(message, &msg); err != nil {
						t.Logf("Unmarshal error: %v", err)
						continue
					}

					if msg.Type == MessageTypeCommand {
						mu.Lock()
						receivedCommand = msg.Command
						receivedData = msg.Data
						mu.Unlock()

						// Send acknowledgement
						response := WSMessage{
							Type:      MessageTypeUpdateProgress,
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"status":  "checking",
								"message": "Command received: " + msg.Command,
							},
						}
						respBytes, _ := json.Marshal(response)
						conn.WriteMessage(websocket.TextMessage, respBytes)

						mu.Lock()
						responseSent = true
						mu.Unlock()
					}
				}
			}))
			defer agentServer.Close()

			// Connect to mock agent
			wsURL := "ws" + strings.TrimPrefix(agentServer.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("Failed to connect: %v", err)
			}
			defer conn.Close()

			// Send command
			cmdMsg := WSMessage{
				Type:      MessageTypeCommand,
				Command:   tc.command,
				Data:      tc.data,
				Timestamp: time.Now(),
			}
			msgBytes, _ := json.Marshal(cmdMsg)
			if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
				t.Fatalf("Failed to send command: %v", err)
			}

			// Wait for and read response
			if tc.expectResponse {
				conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, respMessage, err := conn.ReadMessage()
				if err != nil {
					t.Fatalf("Failed to read response: %v", err)
				}

				var resp WSMessage
				if err := json.Unmarshal(respMessage, &resp); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}

				if resp.Type != MessageTypeUpdateProgress {
					t.Errorf("Expected response type %s, got %s", MessageTypeUpdateProgress, resp.Type)
				}
			}

			// Give time for processing
			time.Sleep(100 * time.Millisecond)

			mu.Lock()
			defer mu.Unlock()

			if receivedCommand != tc.command {
				t.Errorf("Expected command %q, got %q", tc.command, receivedCommand)
			}

			if tc.data != nil {
				for key, expected := range tc.data {
					if actual, ok := receivedData[key]; !ok || actual != expected {
						t.Errorf("Expected data[%s]=%v, got %v", key, expected, actual)
					}
				}
			}

			if tc.expectResponse && !responseSent {
				t.Error("Expected response to be sent")
			}
		})
	}
}

// TestWebSocketUpdateProgress tests update progress messages from agent to server
func TestWebSocketUpdateProgress(t *testing.T) {
	t.Parallel()

	progressStates := []struct {
		status        string
		progress      int
		targetVersion string
		message       string
		hasError      bool
	}{
		{"checking", 0, "", "Checking for updates...", false},
		{"downloading", 25, "1.1.0", "Downloading update...", false},
		{"downloading", 50, "1.1.0", "Downloading update...", false},
		{"downloading", 75, "1.1.0", "Downloading update...", false},
		{"staging", 80, "1.1.0", "Staging files...", false},
		{"applying", 90, "1.1.0", "Applying update...", false},
		{"restarting", 95, "1.1.0", "Restarting service...", false},
		{"succeeded", 100, "1.1.0", "Update completed successfully", false},
	}

	var receivedMessages []map[string]interface{}
	var mu sync.Mutex

	// Create mock server to receive progress updates
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeUpdateProgress {
				mu.Lock()
				receivedMessages = append(receivedMessages, msg.Data)
				mu.Unlock()
			}
		}
	}))
	defer server.Close()

	// Connect as agent
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send progress updates
	for _, state := range progressStates {
		msg := WSMessage{
			Type:      MessageTypeUpdateProgress,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"status":         state.status,
				"progress":       state.progress,
				"target_version": state.targetVersion,
				"message":        state.message,
			},
		}

		if state.hasError {
			msg.Data["error"] = "Update failed"
		}

		msgBytes, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
			t.Fatalf("Failed to send progress: %v", err)
		}

		time.Sleep(10 * time.Millisecond)
	}

	// Wait for messages to be received
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(receivedMessages) != len(progressStates) {
		t.Errorf("Expected %d progress messages, got %d", len(progressStates), len(receivedMessages))
	}

	// Verify final state
	if len(receivedMessages) > 0 {
		last := receivedMessages[len(receivedMessages)-1]
		if last["status"] != "succeeded" {
			t.Errorf("Expected final status 'succeeded', got %v", last["status"])
		}
		if progress, ok := last["progress"].(float64); !ok || int(progress) != 100 {
			t.Errorf("Expected final progress 100, got %v", last["progress"])
		}
	}
}

// TestWebSocketUpdateFailure tests update failure scenario
func TestWebSocketUpdateFailure(t *testing.T) {
	t.Parallel()

	var receivedError bool
	var errorMessage string
	var mu sync.Mutex

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeUpdateProgress {
				if errMsg, ok := msg.Data["error"].(string); ok && errMsg != "" {
					mu.Lock()
					receivedError = true
					errorMessage = errMsg
					mu.Unlock()
				}
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send failure message
	msg := WSMessage{
		Type:      MessageTypeUpdateProgress,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"status":     "failed",
			"progress":   -1,
			"error":      "Download failed: network timeout",
			"error_code": "DOWNLOAD_FAILED",
		},
	}
	msgBytes, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, msgBytes)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if !receivedError {
		t.Error("Expected error to be received")
	}
	if !strings.Contains(errorMessage, "network timeout") {
		t.Errorf("Expected error message to contain 'network timeout', got %q", errorMessage)
	}
}

// TestWebSocketUpdateCancellation tests update cancellation handling
func TestWebSocketUpdateCancellation(t *testing.T) {
	t.Parallel()

	var cancelReceived bool
	var mu sync.Mutex

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Mock agent that handles cancel command
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeCommand && msg.Command == "cancel_update" {
				mu.Lock()
				cancelReceived = true
				mu.Unlock()

				// Send cancelled status
				response := WSMessage{
					Type:      MessageTypeUpdateProgress,
					Timestamp: time.Now(),
					Data: map[string]interface{}{
						"status":  "cancelled",
						"message": "Update cancelled by user",
					},
				}
				respBytes, _ := json.Marshal(response)
				conn.WriteMessage(websocket.TextMessage, respBytes)
			}
		}
	}))
	defer agentServer.Close()

	wsURL := "ws" + strings.TrimPrefix(agentServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send cancel command
	cmdMsg := WSMessage{
		Type:      MessageTypeCommand,
		Command:   "cancel_update",
		Timestamp: time.Now(),
	}
	msgBytes, _ := json.Marshal(cmdMsg)
	conn.WriteMessage(websocket.TextMessage, msgBytes)

	// Read response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, respMessage, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	var resp WSMessage
	json.Unmarshal(respMessage, &resp)

	mu.Lock()
	defer mu.Unlock()

	if !cancelReceived {
		t.Error("Expected cancel command to be received")
	}

	status, _ := resp.Data["status"].(string)
	if status != "cancelled" {
		t.Errorf("Expected status 'cancelled', got %q", status)
	}
}

// TestWebSocketReconnection tests agent reconnection behavior
func TestWebSocketReconnection(t *testing.T) {
	t.Parallel()

	connectionCount := 0
	var mu sync.Mutex

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		currentCount := connectionCount
		mu.Unlock()

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// First connection closes immediately to simulate disconnect
		if currentCount == 1 {
			time.Sleep(50 * time.Millisecond)
			conn.Close()
			return
		}

		// Second connection stays open
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// First connection - will be closed by server
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("First connection failed: %v", err)
	}

	// Wait for server to close connection
	time.Sleep(100 * time.Millisecond)
	conn1.Close()

	// Second connection - should succeed
	conn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Reconnection failed: %v", err)
	}
	defer conn2.Close()

	mu.Lock()
	count := connectionCount
	mu.Unlock()

	if count < 2 {
		t.Errorf("Expected at least 2 connections, got %d", count)
	}
}

// TestWebSocketHeartbeatDuringUpdate tests heartbeat handling during active update
func TestWebSocketHeartbeatDuringUpdate(t *testing.T) {
	t.Parallel()

	var heartbeatsDuringUpdate int
	var mu sync.Mutex
	updateInProgress := true

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			// Check if this is a heartbeat message
			if msg["type"] == "heartbeat" {
				mu.Lock()
				if updateInProgress {
					heartbeatsDuringUpdate++
				}
				mu.Unlock()
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send heartbeats while "update in progress"
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			hb := map[string]interface{}{
				"type":      "heartbeat",
				"timestamp": time.Now().Format(time.RFC3339),
			}
			hbBytes, _ := json.Marshal(hb)
			conn.WriteMessage(websocket.TextMessage, hbBytes)
		}
	}

done:
	mu.Lock()
	updateInProgress = false
	count := heartbeatsDuringUpdate
	mu.Unlock()

	// Should have received multiple heartbeats
	if count < 3 {
		t.Errorf("Expected at least 3 heartbeats during update, got %d", count)
	}
}
