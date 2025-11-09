package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"crypto/tls"

	"github.com/gorilla/websocket"
)

// WebSocket message types
const (
	MessageTypeHeartbeat     = "heartbeat"
	MessageTypePong          = "pong"
	MessageTypeError         = "error"
	MessageTypeProxyRequest  = "proxy_request"
	MessageTypeProxyResponse = "proxy_response"
)

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// WSClient manages a persistent WebSocket connection to the server
type WSClient struct {
	serverURL     string
	token         string
	conn          *websocket.Conn
	mu            sync.RWMutex
	connected     bool
	reconnectChan chan struct{}
	stopChan      chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *log.Logger

	// Configuration
	reconnectDelay    time.Duration
	pingInterval      time.Duration
	writeTimeout      time.Duration
	readTimeout       time.Duration
	handshakeTimeout  time.Duration
	maxReconnectDelay time.Duration
}

// NewWSClient creates a new WebSocket client
func NewWSClient(serverURL, token string, logger *log.Logger) *WSClient {
	ctx, cancel := context.WithCancel(context.Background())

	return &WSClient{
		serverURL:         serverURL,
		token:             token,
		reconnectChan:     make(chan struct{}, 1),
		stopChan:          make(chan struct{}),
		ctx:               ctx,
		cancel:            cancel,
		logger:            logger,
		reconnectDelay:    5 * time.Second,
		pingInterval:      30 * time.Second,
		writeTimeout:      10 * time.Second,
		readTimeout:       60 * time.Second,
		handshakeTimeout:  10 * time.Second,
		maxReconnectDelay: 5 * time.Minute,
	}
}

// Start begins the WebSocket connection and management goroutines
func (ws *WSClient) Start() error {
	ws.logger.Println("Starting WebSocket client...")

	// Initial connection attempt
	if err := ws.connect(); err != nil {
		ws.logger.Printf("Initial WebSocket connection failed: %v (will retry)", err)
		// Don't return error - reconnect loop will handle it
	}

	// Start connection manager
	ws.logger.Println("Starting connection manager goroutine")
	go ws.connectionManager()

	return nil
}

// Stop gracefully stops the WebSocket client
func (ws *WSClient) Stop() error {
	ws.logger.Println("Stopping WebSocket client...")
	ws.cancel()
	close(ws.stopChan)

	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.conn != nil {
		// Send close message
		// Set a short write deadline to avoid hangs during shutdown
		ws.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := ws.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			ws.logger.Printf("Error sending close message: %v", err)
		} else {
			ws.logger.Printf("Sent WebSocket close message to server")
		}

		ws.conn.Close()
		ws.conn = nil
		ws.logger.Printf("Closed WebSocket connection object")
	}

	ws.connected = false
	return nil
}

// IsConnected returns whether the WebSocket is currently connected
func (ws *WSClient) IsConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.connected
}

// connect establishes a WebSocket connection to the server
func (ws *WSClient) connect() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	// Close existing connection if any
	if ws.conn != nil {
		ws.conn.Close()
		ws.conn = nil
		ws.connected = false
		// Give some time for goroutines to observe ctx.Done / stopChan and exit
		// We won't block indefinitely; just log whether they exited within a short window
		done := make(chan struct{})
		go func() {
			// Poll for connection cleared
			for i := 0; i < 10; i++ {
				ws.mu.RLock()
				c := ws.conn
				connState := ws.connected
				ws.mu.RUnlock()
				if c == nil && !connState {
					close(done)
					return
				}
				time.Sleep(200 * time.Millisecond)
			}
			close(done)
		}()

		select {
		case <-done:
			ws.logger.Printf("Stop: connection and goroutines appear to have shut down (or timed check)")
		case <-time.After(6 * time.Second):
			ws.logger.Printf("Stop: timeout waiting for goroutines to exit")
		}
	}

	// Parse and build WebSocket URL
	u, err := url.Parse(ws.serverURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// Convert http(s) to ws(s)
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	// Set WebSocket endpoint path
	u.Path = "/api/v1/agents/ws"

	// Add authentication token as query parameter
	q := u.Query()
	q.Set("token", ws.token)
	u.RawQuery = q.Encode()

	ws.logger.Printf("Connecting to WebSocket: %s", u.String())

	// Determine whether to skip TLS verification for the WebSocket dialer.
	// This respects the same environment variable used by agent config
	// (SERVER_INSECURE_SKIP_VERIFY) so the behavior can be toggled at runtime.
	skipVerify := false
	if val := os.Getenv("SERVER_INSECURE_SKIP_VERIFY"); val != "" {
		lv := strings.ToLower(val)
		if lv == "1" || lv == "true" || lv == "yes" {
			skipVerify = true
		}
	}

	// Create WebSocket dialer with timeouts and TLS settings
	dialer := &websocket.Dialer{
		HandshakeTimeout: ws.handshakeTimeout,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: skipVerify},
	}

	ws.logger.Printf("WebSocket dialer TLS InsecureSkipVerify=%v", skipVerify)

	// Connect
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		// Try to include HTTP response body from the upgrade attempt for easier debugging
		if resp != nil {
			var bodyBytes []byte
			if resp.Body != nil {
				bodyBytes, _ = io.ReadAll(resp.Body)
			}
			ws.logger.Printf("WebSocket dial failed - status=%d respBody=%s err=%v", resp.StatusCode, string(bodyBytes), err)
			return fmt.Errorf("WebSocket connection failed (status %d): %w", resp.StatusCode, err)
		}
		ws.logger.Printf("WebSocket dial failed - err=%v", err)
		return fmt.Errorf("WebSocket connection failed: %w", err)
	}

	ws.conn = conn
	ws.connected = true
	ws.logger.Println("WebSocket connected successfully")

	// Start read and ping loops for this connection
	go ws.readLoop()
	go ws.pingLoop()

	return nil
}

