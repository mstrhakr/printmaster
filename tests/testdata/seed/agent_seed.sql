-- PrintMaster Agent E2E Test Seed Data
-- This SQL creates a pre-populated agent database for E2E testing.
--
-- Compatible with agent schema (matches agent/storage/sqlite.go initSchema)
-- Run with: sqlite3 testdata/agent/agent.db < seed/agent_seed.sql

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- ============================================================================
-- SCHEMA (must match agent/storage/sqlite.go initSchema exactly)
-- ============================================================================

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL
);
INSERT INTO schema_version (version, applied_at) VALUES (7, datetime('now'));

-- Devices table (matches actual agent schema)
CREATE TABLE IF NOT EXISTS devices (
    serial TEXT PRIMARY KEY,
    ip TEXT NOT NULL,
    manufacturer TEXT,
    model TEXT,
    hostname TEXT,
    firmware TEXT,
    mac_address TEXT,
    subnet_mask TEXT,
    gateway TEXT,
    dns_servers TEXT,
    dhcp_server TEXT,
    page_count INTEGER DEFAULT 0,
    toner_levels TEXT,
    consumables TEXT,
    status_messages TEXT,
    last_seen DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    first_seen DATETIME NOT NULL,
    is_saved BOOLEAN DEFAULT 0,
    visible BOOLEAN DEFAULT 1,
    discovery_method TEXT,
    walk_filename TEXT,
    last_scan_id INTEGER,
    raw_data TEXT
);

CREATE INDEX IF NOT EXISTS idx_devices_is_saved ON devices(is_saved);
CREATE INDEX IF NOT EXISTS idx_devices_visible ON devices(visible);
CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);
CREATE INDEX IF NOT EXISTS idx_devices_manufacturer ON devices(manufacturer);

-- Scan history for device changes
CREATE TABLE IF NOT EXISTS scan_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ip TEXT NOT NULL,
    hostname TEXT,
    firmware TEXT,
    consumables TEXT,
    status_messages TEXT,
    discovery_method TEXT,
    walk_filename TEXT,
    raw_data TEXT,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_scan_history_serial ON scan_history(serial);
CREATE INDEX IF NOT EXISTS idx_scan_history_created ON scan_history(created_at);

-- Raw metrics: high-resolution 5-minute data
CREATE TABLE IF NOT EXISTS metrics_raw (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    timestamp DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    page_count INTEGER DEFAULT 0,
    color_pages INTEGER DEFAULT 0,
    mono_pages INTEGER DEFAULT 0,
    scan_count INTEGER DEFAULT 0,
    toner_levels TEXT,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE
);

-- Hourly aggregates
CREATE TABLE IF NOT EXISTS metrics_hourly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    hour_start DATETIME NOT NULL,
    sample_count INTEGER DEFAULT 0,
    page_count_min INTEGER DEFAULT 0,
    page_count_max INTEGER DEFAULT 0,
    page_count_avg INTEGER DEFAULT 0,
    color_pages_min INTEGER DEFAULT 0,
    color_pages_max INTEGER DEFAULT 0,
    color_pages_avg INTEGER DEFAULT 0,
    mono_pages_min INTEGER DEFAULT 0,
    mono_pages_max INTEGER DEFAULT 0,
    mono_pages_avg INTEGER DEFAULT 0,
    scan_count_min INTEGER DEFAULT 0,
    scan_count_max INTEGER DEFAULT 0,
    scan_count_avg INTEGER DEFAULT 0,
    toner_levels_avg TEXT,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
    UNIQUE(serial, hour_start)
);

