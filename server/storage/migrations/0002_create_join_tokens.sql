-- 0002_create_join_tokens.sql
-- Create join_tokens table and ensure tenant_id columns exist on agents/devices/metrics_history.

BEGIN;

-- Tenants table (if not already present)
CREATE TABLE IF NOT EXISTS tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Join tokens table stores only token hashes for security
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

-- Add tenant_id columns if missing
ALTER TABLE agents ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE metrics_history ADD COLUMN IF NOT EXISTS tenant_id TEXT;

-- Ensure a default tenant exists
INSERT INTO tenants (id, name)
  SELECT 'default', 'default' WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE id='default');

COMMIT;

-- Down migration: drop join_tokens and tenants (note: dropping columns in SQLite is non-trivial)
BEGIN;
DROP TABLE IF EXISTS join_tokens;
DROP TABLE IF EXISTS tenants;
COMMIT;
