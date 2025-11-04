package agent

import (
	"testing"
)

func TestParseRangeText_SingleIP(t *testing.T) {
	txt := "10.2.106.72"
	res, err := ParseRangeText(txt, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("expected 1 ip, got %d", res.Count)
	}
	if res.IPs[0] != "10.2.106.72" {
		t.Fatalf("unexpected ip: %s", res.IPs[0])
	}
}

func TestParseRangeText_CIDR(t *testing.T) {
	txt := "192.168.10.0/30"
	res, err := ParseRangeText(txt, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 4 {
		t.Fatalf("expected 4 ips, got %d", res.Count)
	}
}

func TestParseRangeText_Wildcard(t *testing.T) {
	txt := "192.168.100.x"
	res, err := ParseRangeText(txt, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Count != 256 {
		t.Fatalf("expected 256 ips, got %d", res.Count)
	}
}

func TestParseRangeText_Invalid(t *testing.T) {
	txt := "not-an-ip"
	res, err := ParseRangeText(txt, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Errors) == 0 {
		t.Fatalf("expected parse errors for invalid input")
	}
}
