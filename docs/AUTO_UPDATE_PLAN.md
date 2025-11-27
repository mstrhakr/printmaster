# Auto-Update & Installer Repackaging Plan

This document captures the agreed strategy for server- and agent-driven updates, including manifest signing, installer repackaging, and rollout choreography. It serves as a checklist we can iterate through incrementally.

## Goals

- Enable the PrintMaster **server** to self-update without user intervention (non-Docker deployments).
- Allow the server to orchestrate **agent** updates, including fully background installations triggered from the fleet UI.
- Provide **customized installers** (per fleet/tenant) that embed configuration data and can be used for both onboarding and update flows.
- Maintain **trust** without external code-signing certificates by using server-signed manifests and checksum validation.
- Keep Docker deployments opt-in / manual since they already rely on image pulls.

## High-Level Architecture

1. **Release Intake**
   - Server polls the authoritative release feed (GitHub or artifact bucket) for new agent/server versions.
   - Downloaded artifacts are verified with upstream checksums and cached per platform.

2. **Manifest Signing**
   - Server maintains an Ed25519 signing key pair.
   - For each cached version the server emits a manifest describing version, platform, SHA-256 hash, and supported minor line.
   - Agents embed the public key; all update/install downloads must match a signed manifest.

3. **Installer Repackaging**
   - When the admin requests an installer, the server unwraps the official artifact, injects fleet-specific config (join tokens, CA path, policy), then repackages it.
   - Resulting artifacts are available via authenticated endpoints (e.g., `/api/v1/installers/{fleet}/{platform}`) and reused by auto-update flows.

4. **Server Self-Update**
   - Non-Docker deployments download the new server build, verify the manifest, stage it, and swap binaries with automatic rollback.
   - Docker deployments are detected (env flag / filesystem marker) and instructed to update through container orchestration instead.

5. **Agent Auto-Update**
   - Agents poll the server (per policy) for new updates and fetch repackaged installers.
   - Updates respect version pinning strategy (major: stay on 0.x, minor: stay on 0.9.x, patch: stay on 0.9.14) unless an admin explicitly initiates an upgrade.
   - The agent stages the new build, replaces the running service, and reports status.

6. **Policy & UX**
   - Fleet setting controls cadence (disabled, daily, weekly, monthly) plus target minor version.
   - Agents can override locally (important for air-gapped installs) but default to fleet policy.
   - Server UI shows per-agent status, current/target versions, manual "Update now" actions, and install links.

## Implementation Checklist

### Phase 1 – Foundations & Policy

- [x] Define fleet-level auto-update settings (cadence, version pinning strategy: major/minor/patch, allow-major-upgrade flag) in server storage schema.
- [x] Add maintenance window scheduling (time-of-day preferences, timezone support) to avoid business-hour disruptions.
- [x] Add rollout control settings (staggered deployment, max concurrent updates, jitter, emergency abort flag) to prevent bandwidth saturation.
- [x] Add agent-side configuration fields for local override, defaulting to fleet settings when connected.
- [x] Expose settings in server admin UI + API.
- [x] Add and validate tests for this phase.

### Phase 2 – Release Intake & Manifests

- [x] Implement server job to fetch official release metadata + artifacts for each supported platform.
- [x] Fetch and cache release notes/changelogs alongside artifacts for UI display and audit logs.
- [x] Store artifacts in a versioned cache with integrity data (SHA-256, upstream signature if available).
- [x] Introduce manifest-signing module (Ed25519 key generation, rotation, storage) and embed public key in agent + server binaries.
- [x] Provide CLI/admin endpoints to rotate signing keys and regenerate manifests.
- [x] Add and validate tests for this phase.

**Phase 2 Notes:**
- Server exposes `/api/v1/releases/signing-keys` (list/rotate) and `/api/v1/releases/manifests` routes gated behind `releases.read/release.write` scopes.
- Release intake worker now ensures manifests are generated for every cached artifact; regeneration re-signs existing manifests on key rotation.
- Tests cover storage schema v5, manager rotation/regeneration, and HTTP handlers to prevent regressions.

### Phase 3 – Installer Repackaging Service

