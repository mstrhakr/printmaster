-- PrintMaster Agent E2E Test Seed Data
-- This SQL creates a pre-populated agent database for E2E testing.
--
-- Compatible with agent schema v7+
-- Run with: sqlite3 testdata/agent/agent.db < seed/agent_seed.sql

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- ============================================================================
-- SCHEMA (minimal - agent will auto-migrate, but we need base tables)
-- ============================================================================

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO schema_version (version) VALUES (7);

-- Devices (local discovery cache)
CREATE TABLE IF NOT EXISTS devices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial_number TEXT NOT NULL UNIQUE,
    ip_address TEXT,
    mac_address TEXT,
    hostname TEXT,
    model TEXT,
    vendor TEXT,
    device_type TEXT DEFAULT 'printer',
    firmware_version TEXT,
    web_ui_url TEXT,
    location TEXT,
    subnet_mask TEXT,
    gateway TEXT,
    snmp_engine_id TEXT,
    status TEXT DEFAULT 'unknown',
    last_seen TEXT,
    first_seen TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Device Metrics (local time-series cache)
CREATE TABLE IF NOT EXISTS device_metrics (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial_number TEXT NOT NULL,
    timestamp TEXT NOT NULL DEFAULT (datetime('now')),
    page_count INTEGER,
    toner_black INTEGER,
    toner_cyan INTEGER,
    toner_magenta INTEGER,
    toner_yellow INTEGER,
    drum_level INTEGER,
    waste_toner INTEGER,
    fuser_level INTEGER,
    status TEXT,
    status_message TEXT,
    raw_data TEXT,  -- JSON blob for full SNMP response
    FOREIGN KEY (serial_number) REFERENCES devices(serial_number) ON DELETE CASCADE
);

-- Scan History
CREATE TABLE IF NOT EXISTS scan_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    subnet TEXT,
    devices_found INTEGER DEFAULT 0,
    devices_new INTEGER DEFAULT 0,
    devices_updated INTEGER DEFAULT 0,
    status TEXT DEFAULT 'running',
    error_message TEXT
);

-- Scanner Configuration
CREATE TABLE IF NOT EXISTS scanner_config (
    id INTEGER PRIMARY KEY,
    subnets TEXT,  -- JSON array of subnets to scan
    snmp_community TEXT DEFAULT 'public',
    snmp_timeout_ms INTEGER DEFAULT 2000,
    snmp_retries INTEGER DEFAULT 1,
    scan_interval_minutes INTEGER DEFAULT 60,
    enabled INTEGER DEFAULT 1,
    last_modified TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Settings (key-value store)
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip_address);
CREATE INDEX IF NOT EXISTS idx_devices_serial ON devices(serial_number);
CREATE INDEX IF NOT EXISTS idx_metrics_serial ON device_metrics(serial_number);
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON device_metrics(timestamp);

-- ============================================================================
-- SEED DATA
-- ============================================================================

-- Test devices (matching server seed data)
INSERT INTO devices (serial_number, ip_address, mac_address, model, vendor, status, last_seen, first_seen) VALUES
    ('HP-E2E-001', '192.168.100.10', 'AA:BB:CC:DD:EE:01', 'LaserJet Pro M404dn', 'hp', 'online', datetime('now'), datetime('now', '-30 days')),
    ('KYOCERA-E2E-002', '192.168.100.11', 'AA:BB:CC:DD:EE:02', 'ECOSYS M3655idn', 'kyocera', 'online', datetime('now'), datetime('now', '-30 days')),
    ('BROTHER-E2E-003', '192.168.100.12', 'AA:BB:CC:DD:EE:03', 'MFC-L8900CDW', 'brother', 'online', datetime('now'), datetime('now', '-30 days')),
    ('LEXMARK-E2E-004', '192.168.100.13', 'AA:BB:CC:DD:EE:04', 'MS621dn', 'lexmark', 'warning', datetime('now'), datetime('now', '-30 days')),
    ('XEROX-E2E-005', '192.168.100.14', 'AA:BB:CC:DD:EE:05', 'VersaLink C405', 'xerox', 'error', datetime('now'), datetime('now', '-30 days'));

-- Sample metrics (matching server)
INSERT INTO device_metrics (serial_number, timestamp, page_count, toner_black, status) VALUES
    ('HP-E2E-001', datetime('now', '-24 hours'), 10000, 85, 'online'),
    ('HP-E2E-001', datetime('now', '-12 hours'), 10050, 84, 'online'),
    ('HP-E2E-001', datetime('now', '-6 hours'), 10080, 83, 'online'),
    ('HP-E2E-001', datetime('now'), 10100, 82, 'online');

INSERT INTO device_metrics (serial_number, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, status) VALUES
    ('KYOCERA-E2E-002', datetime('now', '-24 hours'), 25000, 70, 65, 68, 62, 'online'),
    ('KYOCERA-E2E-002', datetime('now'), 25200, 68, 63, 66, 60, 'online');

INSERT INTO device_metrics (serial_number, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, drum_level, status) VALUES
    ('BROTHER-E2E-003', datetime('now', '-24 hours'), 5000, 45, 50, 48, 55, 80, 'online'),
    ('BROTHER-E2E-003', datetime('now'), 5100, 43, 48, 46, 53, 79, 'online');

INSERT INTO device_metrics (serial_number, timestamp, page_count, toner_black, status) VALUES
    ('LEXMARK-E2E-004', datetime('now'), 15000, 12, 'warning');

INSERT INTO device_metrics (serial_number, timestamp, page_count, toner_black, toner_cyan, toner_magenta, toner_yellow, status) VALUES
    ('XEROX-E2E-005', datetime('now'), 8000, 5, 8, 3, 10, 'error');

-- Scanner configuration (discovery disabled for E2E)
INSERT INTO scanner_config (id, subnets, enabled) VALUES
    (1, '["192.168.100.0/24"]', 0);

-- Scan history (one completed scan)
INSERT INTO scan_history (started_at, completed_at, subnet, devices_found, devices_new, status) VALUES
    (datetime('now', '-1 hour'), datetime('now', '-55 minutes'), '192.168.100.0/24', 5, 5, 'completed');

-- Settings
INSERT INTO settings (key, value) VALUES
    ('server_url', 'http://server:8443'),
    ('server_enabled', 'true'),
    ('last_upload', datetime('now', '-5 minutes'));

-- Done
SELECT 'Agent seed data loaded successfully' AS status;
