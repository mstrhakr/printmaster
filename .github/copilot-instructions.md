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
- See `server/main.go::proxyThroughWebSocket()` and `server/websocket.go`

---

## Cross-Platform Requirements

- **Full cross-platform support is mandatory** (Windows, Mac, Linux)
- Pure Go dependencies only (no CGO) - use `modernc.org/sqlite` not `mattn/go-sqlite3`
- Platform-specific paths: Use `config.GetDataDirectory()` from `common/config/`
- Test on all platforms before major releases

---

## Code Organization & Best Practices

### Module Structure
- `agent/main.go`: HTTP server, UI, API endpoints (keep lean, extract logic to packages)
- `agent/agent/`: Discovery protocols (mDNS, SSDP, WS-Discovery, SNMP traps, LLMNR, ARP)
- `agent/scanner/`: SNMP querying, 3-stage pipeline, device detection
- `agent/storage/`: SQLite persistence (DeviceStore, AgentConfigStore interfaces)
- `agent/upload_worker.go`: Background worker for server communication
- `server/main.go`: HTTP/WebSocket handlers, agent management
- `server/storage/`: Server-side SQLite (Agent, Device, Metrics tables)
- `server/websocket.go`: WebSocket connection management and proxy protocol

### Key Patterns
- **Interface-based design**: `storage.DeviceStore`, `scanner.SNMPClient` for testability
- **Dependency injection**: Pass interfaces to constructors (see `UploadWorker`, `Scanner`)
- **Context propagation**: Use `context.Context` for cancellation/timeout (all SNMP queries)
- **Embed for UI**: `//go:embed web` for bundling static assets
- **Shared web assets**: `common/web/shared.go` contains CSS constants served by both agent/server (see `docs/SHARED_WEB_ASSETS.md`)
- **Structured logging**: Use `common/logger` with SSE streaming to UI

---

## Testing Guidelines

- Write tests for all major features and maintain high coverage
- Use `t.Parallel()` in tests to run them concurrently when possible
- For table-driven tests or tests with multiple subtests, add `t.Parallel()` to both the parent test and each subtest
- Parallel tests significantly reduce test execution time (e.g., 24s → 6s for agent tests)
- Ensure tests are independent and don't share mutable state when running in parallel

### Running Tests
- **ALWAYS use the `runTests` tool** instead of terminal commands for running tests
- If tests pass or fail, the code already built successfully (compilation verified)
- Use `runTests` with specific file paths to run targeted tests
- Only use terminal for non-test commands (benchmarks, manual builds, etc.)
- The `runTests` tool is pre-approved and won't prompt for user confirmation

### Test Architecture
- `tests/http_api_test.go`: E2E agent registration, heartbeat, device upload
- `tests/websocket_proxy_test.go`: WebSocket proxy flow, error handling, HTML meta injection
- Mock interfaces: `scanner.SNMPClient`, `storage.DeviceStore` for unit tests
- Use `httptest.NewServer()` for HTTP endpoint testing

---

## Build & Release Workflow

### Daily Development
```powershell
# Build agent (dev mode with debug info)
.\build.ps1 agent

# Build server
.\build.ps1 server

# Build both
.\build.ps1 both

# Run all tests
.\build.ps1 test-all

# Check project status (versions, git, processes)
.\status.ps1
```

**Build script features**:
- Automatically injects version, git commit, build time via `-ldflags`
- Dev builds: Full debug info, no stripping
- Release builds: Optimized, stripped symbols
- See `build.ps1` for implementation

**Service Operations**:
- Both agent and server support service installation: `--service install/uninstall/start/stop`
- Use `--quiet` or `-q` flag to suppress informational output during automation (see `docs/QUIET_MODE.md`)
- Errors and warnings still displayed in quiet mode for safety

### VS Code Integration
**Tasks (Ctrl+Shift+B)**:
- Build: Agent/Server/Both (Dev)
- Test: Agent/Server (all)
- Release: Agent/Server/Both Patch/Minor/Major
- Git: Status/Commit/Push/Pull