-- Daily aggregates
CREATE TABLE IF NOT EXISTS metrics_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    serial TEXT NOT NULL,
    day_start DATETIME NOT NULL,
    sample_count INTEGER DEFAULT 0,
    page_count_min INTEGER DEFAULT 0,
    page_count_max INTEGER DEFAULT 0,
    page_count_avg INTEGER DEFAULT 0,
    color_pages_min INTEGER DEFAULT 0,
    color_pages_max INTEGER DEFAULT 0,
    color_pages_avg INTEGER DEFAULT 0,
    mono_pages_min INTEGER DEFAULT 0,
    mono_pages_max INTEGER DEFAULT 0,
    mono_pages_avg INTEGER DEFAULT 0,
    scan_count_min INTEGER DEFAULT 0,
    scan_count_max INTEGER DEFAULT 0,
    scan_count_avg INTEGER DEFAULT 0,
    toner_levels_avg TEXT,
    FOREIGN KEY (serial) REFERENCES devices(serial) ON DELETE CASCADE,
    UNIQUE(serial, day_start)
);

-- Settings table (key-value store)
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================================
-- SEED DATA - Realistic printers based on real device reports
-- ============================================================================

-- Epson WF-C5790 (color inkjet MFP) - from gist data
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'CV25P8',
    '192.168.100.108',
    'Epson',
    'WF-C5790 Series',
    'EPSONB2A0AF',
    'Printer FW',
    'B0:E4:D5:B2:A0:AF',
    '255.255.255.0',
    765,
    '{"toner_black":39,"toner_cyan":15,"toner_magenta":1,"toner_yellow":14}',
    '["Black Ink Supply Unit 902, 902XL, 902XXL","Cyan Ink Supply Unit 902, 902XL","Magenta Ink Supply Unit 902, 902XL","Yellow Ink Supply Unit 902, 902XL"]',
    datetime('now'),
    datetime('now', '-30 days'),
    datetime('now', '-30 days'),
    1,
    1,
    'mdns',
    '{"is_color":true,"is_inkjet":true,"is_laser":false,"is_copier":false,"is_fax":false,"is_scanner":false,"open_ports":[9100],"toner_level_black":39,"toner_level_cyan":15,"toner_level_magenta":1,"toner_level_yellow":14,"toner_desc_black":"Black Ink Supply Unit 902, 902XL, 902XXL","toner_desc_cyan":"Cyan Ink Supply Unit 902, 902XL","toner_desc_magenta":"Magenta Ink Supply Unit 902, 902XL","toner_desc_yellow":"Yellow Ink Supply Unit 902, 902XL"}'
);

-- Kyocera ECOSYS M3655idn (mono laser MFP) - common office workhorse
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'VXF5012345',
    '192.168.100.20',
    'Kyocera',
    'ECOSYS M3655idn',
    'KM5012345',
    '2US_S000.002.502',
    '00:C0:EE:50:12:34',
    '255.255.255.0',
    125847,
    '{"toner_black":68}',
    '["TK-3182 Toner"]',
    datetime('now'),
    datetime('now', '-90 days'),
    datetime('now', '-90 days'),
    1,
    1,
    'snmp',
    '{"is_color":false,"is_inkjet":false,"is_laser":true,"is_copier":true,"is_fax":false,"is_scanner":true,"open_ports":[80,443,9100],"toner_level_black":68,"toner_desc_black":"TK-3182 Toner","duplex_supported":true}'
);

-- HP LaserJet Pro M404dn (mono laser)
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'PHCBD82R4K',
    '192.168.100.30',
    'HP',
    'HP LaserJet Pro M404dn',
    'HPB82R4K',
    '002.2339A',
    '3C:18:A0:B8:2R:4K',
    '255.255.255.0',
    45231,
    '{"toner_black":42}',
    '["HP 58A Black Original LaserJet Toner Cartridge (CF258A)"]',
    datetime('now'),
    datetime('now', '-60 days'),
    datetime('now', '-60 days'),
    1,
    1,
    'snmp',
    '{"is_color":false,"is_inkjet":false,"is_laser":true,"is_copier":false,"is_fax":false,"is_scanner":false,"open_ports":[80,443,9100],"toner_level_black":42,"toner_desc_black":"HP 58A Black Original LaserJet Toner Cartridge (CF258A)","duplex_supported":true}'
);

