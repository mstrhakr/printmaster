package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// BaseStore provides shared database operations that work across SQLite and PostgreSQL.
// It embeds a *sql.DB connection and a Dialect for handling SQL syntax differences.
//
// Query placeholders are written using SQLite style (?) and converted at runtime
// when using PostgreSQL. This allows a single codebase for all database operations.
type BaseStore struct {
	db      *sql.DB
	dialect Dialect
	dbPath  string // For SQLite compatibility (stores path or DSN)
}

// NewBaseStore creates a new BaseStore with the given database connection and dialect.
func NewBaseStore(db *sql.DB, dialect Dialect, dbPath string) *BaseStore {
	return &BaseStore{
		db:      db,
		dialect: dialect,
		dbPath:  dbPath,
	}
}

// DB returns the underlying database connection.
func (s *BaseStore) DB() *sql.DB {
	return s.db
}

// Dialect returns the SQL dialect being used.
func (s *BaseStore) Dialect() Dialect {
	return s.dialect
}

// Close closes the database connection.
func (s *BaseStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// query converts SQLite-style ? placeholders to the dialect's format and executes.
// This allows writing queries once with ? and having them work on both SQLite and Postgres.
func (s *BaseStore) query(q string) string {
	if s.dialect.Name() == "postgres" {
		return ConvertPlaceholders(q)
	}
	return q
}

// execContext wraps ExecContext with placeholder conversion.
func (s *BaseStore) execContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.query(query), args...)
}

// queryContext wraps QueryContext with placeholder conversion.
func (s *BaseStore) queryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, s.query(query), args...)
}

// queryRowContext wraps QueryRowContext with placeholder conversion.
func (s *BaseStore) queryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	return s.db.QueryRowContext(ctx, s.query(query), args...)
}

// ============================================================================
// Agent Management Methods
// ============================================================================

// RegisterAgent registers a new agent or updates existing
func (s *BaseStore) RegisterAgent(ctx context.Context, agent *Agent) error {
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

	_, err := s.execContext(ctx, query,
		agent.AgentID, agent.Name, agent.Hostname, agent.IP, agent.Platform,
		agent.Version, agent.ProtocolVersion, agent.Token, agent.TenantID, agent.RegisteredAt,
		agent.LastSeen, agent.Status,
		agent.OSVersion, agent.GoVersion, agent.Architecture, agent.NumCPU,
		agent.TotalMemoryMB, agent.BuildType, agent.GitCommit, agent.LastHeartbeat)

	return err
}

// GetAgent retrieves an agent by ID
func (s *BaseStore) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
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

	err := s.queryRowContext(ctx, query, agentID).Scan(
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
	agent.Name = name.String
	agent.OSVersion = osVersion.String
	agent.GoVersion = goVersion.String
	agent.Architecture = architecture.String
	agent.BuildType = buildType.String
	agent.GitCommit = gitCommit.String
	agent.TenantID = tenantID.String
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

	return &agent, nil
}

// GetAgentByToken retrieves an agent by its authentication token
func (s *BaseStore) GetAgentByToken(ctx context.Context, token string) (*Agent, error) {
	query := `
		SELECT id, agent_id, name, hostname, ip, platform, version, protocol_version,
		       token, tenant_id, registered_at, last_seen, status,
		       os_version, go_version, architecture, num_cpu, total_memory_mb,
		       build_type, git_commit, last_heartbeat, device_count,
		       last_device_sync, last_metrics_sync
		FROM agents
		WHERE token = ?
	`

	var agent Agent
	var name, osVersion, goVersion, architecture, buildType, gitCommit sql.NullString
	var numCPU, deviceCount sql.NullInt64
	var totalMemoryMB sql.NullInt64
	var lastHeartbeat, lastDeviceSync, lastMetricsSync sql.NullTime
	var tenantID sql.NullString

	err := s.queryRowContext(ctx, query, token).Scan(
		&agent.ID, &agent.AgentID, &name, &agent.Hostname, &agent.IP,
		&agent.Platform, &agent.Version, &agent.ProtocolVersion,
		&agent.Token, &tenantID, &agent.RegisteredAt, &agent.LastSeen, &agent.Status,
		&osVersion, &goVersion, &architecture, &numCPU, &totalMemoryMB,
		&buildType, &gitCommit, &lastHeartbeat, &deviceCount,
		&lastDeviceSync, &lastMetricsSync,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found")
	}
	if err != nil {
		return nil, err
	}

	agent.Name = name.String
	agent.OSVersion = osVersion.String
	agent.GoVersion = goVersion.String
	agent.Architecture = architecture.String
	agent.BuildType = buildType.String
	agent.GitCommit = gitCommit.String
	agent.TenantID = tenantID.String
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

	return &agent, nil
}

