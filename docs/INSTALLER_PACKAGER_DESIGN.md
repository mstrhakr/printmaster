# Installer Packager Design (Phase 3)

This document captures the detailed plan for the Phase 3 work stream: server-side installer repackaging. It focuses on Windows MSI installers first (highest demand) plus other low-friction formats we can support with minimal effort (ZIP bundles, generic tarballs). Debian/apt deliverables are deferred until we invest in a full repository publishing flow.

## Goals

1. Allow admins to download tenant-scoped installers directly from the server UI/API without rebuilding artifacts locally.
2. Reuse the Phase 2 release cache (artifacts + manifests) so repackaging never hits external networks once the upstream binaries are cached.
3. Embed fleet-specific configuration (join token, CA chain, policy defaults) into the installer in a secure, tamper-resistant way.
4. Produce repeatable, auditable outputs with metadata stored alongside the cached artifacts so we can regenerate or revoke installers deterministically.
5. Keep implementation incremental: start with Windows MSI (existing build chain) and generic archive-based installers (ZIP/TAR). Defer `.deb`/apt until we implement full repo signing and distribution.

## Scope & Priorities

| Priority | Format | Notes |
| --- | --- | --- |
| Deferred | **Windows MSI** | CI now ships signed MSI artifacts, but server-side repackaging remains blocked on a robust Go-native toolchain. We'll revisit once phase-three archives and UI are complete. |
| P1 | **Windows ZIP (portable)** | Useful for labs/POCs without MSI rights. Simple file overlay + config drop, minimal tooling. |
| P1 | **Generic TAR.GZ** | Works for manual Linux/macOS installs until we have native packages; same overlay approach as ZIP. |
| Deferred | Debian `.deb` | Requires editing `control`/maintainer scripts, `dpkg-deb` tooling, and apt metadata signing. Keep notes in doc but no code yet. |

## Configuration Payload

- Canonical format: `config.toml` pre-populated with tenant defaults + join token, plus optional `ca.pem` bundle if the tenant uploaded a custom CA.
- Packager will render a JSON summary (metadata) stored in the DB so admins can audit what secrets were embedded.
- Join tokens should be short-lived; packager will request/refresh a one-time token when building artifacts to reduce blast radius.

## Architecture Overview

```
 Release Cache (Phase 2)              Installer Packager (new)
 ┌─────────────────────────┐          ┌───────────────────────────────┐
 │ release_artifacts table │  ----▶  │ packager.Manager              │
 │ cached MSI/ZIP/TAR bits │         │  • loads artifact blob        │
 │ manifests + signatures  │         │  • stages temp workspace      │
 └─────────────────────────┘         │  • injects config payload     │
                                     │  • rehydrates installer       │
                                     │  • stores packaged result     │
                                     └───────────────────────────────┘
```

### Key Components

1. **Packager Manager (server/packager/manager.go)**
   - API: `BuildInstaller(ctx, tenantID, component, version, platform, arch, format)`.
   - Dependencies: storage.Store (Phase 2 schema), release manifest manager (for signing metadata), tenancy service (fetch tenant config + CA), join token service.
   - Responsibilities: orchestrate staging temp directories, call format-specific builders, persist output metadata + cache file path.

2. **Format Builders (interface per format)**
   - `MSIBuilder`: Uses WiX `dark`/`light` style approach? (Exploding MSI with `lessmsi`, injecting config, re-light?). Instead we'll embed config in a CAB subdirectory the agent already looks at (`config/bootstrap.toml`). Validate service install scripts still pick it up.
   - `ZipBuilder` / `TarBuilder`: Simple overlay plus optional script injection to copy config after unzip.

3. **Cache Storage**
   - New table `installer_bundles` (planned) storing: tenant_id, component, version, platform, arch, format, source_artifact_id, config_hash, path, size, created_at, expires_at.
   - Files live under `server/cache/installers/{tenant}/{component}/{version}/...` with random suffix.

4. **API Layer**
   - `/api/v1/installers/{component}/{version}/{platform}/{arch}` with query params: `format=msi|zip|tar`, `tenant_id` (implicit via auth if single-tenant admin).
   - Authorization: reuse RBAC (new `installers.read/installers.write` actions or reuse `tenants.write`). TBD in design doc.

## Workflow Details

### MSI Repackaging

