package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	commonconfig "printmaster/common/config"
	"strconv"
	"testing"
)

func TestRunAgentHealthCheckHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(handleHealth))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, _ := strconv.Atoi(parsed.Port())

	cfg := DefaultAgentConfig()
	cfg.Web.HTTPPort = port
	cfg.Web.HTTPSPort = 0

	configPath := filepath.Join(t.TempDir(), "agent-config-http.toml")
	if err := commonconfig.WriteTOML(configPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := runAgentHealthCheck(configPath); err != nil {
		t.Fatalf("expected health check success, got error: %v", err)
	}
}

func TestRunAgentHealthCheckHTTPS(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(handleHealth))
	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, _ := strconv.Atoi(parsed.Port())

	cfg := DefaultAgentConfig()
	cfg.Web.HTTPPort = 0
	cfg.Web.HTTPSPort = port

	configPath := filepath.Join(t.TempDir(), "agent-config-https.toml")
	if err := commonconfig.WriteTOML(configPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := runAgentHealthCheck(configPath); err != nil {
		t.Fatalf("expected health check success, got error: %v", err)
	}
}

func TestRunAgentHealthCheckFailure(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := DefaultAgentConfig()
	cfg.Web.HTTPPort = port
	cfg.Web.HTTPSPort = 0

	configPath := filepath.Join(t.TempDir(), "agent-config-fail.toml")
	if err := commonconfig.WriteTOML(configPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := runAgentHealthCheck(configPath); err == nil {
		t.Fatalf("expected health check to fail, but it succeeded")
	}
}