// connectionManager handles reconnection logic
func (ws *WSClient) connectionManager() {
	ws.logger.Println("connectionManager started")
	currentDelay := ws.reconnectDelay

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-ws.stopChan:
			return
		case <-ws.reconnectChan:
			ws.logger.Printf("Reconnecting in %v...", currentDelay)

			timer := time.NewTimer(currentDelay)
			select {
			case <-ws.ctx.Done():
				timer.Stop()
				ws.logger.Println("connectionManager exiting due to context cancellation")
				return
			case <-ws.stopChan:
				timer.Stop()
				ws.logger.Println("connectionManager exiting due to stopChan")
				return
			case <-timer.C:
				// Attempt reconnection
				if err := ws.connect(); err != nil {
					ws.logger.Printf("Reconnection failed: %v", err)

					// Exponential backoff (up to max)
					currentDelay *= 2
					if currentDelay > ws.maxReconnectDelay {
						currentDelay = ws.maxReconnectDelay
					}

					// Trigger another reconnection attempt
					select {
					case ws.reconnectChan <- struct{}{}:
					default:
					}
				} else {
					// Reset delay on successful connection
					currentDelay = ws.reconnectDelay
				}
			}
		}
	}
}

// readLoop reads messages from the WebSocket connection
func (ws *WSClient) readLoop() {
	defer func() {
		ws.mu.Lock()
		ws.connected = false
		ws.mu.Unlock()

		// Trigger reconnection
		select {
		case ws.reconnectChan <- struct{}{}:
		default:
		}
		ws.logger.Println("readLoop exiting")
	}()

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-ws.stopChan:
			return
		default:
			ws.mu.RLock()
			conn := ws.conn
			ws.mu.RUnlock()

			if conn == nil {
				ws.logger.Println("readLoop: conn is nil, exiting")
				return
			}

			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(ws.readTimeout))

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					ws.logger.Printf("WebSocket read error: %v", err)
				} else {
					ws.logger.Printf("WebSocket read returned error: %v", err)
				}
				return
			}

			// Parse message
			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				ws.logger.Printf("Failed to parse WebSocket message: %v", err)
				continue
			}

			// Always log incoming message type (useful when debugging auth/proxy flows)
			if msg.Type != "" {
				// Try to include agent-visible request_id/url when present
				rid := ""
				if v, ok := msg.Data["request_id"].(string); ok {
					rid = v
				}
				if rid != "" {
					ws.logger.Printf("Received WS message type=%s request_id=%s", msg.Type, rid)
				} else {
					ws.logger.Printf("Received WS message type=%s", msg.Type)
				}
			}

			// Handle message types
			switch msg.Type {
			case MessageTypePong:
				// Pong received, connection is healthy
			case MessageTypeError:
				ws.logger.Printf("Server error: %v", msg.Data)
			case MessageTypeProxyRequest:
				// Handle proxy request from server
				// Log some request details to help trace proxy issues
				if requestID, ok := msg.Data["request_id"].(string); ok {
					if urlStr, ok := msg.Data["url"].(string); ok {
						method := "GET"
						if m, ok := msg.Data["method"].(string); ok && m != "" {
							method = m
						}
						ws.logger.Printf("Incoming proxy_request id=%s method=%s url=%s", requestID, method, urlStr)
					} else {
						ws.logger.Printf("Incoming proxy_request id=%s (no url provided)", requestID)
					}
				}
				go ws.handleProxyRequest(msg)
			default:
				ws.logger.Printf("Unknown message type: %s", msg.Type)
			}
		}
	}
}

// pingLoop sends periodic ping messages to keep connection alive
func (ws *WSClient) pingLoop() {
	ticker := time.NewTicker(ws.pingInterval)
	defer ticker.Stop()
	ws.logger.Println("pingLoop started")
	for {
		select {
		case <-ws.ctx.Done():
			ws.logger.Println("pingLoop exiting due to context cancellation")
			return
		case <-ws.stopChan:
			ws.logger.Println("pingLoop exiting due to stopChan")
			return
		case <-ticker.C:
			ws.mu.RLock()
			conn := ws.conn
			connected := ws.connected
			ws.mu.RUnlock()

			if !connected || conn == nil {
				ws.logger.Println("pingLoop: not connected, exiting")
				return
			}

			// Send ping
			conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				ws.logger.Printf("Failed to send ping: %v", err)
				return
			}
			ws.logger.Println("pingLoop: sent ping")
		}
	}
}

