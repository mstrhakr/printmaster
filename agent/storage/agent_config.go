package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// AgentConfigStore handles storage of agent configuration (IP ranges, settings, etc.)
type AgentConfigStore interface {
	// GetRanges returns the saved IP ranges as a newline-separated string
	GetRanges() (string, error)
	// SetRanges saves IP ranges (newline-separated string)
	SetRanges(text string) error
	// GetRangesList returns the saved IP ranges as a slice
	GetRangesList() ([]string, error)
	// SetConfigValue stores any JSON-serializable config value
	SetConfigValue(key string, value interface{}) error
	// DeleteConfigValue removes a stored config value by key
	DeleteConfigValue(key string) error
	// GetConfigValue retrieves any JSON-serializable config value
	GetConfigValue(key string, dest interface{}) error
	// Close closes the database connection
	Close() error
}

// SQLiteAgentConfig implements AgentConfigStore using SQLite
type SQLiteAgentConfig struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewAgentConfigStore creates a new AgentConfigStore with SQLite backend
func NewAgentConfigStore(dbPath string) (AgentConfigStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open agent config database: %w", err)
	}

	store := &SQLiteAgentConfig{db: db}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

// initialize creates the necessary tables
func (s *SQLiteAgentConfig) initialize() error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_config (
		key TEXT PRIMARY KEY,
		value TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_agent_config_key ON agent_config(key);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create agent_config schema: %w", err)
	}

	return nil
}

// GetRanges returns the saved IP ranges as a newline-separated string
func (s *SQLiteAgentConfig) GetRanges() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var value string
	err := s.db.QueryRow("SELECT value FROM agent_config WHERE key = ?", "ip_ranges").Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil // No ranges saved yet
	}
	if err != nil {
		return "", fmt.Errorf("failed to get ranges: %w", err)
	}

	return value, nil
}

// SetRanges saves IP ranges (newline-separated string)
func (s *SQLiteAgentConfig) SetRanges(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO agent_config (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, "ip_ranges", text)

	if err != nil {
		return fmt.Errorf("failed to save ranges: %w", err)
	}

	return nil
}

// GetRangesList returns the saved IP ranges as a slice
func (s *SQLiteAgentConfig) GetRangesList() ([]string, error) {
	text, err := s.GetRanges()
	if err != nil {
		return nil, err
	}

	if text == "" {
		return []string{}, nil
	}

	// Split by newlines and filter empty lines
	lines := strings.Split(text, "\n")
	ranges := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			ranges = append(ranges, line)
		}
	}

	return ranges, nil
}

// Close closes the database connection
func (s *SQLiteAgentConfig) Close() error {
	return s.db.Close()
}

// Helper function to store any JSON-serializable config value
func (s *SQLiteAgentConfig) SetConfigValue(key string, value interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal config value: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO agent_config (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
	`, key, string(jsonValue))

	if err != nil {
		return fmt.Errorf("failed to save config value: %w", err)
	}

	return nil
}

// Helper function to retrieve any JSON-serializable config value
func (s *SQLiteAgentConfig) GetConfigValue(key string, dest interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var value string
	err := s.db.QueryRow("SELECT value FROM agent_config WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil // Key not found, dest remains unchanged
	}
	if err != nil {
		return fmt.Errorf("failed to get config value: %w", err)
	}

	if err := json.Unmarshal([]byte(value), dest); err != nil {
		return fmt.Errorf("failed to unmarshal config value: %w", err)
	}

	return nil
}

// DeleteConfigValue removes a key from the config store
func (s *SQLiteAgentConfig) DeleteConfigValue(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM agent_config WHERE key = ?`, key)
	if err != nil {
		return fmt.Errorf("failed to delete config value: %w", err)
	}
	return nil
}
