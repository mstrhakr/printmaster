package ws

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMessageMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		msg     Message
		wantErr bool
	}{
		{
			name: "basic message",
			msg: Message{
				Type: MessageTypeHeartbeat,
			},
			wantErr: false,
		},
		{
			name: "message with data",
			msg: Message{
				Type: MessageTypeCommand,
				Data: map[string]interface{}{
					"action": "scan",
					"count":  42,
				},
			},
			wantErr: false,
		},
		{
			name: "message with timestamp",
			msg: Message{
				Type:      MessageTypePong,
				Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := tc.msg.Marshal()
			if (err != nil) != tc.wantErr {
				t.Errorf("Marshal() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if err == nil && len(data) == 0 {
				t.Error("Marshal() returned empty data")
			}
		})
	}
}

func TestMessageMarshalSetsTimestamp(t *testing.T) {
	t.Parallel()

	msg := Message{
		Type: MessageTypeHeartbeat,
	}

	before := time.Now()
	data, err := msg.Marshal()
	after := time.Now()

	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Timestamp.IsZero() {
		t.Error("Marshal() did not set timestamp")
	}
	if decoded.Timestamp.Before(before) || decoded.Timestamp.After(after) {
		t.Error("Marshal() set timestamp outside expected range")
	}
}

func TestMessageMarshalPreservesExistingTimestamp(t *testing.T) {
	t.Parallel()

	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	msg := Message{
		Type:      MessageTypeHeartbeat,
		Timestamp: ts,
	}

	data, err := msg.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !decoded.Timestamp.Equal(ts) {
		t.Errorf("Marshal() changed timestamp: got %v, want %v", decoded.Timestamp, ts)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	t.Parallel()

	original := Message{
		Type: MessageTypeProxyRequest,
		Data: map[string]interface{}{
			"url":    "/api/devices",
			"method": "GET",
		},
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.Data["url"] != original.Data["url"] {
		t.Errorf("Data[url] mismatch: got %v, want %v", decoded.Data["url"], original.Data["url"])
	}
	if decoded.Data["method"] != original.Data["method"] {
		t.Errorf("Data[method] mismatch: got %v, want %v", decoded.Data["method"], original.Data["method"])
	}
}

func TestMessageConstants(t *testing.T) {
	t.Parallel()

	// Ensure constants are non-empty and unique
	constants := []string{
		MessageTypeHeartbeat,
		MessageTypePong,
		MessageTypeError,
		MessageTypeProxyRequest,
		MessageTypeProxyResponse,
		MessageTypeCommand,
		MessageTypeCommandResult,
		MessageTypeUpdateProgress,
		MessageTypeJobProgress,
	}

	seen := make(map[string]bool)
	for _, c := range constants {
		if c == "" {
			t.Error("found empty message type constant")
		}
		if seen[c] {
			t.Errorf("duplicate message type constant: %q", c)
		}
		seen[c] = true
	}
}
