package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"printmaster/common/logger"
	pmsettings "printmaster/common/settings"

	"golang.org/x/crypto/argon2"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO required)
)

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

const schemaVersion = 8

// Optional package-level logger that can be set by the application (server)
var Log *logger.Logger

// SetLogger injects the structured logger from the main application.
func SetLogger(l *logger.Logger) {
	Log = l
}

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

	logInfo("Opened SQLite database", "path", dbPath)

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
		name TEXT NOT NULL DEFAULT '',
		hostname TEXT NOT NULL,
		ip TEXT NOT NULL,
		platform TEXT NOT NULL,
		version TEXT NOT NULL,
		protocol_version TEXT NOT NULL,
		token TEXT NOT NULL,
		registered_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'active',
		os_version TEXT,
		go_version TEXT,
		architecture TEXT,
		num_cpu INTEGER DEFAULT 0,
		total_memory_mb INTEGER DEFAULT 0,
		build_type TEXT,
		git_commit TEXT,
		last_heartbeat DATETIME,
		device_count INTEGER DEFAULT 0,
		last_device_sync DATETIME,
		last_metrics_sync DATETIME
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

	-- Audit log for agent and admin operations
	CREATE TABLE IF NOT EXISTS audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		actor_type TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		actor_name TEXT,
		action TEXT NOT NULL,
		target_type TEXT,
		target_id TEXT,
		tenant_id TEXT,
		severity TEXT NOT NULL DEFAULT 'info',
		details TEXT,
		metadata TEXT,
		ip_address TEXT,
		user_agent TEXT,
		request_id TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor_type, actor_id);
	CREATE INDEX IF NOT EXISTS idx_audit_tenant ON audit_log(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);

	-- Tenants table for multi-tenant support
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		contact_name TEXT,
		contact_email TEXT,
		contact_phone TEXT,
		business_unit TEXT,
		billing_code TEXT,
		address TEXT,
		login_domain TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Join tokens for agent onboarding (store only token hash)
	CREATE TABLE IF NOT EXISTS join_tokens (
		id TEXT PRIMARY KEY,
		token_hash TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		one_time INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		used_at DATETIME,
		revoked INTEGER DEFAULT 0,
		FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_join_tokens_hash ON join_tokens(token_hash);
	CREATE INDEX IF NOT EXISTS idx_join_tokens_tenant ON join_tokens(tenant_id);

	-- Local users for UI and API authentication
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'user',
		tenant_id TEXT,
		email TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

	CREATE TABLE IF NOT EXISTS user_tenants (
		user_id INTEGER NOT NULL,
		tenant_id TEXT NOT NULL,
		PRIMARY KEY (user_id, tenant_id),
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_user_tenants_tenant ON user_tenants(tenant_id);

	-- Sessions for local login (token stored as plain long random string)
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

	-- Password reset tokens (store hashed token)
	CREATE TABLE IF NOT EXISTS password_resets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_hash TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		expires_at DATETIME NOT NULL,
		used INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets(user_id);

	-- OIDC providers for SSO
	CREATE TABLE IF NOT EXISTS oidc_providers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		issuer TEXT NOT NULL,
		client_id TEXT NOT NULL,
		client_secret TEXT NOT NULL,
		scopes TEXT NOT NULL DEFAULT 'openid profile email',
		icon TEXT,
		button_text TEXT,
		button_style TEXT,
		auto_login INTEGER NOT NULL DEFAULT 0,
		tenant_id TEXT,
		default_role TEXT NOT NULL DEFAULT 'user',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS oidc_sessions (
		id TEXT PRIMARY KEY,
		provider_slug TEXT NOT NULL,
		tenant_id TEXT,
		nonce TEXT NOT NULL,
		state TEXT NOT NULL,
		redirect_url TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
		FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS oidc_links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider_slug TEXT NOT NULL,
		subject TEXT NOT NULL,
		email TEXT,
		user_id INTEGER NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(provider_slug, subject),
		FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	-- Settings tables (global + tenant overrides)
	CREATE TABLE IF NOT EXISTS settings_global (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		schema_version TEXT NOT NULL,
		payload TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT
	);

	CREATE TABLE IF NOT EXISTS settings_tenant (
		tenant_id TEXT PRIMARY KEY,
		schema_version TEXT NOT NULL,
		payload TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT,
		FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	-- Fleet update policy table (per-tenant auto-update configuration)
	CREATE TABLE IF NOT EXISTS fleet_update_policies (
		tenant_id TEXT PRIMARY KEY,
		update_check_days INTEGER NOT NULL DEFAULT 0,
		version_pin_strategy TEXT NOT NULL DEFAULT 'minor',
		allow_major_upgrade INTEGER NOT NULL DEFAULT 0,
		target_version TEXT,
		maintenance_window_enabled INTEGER NOT NULL DEFAULT 0,
		maintenance_window_start_hour INTEGER DEFAULT 2,
		maintenance_window_start_min INTEGER DEFAULT 0,
		maintenance_window_end_hour INTEGER DEFAULT 4,
		maintenance_window_end_min INTEGER DEFAULT 0,
		maintenance_window_timezone TEXT DEFAULT 'UTC',
		maintenance_window_days TEXT,
		rollout_staggered INTEGER NOT NULL DEFAULT 0,
		rollout_max_concurrent INTEGER DEFAULT 0,
		rollout_batch_size INTEGER DEFAULT 0,
		rollout_delay_between_waves INTEGER DEFAULT 300,
		rollout_jitter_seconds INTEGER DEFAULT 60,
		rollout_emergency_abort INTEGER NOT NULL DEFAULT 0,
		collect_telemetry INTEGER NOT NULL DEFAULT 1,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS fleet_update_policy_global (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		update_check_days INTEGER NOT NULL DEFAULT 0,
		version_pin_strategy TEXT NOT NULL DEFAULT 'minor',
		allow_major_upgrade INTEGER NOT NULL DEFAULT 0,
		target_version TEXT,
		maintenance_window_enabled INTEGER NOT NULL DEFAULT 0,
		maintenance_window_start_hour INTEGER DEFAULT 2,
		maintenance_window_start_min INTEGER DEFAULT 0,
		maintenance_window_end_hour INTEGER DEFAULT 4,
		maintenance_window_end_min INTEGER DEFAULT 0,
		maintenance_window_timezone TEXT DEFAULT 'UTC',
		maintenance_window_days TEXT,
		rollout_staggered INTEGER NOT NULL DEFAULT 0,
		rollout_max_concurrent INTEGER DEFAULT 0,
		rollout_batch_size INTEGER DEFAULT 0,
		rollout_delay_between_waves INTEGER DEFAULT 300,
		rollout_jitter_seconds INTEGER DEFAULT 60,
		rollout_emergency_abort INTEGER NOT NULL DEFAULT 0,
		collect_telemetry INTEGER NOT NULL DEFAULT 1,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Release artifact cache (phase 2 auto-update intake)
	CREATE TABLE IF NOT EXISTS release_artifacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		component TEXT NOT NULL,
		version TEXT NOT NULL,
		platform TEXT NOT NULL,
		arch TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT 'stable',
		source_url TEXT NOT NULL,
		cache_path TEXT,
		sha256 TEXT,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		release_notes TEXT,
		published_at DATETIME,
		downloaded_at DATETIME,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(component, version, platform, arch)
	);

	CREATE INDEX IF NOT EXISTS idx_release_artifacts_component ON release_artifacts(component);

	CREATE TABLE IF NOT EXISTS signing_keys (
		id TEXT PRIMARY KEY,
		algorithm TEXT NOT NULL,
		public_key TEXT NOT NULL,
		private_key TEXT NOT NULL,
		notes TEXT,
		active INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		rotated_at DATETIME
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_signing_keys_active ON signing_keys(active)
		WHERE active = 1;

	CREATE TABLE IF NOT EXISTS release_manifests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		component TEXT NOT NULL,
		version TEXT NOT NULL,
		platform TEXT NOT NULL,
		arch TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT 'stable',
		manifest_version TEXT NOT NULL,
		manifest_json TEXT NOT NULL,
		signature TEXT NOT NULL,
		signing_key_id TEXT NOT NULL,
		generated_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(component, version, platform, arch)
	);

	CREATE INDEX IF NOT EXISTS idx_release_manifests_component ON release_manifests(component);

	       CREATE TABLE IF NOT EXISTS installer_bundles (
		       id INTEGER PRIMARY KEY AUTOINCREMENT,
		       tenant_id TEXT NOT NULL,
		       component TEXT NOT NULL,
		       version TEXT NOT NULL,
		       platform TEXT NOT NULL,
		       arch TEXT NOT NULL,
		       format TEXT NOT NULL,
		       source_artifact_id INTEGER,
		       config_hash TEXT NOT NULL,
		       bundle_path TEXT NOT NULL,
		       size_bytes INTEGER NOT NULL DEFAULT 0,
	       	encrypted INTEGER NOT NULL DEFAULT 0,
	       	encryption_key_id TEXT,
		       metadata_json TEXT,
		       expires_at DATETIME,
		       created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		       updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		       FOREIGN KEY(source_artifact_id) REFERENCES release_artifacts(id) ON DELETE SET NULL,
		       UNIQUE(tenant_id, component, version, platform, arch, format, config_hash)
	       );

	       CREATE INDEX IF NOT EXISTS idx_installer_bundles_tenant ON installer_bundles(tenant_id);
	       CREATE INDEX IF NOT EXISTS idx_installer_bundles_expires ON installer_bundles(expires_at);

	CREATE TABLE IF NOT EXISTS self_update_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL,
		requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		completed_at DATETIME,
		current_version TEXT,
		target_version TEXT,
		channel TEXT NOT NULL DEFAULT 'stable',
		platform TEXT,
		arch TEXT,
		release_artifact_id INTEGER,
		error_code TEXT,
		error_message TEXT,
		metadata_json TEXT,
		requested_by TEXT,
		FOREIGN KEY(release_artifact_id) REFERENCES release_artifacts(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_self_update_runs_requested_at ON self_update_runs(requested_at DESC);
	CREATE INDEX IF NOT EXISTS idx_self_update_runs_status ON self_update_runs(status);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	if err := s.ensureGlobalSettingsSeed(); err != nil {
		return err
	}

	// Add name column if it doesn't exist (migration for existing databases)
	// SQLite will error if column exists, so we ignore that specific error
	// Best-effort migrations for agents table: attempt to add newer columns that
	// may be missing from older database files. We intentionally ignore errors
	// (column already exists) so this is safe to run on existing DBs.
	altStmts := []string{
		// tenancy tenant_id support
		"ALTER TABLE agents ADD COLUMN tenant_id TEXT",
		"ALTER TABLE devices ADD COLUMN tenant_id TEXT",
		"ALTER TABLE metrics_history ADD COLUMN tenant_id TEXT",
		"ALTER TABLE oidc_providers ADD COLUMN tenant_id TEXT",
		"ALTER TABLE tenants ADD COLUMN contact_name TEXT",
		"ALTER TABLE tenants ADD COLUMN contact_email TEXT",
		"ALTER TABLE tenants ADD COLUMN contact_phone TEXT",
		"ALTER TABLE tenants ADD COLUMN business_unit TEXT",
		"ALTER TABLE tenants ADD COLUMN billing_code TEXT",
		"ALTER TABLE tenants ADD COLUMN address TEXT",
		"ALTER TABLE tenants ADD COLUMN login_domain TEXT",
		"ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user'",
		"ALTER TABLE agents ADD COLUMN name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE agents ADD COLUMN os_version TEXT",
		"ALTER TABLE agents ADD COLUMN go_version TEXT",
		"ALTER TABLE agents ADD COLUMN architecture TEXT",
		"ALTER TABLE agents ADD COLUMN num_cpu INTEGER DEFAULT 0",
		"ALTER TABLE agents ADD COLUMN total_memory_mb INTEGER DEFAULT 0",
		"ALTER TABLE agents ADD COLUMN build_type TEXT",
		"ALTER TABLE agents ADD COLUMN git_commit TEXT",
		"ALTER TABLE agents ADD COLUMN last_heartbeat DATETIME",
		"ALTER TABLE agents ADD COLUMN device_count INTEGER DEFAULT 0",
		"ALTER TABLE agents ADD COLUMN last_device_sync DATETIME",
		"ALTER TABLE agents ADD COLUMN last_metrics_sync DATETIME",
		// users: add email column
		"ALTER TABLE users ADD COLUMN email TEXT",
		// installer bundle encryption columns
		"ALTER TABLE installer_bundles ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE installer_bundles ADD COLUMN encryption_key_id TEXT",
	}

	for _, stmt := range altStmts {
		if _, err := s.db.Exec(stmt); err != nil {
			if isSQLiteDuplicateColumnErr(err) {
				logDebug("SQLite migration statement skipped (column already exists)", "stmt", stmt)
				continue
			}
			// Only log unexpected errors as warnings so we do not spam duplicate column output during normal startup
			logWarn("SQLite migration statement (ignored error)", "stmt", stmt, "error", err)
		} else {
			logDebug("SQLite migration statement applied (or already present)", "stmt", stmt)
		}
	}

	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_login_domain ON tenants(login_domain) WHERE login_domain IS NOT NULL AND login_domain != ''`); err != nil {
		logWarn("SQLite migration statement (ignored error)", "stmt", "CREATE UNIQUE INDEX idx_tenants_login_domain", "error", err)
	} else {
		logDebug("SQLite migration statement applied (or already present)", "stmt", "CREATE UNIQUE INDEX idx_tenants_login_domain")
	}

	s.backfillUserTenantMappings()
	s.migrateLegacyRoles()

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

	// Security migration: detect and clear legacy sessions stored as raw tokens
	// New behavior stores SHA-256 hex tokens (64 chars). If any existing rows
	// contain tokens of a different length, clear the sessions table so users
	// must re-authenticate. This avoids keeping raw tokens in the DB.
	var nonHashedCount int
	err = s.db.QueryRow("SELECT COALESCE(COUNT(1),0) FROM sessions WHERE LENGTH(token) != 64").Scan(&nonHashedCount)
	if err == nil && nonHashedCount > 0 {
		logInfo("Clearing legacy sessions (security migration)", "non_hashed_count", nonHashedCount)
		if _, derr := s.db.Exec("DELETE FROM sessions"); derr != nil {
			logWarn("Failed to clear legacy sessions", "error", derr)
		}
	}

	logInfo("Schema initialized for DB", "path", s.dbPath, "schemaVersion", schemaVersion)

	return nil
}

// isSQLiteDuplicateColumnErr returns true when the provided error indicates an
// ALTER TABLE attempted to add a column that already exists. These are expected
// during startup when running best-effort migrations on fresh databases, so we
// suppress warning logs for them.
func isSQLiteDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func (s *SQLiteStore) ensureGlobalSettingsSeed() error {
	defaults := pmsettings.DefaultSettings()
	pmsettings.Sanitize(&defaults)
	payload, err := json.Marshal(defaults)
	if err != nil {
		return fmt.Errorf("failed to marshal default settings: %w", err)
	}
	stmt := `
		INSERT INTO settings_global (id, schema_version, payload, updated_at, updated_by)
		SELECT 1, ?, ?, CURRENT_TIMESTAMP, 'system'
		WHERE NOT EXISTS (SELECT 1 FROM settings_global WHERE id = 1);
	`
	if _, err := s.db.Exec(stmt, pmsettings.SchemaVersion, string(payload)); err != nil {
		return fmt.Errorf("failed to seed global settings: %w", err)
	}
	return nil
}

// RegisterAgent registers a new agent or updates existing
func (s *SQLiteStore) RegisterAgent(ctx context.Context, agent *Agent) error {
	query := `
		INSERT INTO agents (
			agent_id, name, hostname, ip, platform, version, protocol_version, token, tenant_id,
			registered_at, last_seen, status,
			os_version, go_version, architecture, num_cpu, total_memory_mb,
			build_type, git_commit, last_heartbeat
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET
			name = excluded.name,
			hostname = excluded.hostname,
			ip = excluded.ip,
			platform = excluded.platform,
			version = excluded.version,
			protocol_version = excluded.protocol_version,
			token = excluded.token,
			tenant_id = excluded.tenant_id,
			last_seen = excluded.last_seen,
			status = excluded.status,
			os_version = excluded.os_version,
			go_version = excluded.go_version,
			architecture = excluded.architecture,
			num_cpu = excluded.num_cpu,
			total_memory_mb = excluded.total_memory_mb,
			build_type = excluded.build_type,
			git_commit = excluded.git_commit,
			last_heartbeat = excluded.last_heartbeat
	`

	_, err := s.db.ExecContext(ctx, query,
		agent.AgentID, agent.Name, agent.Hostname, agent.IP, agent.Platform,
		agent.Version, agent.ProtocolVersion, agent.Token, agent.TenantID, agent.RegisteredAt,
		agent.LastSeen, agent.Status,
		agent.OSVersion, agent.GoVersion, agent.Architecture, agent.NumCPU,
		agent.TotalMemoryMB, agent.BuildType, agent.GitCommit, agent.LastHeartbeat)

	return err
}

// GetAgent retrieves an agent by ID
func (s *SQLiteStore) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	query := `
		SELECT id, agent_id, name, hostname, ip, platform, version, protocol_version,
		       token, tenant_id, registered_at, last_seen, status,
		       os_version, go_version, architecture, num_cpu, total_memory_mb,
		       build_type, git_commit, last_heartbeat, device_count,
		       last_device_sync, last_metrics_sync
		FROM agents
		WHERE agent_id = ?
	`

	var agent Agent
	var name, osVersion, goVersion, architecture, buildType, gitCommit sql.NullString
	var numCPU, deviceCount sql.NullInt64
	var totalMemoryMB sql.NullInt64
	var lastHeartbeat, lastDeviceSync, lastMetricsSync sql.NullTime
	var tenantID sql.NullString

	err := s.db.QueryRowContext(ctx, query, agentID).Scan(
		&agent.ID, &agent.AgentID, &name, &agent.Hostname, &agent.IP,
		&agent.Platform, &agent.Version, &agent.ProtocolVersion,
		&agent.Token, &tenantID, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
		&osVersion, &goVersion, &architecture, &numCPU, &totalMemoryMB,
		&buildType, &gitCommit, &lastHeartbeat, &deviceCount,
		&lastDeviceSync, &lastMetricsSync,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	if err != nil {
		return nil, err
	}

	// Handle nullable fields
	if name.Valid {
		agent.Name = name.String
	}
	if osVersion.Valid {
		agent.OSVersion = osVersion.String
	}
	if goVersion.Valid {
		agent.GoVersion = goVersion.String
	}
	if architecture.Valid {
		agent.Architecture = architecture.String
	}
	if buildType.Valid {
		agent.BuildType = buildType.String
	}
	if gitCommit.Valid {
		agent.GitCommit = gitCommit.String
	}
	if numCPU.Valid {
		agent.NumCPU = int(numCPU.Int64)
	}
	if totalMemoryMB.Valid {
		agent.TotalMemoryMB = totalMemoryMB.Int64
	}
	if deviceCount.Valid {
		agent.DeviceCount = int(deviceCount.Int64)
	}
	if lastHeartbeat.Valid {
		agent.LastHeartbeat = lastHeartbeat.Time
	}
	if lastDeviceSync.Valid {
		agent.LastDeviceSync = lastDeviceSync.Time
	}
	if lastMetricsSync.Valid {
		agent.LastMetricsSync = lastMetricsSync.Time
	}
	if tenantID.Valid {
		agent.TenantID = tenantID.String
	}

	return &agent, nil
}

// ListAgents returns all registered agents
func (s *SQLiteStore) ListAgents(ctx context.Context) ([]*Agent, error) {
	query := `
		SELECT id, agent_id, name, hostname, ip, platform, version, protocol_version,
		       token, tenant_id, registered_at, last_seen, status,
		       os_version, go_version, architecture, num_cpu, total_memory_mb,
		       build_type, git_commit, last_heartbeat, device_count,
		       last_device_sync, last_metrics_sync
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
		var name, osVersion, goVersion, architecture, buildType, gitCommit sql.NullString
		var numCPU, deviceCount sql.NullInt64
		var totalMemoryMB sql.NullInt64
		var lastHeartbeat, lastDeviceSync, lastMetricsSync sql.NullTime
		var tenantID sql.NullString

		err := rows.Scan(
			&agent.ID, &agent.AgentID, &name, &agent.Hostname, &agent.IP,
			&agent.Platform, &agent.Version, &agent.ProtocolVersion,
			&agent.Token, &tenantID, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
			&osVersion, &goVersion, &architecture, &numCPU, &totalMemoryMB,
			&buildType, &gitCommit, &lastHeartbeat, &deviceCount,
			&lastDeviceSync, &lastMetricsSync,
		)
		if err != nil {
			return nil, err
		}

		// Handle nullable fields
		if name.Valid {
			agent.Name = name.String
		}
		if osVersion.Valid {
			agent.OSVersion = osVersion.String
		}
		if goVersion.Valid {
			agent.GoVersion = goVersion.String
		}
		if architecture.Valid {
			agent.Architecture = architecture.String
		}
		if buildType.Valid {
			agent.BuildType = buildType.String
		}
		if gitCommit.Valid {
			agent.GitCommit = gitCommit.String
		}
		if numCPU.Valid {
			agent.NumCPU = int(numCPU.Int64)
		}
		if totalMemoryMB.Valid {
			agent.TotalMemoryMB = totalMemoryMB.Int64
		}
		if deviceCount.Valid {
			agent.DeviceCount = int(deviceCount.Int64)
		}
		if lastHeartbeat.Valid {
			agent.LastHeartbeat = lastHeartbeat.Time
		}
		if lastDeviceSync.Valid {
			agent.LastDeviceSync = lastDeviceSync.Time
		}
		if lastMetricsSync.Valid {
			agent.LastMetricsSync = lastMetricsSync.Time
		}
		if tenantID.Valid {
			agent.TenantID = tenantID.String
		}

		agents = append(agents, &agent)
	}

	return agents, rows.Err()
}

// Tenancy methods
func (s *SQLiteStore) CreateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant == nil {
		return fmt.Errorf("tenant required")
	}
	if tenant.ID == "" {
		// generate simple id
		b := make([]byte, 6)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		tenant.ID = hex.EncodeToString(b)
	}
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = time.Now().UTC()
	}

	tenant.LoginDomain = NormalizeTenantDomain(tenant.LoginDomain)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tenants (
		id, name, description, contact_name, contact_email, contact_phone,
		business_unit, billing_code, address, login_domain, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.Description, tenant.ContactName, tenant.ContactEmail,
		tenant.ContactPhone, tenant.BusinessUnit, tenant.BillingCode, tenant.Address, nullString(tenant.LoginDomain), tenant.CreatedAt)
	return err
}

func (s *SQLiteStore) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant == nil {
		return fmt.Errorf("tenant required")
	}
	if tenant.ID == "" {
		return fmt.Errorf("tenant id required")
	}
	tenant.LoginDomain = NormalizeTenantDomain(tenant.LoginDomain)

	res, err := s.db.ExecContext(ctx, `UPDATE tenants SET
		name = ?,
		description = ?,
		contact_name = ?,
		contact_email = ?,
		contact_phone = ?,
		business_unit = ?,
		billing_code = ?,
		address = ?,
		login_domain = ?
	WHERE id = ?`,
		tenant.Name, tenant.Description, tenant.ContactName, tenant.ContactEmail, tenant.ContactPhone,
		tenant.BusinessUnit, tenant.BillingCode, tenant.Address, nullString(tenant.LoginDomain), tenant.ID)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil {
		if rows == 0 {
			return sql.ErrNoRows
		}
	} else {
		return err
	}
	return nil
}

func (s *SQLiteStore) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, description, contact_name, contact_email, contact_phone, business_unit, billing_code, address, login_domain, created_at FROM tenants WHERE id = ?`, id)
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail, &t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address, &t.LoginDomain, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *SQLiteStore) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, contact_name, contact_email, contact_phone, business_unit, billing_code, address, login_domain, created_at FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail, &t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address, &t.LoginDomain, &t.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, &t)
	}
	return res, nil
}

// FindTenantByDomain attempts to resolve a tenant by its normalized login domain.
func (s *SQLiteStore) FindTenantByDomain(ctx context.Context, domain string) (*Tenant, error) {
	norm := NormalizeTenantDomain(domain)
	if norm == "" {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, name, description, contact_name, contact_email, contact_phone, business_unit, billing_code, address, login_domain, created_at FROM tenants WHERE login_domain = ?`, norm)
	var t Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail, &t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address, &t.LoginDomain, &t.CreatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateJoinToken generates an opaque token for tenant onboarding and stores only its hash.
func (s *SQLiteStore) CreateJoinToken(ctx context.Context, tenantID string, ttlMinutes int, oneTime bool) (*JoinToken, string, error) {
	// verify tenant exists
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM tenants WHERE id = ?`, tenantID).Scan(&exists); err != nil {
		return nil, "", err
	}

	// generate secret and id. The returned raw token will be <id>.<secret>
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, "", err
	}
	secretStr := hex.EncodeToString(secret)

	idb := make([]byte, 8)
	if _, err := rand.Read(idb); err != nil {
		return nil, "", err
	}
	id := hex.EncodeToString(idb)

	expires := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)

	// hash the secret using Argon2id
	encoded, err := generateArgonHash(secretStr)
	if err != nil {
		return nil, "", err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO join_tokens (id, token_hash, tenant_id, expires_at, one_time, created_at, revoked) VALUES (?, ?, ?, ?, ?, ?, 0)`,
		id, encoded, tenantID, expires, boolToInt(oneTime), time.Now().UTC())
	if err != nil {
		return nil, "", err
	}

	jt := &JoinToken{ID: id, TokenHash: encoded, TenantID: tenantID, ExpiresAt: expires, OneTime: oneTime, CreatedAt: time.Now().UTC(), Revoked: false}
	// return raw token as id.secret
	raw := id + "." + secretStr
	return jt, raw, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func encodeMetadata(meta map[string]any) (sql.NullString, error) {
	if len(meta) == 0 {
		return sql.NullString{Valid: false}, nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}

func decodeMetadata(raw sql.NullString) map[string]any {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw.String), &meta); err != nil {
		logWarn("Failed to decode self-update metadata", "error", err)
		return nil
	}
	return meta
}