1. Fetch cached MSI (Phase 2 ensures SHA256 + manifest already present).
2. Extract MSI using `lessmsi` (pure Go wrapper or shell out) into temp dir.
3. Drop `config/bootstrap.toml` + `ca.pem` + `metadata.json` into a known directory the agent reads on first launch.
4. Update any MSI tables necessary to ensure the files install to the right path (likely `File`, `Component`, `Directory` tables). We'll script this using `lessmsi` library (or fallback to `msidb` if available). Goal: no manual WiX editing.
5. Rebuild MSI and update summary info (size, sha256). We do **not** re-sign the MSI with Authenticode; instead we rely on manifest signature (server trust root). Future work may integrate SignTool but not required for private fleets.
6. Store packaged MSI + metadata.

### MSI Artifact Production (CI Source)

- **Why**: The server packager can only customize installers that already exist in the release cache. We now build the “official” MSI during tagged CI runs so Phase 3 always has a canonical source artifact.
- **Toolchain**: Use WiX Toolset on the Windows GitHub Actions runner (pure Go requirement applies to runtime; CI can rely on WiX binaries). We keep a checked-in `.wxs` template under `build/windows/msi/` that declares the installation layout, service registration custom actions, and config placeholders.
- **Pipeline steps**:
   1. Extend `build-binaries` job with a Windows-specific step that installs WiX (download zip, add to PATH) before agent build.
   2. Run `go build` to produce the release `printmaster-agent.exe` (as today), then invoke `candle.exe` and `light.exe` against the `.wxs` file to emit `printmaster-agent-v<version>-windows-amd64.msi`.
   3. Wire the MSI output into the existing artifact upload block so releases publish `.exe` **and** `.msi` assets with SHA256 in manifests.
   4. Update the release-intake worker (Phase 2) to recognize `.msi` files and register them in `release_artifacts` so the packager manager can find them.
- **Customization hooks**: The WiX template includes `Component` entries for the agent binary, bootstrap/config directory skeleton, optional CA bundle, and defines `CustomAction`s that call `printmaster-agent.exe --service install`/`--service uninstall`. Future MSI options (e.g., install directory overrides, proxy defaults) become WiX properties that the server packager can manipulate when repackaging.
- **Integrity**: CI signs manifests for the MSI like any other artifact; no Authenticode signing yet.

### ZIP / TAR Builders

- Much simpler: decompress artifact, copy config files, recompress.
- Ensure file permissions (especially for TAR) survive round-trip; use `archive/tar` w/ metadata or shell out to `tar` when on Linux builder.
- Because these formats aren’t “installers,” we’ll provide helper script (`install.ps1`/`install.sh`) that reads the embedded config and installs the agent service. Include instructions in metadata.

### Security Considerations

- Sensitive fields (join token) should be encrypted at rest. Options:
  1. Encrypt serialized config blob in DB using existing server KMS (if available) or libsodium sealed boxes with server secret.
  2. On disk, rely on restricted file system ACLs plus short expiration; still keep checksum for tamper detection.
- Access control: only tenant-scoped admins with `tenants.write` (or new `installers.write`) can request new bundles; `installers.read` for download.
- Audit logging: log every request that generates or downloads a packager output with actor + tenant context.

### Operational Considerations

- **Expiration / Cleanup**: packaged installers should expire after configurable TTL (default 7 days). Background job reaps expired bundles from disk + DB.
- **Idempotency**: identical requests (same tenant, version, format, config hash) should reuse cached bundle if still valid; store `config_hash` to detect changes.
- **Telemetry**: store size, build duration, builder version to spot regressions.

## Deferred Debian/Apt Work

- Requires editing `control` metadata, maintainer scripts, and using `dpkg-deb` to rebuild packages.
- Must publish apt repository metadata (Release, Packages, InRelease) signed with GPG.
- Needs systemd unit install scripts + postinst hooks.
- Until we commit to hosting apt repos, we keep `.deb` work out of scope to avoid semi-supported artifacts.

## Next Steps

1. Finalize schema changes (`installer_bundles` table + API RBAC actions).
2. Implement packager manager + format builders (ZIP + TAR.GZ first; MSI deferred to a later phase).
3. Expose download/build APIs with proper auth.
4. Add cleanup worker and telemetry instrumentation.
5. Revisit Debian/apt scope in a future phase once repository tooling is ready.

### Current Status (Nov 2025)

- CI/CD now produces canonical MSI installers, but we still lack a maintainable Go-native repackager. Rather than shelling out to WiX/lessmsi and reworking Authenticode/signing flows, MSI customization is formally deferred to the follow-on phase.
- Archive-based ZIP/TAR builders are live on the server: the packager manager is initialized at startup, installers are cached/cleaned automatically, and the **Add Agent** UI now offers script _and_ archive generation (platform, format, and architecture selectors) with inline download links/metadata.
- RBAC: `packages.generate` is granted to admins and operators by default so helpdesk teams can produce archives without full admin rights; viewers remain read-only.