-- Brother MFC-L8900CDW (color laser MFP)
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'U64180H8N123456',
    '192.168.100.40',
    'Brother',
    'MFC-L8900CDW',
    'BRN123456',
    'N',
    '00:80:77:12:34:56',
    '255.255.255.0',
    28450,
    '{"toner_black":85,"toner_cyan":62,"toner_magenta":71,"toner_yellow":58,"drum_black":78,"drum_cyan":81,"drum_magenta":79,"drum_yellow":80}',
    '["TN-436BK Black Toner","TN-436C Cyan Toner","TN-436M Magenta Toner","TN-436Y Yellow Toner","DR-431CL Drum Unit"]',
    datetime('now'),
    datetime('now', '-45 days'),
    datetime('now', '-45 days'),
    1,
    1,
    'snmp',
    '{"is_color":true,"is_inkjet":false,"is_laser":true,"is_copier":true,"is_fax":true,"is_scanner":true,"open_ports":[80,443,631,9100],"toner_level_black":85,"toner_level_cyan":62,"toner_level_magenta":71,"toner_level_yellow":58,"drum_level":78,"duplex_supported":true}'
);

-- Lexmark MS621dn (mono laser) - warning state, low toner
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, status_messages, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    '47TT812',
    '192.168.100.50',
    'Lexmark',
    'MS621dn',
    'LXK47TT812',
    'MSNGM.076.293',
    '40:B0:34:47:TT:81',
    '255.255.255.0',
    89234,
    '{"toner_black":8}',
    '["56F1000 Black Toner Cartridge"]',
    '["Toner Low"]',
    datetime('now'),
    datetime('now', '-120 days'),
    datetime('now', '-120 days'),
    1,
    1,
    'snmp',
    '{"is_color":false,"is_inkjet":false,"is_laser":true,"is_copier":false,"is_fax":false,"is_scanner":false,"open_ports":[80,443,9100],"toner_level_black":8,"toner_desc_black":"56F1000 Black Toner Cartridge","duplex_supported":true,"alerts":["Toner Low"]}'
);

-- Xerox VersaLink C405 (color laser MFP) - error state, paper jam
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, status_messages, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'C1J012345',
    '192.168.100.60',
    'Xerox',
    'VersaLink C405DN',
    'XRX012345',
    '116.050.008.41600',
    '00:00:AA:C1:J0:12',
    '255.255.255.0',
    52198,
    '{"toner_black":45,"toner_cyan":32,"toner_magenta":28,"toner_yellow":51}',
    '["106R03512 Black Toner","106R03513 Cyan Toner","106R03514 Magenta Toner","106R03515 Yellow Toner"]',
    '["Paper Jam in Tray 1"]',
    datetime('now', '-2 hours'),
    datetime('now', '-75 days'),
    datetime('now', '-75 days'),
    1,
    1,
    'snmp',
    '{"is_color":true,"is_inkjet":false,"is_laser":true,"is_copier":true,"is_fax":true,"is_scanner":true,"open_ports":[80,443,9100],"toner_level_black":45,"toner_level_cyan":32,"toner_level_magenta":28,"toner_level_yellow":51,"duplex_supported":true,"alerts":["Paper Jam in Tray 1"]}'
);

-- Epson ST-C8090 (large-format color inkjet)
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'X4MF012345',
    '192.168.100.70',
    'Epson',
    'ST-C8090 Series',
    'EPSONC8090',
    'SC20M2',
    'E0:70:EA:C8:09:00',
    '255.255.255.0',
    3240,
    '{"toner_black":95,"toner_cyan":88,"toner_magenta":92,"toner_yellow":87}',
    '["T01C1 Black Ink Pack","T01C2 Cyan Ink Pack","T01C3 Magenta Ink Pack","T01C4 Yellow Ink Pack"]',
    datetime('now'),
    datetime('now', '-15 days'),
    datetime('now', '-15 days'),
    1,
    1,
    'mdns',
    '{"is_color":true,"is_inkjet":true,"is_laser":false,"is_copier":false,"is_fax":false,"is_scanner":false,"open_ports":[80,9100],"toner_level_black":95,"toner_level_cyan":88,"toner_level_magenta":92,"toner_level_yellow":87}'
);