func (s *SQLiteStore) backfillUserTenantMappings() {
	stmt := `INSERT OR IGNORE INTO user_tenants (user_id, tenant_id)
		SELECT id, tenant_id FROM users
		WHERE tenant_id IS NOT NULL AND tenant_id != ''`
	if _, err := s.db.Exec(stmt); err != nil {
		logError("Failed to backfill user_tenants", "error", err)
	}
}

func (s *SQLiteStore) migrateLegacyRoles() {
	stmts := []string{
		"UPDATE users SET role = 'operator' WHERE role = 'user'",
		"UPDATE users SET role = 'viewer' WHERE role IS NULL OR TRIM(role) = ''",
		"UPDATE oidc_providers SET default_role = 'viewer' WHERE default_role NOT IN ('admin','operator','viewer')",
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			logError("Legacy role migration failed", "stmt", stmt, "error", err)
		}
	}
}

func intToBool(i int) bool { return i != 0 }

// ValidateJoinToken checks token validity and consumes it if one-time.
func (s *SQLiteStore) ValidateJoinToken(ctx context.Context, token string) (*JoinToken, error) {
	// Expect token format: id.secret
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}
	id := parts[0]
	secret := parts[1]

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	row := tx.QueryRowContext(ctx, `SELECT id, tenant_id, expires_at, one_time, created_at, used_at, revoked, token_hash FROM join_tokens WHERE id = ?`, id)
	var jt JoinToken
	var oneInt int
	var usedAt sql.NullTime
	var revokedInt int
	var storedHash string
	if err := row.Scan(&jt.ID, &jt.TenantID, &jt.ExpiresAt, &oneInt, &jt.CreatedAt, &usedAt, &revokedInt, &storedHash); err != nil {
		tx.Rollback()
		return nil, err
	}
	jt.OneTime = oneInt != 0
	jt.Revoked = revokedInt != 0
	if usedAt.Valid {
		jt.UsedAt = usedAt.Time
	}

	if jt.Revoked {
		tx.Rollback()
		return nil, fmt.Errorf("token revoked")
	}
	if time.Now().UTC().After(jt.ExpiresAt) {
		tx.Rollback()
		return nil, fmt.Errorf("token expired")
	}

	// verify secret
	ok, verr := verifyArgonHash(secret, storedHash)
	if verr != nil {
		tx.Rollback()
		return nil, verr
	}
	if !ok {
		tx.Rollback()
		return nil, fmt.Errorf("invalid token")
	}

	if jt.OneTime {
		// consume
		if _, err := tx.ExecContext(ctx, `UPDATE join_tokens SET used_at = ?, revoked = 1 WHERE id = ?`, time.Now().UTC(), jt.ID); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &jt, nil
}

