# Copilot Instructions for PrintMaster

> **Important**: Fix root causes, not symptoms. Do NOT write code "bandaids" or patches—solve the underlying problem.

## Architecture (Agent-Server Fleet Management)

```
Agent (site) ──WebSocket/HTTP──▶ Server (central) ◀── Agent (site)
   │                                   │
   └── SNMP/mDNS discovery             └── Aggregates multi-site data
```

- **Agent** (`agent/`): Single binary + embedded web UI, discovers printers via SNMP, stores in SQLite
- **Server** (`server/`): Central hub, WebSocket proxy to agents, fleet-wide UI
- **Common** (`common/`): Shared packages (logger, config, web assets via `//go:embed`)

### Key Data Flows

| Flow | Key Files | Notes |
|------|-----------|-------|
| Agent Identity | `agent/config.go::LoadOrGenerateAgentID()` | UUID persists to `{datadir}/agent_id`, stays stable when name changes |
| 3-Stage Discovery | `agent/scanner/pipeline.go`, `detector.go` | Liveness (TCP) → Detection (SNMP serial) → Deep Scan (full walk) |
| Device vs Metrics | `agent/storage/sqlite.go`, `interface.go` | Separate tables; metrics use time-series tiering (raw→hourly→daily) |
| Agent↔Server Comms | `agent/upload_worker.go`, `server/websocket.go` | WebSocket at `/api/v1/agents/ws`, HTTP fallback at `/heartbeat` |

## Build & Test Commands

```powershell
.\build.ps1 agent          # Dev build agent
.\build.ps1 server         # Dev build server
.\build.ps1 both           # Build both

# VS Code tasks (Ctrl+Shift+B): "Build: Agent (Dev)", "Test: Agent (all)", etc.

# Run tests
cd agent && go test ./... -v
cd server && go test ./... -v

# Release (NEVER edit VERSION files manually)
.\release.ps1 agent patch  # Bumps version, creates tag
```

## Project Patterns

### Interface-Based DI (for mocking)
Key interfaces to know: `DeviceStore`, `SNMPClient`, `VendorModule`, `Logger`
```go
// agent/storage/interface.go
type DeviceStore interface {
    Get(ctx context.Context, serial string) (*Device, error)
    Upsert(ctx context.Context, device *Device) error
    // ...
}
```

### Vendor SNMP Modules
Add new vendor support in `agent/scanner/vendor/`:
```go
// Implement VendorModule interface
type VendorModule interface {
    Name() string
    Detect(sysObjectID, sysDescr, model string) bool
    BaseOIDs() []string
    MetricOIDs(caps *capabilities.DeviceCapabilities) []string
    Parse(pdus []gosnmp.SnmpPDU) map[string]interface{}
}
```
See `vendor/registry.go` for registration, existing vendors (HP, Kyocera, Epson) for examples.

### Embedded Web Assets
Both binaries embed their UIs via `//go:embed web`. Shared CSS/JS in `common/web/shared.go`.

## Conventions

- **Cross-platform**: Pure-Go SQLite (`modernc.org/sqlite`), no CGO in CI
- **Context propagation**: All network calls use `context.Context` for cancellation
- **Parallel tests**: Use `t.Parallel()` in table-driven tests; avoid shared mutable state
- **Small commits**: Land each functional slice when tests pass—don't accumulate large diffs

## Quick Reference

| Task | Location |
|------|----------|
| Add vendor OID mapping | `agent/scanner/vendor/` (implement `VendorModule`) |
| Modify DB schema | `agent/storage/sqlite.go::initSchema()` |
| Agent↔Server API changes | `agent/upload_worker.go`, `server/websocket.go` |
| Shared web components | `common/web/shared.go` |
| Design docs | `docs/dev/PROJECT_STRUCTURE.md`, `BUILD_WORKFLOW.md` |
