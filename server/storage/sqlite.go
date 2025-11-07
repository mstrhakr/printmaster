package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO required)
)

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

const schemaVersion = 1

// NewSQLiteStore creates a new SQLite-backed store
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure directory exists (unless in-memory)
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	// Build connection string with pragmas (skip for in-memory databases)
	connStr := dbPath
	if dbPath != ":memory:" {
		connStr += "?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-64000&_foreign_keys=ON"
	} else {
		connStr += "?_foreign_keys=ON"
	}

	// Open database
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	store := &SQLiteStore{
		db:     db,
		dbPath: dbPath,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return store, nil
}

// initSchema creates tables if they don't exist
func (s *SQLiteStore) initSchema() error {
	schema := `
	-- Schema version tracking
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Registered agents
	CREATE TABLE IF NOT EXISTS agents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		agent_id TEXT NOT NULL UNIQUE,
		hostname TEXT NOT NULL,
		ip TEXT NOT NULL,
		platform TEXT NOT NULL,
		version TEXT NOT NULL,
		protocol_version TEXT NOT NULL,
		token TEXT NOT NULL,
		registered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'active'
	);

	CREATE INDEX IF NOT EXISTS idx_agents_agent_id ON agents(agent_id);
	CREATE INDEX IF NOT EXISTS idx_agents_last_seen ON agents(last_seen);
	CREATE INDEX IF NOT EXISTS idx_agents_token ON agents(token);

	-- Devices discovered by agents
	CREATE TABLE IF NOT EXISTS devices (
		serial TEXT PRIMARY KEY,
		agent_id TEXT NOT NULL,
		ip TEXT NOT NULL,
		manufacturer TEXT,
		model TEXT,
		hostname TEXT,
		firmware TEXT,
		mac_address TEXT,
		subnet_mask TEXT,
		gateway TEXT,
		consumables TEXT,
		status_messages TEXT,
		last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		first_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		discovery_method TEXT,
		asset_number TEXT,
		location TEXT,
		description TEXT,
		web_ui_url TEXT,
		raw_data TEXT,
		FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_devices_agent_id ON devices(agent_id);
	CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
	CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);

	-- Metrics history
	CREATE TABLE IF NOT EXISTS metrics_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		serial TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		page_count INTEGER DEFAULT 0,
		color_pages INTEGER DEFAULT 0,
		mono_pages INTEGER DEFAULT 0,
		scan_count INTEGER DEFAULT 0,
		toner_levels TEXT,
		FOREIGN KEY(serial) REFERENCES devices(serial) ON DELETE CASCADE,
		FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_serial ON metrics_history(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_agent_id ON metrics_history(agent_id);
	CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics_history(timestamp);

	-- Audit log for agent operations
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		agent_id TEXT NOT NULL,
		action TEXT NOT NULL,
		details TEXT,
		ip_address TEXT,
		FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_audit_agent_id ON audit_log(agent_id);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Check/update schema version
	var currentVersion int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}

	if currentVersion < schemaVersion {
		_, err = s.db.Exec("INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (?, ?)",
			schemaVersion, time.Now())
		if err != nil {
			return fmt.Errorf("failed to update schema version: %w", err)
		}
	}

	return nil
}

// RegisterAgent registers a new agent or updates existing
func (s *SQLiteStore) RegisterAgent(ctx context.Context, agent *Agent) error {
	query := `
		INSERT INTO agents (agent_id, hostname, ip, platform, version, protocol_version, token, registered_at, last_seen, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			hostname = excluded.hostname,
			ip = excluded.ip,
			platform = excluded.platform,
			version = excluded.version,
			protocol_version = excluded.protocol_version,
			token = excluded.token,
			last_seen = excluded.last_seen,
			status = excluded.status
	`

	_, err := s.db.ExecContext(ctx, query,
		agent.AgentID, agent.Hostname, agent.IP, agent.Platform,
		agent.Version, agent.ProtocolVersion, agent.Token, agent.RegisteredAt,
		agent.LastSeen, agent.Status)

	return err
}

// GetAgent retrieves an agent by ID
func (s *SQLiteStore) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	query := `
		SELECT id, agent_id, hostname, ip, platform, version, protocol_version,
		       token, registered_at, last_seen, status
		FROM agents
		WHERE agent_id = ?
	`

	var agent Agent
	err := s.db.QueryRowContext(ctx, query, agentID).Scan(
		&agent.ID, &agent.AgentID, &agent.Hostname, &agent.IP,
		&agent.Platform, &agent.Version, &agent.ProtocolVersion,
		&agent.Token, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	if err != nil {
		return nil, err
	}

	return &agent, nil
}

// ListAgents returns all registered agents
func (s *SQLiteStore) ListAgents(ctx context.Context) ([]*Agent, error) {
	query := `
		SELECT id, agent_id, hostname, ip, platform, version, protocol_version,
		       token, registered_at, last_seen, status
		FROM agents
		ORDER BY last_seen DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		var agent Agent
		err := rows.Scan(
			&agent.ID, &agent.AgentID, &agent.Hostname, &agent.IP,
			&agent.Platform, &agent.Version, &agent.ProtocolVersion,
			&agent.Token, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
		)
		if err != nil {
			return nil, err
		}
		agents = append(agents, &agent)
	}

	return agents, rows.Err()
}