// Argon2id helpers
func generateArgonHash(secret string) (string, error) {
	// Parameters (tune as needed)
	time := uint32(1)
	memory := uint32(64 * 1024)
	threads := uint8(2)
	keyLen := uint32(32)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(secret), salt, time, memory, threads, keyLen)

	// Encode as: $argon2id$v=19$m=...,t=...,p=...$<salt_b64>$<hash_b64>
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", memory, time, threads, b64Salt, b64Hash)
	return encoded, nil
}

func verifyArgonHash(secret, encoded string) (bool, error) {
	// encoded format: $argon2id$v=19$m=<mem>,t=<time>,p=<threads>$<salt>$<hash>
	parts := strings.Split(encoded, "$")
	if len(parts) < 6 {
		return false, fmt.Errorf("bad encoded hash")
	}
	// parts[3] = params, parts[4]=salt, parts[5]=hash
	params := parts[3]
	saltB64 := parts[4]
	hashB64 := parts[5]

	// parse params (m=...,t=...,p=...)
	var memory uint32
	var time uint32
	var threads uint8
	_, err := fmt.Sscanf(params, "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		// fallback: try comma separated parsing
		vals := strings.Split(params, ",")
		for _, v := range vals {
			kv := strings.SplitN(v, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "m":
				var m uint32
				fmt.Sscanf(kv[1], "%d", &m)
				memory = m
			case "t":
				var tt uint32
				fmt.Sscanf(kv[1], "%d", &tt)
				time = tt
			case "p":
				var p uint8
				fmt.Sscanf(kv[1], "%d", &p)
				threads = p
			}
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return false, err
	}
	expected, err := base64.RawStdEncoding.DecodeString(hashB64)
	if err != nil {
		return false, err
	}

	keyLen := uint32(len(expected))
	derived := argon2.IDKey([]byte(secret), salt, time, memory, threads, keyLen)

	if subtleConstantTimeCompare(derived, expected) {
		return true, nil
	}
	return false, nil
}

// OIDC provider CRUD
func (s *SQLiteStore) CreateOIDCProvider(ctx context.Context, provider *OIDCProvider) error {
	if provider == nil {
		return fmt.Errorf("provider required")
	}
	if provider.CreatedAt.IsZero() {
		provider.CreatedAt = time.Now().UTC()
	}
	if provider.UpdatedAt.IsZero() {
		provider.UpdatedAt = provider.CreatedAt
	}
	if provider.Slug == "" {
		return fmt.Errorf("slug required")
	}

	scopes := strings.Join(provider.Scopes, " ")
	provider.DefaultRole = NormalizeRole(string(provider.DefaultRole))
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_providers (
		slug, display_name, issuer, client_id, client_secret, scopes, icon,
		button_text, button_style, auto_login, tenant_id, default_role,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider.Slug, provider.DisplayName, provider.Issuer, provider.ClientID,
		provider.ClientSecret, scopes, provider.Icon, provider.ButtonText,
		provider.ButtonStyle, boolToInt(provider.AutoLogin), provider.TenantID,
		string(provider.DefaultRole), provider.CreatedAt, provider.UpdatedAt,
	)
	return err
}

func (s *SQLiteStore) UpdateOIDCProvider(ctx context.Context, provider *OIDCProvider) error {
	if provider == nil {
		return fmt.Errorf("provider required")
	}
	if provider.Slug == "" {
		return fmt.Errorf("slug required")
	}
	provider.UpdatedAt = time.Now().UTC()
	scopes := strings.Join(provider.Scopes, " ")
	provider.DefaultRole = NormalizeRole(string(provider.DefaultRole))
	_, err := s.db.ExecContext(ctx, `UPDATE oidc_providers SET display_name=?, issuer=?, client_id=?, client_secret=?, scopes=?, icon=?, button_text=?, button_style=?, auto_login=?, tenant_id=?, default_role=?, updated_at=? WHERE slug=?`,
		provider.DisplayName, provider.Issuer, provider.ClientID, provider.ClientSecret,
		scopes, provider.Icon, provider.ButtonText, provider.ButtonStyle,
		boolToInt(provider.AutoLogin), provider.TenantID, string(provider.DefaultRole),
		provider.UpdatedAt, provider.Slug,
	)
	return err
}

func (s *SQLiteStore) DeleteOIDCProvider(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oidc_providers WHERE slug=?`, slug)
	return err
}

func (s *SQLiteStore) GetOIDCProvider(ctx context.Context, slug string) (*OIDCProvider, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, display_name, issuer, client_id, client_secret, scopes, icon, button_text, button_style, auto_login, tenant_id, default_role, created_at, updated_at FROM oidc_providers WHERE slug=?`, slug)
	var p OIDCProvider
	var scopes string
	var autoLogin int
	var defaultRole string
	if err := row.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Issuer, &p.ClientID, &p.ClientSecret, &scopes, &p.Icon, &p.ButtonText, &p.ButtonStyle, &autoLogin, &p.TenantID, &defaultRole, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.AutoLogin = intToBool(autoLogin)
	if scopes == "" {
		scopes = "openid profile email"
	}
	p.Scopes = strings.Fields(scopes)
	p.DefaultRole = NormalizeRole(defaultRole)
	return &p, nil
}

func (s *SQLiteStore) ListOIDCProviders(ctx context.Context, tenantID string) ([]*OIDCProvider, error) {
	query := `SELECT id, slug, display_name, issuer, client_id, client_secret, scopes, icon, button_text, button_style, auto_login, tenant_id, default_role, created_at, updated_at FROM oidc_providers`
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE tenant_id IS NULL OR tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " ORDER BY tenant_id IS NULL DESC, display_name"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var providers []*OIDCProvider
	for rows.Next() {
		var p OIDCProvider
		var scopes string
		var autoLogin int
		var defaultRole string
		if err := rows.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Issuer, &p.ClientID, &p.ClientSecret, &scopes, &p.Icon, &p.ButtonText, &p.ButtonStyle, &autoLogin, &p.TenantID, &defaultRole, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.AutoLogin = intToBool(autoLogin)
		p.Scopes = strings.Fields(scopes)
		p.DefaultRole = NormalizeRole(defaultRole)
		providers = append(providers, &p)
	}
	return providers, rows.Err()
}

func (s *SQLiteStore) CreateOIDCSession(ctx context.Context, sess *OIDCSession) error {
	if sess == nil {
		return fmt.Errorf("session required")
	}
	if sess.ID == "" {
		return fmt.Errorf("session id required")
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_sessions (id, provider_slug, tenant_id, nonce, state, redirect_url, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.ProviderSlug, sess.TenantID, sess.Nonce, sess.State, sess.RedirectURL, sess.CreatedAt)
	return err
}

func (s *SQLiteStore) GetOIDCSession(ctx context.Context, id string) (*OIDCSession, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, provider_slug, tenant_id, nonce, state, redirect_url, created_at FROM oidc_sessions WHERE id = ?`, id)
	var sess OIDCSession
	if err := row.Scan(&sess.ID, &sess.ProviderSlug, &sess.TenantID, &sess.Nonce, &sess.State, &sess.RedirectURL, &sess.CreatedAt); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *SQLiteStore) DeleteOIDCSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oidc_sessions WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) CreateOIDCLink(ctx context.Context, link *OIDCLink) error {
	if link == nil {
		return fmt.Errorf("link required")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_links (provider_slug, subject, email, user_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		link.ProviderSlug, link.Subject, link.Email, link.UserID, time.Now().UTC())
	return err
}

func (s *SQLiteStore) GetOIDCLink(ctx context.Context, providerSlug, subject string) (*OIDCLink, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, provider_slug, subject, email, user_id, created_at FROM oidc_links WHERE provider_slug = ? AND subject = ?`, providerSlug, subject)
	var link OIDCLink
	if err := row.Scan(&link.ID, &link.ProviderSlug, &link.Subject, &link.Email, &link.UserID, &link.CreatedAt); err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *SQLiteStore) ListOIDCLinksForUser(ctx context.Context, userID int64) ([]*OIDCLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, provider_slug, subject, email, user_id, created_at FROM oidc_links WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []*OIDCLink
	for rows.Next() {
		var link OIDCLink
		if err := rows.Scan(&link.ID, &link.ProviderSlug, &link.Subject, &link.Email, &link.UserID, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, &link)
	}
	return links, rows.Err()
}

func (s *SQLiteStore) DeleteOIDCLink(ctx context.Context, providerSlug, subject string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM oidc_links WHERE provider_slug = ? AND subject = ?`, providerSlug, subject)
	return err
}

func subtleConstantTimeCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte = 0
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// tokenHash computes a stable SHA-256 hex digest of a session token.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ListJoinTokens lists tokens for a tenant (admin view)
func (s *SQLiteStore) ListJoinTokens(ctx context.Context, tenantID string) ([]*JoinToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, token_hash, tenant_id, expires_at, one_time, created_at, used_at, revoked FROM join_tokens WHERE tenant_id = ? ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*JoinToken
	for rows.Next() {
		var jt JoinToken
		var oneInt int
		var usedAt sql.NullTime
		var revokedInt int
		if err := rows.Scan(&jt.ID, &jt.TokenHash, &jt.TenantID, &jt.ExpiresAt, &oneInt, &jt.CreatedAt, &usedAt, &revokedInt); err != nil {
			return nil, err
		}
		jt.OneTime = oneInt != 0
		jt.Revoked = revokedInt != 0
		if usedAt.Valid {
			jt.UsedAt = usedAt.Time
		}
		res = append(res, &jt)
	}
	return res, nil
}

// RevokeJoinToken marks a join token revoked (admin action)
func (s *SQLiteStore) RevokeJoinToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE join_tokens SET revoked = 1 WHERE id = ?`, id)
	return err
}

func normalizeUserTenantIDs(u *User) []string {
	ids := u.TenantIDs
	if len(ids) == 0 && strings.TrimSpace(u.TenantID) != "" {
		ids = []string{u.TenantID}
	}
	return SortTenantIDs(ids)
}

func primaryTenantID(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (s *SQLiteStore) replaceUserTenantIDsTx(ctx context.Context, tx *sql.Tx, userID int64, tenantIDs []string) error {
	if tx == nil {
		return fmt.Errorf("transaction required for tenant assignment")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_tenants WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, tenantID := range tenantIDs {
		if tenantID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES (?, ?)`, userID, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) listTenantIDsForUser(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id FROM user_tenants WHERE user_id = ? ORDER BY tenant_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, err
		}
		ids = append(ids, tid)
	}
	return ids, nil
}

func (s *SQLiteStore) loadUserTenantMap(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, tenant_id FROM user_tenants ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := make(map[int64][]string)
	for rows.Next() {
		var userID int64
		var tenantID string
		if err := rows.Scan(&userID, &tenantID); err != nil {
			return nil, err
		}
		if tenantID == "" {
			continue
		}
		res[userID] = append(res[userID], tenantID)
	}
	for id, ids := range res {
		res[id] = SortTenantIDs(ids)
	}
	return res, nil
}

func applyTenantAssignments(u *User, legacy sql.NullString, ids []string) {
	ids = SortTenantIDs(ids)
	if len(ids) == 0 && legacy.Valid && legacy.String != "" {
		ids = []string{legacy.String}
	}
	u.TenantIDs = ids
	if len(ids) > 0 {
		u.TenantID = ids[0]
	} else if legacy.Valid {
		u.TenantID = legacy.String
	}
}

// CreateUser creates a new local user with a hashed password
func (s *SQLiteStore) CreateUser(ctx context.Context, user *User, rawPassword string) error {
	if user == nil {
		return fmt.Errorf("user required")
	}
	if user.Username == "" {
		return fmt.Errorf("username required")
	}
	if rawPassword == "" {
		return fmt.Errorf("password required")
	}

	// Hash password using existing argon2 helper
	encoded, err := generateArgonHash(rawPassword)
	if err != nil {
		return err
	}

	role := NormalizeRole(string(user.Role))
	tenantIDs := normalizeUserTenantIDs(user)
	primaryTenant := primaryTenantID(tenantIDs)
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, tenant_id, email, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		user.Username, encoded, string(role), primaryTenant, user.Email, now)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	user.ID = id
	if err := s.replaceUserTenantIDsTx(ctx, tx, id, tenantIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	user.Role = role
	user.PasswordHash = encoded
	user.CreatedAt = now
	user.TenantIDs = tenantIDs
	user.TenantID = primaryTenant
	return nil
}

// GetUserByUsername returns a user by username
func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE username = ?`, username)
	var u User
	var role string
	var tenant sql.NullString
	var email sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &tenant, &email, &created); err != nil {
		return nil, err
	}
	ids, err := s.listTenantIDsForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	applyTenantAssignments(&u, tenant, ids)
	u.Role = NormalizeRole(role)
	if email.Valid {
		u.Email = email.String
	}
	if created.Valid {
		u.CreatedAt = created.Time
	}
	return &u, nil
}

// GetUserByID returns a user by numeric ID
func (s *SQLiteStore) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE id = ?`, id)
	var u User
	var role string
	var tenant sql.NullString
	var email sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &tenant, &email, &created); err != nil {
		return nil, err
	}
	ids, err := s.listTenantIDsForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	applyTenantAssignments(&u, tenant, ids)
	u.Role = NormalizeRole(role)
	if email.Valid {
		u.Email = email.String
	}
	if created.Valid {
		u.CreatedAt = created.Time
	}
	return &u, nil
}

// ListUsers returns all users (admin use)
func (s *SQLiteStore) ListUsers(ctx context.Context) ([]*User, error) {
	tenantMap, err := s.loadUserTenantMap(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []*User
	for rows.Next() {
		var u User
		var role string
		var tenant sql.NullString
		var email sql.NullString
		var created sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &tenant, &email, &created); err != nil {
			return nil, err
		}
		ids := tenantMap[u.ID]
		applyTenantAssignments(&u, tenant, ids)
		u.Role = NormalizeRole(role)
		if email.Valid {
			u.Email = email.String
		}
		if created.Valid {
			u.CreatedAt = created.Time
		}
		res = append(res, &u)
	}
	return res, nil
}

// DeleteUser removes a user by ID
func (s *SQLiteStore) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// UpdateUser updates a user's role, tenant and email
func (s *SQLiteStore) UpdateUser(ctx context.Context, user *User) error {
	if user == nil {
		return fmt.Errorf("user required")
	}
	role := NormalizeRole(string(user.Role))
	tenantIDs := normalizeUserTenantIDs(user)
	primaryTenant := primaryTenantID(tenantIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ?, tenant_id = ?, email = ? WHERE id = ?`, string(role), primaryTenant, user.Email, user.ID); err != nil {
		return err
	}
	if err := s.replaceUserTenantIDsTx(ctx, tx, user.ID, tenantIDs); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	user.Role = role
	user.TenantIDs = tenantIDs
	user.TenantID = primaryTenant
	return nil
}

