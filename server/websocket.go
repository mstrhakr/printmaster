package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"printmaster/server/storage"

	"github.com/gorilla/websocket"
)

var (
	// WebSocket upgrader with default settings
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Allow all origins for now (TODO: restrict in production)
			return true
		},
	}

	// Track active WebSocket connections by agent ID (string)
	wsConnections     = make(map[string]*websocket.Conn)
	wsConnectionsLock sync.RWMutex

	// Track pending proxy requests awaiting responses from agents
	proxyRequests     = make(map[string]chan WSMessage) // key: requestID
	proxyRequestsLock sync.RWMutex
)

// WSMessage represents a WebSocket message (matches agent's structure)
type WSMessage struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// WebSocket message types
const (
	MessageTypeHeartbeat     = "heartbeat"
	MessageTypePong          = "pong"
	MessageTypeError         = "error"
	MessageTypeProxyRequest  = "proxy_request"
	MessageTypeProxyResponse = "proxy_response"
)

// handleAgentWebSocket handles WebSocket connections from agents
func handleAgentWebSocket(w http.ResponseWriter, r *http.Request, serverStore storage.Store) {
	// Extract client IP address
	clientIP := extractIPFromAddr(r.RemoteAddr)

	// Extract and validate authentication token from query parameter
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Missing authentication token", http.StatusUnauthorized)
		return
	}

	tokenPrefix := token
	if len(token) > 8 {
		tokenPrefix = token[:8]
	}

	// Check if this IP+token is currently blocked
	if authRateLimiter != nil {
		if isBlocked, blockedUntil := authRateLimiter.IsBlocked(clientIP, tokenPrefix); isBlocked {
			if serverLogger != nil {
				serverLogger.Warn("Blocked WebSocket connection attempt",
					"ip", clientIP,
					"token", tokenPrefix+"...",
					"blocked_until", blockedUntil.Format(time.RFC3339),
					"user_agent", r.Header.Get("User-Agent"))
			}
			http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
	}

	// Authenticate agent
	agent, err := serverStore.GetAgentByToken(r.Context(), token)
	if err != nil {
		// Record failed attempt and check if we should log
		var isBlocked, shouldLog bool
		var attemptCount int
		if authRateLimiter != nil {
			isBlocked, shouldLog, attemptCount = authRateLimiter.RecordFailure(clientIP, tokenPrefix)
		} else {
			shouldLog = true
		}

		if serverLogger != nil && shouldLog {
			fields := []interface{}{
				"ip", clientIP,
				"token", tokenPrefix + "...",
				"error", err.Error(),
				"attempt_count", attemptCount,
				"protocol", "websocket",
				"user_agent", r.Header.Get("User-Agent"),
			}

			if isBlocked {
				fields = append(fields, "status", "BLOCKED")
				serverLogger.Error("WebSocket auth failed - IP blocked", fields...)

				// Log to audit trail when blocking occurs
				logAuditEntry(r.Context(), "UNKNOWN", "auth_blocked_websocket",
					fmt.Sprintf("IP blocked after %d failed WebSocket auth attempts with token %s... Error: %s",
						attemptCount, tokenPrefix, err.Error()),
					clientIP)
			} else if attemptCount >= 3 {
				serverLogger.Warn("Repeated WebSocket auth failures", fields...)
			} else {
				serverLogger.Warn("Invalid WebSocket authentication", fields...)
			}
		}

		if isBlocked {
			http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
		} else {
			http.Error(w, "Invalid authentication token", http.StatusUnauthorized)
		}
		return
	}

	if agent == nil {
		// Same rate limiting logic for nil agent
		var isBlocked, shouldLog bool
		var attemptCount int
		if authRateLimiter != nil {
			isBlocked, shouldLog, attemptCount = authRateLimiter.RecordFailure(clientIP, tokenPrefix)
		} else {
			shouldLog = true
		}

		if serverLogger != nil && shouldLog {
			if isBlocked {
				serverLogger.Error("WebSocket auth returned nil agent - IP blocked",
					"ip", clientIP,
					"token", tokenPrefix+"...",
					"attempt_count", attemptCount,
					"status", "BLOCKED")

				// Log to audit trail when blocking occurs
				logAuditEntry(r.Context(), "UNKNOWN", "auth_blocked_websocket",
					fmt.Sprintf("IP blocked after %d failed WebSocket auth attempts with token %s... (nil agent)",
						attemptCount, tokenPrefix),
					clientIP)
			} else {
				serverLogger.Warn("WebSocket auth returned nil agent",
					"ip", clientIP,
					"token", tokenPrefix+"...",
					"attempt_count", attemptCount)
			}
		}

		if isBlocked {
			http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
		} else {
			http.Error(w, "Invalid authentication token", http.StatusUnauthorized)
		}
		return
	}

	// Success - clear any failure records for this IP+token
	if authRateLimiter != nil {
		authRateLimiter.RecordSuccess(clientIP, tokenPrefix)
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if serverLogger != nil {
			serverLogger.Error("WebSocket upgrade failed",
				"agent_id", agent.AgentID,
				"hostname", agent.Hostname,
				"ip", clientIP,
				"error", err)
		}
		return
	}

	if serverLogger != nil {
		serverLogger.Info("Agent WebSocket connected",
			"agent_id", agent.AgentID,
			"hostname", agent.Hostname,
			"ip", clientIP,
			"remote_addr", r.RemoteAddr)
	}

	// Register connection
	wsConnectionsLock.Lock()
	// Close existing connection if any (agent reconnecting)
	if existingConn, exists := wsConnections[agent.AgentID]; exists {
		if serverLogger != nil {
			serverLogger.Info("Closing existing WebSocket for reconnection", "agent_id", agent.AgentID)
		}
		existingConn.Close()
	}
	wsConnections[agent.AgentID] = conn
	wsConnectionsLock.Unlock()

	// Broadcast agent_connected event to UI via SSE
	if sseHub != nil {
		sseHub.Broadcast(SSEEvent{
			Type: "agent_connected",
			Data: map[string]interface{}{
				"agent_id": agent.AgentID,
				"name":     agent.Name,
			},
		})
	}

	// Handle connection cleanup on exit
	defer func() {
		wsConnectionsLock.Lock()
		if wsConnections[agent.AgentID] == conn {
			delete(wsConnections, agent.AgentID)
			if serverLogger != nil {
				serverLogger.Info("Agent WebSocket disconnected", "agent_id", agent.AgentID)
			}

			// Broadcast agent_disconnected event to UI via SSE
			if sseHub != nil {
				sseHub.Broadcast(SSEEvent{
					Type: "agent_disconnected",
					Data: map[string]interface{}{
						"agent_id": agent.AgentID,
					},
				})
			}
		}
		wsConnectionsLock.Unlock()
		conn.Close()
	}()

	// Set up ping/pong handler to keep connection alive
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	// Read messages from agent
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket error for agent %s: %v", agent.AgentID, err)
			}
			break
		}

		// Parse message
		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Printf("Failed to parse WebSocket message from agent %s: %v", agent.AgentID, err)
			sendWSError(conn, "Invalid message format")
			continue
		}

		// Handle different message types
		switch msg.Type {
		case "heartbeat":
			handleWSHeartbeat(conn, agent, msg, serverStore)
		case MessageTypeProxyResponse:
			handleWSProxyResponse(msg)
		default:
			log.Printf("Unknown WebSocket message type from agent %s: %s", agent.AgentID, msg.Type)
			sendWSError(conn, "Unknown message type")
		}
	}
}

