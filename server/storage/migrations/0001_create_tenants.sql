-- 0001_create_tenants.sql
-- Add a tenants table and tenant_id columns to agents, devices, and metrics_history.
-- Intended for use with golang-migrate (up/down sections).

-- Up migration
BEGIN;

-- Create tenants table (idempotent)
CREATE TABLE IF NOT EXISTS tenants (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Add tenant_id columns to existing tables. These statements will fail on some SQL engines
-- if the column already exists, but golang-migrate applies each migration only once.
-- For SQLite, ALTER TABLE ... ADD COLUMN is supported.

ALTER TABLE agents ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS tenant_id TEXT;
ALTER TABLE metrics_history ADD COLUMN IF NOT EXISTS tenant_id TEXT;

-- Insert a default tenant if missing
INSERT INTO tenants (id, name)
  SELECT 'default', 'default' WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE id='default');

COMMIT;

-- Down migration
-- Note: Removing columns in SQLite is non-trivial (requires table recreation). For safety,
-- the down migration will drop the tenants table but leave columns in place. Adjust as needed
-- for your production DBs.

-- Down
BEGIN;

DROP TABLE IF EXISTS tenants;

COMMIT;