// ListAgents returns all registered agents
func (s *BaseStore) ListAgents(ctx context.Context) ([]*Agent, error) {
	query := `
		SELECT id, agent_id, name, hostname, ip, platform, version, protocol_version,
		       token, tenant_id, registered_at, last_seen, status,
		       os_version, go_version, architecture, num_cpu, total_memory_mb,
		       build_type, git_commit, last_heartbeat, device_count,
		       last_device_sync, last_metrics_sync
		FROM agents
		ORDER BY last_seen DESC
	`

	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*Agent
	for rows.Next() {
		agent, err := s.scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}

	return agents, rows.Err()
}

// scanAgent scans an agent from a row scanner (works with both *sql.Row and *sql.Rows)
func (s *BaseStore) scanAgent(rows *sql.Rows) (*Agent, error) {
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

	agent.Name = name.String
	agent.OSVersion = osVersion.String
	agent.GoVersion = goVersion.String
	agent.Architecture = architecture.String
	agent.BuildType = buildType.String
	agent.GitCommit = gitCommit.String
	agent.TenantID = tenantID.String
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

	return &agent, nil
}

// UpdateAgentHeartbeat updates the last_seen and status for an agent
func (s *BaseStore) UpdateAgentHeartbeat(ctx context.Context, agentID string, status string) error {
	now := time.Now().UTC()
	_, err := s.execContext(ctx,
		`UPDATE agents SET last_seen = ?, last_heartbeat = ?, status = ? WHERE agent_id = ?`,
		now, now, status, agentID)
	return err
}

// UpdateAgentInfo updates agent metadata (version, platform, etc.) typically on heartbeat
func (s *BaseStore) UpdateAgentInfo(ctx context.Context, agent *Agent) error {
	query := `
		UPDATE agents SET
			name = ?,
			hostname = ?,
			ip = ?,
			platform = ?,
			version = ?,
			protocol_version = ?,
			os_version = ?,
			go_version = ?,
			architecture = ?,
			num_cpu = ?,
			total_memory_mb = ?,
			build_type = ?,
			git_commit = ?,
			device_count = ?,
			last_seen = ?,
			last_heartbeat = ?,
			status = ?
		WHERE agent_id = ?
	`

	_, err := s.execContext(ctx, query,
		agent.Name, agent.Hostname, agent.IP, agent.Platform,
		agent.Version, agent.ProtocolVersion,
		agent.OSVersion, agent.GoVersion, agent.Architecture,
		agent.NumCPU, agent.TotalMemoryMB, agent.BuildType, agent.GitCommit,
		agent.DeviceCount, agent.LastSeen, agent.LastHeartbeat, agent.Status,
		agent.AgentID)

	return err
}

// UpdateAgentName updates the user-friendly name for an agent
func (s *BaseStore) UpdateAgentName(ctx context.Context, agentID string, name string) error {
	_, err := s.execContext(ctx, `UPDATE agents SET name = ? WHERE agent_id = ?`, name, agentID)
	return err
}

// DeleteAgent removes an agent by its ID
func (s *BaseStore) DeleteAgent(ctx context.Context, agentID string) error {
	_, err := s.execContext(ctx, `DELETE FROM agents WHERE agent_id = ?`, agentID)
	return err
}

