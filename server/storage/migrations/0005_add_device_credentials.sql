-- Device web UI credentials table (for proxy auto-login)
-- Stored on server so agents remain stateless

CREATE TABLE IF NOT EXISTS device_credentials (
    serial TEXT PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    encrypted_password TEXT NOT NULL DEFAULT '',
    auth_type TEXT NOT NULL DEFAULT 'basic',
    auto_login INTEGER NOT NULL DEFAULT 0,
    tenant_id TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(serial) REFERENCES devices(serial) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_device_credentials_tenant ON device_credentials(tenant_id);