**Launch Configs (F5)**:
- Debug: Agent (Default Port), Server (Default Port), Agent + Server Together
- All configs auto-kill existing processes via preLaunchTask

### Making Releases
**CRITICAL: Use `release.ps1` for all version bumps - NEVER edit VERSION files manually**

```powershell
# Patch release (0.1.0 → 0.1.1) - Bug fixes
.\release.ps1 agent patch

# Minor release (0.1.0 → 0.2.0) - New features
.\release.ps1 agent minor

# Major release (0.1.0 → 1.0.0) - Breaking changes
.\release.ps1 agent major

# Release both components together
.\release.ps1 both patch
```

**What `release.ps1` does**:
1. ✅ Checks git status (warns if uncommitted changes)
2. ✅ Bumps version in VERSION file(s) using SemVer
3. ✅ Runs all tests (fails if any test fails)
4. ✅ Builds optimized release binary
5. ✅ Commits VERSION file with message "chore: Release agent v0.2.0"
6. ✅ Creates git tag (v0.2.0 for agent, server-v0.2.0 for server)
7. ✅ Pushes commit and tags to GitHub

**Flags**:
- `--DryRun`: Preview without executing
- `--SkipTests`: Skip test execution (NOT recommended)
- `--SkipPush`: Commit/tag locally but don't push

### Git Workflow
**Commit message conventions** (Conventional Commits):
- `feat:` - New features
- `fix:` - Bug fixes
- `docs:` - Documentation only
- `chore:` - Maintenance (version bumps, deps)
- `refactor:` - Code restructuring without behavior change
- `test:` - Test additions/changes

**Copilot should**:
- Suggest `release.ps1` when user asks to "bump version" or "make a release"
- Remind user to commit working changes before releasing
- Use git commands directly for normal commits, but ALWAYS use `release.ps1` for releases
- Check `.\status.ps1` output when unclear about project state

---

## Tool Usage Priority

- **ALWAYS prefer built-in tools over terminal commands**
- Use `runTests` for running Go tests (pre-approved, no user prompt)
- Use `read_file`, `grep_search`, `semantic_search` for code exploration
- Use `replace_string_in_file`, `create_file` for code changes
- Use `get_errors` to check for compilation errors
- Only resort to terminal for:
  - Benchmarks (`go test -bench`)
  - Manual builds when not triggered by tests
  - Non-Go commands (git, npm, etc.)
  - Background processes (servers, watch mode)

### Terminal Usage Guidelines
- **ALWAYS check terminal's current working directory before running commands**
- Use terminal context provided (Cwd field)
- **CRITICAL: Use ONLY absolute paths with `cd` command - NEVER use relative paths**
  - ✅ Correct: `cd C:\temp\printmaster\agent`
  - ❌ Wrong: `cd agent` or `cd ..\agent` or `cd .\storage`
- If you need to change directories:
  1. Check current Cwd from terminal context
  2. Determine full absolute path needed
  3. Use `cd <absolute-path>` with complete path

### Build and Compilation
- **If tests pass or fail, compilation was already successful**
- Don't run separate `go build` commands after successful test runs
- Use `get_errors` tool to check for compile-time errors before running tests
- Test execution implies successful build of the tested package

---

## Database Schema & Migrations

### Current Schema Version: 8 (Nov 2025)
**Key tables**:
- `devices`: Identity/network info (Serial PK, IP, Manufacturer, Model, Hostname, Firmware, MAC, etc.)
- `metrics_history`: Time-series data (Timestamp, Serial FK, PageCount, ColorPages, TonerLevels JSON)
- `metrics_hourly`, `metrics_daily`, `metrics_monthly`: Downsampled aggregates
- `agents` (server): AgentID (UUID) PK, Name (display), Hostname, Token, Platform, Version
- `scan_history`: Audit trail of device state changes

### Migration Strategy
- **Schema migrations are idempotent** - safe to run multiple times
- **Automatic database rotation on failure**: If migration fails, old DB is backed up and new one created
- User notification via UI warnings (see `agent/web/app.js::checkDatabaseRotationWarning()`)
- Count-based backup cleanup (keeps 10 most recent)
- See `agent/storage/sqlite.go::initSchema()` for migration logic