// GetUserByEmail returns a user by email
func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE email = ?`, email)
	var u User
	var role string
	var tenant sql.NullString
	var em sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &tenant, &em, &created); err != nil {
		return nil, err
	}
	ids, err := s.listTenantIDsForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	applyTenantAssignments(&u, tenant, ids)
	u.Role = NormalizeRole(role)
	if em.Valid {
		u.Email = em.String
	}
	if created.Valid {
		u.CreatedAt = created.Time
	}
	return &u, nil
}

// CreatePasswordResetToken creates a reset token for a user and stores its hash
func (s *SQLiteStore) CreatePasswordResetToken(ctx context.Context, userID int64, ttlMinutes int) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	// Hash token
	h, err := generateArgonHash(token)
	if err != nil {
		return "", err
	}
	expires := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO password_resets (token_hash, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, h, userID, expires, time.Now().UTC()); err != nil {
		return "", err
	}
	return token, nil
}

// ValidatePasswordResetToken verifies the token and marks it used; returns userID
func (s *SQLiteStore) ValidatePasswordResetToken(ctx context.Context, token string) (int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, token_hash, user_id, expires_at, used FROM password_resets WHERE used = 0 AND expires_at > ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var hash string
		var userID int64
		var expires time.Time
		var usedInt int
		if err := rows.Scan(&id, &hash, &userID, &expires, &usedInt); err != nil {
			return 0, err
		}
		ok, verr := verifyArgonHash(token, hash)
		if verr != nil {
			return 0, verr
		}
		if ok {
			// mark used
			if _, err := s.db.ExecContext(ctx, `UPDATE password_resets SET used = 1 WHERE id = ?`, id); err != nil {
				return 0, err
			}
			return userID, nil
		}
	}
	return 0, fmt.Errorf("invalid or expired token")
}

// DeletePasswordResetToken deletes a matching reset token (if present)
func (s *SQLiteStore) DeletePasswordResetToken(ctx context.Context, token string) error {
	// scan to find matching id
	rows, err := s.db.QueryContext(ctx, `SELECT id, token_hash FROM password_resets`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return err
		}
		ok, verr := verifyArgonHash(token, hash)
		if verr != nil {
			return verr
		}
		if ok {
			_, err := s.db.ExecContext(ctx, `DELETE FROM password_resets WHERE id = ?`, id)
			return err
		}
	}
	return nil
}

// UpdateUserPassword replaces the password hash for a user
func (s *SQLiteStore) UpdateUserPassword(ctx context.Context, userID int64, rawPassword string) error {
	if rawPassword == "" {
		return fmt.Errorf("password required")
	}
	h, err := generateArgonHash(rawPassword)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, h, userID)
	return err
}

// AuthenticateUser verifies username/password and returns the user if valid
func (s *SQLiteStore) AuthenticateUser(ctx context.Context, username, rawPassword string) (*User, error) {
	u, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}
	ok, verr := verifyArgonHash(rawPassword, u.PasswordHash)
	if verr != nil {
		return nil, verr
	}
	if !ok {
		return nil, fmt.Errorf("invalid credentials")
	}
	// Remove password hash from returned struct for safety
	u.PasswordHash = ""
	return u, nil
}

// CreateSession creates a session token for a user and stores its hash
// The raw token is returned to the caller but only the hash is persisted
// so that raw session tokens are not stored in the database.
func (s *SQLiteStore) CreateSession(ctx context.Context, userID int64, ttlMinutes int) (*Session, error) {
	// generate raw token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	expires := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)

	// store only hash of token in DB
	h := tokenHash(token)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`, h, userID, expires, time.Now().UTC()); err != nil {
		return nil, err
	}

	return &Session{Token: token, UserID: userID, ExpiresAt: expires, CreatedAt: time.Now().UTC()}, nil
}

// GetSessionByToken retrieves a session by raw token (hashing it first) and ensures it's not expired
func (s *SQLiteStore) GetSessionByToken(ctx context.Context, token string) (*Session, error) {
	h := tokenHash(token)
	row := s.db.QueryRowContext(ctx, `SELECT token, user_id, expires_at, created_at FROM sessions WHERE token = ?`, h)
	var ses Session
	var created sql.NullTime
	if err := row.Scan(&ses.Token, &ses.UserID, &ses.ExpiresAt, &created); err != nil {
		return nil, err
	}
	if created.Valid {
		ses.CreatedAt = created.Time
	}
	if time.Now().UTC().After(ses.ExpiresAt) {
		// session expired - delete and return error
		s.DeleteSession(ctx, token)
		return nil, fmt.Errorf("session expired")
	}
	// Do not return the hashed token value back to callers; replace with raw input
	ses.Token = token
	return &ses, nil
}

// DeleteSession deletes a session token
func (s *SQLiteStore) DeleteSession(ctx context.Context, token string) error {
	h := tokenHash(token)
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, h)
	return err
}

// DeleteSessionByHash deletes a session by the stored token hash (used by admin revocation)
func (s *SQLiteStore) DeleteSessionByHash(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, tokenHash)
	return err
}

// ListSessions returns all sessions with optional username if available
func (s *SQLiteStore) ListSessions(ctx context.Context) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.token, s.user_id, s.expires_at, s.created_at, u.username FROM sessions s LEFT JOIN users u ON s.user_id = u.id ORDER BY s.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*Session
	for rows.Next() {
		var tokenHash string
		var userID int64
		var expires time.Time
		var created sql.NullTime
		var username sql.NullString
		if err := rows.Scan(&tokenHash, &userID, &expires, &created, &username); err != nil {
			return nil, err
		}
		ses := &Session{Token: tokenHash, UserID: userID, ExpiresAt: expires}
		if created.Valid {
			ses.CreatedAt = created.Time
		}
		if username.Valid {
			ses.Username = username.String
		}
		// Do not expose raw tokens; Token here is the stored hash (used for deletion)
		res = append(res, ses)
	}
	return res, rows.Err()
}

// GetAgentByToken retrieves an agent by bearer token
func (s *SQLiteStore) GetAgentByToken(ctx context.Context, token string) (*Agent, error) {
	query := `
		SELECT id, agent_id, name, hostname, ip, platform, version, protocol_version,
		       token, registered_at, last_seen, status
		FROM agents
		WHERE token = ?
	`

	var agent Agent
	var name sql.NullString
	err := s.db.QueryRowContext(ctx, query, token).Scan(
		&agent.ID, &agent.AgentID, &name, &agent.Hostname, &agent.IP,
		&agent.Platform, &agent.Version, &agent.ProtocolVersion,
		&agent.Token, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid token")
	}
	if err != nil {
		return nil, err
	}

	if name.Valid {
		agent.Name = name.String
	}

	return &agent, nil
}

// UpdateAgentHeartbeat updates agent's last_seen timestamp
func (s *SQLiteStore) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string) error {
	query := `UPDATE agents SET last_seen = ?, status = ? WHERE agent_id = ?`
	_, err := s.db.ExecContext(ctx, query, time.Now(), status, agentID)
	return err
}

// UpdateAgentInfo updates agent metadata (version, platform, etc.) typically on heartbeat
func (s *SQLiteStore) UpdateAgentInfo(ctx context.Context, agent *Agent) error {
	query := `UPDATE agents SET 
		version = COALESCE(NULLIF(?, ''), version),
		protocol_version = COALESCE(NULLIF(?, ''), protocol_version),
		hostname = COALESCE(NULLIF(?, ''), hostname),
		ip = COALESCE(NULLIF(?, ''), ip),
		platform = COALESCE(NULLIF(?, ''), platform),
		os_version = COALESCE(NULLIF(?, ''), os_version),
		go_version = COALESCE(NULLIF(?, ''), go_version),
		architecture = COALESCE(NULLIF(?, ''), architecture),
		build_type = COALESCE(NULLIF(?, ''), build_type),
		git_commit = COALESCE(NULLIF(?, ''), git_commit),
		last_seen = ?,
		status = ?
		WHERE agent_id = ?`
	_, err := s.db.ExecContext(ctx, query,
		agent.Version,
		agent.ProtocolVersion,
		agent.Hostname,
		agent.IP,
		agent.Platform,
		agent.OSVersion,
		agent.GoVersion,
		agent.Architecture,
		agent.BuildType,
		agent.GitCommit,
		time.Now(),
		agent.Status,
		agent.AgentID,
	)
	return err
}

// UpdateAgentName updates the stored user-friendly name for an agent
func (s *SQLiteStore) UpdateAgentName(ctx context.Context, agentID string, name string) error {
	query := `UPDATE agents SET name = ? WHERE agent_id = ?`
	_, err := s.db.ExecContext(ctx, query, name, agentID)
	return err
}