-- Kyocera ECOSYS P2040dw (mono laser, low cost)
INSERT INTO devices (serial, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, page_count, toner_levels, consumables, last_seen, created_at, first_seen, is_saved, visible, discovery_method, raw_data)
VALUES (
    'VXL8123456',
    '192.168.100.80',
    'Kyocera',
    'ECOSYS P2040dw',
    'KM8123456',
    '2US_S000.002.407',
    '00:C0:EE:81:23:45',
    '255.255.255.0',
    8234,
    '{"toner_black":55}',
    '["TK-1172 Toner"]',
    datetime('now'),
    datetime('now', '-20 days'),
    datetime('now', '-20 days'),
    1,
    1,
    'snmp',
    '{"is_color":false,"is_inkjet":false,"is_laser":true,"is_copier":false,"is_fax":false,"is_scanner":false,"open_ports":[80,443,9100],"toner_level_black":55,"toner_desc_black":"TK-1172 Toner","duplex_supported":true}'
);

-- ============================================================================
-- METRICS DATA - Recent usage patterns
-- ============================================================================

-- Raw metrics for Epson WF-C5790 (last 6 hours, 5-min intervals sampled)
INSERT INTO metrics_raw (serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels) VALUES
    ('CV25P8', datetime('now', '-6 hours'), 760, 200, 560, 45, '{"toner_black":41,"toner_cyan":17,"toner_magenta":3,"toner_yellow":16}'),
    ('CV25P8', datetime('now', '-5 hours'), 761, 201, 560, 45, '{"toner_black":41,"toner_cyan":17,"toner_magenta":3,"toner_yellow":16}'),
    ('CV25P8', datetime('now', '-4 hours'), 762, 201, 561, 46, '{"toner_black":40,"toner_cyan":16,"toner_magenta":2,"toner_yellow":15}'),
    ('CV25P8', datetime('now', '-3 hours'), 763, 202, 561, 47, '{"toner_black":40,"toner_cyan":16,"toner_magenta":2,"toner_yellow":15}'),
    ('CV25P8', datetime('now', '-2 hours'), 764, 202, 562, 48, '{"toner_black":39,"toner_cyan":15,"toner_magenta":1,"toner_yellow":14}'),
    ('CV25P8', datetime('now', '-1 hours'), 765, 203, 562, 48, '{"toner_black":39,"toner_cyan":15,"toner_magenta":1,"toner_yellow":14}');

-- Raw metrics for Kyocera M3655idn (high volume device)
INSERT INTO metrics_raw (serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels) VALUES
    ('VXF5012345', datetime('now', '-6 hours'), 125800, 0, 125800, 12500, '{"toner_black":70}'),
    ('VXF5012345', datetime('now', '-4 hours'), 125823, 0, 125823, 12505, '{"toner_black":69}'),
    ('VXF5012345', datetime('now', '-2 hours'), 125840, 0, 125840, 12508, '{"toner_black":68}'),
    ('VXF5012345', datetime('now'), 125847, 0, 125847, 12510, '{"toner_black":68}');

-- Raw metrics for HP M404dn
INSERT INTO metrics_raw (serial, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels) VALUES
    ('PHCBD82R4K', datetime('now', '-4 hours'), 45220, 0, 45220, 0, '{"toner_black":43}'),
    ('PHCBD82R4K', datetime('now', '-2 hours'), 45225, 0, 45225, 0, '{"toner_black":42}'),
    ('PHCBD82R4K', datetime('now'), 45231, 0, 45231, 0, '{"toner_black":42}');

