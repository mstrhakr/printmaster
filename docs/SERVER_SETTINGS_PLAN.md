# Server-Controlled Settings Plan

This document outlines how the server will own tenant-aware settings using the shared `common/settings` schema and drive agent defaults remotely.

## Objectives

- Store the canonical settings state per scope (global → tenant → agent).
- Provide APIs/UI for admins to view, diff, and update settings according to RBAC.
- Deliver resolved settings to agents (push/pull) so they no longer rely solely on local config.
- Reuse the shared schema (`common/settings`) for validation, metadata, and defaults.

### Current Status (Nov 2025)

- Storage schema + seed logic now lives in `server/storage/sqlite.go` (no standalone migration required pre-v1.0).
- Resolver + REST handlers ship behind the tenancy feature flag with routes under `/api/v1/settings/*` plus the alias `/api/v1/tenants/{id}/settings` via the new subresource dispatcher.
- Global and per-tenant endpoints enforce `settings.*` RBAC and record `updated_by` metadata for auditing.

## Scope & Assumptions

- Tenancy feature flag remains in place; all new APIs require it enabled until GA.
- Agents already expose `/settings`; the server will consume/override via the upload channel after the schema lands.
- Initial milestone focuses on server storage + REST APIs; UI wiring and agent pull mechanics can follow once endpoints stabilize.

## Storage Additions (server/storage)

1. **Tables** (SQLite + future Postgres):
   - `settings_global` (single row)
     - `schema_version TEXT NOT NULL` (matches `common/settings.SchemaVersion`).
     - `payload JSONB/TEXT NOT NULL` (serialized `pmsettings.Settings`).
     - `updated_at DATETIME`.
   - `settings_tenant`
     - `tenant_id TEXT PRIMARY KEY` (FK → `tenants.id`).
     - `schema_version TEXT NOT NULL`.
     - `payload JSONB/TEXT NOT NULL` (partial struct: only fields the tenant overrides).
     - `updated_at DATETIME`.
   - `settings_agent_override` (optional future; not in MVP but leave migration stub for per-agent exceptions).

2. **Store interfaces** (`server/storage/types.go` → `Store` interface):
   - `GetGlobalSettings(ctx) (pmsettings.Settings, error)`.
   - `UpsertGlobalSettings(ctx, pmsettings.Settings) error`.
   - `GetTenantSettings(ctx, tenantID string) (pmsettings.Settings, error)` (returns merged base+tenant subset plus flags that indicate overrides).
   - `UpsertTenantSettings(ctx, tenantID string, overrides pmsettings.Settings) error`.
   - `ListTenantSettings(ctx) ([]*TenantSettingsSummary, error)` to power UI tables.

3. **Migration** (`server/storage/migrations/0002_settings.sql`):
   - Adds tables, default row seeded using `common/settings.DefaultSettings()` serialized via CLI helper.
   - Ensures `tenant_id` cascade delete for overrides when tenant removed.

## Resolver Layer (`server/settings/resolver.go`)

- New package `server/settings` encapsulating:
  - `Loader` struct with dependencies on `storage.Store` + `pmsettings.Schema`.
  - `ResolveForTenant(ctx, tenantID string) (pmsettings.Settings, error)` applying precedence: `DefaultSettings()` → global overrides → tenant overrides → `pmsettings.Sanitize`.
  - `ResolveForAgent(ctx, tenantID, agentID string) (pmsettings.Settings, error)` placeholder for later per-agent overrides.
  - `Diff(base, override)` helper returning metadata (field path + before/after) for UI.
  - `ValidatePayload(raw map[string]any)` using `pmsettings.Validate` and schema range metadata; returns `[]pmsettings.ValidationError`.

## REST / GraphQL APIs (server/main.go + handlers)

1. **Schema exposure**:
   - `GET /api/v1/settings/schema` returns `pmsettings.DefaultSchema()` to drive UI forms.

2. **Global settings**:
   - `GET /api/v1/settings/global` → resolved snapshot + `last_updated`.
   - `PUT /api/v1/settings/global` accepts `{ "discovery": {...}, "developer": {...}, "security": {...} }` (same envelope as agent). Validates via shared helpers, persists diff, records audit (`settings.global.update`).

3. **Tenant settings**:
   - `GET /api/v1/tenants/{id}/settings` → resolved + override flags.
   - `PUT /api/v1/tenants/{id}/settings` accepts partial struct; only fields provided are stored as overrides.
   - `DELETE /api/v1/tenants/{id}/settings` resets tenant to inherit from global.

4. **Agent delivery hooks** (future but plan now):
   - Extend `/api/v1/agents/{uuid}/settings` (new) to let agents fetch resolved config after auth.
   - Upload worker may receive `settings_version` in heartbeat; server will respond with delta if mismatch.

5. **RBAC Enforcement** (`server/authorize.go`):
   - Global endpoints require `server.admin` action.
   - Tenant endpoints require `tenant.admin` for the target tenant; server admins can act on any tenant.
   - Expose `EditableBy` from schema to UI so controls disable automatically for lower roles.

## UI/UX Considerations (follow-up work)

- Admin Console adds Settings area with tabs: Global Defaults, Tenants (per-tenant override table), Preview JSON.
- Use schema metadata for form rendering (types, enum, descriptions).
- Highlight inherited vs overridden values with badges; allow quick reset per field.

## Agent Integration Roadmap

1. Extend heartbeat payload to include `settings_version` + `tenant_id`.
2. Server compares with resolver hash and replies over WebSocket or REST with updates.
3. Agent `/settings` handler adds “managed fields” indicator and blocks editing when server-controlled scope applies.

## Testing Strategy

- Unit tests for resolver ensuring merge order + validation.
- Storage tests (SQLite) confirming migrations, CRUD, and tenant cascade.
- API tests using `httptest` to verify RBAC, validation errors, audit logging.
- Playwright smoke flows: server admin edits global toggle, tenant admin overrides, agent view reflects change.

## Delivery Steps

1. Create `server/settings` package (resolver + validation helpers).
2. Add DB migrations + wire store methods.
3. Implement REST handlers + RBAC wiring.
4. Update UI + Playwright tests.
5. Agent heartbeat/settings sync.
6. Documentation updates (`docs/TENANCY_ROADMAP.md`, `docs/API.md`).

## Open Questions

- How to version settings for conflict detection? Proposal: store `updated_at` + `version` (incremental int) in each table and include in API responses.
- Should tenants inherit only editable scopes? For MVP we allow overrides only for schema fields with `ScopeTenant`.
- Notification channel for running agents? Option A: piggyback on upload responses; Option B: SSE/WebSocket broadcast to agents.

---
This plan unblocks the remaining todo item and gives a concrete path to implement server-driven settings control.
