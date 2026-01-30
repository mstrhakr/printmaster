-- PrintMaster Server E2E Test Data
-- This SQL contains ONLY INSERT statements for seeding test data.
-- The schema is created by the server container on startup.
--
-- Usage: sqlite3 /path/to/server.db < server_data.sql
-- Note: Run AFTER container has created the schema

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- ============================================================================
-- SEED DATA - Realistic printers synchronized from agent
-- ============================================================================

-- Default tenant
INSERT INTO tenants (id, name, description, created_at) VALUES
    ('e2e-tenant-001', 'E2E Test Organization', 'Default tenant for E2E testing', datetime('now'));

-- Test agent (matches what the E2E agent will register as)
-- The agent_id is generated on first run, so we use a placeholder
-- that will be updated when the actual agent connects
INSERT INTO agents (agent_id, name, hostname, ip, platform, version, protocol_version, token, status, device_count, registered_at, last_seen, tenant_id)
VALUES (
    'e2e00000-0000-0000-0000-000000000001',
    'e2e-test-agent',
    'agent',
    '172.20.0.3',
    'linux',
    'e2e-test',
    '1',
    'e2e-test-token-placeholder',
    'active',
    8,
    datetime('now', '-30 days'),
    datetime('now'),
    'e2e-tenant-001'
);

-- Devices (same as agent seed, synced via upload)
-- Epson WF-C5790
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'CV25P8',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.108',
    'Epson',
    'WF-C5790 Series',
    'EPSONB2A0AF',
    'Printer FW',
    'B0:E4:D5:B2:A0:AF',
    '255.255.255.0',
    '["Black Ink Supply Unit 902, 902XL, 902XXL","Cyan Ink Supply Unit 902, 902XL","Magenta Ink Supply Unit 902, 902XL","Yellow Ink Supply Unit 902, 902XL"]',
    datetime('now'),
    datetime('now', '-30 days'),
    datetime('now', '-30 days'),
    'mdns',
    'https://192.168.100.108',
    '{"is_color":true,"is_inkjet":true,"toner_level_black":39,"toner_level_cyan":15,"toner_level_magenta":1,"toner_level_yellow":14}',
    'e2e-tenant-001'
);

-- Kyocera ECOSYS M3655idn
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'VXF5012345',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.20',
    'Kyocera',
    'ECOSYS M3655idn',
    'KM5012345',
    '2US_S000.002.502',
    '00:C0:EE:50:12:34',
    '255.255.255.0',
    '["TK-3182 Toner"]',
    datetime('now'),
    datetime('now', '-90 days'),
    datetime('now', '-90 days'),
    'snmp',
    'https://192.168.100.20',
    '{"is_color":false,"is_laser":true,"is_copier":true,"is_scanner":true,"toner_level_black":68}',
    'e2e-tenant-001'
);

-- HP LaserJet Pro M404dn
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'PHCBD82R4K',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.30',
    'HP',
    'HP LaserJet Pro M404dn',
    'HPB82R4K',
    '002.2339A',
    '3C:18:A0:B8:2R:4K',
    '255.255.255.0',
    '["HP 58A Black Original LaserJet Toner Cartridge (CF258A)"]',
    datetime('now'),
    datetime('now', '-60 days'),
    datetime('now', '-60 days'),
    'snmp',
    'https://192.168.100.30',
    '{"is_color":false,"is_laser":true,"toner_level_black":42}',
    'e2e-tenant-001'
);

-- Brother MFC-L8900CDW
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'U64180H8N123456',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.40',
    'Brother',
    'MFC-L8900CDW',
    'BRN123456',
    'N',
    '00:80:77:12:34:56',
    '255.255.255.0',
    '["TN-436BK Black Toner","TN-436C Cyan Toner","TN-436M Magenta Toner","TN-436Y Yellow Toner","DR-431CL Drum Unit"]',
    datetime('now'),
    datetime('now', '-45 days'),
    datetime('now', '-45 days'),
    'snmp',
    'https://192.168.100.40',
    '{"is_color":true,"is_laser":true,"is_copier":true,"is_fax":true,"is_scanner":true,"toner_level_black":85,"toner_level_cyan":62,"toner_level_magenta":71,"toner_level_yellow":58}',
    'e2e-tenant-001'
);

