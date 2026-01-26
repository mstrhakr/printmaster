-- PrintMaster Server E2E Test Seed Data
-- This SQL creates a pre-populated server database for E2E testing.
--
-- Compatible with server schema v10+
-- Run with: sqlite3 testdata/server/server.db < seed/server_seed.sql

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- ============================================================================
-- SCHEMA (minimal - server will auto-migrate, but we need base tables)
-- ============================================================================

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO schema_version (version) VALUES (10);

-- Tenants
CREATE TABLE IF NOT EXISTS tenants (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    api_key TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Users (for web UI authentication)
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'user',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, username)
);

-- Agents
CREATE TABLE IF NOT EXISTS agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,  -- UUID from agent
    name TEXT NOT NULL,
    hostname TEXT,
    platform TEXT,
    version TEXT,
    protocol_version TEXT,
    last_heartbeat TEXT,
    online INTEGER NOT NULL DEFAULT 0,
    websocket_connected INTEGER NOT NULL DEFAULT 0,
    ip_address TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, agent_id)
);

-- Devices
CREATE TABLE IF NOT EXISTS devices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL,
    serial_number TEXT NOT NULL,
    ip_address TEXT,
    mac_address TEXT,
    hostname TEXT,
    model TEXT,
    vendor TEXT,
    device_type TEXT DEFAULT 'printer',
    firmware_version TEXT,
    web_ui_url TEXT,
    location TEXT,
    status TEXT DEFAULT 'unknown',
    last_seen TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, serial_number)
);

-- Device Metrics (time-series)
CREATE TABLE IF NOT EXISTS device_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp TEXT NOT NULL DEFAULT (datetime('now')),
    page_count INTEGER,
    toner_black INTEGER,
    toner_cyan INTEGER,
    toner_magenta INTEGER,
    toner_yellow INTEGER,
    drum_level INTEGER,
    status TEXT,
    raw_data TEXT  -- JSON blob for additional metrics
);

-- Create indexes for common queries
CREATE INDEX IF NOT EXISTS idx_devices_tenant ON devices(tenant_id);
CREATE INDEX IF NOT EXISTS idx_devices_agent ON devices(agent_id);
CREATE INDEX IF NOT EXISTS idx_devices_serial ON devices(serial_number);
CREATE INDEX IF NOT EXISTS idx_metrics_device ON device_metrics(device_id);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON device_metrics(timestamp);
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_agents_uuid ON agents(agent_id);

-- ============================================================================
-- SEED DATA
-- ============================================================================

-- Default tenant for E2E tests
INSERT INTO tenants (id, name, api_key) VALUES 
    (1, 'E2E Test Tenant', 'e2e-test-api-key-00000000');

-- Admin user (password: e2e-test-password, bcrypt hash)
-- Generated with: htpasswd -nbBC 10 admin e2e-test-password
INSERT INTO users (tenant_id, username, password_hash, role) VALUES
    (1, 'admin', '$2a$10$E2ETestHashForAdminPasswordE2ETestHashFor', 'admin');

-- Pre-registered test agent
INSERT INTO agents (tenant_id, agent_id, name, hostname, platform, version, protocol_version, online, websocket_connected) VALUES
    (1, 'e2e00000-0000-0000-0000-000000000001', 'e2e-test-agent', 'agent', 'linux', 'e2e-test', '1', 0, 0);

-- Test devices (various vendors for coverage)
INSERT INTO devices (tenant_id, agent_id, serial_number, ip_address, mac_address, model, vendor, status, last_seen) VALUES
    (1, 'e2e00000-0000-0000-0000-000000000001', 'HP-E2E-001', '192.168.100.10', 'AA:BB:CC:DD:EE:01', 'LaserJet Pro M404dn', 'hp', 'online', datetime('now')),
    (1, 'e2e00000-0000-0000-0000-000000000001', 'KYOCERA-E2E-002', '192.168.100.11', 'AA:BB:CC:DD:EE:02', 'ECOSYS M3655idn', 'kyocera', 'online', datetime('now')),
    (1, 'e2e00000-0000-0000-0000-000000000001', 'BROTHER-E2E-003', '192.168.100.12', 'AA:BB:CC:DD:EE:03', 'MFC-L8900CDW', 'brother', 'online', datetime('now')),
    (1, 'e2e00000-0000-0000-0000-000000000001', 'LEXMARK-E2E-004', '192.168.100.13', 'AA:BB:CC:DD:EE:04', 'MS621dn', 'lexmark', 'warning', datetime('now')),
    (1, 'e2e00000-0000-0000-0000-000000000001', 'XEROX-E2E-005', '192.168.100.14', 'AA:BB:CC:DD:EE:05', 'VersaLink C405', 'xerox', 'error', datetime('now'));

-- Sample metrics for HP device (last 24 hours, hourly)
INSERT INTO device_metrics (device_id, timestamp, page_count, toner_black, status) VALUES
    (1, datetime('now', '-24 hours'), 10000, 85, 'online'),
    (1, datetime('now', '-12 hours'), 10050, 84, 'online'),
    (1, datetime('now', '-6 hours'), 10080, 83, 'online'),
    (1, datetime('now'), 10100, 82, 'online');

-- Sample metrics for Kyocera device (color printer)
INSERT INTO device_metrics (device_id, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, status) VALUES
    (2, datetime('now', '-24 hours'), 25000, 70, 65, 68, 62, 'online'),
    (2, datetime('now'), 25200, 68, 63, 66, 60, 'online');

-- Sample metrics for Brother device
INSERT INTO device_metrics (device_id, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, drum_level, status) VALUES
    (3, datetime('now', '-24 hours'), 5000, 45, 50, 48, 55, 80, 'online'),
    (3, datetime('now'), 5100, 43, 48, 46, 53, 79, 'online');

-- Lexmark with low toner warning
INSERT INTO device_metrics (device_id, timestamp, page_count, toner_black, status) VALUES
    (4, datetime('now'), 15000, 12, 'warning');

-- Xerox with error state
INSERT INTO device_metrics (device_id, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, status) VALUES
    (5, datetime('now'), 8000, 5, 8, 3, 10, 'error');

-- Done
SELECT 'Server seed data loaded successfully' AS status;
