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
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"printmaster/common/logger"

	"golang.org/x/crypto/argon2"

	_ "modernc.org/sqlite" // Pure Go SQLite driver (no CGO required)
)

// SQLiteStore implements Store using SQLite
type SQLiteStore struct {
	db     *sql.DB
	dbPath string
}

const schemaVersion = 1

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

	// Use structured logger if set, otherwise fallback to stdlog
	if Log != nil {
		Log.Info("Opened SQLite database", "path", dbPath)
	} else {
		log.Printf("Opened SQLite database at %s", dbPath)
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

	-- Tenants table for multi-tenant support
	CREATE TABLE IF NOT EXISTS tenants (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
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
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
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
	}

	for _, stmt := range altStmts {
		if _, err := s.db.Exec(stmt); err != nil {
			// Ignore errors, but log them for visibility during migration
			if Log != nil {
				Log.Warn("SQLite migration statement (ignored error)", "stmt", stmt, "error", err)
			} else {
				log.Printf("SQLite migration statement (ignored error): %s -> %v", stmt, err)
			}
		} else {
			if Log != nil {
				Log.Debug("SQLite migration statement applied (or already present)", "stmt", stmt)
			} else {
				log.Printf("SQLite migration statement applied (or already present): %s", stmt)
			}
		}
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

	// Security migration: detect and clear legacy sessions stored as raw tokens
	// New behavior stores SHA-256 hex tokens (64 chars). If any existing rows
	// contain tokens of a different length, clear the sessions table so users
	// must re-authenticate. This avoids keeping raw tokens in the DB.
	var nonHashedCount int
	err = s.db.QueryRow("SELECT COALESCE(COUNT(1),0) FROM sessions WHERE LENGTH(token) != 64").Scan(&nonHashedCount)
	if err == nil && nonHashedCount > 0 {
		if Log != nil {
			Log.Info("Clearing legacy sessions (security migration)", "non_hashed_count", nonHashedCount)
		} else {
			log.Printf("Clearing %d legacy sessions (security migration)", nonHashedCount)
		}
		if _, derr := s.db.Exec("DELETE FROM sessions"); derr != nil {
			if Log != nil {
				Log.Warn("Failed to clear legacy sessions", "error", derr)
			} else {
				log.Printf("Failed to clear legacy sessions: %v", derr)
			}
		}
	}

	if Log != nil {
		Log.Info("Schema initialized for DB", "path", s.dbPath, "schemaVersion", schemaVersion)
	} else {
		log.Printf("Schema initialized for DB %s (schemaVersion=%d)", s.dbPath, schemaVersion)
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
		       token, registered_at, last_seen, status,
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

		err := rows.Scan(
			&agent.ID, &agent.AgentID, &name, &agent.Hostname, &agent.IP,
			&agent.Platform, &agent.Version, &agent.ProtocolVersion,
			&agent.Token, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
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

	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO tenants (id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.Description, tenant.CreatedAt)
	return err
}

func (s *SQLiteStore) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, description, created_at FROM tenants WHERE id = ?`, id)
	var t Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *SQLiteStore) ListTenants(ctx context.Context) ([]*Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, created_at FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt); err != nil {
			return nil, err
		}
		res = append(res, &t)
	}
	return res, nil
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO oidc_providers (
		slug, display_name, issuer, client_id, client_secret, scopes, icon,
		button_text, button_style, auto_login, tenant_id, default_role,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider.Slug, provider.DisplayName, provider.Issuer, provider.ClientID,
		provider.ClientSecret, scopes, provider.Icon, provider.ButtonText,
		provider.ButtonStyle, boolToInt(provider.AutoLogin), provider.TenantID,
		provider.DefaultRole, provider.CreatedAt, provider.UpdatedAt,
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
	_, err := s.db.ExecContext(ctx, `UPDATE oidc_providers SET display_name=?, issuer=?, client_id=?, client_secret=?, scopes=?, icon=?, button_text=?, button_style=?, auto_login=?, tenant_id=?, default_role=?, updated_at=? WHERE slug=?`,
		provider.DisplayName, provider.Issuer, provider.ClientID, provider.ClientSecret,
		scopes, provider.Icon, provider.ButtonText, provider.ButtonStyle,
		boolToInt(provider.AutoLogin), provider.TenantID, provider.DefaultRole,
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
	if err := row.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Issuer, &p.ClientID, &p.ClientSecret, &scopes, &p.Icon, &p.ButtonText, &p.ButtonStyle, &autoLogin, &p.TenantID, &p.DefaultRole, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.AutoLogin = intToBool(autoLogin)
	if scopes == "" {
		scopes = "openid profile email"
	}
	p.Scopes = strings.Fields(scopes)
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
		if err := rows.Scan(&p.ID, &p.Slug, &p.DisplayName, &p.Issuer, &p.ClientID, &p.ClientSecret, &scopes, &p.Icon, &p.ButtonText, &p.ButtonStyle, &autoLogin, &p.TenantID, &p.DefaultRole, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.AutoLogin = intToBool(autoLogin)
		p.Scopes = strings.Fields(scopes)
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

	res, err := s.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, tenant_id, email, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		user.Username, encoded, user.Role, user.TenantID, user.Email, time.Now().UTC())
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	user.ID = id
	user.PasswordHash = encoded
	user.CreatedAt = time.Now().UTC()
	return nil
}

// GetUserByUsername returns a user by username
func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE username = ?`, username)
	var u User
	var tenant sql.NullString
	var email sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenant, &email, &created); err != nil {
		return nil, err
	}
	if tenant.Valid {
		u.TenantID = tenant.String
	}
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
	var tenant sql.NullString
	var email sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenant, &email, &created); err != nil {
		return nil, err
	}
	if tenant.Valid {
		u.TenantID = tenant.String
	}
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
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, tenant_id, email, created_at FROM users ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var res []*User
	for rows.Next() {
		var u User
		var tenant sql.NullString
		var email sql.NullString
		var created sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &tenant, &email, &created); err != nil {
			return nil, err
		}
		if tenant.Valid {
			u.TenantID = tenant.String
		}
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
	_, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, tenant_id = ?, email = ? WHERE id = ?`, user.Role, user.TenantID, user.Email, user.ID)
	return err
}

// GetUserByEmail returns a user by email
func (s *SQLiteStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE email = ?`, email)
	var u User
	var tenant sql.NullString
	var em sql.NullString
	var created sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenant, &em, &created); err != nil {
		return nil, err
	}
	if tenant.Valid {
		u.TenantID = tenant.String
	}
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
