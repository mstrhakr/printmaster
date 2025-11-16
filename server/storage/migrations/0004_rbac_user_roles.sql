-- 0004_rbac_user_roles.sql
-- Introduce role normalization and user_tenants mapping table.

BEGIN;

CREATE TABLE IF NOT EXISTS user_tenants (
  user_id INTEGER NOT NULL,
  tenant_id TEXT NOT NULL,
  PRIMARY KEY (user_id, tenant_id),
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_tenants_tenant ON user_tenants(tenant_id);

INSERT OR IGNORE INTO user_tenants (user_id, tenant_id)
  SELECT id, tenant_id FROM users
  WHERE tenant_id IS NOT NULL AND TRIM(tenant_id) != '';

UPDATE users SET role = 'operator' WHERE role = 'user';
UPDATE users SET role = 'viewer' WHERE role NOT IN ('admin','operator','viewer') OR role IS NULL OR TRIM(role) = '';
UPDATE oidc_providers SET default_role = 'viewer'
  WHERE default_role NOT IN ('admin','operator','viewer') OR default_role IS NULL OR TRIM(default_role) = '';

COMMIT;
