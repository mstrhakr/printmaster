# PrintMaster Documentation

Welcome to PrintMaster, a cross-platform printer/copier fleet management system built for MSPs, MPS providers, copier dealers, and IT departments managing large print fleets.

## Quick Links

| Document | Description |
|----------|-------------|
| [Installation Guide](INSTALL.md) | Complete installation instructions for all platforms |
| [Getting Started](GETTING_STARTED.md) | First steps after installation |
| [Features Guide](FEATURES.md) | Detailed explanation of all features |
| [Configuration](CONFIGURATION.md) | All configuration options explained |
| [Troubleshooting](TROUBLESHOOTING.md) | Common issues and solutions |
| [FAQ](FAQ.md) | Frequently asked questions |

## What is PrintMaster?

PrintMaster consists of two components:

### Agent
A lightweight service that runs at each site to discover and monitor printers. The agent:
- Automatically discovers printers on your network via SNMP
- Collects device information (model, serial number, page counts, toner levels)
- Provides a local web UI for management
- Can run standalone or report to a central server

### Server
A central hub for managing multiple agents across sites. The server:
- Aggregates device data from all connected agents
- Provides a unified fleet view dashboard
- Enables remote access to agent UIs via WebSocket proxy
- Supports multi-tenant deployments

## Architecture Overview

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent     │────────▶│   Server    │◀────────│   Agent     │
│   Site A    │  API    │  (Central)  │  API    │   Site B    │
└─────────────┘         └─────────────┘         └─────────────┘
      ↓                       ↓                       ↓
  Printers              Web Dashboard            Printers
```

## Getting Help

- **Documentation**: Browse the guides linked above
- **Issues**: [GitHub Issues](https://github.com/mstrhakr/printmaster/issues)
- **Discussions**: [GitHub Discussions](https://github.com/mstrhakr/printmaster/discussions)

## For Developers

Looking for technical documentation? See the [Developer Documentation](dev/README.md) for:
- Build instructions
- API reference  
- Architecture details
- Contributing guidelines