- [ ] Build packager that unpacks cached release, injects fleet config (join token, CA path, policy), and repacks per OS (ZIP/TAR/MSI wrapper).
- [x] Ensure sensitive data (tokens) are encrypted at rest within server cache.
- [ ] Add authenticated download endpoints for the customized installers + raw update bundles.
- [ ] Surface "Download installer" button in server UI referencing those endpoints.
- [ ] Add and validate tests for this phase.

### Phase 4 – Server Self-Update (Non-Docker)

- [ ] Add component that checks for new server version respecting target minor.
- [ ] Download + verify manifest/hash, stage binary, back up current version.
- [ ] Integrate with Windows service + Linux systemd to perform controlled restart and rollback on failure.
- [ ] Detect Docker environments and disable automated self-update, displaying guidance instead.
- [ ] Record and expose self-update history/status in UI/logs.
- [ ] Add and validate tests for this phase.

### Phase 5 – Agent Auto-Update Worker

- [ ] Implement agent background worker honoring fleet/local cadence and maintenance windows.
- [ ] Pre-flight checks: verify sufficient disk space for staging + backup before starting download.
- [ ] Request manifest + download from server with exponential backoff retry (max attempts configurable), verify signature/hash, stage update safely.
- [ ] Support HTTP range requests for partial download resume on interrupted transfers.
- [ ] Use existing install scripts (PowerShell/service manager) to replace binaries and restart.
- [ ] Post-update health check: verify server connectivity and basic functionality after restart; trigger rollback if checks fail.
- [ ] Keep previous version for rollback; automatically retry on transient failures.
- [ ] Report progress and telemetry to server (e.g., pending/downloading/installing/restarting/done, download time, success/failure) for UI consumption and metrics.
- [ ] Add and validate tests for this phase.

### Phase 6 – UI & Operational UX

- [ ] Server UI: dashboard widgets showing current vs target versions, rollout status, and manual update controls.
- [ ] Display cached release notes/changelogs for pending updates before admin approval.
- [ ] Implement staggered rollout UI controls (percentage/batch size, delay between waves, emergency abort button).
- [ ] Add telemetry dashboard showing update success rate, average download time, rollback frequency per version.
- [ ] Agent UI: settings page showing current policy, next scheduled check, and last update result (read-only unless override enabled).
- [ ] Notification/log integration (e.g., toasts, audit log entries) for update events with changelog snippets.
- [ ] Add and validate tests for this phase.

### Phase 7 – Testing & Rollout

- [ ] Unit/integration tests for manifest signing, download verification, and packaging logic.
- [ ] End-to-end tests (possibly via CI) that spin up server + agent, trigger update, and assert version change.
- [ ] Documentation updates (admin guide, deployment notes) covering new features.
- [ ] Gradual rollout plan (beta fleet, staged deployment) before enabling for all installations.
- [ ] Add and validate tests for this phase.

## Edge Cases & Notes

- **Docker/Kubernetes**: provide environment flag (`PM_DISABLE_SELFUPDATE=1`) and UI hints; rely on container image updates.
- **Schema migrations**: auto-updater must confirm compatibility before applying a version with DB changes (migration gating and backups).
- **Offline agents**: if unable to reach server, fall back to local override schedule or postpone until connectivity returns.
- **Security posture**: regular key rotation, audit of downloaded artifacts, and strict auth on installer endpoints are mandatory.
- **Rollback triggers**: define explicit criteria for automatic rollback (service start failure, post-update health check failure, connectivity loss).
- **Bandwidth management**: staggered rollout with jitter prevents simultaneous downloads from overwhelming server/network; configurable per fleet.
- **Maintenance windows**: respect local time zones and business hour preferences; defer updates outside configured windows.
- **Network resilience**: exponential backoff with jitter for retries; support HTTP range requests to resume interrupted downloads.
- **Disk constraints**: pre-flight disk space checks prevent partial installs; alert admins when agents lack sufficient space.
- **Version control**: admins can pin to major, minor, or specific patch versions for validation/testing before fleet-wide rollout.
- **Telemetry feedback loop**: track success rates and download metrics to identify problematic releases early and inform rollout decisions.

This plan should be treated as a living document; check off tasks as they land and adjust phases as we learn more from early prototypes.
