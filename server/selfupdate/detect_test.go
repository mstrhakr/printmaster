package selfupdate

import "testing"

func TestForcedEnvDisable(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		expect bool
	}{
		{"blank", "", false},
		{"true-lower", "true", true},
		{"true-upper", "TRUE", true},
		{"one", "1", true},
		{"false", "false", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := forcedEnvDisable(tt.value)
			if ok != tt.expect {
				t.Fatalf("expected %v, got %v", tt.expect, ok)
			}
		})
	}
}