### Key Schema Decisions
- **NO PageCount/TonerLevels in devices table** (removed in schema v7) - moved to metrics_history for time-series
- **IsSaved bool**: Discovered devices can be "saved" to persist across clears
- **Visible bool**: Soft-delete for devices (hidden but not deleted)
- **Agent UUID separation**: agent_id (stable UUID) vs name (user-configurable display name)

---

## Project Structure Reference

```
printmaster/
├── agent/                      # Main Go module (go.mod here)
│   ├── main.go                 # HTTP server, embedded UI, SSE hub
│   ├── upload_worker.go        # Background server sync (heartbeat/upload)
│   ├── agent/                  # Discovery logic (mDNS, SSDP, WS-Discovery, LLMNR, ARP)
│   ├── scanner/                # SNMP pipeline (liveness → detection → deep scan)
│   │   ├── pipeline.go         # 3-stage worker pools
│   │   ├── detector.go         # Detection/deep-scan functions
│   │   ├── query.go            # SNMP query execution
│   │   └── capabilities/       # Device capability detection
│   ├── storage/                # SQLite persistence
│   │   ├── sqlite.go           # Database operations
│   │   ├── interface.go        # DeviceStore/AgentConfigStore interfaces
│   │   └── agent_config.go     # Settings CRUD
│   ├── proxy/                  # WebSocket proxy (agent-side)
│   ├── util/                   # Helpers (parsing, encryption)
│   ├── tools/                  # CLI utilities (MIB walk aggregation, etc.)
│   └── web/                    # Embedded web UI (app.js, styles.css, index.html)
├── server/                     # Server Go module (separate go.mod)
│   ├── main.go                 # HTTP/WebSocket handlers, registration
│   ├── websocket.go            # WebSocket connection management, proxy protocol
│   ├── storage/                # Server-side SQLite (Agent, Device, Metrics tables)
│   │   ├── types.go            # Data structures, Store interface
│   │   └── sqlite.go           # Database operations
│   └── web/                    # Embedded server web UI
├── common/                     # Shared packages (logger, config)
│   ├── logger/                 # Structured logging with SSE streaming
│   └── config/                 # Platform-aware config paths
├── tests/                      # E2E integration tests
│   ├── http_api_test.go        # Agent registration, heartbeat, device upload
│   └── websocket_proxy_test.go # WebSocket proxy flow, HTML injection
├── docs/                       # Comprehensive documentation
│   ├── BUILD_WORKFLOW.md       # Build, test, release procedures ⭐
│   ├── PROJECT_STRUCTURE.md    # Module descriptions
│   ├── ROADMAP.md              # Version milestones, feature plans
│   └── [30+ design docs]
├── build.ps1                   # Build automation
├── release.ps1                 # Release automation
├── status.ps1                  # Project status checker
└── VERSION / server/VERSION    # Semantic version files
```

---

## Contribution Guidelines

- All code should be clean, well-documented, and follow project structure
- New features should include tests and documentation updates
- Update `docs/ROADMAP.md` when completing milestones
- Use `release.ps1` for all version bumps (never manual VERSION edits)
- Commit often with conventional commit messages (`feat:`, `fix:`, `docs:`, `chore:`, `refactor:`)
- Test locally before pushing (`runTests` tool or VS Code tasks)
- Keep `main.go` files lean - extract complex logic to packages

## Documentation Reference

- `docs/BUILD_WORKFLOW.md` - Complete build, test, and release guide ⭐
- `docs/PROJECT_STRUCTURE.md` - Module descriptions and file organization
- `docs/ROADMAP.md` - Version milestones and feature roadmap
- `docs/API.md` - HTTP API reference
- `docs/CONFIGURATION.md` - Config file reference
- `GIT_INTEGRATION_SUMMARY.md` - Git/GitHub integration reference

---

**These instructions ensure the project remains organized, professional, and maintainable. The automated build/release workflow keeps versioning consistent and reduces human error.**