-- Lexmark MS621dn (low toner warning)
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, status_messages, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    '47TT812',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.50',
    'Lexmark',
    'MS621dn',
    'LXK47TT812',
    'MSNGM.076.293',
    '40:B0:34:47:TT:81',
    '255.255.255.0',
    '["56F1000 Black Toner Cartridge"]',
    '["Toner Low"]',
    datetime('now'),
    datetime('now', '-120 days'),
    datetime('now', '-120 days'),
    'snmp',
    'https://192.168.100.50',
    '{"is_color":false,"is_laser":true,"toner_level_black":8,"alerts":["Toner Low"]}',
    'e2e-tenant-001'
);

-- Xerox VersaLink C405 (paper jam error)
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, status_messages, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'C1J012345',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.60',
    'Xerox',
    'VersaLink C405DN',
    'XRX012345',
    '116.050.008.41600',
    '00:00:AA:C1:J0:12',
    '255.255.255.0',
    '["106R03512 Black Toner","106R03513 Cyan Toner","106R03514 Magenta Toner","106R03515 Yellow Toner"]',
    '["Paper Jam in Tray 1"]',
    datetime('now', '-2 hours'),
    datetime('now', '-75 days'),
    datetime('now', '-75 days'),
    'snmp',
    'https://192.168.100.60',
    '{"is_color":true,"is_laser":true,"is_copier":true,"is_fax":true,"is_scanner":true,"toner_level_black":45,"toner_level_cyan":32,"toner_level_magenta":28,"toner_level_yellow":51,"alerts":["Paper Jam in Tray 1"]}',
    'e2e-tenant-001'
);

-- Epson ST-C8090
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'X4MF012345',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.70',
    'Epson',
    'ST-C8090 Series',
    'EPSONC8090',
    'SC20M2',
    'E0:70:EA:C8:09:00',
    '255.255.255.0',
    '["T01C1 Black Ink Pack","T01C2 Cyan Ink Pack","T01C3 Magenta Ink Pack","T01C4 Yellow Ink Pack"]',
    datetime('now'),
    datetime('now', '-15 days'),
    datetime('now', '-15 days'),
    'mdns',
    'https://192.168.100.70',
    '{"is_color":true,"is_inkjet":true,"toner_level_black":95,"toner_level_cyan":88,"toner_level_magenta":92,"toner_level_yellow":87}',
    'e2e-tenant-001'
);

-- Kyocera ECOSYS P2040dw
INSERT INTO devices (serial, agent_id, ip, manufacturer, model, hostname, firmware, mac_address, subnet_mask, consumables, last_seen, first_seen, created_at, discovery_method, web_ui_url, raw_data, tenant_id)
VALUES (
    'VXL8123456',
    'e2e00000-0000-0000-0000-000000000001',
    '192.168.100.80',
    'Kyocera',
    'ECOSYS P2040dw',
    'KM8123456',
    '2US_S000.002.407',
    '00:C0:EE:81:23:45',
    '255.255.255.0',
    '["TK-1172 Toner"]',
    datetime('now'),
    datetime('now', '-20 days'),
    datetime('now', '-20 days'),
    'snmp',
    'https://192.168.100.80',
    '{"is_color":false,"is_laser":true,"toner_level_black":55}',
    'e2e-tenant-001'
);

-- ============================================================================
-- METRICS HISTORY - Usage data synced from agent
-- ============================================================================

