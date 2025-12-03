package ws

import (
	"testing"
	"time"
)

func TestConnNilSafety(t *testing.T) {
	t.Parallel()

	// Test that nil Conn doesn't panic on method calls
	t.Run("nil Conn", func(t *testing.T) {
		t.Parallel()
		var conn *Conn

		// ReadMessage should return error
		_, err := conn.ReadMessage()
		if err == nil {
			t.Error("ReadMessage on nil Conn should return error")
		}

		// WriteMessage should return error
		err = conn.WriteMessage(&Message{Type: "test"}, time.Second)
		if err == nil {
			t.Error("WriteMessage on nil Conn should return error")
		}

		// WriteRaw should return error
		err = conn.WriteRaw([]byte("test"), time.Second)
		if err == nil {
			t.Error("WriteRaw on nil Conn should return error")
		}

		// WritePing should return error
		err = conn.WritePing(time.Second)
		if err == nil {
			t.Error("WritePing on nil Conn should return error")
		}

		// SetWriteDeadline should return error
		err = conn.SetWriteDeadline(time.Now())
		if err == nil {
			t.Error("SetWriteDeadline on nil Conn should return error")
		}

		// SetReadDeadline should return error
		err = conn.SetReadDeadline(time.Now())
		if err == nil {
			t.Error("SetReadDeadline on nil Conn should return error")
		}

		// Close should not panic and return nil
		err = conn.Close()
		if err != nil {
			t.Errorf("Close on nil Conn should return nil, got %v", err)
		}

		// RemoteAddr should return empty string
		addr := conn.RemoteAddr()
		if addr != "" {
			t.Errorf("RemoteAddr on nil Conn should return empty string, got %q", addr)
		}

		// SetPongHandler should not panic
		conn.SetPongHandler(func(s string) error { return nil })
	})

	t.Run("Conn with nil underlying", func(t *testing.T) {
		t.Parallel()
		conn := &Conn{c: nil}

		// ReadMessage should return error
		_, err := conn.ReadMessage()
		if err == nil {
			t.Error("ReadMessage on Conn with nil c should return error")
		}

		// WriteMessage should return error
		err = conn.WriteMessage(&Message{Type: "test"}, time.Second)
		if err == nil {
			t.Error("WriteMessage on Conn with nil c should return error")
		}

		// Close should not panic
		err = conn.Close()
		if err != nil {
			t.Errorf("Close on Conn with nil c should return nil, got %v", err)
		}

		// RemoteAddr should return empty string
		addr := conn.RemoteAddr()
		if addr != "" {
			t.Errorf("RemoteAddr on Conn with nil c should return empty string, got %q", addr)
		}
	})
}

func TestFormatCloseMessage(t *testing.T) {
	t.Parallel()

	msg := FormatCloseMessage(CloseNormalClosure, "goodbye")
	if len(msg) == 0 {
		t.Error("FormatCloseMessage returned empty slice")
	}
}

func TestCloseNormalClosureConstant(t *testing.T) {
	t.Parallel()

	// CloseNormalClosure should be 1000 per WebSocket spec
	if CloseNormalClosure != 1000 {
		t.Errorf("CloseNormalClosure = %d, want 1000", CloseNormalClosure)
	}
}