// ============================================================================
// Device Management Methods
// ============================================================================

// UpsertDevice creates or updates a device
func (s *BaseStore) UpsertDevice(ctx context.Context, device *Device) error {
	// Serialize JSON fields
	consumablesJSON, _ := json.Marshal(device.Consumables)
	statusJSON, _ := json.Marshal(device.StatusMessages)
	rawDataJSON, _ := json.Marshal(device.RawData)

	now := time.Now().UTC()
	if device.LastSeen.IsZero() {
		device.LastSeen = now
	}
	if device.FirstSeen.IsZero() {
		device.FirstSeen = now
	}
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}

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

	_, err := s.execContext(ctx, query,
		device.Serial, device.AgentID, device.IP, device.Manufacturer,
		device.Model, device.Hostname, device.Firmware, device.MACAddress,
		device.SubnetMask, device.Gateway, string(consumablesJSON),
		string(statusJSON), device.LastSeen, device.FirstSeen,
		device.CreatedAt, device.DiscoveryMethod, device.AssetNumber,
		device.Location, device.Description, device.WebUIURL,
		string(rawDataJSON))

	return err
}

// GetDevice retrieves a device by serial number
func (s *BaseStore) GetDevice(ctx context.Context, serial string) (*Device, error) {
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

	err := s.queryRowContext(ctx, query, serial).Scan(
		&device.Serial, &device.AgentID, &device.IP, &device.Manufacturer,
		&device.Model, &device.Hostname, &device.Firmware, &device.MACAddress,
		&device.SubnetMask, &device.Gateway, &consumablesJSON, &statusJSON,
		&device.LastSeen, &device.FirstSeen, &device.CreatedAt,
		&device.DiscoveryMethod, &device.AssetNumber, &device.Location,
		&device.Description, &device.WebUIURL, &rawDataJSON)

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
func (s *BaseStore) ListDevices(ctx context.Context, agentID string) ([]*Device, error) {
	query := `
		SELECT serial, agent_id, ip, manufacturer, model, hostname, firmware,
		       mac_address, subnet_mask, gateway, consumables, status_messages,
		       last_seen, first_seen, created_at, discovery_method,
		       asset_number, location, description, web_ui_url, raw_data
		FROM devices
		WHERE agent_id = ?
		ORDER BY last_seen DESC
	`

	rows, err := s.queryContext(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDevices(rows)
}

// ListAllDevices returns all devices across all agents
func (s *BaseStore) ListAllDevices(ctx context.Context) ([]*Device, error) {
	query := `
		SELECT serial, agent_id, ip, manufacturer, model, hostname, firmware,
		       mac_address, subnet_mask, gateway, consumables, status_messages,
		       last_seen, first_seen, created_at, discovery_method,
		       asset_number, location, description, web_ui_url, raw_data
		FROM devices
		ORDER BY last_seen DESC
	`

	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDevices(rows)
}

// scanDevices scans multiple device rows
func (s *BaseStore) scanDevices(rows *sql.Rows) ([]*Device, error) {
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
			&device.Description, &device.WebUIURL, &rawDataJSON)
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

// ============================================================================
// Metrics Methods
// ============================================================================

// SaveMetrics saves a metrics snapshot
func (s *BaseStore) SaveMetrics(ctx context.Context, metrics *MetricsSnapshot) error {
	tonerJSON, _ := json.Marshal(metrics.TonerLevels)

	query := `
		INSERT INTO metrics_history (serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.execContext(ctx, query,
		metrics.Serial, metrics.AgentID, metrics.Timestamp,
		metrics.PageCount, metrics.ColorPages, metrics.MonoPages,
		metrics.ScanCount, string(tonerJSON))

	return err
}

// GetLatestMetrics retrieves the most recent metrics for a device
func (s *BaseStore) GetLatestMetrics(ctx context.Context, serial string) (*MetricsSnapshot, error) {
	query := `
		SELECT id, serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels
		FROM metrics_history
		WHERE serial = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var m MetricsSnapshot
	var tonerJSON sql.NullString

	err := s.queryRowContext(ctx, query, serial).Scan(
		&m.ID, &m.Serial, &m.AgentID, &m.Timestamp,
		&m.PageCount, &m.ColorPages, &m.MonoPages, &m.ScanCount, &tonerJSON)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if tonerJSON.Valid {
		json.Unmarshal([]byte(tonerJSON.String), &m.TonerLevels)
	}

	return &m, nil
}

// GetMetricsHistory retrieves metrics history for a device since a given time
func (s *BaseStore) GetMetricsHistory(ctx context.Context, serial string, since time.Time) ([]*MetricsSnapshot, error) {
	query := `
		SELECT id, serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels
		FROM metrics_history
		WHERE serial = ? AND timestamp >= ?
		ORDER BY timestamp ASC
	`

	rows, err := s.queryContext(ctx, query, serial, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metrics []*MetricsSnapshot
	for rows.Next() {
		var m MetricsSnapshot
		var tonerJSON sql.NullString

		err := rows.Scan(&m.ID, &m.Serial, &m.AgentID, &m.Timestamp,
			&m.PageCount, &m.ColorPages, &m.MonoPages, &m.ScanCount, &tonerJSON)
		if err != nil {
			return nil, err
		}

		if tonerJSON.Valid {
			json.Unmarshal([]byte(tonerJSON.String), &m.TonerLevels)
		}

		metrics = append(metrics, &m)
	}

	return metrics, rows.Err()
}

// ============================================================================
// Tenant Management Methods
// ============================================================================

// CreateTenant creates a new tenant
func (s *BaseStore) CreateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant == nil {
		return fmt.Errorf("tenant required")
	}
	if tenant.ID == "" {
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

	query := `
		INSERT INTO tenants (
			id, name, description, contact_name, contact_email, contact_phone,
			business_unit, billing_code, address, login_domain, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.execContext(ctx, query,
		tenant.ID, tenant.Name, tenant.Description, tenant.ContactName, tenant.ContactEmail,
		tenant.ContactPhone, tenant.BusinessUnit, tenant.BillingCode, tenant.Address,
		nullString(tenant.LoginDomain), tenant.CreatedAt)
	return err
}

// UpdateTenant updates an existing tenant
func (s *BaseStore) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	if tenant == nil {
		return fmt.Errorf("tenant required")
	}
	if tenant.ID == "" {
		return fmt.Errorf("tenant id required")
	}

	tenant.LoginDomain = NormalizeTenantDomain(tenant.LoginDomain)

	query := `
		UPDATE tenants SET
			name = ?,
			description = ?,
			contact_name = ?,
			contact_email = ?,
			contact_phone = ?,
			business_unit = ?,
			billing_code = ?,
			address = ?,
			login_domain = ?
		WHERE id = ?
	`

	res, err := s.execContext(ctx, query,
		tenant.Name, tenant.Description, tenant.ContactName, tenant.ContactEmail, tenant.ContactPhone,
		tenant.BusinessUnit, tenant.BillingCode, tenant.Address, nullString(tenant.LoginDomain), tenant.ID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetTenant retrieves a tenant by ID
func (s *BaseStore) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	query := `
		SELECT id, name, description, contact_name, contact_email, contact_phone,
		       business_unit, billing_code, address, login_domain, created_at
		FROM tenants WHERE id = ?
	`

	var t Tenant
	var loginDomain sql.NullString
	err := s.queryRowContext(ctx, query, id).Scan(
		&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail,
		&t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address,
		&loginDomain, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	t.LoginDomain = loginDomain.String
	return &t, nil
}

// ListTenants returns all tenants
func (s *BaseStore) ListTenants(ctx context.Context) ([]*Tenant, error) {
	query := `
		SELECT id, name, description, contact_name, contact_email, contact_phone,
		       business_unit, billing_code, address, login_domain, created_at
		FROM tenants ORDER BY created_at DESC
	`

	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tenants []*Tenant
	for rows.Next() {
		var t Tenant
		var loginDomain sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail,
			&t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address,
			&loginDomain, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.LoginDomain = loginDomain.String
		tenants = append(tenants, &t)
	}
	return tenants, nil
}

// FindTenantByDomain finds a tenant by its login domain
func (s *BaseStore) FindTenantByDomain(ctx context.Context, domain string) (*Tenant, error) {
	norm := NormalizeTenantDomain(domain)
	if norm == "" {
		return nil, sql.ErrNoRows
	}

	query := `
		SELECT id, name, description, contact_name, contact_email, contact_phone,
		       business_unit, billing_code, address, login_domain, created_at
		FROM tenants WHERE login_domain = ?
	`

	var t Tenant
	var loginDomain sql.NullString
	err := s.queryRowContext(ctx, query, norm).Scan(
		&t.ID, &t.Name, &t.Description, &t.ContactName, &t.ContactEmail,
		&t.ContactPhone, &t.BusinessUnit, &t.BillingCode, &t.Address,
		&loginDomain, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	t.LoginDomain = loginDomain.String
	return &t, nil
}

// ============================================================================
// Site Management Methods
// ============================================================================

// CreateSite creates a new site within a tenant
func (s *BaseStore) CreateSite(ctx context.Context, site *Site) error {
	if site == nil {
		return fmt.Errorf("site required")
	}
	if site.TenantID == "" {
		return fmt.Errorf("tenant_id required")
	}
	if site.Name == "" {
		return fmt.Errorf("name required")
	}
	if site.ID == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		site.ID = hex.EncodeToString(b)
	}
	if site.CreatedAt.IsZero() {
		site.CreatedAt = time.Now().UTC()
	}

	var rulesJSON []byte
	if len(site.FilterRules) > 0 {
		var err error
		rulesJSON, err = json.Marshal(site.FilterRules)
		if err != nil {
			return fmt.Errorf("failed to marshal filter rules: %w", err)
		}
	}

	query := `INSERT INTO sites (id, tenant_id, name, description, address, filter_rules, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.execContext(ctx, query, site.ID, site.TenantID, site.Name, site.Description, site.Address, nullBytes(rulesJSON), site.CreatedAt)
	return err
}

// UpdateSite updates an existing site
func (s *BaseStore) UpdateSite(ctx context.Context, site *Site) error {
	if site == nil {
		return fmt.Errorf("site required")
	}
	if site.ID == "" {
		return fmt.Errorf("site id required")
	}

	var rulesJSON []byte
	if len(site.FilterRules) > 0 {
		var err error
		rulesJSON, err = json.Marshal(site.FilterRules)
		if err != nil {
			return fmt.Errorf("failed to marshal filter rules: %w", err)
		}
	}

	res, err := s.execContext(ctx, `UPDATE sites SET name = ?, description = ?, address = ?, filter_rules = ? WHERE id = ?`,
		site.Name, site.Description, site.Address, nullBytes(rulesJSON), site.ID)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSite retrieves a site by ID
func (s *BaseStore) GetSite(ctx context.Context, id string) (*Site, error) {
	query := `SELECT id, tenant_id, name, description, address, filter_rules, created_at FROM sites WHERE id = ?`
	row := s.queryRowContext(ctx, query, id)

	var site Site
	var rulesJSON sql.NullString
	err := row.Scan(&site.ID, &site.TenantID, &site.Name, &site.Description, &site.Address, &rulesJSON, &site.CreatedAt)
	if err != nil {
		return nil, err
	}
	if rulesJSON.Valid && rulesJSON.String != "" {
		json.Unmarshal([]byte(rulesJSON.String), &site.FilterRules)
	}
	return &site, nil
}

// ListSitesByTenant returns all sites for a tenant
func (s *BaseStore) ListSitesByTenant(ctx context.Context, tenantID string) ([]*Site, error) {
	query := `SELECT id, tenant_id, name, description, address, filter_rules, created_at FROM sites WHERE tenant_id = ? ORDER BY name`
	rows, err := s.queryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []*Site
	for rows.Next() {
		var site Site
		var rulesJSON sql.NullString
		if err := rows.Scan(&site.ID, &site.TenantID, &site.Name, &site.Description, &site.Address, &rulesJSON, &site.CreatedAt); err != nil {
			return nil, err
		}
		if rulesJSON.Valid && rulesJSON.String != "" {
			json.Unmarshal([]byte(rulesJSON.String), &site.FilterRules)
		}
		sites = append(sites, &site)
	}
	return sites, rows.Err()
}

// DeleteSite removes a site
func (s *BaseStore) DeleteSite(ctx context.Context, id string) error {
	res, err := s.execContext(ctx, `DELETE FROM sites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AssignAgentToSite assigns an agent to a site
func (s *BaseStore) AssignAgentToSite(ctx context.Context, agentID, siteID string) error {
	query := `INSERT INTO agent_sites (agent_id, site_id, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`
	_, err := s.execContext(ctx, query, agentID, siteID, time.Now().UTC())
	return err
}

// UnassignAgentFromSite removes an agent from a site
func (s *BaseStore) UnassignAgentFromSite(ctx context.Context, agentID, siteID string) error {
	_, err := s.execContext(ctx, `DELETE FROM agent_sites WHERE agent_id = ? AND site_id = ?`, agentID, siteID)
	return err
}

// GetAgentSiteIDs returns the site IDs for an agent
func (s *BaseStore) GetAgentSiteIDs(ctx context.Context, agentID string) ([]string, error) {
	rows, err := s.queryContext(ctx, `SELECT site_id FROM agent_sites WHERE agent_id = ?`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetSiteAgentIDs returns the agent IDs for a site
func (s *BaseStore) GetSiteAgentIDs(ctx context.Context, siteID string) ([]string, error) {
	rows, err := s.queryContext(ctx, `SELECT agent_id FROM agent_sites WHERE site_id = ?`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetAgentSites replaces all site assignments for an agent
func (s *BaseStore) SetAgentSites(ctx context.Context, agentID string, siteIDs []string) error {
	// Delete existing assignments
	if _, err := s.execContext(ctx, `DELETE FROM agent_sites WHERE agent_id = ?`, agentID); err != nil {
		return err
	}

	// Insert new assignments
	now := time.Now().UTC()
	for _, siteID := range siteIDs {
		if _, err := s.execContext(ctx, `INSERT INTO agent_sites (agent_id, site_id, created_at) VALUES (?, ?, ?)`,
			agentID, siteID, now); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// User Management Methods
// ============================================================================

// CreateUser creates a new user with hashed password
func (s *BaseStore) CreateUser(ctx context.Context, user *User, rawPassword string) error {
	if user == nil {
		return fmt.Errorf("user required")
	}
	if user.Username == "" {
		return fmt.Errorf("username required")
	}
	if rawPassword == "" {
		return fmt.Errorf("password required")
	}

	hash, err := hashArgon(rawPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	if user.Role == "" {
		user.Role = RoleViewer
	}

	query := `INSERT INTO users (username, password_hash, role, tenant_id, email, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	res, err := s.execContext(ctx, query, user.Username, hash, string(user.Role), nullString(user.TenantID), nullString(user.Email), user.CreatedAt)
	if err != nil {
		return err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = id

	// Handle multi-tenant assignment
	if len(user.TenantIDs) > 0 {
		for _, tid := range SortTenantIDs(user.TenantIDs) {
			if _, err := s.execContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES (?, ?)`, user.ID, tid); err != nil {
				logWarn("Failed to insert user_tenants row", "user_id", user.ID, "tenant_id", tid, "error", err)
			}
		}
	}

	return nil
}

// GetUserByUsername retrieves a user by username
func (s *BaseStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE username = ?`

	var u User
	var tenantID, email sql.NullString
	err := s.queryRowContext(ctx, query, username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenantID, &email, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.TenantID = tenantID.String
	u.Email = email.String
	u.Role = NormalizeRole(string(u.Role))

	// Load multi-tenant assignments
	u.TenantIDs, _ = s.loadUserTenantIDs(ctx, u.ID)

	return &u, nil
}

// GetUserByID retrieves a user by ID
func (s *BaseStore) GetUserByID(ctx context.Context, id int64) (*User, error) {
	query := `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE id = ?`

	var u User
	var tenantID, email sql.NullString
	err := s.queryRowContext(ctx, query, id).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenantID, &email, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.TenantID = tenantID.String
	u.Email = email.String
	u.Role = NormalizeRole(string(u.Role))
	u.TenantIDs, _ = s.loadUserTenantIDs(ctx, u.ID)

	return &u, nil
}

// GetUserByEmail retrieves a user by email
func (s *BaseStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `SELECT id, username, password_hash, role, tenant_id, email, created_at FROM users WHERE email = ?`

	var u User
	var tenantID, emailVal sql.NullString
	err := s.queryRowContext(ctx, query, email).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &tenantID, &emailVal, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.TenantID = tenantID.String
	u.Email = emailVal.String
	u.Role = NormalizeRole(string(u.Role))
	u.TenantIDs, _ = s.loadUserTenantIDs(ctx, u.ID)

	return &u, nil
}

// ListUsers returns all users
func (s *BaseStore) ListUsers(ctx context.Context) ([]*User, error) {
	query := `SELECT id, username, role, tenant_id, email, created_at FROM users ORDER BY created_at DESC`

	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var tenantID, email sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &tenantID, &email, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.TenantID = tenantID.String
		u.Email = email.String
		u.Role = NormalizeRole(string(u.Role))
		u.TenantIDs, _ = s.loadUserTenantIDs(ctx, u.ID)
		users = append(users, &u)
	}
	return users, rows.Err()
}

// UpdateUser updates a user's profile
func (s *BaseStore) UpdateUser(ctx context.Context, user *User) error {
	if user == nil {
		return fmt.Errorf("user required")
	}

	query := `UPDATE users SET username = ?, role = ?, tenant_id = ?, email = ? WHERE id = ?`
	_, err := s.execContext(ctx, query, user.Username, string(user.Role), nullString(user.TenantID), nullString(user.Email), user.ID)
	if err != nil {
		return err
	}

	// Update tenant assignments
	s.execContext(ctx, `DELETE FROM user_tenants WHERE user_id = ?`, user.ID)
	for _, tid := range SortTenantIDs(user.TenantIDs) {
		s.execContext(ctx, `INSERT INTO user_tenants (user_id, tenant_id) VALUES (?, ?)`, user.ID, tid)
	}

	return nil
}

// DeleteUser removes a user
func (s *BaseStore) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.execContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// UpdateUserPassword updates a user's password
func (s *BaseStore) UpdateUserPassword(ctx context.Context, userID int64, rawPassword string) error {
	hash, err := hashArgon(rawPassword)
	if err != nil {
		return err
	}
	_, err = s.execContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, userID)
	return err
}

// AuthenticateUser verifies username/password and returns the user if valid
func (s *BaseStore) AuthenticateUser(ctx context.Context, username, rawPassword string) (*User, error) {
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
	u.PasswordHash = "" // Don't return hash
	return u, nil
}

// loadUserTenantIDs loads multi-tenant assignments for a user
func (s *BaseStore) loadUserTenantIDs(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.queryContext(ctx, `SELECT tenant_id FROM user_tenants WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return SortTenantIDs(ids), rows.Err()
}

// ============================================================================
// Session Management Methods
// ============================================================================

// CreateSession creates a new session token
func (s *BaseStore) CreateSession(ctx context.Context, userID int64, ttlMinutes int) (*Session, error) {
	rawToken := generateSecureToken(32)
	tokenHash := hashSHA256(rawToken)

	expiresAt := time.Now().UTC().Add(time.Duration(ttlMinutes) * time.Minute)
	createdAt := time.Now().UTC()

	query := `INSERT INTO sessions (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`
	_, err := s.execContext(ctx, query, tokenHash, userID, expiresAt, createdAt)
	if err != nil {
		return nil, err
	}

	return &Session{
		Token:     rawToken,
		UserID:    userID,
		ExpiresAt: expiresAt,
		CreatedAt: createdAt,
	}, nil
}

// GetSessionByToken retrieves a session by raw token
func (s *BaseStore) GetSessionByToken(ctx context.Context, token string) (*Session, error) {
	tokenHash := hashSHA256(token)

	query := `
		SELECT s.token, s.user_id, s.expires_at, s.created_at, u.username
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token = ? AND s.expires_at > ?
	`

	var sess Session
	var username sql.NullString
	err := s.queryRowContext(ctx, query, tokenHash, time.Now().UTC()).Scan(
		&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt, &username)
	if err != nil {
		return nil, err
	}
	sess.Username = username.String
	return &sess, nil
}

// DeleteSession removes a session by raw token
func (s *BaseStore) DeleteSession(ctx context.Context, token string) error {
	tokenHash := hashSHA256(token)
	_, err := s.execContext(ctx, `DELETE FROM sessions WHERE token = ?`, tokenHash)
	return err
}

// DeleteSessionByHash removes a session by its stored hash
func (s *BaseStore) DeleteSessionByHash(ctx context.Context, tokenHash string) error {
	_, err := s.execContext(ctx, `DELETE FROM sessions WHERE token = ?`, tokenHash)
	return err
}

// ListSessions returns all sessions
func (s *BaseStore) ListSessions(ctx context.Context) ([]*Session, error) {
	query := `
		SELECT s.token, s.user_id, s.expires_at, s.created_at, u.username
		FROM sessions s
		LEFT JOIN users u ON s.user_id = u.id
		ORDER BY s.created_at DESC
	`

	rows, err := s.queryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var sess Session
		var username sql.NullString
		if err := rows.Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt, &sess.CreatedAt, &username); err != nil {
			return nil, err
		}
		sess.Username = username.String
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

// ============================================================================
// Audit Log Methods
// ============================================================================

// SaveAuditEntry saves an audit log entry
func (s *BaseStore) SaveAuditEntry(ctx context.Context, entry *AuditEntry) error {
	if entry == nil {
		return fmt.Errorf("entry required")
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.Severity == "" {
		entry.Severity = AuditSeverityInfo
	}

	metadataJSON, _ := json.Marshal(entry.Metadata)

	query := `
		INSERT INTO audit_log (
			timestamp, actor_type, actor_id, actor_name, action, target_type, target_id,
			tenant_id, severity, details, metadata, ip_address, user_agent, request_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	res, err := s.execContext(ctx, query,
		entry.Timestamp, entry.ActorType, entry.ActorID, entry.ActorName, entry.Action,
		entry.TargetType, entry.TargetID, entry.TenantID, entry.Severity, entry.Details,
		string(metadataJSON), entry.IPAddress, entry.UserAgent, entry.RequestID)
	if err != nil {
		return err
	}

	id, _ := res.LastInsertId()
	entry.ID = id
	return nil
}

// GetAuditLog retrieves audit entries for an actor since a given time
func (s *BaseStore) GetAuditLog(ctx context.Context, actorID string, since time.Time) ([]*AuditEntry, error) {
	query := `
		SELECT id, timestamp, actor_type, actor_id, actor_name, action, target_type, target_id,
		       tenant_id, severity, details, metadata, ip_address, user_agent, request_id
		FROM audit_log
		WHERE actor_id = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`

	rows, err := s.queryContext(ctx, query, actorID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		var e AuditEntry
		var metadataJSON sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.ActorType, &e.ActorID, &e.ActorName, &e.Action,
			&e.TargetType, &e.TargetID, &e.TenantID, &e.Severity, &e.Details, &metadataJSON,
			&e.IPAddress, &e.UserAgent, &e.RequestID); err != nil {
			return nil, err
		}
		if metadataJSON.Valid {
			json.Unmarshal([]byte(metadataJSON.String), &e.Metadata)
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}