// DeleteAgent removes an agent and all associated devices and metrics
func (s *SQLiteStore) DeleteAgent(ctx context.Context, agentID string) error {
	// Start transaction to ensure atomic deletion
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete metrics for devices owned by this agent
	// NOTE: metrics are stored in the metrics_history table (table name corrected)
	if _, err := tx.ExecContext(ctx, `DELETE FROM metrics_history WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("failed to delete metrics: %w", err)
	}

	// Delete devices owned by this agent
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("failed to delete devices: %w", err)
	}

	// Delete the agent
	result, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE agent_id = ?`, agentID)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	// Check if agent existed
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("agent not found")
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
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

	// Normalize timestamp to UTC RFC3339Nano before storing to ensure
	// consistent, comparable text representation in SQLite.
	ts := metrics.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}
	tsStr := ts.Format(time.RFC3339Nano)

	query := `
		INSERT INTO metrics_history (
			serial, agent_id, timestamp, page_count, color_pages,
			mono_pages, scan_count, toner_levels
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		metrics.Serial, metrics.AgentID, tsStr,
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
		ORDER BY timestamp ASC
	`

	// Use parameter binding for the since time (normalized to UTC RFC3339Nano)
	sinceStr := since.UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(ctx, query, serial, sinceStr)
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
	if entry == nil {
		return fmt.Errorf("audit entry is nil")
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.ActorType == "" {
		entry.ActorType = AuditActorSystem
	}
	if entry.Severity == "" {
		entry.Severity = AuditSeverityInfo
	}

	var metadataJSON sql.NullString
	if len(entry.Metadata) > 0 {
		data, err := json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("failed to encode audit metadata: %w", err)
		}
		metadataJSON.Valid = true
		metadataJSON.String = string(data)
	}

	query := `
		INSERT INTO audit_log (
			timestamp, actor_type, actor_id, actor_name, action,
			target_type, target_id, tenant_id, severity, details,
			metadata, ip_address, user_agent, request_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		entry.Timestamp.UTC(), string(entry.ActorType), entry.ActorID, entry.ActorName,
		entry.Action, entry.TargetType, entry.TargetID, entry.TenantID,
		string(entry.Severity), entry.Details, metadataJSON,
		entry.IPAddress, entry.UserAgent, entry.RequestID,
	)
	return err
}

// GetAuditLog retrieves audit log entries for an agent since a given time
func (s *SQLiteStore) GetAuditLog(ctx context.Context, actorID string, since time.Time) ([]*AuditEntry, error) {
	query := `
		SELECT id, timestamp, actor_type, actor_id, actor_name, action,
		       target_type, target_id, tenant_id, severity, details,
		       metadata, ip_address, user_agent, request_id
		FROM audit_log
		WHERE timestamp >= ?
	`
	args := []interface{}{since}
	if actorID != "" {
		query += " AND actor_id = ?"
		args = append(args, actorID)
	}
	query += " ORDER BY timestamp DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		var (
			entry      AuditEntry
			actorType  string
			actorName  sql.NullString
			targetType sql.NullString
			targetID   sql.NullString
			tenantID   sql.NullString
			severity   string
			details    sql.NullString
			metadata   sql.NullString
			ipAddress  sql.NullString
			userAgent  sql.NullString
			requestID  sql.NullString
		)

		err := rows.Scan(
			&entry.ID, &entry.Timestamp, &actorType, &entry.ActorID,
			&actorName, &entry.Action, &targetType, &targetID,
			&tenantID, &severity, &details, &metadata,
			&ipAddress, &userAgent, &requestID,
		)
		if err != nil {
			return nil, err
		}
		entry.ActorType = AuditActorType(actorType)
		if actorName.Valid {
			entry.ActorName = actorName.String
		}
		if targetType.Valid {
			entry.TargetType = targetType.String
		}
		if targetID.Valid {
			entry.TargetID = targetID.String
		}
		if tenantID.Valid {
			entry.TenantID = tenantID.String
		}
		entry.Severity = AuditSeverity(severity)
		if details.Valid {
			entry.Details = details.String
		}
		if metadata.Valid {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(metadata.String), &meta); err == nil {
				entry.Metadata = meta
			}
		}
		if ipAddress.Valid {
			entry.IPAddress = ipAddress.String
		}
		if userAgent.Valid {
			entry.UserAgent = userAgent.String
		}
		if requestID.Valid {
			entry.RequestID = requestID.String
		}

		entries = append(entries, &entry)
	}

	return entries, rows.Err()
}

// Settings storage ---------------------------------------------------------

// GetGlobalSettings returns the persisted global settings snapshot.
func (s *SQLiteStore) GetGlobalSettings(ctx context.Context) (*SettingsRecord, error) {
	query := `
		SELECT schema_version, payload, updated_at, IFNULL(updated_by, '')
		FROM settings_global
		WHERE id = 1
	`
	var (
		schemaVersion string
		payload       sql.NullString
		updatedAt     sql.NullTime
		updatedBy     sql.NullString
	)
	err := s.db.QueryRowContext(ctx, query).Scan(&schemaVersion, &payload, &updatedAt, &updatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	settingsPayload := pmsettings.DefaultSettings()
	if payload.Valid && payload.String != "" {
		if err := json.Unmarshal([]byte(payload.String), &settingsPayload); err != nil {
			return nil, fmt.Errorf("failed to decode global settings: %w", err)
		}
	}
	pmsettings.Sanitize(&settingsPayload)
	rec := &SettingsRecord{
		SchemaVersion: schemaVersion,
		Settings:      settingsPayload,
		UpdatedBy:     updatedBy.String,
	}
	if updatedAt.Valid {
		rec.UpdatedAt = updatedAt.Time
	}
	return rec, nil
}

// UpsertGlobalSettings replaces the canonical global settings row.
func (s *SQLiteStore) UpsertGlobalSettings(ctx context.Context, rec *SettingsRecord) error {
	if rec == nil {
		return fmt.Errorf("settings record required")
	}
	pmsettings.Sanitize(&rec.Settings)
	payload, err := json.Marshal(rec.Settings)
	if err != nil {
		return fmt.Errorf("failed to encode settings: %w", err)
	}
	if rec.SchemaVersion == "" {
		rec.SchemaVersion = pmsettings.SchemaVersion
	}
	if rec.UpdatedBy == "" {
		rec.UpdatedBy = "system"
	}
	query := `
		INSERT INTO settings_global (id, schema_version, payload, updated_at, updated_by)
		VALUES (1, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(id) DO UPDATE SET
			schema_version = excluded.schema_version,
			payload = excluded.payload,
			updated_at = CURRENT_TIMESTAMP,
			updated_by = excluded.updated_by
	`
	_, err = s.db.ExecContext(ctx, query, rec.SchemaVersion, string(payload), rec.UpdatedBy)
	return err
}

// GetTenantSettings returns tenant-specific overrides (if any).
func (s *SQLiteStore) GetTenantSettings(ctx context.Context, tenantID string) (*TenantSettingsRecord, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("tenant id required")
	}
	query := `
		SELECT tenant_id, schema_version, payload, updated_at, IFNULL(updated_by, '')
		FROM settings_tenant
		WHERE tenant_id = ?
	`
	var (
		id            string
		schemaVersion string
		payload       sql.NullString
		updatedAt     sql.NullTime
		updatedBy     sql.NullString
	)
	err := s.db.QueryRowContext(ctx, query, tenantID).Scan(&id, &schemaVersion, &payload, &updatedAt, &updatedBy)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	overrides := map[string]interface{}{}
	if payload.Valid && payload.String != "" {
		if err := json.Unmarshal([]byte(payload.String), &overrides); err != nil {
			return nil, fmt.Errorf("failed to decode tenant settings: %w", err)
		}
	}
	rec := &TenantSettingsRecord{
		TenantID:      id,
		SchemaVersion: schemaVersion,
		Overrides:     overrides,
		UpdatedBy:     updatedBy.String,
	}
	if updatedAt.Valid {
		rec.UpdatedAt = updatedAt.Time
	}
	return rec, nil
}

// UpsertTenantSettings stores tenant override patches.
func (s *SQLiteStore) UpsertTenantSettings(ctx context.Context, rec *TenantSettingsRecord) error {
	if rec == nil {
		return fmt.Errorf("tenant settings record required")
	}
	if strings.TrimSpace(rec.TenantID) == "" {
		return fmt.Errorf("tenant id required")
	}
	if rec.Overrides == nil {
		rec.Overrides = map[string]interface{}{}
	}
	payload, err := json.Marshal(rec.Overrides)
	if err != nil {
		return fmt.Errorf("failed to encode overrides: %w", err)
	}
	if rec.SchemaVersion == "" {
		rec.SchemaVersion = pmsettings.SchemaVersion
	}
	if rec.UpdatedBy == "" {
		rec.UpdatedBy = "system"
	}
	query := `
		INSERT INTO settings_tenant (tenant_id, schema_version, payload, updated_at, updated_by)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(tenant_id) DO UPDATE SET
			schema_version = excluded.schema_version,
			payload = excluded.payload,
			updated_at = CURRENT_TIMESTAMP,
			updated_by = excluded.updated_by
	`
	_, err = s.db.ExecContext(ctx, query, rec.TenantID, rec.SchemaVersion, string(payload), rec.UpdatedBy)
	return err
}

// DeleteTenantSettings removes overrides for a tenant.
func (s *SQLiteStore) DeleteTenantSettings(ctx context.Context, tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("tenant id required")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM settings_tenant WHERE tenant_id = ?", tenantID)
	return err
}

// ListTenantSettings returns all tenants with overrides.
func (s *SQLiteStore) ListTenantSettings(ctx context.Context) ([]*TenantSettingsRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, schema_version, payload, updated_at, IFNULL(updated_by, '')
		FROM settings_tenant
		ORDER BY tenant_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*TenantSettingsRecord
	for rows.Next() {
		var (
			tenantID      string
			schemaVersion string
			payload       sql.NullString
			updatedAt     sql.NullTime
			updatedBy     sql.NullString
		)
		if err := rows.Scan(&tenantID, &schemaVersion, &payload, &updatedAt, &updatedBy); err != nil {
			return nil, err
		}
		overrides := map[string]interface{}{}
		if payload.Valid && payload.String != "" {
			if err := json.Unmarshal([]byte(payload.String), &overrides); err != nil {
				return nil, fmt.Errorf("failed to decode tenant settings: %w", err)
			}
		}
		rec := &TenantSettingsRecord{
			TenantID:      tenantID,
			SchemaVersion: schemaVersion,
			Overrides:     overrides,
			UpdatedBy:     updatedBy.String,
		}
		if updatedAt.Valid {
			rec.UpdatedAt = updatedAt.Time
		}
		records = append(records, rec)
	}
	return records, rows.Err()
}

// GetFleetUpdatePolicy retrieves the update policy for a tenant.
func (s *SQLiteStore) GetFleetUpdatePolicy(ctx context.Context, tenantID string) (*FleetUpdatePolicy, error) {
	if tenantID == GlobalFleetPolicyTenantID {
		return s.getGlobalFleetUpdatePolicy(ctx)
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, update_check_days, version_pin_strategy, allow_major_upgrade, target_version,
		       maintenance_window_enabled, maintenance_window_start_hour, maintenance_window_start_min,
		       maintenance_window_end_hour, maintenance_window_end_min, maintenance_window_timezone,
		       maintenance_window_days, rollout_staggered, rollout_max_concurrent, rollout_batch_size,
		       rollout_delay_between_waves, rollout_jitter_seconds, rollout_emergency_abort,
		       collect_telemetry, updated_at
		FROM fleet_update_policies
		WHERE tenant_id = ?
	`, tenantID)

	var (
		tid                   string
		updateCheckDays       int
		pinStrategy           string
		allowMajor            int
		targetVersion         sql.NullString
		mwEnabled             int
		mwStartHour           int
		mwStartMin            int
		mwEndHour             int
		mwEndMin              int
		mwTimezone            string
		mwDaysJSON            sql.NullString
		rolloutStaggered      int
		rolloutMaxConcurrent  int
		rolloutBatchSize      int
		rolloutDelayWaves     int
		rolloutJitterSec      int
		rolloutEmergencyAbort int
		collectTelemetry      int
		updatedAt             time.Time
	)

	if err := row.Scan(&tid, &updateCheckDays, &pinStrategy, &allowMajor, &targetVersion,
		&mwEnabled, &mwStartHour, &mwStartMin, &mwEndHour, &mwEndMin, &mwTimezone, &mwDaysJSON,
		&rolloutStaggered, &rolloutMaxConcurrent, &rolloutBatchSize, &rolloutDelayWaves,
		&rolloutJitterSec, &rolloutEmergencyAbort, &collectTelemetry, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	var daysOfWeek []int
	if mwDaysJSON.Valid && mwDaysJSON.String != "" {
		if err := json.Unmarshal([]byte(mwDaysJSON.String), &daysOfWeek); err != nil {
			return nil, fmt.Errorf("failed to decode days_of_week: %w", err)
		}
	}

	policy := &FleetUpdatePolicy{
		TenantID:  tid,
		UpdatedAt: updatedAt,
		PolicySpec: PolicySpec{
			UpdateCheckDays:    updateCheckDays,
			VersionPinStrategy: VersionPinStrategy(pinStrategy),
			AllowMajorUpgrade:  allowMajor != 0,
			TargetVersion:      targetVersion.String,
			CollectTelemetry:   collectTelemetry != 0,
			MaintenanceWindow: MaintenanceWindow{
				Enabled:    mwEnabled != 0,
				StartHour:  mwStartHour,
				StartMin:   mwStartMin,
				EndHour:    mwEndHour,
				EndMin:     mwEndMin,
				Timezone:   mwTimezone,
				DaysOfWeek: daysOfWeek,
			},
			RolloutControl: RolloutControl{
				Staggered:         rolloutStaggered != 0,
				MaxConcurrent:     rolloutMaxConcurrent,
				BatchSize:         rolloutBatchSize,
				DelayBetweenWaves: rolloutDelayWaves,
				JitterSeconds:     rolloutJitterSec,
				EmergencyAbort:    rolloutEmergencyAbort != 0,
			},
		},
	}

	return policy, nil
}

func (s *SQLiteStore) getGlobalFleetUpdatePolicy(ctx context.Context) (*FleetUpdatePolicy, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT update_check_days, version_pin_strategy, allow_major_upgrade, target_version,
		       maintenance_window_enabled, maintenance_window_start_hour, maintenance_window_start_min,
		       maintenance_window_end_hour, maintenance_window_end_min, maintenance_window_timezone,
		       maintenance_window_days, rollout_staggered, rollout_max_concurrent, rollout_batch_size,
		       rollout_delay_between_waves, rollout_jitter_seconds, rollout_emergency_abort,
		       collect_telemetry, updated_at
		FROM fleet_update_policy_global
		WHERE singleton = 1
	`)

	var (
		updateCheckDays       int
		pinStrategy           string
		allowMajor            int
		targetVersion         sql.NullString
		mwEnabled             int
		mwStartHour           int
		mwStartMin            int
		mwEndHour             int
		mwEndMin              int
		mwTimezone            string
		mwDaysJSON            sql.NullString
		rolloutStaggered      int
		rolloutMaxConcurrent  int
		rolloutBatchSize      int
		rolloutDelayWaves     int
		rolloutJitterSec      int
		rolloutEmergencyAbort int
		collectTelemetry      int
		updatedAt             time.Time
	)

	if err := row.Scan(&updateCheckDays, &pinStrategy, &allowMajor, &targetVersion,
		&mwEnabled, &mwStartHour, &mwStartMin, &mwEndHour, &mwEndMin, &mwTimezone, &mwDaysJSON,
		&rolloutStaggered, &rolloutMaxConcurrent, &rolloutBatchSize, &rolloutDelayWaves,
		&rolloutJitterSec, &rolloutEmergencyAbort, &collectTelemetry, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	var daysOfWeek []int
	if mwDaysJSON.Valid && mwDaysJSON.String != "" {
		if err := json.Unmarshal([]byte(mwDaysJSON.String), &daysOfWeek); err != nil {
			return nil, fmt.Errorf("failed to decode days_of_week: %w", err)
		}
	}

	policy := &FleetUpdatePolicy{
		TenantID:  GlobalFleetPolicyTenantID,
		UpdatedAt: updatedAt,
		PolicySpec: PolicySpec{
			UpdateCheckDays:    updateCheckDays,
			VersionPinStrategy: VersionPinStrategy(pinStrategy),
			AllowMajorUpgrade:  allowMajor != 0,
			TargetVersion:      targetVersion.String,
			CollectTelemetry:   collectTelemetry != 0,
			MaintenanceWindow: MaintenanceWindow{
				Enabled:    mwEnabled != 0,
				StartHour:  mwStartHour,
				StartMin:   mwStartMin,
				EndHour:    mwEndHour,
				EndMin:     mwEndMin,
				Timezone:   mwTimezone,
				DaysOfWeek: daysOfWeek,
			},
			RolloutControl: RolloutControl{
				Staggered:         rolloutStaggered != 0,
				MaxConcurrent:     rolloutMaxConcurrent,
				BatchSize:         rolloutBatchSize,
				DelayBetweenWaves: rolloutDelayWaves,
				JitterSeconds:     rolloutJitterSec,
				EmergencyAbort:    rolloutEmergencyAbort != 0,
			},
		},
	}

	return policy, nil
}

// UpsertFleetUpdatePolicy creates or updates a tenant's update policy.
func (s *SQLiteStore) UpsertFleetUpdatePolicy(ctx context.Context, policy *FleetUpdatePolicy) error {
	if policy == nil {
		return fmt.Errorf("policy cannot be nil")
	}

	if policy.TenantID == GlobalFleetPolicyTenantID {
		return s.upsertGlobalFleetUpdatePolicy(ctx, policy)
	}

	daysJSON, err := json.Marshal(policy.MaintenanceWindow.DaysOfWeek)
	if err != nil {
		return fmt.Errorf("failed to encode days_of_week: %w", err)
	}

	policy.UpdatedAt = time.Now().UTC()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO fleet_update_policies (
			tenant_id, update_check_days, version_pin_strategy, allow_major_upgrade, target_version,
			maintenance_window_enabled, maintenance_window_start_hour, maintenance_window_start_min,
			maintenance_window_end_hour, maintenance_window_end_min, maintenance_window_timezone,
			maintenance_window_days, rollout_staggered, rollout_max_concurrent, rollout_batch_size,
			rollout_delay_between_waves, rollout_jitter_seconds, rollout_emergency_abort,
			collect_telemetry, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id) DO UPDATE SET
			update_check_days = excluded.update_check_days,
			version_pin_strategy = excluded.version_pin_strategy,
			allow_major_upgrade = excluded.allow_major_upgrade,
			target_version = excluded.target_version,
			maintenance_window_enabled = excluded.maintenance_window_enabled,
			maintenance_window_start_hour = excluded.maintenance_window_start_hour,
			maintenance_window_start_min = excluded.maintenance_window_start_min,
			maintenance_window_end_hour = excluded.maintenance_window_end_hour,
			maintenance_window_end_min = excluded.maintenance_window_end_min,
			maintenance_window_timezone = excluded.maintenance_window_timezone,
			maintenance_window_days = excluded.maintenance_window_days,
			rollout_staggered = excluded.rollout_staggered,
			rollout_max_concurrent = excluded.rollout_max_concurrent,
			rollout_batch_size = excluded.rollout_batch_size,
			rollout_delay_between_waves = excluded.rollout_delay_between_waves,
			rollout_jitter_seconds = excluded.rollout_jitter_seconds,
			rollout_emergency_abort = excluded.rollout_emergency_abort,
			collect_telemetry = excluded.collect_telemetry,
			updated_at = excluded.updated_at
	`,
		policy.TenantID,
		policy.UpdateCheckDays,
		string(policy.VersionPinStrategy),
		boolToInt(policy.AllowMajorUpgrade),
		nullString(policy.TargetVersion),
		boolToInt(policy.MaintenanceWindow.Enabled),
		policy.MaintenanceWindow.StartHour,
		policy.MaintenanceWindow.StartMin,
		policy.MaintenanceWindow.EndHour,
		policy.MaintenanceWindow.EndMin,
		policy.MaintenanceWindow.Timezone,
		string(daysJSON),
		boolToInt(policy.RolloutControl.Staggered),
		policy.RolloutControl.MaxConcurrent,
		policy.RolloutControl.BatchSize,
		policy.RolloutControl.DelayBetweenWaves,
		policy.RolloutControl.JitterSeconds,
		boolToInt(policy.RolloutControl.EmergencyAbort),
		boolToInt(policy.CollectTelemetry),
		policy.UpdatedAt,
	)

	return err
}

