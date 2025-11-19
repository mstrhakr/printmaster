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
	cfg.Developer.LogLevel = agentLocalDefaults.Developer.LogLevel
	cfg.Developer.DumpParseDebug = agentLocalDefaults.Developer.DumpParseDebug
	cfg.Developer.ShowLegacy = agentLocalDefaults.Developer.ShowLegacy
	cfg.Security.EnableHTTP = agentLocalDefaults.Security.EnableHTTP
	cfg.Security.EnableHTTPS = agentLocalDefaults.Security.EnableHTTPS
	cfg.Security.HTTPPort = agentLocalDefaults.Security.HTTPPort
	cfg.Security.HTTPSPort = agentLocalDefaults.Security.HTTPSPort
	cfg.Security.RedirectHTTPToHTTPS = agentLocalDefaults.Security.RedirectHTTPToHTTPS
	cfg.Security.CustomCertPath = agentLocalDefaults.Security.CustomCertPath
	cfg.Security.CustomKeyPath = agentLocalDefaults.Security.CustomKeyPath
}

// CopyAgentLocalFields copies agent-local fields from src into dst.
func CopyAgentLocalFields(src Settings, dst *Settings) {
	if dst == nil {
		return
	}
	dst.Developer.LogLevel = src.Developer.LogLevel
	dst.Developer.DumpParseDebug = src.Developer.DumpParseDebug
	dst.Developer.ShowLegacy = src.Developer.ShowLegacy
	dst.Security.EnableHTTP = src.Security.EnableHTTP
	dst.Security.EnableHTTPS = src.Security.EnableHTTPS
	dst.Security.HTTPPort = src.Security.HTTPPort
	dst.Security.HTTPSPort = src.Security.HTTPSPort
	dst.Security.RedirectHTTPToHTTPS = src.Security.RedirectHTTPToHTTPS
	dst.Security.CustomCertPath = src.Security.CustomCertPath
	dst.Security.CustomKeyPath = src.Security.CustomKeyPath
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
