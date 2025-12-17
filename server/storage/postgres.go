package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"printmaster/common/config"
	pmsettings "printmaster/common/settings"

	// Import postgres driver
	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresStore implements Store interface for PostgreSQL.
type PostgresStore struct {
	BaseStore
}

const pgSchemaVersion = 9

// NewPostgresStore creates a new PostgreSQL store.
func NewPostgresStore(cfg *config.DatabaseConfig) (*PostgresStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config required")
	}

	dsn := cfg.BuildDSN()
	if dsn == "" {
		return nil, fmt.Errorf("invalid database configuration: could not build DSN")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Configure connection pool
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetimeSecs > 0 {
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSecs) * time.Second)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	store := &PostgresStore{
		BaseStore: BaseStore{
			db:      db,
			dialect: &PostgresDialect{},
		},
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize postgres schema: %w", err)
	}

	logInfo("Opened PostgreSQL database", "host", cfg.Host, "database", cfg.Name)

	return store, nil
}

// initSchema creates the database schema for PostgreSQL.
func (s *PostgresStore) initSchema() error {
	schema := `
	-- Schema version tracking
	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Registered agents
	CREATE TABLE IF NOT EXISTS agents (
		id BIGSERIAL PRIMARY KEY,
		agent_id TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '',
		hostname TEXT NOT NULL,
		ip TEXT NOT NULL,
		platform TEXT NOT NULL,
		version TEXT NOT NULL,
		protocol_version TEXT NOT NULL,
		token TEXT NOT NULL,
		registered_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		last_seen TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		status TEXT NOT NULL DEFAULT 'active',
		tenant_id TEXT,
		os_version TEXT,
		go_version TEXT,
		architecture TEXT,
		num_cpu INTEGER DEFAULT 0,
		total_memory_mb BIGINT DEFAULT 0,
		build_type TEXT,
		git_commit TEXT,
		last_heartbeat TIMESTAMPTZ,
		device_count INTEGER DEFAULT 0,
		last_device_sync TIMESTAMPTZ,
		last_metrics_sync TIMESTAMPTZ
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
		last_seen TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		first_seen TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		discovery_method TEXT,
		asset_number TEXT,
		location TEXT,
		description TEXT,
		web_ui_url TEXT,
		raw_data TEXT,
		tenant_id TEXT,
		CONSTRAINT fk_devices_agent FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_devices_agent_id ON devices(agent_id);
	CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
	CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);

	-- Metrics history
	CREATE TABLE IF NOT EXISTS metrics_history (
		id BIGSERIAL PRIMARY KEY,
		serial TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		timestamp TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		page_count INTEGER DEFAULT 0,
		color_pages INTEGER DEFAULT 0,
		mono_pages INTEGER DEFAULT 0,
		scan_count INTEGER DEFAULT 0,
		toner_levels TEXT,
		tenant_id TEXT,
		CONSTRAINT fk_metrics_device FOREIGN KEY(serial) REFERENCES devices(serial) ON DELETE CASCADE,
		CONSTRAINT fk_metrics_agent FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_metrics_serial ON metrics_history(serial);
	CREATE INDEX IF NOT EXISTS idx_metrics_agent_id ON metrics_history(agent_id);
	CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON metrics_history(timestamp);

	-- Audit log
	CREATE TABLE IF NOT EXISTS audit_log (
		id BIGSERIAL PRIMARY KEY,
		timestamp TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_tenants_login_domain ON tenants(login_domain)
		WHERE login_domain IS NOT NULL AND login_domain != '';

	-- Sites table
	CREATE TABLE IF NOT EXISTS sites (
		id TEXT PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		address TEXT,
		filter_rules TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_sites_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_sites_tenant ON sites(tenant_id);

	-- Agent-to-site assignments
	CREATE TABLE IF NOT EXISTS agent_sites (
		agent_id TEXT NOT NULL,
		site_id TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (agent_id, site_id),
		CONSTRAINT fk_agent_sites_site FOREIGN KEY(site_id) REFERENCES sites(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_agent_sites_site ON agent_sites(site_id);
	CREATE INDEX IF NOT EXISTS idx_agent_sites_agent ON agent_sites(agent_id);

	-- Join tokens for agent onboarding
	CREATE TABLE IF NOT EXISTS join_tokens (
		id TEXT PRIMARY KEY,
		token_hash TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		one_time BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		used_at TIMESTAMPTZ,
		revoked BOOLEAN DEFAULT FALSE,
		CONSTRAINT fk_join_tokens_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_join_tokens_hash ON join_tokens(token_hash);
	CREATE INDEX IF NOT EXISTS idx_join_tokens_tenant ON join_tokens(tenant_id);

	-- Local users
	CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'viewer',
		tenant_id TEXT,
		email TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);

	-- User-tenant mappings
	CREATE TABLE IF NOT EXISTS user_tenants (
		user_id BIGINT NOT NULL,
		tenant_id TEXT NOT NULL,
		PRIMARY KEY (user_id, tenant_id),
		CONSTRAINT fk_user_tenants_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
		CONSTRAINT fk_user_tenants_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_user_tenants_tenant ON user_tenants(tenant_id);

	-- Sessions
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id BIGINT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_sessions_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

	-- Password reset tokens
	CREATE TABLE IF NOT EXISTS password_resets (
		id BIGSERIAL PRIMARY KEY,
		token_hash TEXT NOT NULL,
		user_id BIGINT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		used BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_password_resets_user FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_password_resets_user_id ON password_resets(user_id);

	-- User invitations
	CREATE TABLE IF NOT EXISTS user_invitations (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL,
		username TEXT,
		role TEXT NOT NULL DEFAULT 'viewer',
		tenant_id TEXT,
		token_hash TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		used BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_by TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_user_invitations_email ON user_invitations(email);

	-- OIDC providers
	CREATE TABLE IF NOT EXISTS oidc_providers (
		id BIGSERIAL PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		display_name TEXT NOT NULL,
		issuer TEXT NOT NULL,
		client_id TEXT NOT NULL,
		client_secret TEXT NOT NULL,
		scopes TEXT NOT NULL DEFAULT 'openid profile email',
		icon TEXT,
		button_text TEXT,
		button_style TEXT,
		auto_login BOOLEAN NOT NULL DEFAULT FALSE,
		tenant_id TEXT,
		default_role TEXT NOT NULL DEFAULT 'viewer',
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_oidc_providers_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	-- OIDC sessions
	CREATE TABLE IF NOT EXISTS oidc_sessions (
		id TEXT PRIMARY KEY,
		provider_slug TEXT NOT NULL,
		tenant_id TEXT,
		nonce TEXT NOT NULL,
		state TEXT NOT NULL,
		redirect_url TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_oidc_sessions_provider FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
		CONSTRAINT fk_oidc_sessions_tenant FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	-- OIDC links
	CREATE TABLE IF NOT EXISTS oidc_links (
		id BIGSERIAL PRIMARY KEY,
		provider_slug TEXT NOT NULL,
		subject TEXT NOT NULL,
		email TEXT,
		user_id BIGINT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(provider_slug, subject),
		CONSTRAINT fk_oidc_links_provider FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
		CONSTRAINT fk_oidc_links_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	-- Global settings
	CREATE TABLE IF NOT EXISTS settings_global (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		schema_version TEXT NOT NULL,
		payload TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT
	);

	-- Tenant settings
	CREATE TABLE IF NOT EXISTS settings_tenant (
		tenant_id TEXT PRIMARY KEY,
		schema_version TEXT NOT NULL,
		payload TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT,
		CONSTRAINT fk_settings_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	-- Agent settings overrides (per-agent override patches)
	CREATE TABLE IF NOT EXISTS settings_agent_override (
		agent_id TEXT PRIMARY KEY,
		schema_version TEXT NOT NULL,
		payload TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_by TEXT,
		CONSTRAINT fk_settings_agent_override FOREIGN KEY(agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE
	);

	-- Fleet update policies
	CREATE TABLE IF NOT EXISTS fleet_update_policies (
		tenant_id TEXT PRIMARY KEY,
		update_check_days INTEGER NOT NULL DEFAULT 0,
		version_pin_strategy TEXT NOT NULL DEFAULT 'minor',
		allow_major_upgrade BOOLEAN NOT NULL DEFAULT FALSE,
		target_version TEXT,
		maintenance_window_enabled BOOLEAN NOT NULL DEFAULT FALSE,
		maintenance_window_start_hour INTEGER DEFAULT 2,
		maintenance_window_start_min INTEGER DEFAULT 0,
		maintenance_window_end_hour INTEGER DEFAULT 4,
		maintenance_window_end_min INTEGER DEFAULT 0,
		maintenance_window_timezone TEXT DEFAULT 'UTC',
		maintenance_window_days TEXT,
		rollout_staggered BOOLEAN NOT NULL DEFAULT FALSE,
		rollout_max_concurrent INTEGER DEFAULT 0,
		rollout_batch_size INTEGER DEFAULT 0,
		rollout_delay_between_waves INTEGER DEFAULT 300,
		rollout_jitter_seconds INTEGER DEFAULT 60,
		rollout_emergency_abort BOOLEAN NOT NULL DEFAULT FALSE,
		collect_telemetry BOOLEAN NOT NULL DEFAULT TRUE,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_fleet_policies_tenant FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
	);

	-- Global fleet update policy
	CREATE TABLE IF NOT EXISTS fleet_update_policy_global (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		update_check_days INTEGER NOT NULL DEFAULT 0,
		version_pin_strategy TEXT NOT NULL DEFAULT 'minor',
		allow_major_upgrade BOOLEAN NOT NULL DEFAULT FALSE,
		target_version TEXT,
		maintenance_window_enabled BOOLEAN NOT NULL DEFAULT FALSE,
		maintenance_window_start_hour INTEGER DEFAULT 2,
		maintenance_window_start_min INTEGER DEFAULT 0,
		maintenance_window_end_hour INTEGER DEFAULT 4,
		maintenance_window_end_min INTEGER DEFAULT 0,
		maintenance_window_timezone TEXT DEFAULT 'UTC',
		maintenance_window_days TEXT,
		rollout_staggered BOOLEAN NOT NULL DEFAULT FALSE,
		rollout_max_concurrent INTEGER DEFAULT 0,
		rollout_batch_size INTEGER DEFAULT 0,
		rollout_delay_between_waves INTEGER DEFAULT 300,
		rollout_jitter_seconds INTEGER DEFAULT 60,
		rollout_emergency_abort BOOLEAN NOT NULL DEFAULT FALSE,
		collect_telemetry BOOLEAN NOT NULL DEFAULT TRUE,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Release artifacts
	CREATE TABLE IF NOT EXISTS release_artifacts (
		id BIGSERIAL PRIMARY KEY,
		component TEXT NOT NULL,
		version TEXT NOT NULL,
		platform TEXT NOT NULL,
		arch TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT 'stable',
		source_url TEXT NOT NULL,
		cache_path TEXT,
		sha256 TEXT,
		size_bytes BIGINT NOT NULL DEFAULT 0,
		release_notes TEXT,
		published_at TIMESTAMPTZ,
		downloaded_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(component, version, platform, arch)
	);

	CREATE INDEX IF NOT EXISTS idx_release_artifacts_component ON release_artifacts(component);

	-- Signing keys
	CREATE TABLE IF NOT EXISTS signing_keys (
		id TEXT PRIMARY KEY,
		algorithm TEXT NOT NULL,
		public_key TEXT NOT NULL,
		private_key TEXT NOT NULL,
		notes TEXT,
		active BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		rotated_at TIMESTAMPTZ
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_signing_keys_active ON signing_keys(active) WHERE active = TRUE;

	-- Release manifests
	CREATE TABLE IF NOT EXISTS release_manifests (
		id BIGSERIAL PRIMARY KEY,
		component TEXT NOT NULL,
		version TEXT NOT NULL,
		platform TEXT NOT NULL,
		arch TEXT NOT NULL,
		channel TEXT NOT NULL DEFAULT 'stable',
		manifest_version TEXT NOT NULL,
		manifest_json TEXT NOT NULL,
		signature TEXT NOT NULL,
		signing_key_id TEXT NOT NULL,
		generated_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(component, version, platform, arch)
	);

	CREATE INDEX IF NOT EXISTS idx_release_manifests_component ON release_manifests(component);

	-- Installer bundles
	CREATE TABLE IF NOT EXISTS installer_bundles (
		id BIGSERIAL PRIMARY KEY,
		tenant_id TEXT NOT NULL,
		component TEXT NOT NULL,
		version TEXT NOT NULL,
		platform TEXT NOT NULL,
		arch TEXT NOT NULL,
		format TEXT NOT NULL,
		source_artifact_id BIGINT,
		config_hash TEXT NOT NULL,
		bundle_path TEXT NOT NULL,
		size_bytes BIGINT NOT NULL DEFAULT 0,
		encrypted BOOLEAN NOT NULL DEFAULT FALSE,
		encryption_key_id TEXT,
		metadata_json TEXT,
		expires_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_bundles_artifact FOREIGN KEY(source_artifact_id) REFERENCES release_artifacts(id) ON DELETE SET NULL,
		UNIQUE(tenant_id, component, version, platform, arch, format, config_hash)
	);

	CREATE INDEX IF NOT EXISTS idx_installer_bundles_tenant ON installer_bundles(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_installer_bundles_expires ON installer_bundles(expires_at);

	-- Self-update runs
	CREATE TABLE IF NOT EXISTS self_update_runs (
		id BIGSERIAL PRIMARY KEY,
		status TEXT NOT NULL,
		requested_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		started_at TIMESTAMPTZ,
		completed_at TIMESTAMPTZ,
		current_version TEXT,
		target_version TEXT,
		channel TEXT NOT NULL DEFAULT 'stable',
		platform TEXT,
		arch TEXT,
		release_artifact_id BIGINT,
		error_code TEXT,
		error_message TEXT,
		metadata_json TEXT,
		requested_by TEXT,
		CONSTRAINT fk_update_runs_artifact FOREIGN KEY(release_artifact_id) REFERENCES release_artifacts(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_self_update_runs_requested_at ON self_update_runs(requested_at DESC);
	CREATE INDEX IF NOT EXISTS idx_self_update_runs_status ON self_update_runs(status);

	-- Alert rules (create first as alerts references it)
	CREATE TABLE IF NOT EXISTS alert_rules (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		scope TEXT NOT NULL,
		tenant_ids TEXT,
		site_ids TEXT,
		agent_ids TEXT,
		condition_json TEXT,
		threshold REAL,
		threshold_unit TEXT,
		duration_minutes INTEGER NOT NULL DEFAULT 0,
		channel_ids TEXT,
		escalation_policy_id BIGINT,
		cooldown_minutes INTEGER NOT NULL DEFAULT 15,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_by TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_alert_rules_type ON alert_rules(type);
	CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(enabled);

	-- Escalation policies
	CREATE TABLE IF NOT EXISTS escalation_policies (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		steps_json TEXT NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Add FK constraint after both tables exist
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_name = 'fk_alert_rules_escalation'
		) THEN
			ALTER TABLE alert_rules
			ADD CONSTRAINT fk_alert_rules_escalation
			FOREIGN KEY (escalation_policy_id) REFERENCES escalation_policies(id) ON DELETE SET NULL;
		END IF;
	END $$;

	-- Alerts
	CREATE TABLE IF NOT EXISTS alerts (
		id BIGSERIAL PRIMARY KEY,
		rule_id BIGINT,
		type TEXT NOT NULL,
		severity TEXT NOT NULL,
		scope TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		tenant_id TEXT,
		site_id TEXT,
		agent_id TEXT,
		device_serial TEXT,
		title TEXT NOT NULL,
		message TEXT NOT NULL,
		details TEXT,
		triggered_at TIMESTAMPTZ NOT NULL,
		acknowledged_at TIMESTAMPTZ,
		acknowledged_by TEXT,
		resolved_at TIMESTAMPTZ,
		suppressed_until TIMESTAMPTZ,
		expires_at TIMESTAMPTZ,
		escalation_level INTEGER NOT NULL DEFAULT 0,
		last_escalated_at TIMESTAMPTZ,
		state_change_count INTEGER NOT NULL DEFAULT 0,
		is_flapping BOOLEAN NOT NULL DEFAULT FALSE,
		parent_alert_id BIGINT,
		child_count INTEGER NOT NULL DEFAULT 0,
		notifications_sent INTEGER NOT NULL DEFAULT 0,
		last_notified_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_alerts_rule FOREIGN KEY(rule_id) REFERENCES alert_rules(id) ON DELETE SET NULL,
		CONSTRAINT fk_alerts_parent FOREIGN KEY(parent_alert_id) REFERENCES alerts(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status);
	CREATE INDEX IF NOT EXISTS idx_alerts_severity ON alerts(severity);
	CREATE INDEX IF NOT EXISTS idx_alerts_scope ON alerts(scope);
	CREATE INDEX IF NOT EXISTS idx_alerts_type ON alerts(type);
	CREATE INDEX IF NOT EXISTS idx_alerts_tenant_id ON alerts(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_alerts_triggered_at ON alerts(triggered_at DESC);

	-- Notification channels
	CREATE TABLE IF NOT EXISTS notification_channels (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		config_json TEXT,
		min_severity TEXT NOT NULL DEFAULT 'warning',
		tenant_ids TEXT,
		rate_limit_per_hour INTEGER NOT NULL DEFAULT 100,
		last_sent_at TIMESTAMPTZ,
		sent_this_hour INTEGER NOT NULL DEFAULT 0,
		use_quiet_hours BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_notification_channels_type ON notification_channels(type);
	CREATE INDEX IF NOT EXISTS idx_notification_channels_enabled ON notification_channels(enabled);

	-- Maintenance windows
	CREATE TABLE IF NOT EXISTS maintenance_windows (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		scope TEXT NOT NULL,
		tenant_id TEXT,
		site_id TEXT,
		agent_id TEXT,
		device_serial TEXT,
		start_time TIMESTAMPTZ NOT NULL,
		end_time TIMESTAMPTZ NOT NULL,
		timezone TEXT NOT NULL DEFAULT 'UTC',
		recurring BOOLEAN NOT NULL DEFAULT FALSE,
		recur_pattern TEXT,
		recur_days TEXT,
		alert_types TEXT,
		allow_critical BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		created_by TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_maintenance_windows_time ON maintenance_windows(start_time, end_time);
	CREATE INDEX IF NOT EXISTS idx_maintenance_windows_scope ON maintenance_windows(scope);

	-- Alert settings
	CREATE TABLE IF NOT EXISTS alert_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	-- Report definitions
	CREATE TABLE IF NOT EXISTS reports (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		type TEXT NOT NULL,
		format TEXT NOT NULL DEFAULT 'json',
		scope TEXT NOT NULL DEFAULT 'fleet',
		tenant_ids TEXT,
		site_ids TEXT,
		agent_ids TEXT,
		device_filter TEXT,
		time_range_type TEXT,
		time_range_days INTEGER,
		time_range_start TEXT,
		time_range_end TEXT,
		options_json TEXT,
		columns TEXT,
		group_by TEXT,
		order_by TEXT,
		report_limit INTEGER,
		email_recipients TEXT,
		webhook_url TEXT,
		created_by TEXT,
		is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_reports_type ON reports(type);
	CREATE INDEX IF NOT EXISTS idx_reports_scope ON reports(scope);
	CREATE INDEX IF NOT EXISTS idx_reports_created_by ON reports(created_by);

	-- Report schedules
	CREATE TABLE IF NOT EXISTS report_schedules (
		id BIGSERIAL PRIMARY KEY,
		report_id BIGINT NOT NULL,
		name TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT TRUE,
		frequency TEXT NOT NULL,
		day_of_week INTEGER,
		day_of_month INTEGER,
		time_of_day TEXT NOT NULL,
		timezone TEXT NOT NULL DEFAULT 'UTC',
		next_run_at TIMESTAMPTZ NOT NULL,
		last_run_at TIMESTAMPTZ,
		last_run_id BIGINT,
		failure_count INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_schedules_report FOREIGN KEY(report_id) REFERENCES reports(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_report_schedules_report ON report_schedules(report_id);
	CREATE INDEX IF NOT EXISTS idx_report_schedules_next_run ON report_schedules(next_run_at) WHERE enabled = TRUE;

	-- Report runs
	CREATE TABLE IF NOT EXISTS report_runs (
		id BIGSERIAL PRIMARY KEY,
		report_id BIGINT NOT NULL,
		schedule_id BIGINT,
		status TEXT NOT NULL DEFAULT 'pending',
		format TEXT NOT NULL,
		started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMPTZ,
		duration_ms INTEGER,
		parameters_json TEXT,
		row_count INTEGER,
		result_size_bytes INTEGER,
		result_path TEXT,
		result_data TEXT,
		error_message TEXT,
		run_by TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT fk_runs_report FOREIGN KEY(report_id) REFERENCES reports(id) ON DELETE CASCADE,
		CONSTRAINT fk_runs_schedule FOREIGN KEY(schedule_id) REFERENCES report_schedules(id) ON DELETE SET NULL
	);

	CREATE INDEX IF NOT EXISTS idx_report_runs_report ON report_runs(report_id);
	CREATE INDEX IF NOT EXISTS idx_report_runs_schedule ON report_runs(schedule_id);
	CREATE INDEX IF NOT EXISTS idx_report_runs_status ON report_runs(status);
	CREATE INDEX IF NOT EXISTS idx_report_runs_started ON report_runs(started_at);

	-- Add FK from report_schedules.last_run_id to report_runs
	DO $$
	BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_name = 'fk_schedules_last_run'
		) THEN
			ALTER TABLE report_schedules
			ADD CONSTRAINT fk_schedules_last_run
			FOREIGN KEY (last_run_id) REFERENCES report_runs(id) ON DELETE SET NULL;
		END IF;
	END $$;
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Fix up settings_agent_override FK for upgraded databases.
	// Historical versions referenced agents(id) (BIGSERIAL) while storing a TEXT agent UUID.
	// Normalize any numeric legacy values to the stable agents.agent_id and recreate the FK.
	if _, err := s.db.Exec(`
		UPDATE settings_agent_override sao
		SET agent_id = a.agent_id
		FROM agents a
		WHERE sao.agent_id ~ '^[0-9]+$'
		  AND a.id = sao.agent_id::bigint
	`); err != nil {
		return fmt.Errorf("failed to normalize settings_agent_override agent_id: %w", err)
	}
	if _, err := s.db.Exec(`
		ALTER TABLE settings_agent_override
		DROP CONSTRAINT IF EXISTS fk_settings_agent_override;
		ALTER TABLE settings_agent_override
		ADD CONSTRAINT fk_settings_agent_override
		FOREIGN KEY (agent_id) REFERENCES agents(agent_id) ON DELETE CASCADE;
	`); err != nil {
		return fmt.Errorf("failed to update settings_agent_override foreign key: %w", err)
	}

	// Seed global settings if not present
	if err := s.ensureGlobalSettingsSeed(); err != nil {
		return err
	}

	// Seed default alert rules on first run
	if err := s.SeedDefaultAlertRules(context.Background()); err != nil {
		return err
	}

	// Update schema version
	var currentVersion int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("failed to check schema version: %w", err)
	}

	if currentVersion < pgSchemaVersion {
		_, err = s.db.Exec("INSERT INTO schema_version (version, applied_at) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING",
			pgSchemaVersion, time.Now())
		if err != nil {
			return fmt.Errorf("failed to update schema version: %w", err)
		}
	}

	logInfo("Schema initialized for PostgreSQL", "schemaVersion", pgSchemaVersion)

	return nil
}

// ensureGlobalSettingsSeed creates the default global settings row if it doesn't exist.
func (s *PostgresStore) ensureGlobalSettingsSeed() error {
	defaults := pmsettings.DefaultSettings()
	pmsettings.Sanitize(&defaults)
	payload, err := json.Marshal(defaults)
	if err != nil {
		return fmt.Errorf("failed to marshal default settings: %w", err)
	}

	stmt := `
		INSERT INTO settings_global (id, schema_version, payload, updated_at, updated_by)
		VALUES (1, $1, $2, CURRENT_TIMESTAMP, 'system')
		ON CONFLICT (id) DO NOTHING
	`
	_, err = s.db.Exec(stmt, pmsettings.SchemaVersion, string(payload))
	return err
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Path returns an empty string since PostgreSQL doesn't use file paths.
func (s *PostgresStore) Path() string {
	return ""
}