func (s *SQLiteStore) upsertGlobalFleetUpdatePolicy(ctx context.Context, policy *FleetUpdatePolicy) error {
	if policy == nil {
		return fmt.Errorf("policy cannot be nil")
	}

	daysJSON, err := json.Marshal(policy.MaintenanceWindow.DaysOfWeek)
	if err != nil {
		return fmt.Errorf("failed to encode days_of_week: %w", err)
	}

	policy.UpdatedAt = time.Now().UTC()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO fleet_update_policy_global (
			singleton, update_check_days, version_pin_strategy, allow_major_upgrade, target_version,
			maintenance_window_enabled, maintenance_window_start_hour, maintenance_window_start_min,
			maintenance_window_end_hour, maintenance_window_end_min, maintenance_window_timezone,
			maintenance_window_days, rollout_staggered, rollout_max_concurrent, rollout_batch_size,
			rollout_delay_between_waves, rollout_jitter_seconds, rollout_emergency_abort,
			collect_telemetry, updated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET
			update_check_days = excluded.update_check_days,
			version_pin_strategy = excluded.version_pin_strategy,
			allow_major_upgrade = excluded.allow_major_upgrade,
			target_version = excluded.target_version,
			maintenance_window_enabled = excluded.maintenance_window_enabled,
			maintenance_window_start_hour = excluded.maintenance_window_start_hour,
			maintenance_window_start_min = excluded.maintenance_window_start_min,
			maintenance_window_end_hour = excluded.maintenance_window_end_hour,
			maintenance_window_end_min = excluded.maintenance_window_end_min,
			maintenance_window_timezone = excluded.maintenance_window_timezone,
			maintenance_window_days = excluded.maintenance_window_days,
			rollout_staggered = excluded.rollout_staggered,
			rollout_max_concurrent = excluded.rollout_max_concurrent,
			rollout_batch_size = excluded.rollout_batch_size,
			rollout_delay_between_waves = excluded.rollout_delay_between_waves,
			rollout_jitter_seconds = excluded.rollout_jitter_seconds,
			rollout_emergency_abort = excluded.rollout_emergency_abort,
			collect_telemetry = excluded.collect_telemetry,
			updated_at = excluded.updated_at
	`,
		policy.UpdateCheckDays,
		string(policy.VersionPinStrategy),
		boolToInt(policy.AllowMajorUpgrade),
		nullString(policy.TargetVersion),
		boolToInt(policy.MaintenanceWindow.Enabled),
		policy.MaintenanceWindow.StartHour,
		policy.MaintenanceWindow.StartMin,
		policy.MaintenanceWindow.EndHour,
		policy.MaintenanceWindow.EndMin,
		policy.MaintenanceWindow.Timezone,
		string(daysJSON),
		boolToInt(policy.RolloutControl.Staggered),
		policy.RolloutControl.MaxConcurrent,
		policy.RolloutControl.BatchSize,
		policy.RolloutControl.DelayBetweenWaves,
		policy.RolloutControl.JitterSeconds,
		boolToInt(policy.RolloutControl.EmergencyAbort),
		boolToInt(policy.CollectTelemetry),
		policy.UpdatedAt,
	)

	return err
}

// DeleteFleetUpdatePolicy removes a tenant's update policy.
func (s *SQLiteStore) DeleteFleetUpdatePolicy(ctx context.Context, tenantID string) error {
	if tenantID == GlobalFleetPolicyTenantID {
		_, err := s.db.ExecContext(ctx, `DELETE FROM fleet_update_policy_global WHERE singleton = 1`)
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM fleet_update_policies WHERE tenant_id = ?`, tenantID)
	return err
}

// ListFleetUpdatePolicies returns all configured update policies.
func (s *SQLiteStore) ListFleetUpdatePolicies(ctx context.Context) ([]*FleetUpdatePolicy, error) {
	var policies []*FleetUpdatePolicy
	if globalPolicy, err := s.getGlobalFleetUpdatePolicy(ctx); err != nil {
		return nil, err
	} else if globalPolicy != nil {
		policies = append(policies, globalPolicy)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, update_check_days, version_pin_strategy, allow_major_upgrade, target_version,
		       maintenance_window_enabled, maintenance_window_start_hour, maintenance_window_start_min,
		       maintenance_window_end_hour, maintenance_window_end_min, maintenance_window_timezone,
		       maintenance_window_days, rollout_staggered, rollout_max_concurrent, rollout_batch_size,
		       rollout_delay_between_waves, rollout_jitter_seconds, rollout_emergency_abort,
		       collect_telemetry, updated_at
		FROM fleet_update_policies
		ORDER BY tenant_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tid                   string
			updateCheckDays       int
			pinStrategy           string
			allowMajor            int
			targetVersion         sql.NullString
			mwEnabled             int
			mwStartHour           int
			mwStartMin            int
			mwEndHour             int
			mwEndMin              int
			mwTimezone            string
			mwDaysJSON            sql.NullString
			rolloutStaggered      int
			rolloutMaxConcurrent  int
			rolloutBatchSize      int
			rolloutDelayWaves     int
			rolloutJitterSec      int
			rolloutEmergencyAbort int
			collectTelemetry      int
			updatedAt             time.Time
		)

		if err := rows.Scan(&tid, &updateCheckDays, &pinStrategy, &allowMajor, &targetVersion,
			&mwEnabled, &mwStartHour, &mwStartMin, &mwEndHour, &mwEndMin, &mwTimezone, &mwDaysJSON,
			&rolloutStaggered, &rolloutMaxConcurrent, &rolloutBatchSize, &rolloutDelayWaves,
			&rolloutJitterSec, &rolloutEmergencyAbort, &collectTelemetry, &updatedAt); err != nil {
			return nil, err
		}

		var daysOfWeek []int
		if mwDaysJSON.Valid && mwDaysJSON.String != "" {
			if err := json.Unmarshal([]byte(mwDaysJSON.String), &daysOfWeek); err != nil {
				return nil, fmt.Errorf("failed to decode days_of_week: %w", err)
			}
		}

		policy := &FleetUpdatePolicy{
			TenantID:  tid,
			UpdatedAt: updatedAt,
			PolicySpec: PolicySpec{
				UpdateCheckDays:    updateCheckDays,
				VersionPinStrategy: VersionPinStrategy(pinStrategy),
				AllowMajorUpgrade:  allowMajor != 0,
				TargetVersion:      targetVersion.String,
				CollectTelemetry:   collectTelemetry != 0,
				MaintenanceWindow: MaintenanceWindow{
					Enabled:    mwEnabled != 0,
					StartHour:  mwStartHour,
					StartMin:   mwStartMin,
					EndHour:    mwEndHour,
					EndMin:     mwEndMin,
					Timezone:   mwTimezone,
					DaysOfWeek: daysOfWeek,
				},
				RolloutControl: RolloutControl{
					Staggered:         rolloutStaggered != 0,
					MaxConcurrent:     rolloutMaxConcurrent,
					BatchSize:         rolloutBatchSize,
					DelayBetweenWaves: rolloutDelayWaves,
					JitterSeconds:     rolloutJitterSec,
					EmergencyAbort:    rolloutEmergencyAbort != 0,
				},
			},
		}

		policies = append(policies, policy)
	}

	return policies, rows.Err()
}

// UpsertReleaseArtifact stores or updates metadata for a cached release artifact.
func (s *SQLiteStore) UpsertReleaseArtifact(ctx context.Context, artifact *ReleaseArtifact) error {
	if artifact == nil {
		return fmt.Errorf("artifact cannot be nil")
	}
	if artifact.Component == "" || artifact.Version == "" || artifact.Platform == "" || artifact.Arch == "" {
		return fmt.Errorf("artifact missing required identity fields")
	}
	if artifact.Channel == "" {
		artifact.Channel = "stable"
	}
	if artifact.SourceURL == "" {
		return fmt.Errorf("artifact source url required")
	}
	artifact.UpdatedAt = time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = artifact.UpdatedAt
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO release_artifacts (
			component, version, platform, arch, channel, source_url,
			cache_path, sha256, size_bytes, release_notes, published_at,
			downloaded_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(component, version, platform, arch) DO UPDATE SET
			channel = excluded.channel,
			source_url = excluded.source_url,
			cache_path = excluded.cache_path,
			sha256 = excluded.sha256,
			size_bytes = excluded.size_bytes,
			release_notes = excluded.release_notes,
			published_at = excluded.published_at,
			downloaded_at = excluded.downloaded_at,
			updated_at = excluded.updated_at
	`,
		artifact.Component,
		artifact.Version,
		artifact.Platform,
		artifact.Arch,
		artifact.Channel,
		artifact.SourceURL,
		nullString(artifact.CachePath),
		nullString(artifact.SHA256),
		artifact.SizeBytes,
		nullString(artifact.ReleaseNotes),
		nullTime(artifact.PublishedAt),
		nullTime(artifact.DownloadedAt),
		artifact.CreatedAt,
		artifact.UpdatedAt,
	)
	return err
}

// GetReleaseArtifact returns the cached artifact metadata for the requested tuple.
func (s *SQLiteStore) GetReleaseArtifact(ctx context.Context, component, version, platform, arch string) (*ReleaseArtifact, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, component, version, platform, arch, channel, source_url,
		       cache_path, sha256, size_bytes, release_notes, published_at,
		       downloaded_at, created_at, updated_at
		FROM release_artifacts
		WHERE component = ? AND version = ? AND platform = ? AND arch = ?
	`, component, version, platform, arch)
	return scanReleaseArtifact(row)
}

// ListReleaseArtifacts lists cached artifacts for a component ordered by recency.
func (s *SQLiteStore) ListReleaseArtifacts(ctx context.Context, component string, limit int) ([]*ReleaseArtifact, error) {
	query := `
		SELECT id, component, version, platform, arch, channel, source_url,
		       cache_path, sha256, size_bytes, release_notes, published_at,
		       downloaded_at, created_at, updated_at
		FROM release_artifacts
		WHERE (? = '' OR component = ?)
		ORDER BY created_at DESC, version DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += " LIMIT ?"
		rows, err = s.db.QueryContext(ctx, query, component, component, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query, component, component)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []*ReleaseArtifact
	for rows.Next() {
		artifact, serr := scanReleaseArtifact(rows)
		if serr != nil {
			return nil, serr
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func scanReleaseArtifact(scanner interface {
	Scan(dest ...interface{}) error
}) (*ReleaseArtifact, error) {
	var (
		id                   int64
		component            string
		version              string
		platform             string
		arch                 string
		channel              string
		sourceURL            string
		cachePath            sql.NullString
		sha                  sql.NullString
		sizeBytes            int64
		releaseNotes         sql.NullString
		publishedAt          sql.NullTime
		downloadedAt         sql.NullTime
		createdAt, updatedAt time.Time
	)

	if err := scanner.Scan(&id, &component, &version, &platform, &arch, &channel, &sourceURL,
		&cachePath, &sha, &sizeBytes, &releaseNotes, &publishedAt, &downloadedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	artifact := &ReleaseArtifact{
		ID:        id,
		Component: component,
		Version:   version,
		Platform:  platform,
		Arch:      arch,
		Channel:   channel,
		SourceURL: sourceURL,
		SizeBytes: sizeBytes,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if cachePath.Valid {
		artifact.CachePath = cachePath.String
	}
	if sha.Valid {
		artifact.SHA256 = sha.String
	}
	if releaseNotes.Valid {
		artifact.ReleaseNotes = releaseNotes.String
	}
	if publishedAt.Valid {
		artifact.PublishedAt = publishedAt.Time
	}
	if downloadedAt.Valid {
		artifact.DownloadedAt = downloadedAt.Time
	}
	return artifact, nil
}

// CreateSigningKey persists a new signing key record.
func (s *SQLiteStore) CreateSigningKey(ctx context.Context, key *SigningKey) error {
	if key == nil {
		return fmt.Errorf("signing key cannot be nil")
	}
	if key.ID == "" || key.Algorithm == "" {
		return fmt.Errorf("signing key id and algorithm required")
	}
	if key.PublicKey == "" || key.PrivateKey == "" {
		return fmt.Errorf("signing key material incomplete")
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO signing_keys (id, algorithm, public_key, private_key, notes, active, created_at, rotated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		key.ID,
		key.Algorithm,
		key.PublicKey,
		key.PrivateKey,
		nullString(key.Notes),
		boolToInt(key.Active),
		key.CreatedAt,
		nullTime(key.RotatedAt),
	)
	return err
}

// GetSigningKey loads key metadata (including private material) by id.
func (s *SQLiteStore) GetSigningKey(ctx context.Context, id string) (*SigningKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, algorithm, public_key, private_key, notes, active, created_at, rotated_at
		FROM signing_keys WHERE id = ?
	`, id)
	return scanSigningKey(row)
}

