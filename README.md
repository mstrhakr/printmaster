# PrintMaster

**Printer/Copier Fleet Management System**

[![CI/CD](https://github.com/mstrhakr/printmaster/actions/workflows/ci-cd.yml/badge.svg)](https://github.com/mstrhakr/printmaster/actions/workflows/ci-cd.yml)
[![Docker](https://ghcr-badge.egpl.dev/mstrhakr/printmaster-server/latest_tag?trim=major&label=latest)](https://github.com/mstrhakr/printmaster/pkgs/container/printmaster-server)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.24-blue.svg)](https://go.dev/)

PrintMaster is a cross-platform printer/copier fleet management system built for **MSPs**, **MPS providers**, **copier dealers**, and **IT departments** managing large print fleets. Implemented in Go with automated discovery and centralized monitoring.

## Features

- **🔍 Automated Discovery** - SNMP-based network scanning with intelligent device detection
- **📊 Fleet Monitoring** - Track device status, counters, supplies across all sites
- **🏢 Multi-Site Support** - Central server aggregates data from distributed agents
- **⚡ Real-Time Communication** - WebSocket-based live heartbeat for instant status updates
- **🌐 WebSocket Proxy** - Access agent UIs and device web interfaces through secure WebSocket tunnels
- **🐳 Docker Ready** - Container images for easy deployment (Unraid, Docker Compose)
- **🌐 Web UI** - Modern interface for configuration and monitoring
- **📈 Scalable** - Tested with large deployments (1000+ devices)
- **🔐 Secure** - Optional TLS, token authentication, reverse proxy support
- **🚀 Fast** - Concurrent SNMP queries, efficient SQLite storage

## Who This Is For

- **Managed Service Providers (MSPs)** - Monitor client print infrastructure across multiple sites
- **Managed Print Services (MPS)** - Track usage, supplies, and service needs proactively  
- **Print Solutions Providers / Copier Dealers** - Monitor devices under maintenance agreements, integrate with PaperCut, provide tier-2 support
- **IT Departments** - Manage corporate print fleets with automated discovery and reporting

## Components

- **Agent** - Discovers network printers, collects device metadata (model, serial, life counters) via SNMP, can run standalone or report to server
- **Server** - Central hub for managing multiple agents across sites, aggregating data, reporting, and alerting

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent     │────────▶│   Server    │◀────────│   Agent     │
│   Site A    │  API v1 │  (Central)  │  API v1 │   Site B    │
└─────────────┘         └─────────────┘         └─────────────┘
```

## Where to look
- `agent/` — the Go agent and web UI server (single binary, can run standalone)
- `server/` — the Go server for multi-agent management (central hub)
- `docs/` — detailed design notes, scanning pipeline, range syntax, and API/UI mapping. Key files:
	- `docs/BUILD_WORKFLOW.md` — **build, test, and release procedures** ⭐
	- `docs/SCAN_PIPELINE.md` — scan pipeline and stage design
	- `docs/RANGE_SYNTAX.md` — range editor formats and parser behavior
	- `docs/API_AND_UI.md` — endpoint reference and UI behavior
	- `docs/DECISIONS.md` — design decisions and rationale

## Quick Start

### Using Docker (Recommended)

**Run Server:**
```bash
docker run -d \
  --name printmaster-server \
  -p 9090:9090 \
  -v printmaster-data:/var/lib/printmaster/server \
  -v printmaster-logs:/var/log/printmaster/server \
  -e BEHIND_PROXY=false \
  -e LOG_LEVEL=info \
  ghcr.io/mstrhakr/printmaster-server:latest
```

Access UI at `http://localhost:9090`

**For Unraid:** See [docs/UNRAID_DEPLOYMENT.md](docs/UNRAID_DEPLOYMENT.md)

**For Docker Compose:** See [server/docker-compose.yml](server/docker-compose.yml)

### For Developers

**Prerequisites:**
- Go 1.24+
- Git

**Build from source:**

```powershell
# Clone repository
git clone https://github.com/mstrhakr/printmaster.git
cd printmaster

# Build agent (standalone mode)
.\build.ps1 agent

# Build server (central hub)
.\build.ps1 server

# Build both
.\build.ps1 both
```

**Run standalone agent:**
```powershell
cd agent
.\printmaster-agent.exe -port 8080
# Open http://localhost:8080
```

**Run standalone agent:**
```powershell
cd agent
.\printmaster-agent.exe
# Open http://localhost:8080
```

**Run agent + server:**
```powershell
# Terminal 1: Start server
cd server
.\printmaster-server.exe

# Terminal 2: Configure and start agent
cd agent
# Edit config.toml: set server.enabled=true, server.url="http://localhost:9090"
.\printmaster-agent.exe
```

## Documentation

- **[Build & Release Guide](docs/BUILD_WORKFLOW.md)** - Development workflow, testing, releases
- **[Unraid Deployment](docs/UNRAID_DEPLOYMENT.md)** - Docker template and setup guide
- **[Docker Deployment](docs/DOCKER_DEPLOYMENT.md)** - Container deployment options
- **[Configuration Guide](docs/CONFIGURATION.md)** - All config options explained
- **[API Reference](docs/API.md)** - REST API documentation
- **[Project Structure](docs/PROJECT_STRUCTURE.md)** - Codebase organization

## Deployment Options

| Method | Best For | Documentation |
|--------|----------|---------------|
| **Docker** | Production deployments | [DOCKER_DEPLOYMENT.md](docs/DOCKER_DEPLOYMENT.md) |
| **Unraid** | Home lab, small business | [UNRAID_DEPLOYMENT.md](docs/UNRAID_DEPLOYMENT.md) |
| **Windows Service** | Windows servers | [SERVICE_DEPLOYMENT.md](docs/SERVICE_DEPLOYMENT.md) |
| **Linux Service** | Linux servers | [agent/printmaster-agent.service](agent/printmaster-agent.service) |
| **Standalone Binary** | Testing, development | Build with `build.ps1` |

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent     │────────▶│   Server    │◀────────│   Agent     │
│   Site A    │  API v1 │  (Central)  │  API v1 │   Site B    │
│             │  WS/HTTP│   SQLite    │ WS/HTTP │             │
│   SQLite    │         │   Docker    │         │   SQLite    │
└─────────────┘         └─────────────┘         └─────────────┘
      ↓                       ↓                         ↓
  Printers              Web UI/API               Printers
```

**Communication:**
- **WebSocket** - Real-time heartbeat with automatic reconnection and HTTP fallback
- **REST API** - Device data upload, metrics sync, agent registration
- **HTTPS** - Optional TLS encryption for secure communication

**Agent Features:**
- SNMP device discovery
- Local SQLite database
- Web UI for management
- WebSocket heartbeat (live status)
- Uploads to server (optional)
- Can run standalone

**Server Features:**
- Aggregates data from agents
- Central reporting dashboard
- Multi-site management
- Real-time agent monitoring via WebSocket
- API for integrations
- Docker-ready

## Real-Time Communication

PrintMaster uses **WebSocket** connections for live agent heartbeat and status updates:

- **Instant Status** - Agents connect via WebSocket for real-time presence detection
- **Auto-Reconnect** - Exponential backoff (5s to 5min) ensures reliable recovery
- **HTTP Fallback** - Seamless degradation to HTTP POST if WebSocket unavailable
- **HTTP Proxy** - Access agent UIs and device web interfaces through WebSocket tunnels (see [docs/WEBSOCKET_PROXY.md](docs/WEBSOCKET_PROXY.md))
- **Efficient** - Single persistent connection reduces bandwidth vs. polling
- **Scalable** - Handles multiple concurrent agent connections

The WebSocket connection is established at `/api/v1/agents/ws` with token authentication. If the WebSocket fails, agents automatically fall back to HTTP heartbeat at `/api/v1/agents/heartbeat` (default: every 60 seconds).

### WebSocket Proxy Feature

Access agent web UIs and device admin pages from anywhere through secure WebSocket tunnels:

- **Remote Access** - Access agents behind NAT/firewalls without port forwarding
- **Device Management** - Open printer/copier web interfaces directly from server UI
- **One-Click Access** - "Open UI" buttons on agent and device cards
- **Transparent** - Works with any HTTP-based device web interface

See [docs/WEBSOCKET_PROXY.md](docs/WEBSOCKET_PROXY.md) for details.

## Where to Look

- `agent/` — Agent binary, SNMP scanning, local web UI
- `server/` — Central server for multi-agent management
- `common/` — Shared libraries (config, logging)
- `docs/` — Comprehensive documentation
- `.github/workflows/` — CI/CD pipeline (automated testing, builds, releases)

## Development

### Build & Release Workflow

```powershell
# Check project status
.\status.ps1

# Make a release (automated: test → build → commit → tag → push)
.\release.ps1 agent patch

# Or use VS Code: Ctrl+Shift+B for build tasks, F5 for debugging
```

### Build & Release Workflow

```powershell
# Check project status
.\status.ps1

# Run tests
.\build.ps1 agent -Test
.\build.ps1 server -Test

# Make a release (automated: version bump → test → build → tag → push → GitHub release)
.\release.ps1 agent patch   # Bug fixes
.\release.ps1 agent minor   # New features
.\release.ps1 both patch    # Release both components

# Or use VS Code tasks: Ctrl+Shift+B for builds, F5 for debugging
```

See **[docs/BUILD_WORKFLOW.md](docs/BUILD_WORKFLOW.md)** for the complete guide.

### Running Tests

```powershell
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test
go test -v -run TestScannerPipeline ./agent/scanner/
```

## Configuration

**Agent** (`agent/config.toml`):
```toml
discovery_concurrency = 50

[snmp]
  community = "public"
  timeout_ms = 2000

[server]
  enabled = true
  url = "https://printmaster.example.com"
  upload_interval_seconds = 300
```

**Server** (`server/config.toml`):
```toml
[server]
  http_port = 9090
  behind_proxy = true  # If using Nginx/Traefik

[database]
  path = "printmaster.db"
```

See example configs: [agent/config.example.toml](agent/config.example.toml) | [server/config.example.toml](server/config.example.toml)

## Contributing

Contributions welcome! Please:

1. **Fork the repository**
2. **Create a feature branch** (`git checkout -b feature/amazing-feature`)
3. **Follow Go conventions** (use `gofmt`, add tests)
4. **Commit your changes** (`git commit -m 'feat: Add amazing feature'`)
5. **Push to branch** (`git push origin feature/amazing-feature`)
6. **Open a Pull Request**

See [docs/TESTING.md](docs/TESTING.md) for testing guidelines.

## Roadmap

- [x] SNMP discovery and device detection
- [x] SQLite storage and metrics history
- [x] Agent-to-server communication
- [x] Docker containerization
- [x] CI/CD pipeline with automated releases
- [ ] Enhanced device capability detection
- [ ] Advanced reporting and dashboards
- [ ] Email/webhook alerting
- [ ] Multi-tenancy support
- [ ] PaperCut integration
- [ ] Prometheus metrics export

See [docs/ROADMAP.md](docs/ROADMAP.md) for detailed plans.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Support

- **Issues:** [GitHub Issues](https://github.com/mstrhakr/printmaster/issues)
- **Discussions:** [GitHub Discussions](https://github.com/mstrhakr/printmaster/discussions)
- **Documentation:** [docs/](docs/)

## Acknowledgments

Built with:
- [Go](https://go.dev/) - Primary language
- [gosnmp](https://github.com/gosnmp/gosnmp) - SNMP library
- [gorilla/websocket](https://github.com/gorilla/websocket) - WebSocket implementation
- [SQLite](https://www.sqlite.org/) - Embedded database
- [Alpine Linux](https://alpinelinux.org/) - Docker base image

