package main

import (
	"testing"
)

func TestInjectProxyMetaAndBase(t *testing.T) {
	sample := `<!doctype html><html><head><title>Agent UI</title></head><body><img src="http://127.0.0.1:8080/static/logo.png"></body></html>`
	agentID := "agent-123"
	targetURL := "http://127.0.0.1:8080/"

	proxyBase := "/api/v1/proxy/agent/" + agentID + "/"
	out := injectProxyMetaAndBase([]byte(sample), proxyBase, agentID, targetURL)
	s := string(out)

	// Expect meta tag and base present
	if !contains(s, `http-equiv="X-PrintMaster-Proxied"`) {
		t.Fatalf("injected HTML missing proxied meta: %s", s)
	}
	if !contains(s, `<base href="/api/v1/proxy/agent/`+agentID+`/">`) {
		t.Fatalf("injected HTML missing base tag: %s", s)
	}

	// Expect the absolute origin replaced by proxy base
	if contains(s, "http://127.0.0.1:8080/static/logo.png") {
		t.Fatalf("origin was not rewritten: %s", s)
	}
}

// contains is a tiny helper to avoid importing strings in the test body
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return (len(s) >= len(sub) && indexOf(s, sub) >= 0) })()
}

// indexOf - naive implementation
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