// GetAgentByToken retrieves an agent by bearer token
func (s *SQLiteStore) GetAgentByToken(ctx context.Context, token string) (*Agent, error) {
	query := `
		SELECT id, agent_id, hostname, ip, platform, version, protocol_version,
		       token, registered_at, last_seen, status
		FROM agents
		WHERE token = ?
	`

	var agent Agent
	err := s.db.QueryRowContext(ctx, query, token).Scan(
		&agent.ID, &agent.AgentID, &agent.Hostname, &agent.IP,
		&agent.Platform, &agent.Version, &agent.ProtocolVersion,
		&agent.Token, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid token")
	}
	if err != nil {
		return nil, err
	}

	return &agent, nil
}

// UpdateAgentHeartbeat updates agent's last_seen timestamp
func (s *SQLiteStore) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string) error {
	query := `UPDATE agents SET last_seen = ?, status = ? WHERE agent_id = ?`
	_, err := s.db.ExecContext(ctx, query, time.Now(), status, agentID)
	return err
}

// UpsertDevice inserts or updates a device
func (s *SQLiteStore) UpsertDevice(ctx context.Context, device *Device) error {
	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	rawDataJSON, _ := json.Marshal(device.RawData)

	query := `
		INSERT INTO devices (
			serial, agent_id, ip, manufacturer, model, hostname, firmware,
			mac_address, subnet_mask, gateway, consumables, status_messages,
			last_seen, first_seen, created_at, discovery_method,
			asset_number, location, description, web_ui_url, raw_data
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(serial) DO UPDATE SET
			agent_id = excluded.agent_id,
			ip = excluded.ip,
			manufacturer = excluded.manufacturer,
			model = excluded.model,
			hostname = excluded.hostname,
			firmware = excluded.firmware,
			mac_address = excluded.mac_address,
			subnet_mask = excluded.subnet_mask,
			gateway = excluded.gateway,
			consumables = excluded.consumables,
			status_messages = excluded.status_messages,
			last_seen = excluded.last_seen,
			discovery_method = excluded.discovery_method,
			raw_data = excluded.raw_data
	`

	_, err := s.db.ExecContext(ctx, query,
		device.Serial, device.AgentID, device.IP, device.Manufacturer,
		device.Model, device.Hostname, device.Firmware, device.MACAddress,
		device.SubnetMask, device.Gateway, string(consumablesJSON),
		string(statusJSON), device.LastSeen, device.FirstSeen,
		device.CreatedAt, device.DiscoveryMethod, device.AssetNumber,
		device.Location, device.Description, device.WebUIURL,
		string(rawDataJSON),
	)

	return err
}

