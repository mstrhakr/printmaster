package ws

import (
	"encoding/json"
	"time"
)

// Message is the shared WebSocket message shape used by server and clients.
type Message struct {
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp,omitempty"`
}

// Marshal marshals the message to JSON bytes.
func (m *Message) Marshal() ([]byte, error) {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}
	return json.Marshal(m)
}

// Note: writing JSON to a *websocket.Conn is intentionally left to the
// caller to avoid dragging websocket dependency into this package's
// go.mod. Use Message.Marshal() and write bytes with an appropriate
// deadline in your server/agent code.

// Standard message type constants used by server/agent/UI
const (
	MessageTypeHeartbeat     = "heartbeat"
	MessageTypePong          = "pong"
	MessageTypeError         = "error"
	MessageTypeProxyRequest  = "proxy_request"
	MessageTypeProxyResponse = "proxy_response"
	MessageTypeCommand       = "command"        // Server-to-agent command
	MessageTypeCommandResult = "command_result" // Agent-to-server command response
)
