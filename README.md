# PrintMaster

PrintMaster is a cross-platform printer/copier fleet management system implemented in Go. It consists of two components:

- **Agent** - Discovers network printers, collects device metadata (model, serial, life counters) via SNMP, and can run standalone or report to server
- **Server** - Central hub for managing multiple agents, aggregating data, reporting, and alerting

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
	- `docs/SCAN_PIPELINE.md` — scan pipeline and stage design
	- `docs/RANGE_SYNTAX.md` — range editor formats and parser behavior
	- `docs/API_AND_UI.md` — endpoint reference and UI behavior
	- `docs/DECISIONS.md` — design decisions and rationale

Quick start (developer)
1. Install Go 1.21+
2. Build components:

```powershell
# Build agent (standalone mode)
.\build.ps1 agent

# Build server (central hub)
.\build.ps1 server

# Build both
.\build.ps1 both
```

3. Run standalone agent:
```powershell
cd agent
.\printmaster-agent.exe -port 8080
# Open http://localhost:8080
```

4. Or run agent + server:
```powershell
# Terminal 1: Start server
cd server
.\printmaster-server.exe -port 9090

# Terminal 2: Start agent (configure to send to server)
cd agent
.\printmaster-agent.exe -port 8080
```

Notes
- The agent runs scans asynchronously. Use the web UI or the `/scan_status` endpoint to poll progress.
- User ranges are saved to `config.json` and limited by a safe default expansion cap (4096 addresses).

Contributing
- Follow Go formatting (gofmt) and add tests for new features. See `docs/` for design notes and planned next steps (producer/consumer scan pipeline, SNMP quick probes, mDNS/SSDP discovery, job control).