// GetDevice retrieves a device by serial
func (s *SQLiteStore) GetDevice(ctx context.Context, serial string) (*Device, error) {
	query := `
		SELECT serial, agent_id, ip, manufacturer, model, hostname, firmware,
		       mac_address, subnet_mask, gateway, consumables, status_messages,
		       last_seen, first_seen, created_at, discovery_method,
		       asset_number, location, description, web_ui_url, raw_data
		FROM devices
		WHERE serial = ?
	`

	var device Device
	var consumablesJSON, statusJSON, rawDataJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, serial).Scan(
		&device.Serial, &device.AgentID, &device.IP, &device.Manufacturer,
		&device.Model, &device.Hostname, &device.Firmware, &device.MACAddress,
		&device.SubnetMask, &device.Gateway, &consumablesJSON, &statusJSON,
		&device.LastSeen, &device.FirstSeen, &device.CreatedAt,
		&device.DiscoveryMethod, &device.AssetNumber, &device.Location,
		&device.Description, &device.WebUIURL, &rawDataJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("device not found: %s", serial)
	}
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON fields
	if consumablesJSON.Valid {
		json.Unmarshal([]byte(consumablesJSON.String), &device.Consumables)
	}
	if statusJSON.Valid {
		json.Unmarshal([]byte(statusJSON.String), &device.StatusMessages)
	}
	if rawDataJSON.Valid {
		json.Unmarshal([]byte(rawDataJSON.String), &device.RawData)
	}

	return &device, nil
}

// ListDevices returns devices for a specific agent
func (s *SQLiteStore) ListDevices(ctx context.Context, agentID string) ([]*Device, error) {
	query := `
		SELECT serial, agent_id, ip, manufacturer, model, hostname, firmware,
		       mac_address, subnet_mask, gateway, consumables, status_messages,
		       last_seen, first_seen, created_at, discovery_method,
		       asset_number, location, description, web_ui_url, raw_data
		FROM devices
		WHERE agent_id = ?
		ORDER BY last_seen DESC
	`

	return s.queryDevices(ctx, query, agentID)
}

// ListAllDevices returns all devices from all agents
func (s *SQLiteStore) ListAllDevices(ctx context.Context) ([]*Device, error) {
	query := `
		SELECT serial, agent_id, ip, manufacturer, model, hostname, firmware,
		       mac_address, subnet_mask, gateway, consumables, status_messages,
		       last_seen, first_seen, created_at, discovery_method,
		       asset_number, location, description, web_ui_url, raw_data
		FROM devices
		ORDER BY last_seen DESC
	`

	return s.queryDevices(ctx, query)
}

// queryDevices is a helper for device queries
func (s *SQLiteStore) queryDevices(ctx context.Context, query string, args ...interface{}) ([]*Device, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []*Device
	for rows.Next() {
		var device Device
		var consumablesJSON, statusJSON, rawDataJSON sql.NullString

		err := rows.Scan(
			&device.Serial, &device.AgentID, &device.IP, &device.Manufacturer,
			&device.Model, &device.Hostname, &device.Firmware, &device.MACAddress,
			&device.SubnetMask, &device.Gateway, &consumablesJSON, &statusJSON,
			&device.LastSeen, &device.FirstSeen, &device.CreatedAt,
			&device.DiscoveryMethod, &device.AssetNumber, &device.Location,
			&device.Description, &device.WebUIURL, &rawDataJSON,
		)
		if err != nil {
			return nil, err
		}

		// Unmarshal JSON fields
		if consumablesJSON.Valid {
			json.Unmarshal([]byte(consumablesJSON.String), &device.Consumables)
		}
		if statusJSON.Valid {
			json.Unmarshal([]byte(statusJSON.String), &device.StatusMessages)
		}
		if rawDataJSON.Valid {
			json.Unmarshal([]byte(rawDataJSON.String), &device.RawData)
		}

		devices = append(devices, &device)
	}

	return devices, rows.Err()
}