-- Hourly aggregates for the past 24 hours (Epson WF-C5790)
INSERT INTO metrics_hourly (serial, hour_start, sample_count, page_count_min, page_count_max, page_count_avg, color_pages_avg, mono_pages_avg, scan_count_avg, toner_levels_avg) VALUES
    ('CV25P8', strftime('%Y-%m-%d %H:00:00', datetime('now', '-24 hours')), 12, 740, 745, 742, 190, 552, 40, '{"toner_black":50,"toner_cyan":25,"toner_magenta":12,"toner_yellow":22}'),
    ('CV25P8', strftime('%Y-%m-%d %H:00:00', datetime('now', '-12 hours')), 12, 752, 758, 755, 195, 560, 43, '{"toner_black":45,"toner_cyan":20,"toner_magenta":7,"toner_yellow":18}'),
    ('CV25P8', strftime('%Y-%m-%d %H:00:00', datetime('now', '-6 hours')), 12, 760, 765, 762, 201, 561, 47, '{"toner_black":40,"toner_cyan":16,"toner_magenta":2,"toner_yellow":15}');

-- Daily aggregates for the past week (Kyocera M3655idn - high volume)
INSERT INTO metrics_daily (serial, day_start, sample_count, page_count_min, page_count_max, page_count_avg, mono_pages_avg, scan_count_avg, toner_levels_avg) VALUES
    ('VXF5012345', date('now', '-7 days'), 24, 125100, 125250, 125175, 125175, 12400, '{"toner_black":82}'),
    ('VXF5012345', date('now', '-6 days'), 24, 125250, 125380, 125315, 125315, 12420, '{"toner_black":80}'),
    ('VXF5012345', date('now', '-5 days'), 24, 125380, 125500, 125440, 125440, 12440, '{"toner_black":78}'),
    ('VXF5012345', date('now', '-4 days'), 24, 125500, 125610, 125555, 125555, 12460, '{"toner_black":76}'),
    ('VXF5012345', date('now', '-3 days'), 24, 125610, 125720, 125665, 125665, 12480, '{"toner_black":74}'),
    ('VXF5012345', date('now', '-2 days'), 24, 125720, 125800, 125760, 125760, 12495, '{"toner_black":72}'),
    ('VXF5012345', date('now', '-1 days'), 24, 125800, 125847, 125823, 125823, 12508, '{"toner_black":69}');

-- ============================================================================
-- SCAN HISTORY - Discovery events
-- ============================================================================

INSERT INTO scan_history (serial, created_at, ip, hostname, firmware, consumables, discovery_method) VALUES
    ('CV25P8', datetime('now', '-30 days'), '192.168.100.108', 'EPSONB2A0AF', 'Printer FW', '["Black Ink Supply Unit 902","Cyan Ink Supply Unit 902","Magenta Ink Supply Unit 902","Yellow Ink Supply Unit 902"]', 'mdns'),
    ('CV25P8', datetime('now'), '192.168.100.108', 'EPSONB2A0AF', 'Printer FW', '["Black Ink Supply Unit 902, 902XL, 902XXL","Cyan Ink Supply Unit 902, 902XL","Magenta Ink Supply Unit 902, 902XL","Yellow Ink Supply Unit 902, 902XL"]', 'snmp'),
    ('VXF5012345', datetime('now', '-90 days'), '192.168.100.20', 'KM5012345', '2US_S000.002.502', '["TK-3182 Toner"]', 'snmp'),
    ('PHCBD82R4K', datetime('now', '-60 days'), '192.168.100.30', 'HPB82R4K', '002.2339A', '["HP 58A Black Original LaserJet Toner Cartridge (CF258A)"]', 'snmp');

-- ============================================================================
-- SETTINGS - Agent configuration
-- ============================================================================

INSERT INTO settings (key, value, updated_at) VALUES
    ('server_url', 'http://server:9090', datetime('now')),
    ('server_enabled', 'true', datetime('now')),
    ('agent_name', 'e2e-test-agent', datetime('now')),
    ('scan_interval_minutes', '60', datetime('now')),
    ('autoupdate_enabled', 'false', datetime('now'));

-- Done
SELECT 'Agent seed data loaded: 8 devices, metrics, scan history' AS status;
