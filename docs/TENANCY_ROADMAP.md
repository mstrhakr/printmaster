# Tenancy & SSO Roadmap

This document captures the prioritized plan and concrete next steps to evolve PrintMaster from single-tenant to multi-tenant with SSO and secure agent onboarding.

Goals
- Add first-class tenants (customers) to the server and associate agents/devices/metrics with a tenant.
- Provide a secure, simple onboarding flow for agents to join a tenant (join tokens & package generation).
- Add SSO (OIDC + SAML) and robust local login as authentication options.
- Enforce tenant-scoped access and RBAC in UI and API.
- Keep SQLite as default but make the database pluggable (Postgres/MariaDB) for scale.

Prioritized starting order (why and what to do first)

1) Data model & migrations (High priority)
- Why: foundational. Tenants must exist before any UI or enforcement can be meaningful.
- What: create `tenants` table and add nullable `tenant_id` column to `agents`, `devices`, `metrics_history`.
- Acceptance: idempotent migration script + default tenant insertion for upgrades + backup step.

2) Agent onboarding: join tokens + package generator (High priority)
- Why: enables automated secure assignment of agents to tenants without manual config edits.
- What: server API to create join tokens (short-lived or one-time) and downloadable agent packages configured with server URL + token.
- Acceptance: admin can generate token/package; agent uses token to register and is stored with `tenant_id`.

3) Auth: OIDC + SAML + local login (High priority)
- Why: secure access to tenant admin screens and SSO for customers.
- What: add OIDC + SAML provider support plus local username/password fallback; session management; mapping of SSO users to tenants/roles.
- Acceptance: OIDC login + local login flows work; admins can map users to tenants/roles.

4) Tenant management UI (Customers screen) (Medium priority)
- Why: UX and admin operations; ties UI to data model.
- What: customers list + create tenant + actions (generate join token, view devices).
- Acceptance: UI lists tenants and can create/generate tokens.

5) Server middleware & RBAC (Medium priority)
- Why: secure enforcement of tenant boundaries.
- What: middleware to enforce tenant-scoped queries and role checks on handlers and WebSocket operations.
- Acceptance: cross-tenant access blocked; role-based actions enforced.

6) Agent changes: persist tenant + token refresh (Medium priority)
- Why: agent-level support for tenancy and secure periodic auth.
- What: agent stores tenant_id locally, accepts join tokens, supports refresh/rotation.
- Acceptance: agent registers with tenant_id persisted and continues uploads.

7) Tests, CI, Docs, Security Review, Feature Flags (ongoing)
- Add automated tests for migrations, registration, tenant enforcement.
- Update docs with migration and rollout steps.
- Conduct quick threat model and implement mitigations (token expiry, rate-limits).
- Add feature flags to gate tenancy and SSO during rollout.

Implementation notes & patterns
- DB migrations: use `golang-migrate` and keep migrations in `server/storage/migrations/`.
- Tokens: issue short-lived join tokens (JWT or opaque + hashed storage) and exchange for agent bearer token at registration.
- Auth: use `go-oidc` for OIDC and `crewjam/saml` for SAML; provide mapping rules (email domain/group -> tenant) and an invite flow for explicit mappings.
- Backups: always make a DB backup before migrations. For SQLite, copy file; for SQL servers use `pg_dump` or `mysqldump`.
- Feature flags: add `tenancy.enabled` configuration so installs can migrate safely.

Short-term milestones (2-week slices)
- Week 1: Add DB migration scaffolding + `tenants` migration; implement join-token DB model and server API skeleton.
	- Note: initial migration filename created in this work: `server/storage/migrations/0001_create_tenants.sql`.
- Week 2: Implement agent registration with join token + persist tenant_id; basic `Customers` API and minimal UI stub to generate tokens.
- Week 3: Add auth middleware + local login flow; session support and simple tenant mapping UI.
- Week 4: Add OIDC + SAML provider support (start with OIDC), RBAC enforcement, and tests.

Rollout plan
- Beta: enable tenancy behind feature flag and test on staging with real-ish datasets (copy DB) and multiple tenants.
- Migration: provide migration CLI that backs up DB and runs migrations.
- GA: enable tenancy by default in new installs; provide migration docs and an assisted migration tool for customers.

Open questions / decisions to make
- Token format: JWT (stateless) vs opaque tokens (store hash). Recommendation: signed JWT + server-side token_id record for audit & revocation.
- Which DB to promote as production default? Recommendation: Postgres for new production deployments; keep SQLite for single-node installs.
- SAML: do we support in-process SAML or recommend a proxy/IdP integration first? Recommendation: implement OIDC first, add SAML via crewjam/saml later.

Next action (I will take if you confirm)
- Create `server/storage/migrations/0001_create_tenants.sql` and add a small `server/internal/db` helper to pick a DB driver from config. Run quick compile checks.

History
- See `docs/PROJECT_STRUCTURE.md`, `docs/AGENT_UPLOAD_ARCHITECTURE.md` and `docs/WEBSOCKET_PROXY.md` for current design references.

---

End of Tenancy Roadmap.
