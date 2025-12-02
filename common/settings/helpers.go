package settings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

var agentLocalDefaults = DefaultSettings()

// StripAgentLocalFields resets agent-local fields to their defaults so server-managed
// snapshots do not attempt to override runtime-only values.
func StripAgentLocalFields(cfg *Settings) {
	if cfg == nil {
		return
	}
	// Logging section is agent-local
	cfg.Logging = agentLocalDefaults.Logging
	// Web section is agent-local
	cfg.Web = agentLocalDefaults.Web
}

// CopyAgentLocalFields copies agent-local fields from src into dst.
func CopyAgentLocalFields(src Settings, dst *Settings) {
	if dst == nil {
		return
	}
	// Logging section is agent-local
	dst.Logging = src.Logging
	// Web section is agent-local
	dst.Web = src.Web
}

// ComputeSettingsVersion hashes the schema version, update timestamp, and settings payload
// to produce a deterministic change token for sync.
func ComputeSettingsVersion(schemaVersion string, updatedAt time.Time, cfg Settings) (string, error) {
	material := struct {
		SchemaVersion string    `json:"schema_version"`
		UpdatedAt     time.Time `json:"updated_at"`
		Settings      Settings  `json:"settings"`
	}{
		SchemaVersion: schemaVersion,
		UpdatedAt:     updatedAt.UTC(),
		Settings:      cfg,
	}
	b, err := json.Marshal(material)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
