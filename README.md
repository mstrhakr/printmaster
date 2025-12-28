# PrintMaster

**Printer & Copier Fleet Management**

[![CI](https://github.com/mstrhakr/printmaster/actions/workflows/ci.yml/badge.svg)](https://github.com/mstrhakr/printmaster/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/mstrhakr/printmaster)](https://github.com/mstrhakr/printmaster/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/mstrhakr/printmaster)](https://goreportcard.com/report/github.com/mstrhakr/printmaster)
[![Docker](https://ghcr-badge.egpl.dev/mstrhakr/printmaster-server/latest_tag?trim=major&label=latest)](https://github.com/mstrhakr/printmaster/pkgs/container/printmaster-server)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

PrintMaster automatically discovers and monitors network printers and copiers. Built for MSPs, MPS providers, copier dealers, and IT departments managing print fleets.

## Features

- **Automated Discovery** — SNMP-based network scanning finds printers automatically
- **Fleet Monitoring** — Track page counts, toner levels, and device status
- **Multi-Site Support** — Central server aggregates data from distributed agents
- **Remote Access** — WebSocket proxy to access agent UIs and printer admin pages
- **Real-Time Updates** — WebSocket heartbeat with automatic HTTP fallback
- **Cross-Platform** — Windows, Linux, macOS, Docker

## Quick Start

### Server (Docker)

```bash
docker run -d \
  --name printmaster-server \
  -p 9090:9090 \
  -v printmaster-data:/var/lib/printmaster/server \
  -e ADMIN_PASSWORD=your-password \
  ghcr.io/mstrhakr/printmaster-server:latest
```

Access at `http://localhost:9090` — Login: `admin` / your password

### Agent (Windows)

Download the MSI from [Releases](https://github.com/mstrhakr/printmaster/releases), or:

```powershell
# Install as service
.\printmaster-agent.exe --service install
.\printmaster-agent.exe --service start
```

Access at `http://localhost:8080`

### Agent (Linux)

```bash
# Debian/Ubuntu
echo "deb [trusted=yes] https://mstrhakr.github.io/printmaster stable main" | \
  sudo tee /etc/apt/sources.list.d/printmaster.list
sudo apt-get update && sudo apt-get install -y printmaster-agent
```

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent     │────────▶│   Server    │◀────────│   Agent     │
│   Site A    │  API    │  (Central)  │  API    │   Site B    │
└─────────────┘         └─────────────┘         └─────────────┘
      ↓                       ↓                       ↓
  Printers              Web Dashboard            Printers
```

**Agent**: Runs at each site, discovers printers via SNMP, stores data locally, optionally reports to server. Can run standalone.

**Server**: Central hub for multi-site management. Aggregates device data, provides fleet dashboard, enables remote access via WebSocket proxy.

## Documentation

| Guide | Description |
|-------|-------------|
| [Installation](docs/INSTALL.md) | Complete setup instructions for all platforms |
| [Getting Started](docs/GETTING_STARTED.md) | First steps after installation |
| [Features](docs/FEATURES.md) | Detailed feature documentation |
| [Configuration](docs/CONFIGURATION.md) | All configuration options |
| [Troubleshooting](docs/TROUBLESHOOTING.md) | Common issues and solutions |
| [FAQ](docs/FAQ.md) | Frequently asked questions |

Developer documentation is in [docs/dev/](docs/dev/).

## Configuration

**Connect agent to server** — edit `config.toml`:

```toml
[server]
enabled = true
url = "http://your-server:9090"
agent_name = "Office A"
```

**Customize SNMP settings**:

```toml
[snmp]
community = "public"
timeout_ms = 2000
retries = 1
```

See [Configuration Guide](docs/CONFIGURATION.md) for all options.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License — see [LICENSE](LICENSE)

## Links

- [Releases](https://github.com/mstrhakr/printmaster/releases)
- [Issues](https://github.com/mstrhakr/printmaster/issues)
- [Discussions](https://github.com/mstrhakr/printmaster/discussions)

