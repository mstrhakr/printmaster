## Project Structure (Overview)

```
printmaster/
├── agent/                        # Agent service and embedded UI (Go)
│   ├── main.go                   # Agent entrypoint
│   ├── config.go / config.toml   # Agent configuration loader + sample
│   ├── agent/                    # Discovery + protocol workers (mDNS, SSDP, etc.)
│   ├── scanner/                  # SNMP pipeline, vendor registry, metrics extraction
│   ├── storage/                  # Embedded SQLite schema + migrations
│   ├── proxy/ / supplies/        # Proxy tunnel + consumables helpers
│   ├── featureflags/             # Toggle definitions used by agent UI
│   ├── web/                      # Embedded UI assets (built into the binary)
│   └── docs/                     # Agent-specific reference material
├── server/                       # Multi-agent server/API hub (Go)
│   ├── main.go                   # Server entrypoint
│   ├── config.go / config.toml   # Server configuration + defaults
│   ├── websocket.go              # Agent tunnel / live proxy handling
│   ├── storage/                  # Server persistence + tenancy data
│   ├── authz/, logger/, tls.go   # Auxiliary subsystems
│   └── web/                      # Server UI + static assets
├── common/                       # Shared Go modules (config, logger, snmp, util, web)
├── docs/                         # Architecture + operations documentation
├── dev/                          # Local developer scripts (e.g., launch.ps1)
├── scripts/                      # Operational PowerShell helpers (kill, update, etc.)
├── tools/                        # Standalone utilities and generators
├── tests/                        # Integration / e2e test harnesses
├── static/                       # Third-party front-end assets (e.g., flatpickr)
├── ui/                           # Legacy UI experiments (kept for reference)
├── logs/, test-results/          # Output folders (ignored in git)
├── build.ps1 / release.ps1       # Build + release orchestration
└── README.md                     # High-level overview
```

## Module Descriptions

### Agent (`agent/`)
Single-binary agent that discovers printers, collects metrics, persists to SQLite, and serves the embedded UI. Key subpackages:

- `agent/agent/`: discovery workers (TCP probing, mDNS, SSDP, WS-Discovery, SNMP traps, range parsing).
- `agent/scanner/`: multi-stage SNMP scan pipeline and vendor-specific profiles.
- `agent/storage/`: schema v8+, metrics tiering, configuration/state persistence.
- `agent/web/`: React/HTMX UI bundled via `//go:embed`.

### Server (`server/`)
Central service coordinating multiple agents, providing RBAC, WebSocket tunneling, and consolidated UI/APIs. Uses its own SQLite database and mirrors the agent’s config model via `config.toml`.

### Common (`common/`)
Shared Go modules consumed by both binaries (config loader, logger, SNMP abstractions, settings helpers, shared web components, WebSocket utilities). This keeps cross-cutting concerns in one place.

### Documentation (`docs/` and `agent/docs/`)
`docs/` holds architecture, roadmap, deployment, and operator guides. `agent/docs/` contains deep dives (MIB profiles, SNMP research) that are specific to the agent runtime.

### Tooling & Automation
- `build.ps1`, `release.ps1`, `status.ps1`, `version.ps1`: primary automation entrypoints.
- `dev/launch.ps1`: developer convenience launcher (tests + run agent).
- `package.json` + `playwright.config.js`: UI tests (Jest/Playwright).
- `.vscode/tasks.json` (generated) + VS Code tasks listed in `BUILD_WORKFLOW.md`.

### Tests & Fixtures
- Go unit tests live beside their packages.
- `tests/` (with supporting `test-results/`) will hold cross-component or UI regression suites.
- `common/web/__tests__` contains Playwright specs invoked via `npm run test:playwright`.

## Architecture Principles (Current)

- **Separation of Concerns**: discovery vs. SNMP pipeline vs. persistence vs. UI.
- **Shared foundations**: anything reusable lives in `common/` to keep agent/server parity.
- **Context-aware operations**: network calls propagate `context.Context` for cancellation.
- **Embedded assets**: both binaries embed their UI/static files to stay single-file deployable.

Data flow (agent):

```
Discovery (agent/agent) → Candidate hosts → SNMP pipeline (agent/scanner)
    → Vendor parsers → Metrics/device records → SQLite (agent/storage) → UI/API
```

## Development Workflow (Quick Reference)

- Build: `./build.ps1 agent`, `./build.ps1 server`, or `./build.ps1 both`.
- Test: `cd agent && go test ./...`, `cd server && go test ./...`, or run VS Code “Test: Agent/Server”.
- Release: `./release.ps1 agent|server|both <patch|minor|major>` (handles VERSION bumping, tagging, artifacts).
- Debug: `./dev/launch.ps1` for the agent; server tasks defined in `.vscode/launch.json`.

## Related Documentation

- [Agent README](../agent/README.md)
- [Server README](../server/README.md)
- [Configuration Guide](CONFIGURATION.md)
- [Build Workflow](BUILD_WORKFLOW.md)
- [Roadmap](ROADMAP.md)
- [Deployment Guides](SERVICE_DEPLOYMENT.md, AGENT_DEPLOYMENT.md)
- [Storage Schema Notes](DATABASE_ROTATION.md)

> Need a document not listed here? Check `docs/README.md` (coming soon) for a living index of authoritative references.
