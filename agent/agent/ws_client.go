package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket message types
const (
	MessageTypeHeartbeat = "heartbeat"
	MessageTypePong      = "pong"
	MessageTypeError     = "error"
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
		err := ws.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		if err != nil {
			ws.logger.Printf("Error sending close message: %v", err)
		}

		ws.conn.Close()
		ws.conn = nil
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

	// Create WebSocket dialer with timeouts
	dialer := &websocket.Dialer{
		HandshakeTimeout: ws.handshakeTimeout,
	}

	// Connect
	conn, resp, err := dialer.Dial(u.String(), nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("WebSocket connection failed (status %d): %w", resp.StatusCode, err)
		}
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
				return
			case <-ws.stopChan:
				timer.Stop()
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
				return
			}

			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(ws.readTimeout))

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					ws.logger.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// Parse message
			var msg WSMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				ws.logger.Printf("Failed to parse WebSocket message: %v", err)
				continue
			}

			// Handle message types
			switch msg.Type {
			case MessageTypePong:
				// Pong received, connection is healthy
			case MessageTypeError:
				ws.logger.Printf("Server error: %v", msg.Data)
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

	for {
		select {
		case <-ws.ctx.Done():
			return
		case <-ws.stopChan:
			return
		case <-ticker.C:
			ws.mu.RLock()
			conn := ws.conn
			connected := ws.connected
			ws.mu.RUnlock()

			if !connected || conn == nil {
				return
			}

			// Send ping
			conn.SetWriteDeadline(time.Now().Add(ws.writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				ws.logger.Printf("Failed to send ping: %v", err)
				return
			}
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
