package ws

import (
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Conn is a thin wrapper around *websocket.Conn exposing small helper methods
// used by server and agent code.
type Conn struct {
	c *websocket.Conn
	// writeMu serializes all writes to the underlying websocket.Conn.
	// Gorilla websocket Conn panics on concurrent writes; protect against that here.
	writeMu sync.Mutex
}

// Dial connects to the given WebSocket URL and returns a wrapped Conn and HTTP response.
// tlsCfg may be nil to use default TLS settings.
func Dial(urlStr string, reqHeader http.Header, tlsCfg *tls.Config, handshakeTimeout time.Duration) (*Conn, *http.Response, error) {
	dialer := &websocket.Dialer{HandshakeTimeout: handshakeTimeout, TLSClientConfig: tlsCfg}
	c, resp, err := dialer.Dial(urlStr, reqHeader)
	if err != nil {
		return nil, resp, err
	}
	return &Conn{c: c}, resp, nil
}

// UpgradeHTTP upgrades an incoming HTTP request to a websocket Conn using a permissive upgrader.
func UpgradeHTTP(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	return &Conn{c: c}, nil
}

// ReadMessage reads a text message and returns the raw bytes.
func (cw *Conn) ReadMessage() ([]byte, error) {
	if cw == nil || cw.c == nil {
		return nil, errors.New("websocket: connection is closed")
	}
	_, msg, err := cw.c.ReadMessage()
	return msg, err
}

// WriteMessage writes a ws Message as JSON with a write deadline.
func (cw *Conn) WriteMessage(msg *Message, timeout time.Duration) error {
	if cw == nil || cw.c == nil {
		return errors.New("websocket: connection is closed")
	}
	// Serialize write operations to avoid gorilla websocket concurrent write panics.
	cw.writeMu.Lock()
	defer cw.writeMu.Unlock()

	if timeout > 0 {
		cw.c.SetWriteDeadline(time.Now().Add(timeout))
	}
	return cw.c.WriteJSON(msg)
}

// WriteRaw writes raw bytes as a text message.
func (cw *Conn) WriteRaw(b []byte, timeout time.Duration) error {
	if cw == nil || cw.c == nil {
		return errors.New("websocket: connection is closed")
	}
	// Serialize write operations to avoid gorilla websocket concurrent write panics.
	cw.writeMu.Lock()
	defer cw.writeMu.Unlock()

	if timeout > 0 {
		cw.c.SetWriteDeadline(time.Now().Add(timeout))
	}
	return cw.c.WriteMessage(websocket.TextMessage, b)
}

// SetWriteDeadline sets write deadline on underlying conn.
func (cw *Conn) SetWriteDeadline(t time.Time) error {
	if cw == nil || cw.c == nil {
		return errors.New("websocket: connection is closed")
	}
	return cw.c.SetWriteDeadline(t)
}

// WritePing sends a ping control message.
func (cw *Conn) WritePing(timeout time.Duration) error {
	if cw == nil || cw.c == nil {
		return errors.New("websocket: connection is closed")
	}
	// Serialize write operations to avoid gorilla websocket concurrent write panics.
	cw.writeMu.Lock()
	defer cw.writeMu.Unlock()

	if timeout > 0 {
		cw.c.SetWriteDeadline(time.Now().Add(timeout))
	}
	return cw.c.WriteMessage(websocket.PingMessage, nil)
}

// Close closes the underlying websocket connection.
func (cw *Conn) Close() error {
	if cw == nil || cw.c == nil {
		return nil
	}
	return cw.c.Close()
}

// SetReadDeadline sets read deadline on underlying conn.
func (cw *Conn) SetReadDeadline(t time.Time) error {
	if cw == nil || cw.c == nil {
		return errors.New("websocket: connection is closed")
	}
	return cw.c.SetReadDeadline(t)
}

// SetPongHandler sets the pong handler.
func (cw *Conn) SetPongHandler(h func(string) error) {
	if cw == nil || cw.c == nil {
		return
	}
	cw.c.SetPongHandler(h)
}

// RemoteAddr returns the remote address if available.
func (cw *Conn) RemoteAddr() string {
	if cw == nil || cw.c == nil || cw.c.RemoteAddr() == nil {
		return ""
	}
	return cw.c.RemoteAddr().String()
}

// FormatCloseMessage returns a close control message.
func FormatCloseMessage(code int, text string) []byte {
	return websocket.FormatCloseMessage(code, text)
}

// CloseNormalClosure constant
const CloseNormalClosure = websocket.CloseNormalClosure

// IsUnexpectedCloseError helper
func IsUnexpectedCloseError(err error, codes ...int) bool {
	return websocket.IsUnexpectedCloseError(err, codes...)
}
