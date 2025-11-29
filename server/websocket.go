package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	wscommon "printmaster/common/ws"
	"printmaster/server/storage"

	"github.com/gorilla/websocket"
)

var (
	// Per-connection map and locks
	wsConnections     = make(map[string]*wscommon.Conn)
	wsConnectionsLock sync.RWMutex

	// (global counters removed - using per-agent diagnostics maps below)
	// Per-agent diagnostics
	wsDiagLock                 sync.RWMutex
	wsPingFailuresPerAgent     = make(map[string]int64)
	wsDisconnectEventsPerAgent = make(map[string]int64)

	// Track pending proxy requests awaiting responses from agents
	proxyRequests     = make(map[string]chan wscommon.Message) // key: requestID
	proxyRequestsLock sync.RWMutex
)

// Use shared message type constants from wscommon

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

	// Always log incoming WS attempt (helps diagnose why agents don't complete handshake)
	logInfo("Incoming WebSocket connection attempt",
		"ip", clientIP,
		"token", tokenPrefix+"...",
		"user_agent", r.Header.Get("User-Agent"))
	logDebug("Incoming WebSocket raw headers",
		"remote_addr", r.RemoteAddr,
		"headers", r.Header)

	// Check if this IP+token is currently blocked
	if authRateLimiter != nil {
		if isBlocked, blockedUntil := authRateLimiter.IsBlocked(clientIP, tokenPrefix); isBlocked {
			logWarn("Blocked WebSocket connection attempt",
				"ip", clientIP,
				"token", tokenPrefix+"...",
				"blocked_until", blockedUntil.Format(time.RFC3339),
				"user_agent", r.Header.Get("User-Agent"))
			logDebug("Blocked WebSocket details", "remote_addr", r.RemoteAddr)
			http.Error(w, "Too many failed attempts. Try again later.", http.StatusTooManyRequests)
			return
		}
	}

	// Authenticate agent
	logDebug("Authenticating WebSocket token", "token_prefix", tokenPrefix+"...", "ip", clientIP)
	agent, err := serverStore.GetAgentByToken(r.Context(), token)
	if err != nil {
		// Record failed attempt and check if we should log
		var isBlocked, shouldLog bool
		var attemptCount int
		if authRateLimiter != nil {
			isBlocked, shouldLog, attemptCount = authRateLimiter.RecordFailure(clientIP, tokenPrefix)
		} else {
			isBlocked, shouldLog = false, true
		}

		if shouldLog {
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
				logError("WebSocket auth failed - IP blocked", fields...)

				// Log to audit trail when blocking occurs
				logAuditEntry(r.Context(), &storage.AuditEntry{
					ActorType: storage.AuditActorAgent,
					ActorID:   tokenPrefix,
					Action:    "auth_blocked_websocket",
					Details: fmt.Sprintf("IP blocked after %d failed WebSocket auth attempts with token %s... Error: %s",
						attemptCount, tokenPrefix, err.Error()),
					IPAddress: clientIP,
					UserAgent: r.Header.Get("User-Agent"),
					Severity:  storage.AuditSeverityWarn,
					Metadata: map[string]interface{}{
						"attempt_count": attemptCount,
						"protocol":      "websocket",
					},
				})
			} else if attemptCount >= 3 {
				logWarn("Repeated WebSocket auth failures", fields...)
			} else {
				logWarn("Invalid WebSocket authentication", fields...)
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
			isBlocked, shouldLog = false, true
		}

		if shouldLog {
			if isBlocked {
				logError("WebSocket auth returned nil agent - IP blocked",
					"ip", clientIP,
					"token", tokenPrefix+"...",
					"attempt_count", attemptCount,
					"status", "BLOCKED")

				// Log to audit trail when blocking occurs
				logAuditEntry(r.Context(), &storage.AuditEntry{
					ActorType: storage.AuditActorAgent,
					ActorID:   tokenPrefix,
					Action:    "auth_blocked_websocket",
					Details: fmt.Sprintf("IP blocked after %d failed WebSocket auth attempts with token %s... (nil agent)",
						attemptCount, tokenPrefix),
					IPAddress: clientIP,
					UserAgent: r.Header.Get("User-Agent"),
					Severity:  storage.AuditSeverityWarn,
					Metadata: map[string]interface{}{
						"attempt_count": attemptCount,
						"protocol":      "websocket",
					},
				})
			} else {
				logWarn("WebSocket auth returned nil agent",
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

	logDebug("WebSocket authentication success", "agent_id_guess", agent.AgentID, "hostname", agent.Hostname, "ip", clientIP)

	// Upgrade HTTP connection to WebSocket (use shared wrapper)
	conn, err := wscommon.UpgradeHTTP(w, r)
	if err != nil {
		logError("WebSocket upgrade failed",
			"agent_id", agent.AgentID,
			"hostname", agent.Hostname,
			"ip", clientIP,
			"error", err)
		return
	}

	logInfo("Agent WebSocket connected",
		"agent_id", agent.AgentID,
		"hostname", agent.Hostname,
		"ip", clientIP,
		"remote_addr", r.RemoteAddr)
	logDebug("Agent WebSocket connect details", "user_agent", r.Header.Get("User-Agent"), "headers", r.Header)

	// Register connection
	wsConnectionsLock.Lock()
	// Close existing connection if any (agent reconnecting)
	if existingConn, exists := wsConnections[agent.AgentID]; exists {
		logInfo("Closing existing WebSocket for reconnection", "agent_id", agent.AgentID)
		existingConn.Close()
	}
	wsConnections[agent.AgentID] = conn
	wsConnectionsLock.Unlock()

	logDebug("Registered WebSocket connection for agent", "agent_id", agent.AgentID)

	// Broadcast agent_connected event to UI via SSE
	sseHub.Broadcast(SSEEvent{
		Type: "agent_connected",
		Data: map[string]interface{}{
			"agent_id": agent.AgentID,
			"name":     agent.Name,
		},
	})

	// Start server-side ping loop to detect half-open TCP connections and
	// surface failures earlier. If ping fails, close the connection which
	// triggers the cleanup below and marks the agent offline.
	pingTicker := time.NewTicker(25 * time.Second)
	pingDone := make(chan struct{})
	go func() {
		defer pingTicker.Stop()
		for {
			select {
			case <-pingTicker.C:
				// send ping
				if err := conn.WritePing(10 * time.Second); err != nil {
					logWarn("WebSocket ping failed, closing connection", "agent_id", agent.AgentID, "error", err)
					wsDiagLock.Lock()
					wsPingFailuresPerAgent[agent.AgentID]++
					pf := wsPingFailuresPerAgent[agent.AgentID]
					de := wsDisconnectEventsPerAgent[agent.AgentID]
					wsDiagLock.Unlock()
					// Broadcast diagnostic SSE event so UI can update
					sseHub.Broadcast(SSEEvent{
						Type: "agent_ws_diag",
						Data: map[string]interface{}{
							"agent_id":             agent.AgentID,
							"ws_ping_failures":     pf,
							"ws_disconnect_events": de,
						},
					})
					conn.Close()
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	// Handle connection cleanup on exit
	defer func() {
		// signal ping goroutine to stop
		close(pingDone)

		wsConnectionsLock.Lock()
		if wsConnections[agent.AgentID] == conn {
			delete(wsConnections, agent.AgentID)
			logInfo("Agent WebSocket disconnected", "agent_id", agent.AgentID)

			// Broadcast agent_disconnected event to UI via SSE
			sseHub.Broadcast(SSEEvent{
				Type: "agent_disconnected",
				Data: map[string]interface{}{
					"agent_id": agent.AgentID,
				},
			})
			// Mark disconnect event for diagnostics
			wsDiagLock.Lock()
			wsDisconnectEventsPerAgent[agent.AgentID]++
			pf := wsPingFailuresPerAgent[agent.AgentID]
			de := wsDisconnectEventsPerAgent[agent.AgentID]
			wsDiagLock.Unlock()
			// Broadcast diagnostic SSE event so UI can update
			sseHub.Broadcast(SSEEvent{
				Type: "agent_ws_diag",
				Data: map[string]interface{}{
					"agent_id":             agent.AgentID,
					"ws_ping_failures":     pf,
					"ws_disconnect_events": de,
				},
			})

			// Debounce marking offline: wait a short window before flipping DB state
			go func(agentID string) {
				// Wait 10 seconds to allow quick reconnects
				time.Sleep(10 * time.Second)
				// If agent reconnected, skip marking offline
				if isAgentConnectedWS(agentID) {
					logDebug("Agent reconnected during debounce window, skipping offline mark", "agent_id", agentID)
					return
				}
				ctx := context.Background()
				if err := serverStore.UpdateAgentHeartbeat(ctx, agentID, "offline"); err != nil {
					logWarn("Failed to mark agent offline after WS disconnect", "agent_id", agentID, "error", err)
				}
			}(agent.AgentID)
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
		message, err := conn.ReadMessage()
		if err != nil {
			if wscommon.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logWarn("WebSocket error", "agent_id", agent.AgentID, "error", err)
			}
			break
		}

		logDebug("WebSocket raw message received", "agent_id", agent.AgentID, "len", len(message))

		// Parse message
		var msg wscommon.Message
		if err := json.Unmarshal(message, &msg); err != nil {
			logWarn("Failed to parse WebSocket message", "agent_id", agent.AgentID, "error", err)
			sendWSError(conn, "Invalid message format")
			continue
		}

		// Handle different message types
		switch msg.Type {
		case wscommon.MessageTypeHeartbeat:
			handleWSHeartbeat(conn, agent, msg, serverStore)
		case wscommon.MessageTypeProxyResponse:
			handleWSProxyResponse(msg)
		case wscommon.MessageTypeUpdateProgress:
			handleWSUpdateProgress(agent, msg)
		default:
			logWarn("Unknown WebSocket message type", "agent_id", agent.AgentID, "message_type", msg.Type)
			sendWSError(conn, "Unknown message type")
		}
	}
}

// handleWSHeartbeat processes heartbeat messages received via WebSocket
func handleWSHeartbeat(conn *wscommon.Conn, agent *storage.Agent, msg wscommon.Message, serverStore storage.Store) {
	// Extract optional device count from heartbeat data
	if deviceCount, ok := msg.Data["device_count"].(float64); ok {
		agent.DeviceCount = int(deviceCount)
	}

	status := wsStringField(msg.Data, "status")
	if status == "" {
		status = "active"
	}

	ctx := context.Background()
	if update := buildAgentUpdateFromWS(agent.AgentID, status, msg.Data); update != nil {
		if err := serverStore.UpdateAgentInfo(ctx, update); err != nil {
			logError("Failed to update agent metadata after WebSocket heartbeat", "agent_id", agent.AgentID, "error", err)
			sendWSError(conn, "Failed to process heartbeat")
			return
		}
	} else {
		if err := serverStore.UpdateAgentHeartbeat(ctx, agent.AgentID, status); err != nil {
			logError("Failed to update agent after WebSocket heartbeat", "agent_id", agent.AgentID, "error", err)
			sendWSError(conn, "Failed to process heartbeat")
			return
		}
	}

	logDebug("WebSocket heartbeat received", "agent_id", agent.AgentID)

	// Send pong response
	pongMsg := wscommon.Message{
		Type:      wscommon.MessageTypePong,
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(pongMsg)
	if err != nil {
		logError("Failed to marshal pong message", "error", err)
		return
	}

	if err := conn.WriteRaw(payload, 10*time.Second); err != nil {
		logWarn("Failed to send pong to agent", "agent_id", agent.AgentID, "error", err)
	}
}

func buildAgentUpdateFromWS(agentID string, status string, data map[string]interface{}) *storage.Agent {
	if len(data) == 0 {
		return nil
	}

	update := &storage.Agent{
		AgentID: agentID,
		Status:  status,
	}

	fields := 0
	if v := wsStringField(data, "version"); v != "" {
		update.Version = v
		fields++
	}
	if v := wsStringField(data, "protocol_version"); v != "" {
		update.ProtocolVersion = v
		fields++
	}
	if v := wsStringField(data, "hostname"); v != "" {
		update.Hostname = v
		fields++
	}
	if v := wsStringField(data, "ip"); v != "" {
		update.IP = v
		fields++
	}
	if v := wsStringField(data, "platform"); v != "" {
		update.Platform = v
		fields++
	}
	if v := wsStringField(data, "os_version"); v != "" {
		update.OSVersion = v
		fields++
	}
	if v := wsStringField(data, "go_version"); v != "" {
		update.GoVersion = v
		fields++
	}
	if v := wsStringField(data, "architecture"); v != "" {
		update.Architecture = v
		fields++
	}
	if v := wsStringField(data, "build_type"); v != "" {
		update.BuildType = v
		fields++
	}
	if v := wsStringField(data, "git_commit"); v != "" {
		update.GitCommit = v
		fields++
	}

	if fields == 0 {
		return nil
	}
	return update
}

func wsStringField(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	val, ok := data[key]
	if !ok || val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// sendWSError sends an error message to the WebSocket client
func sendWSError(conn *wscommon.Conn, errorMsg string) {
	msg := wscommon.Message{
		Type: wscommon.MessageTypeError,
		Data: map[string]interface{}{
			"message": errorMsg,
		},
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		logError("Failed to marshal WebSocket error message", "error", err)
		return
	}

	if err := conn.WriteRaw(payload, 10*time.Second); err != nil {
		logWarn("Failed to send WebSocket error message", "error", err)
	}
}

// getAgentWSConnection returns the WebSocket connection for an agent, if connected
func getAgentWSConnection(agentID string) (*wscommon.Conn, bool) {
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
		logInfo("Closed WebSocket connection for deleted agent", "agent_id", agentID)
	}
}

// handleWSProxyResponse handles HTTP proxy responses from agents
func handleWSProxyResponse(msg wscommon.Message) {
	requestID, ok := msg.Data["request_id"].(string)
	if !ok {
		logWarn("Proxy response missing request_id")
		return
	}

	logDebug("Received WS proxy response", "request_id", requestID)

	// Find the waiting channel for this request
	proxyRequestsLock.Lock()
	respChan, exists := proxyRequests[requestID]
	if exists {
		delete(proxyRequests, requestID)
	}
	proxyRequestsLock.Unlock()

	if !exists {
		logWarn("Received proxy response for unknown request ID", "request_id", requestID)
		return
	}

	// Send response to waiting HTTP handler (non-blocking with timeout)
	select {
	case respChan <- msg:
		// Successfully delivered
	case <-time.After(5 * time.Second):
		logWarn("Timeout delivering proxy response", "request_id", requestID)
	}
}

// sendProxyRequest sends an HTTP proxy request to an agent via WebSocket
func sendProxyRequest(agentID string, requestID string, targetURL string, method string,
	headers map[string]string, body string) error {

	conn, exists := getAgentWSConnection(agentID)
	if !exists {
		return fmt.Errorf("agent not connected via WebSocket")
	}

	msg := wscommon.Message{
		Type: wscommon.MessageTypeProxyRequest,
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

	if err := conn.WriteRaw(payload, 10*time.Second); err != nil {
		return fmt.Errorf("failed to send proxy request: %w", err)
	}

	return nil
}

// handleWSUpdateProgress processes update progress messages from agents
// and broadcasts them to connected UI clients via SSE
func handleWSUpdateProgress(agent *storage.Agent, msg wscommon.Message) {
	logDebug("Update progress received", "agent_id", agent.AgentID, "data", msg.Data)

	// Add agent_id to the data for UI routing
	msg.Data["agent_id"] = agent.AgentID

	// Broadcast to UI clients via the SSE event system
	if sseHub != nil {
		sseHub.Broadcast(SSEEvent{
			Type: "update_progress",
			Data: msg.Data,
		})
	}
}