// SendHeartbeat sends a heartbeat message over the WebSocket
func (ws *WSClient) SendHeartbeat(data map[string]interface{}) error {
	ws.mu.RLock()
	conn := ws.conn
	connected := ws.connected
	ws.mu.RUnlock()

	if !connected || conn == nil {
		ws.logger.Println("SendHeartbeat called but WebSocket not connected")
		return errors.New("WebSocket not connected")
	}

	msg := WSMessage{
		Type:      MessageTypeHeartbeat,
		Data:      data,
		Timestamp: time.Now(),
	}

	// Marshal message
	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	// Send message
	conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}

	return nil
}

// handleProxyRequest handles incoming HTTP proxy requests from the server
func (ws *WSClient) handleProxyRequest(msg WSMessage) {
	requestID, ok := msg.Data["request_id"].(string)
	if !ok {
		ws.logger.Printf("Proxy request missing request_id")
		return
	}

	targetURL, ok := msg.Data["url"].(string)
	if !ok {
		ws.sendProxyError(requestID, "Missing target URL")
		return
	}

	method, ok := msg.Data["method"].(string)
	if !ok {
		method = "GET"
	}

	// Extract headers
	headers := make(map[string]string)
	if headersData, ok := msg.Data["headers"].(map[string]interface{}); ok {
		for k, v := range headersData {
			if vStr, ok := v.(string); ok {
				headers[k] = vStr
			}
		}
	}

	// Decode body if present
	var bodyBytes []byte
	if bodyB64, ok := msg.Data["body"].(string); ok {
		decoded, err := base64.StdEncoding.DecodeString(bodyB64)
		if err == nil {
			bodyBytes = decoded
		}
	}

	ws.logger.Printf("Proxying %s request to %s", method, targetURL)

	// If stop requested, bail early
	select {
	case <-ws.ctx.Done():
		ws.logger.Printf("handleProxyRequest %s: context cancelled before proxying", requestID)
		ws.sendProxyError(requestID, "Server shutdown")
		return
	default:
	}

	// Create HTTP client with timeout. Accept self-signed certs from devices
	// because many printers expose self-signed HTTPS interfaces.
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}

	// Create request
	var req *http.Request
	var err error
	if len(bodyBytes) > 0 {
		req, err = http.NewRequest(method, targetURL, bytes.NewReader(bodyBytes))
	} else {
		req, err = http.NewRequest(method, targetURL, nil)
	}

	if err != nil {
		ws.sendProxyError(requestID, fmt.Sprintf("Failed to create request: %v", err))
		return
	}

	// Set headers
	for k, v := range headers {
		// Skip hop-by-hop headers
		if strings.EqualFold(k, "Connection") || strings.EqualFold(k, "Keep-Alive") ||
			strings.EqualFold(k, "Proxy-Authenticate") || strings.EqualFold(k, "Proxy-Authorization") ||
			strings.EqualFold(k, "TE") || strings.EqualFold(k, "Trailers") ||
			strings.EqualFold(k, "Transfer-Encoding") || strings.EqualFold(k, "Upgrade") {
			continue
		}
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		ws.logger.Printf("handleProxyRequest %s: client.Do error: %v", requestID, err)
		ws.sendProxyError(requestID, fmt.Sprintf("Request failed: %v", err))
		return
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		ws.sendProxyError(requestID, fmt.Sprintf("Failed to read response: %v", err))
		return
	}

	// Extract response headers
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	// Send proxy response back to server
	ws.sendProxyResponse(requestID, resp.StatusCode, respHeaders, respBody)
}

// sendProxyResponse sends a successful proxy response back to the server
func (ws *WSClient) sendProxyResponse(requestID string, statusCode int, headers map[string]string, body []byte) {
	ws.mu.RLock()
	conn := ws.conn
	connected := ws.connected
	ws.mu.RUnlock()

	if !connected || conn == nil {
		ws.logger.Printf("Cannot send proxy response - not connected (requestID=%s)", requestID)
		return
	}

	msg := WSMessage{
		Type: MessageTypeProxyResponse,
		Data: map[string]interface{}{
			"request_id":  requestID,
			"status_code": statusCode,
			"headers":     headers,
			"body":        base64.StdEncoding.EncodeToString(body),
		},
		Timestamp: time.Now(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		ws.logger.Printf("Failed to marshal proxy response: %v", err)
		return
	}

	conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		ws.logger.Printf("Failed to send proxy response: %v", err)
	}
}

// sendProxyError sends an error response for a failed proxy request
func (ws *WSClient) sendProxyError(requestID string, errorMsg string) {
	ws.logger.Printf("Proxy error for request %s: %s", requestID, errorMsg)

	ws.sendProxyResponse(requestID, 502, map[string]string{
		"Content-Type": "text/plain",
	}, []byte(errorMsg))
}