// GetActiveSigningKey retrieves the currently active signing key.
func (s *SQLiteStore) GetActiveSigningKey(ctx context.Context) (*SigningKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, algorithm, public_key, private_key, notes, active, created_at, rotated_at
		FROM signing_keys WHERE active = 1 LIMIT 1
	`)
	return scanSigningKey(row)
}

// ListSigningKeys returns signing key metadata ordered by creation recency.
func (s *SQLiteStore) ListSigningKeys(ctx context.Context, limit int) ([]*SigningKey, error) {
	query := `
		SELECT id, algorithm, public_key, private_key, notes, active, created_at, rotated_at
		FROM signing_keys
		ORDER BY created_at DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += " LIMIT ?"
		rows, err = s.db.QueryContext(ctx, query, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*SigningKey
	for rows.Next() {
		key, serr := scanSigningKey(rows)
		if serr != nil {
			return nil, serr
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// SetSigningKeyActive marks the provided key as active and deactivates others.
func (s *SQLiteStore) SetSigningKeyActive(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("signing key id required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `
		UPDATE signing_keys
		SET active = 0, rotated_at = CASE WHEN rotated_at IS NULL THEN CURRENT_TIMESTAMP ELSE rotated_at END
		WHERE active = 1 AND id != ?
	`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE signing_keys
		SET active = 1, rotated_at = NULL
		WHERE id = ?
	`, id)
	if err != nil {
		return err
	}
	affected, aerr := result.RowsAffected()
	if aerr != nil {
		return aerr
	}
	if affected == 0 {
		return fmt.Errorf("signing key %s not found", id)
	}
	return tx.Commit()
}

// UpsertReleaseManifest stores the signed manifest for an artifact tuple.
func (s *SQLiteStore) UpsertReleaseManifest(ctx context.Context, manifest *ReleaseManifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest cannot be nil")
	}
	if manifest.Component == "" || manifest.Version == "" || manifest.Platform == "" || manifest.Arch == "" {
		return fmt.Errorf("manifest identity incomplete")
	}
	if manifest.Channel == "" {
		manifest.Channel = "stable"
	}
	if manifest.ManifestVersion == "" || manifest.ManifestJSON == "" || manifest.Signature == "" {
		return fmt.Errorf("manifest payload incomplete")
	}
	if manifest.SigningKeyID == "" {
		return fmt.Errorf("manifest missing signing key reference")
	}
	manifest.UpdatedAt = time.Now().UTC()
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = manifest.UpdatedAt
	}
	if manifest.GeneratedAt.IsZero() {
		manifest.GeneratedAt = manifest.UpdatedAt
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO release_manifests (
			component, version, platform, arch, channel,
			manifest_version, manifest_json, signature, signing_key_id,
			generated_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(component, version, platform, arch) DO UPDATE SET
			channel = excluded.channel,
			manifest_version = excluded.manifest_version,
			manifest_json = excluded.manifest_json,
			signature = excluded.signature,
			signing_key_id = excluded.signing_key_id,
			generated_at = excluded.generated_at,
			updated_at = excluded.updated_at
	`,
		manifest.Component,
		manifest.Version,
		manifest.Platform,
		manifest.Arch,
		manifest.Channel,
		manifest.ManifestVersion,
		manifest.ManifestJSON,
		manifest.Signature,
		manifest.SigningKeyID,
		manifest.GeneratedAt,
		manifest.CreatedAt,
		manifest.UpdatedAt,
	)
	return err
}

// GetReleaseManifest fetches the manifest envelope for the given artifact tuple.
func (s *SQLiteStore) GetReleaseManifest(ctx context.Context, component, version, platform, arch string) (*ReleaseManifest, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, component, version, platform, arch, channel,
		       manifest_version, manifest_json, signature, signing_key_id,
		       generated_at, created_at, updated_at
		FROM release_manifests
		WHERE component = ? AND version = ? AND platform = ? AND arch = ?
	`, component, version, platform, arch)
	return scanReleaseManifest(row)
}

// ListReleaseManifests enumerates manifests optionally filtered by component.
func (s *SQLiteStore) ListReleaseManifests(ctx context.Context, component string, limit int) ([]*ReleaseManifest, error) {
	query := `
		SELECT id, component, version, platform, arch, channel,
		       manifest_version, manifest_json, signature, signing_key_id,
		       generated_at, created_at, updated_at
		FROM release_manifests
		WHERE (? = '' OR component = ?)
		ORDER BY generated_at DESC, version DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += " LIMIT ?"
		rows, err = s.db.QueryContext(ctx, query, component, component, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query, component, component)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var manifests []*ReleaseManifest
	for rows.Next() {
		manifest, serr := scanReleaseManifest(rows)
		if serr != nil {
			return nil, serr
		}
		manifests = append(manifests, manifest)
	}
	return manifests, rows.Err()
}

func scanSigningKey(scanner interface {
	Scan(dest ...interface{}) error
}) (*SigningKey, error) {
	var (
		id         string
		algorithm  string
		publicKey  string
		privateKey string
		notes      sql.NullString
		active     int
		createdAt  time.Time
		rotatedAt  sql.NullTime
	)
	if err := scanner.Scan(&id, &algorithm, &publicKey, &privateKey, &notes, &active, &createdAt, &rotatedAt); err != nil {
		return nil, err
	}
	key := &SigningKey{
		ID:         id,
		Algorithm:  algorithm,
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		Active:     active == 1,
		CreatedAt:  createdAt,
	}
	if notes.Valid {
		key.Notes = notes.String
	}
	if rotatedAt.Valid {
		key.RotatedAt = rotatedAt.Time
	}
	return key, nil
}

func scanReleaseManifest(scanner interface {
	Scan(dest ...interface{}) error
}) (*ReleaseManifest, error) {
	var (
		id              int64
		component       string
		version         string
		platform        string
		arch            string
		channel         string
		manifestVersion string
		manifestJSON    string
		signature       string
		signingKeyID    string
		generatedAt     time.Time
		createdAt       time.Time
		updatedAt       time.Time
	)
	if err := scanner.Scan(&id, &component, &version, &platform, &arch, &channel,
		&manifestVersion, &manifestJSON, &signature, &signingKeyID,
		&generatedAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	return &ReleaseManifest{
		ID:              id,
		Component:       component,
		Version:         version,
		Platform:        platform,
		Arch:            arch,
		Channel:         channel,
		ManifestVersion: manifestVersion,
		ManifestJSON:    manifestJSON,
		Signature:       signature,
		SigningKeyID:    signingKeyID,
		GeneratedAt:     generatedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// CreateInstallerBundle stores or updates a tenant-scoped installer record.
func (s *SQLiteStore) CreateInstallerBundle(ctx context.Context, bundle *InstallerBundle) error {
	if bundle == nil {
		return fmt.Errorf("installer bundle cannot be nil")
	}
	if bundle.TenantID == "" || bundle.Component == "" || bundle.Version == "" || bundle.Platform == "" || bundle.Arch == "" || bundle.Format == "" {
		return fmt.Errorf("installer bundle missing required identity fields")
	}
	if bundle.ConfigHash == "" || bundle.BundlePath == "" {
		return fmt.Errorf("installer bundle requires config hash and path")
	}
	bundle.UpdatedAt = time.Now().UTC()
	if bundle.CreatedAt.IsZero() {
		bundle.CreatedAt = bundle.UpdatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO installer_bundles (
			tenant_id, component, version, platform, arch, format,
			source_artifact_id, config_hash, bundle_path, size_bytes,
			encrypted, encryption_key_id, metadata_json, expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, component, version, platform, arch, format, config_hash) DO UPDATE SET
			source_artifact_id = excluded.source_artifact_id,
			bundle_path = excluded.bundle_path,
			size_bytes = excluded.size_bytes,
			encrypted = excluded.encrypted,
			encryption_key_id = excluded.encryption_key_id,
			metadata_json = excluded.metadata_json,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at
	`,
		bundle.TenantID,
		bundle.Component,
		bundle.Version,
		bundle.Platform,
		bundle.Arch,
		bundle.Format,
		nullInt64(bundle.SourceArtifactID),
		bundle.ConfigHash,
		bundle.BundlePath,
		bundle.SizeBytes,
		boolToInt(bundle.Encrypted),
		nullString(bundle.EncryptionKeyID),
		nullString(bundle.MetadataJSON),
		nullTime(bundle.ExpiresAt),
		bundle.CreatedAt,
		bundle.UpdatedAt,
	)
	return err
}

// GetInstallerBundle loads a bundle by numeric id.
func (s *SQLiteStore) GetInstallerBundle(ctx context.Context, id int64) (*InstallerBundle, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, component, version, platform, arch, format,
		       source_artifact_id, config_hash, bundle_path, size_bytes,
		       encrypted, encryption_key_id, metadata_json, expires_at, created_at, updated_at
		FROM installer_bundles WHERE id = ?
	`, id)
	return scanInstallerBundle(row)
}

// FindInstallerBundle fetches a bundle by its unique identity tuple.
func (s *SQLiteStore) FindInstallerBundle(ctx context.Context, tenantID, component, version, platform, arch, format, configHash string) (*InstallerBundle, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, component, version, platform, arch, format,
		       source_artifact_id, config_hash, bundle_path, size_bytes,
		       encrypted, encryption_key_id, metadata_json, expires_at, created_at, updated_at
		FROM installer_bundles
		WHERE tenant_id = ? AND component = ? AND version = ? AND platform = ? AND arch = ? AND format = ? AND config_hash = ?
	`, tenantID, component, version, platform, arch, format, configHash)
	return scanInstallerBundle(row)
}

// ListInstallerBundles returns bundles for a tenant ordered by recency.
func (s *SQLiteStore) ListInstallerBundles(ctx context.Context, tenantID string, limit int) ([]*InstallerBundle, error) {
	query := `
		SELECT id, tenant_id, component, version, platform, arch, format,
		       source_artifact_id, config_hash, bundle_path, size_bytes,
		       encrypted, encryption_key_id, metadata_json, expires_at, created_at, updated_at
		FROM installer_bundles
		WHERE (? = '' OR tenant_id = ?)
		ORDER BY created_at DESC
	`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += " LIMIT ?"
		rows, err = s.db.QueryContext(ctx, query, tenantID, tenantID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query, tenantID, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var bundles []*InstallerBundle
	for rows.Next() {
		bundle, serr := scanInstallerBundle(rows)
		if serr != nil {
			return nil, serr
		}
		bundles = append(bundles, bundle)
	}
	return bundles, rows.Err()
}

// DeleteInstallerBundle removes a bundle by id.
func (s *SQLiteStore) DeleteInstallerBundle(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM installer_bundles WHERE id = ?`, id)
	return err
}

// DeleteExpiredInstallerBundles removes bundles with expires_at before cutoff.
func (s *SQLiteStore) DeleteExpiredInstallerBundles(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM installer_bundles WHERE expires_at IS NOT NULL AND expires_at <= ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CreateSelfUpdateRun persists a new self-update run record.
func (s *SQLiteStore) CreateSelfUpdateRun(ctx context.Context, run *SelfUpdateRun) error {
	if run == nil {
		return fmt.Errorf("self-update run cannot be nil")
	}
	if run.Status == "" {
		run.Status = SelfUpdateStatusPending
	}
	if run.Channel == "" {
		run.Channel = "stable"
	}
	if run.RequestedAt.IsZero() {
		run.RequestedAt = time.Now().UTC()
	}
	metaJSON, err := encodeMetadata(run.Metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO self_update_runs (
			status, requested_at, started_at, completed_at,
			current_version, target_version, channel, platform, arch,
			release_artifact_id, error_code, error_message, metadata_json, requested_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		string(run.Status),
		run.RequestedAt,
		nullTime(run.StartedAt),
		nullTime(run.CompletedAt),
		run.CurrentVersion,
		run.TargetVersion,
		run.Channel,
		run.Platform,
		run.Arch,
		nullInt64(run.ReleaseArtifactID),
		nullString(run.ErrorCode),
		nullString(run.ErrorMessage),
		metaJSON,
		nullString(run.RequestedBy),
	)
	if err != nil {
		return err
	}
	if id, err := result.LastInsertId(); err == nil {
		run.ID = id
	}
	return nil
}

// UpdateSelfUpdateRun updates an existing run record.
func (s *SQLiteStore) UpdateSelfUpdateRun(ctx context.Context, run *SelfUpdateRun) error {
	if run == nil {
		return fmt.Errorf("self-update run cannot be nil")
	}
	if run.ID == 0 {
		return fmt.Errorf("self-update run id required")
	}
	if run.Channel == "" {
		run.Channel = "stable"
	}
	metaJSON, err := encodeMetadata(run.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE self_update_runs SET
			status = ?,
			started_at = ?,
			completed_at = ?,
			current_version = ?,
			target_version = ?,
			channel = ?,
			platform = ?,
			arch = ?,
			release_artifact_id = ?,
			error_code = ?,
			error_message = ?,
			metadata_json = ?,
			requested_by = ?
		WHERE id = ?
	`,
		string(run.Status),
		nullTime(run.StartedAt),
		nullTime(run.CompletedAt),
		run.CurrentVersion,
		run.TargetVersion,
		run.Channel,
		run.Platform,
		run.Arch,
		nullInt64(run.ReleaseArtifactID),
		nullString(run.ErrorCode),
		nullString(run.ErrorMessage),
		metaJSON,
		nullString(run.RequestedBy),
		run.ID,
	)
	return err
}

// GetSelfUpdateRun returns a single self-update run by id.
func (s *SQLiteStore) GetSelfUpdateRun(ctx context.Context, id int64) (*SelfUpdateRun, error) {
	if id == 0 {
		return nil, fmt.Errorf("self-update run id required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, requested_at, started_at, completed_at,
		       current_version, target_version, channel, platform, arch,
		       release_artifact_id, error_code, error_message, metadata_json, requested_by
		FROM self_update_runs
		WHERE id = ?
	`, id)
	return scanSelfUpdateRun(row)
}

// ListSelfUpdateRuns returns the most recent self-update runs.
func (s *SQLiteStore) ListSelfUpdateRuns(ctx context.Context, limit int) ([]*SelfUpdateRun, error) {
	if limit <= 0 {
		limit = 25
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, requested_at, started_at, completed_at,
		       current_version, target_version, channel, platform, arch,
		       release_artifact_id, error_code, error_message, metadata_json, requested_by
		FROM self_update_runs
		ORDER BY requested_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*SelfUpdateRun
	for rows.Next() {
		run, serr := scanSelfUpdateRun(rows)
		if serr != nil {
			return nil, serr
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanInstallerBundle(scanner interface {
	Scan(dest ...interface{}) error
}) (*InstallerBundle, error) {
	var (
		id               int64
		tenantID         string
		component        string
		version          string
		platform         string
		arch             string
		format           string
		sourceArtifactID sql.NullInt64
		configHash       string
		bundlePath       string
		sizeBytes        int64
		encryptedInt     int
		encryptionKeyID  sql.NullString
		metadataJSON     sql.NullString
		expiresAt        sql.NullTime
		createdAt        time.Time
		updatedAt        time.Time
	)
	if err := scanner.Scan(&id, &tenantID, &component, &version, &platform, &arch, &format, &sourceArtifactID, &configHash, &bundlePath, &sizeBytes, &encryptedInt, &encryptionKeyID, &metadataJSON, &expiresAt, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	bundle := &InstallerBundle{
		ID:         id,
		TenantID:   tenantID,
		Component:  component,
		Version:    version,
		Platform:   platform,
		Arch:       arch,
		Format:     format,
		ConfigHash: configHash,
		BundlePath: bundlePath,
		SizeBytes:  sizeBytes,
		Encrypted:  encryptedInt == 1,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}
	if sourceArtifactID.Valid {
		bundle.SourceArtifactID = sourceArtifactID.Int64
	}
	if encryptionKeyID.Valid {
		bundle.EncryptionKeyID = encryptionKeyID.String
	}
	if metadataJSON.Valid {
		bundle.MetadataJSON = metadataJSON.String
	}
	if expiresAt.Valid {
		bundle.ExpiresAt = expiresAt.Time
	}
	return bundle, nil
}

func scanSelfUpdateRun(scanner interface {
	Scan(dest ...interface{}) error
}) (*SelfUpdateRun, error) {
	var (
		id                int64
		status            string
		requestedAt       time.Time
		startedAt         sql.NullTime
		completedAt       sql.NullTime
		currentVersion    sql.NullString
		targetVersion     sql.NullString
		channel           sql.NullString
		platform          sql.NullString
		arch              sql.NullString
		releaseArtifactID sql.NullInt64
		errorCode         sql.NullString
		errorMessage      sql.NullString
		metadataJSON      sql.NullString
		requestedBy       sql.NullString
	)
	if err := scanner.Scan(&id, &status, &requestedAt, &startedAt, &completedAt, &currentVersion, &targetVersion, &channel, &platform, &arch, &releaseArtifactID, &errorCode, &errorMessage, &metadataJSON, &requestedBy); err != nil {
		return nil, err
	}
	run := &SelfUpdateRun{
		ID:             id,
		Status:         SelfUpdateStatus(status),
		RequestedAt:    requestedAt,
		CurrentVersion: currentVersion.String,
		TargetVersion:  targetVersion.String,
		Channel:        channel.String,
		Platform:       platform.String,
		Arch:           arch.String,
		Metadata:       decodeMetadata(metadataJSON),
		RequestedBy:    requestedBy.String,
	}
	if startedAt.Valid {
		run.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time
	}
	if releaseArtifactID.Valid {
		run.ReleaseArtifactID = releaseArtifactID.Int64
	}
	if errorCode.Valid {
		run.ErrorCode = errorCode.String
	}
	if errorMessage.Valid {
		run.ErrorMessage = errorMessage.String
	}
	if !requestedBy.Valid {
		run.RequestedBy = ""
	}
	return run, nil
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
		// Check if running in Docker (common Docker indicators)
		if _, err := os.Stat("/.dockerenv"); err == nil {
			// Running in Docker - use /var/lib path (matches volume mount)
			return "/var/lib/printmaster/server/printmaster.db"
		}

		// Check if /var/lib/printmaster exists (system installation)
		if _, err := os.Stat("/var/lib/printmaster"); err == nil {
			return "/var/lib/printmaster/server/printmaster.db"
		}

		// Fall back to user home directory
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local/share/printmaster/server/server.db")
	}
}

// GetAggregatedMetrics retrieves fleet-wide aggregated metrics for the dashboard view.
func (s *SQLiteStore) GetAggregatedMetrics(ctx context.Context, since time.Time, tenantIDs []string) (*AggregatedMetrics, error) {
	now := time.Now().UTC()
	agg := &AggregatedMetrics{
		GeneratedAt: now,
		RangeStart:  since.UTC(),
		RangeEnd:    now,
	}

	allowedTenants := make(map[string]struct{}, len(tenantIDs))
	for _, id := range tenantIDs {
		allowedTenants[id] = struct{}{}
	}

	agents, err := s.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	filteredAgents := make([]*Agent, 0, len(agents))
	agentTenantMap := make(map[string]string, len(agents))
	for _, a := range agents {
		if a == nil {
			continue
		}
		if len(tenantIDs) > 0 {
			if _, ok := allowedTenants[a.TenantID]; !ok {
				continue
			}
		}
		filteredAgents = append(filteredAgents, a)
		agentTenantMap[a.AgentID] = a.TenantID
	}
	agg.Fleet.Totals.Agents = len(filteredAgents)

	devices, err := s.ListAllDevices(ctx)
	if err != nil {
		return nil, err
	}

	filteredDevices := make([]*Device, 0, len(devices))
	serialMap := make(map[string]struct{}, len(devices))
	deviceBySerial := make(map[string]*Device, len(devices))
	for _, d := range devices {
		if d == nil {
			continue
		}
		if len(tenantIDs) > 0 {
			if _, ok := agentTenantMap[d.AgentID]; !ok {
				continue
			}
		}
		filteredDevices = append(filteredDevices, d)
		serialMap[d.Serial] = struct{}{}
		deviceBySerial[d.Serial] = d

		hasError, hasWarning, hasJam := classifyStatusMessages(d.StatusMessages)
		if hasJam {
			agg.Fleet.Statuses.Jam++
			agg.Fleet.Statuses.Error++
		} else if hasError {
			agg.Fleet.Statuses.Error++
		} else if hasWarning {
			agg.Fleet.Statuses.Warning++
		}
	}
	agg.Fleet.Totals.Devices = len(filteredDevices)

	if len(filteredDevices) == 0 {
		return agg, nil
	}

	buckets := make(map[time.Time]*fleetMetricBucket)
	lastValues := make(map[string]*MetricsSnapshot)

	query := `
		SELECT m.timestamp, m.serial, m.agent_id, m.page_count, m.color_pages, m.mono_pages, m.scan_count, m.toner_levels
		FROM metrics_history m
		JOIN agents a ON m.agent_id = a.agent_id
		WHERE m.timestamp >= ?
	`
	args := []interface{}{since.UTC().Format(time.RFC3339Nano)}
	if len(tenantIDs) > 0 {
		query += ` AND a.tenant_id IN (` + buildPlaceholderList(len(tenantIDs)) + `)`
		for _, id := range tenantIDs {
			args = append(args, id)
		}
	}
	query += ` ORDER BY m.timestamp ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			tsStr    string
			serial   string
			agentID  string
			pc, cp   int64
			mp, sc   int64
			tonerRaw sql.NullString
		)
		if err := rows.Scan(&tsStr, &serial, &agentID, &pc, &cp, &mp, &sc, &tonerRaw); err != nil {
			return nil, err
		}
		if _, ok := serialMap[serial]; !ok {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			continue
		}
		bucketTime := ts.Truncate(time.Hour)
		if _, ok := buckets[bucketTime]; !ok {
			buckets[bucketTime] = &fleetMetricBucket{}
		}
		point := buckets[bucketTime]

		if last, ok := lastValues[serial]; ok {
			if pc >= int64(last.PageCount) {
				point.total += pc - int64(last.PageCount)
			}
			if cp >= int64(last.ColorPages) {
				point.color += cp - int64(last.ColorPages)
			}
			if mp >= int64(last.MonoPages) {
				point.mono += mp - int64(last.MonoPages)
			}
			if sc >= int64(last.ScanCount) {
				point.scan += sc - int64(last.ScanCount)
			}
		}

		var toner map[string]interface{}
		if tonerRaw.Valid && strings.TrimSpace(tonerRaw.String) != "" {
			var tmp map[string]interface{}
			if err := json.Unmarshal([]byte(tonerRaw.String), &tmp); err == nil {
				toner = tmp
			}
		}

		lastValues[serial] = &MetricsSnapshot{
			Serial:      serial,
			AgentID:     agentID,
			Timestamp:   ts,
			PageCount:   int(pc),
			ColorPages:  int(cp),
			MonoPages:   int(mp),
			ScanCount:   int(sc),
			TonerLevels: toner,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, d := range filteredDevices {
		if _, ok := lastValues[d.Serial]; ok {
			continue
		}
		snapshot, err := s.GetLatestMetrics(ctx, d.Serial)
		if err != nil || snapshot == nil {
			continue
		}
		lastValues[d.Serial] = snapshot
	}

	if len(buckets) > 0 {
		ordered := make([]time.Time, 0, len(buckets))
		for ts := range buckets {
			ordered = append(ordered, ts)
		}
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].Before(ordered[j])
		})
		for _, ts := range ordered {
			bucket := buckets[ts]
			agg.Fleet.History.TotalImpressions = append(agg.Fleet.History.TotalImpressions, MetricSeriesPoint{Timestamp: ts, Value: bucket.total})
			agg.Fleet.History.MonoImpressions = append(agg.Fleet.History.MonoImpressions, MetricSeriesPoint{Timestamp: ts, Value: bucket.mono})
			agg.Fleet.History.ColorImpressions = append(agg.Fleet.History.ColorImpressions, MetricSeriesPoint{Timestamp: ts, Value: bucket.color})
			agg.Fleet.History.ScanVolume = append(agg.Fleet.History.ScanVolume, MetricSeriesPoint{Timestamp: ts, Value: bucket.scan})
		}
	}

	bandCounts := make(map[consumableBand]int)
	for _, d := range filteredDevices {
		snapshot := lastValues[d.Serial]
		if snapshot != nil {
			agg.Fleet.Totals.PageCount += int64(snapshot.PageCount)
			agg.Fleet.Totals.ColorPages += int64(snapshot.ColorPages)
			agg.Fleet.Totals.MonoPages += int64(snapshot.MonoPages)
			agg.Fleet.Totals.ScanCount += int64(snapshot.ScanCount)
		}
		band := classifyConsumableBand(snapshot, deviceBySerial[d.Serial])
		bandCounts[band]++
	}

	agg.Fleet.Consumables.Critical = bandCounts[consumableCritical]
	agg.Fleet.Consumables.Low = bandCounts[consumableLow]
	agg.Fleet.Consumables.Medium = bandCounts[consumableMedium]
	agg.Fleet.Consumables.High = bandCounts[consumableHigh]
	agg.Fleet.Consumables.Unknown = bandCounts[consumableUnknown]

	return agg, nil
}

