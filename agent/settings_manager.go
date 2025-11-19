package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"printmaster/agent/agent"
	"printmaster/agent/storage"
	pmsettings "printmaster/common/settings"
)

const serverManagedSettingsKey = "server_managed_settings"

type serverManagedSettings struct {
	Version       string              `json:"version"`
	SchemaVersion string              `json:"schema_version"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Settings      pmsettings.Settings `json:"settings"`
}

// SettingsManager tracks server-managed snapshots and composes effective configs.
type SettingsManager struct {
	store   storage.AgentConfigStore
	mu      sync.RWMutex
	managed *serverManagedSettings
}

func NewSettingsManager(store storage.AgentConfigStore) *SettingsManager {
	mgr := &SettingsManager{store: store}
	mgr.reload()
	return mgr
}

func (m *SettingsManager) reload() {
	if m == nil || m.store == nil {
		return
	}
	var payload serverManagedSettings
	if err := m.store.GetConfigValue(serverManagedSettingsKey, &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.Version) == "" {
		return
	}
	pmsettings.Sanitize(&payload.Settings)
	m.mu.Lock()
	m.managed = &payload
	m.mu.Unlock()
}

func (m *SettingsManager) CurrentVersion() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.managed == nil {
		return ""
	}
	return m.managed.Version
}

func (m *SettingsManager) HasManagedSnapshot() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.managed != nil
}

func (m *SettingsManager) baseSettings() (pmsettings.Settings, bool) {
	if m == nil {
		return pmsettings.DefaultSettings(), false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.managed == nil {
		return pmsettings.DefaultSettings(), false
	}
	return m.managed.Settings, true
}

func (m *SettingsManager) ApplyServerSnapshot(snapshot *agent.SettingsSnapshot) (pmsettings.Settings, error) {
	if m == nil || m.store == nil {
		return pmsettings.Settings{}, fmt.Errorf("settings manager unavailable")
	}
	if snapshot == nil || strings.TrimSpace(snapshot.Version) == "" {
		return pmsettings.Settings{}, fmt.Errorf("invalid snapshot")
	}
	payload := serverManagedSettings{
		Version:       snapshot.Version,
		SchemaVersion: snapshot.SchemaVersion,
		UpdatedAt:     snapshot.UpdatedAt,
		Settings:      snapshot.Settings,
	}
	pmsettings.Sanitize(&payload.Settings)
	if err := m.store.SetConfigValue(serverManagedSettingsKey, payload); err != nil {
		return pmsettings.Settings{}, err
	}
	m.mu.Lock()
	m.managed = &payload
	m.mu.Unlock()
	return loadUnifiedSettings(m.store), nil
}
