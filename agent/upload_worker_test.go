package main

import (
	"testing"
	"time"

	agentpkg "printmaster/agent/agent"
	pmsettings "printmaster/common/settings"
)

type stubLogger struct{}

func (stubLogger) Error(string, ...interface{}) {}
func (stubLogger) Warn(string, ...interface{})  {}
func (stubLogger) Info(string, ...interface{})  {}
func (stubLogger) Debug(string, ...interface{}) {}

func TestUploadWorkerHandleHeartbeatSettingsPersistsSnapshot(t *testing.T) {
	store := newFakeConfigStore()
	mgr := NewSettingsManager(store)
	prev := settingsManager
	settingsManager = mgr
	t.Cleanup(func() { settingsManager = prev })

	worker := &UploadWorker{settings: mgr, logger: stubLogger{}}
	snap := &agentpkg.SettingsSnapshot{
		Version:       "v1",
		SchemaVersion: "schema-1",
		UpdatedAt:     time.Unix(500, 0),
		Settings:      pmsettings.DefaultSettings(),
	}

	worker.handleHeartbeatSettings(&agentpkg.HeartbeatResult{Snapshot: snap})
	if mgr.CurrentVersion() != "v1" {
		t.Fatalf("expected manager version to update")
	}
	if store.setCount(serverManagedSettingsKey) != 1 {
		t.Fatalf("expected snapshot persisted once, got %d", store.setCount(serverManagedSettingsKey))
	}

	worker.handleHeartbeatSettings(&agentpkg.HeartbeatResult{Snapshot: snap, SettingsVersion: snap.Version})
	if store.setCount(serverManagedSettingsKey) != 1 {
		t.Fatalf("expected no-op when version matches")
	}
}

func TestUploadWorkerHandleHeartbeatSettingsIgnoresNilSnapshot(t *testing.T) {
	worker := &UploadWorker{settings: nil, logger: stubLogger{}}
	worker.handleHeartbeatSettings(&agentpkg.HeartbeatResult{})
}