// SaveMetrics saves metrics snapshot
func (s *SQLiteStore) SaveMetrics(ctx context.Context, metrics *MetricsSnapshot) error {
	tonerJSON, _ := json.Marshal(metrics.TonerLevels)

	query := `
		INSERT INTO metrics_history (
			serial, agent_id, timestamp, page_count, color_pages,
			mono_pages, scan_count, toner_levels
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		metrics.Serial, metrics.AgentID, metrics.Timestamp,
		metrics.PageCount, metrics.ColorPages, metrics.MonoPages,
		metrics.ScanCount, string(tonerJSON),
	)

	return err
}

// GetLatestMetrics retrieves the most recent metrics for a device
func (s *SQLiteStore) GetLatestMetrics(ctx context.Context, serial string) (*MetricsSnapshot, error) {
	query := `
		SELECT id, serial, agent_id, timestamp, page_count, color_pages,
		       mono_pages, scan_count, toner_levels
		FROM metrics_history
		WHERE serial = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var metrics MetricsSnapshot
	var tonerJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, serial).Scan(
		&metrics.ID, &metrics.Serial, &metrics.AgentID, &metrics.Timestamp,
		&metrics.PageCount, &metrics.ColorPages, &metrics.MonoPages,
		&metrics.ScanCount, &tonerJSON,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no metrics found for device: %s", serial)
	}
	if err != nil {
		return nil, err
	}

	if tonerJSON.Valid {
		json.Unmarshal([]byte(tonerJSON.String), &metrics.TonerLevels)
	}

	return &metrics, nil
}

// GetMetricsHistory retrieves metrics history since a given time
func (s *SQLiteStore) GetMetricsHistory(ctx context.Context, serial string, since time.Time) ([]*MetricsSnapshot, error) {
	query := `
		SELECT id, serial, agent_id, timestamp, page_count, color_pages,
		       mono_pages, scan_count, toner_levels
		FROM metrics_history
		WHERE serial = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query, serial, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*MetricsSnapshot
	for rows.Next() {
		var metrics MetricsSnapshot
		var tonerJSON sql.NullString

		err := rows.Scan(
			&metrics.ID, &metrics.Serial, &metrics.AgentID, &metrics.Timestamp,
			&metrics.PageCount, &metrics.ColorPages, &metrics.MonoPages,
			&metrics.ScanCount, &tonerJSON,
		)
		if err != nil {
			return nil, err
		}

		if tonerJSON.Valid {
			json.Unmarshal([]byte(tonerJSON.String), &metrics.TonerLevels)
		}

		history = append(history, &metrics)
	}

	return history, rows.Err()
}

// SaveAuditEntry saves an audit log entry to the database
func (s *SQLiteStore) SaveAuditEntry(ctx context.Context, entry *AuditEntry) error {
	query := `
		INSERT INTO audit_log (timestamp, agent_id, action, details, ip_address)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		entry.Timestamp, entry.AgentID, entry.Action, entry.Details, entry.IPAddress)
	return err
}

// GetAuditLog retrieves audit log entries for an agent since a given time
func (s *SQLiteStore) GetAuditLog(ctx context.Context, agentID string, since time.Time) ([]*AuditEntry, error) {
	var query string
	var args []interface{}

	if agentID != "" {
		query = `
			SELECT id, timestamp, agent_id, action, details, ip_address
			FROM audit_log
			WHERE agent_id = ? AND timestamp >= ?
			ORDER BY timestamp DESC
		`
		args = []interface{}{agentID, since}
	} else {
		// Get all audit entries if no agent_id specified
		query = `
			SELECT id, timestamp, agent_id, action, details, ip_address
			FROM audit_log
			WHERE timestamp >= ?
			ORDER BY timestamp DESC
		`
		args = []interface{}{since}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var details, ipAddress sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.Timestamp, &entry.AgentID,
			&entry.Action, &details, &ipAddress,
		)
		if err != nil {
			return nil, err
		}

		if details.Valid {
			entry.Details = details.String
		}
		if ipAddress.Valid {
			entry.IPAddress = ipAddress.String
		}

		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// Close closes the database connection
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// GetDefaultDBPath returns platform-specific default database path
func GetDefaultDBPath() string {
	switch runtime.GOOS {
	case "windows":
		return `C:\ProgramData\PrintMaster\server\server.db`
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library/Application Support/PrintMaster/server/server.db")
	default: // linux, etc.
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local/share/printmaster/server/server.db")
	}
}