// GetDatabaseStats returns high-level counts for observability panels.
func (s *SQLiteStore) GetDatabaseStats(ctx context.Context) (*DatabaseStats, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM agents),
			(SELECT COUNT(*) FROM devices),
			(SELECT COUNT(*) FROM metrics_history),
			(SELECT COUNT(*) FROM sessions),
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM audit_log),
			(SELECT COUNT(*) FROM release_artifacts),
			COALESCE((SELECT SUM(size_bytes) FROM release_artifacts), 0),
			(SELECT COUNT(*) FROM installer_bundles),
			COALESCE((SELECT SUM(size_bytes) FROM installer_bundles), 0)
	`)
	stats := &DatabaseStats{}
	if err := row.Scan(
		&stats.Agents,
		&stats.Devices,
		&stats.MetricsSnapshots,
		&stats.Sessions,
		&stats.Users,
		&stats.AuditEntries,
		&stats.ReleaseArtifacts,
		&stats.ReleaseBytes,
		&stats.InstallerBundles,
		&stats.InstallerBytes,
	); err != nil {
		return nil, err
	}
	return stats, nil
}

type fleetMetricBucket struct {
	total int64
	mono  int64
	color int64
	scan  int64
}

type consumableBand int

const (
	consumableUnknown consumableBand = iota
	consumableHigh
	consumableMedium
	consumableLow
	consumableCritical
)

func classifyStatusMessages(messages []string) (bool, bool, bool) {
	var hasError, hasWarning, hasJam bool
	for _, msg := range messages {
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "jam"):
			hasJam = true
			hasError = true
		case strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "offline"):
			hasError = true
		case strings.Contains(lower, "warn") || strings.Contains(lower, "low"):
			hasWarning = true
		}
	}
	return hasError, hasWarning, hasJam
}

func classifyConsumableBand(snapshot *MetricsSnapshot, device *Device) consumableBand {
	if snapshot != nil {
		if band := bandFromTonerLevels(snapshot.TonerLevels); band != consumableUnknown {
			return band
		}
	}
	if device != nil {
		if band := bandFromConsumableStrings(device.Consumables); band != consumableUnknown {
			return band
		}
	}
	return consumableUnknown
}

func bandFromTonerLevels(levels map[string]interface{}) consumableBand {
	if len(levels) == 0 {
		return consumableUnknown
	}
	worst := consumableUnknown
	for _, raw := range levels {
		if pct, ok := normalizePercentage(raw); ok {
			band := bandForPercentage(pct)
			if band > worst {
				worst = band
			}
		}
	}
	return worst
}

func bandFromConsumableStrings(entries []string) consumableBand {
	worst := consumableUnknown
	for _, entry := range entries {
		lower := strings.ToLower(entry)
		switch {
		case strings.Contains(lower, "empty") || strings.Contains(lower, "replace") || strings.Contains(lower, "exhausted") || strings.Contains(lower, "depleted"):
			if consumableCritical > worst {
				worst = consumableCritical
			}
		case strings.Contains(lower, "very low") || strings.Contains(lower, "near empty"):
			if consumableCritical > worst {
				worst = consumableCritical
			}
		case strings.Contains(lower, "low"):
			if consumableLow > worst {
				worst = consumableLow
			}
		case strings.Contains(lower, "medium") || strings.Contains(lower, "half"):
			if consumableMedium > worst {
				worst = consumableMedium
			}
		case strings.Contains(lower, "high") || strings.Contains(lower, "full") || strings.Contains(lower, "ok") || strings.Contains(lower, "ready"):
			if consumableHigh > worst {
				worst = consumableHigh
			}
		}
		if pct, ok := percentFromString(entry); ok {
			band := bandForPercentage(pct)
			if band > worst {
				worst = band
			}
		}
	}
	return worst
}

func normalizePercentage(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	case string:
		return percentFromString(v)
	}
	return 0, false
}

func percentFromString(s string) (float64, bool) {
	var buf strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			buf.WriteRune(r)
			continue
		}
		if buf.Len() > 0 {
			break
		}
	}
	if buf.Len() == 0 {
		return 0, false
	}
	val, err := strconv.ParseFloat(buf.String(), 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

func bandForPercentage(pct float64) consumableBand {
	switch {
	case pct <= 5:
		return consumableCritical
	case pct <= 15:
		return consumableLow
	case pct <= 60:
		return consumableMedium
	default:
		return consumableHigh
	}
}

func buildPlaceholderList(count int) string {
	if count <= 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
	}
	return b.String()
}