// handleWSHeartbeat processes heartbeat messages received via WebSocket
func handleWSHeartbeat(conn *websocket.Conn, agent *storage.Agent, msg WSMessage, serverStore storage.Store) {
	// Extract optional device count from heartbeat data
	if deviceCount, ok := msg.Data["device_count"].(float64); ok {
		agent.DeviceCount = int(deviceCount)
	}

	// Update agent heartbeat in database (updates last_seen and status)
	ctx := context.Background()
	if err := serverStore.UpdateAgentHeartbeat(ctx, agent.AgentID, "active"); err != nil {
		log.Printf("Failed to update agent %s after WebSocket heartbeat: %v", agent.AgentID, err)
		sendWSError(conn, "Failed to process heartbeat")
		return
	}

	log.Printf("WebSocket heartbeat received from agent %s", agent.AgentID)

	// Send pong response
	pongMsg := WSMessage{
		Type:      "pong",
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(pongMsg)
	if err != nil {
		log.Printf("Failed to marshal pong message: %v", err)
		return
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		log.Printf("Failed to send pong to agent %s: %v", agent.AgentID, err)
	}
}

// sendWSError sends an error message to the WebSocket client
func sendWSError(conn *websocket.Conn, errorMsg string) {
	msg := WSMessage{
		Type: "error",
		Data: map[string]interface{}{
			"message": errorMsg,
		},
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal error message: %v", err)
		return
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		log.Printf("Failed to send error message: %v", err)
	}
}

// getAgentWSConnection returns the WebSocket connection for an agent, if connected
func getAgentWSConnection(agentID string) (*websocket.Conn, bool) {
	wsConnectionsLock.RLock()
	defer wsConnectionsLock.RUnlock()
	conn, exists := wsConnections[agentID]
	return conn, exists
}

// isAgentConnectedWS checks if an agent has an active WebSocket connection
func isAgentConnectedWS(agentID string) bool {
	wsConnectionsLock.RLock()
	defer wsConnectionsLock.RUnlock()
	_, exists := wsConnections[agentID]
	return exists
}

// closeAgentWebSocket closes the WebSocket connection for an agent
func closeAgentWebSocket(agentID string) {
	wsConnectionsLock.Lock()
	defer wsConnectionsLock.Unlock()

	if conn, exists := wsConnections[agentID]; exists {
		conn.Close()
		delete(wsConnections, agentID)
		if serverLogger != nil {
			serverLogger.Info("Closed WebSocket connection for deleted agent", "agent_id", agentID)
		}
	}
}

// handleWSProxyResponse handles HTTP proxy responses from agents
func handleWSProxyResponse(msg WSMessage) {
	requestID, ok := msg.Data["request_id"].(string)
	if !ok {
		log.Printf("Proxy response missing request_id")
		return
	}

	// Find the waiting channel for this request
	proxyRequestsLock.Lock()
	respChan, exists := proxyRequests[requestID]
	if exists {
		delete(proxyRequests, requestID)
	}
	proxyRequestsLock.Unlock()

	if !exists {
		log.Printf("Received proxy response for unknown request ID: %s", requestID)
		return
	}

	// Send response to waiting HTTP handler (non-blocking with timeout)
	select {
	case respChan <- msg:
		// Successfully delivered
	case <-time.After(5 * time.Second):
		log.Printf("Timeout delivering proxy response for request ID: %s", requestID)
	}
}

// sendProxyRequest sends an HTTP proxy request to an agent via WebSocket
func sendProxyRequest(agentID string, requestID string, targetURL string, method string,
	headers map[string]string, body string) error {

	conn, exists := getAgentWSConnection(agentID)
	if !exists {
		return fmt.Errorf("agent not connected via WebSocket")
	}

	msg := WSMessage{
		Type: MessageTypeProxyRequest,
		Data: map[string]interface{}{
			"request_id": requestID,
			"url":        targetURL,
			"method":     method,
			"headers":    headers,
			"body":       body,
		},
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal proxy request: %w", err)
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("failed to send proxy request: %w", err)
	}

	return nil
}
