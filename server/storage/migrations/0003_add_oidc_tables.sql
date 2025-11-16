-- 0003_add_oidc_tables.sql
-- Adds tables required for OIDC single sign-on providers and login sessions.
BEGIN;

CREATE TABLE IF NOT EXISTS oidc_providers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  slug TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  issuer TEXT NOT NULL,
  client_id TEXT NOT NULL,
  client_secret TEXT NOT NULL,
  scopes TEXT NOT NULL DEFAULT 'openid profile email',
  icon TEXT,
  button_text TEXT,
  button_style TEXT,
  auto_login INTEGER NOT NULL DEFAULT 0,
  tenant_id TEXT,
  default_role TEXT NOT NULL DEFAULT 'user',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS oidc_sessions (
  id TEXT PRIMARY KEY,
  provider_slug TEXT NOT NULL,
  tenant_id TEXT,
  nonce TEXT NOT NULL,
  state TEXT NOT NULL,
  redirect_url TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS oidc_links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider_slug TEXT NOT NULL,
  subject TEXT NOT NULL,
  email TEXT,
  user_id INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(provider_slug, subject),
  FOREIGN KEY (provider_slug) REFERENCES oidc_providers(slug) ON DELETE CASCADE,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

COMMIT;