-- Epson WF-C5790 metrics (color inkjet, moderate usage)
INSERT INTO metrics_history (serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels, tenant_id) VALUES
    ('CV25P8', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-24 hours'), 740, 190, 550, 40, '{"toner_black":50,"toner_cyan":25,"toner_magenta":12,"toner_yellow":22}', 'e2e-tenant-001'),
    ('CV25P8', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-12 hours'), 752, 195, 557, 43, '{"toner_black":45,"toner_cyan":20,"toner_magenta":7,"toner_yellow":18}', 'e2e-tenant-001'),
    ('CV25P8', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-6 hours'), 760, 200, 560, 45, '{"toner_black":41,"toner_cyan":17,"toner_magenta":3,"toner_yellow":16}', 'e2e-tenant-001'),
    ('CV25P8', 'e2e00000-0000-0000-0000-000000000001', datetime('now'), 765, 203, 562, 48, '{"toner_black":39,"toner_cyan":15,"toner_magenta":1,"toner_yellow":14}', 'e2e-tenant-001');

-- Kyocera M3655idn metrics (high volume mono)
INSERT INTO metrics_history (serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels, tenant_id) VALUES
    ('VXF5012345', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-7 days'), 125100, 0, 125100, 12380, '{"toner_black":82}', 'e2e-tenant-001'),
    ('VXF5012345', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-3 days'), 125500, 0, 125500, 12450, '{"toner_black":76}', 'e2e-tenant-001'),
    ('VXF5012345', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-1 days'), 125800, 0, 125800, 12500, '{"toner_black":70}', 'e2e-tenant-001'),
    ('VXF5012345', 'e2e00000-0000-0000-0000-000000000001', datetime('now'), 125847, 0, 125847, 12510, '{"toner_black":68}', 'e2e-tenant-001');

-- HP M404dn metrics (steady office printer)
INSERT INTO metrics_history (serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels, tenant_id) VALUES
    ('PHCBD82R4K', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-7 days'), 45000, 0, 45000, 0, '{"toner_black":48}', 'e2e-tenant-001'),
    ('PHCBD82R4K', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-3 days'), 45120, 0, 45120, 0, '{"toner_black":45}', 'e2e-tenant-001'),
    ('PHCBD82R4K', 'e2e00000-0000-0000-0000-000000000001', datetime('now'), 45231, 0, 45231, 0, '{"toner_black":42}', 'e2e-tenant-001');

-- Brother MFC-L8900CDW metrics (color MFP)
INSERT INTO metrics_history (serial, agent_id, timestamp, page_count, color_pages, mono_pages, scan_count, toner_levels, tenant_id) VALUES
    ('U64180H8N123456', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-14 days'), 28000, 8000, 20000, 5500, '{"toner_black":92,"toner_cyan":70,"toner_magenta":78,"toner_yellow":65}', 'e2e-tenant-001'),
    ('U64180H8N123456', 'e2e00000-0000-0000-0000-000000000001', datetime('now', '-7 days'), 28220, 8080, 20140, 5600, '{"toner_black":88,"toner_cyan":66,"toner_magenta":74,"toner_yellow":61}', 'e2e-tenant-001'),
    ('U64180H8N123456', 'e2e00000-0000-0000-0000-000000000001', datetime('now'), 28450, 8150, 20300, 5680, '{"toner_black":85,"toner_cyan":62,"toner_magenta":71,"toner_yellow":58}', 'e2e-tenant-001');

-- ============================================================================
-- AUDIT LOG - Sample events
-- ============================================================================

INSERT INTO audit_log (timestamp, actor_type, actor_id, actor_name, action, target_type, target_id, severity, details, tenant_id) VALUES
    (datetime('now', '-30 days'), 'agent', 'e2e00000-0000-0000-0000-000000000001', 'e2e-test-agent', 'agent.registered', 'agent', 'e2e00000-0000-0000-0000-000000000001', 'info', '{"hostname":"agent","platform":"linux"}', 'e2e-tenant-001'),
    (datetime('now', '-30 days'), 'agent', 'e2e00000-0000-0000-0000-000000000001', 'e2e-test-agent', 'device.discovered', 'device', 'CV25P8', 'info', '{"model":"WF-C5790 Series","manufacturer":"Epson"}', 'e2e-tenant-001'),
    (datetime('now', '-7 days'), 'agent', 'e2e00000-0000-0000-0000-000000000001', 'e2e-test-agent', 'device.alert', 'device', '47TT812', 'warning', '{"alert":"Toner Low","toner_level":12}', 'e2e-tenant-001'),
    (datetime('now', '-2 hours'), 'agent', 'e2e00000-0000-0000-0000-000000000001', 'e2e-test-agent', 'device.alert', 'device', 'C1J012345', 'error', '{"alert":"Paper Jam in Tray 1"}', 'e2e-tenant-001');

-- Done
SELECT 'Server test data loaded: 1 tenant, 1 agent, 8 devices, metrics, audit log' AS status;
