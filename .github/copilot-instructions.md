# Important note for Agents, do NOT write code "bandaids", if we have an issue with code, we should always try to fix or solve the root problem, adding a helper or patch is NOT correct behavior.

# Copilot Instructions for PrintMaster Project

**PrintMaster** is a cross-platform printer fleet management system with distributed agent-server architecture. Agents discover and monitor network printers via SNMP; servers aggregate multi-site data with real-time WebSocket communication.

**Key Documentation**: Always check `docs/` for detailed design docs - particularly `BUILD_WORKFLOW.md`, `PROJECT_STRUCTURE.md`, and `ROADMAP.md`.

---

## Architecture Overview

### Component Structure

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent     │────────▶│   Server    │◀────────│   Agent     │
│   Site A    │  API v1 │  (Central)  │  API v1 │   Site B    │
│  (Go+Web)   │ WebSocket│ (Go+SQLite) │WebSocket│  (Go+Web)   │
└─────────────┘         └─────────────┘         └─────────────┘
```

**Agent** (`agent/`): Standalone binary with embedded web UI, discovers printers via SNMP/mDNS/WS-Discovery, stores locally in SQLite, optionally reports to server
**Server** (`server/`): Central hub for multi-agent management, aggregates devices/metrics, WebSocket proxy for remote device access, web UI for fleet view
**Common** (`common/`): Shared packages (logger, config) used by both components

### Critical Data Flows

**Agent Identity System** (stable UUID + display name):

- Agent generates UUID once on first run → persisted to `{datadir}/agent_id`
- Registration sends both UUID (stable) and name (configurable)
- Proxy URLs use UUID to remain stable when names change
- See `agent/config.go::LoadOrGenerateAgentID()`

**Discovery Pipeline** (3-stage):

1. **Liveness**: Fast TCP port probe (80/443/9100) → filter alive hosts
2. **Detection**: Compact SNMP query (serial number only) → confirm printer
3. **Deep Scan**: Full SNMP walk → collect all device metadata

- See `agent/scanner/pipeline.go` for pool architecture
- See `agent/scanner/detector.go` for stage functions

**Device-to-Metrics Separation** (schema v7+):

- Device table: Identity/network info (Serial, IP, Model, MAC) - relatively static
- Metrics table: Time-series data (PageCount, TonerLevels) - high update frequency
- Downsampling: raw → hourly → daily → monthly (Netdata-style tiering)
- See `agent/storage/sqlite.go` schema and `server/storage/types.go`

**WebSocket Proxy** (server-to-agent tunneling):

- Server proxies HTTP requests through WebSocket to access agent/device UIs
- Prevents infinite loops via `X-PrintMaster-Proxied` meta tag injection

## Copilot / AI Agent Quick Instructions — PrintMaster

Purpose: give an AI coding agent the minimal, project-specific facts to be productive fast.

1) Big picture (where to look)

- Agent: `agent/` (discovery, scanner, storage, web UI). Key: `agent/main.go`, `agent/scanner/`, `agent/storage/sqlite.go`.
- Server: `server/` (WebSocket hub, API, storage). Key: `server/main.go`, `server/websocket.go`, `server/VERSION`.
- Shared: `common/` (config, logger), `docs/` for design decisions.

2) Core patterns and important files

- Multi-stage scanner: `agent/scanner/pipeline.go`, `agent/scanner/detector.go`, vendor profiles in `agent/scanner/vendor/`.
- Interface-based DI: look for `DeviceStore`, `SNMPClient` interfaces to mock/test.
- Use `context.Context` for cancellation across network calls.
- Embedded web UI uses `//go:embed web` and shared CSS in `common/web/shared.go`.

3) Build, test, release (concrete commands)

- Dev build: `.\build.ps1 agent` | server: `.\build.ps1 server` | both: `.\build.ps1 both`
- Quick dev launcher: `pwsh -NoProfile -ExecutionPolicy Bypass .\dev\launch.ps1` (runs tests, builds, starts agent).
- Tests: prefer repository test tooling (VS Code tasks) or `go test ./...` in packages when iterating locally.
- Releases: ALWAYS use `.\release.ps1` (updates `VERSION` and tags). Do not edit `VERSION` files manually.

4) Repo conventions and expectations

- Cross-platform first (Windows/WSL/macOS); SQLite must be pure-Go (`modernc.org/sqlite` used in CI/dev).
- Tests are written to run in parallel: use `t.Parallel()` in table-driven tests; avoid shared mutable state.
- Prefer small, focused changes and add unit tests for parsing/scanner/vendor logic.
- Commit discipline: land each functional slice as soon as it works/tests clean so we never sit on massive uncommitted diffs.

5) Integration points & APIs to reference in PRs

- Agent ↔ Server: WebSocket at `/api/v1/agents/ws` and HTTP heartbeat at `/api/v1/agents/heartbeat` (see `agent/upload_worker.go` and `server/websocket.go`).
- DB schema/migrations: `agent/storage/sqlite.go::initSchema()`.

6) Quick triage checklist for code changes

- Run tests for affected packages (`go test ./pkg -v`) and CI tasks (build + tests).
- Check for `VERSION` changes only via `release.ps1`.
- If changing storage schema, inspect `agent/storage/sqlite.go` and migration handling.

7) Good first examples to inspect

- Add a new vendor OID mapping: see `agent/scanner/vendor/registry.go` + `vendor/*.go`.
- Mocking SNMP: follow `scanner.SNMPClient` usage in `agent/scanner/query.go` and tests.

If anything above is unclear or you'd like a slightly longer version with examples/templates for PR messages, tests, or a checklist, tell me which sections to expand and I'll iterate.

*** End of concise guidance
