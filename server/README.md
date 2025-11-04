# PrintMaster Server

**Central management hub for PrintMaster fleet management**

The PrintMaster Server aggregates data from multiple PrintMaster agents deployed across networks, providing centralized monitoring, reporting, and alerting.

## Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Agent 1   │────────▶│             │◀────────│   Agent 2   │
│  Site A     │  HTTP   │   Server    │  HTTP   │  Site B     │
│  (Local)    │         │  (Central)  │         │  (Remote)   │
└─────────────┘         └─────────────┘         └─────────────┘
                              │
                              │ SQLite/PostgreSQL
                              ▼
                        ┌─────────────┐
                        │  Database   │
                        │  (All data) │
                        └─────────────┘
```

## Features

- **Multi-Agent Management** - Register and monitor multiple agents
- **Centralized Storage** - All device data and metrics in one place
- **Real-time Monitoring** - Live status from all connected agents
- **Reporting** - Cross-site fleet reports and analytics
- **Alerting** - Notifications for toner low, errors, offline devices
- **Web UI** - Manage entire fleet from browser

## Quick Start

### Build

```powershell
# From project root
cd server
go build -o printmaster-server.exe .
```

### Run

```powershell
# Default ports: HTTP 9090, HTTPS 9443
.\printmaster-server.exe

# Custom port
.\printmaster-server.exe -port 8080

# Custom database path
.\printmaster-server.exe -db C:\data\printmaster\server.db
```

### Configure Agents

Point agents to server in their config:

```ini
# agent/config.ini
[server]
url = http://your-server:9090
agent_id = agent-site-a
upload_interval = 60
```

## API Endpoints

### Agent API (v1)

**Protocol Version**: 1

Agents communicate with server using these endpoints:

#### Register Agent
```http
POST /api/v1/agents/register
Content-Type: application/json

{
  "agent_id": "agent-001",
  "agent_version": "1.0.0",
  "protocol_version": "1",
  "hostname": "agent-host",
  "ip": "192.168.1.100",
  "platform": "windows"
}
```

#### Heartbeat
```http
POST /api/v1/agents/heartbeat
Content-Type: application/json

{
  "agent_id": "agent-001",
  "timestamp": "2025-11-03T18:00:00Z",
  "status": "active"
}
```

#### Upload Devices
```http
POST /api/v1/devices/batch
Content-Type: application/json

{
  "agent_id": "agent-001",
  "timestamp": "2025-11-03T18:00:00Z",
  "devices": [
    {
      "serial": "ABC123",
      "manufacturer": "HP",
      "model": "LaserJet Pro 400",
      "ip": "192.168.1.50",
      ...
    }
  ]
}
```

#### Upload Metrics
```http
POST /api/v1/metrics/batch
Content-Type: application/json

{
  "agent_id": "agent-001",
  "timestamp": "2025-11-03T18:00:00Z",
  "metrics": [
    {
      "serial": "ABC123",
      "page_count": 12345,
      "toner_black": 45,
      "toner_cyan": 78,
      ...
    }
  ]
}
```

## Development Status

**Current Version**: 0.1.0 (Early Development)

### Implemented
- ✅ Basic HTTP server
- ✅ Protocol v1 endpoint scaffolding
- ✅ Version management
- ✅ Health checks

### TODO
- [ ] Database schema (agents, devices, metrics)
- [ ] Agent authentication/authorization
- [ ] Data storage and retrieval
- [ ] Web UI for management
- [ ] Reporting engine
- [ ] Alert system
- [ ] HTTPS with cert management

## Database Schema (Planned)

### agents
- id (PK)
- agent_id (unique)
- hostname
- ip
- platform
- version
- protocol_version
- registered_at
- last_seen
- status

### devices
- serial (PK)
- agent_id (FK)
- manufacturer
- model
- ip
- hostname
- ... (all device fields)
- first_seen
- last_seen
- updated_at

### metrics_history
- id (PK)
- serial (FK)
- agent_id (FK)
- timestamp
- page_count
- toner_levels (JSON)
- ... (all metrics)

## Version Strategy

**Server and Agent versions:**
- Share same **Protocol Version** for compatibility
- Can have different component versions
- Protocol v1 = Server 0.x-1.x + Agent 0.x-1.x
- Breaking protocol change = bump both to 2.0

See `docs/ROADMAP_TO_1.0.md` for full versioning strategy.

## Configuration

### Environment Variables

```bash
SERVER_PORT=9090              # HTTP port
SERVER_HTTPS_PORT=9443        # HTTPS port
SERVER_DB_PATH=/path/to/db    # Database location
SERVER_LOG_LEVEL=info         # Log level
```

### config.ini (future)

```ini
[server]
port = 9090
https_port = 9443
db_path = /var/lib/printmaster/server.db

[security]
require_agent_auth = true
api_key_header = X-Agent-API-Key

[alerts]
email_enabled = true
smtp_server = smtp.example.com
smtp_from = alerts@example.com
```

## License

Same as PrintMaster Agent (see root LICENSE file)
