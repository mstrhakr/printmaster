package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

type fakeAgentConfigStore struct {
	values map[string]interface{}
}

func newFakeAgentConfigStore() *fakeAgentConfigStore {
	return &fakeAgentConfigStore{values: make(map[string]interface{})}
}

func (f *fakeAgentConfigStore) GetRanges() (string, error)       { return "", nil }
func (f *fakeAgentConfigStore) SetRanges(string) error           { return nil }
func (f *fakeAgentConfigStore) GetRangesList() ([]string, error) { return nil, nil }
func (f *fakeAgentConfigStore) SetConfigValue(key string, value interface{}) error {
	if f.values == nil {
		f.values = make(map[string]interface{})
	}
	f.values[key] = value
	return nil
}
func (f *fakeAgentConfigStore) DeleteConfigValue(key string) error {
	delete(f.values, key)
	return nil
}
func (f *fakeAgentConfigStore) GetConfigValue(key string, dest interface{}) error {
	val, ok := f.values[key]
	if !ok {
		return fmt.Errorf("key %s not found", key)
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dest)
}
func (f *fakeAgentConfigStore) Close() error { return nil }

func TestApplyServerConfigFromStoreMergesPersistedSettings(t *testing.T) {
	store := newFakeAgentConfigStore()
	_ = store.SetConfigValue("server", ServerConnectionConfig{
		Enabled:            false,
		URL:                "https://central.example.com",
		Name:               "Lab",
		CAPath:             "/tmp/ca.pem",
		InsecureSkipVerify: true,
		UploadInterval:     90,
		HeartbeatInterval:  25,
		AgentID:            "agent-123",
		Token:              "token-xyz",
	})

	cfg := DefaultAgentConfig()
	cfg.Server.Enabled = false
	cfg.Server.UploadInterval = 10
	cfg.Server.HeartbeatInterval = 5

	applyServerConfigFromStore(cfg, store, nil)

	if !cfg.Server.Enabled {
		t.Fatalf("expected server integration to be enabled")
	}
	if cfg.Server.URL != "https://central.example.com" {
		t.Fatalf("unexpected server url: %s", cfg.Server.URL)
	}
	if cfg.Server.Name != "Lab" {
		t.Fatalf("unexpected server name: %s", cfg.Server.Name)
	}
	if !cfg.Server.InsecureSkipVerify {
		t.Fatalf("expected insecure skip verify to be true")
	}
	if cfg.Server.CAPath != "/tmp/ca.pem" {
		t.Fatalf("unexpected CA path: %s", cfg.Server.CAPath)
	}
	if cfg.Server.AgentID != "agent-123" {
		t.Fatalf("unexpected agent id: %s", cfg.Server.AgentID)
	}
	if cfg.Server.Token != "token-xyz" {
		t.Fatalf("unexpected token: %s", cfg.Server.Token)
	}
	if cfg.Server.UploadInterval != 90 {
		t.Fatalf("expected upload interval 90, got %d", cfg.Server.UploadInterval)
	}
	if cfg.Server.HeartbeatInterval != 25 {
		t.Fatalf("expected heartbeat interval 25, got %d", cfg.Server.HeartbeatInterval)
	}
}

func TestApplyServerConfigFromStorePreservesNonPositiveIntervals(t *testing.T) {
	store := newFakeAgentConfigStore()
	_ = store.SetConfigValue("server", ServerConnectionConfig{
		URL:               "https://central.example.com",
		UploadInterval:    0,
		HeartbeatInterval: -10,
	})

	cfg := DefaultAgentConfig()
	cfg.Server.UploadInterval = 120
	cfg.Server.HeartbeatInterval = 60

	applyServerConfigFromStore(cfg, store, nil)

	if cfg.Server.UploadInterval != 120 {
		t.Fatalf("upload interval should remain 120, got %d", cfg.Server.UploadInterval)
	}
	if cfg.Server.HeartbeatInterval != 60 {
		t.Fatalf("heartbeat interval should remain 60, got %d", cfg.Server.HeartbeatInterval)
	}
}
