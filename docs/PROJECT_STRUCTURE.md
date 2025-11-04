## Project Structure (Overview)

```
printmaster/
├── agent/                          # Go agent (main application)
│   ├── main.go                     # HTTP server, UI, API endpoints
│   ├── config.json                 # Configuration file
│   ├── discover.go                 # Legacy discovery wrapper (deprecated)
│   ├── scanner_api.go              # Bridge to scanner package
│   ├── agent/                      # Discovery and detection logic
│   │   ├── detect.go               # Main Discover() function, IP enumeration
│   │   ├── probe.go                # TCP/ICMP probing
│   │   ├── parse.go                # SNMP PDU parsing
│   │   ├── snmp.go                 # Legacy SNMP operations
│   │   ├── mdns.go                 # mDNS/Bonjour discovery
│   │   ├── ssdp.go                 # SSDP/UPnP discovery
│   │   ├── wsdiscovery.go          # WS-Discovery protocol
│   │   ├── snmptraps.go            # SNMP trap listener
│   │   ├── llmnr.go                # LLMNR name resolution
│   │   ├── arp.go                  # ARP table reading
│   │   ├── merge.go                # Device data merging
│   │   ├── types.go                # Data structures
│   │   └── ...
│   ├── scanner/                    # SNMP scanning and device querying
│   │   ├── detector.go             # Device type detection
│   │   ├── pipeline.go             # Multi-stage scan orchestration
│   │   ├── query.go                # SNMP query execution
│   │   ├── snmp.go                 # Low-level SNMP wrapper
│   │   ├── enumerator.go           # IP range enumeration
│   │   └── vendor/                 # Vendor-specific profiles
│   │       ├── hp.go
│   │       ├── canon.go
│   │       ├── brother.go
│   │       ├── generic.go
│   │       └── registry.go
│   ├── logger/                     # Structured logging
│   │   ├── logger.go               # Logger implementation
│   │   └── logger_test.go
│   ├── storage/                    # SQLite database persistence
│   │   ├── sqlite.go               # Database operations
│   │   ├── device.go               # Device CRUD operations
│   │   ├── agent_config.go         # Settings storage
│   │   └── ...
│   ├── util/                       # Utility functions
│   │   ├── helpers.go              # String/numeric parsing
│   │   └── secret.go               # Encryption utilities
│   ├── tools/                      # Development tools
│   │   ├── aggregate_mib_walks.go
│   │   ├── scan_mib_walks.go
│   │   └── ...
│   └── logs/                       # Log files and diagnostics
├── docs/                           # Documentation
│   ├── AGENT_MODULE.md             # Agent package documentation
│   ├── SCANNER_MODULE.md           # Scanner package documentation
│   ├── LOGGER_MODULE.md            # Logger package documentation
│   ├── SETTINGS_TODO.md            # Unimplemented settings tracker
│   ├── LIVE_DISCOVERY_TODO.md      # Discovery method status
│   ├── API_REFERENCE.md            # HTTP API documentation
│   ├── CONFIGURATION.md            # Config file reference
│   ├── PROJECT_STRUCTURE.md        # This file
│   └── ...
├── dev/                            # Development utilities
│   └── launch.ps1                  # Launch script for debugging
├── build.ps1                       # Build script
└── README.md                       # Project overview
```

## Module Descriptions

### Agent (`agent/agent/`)
Discovery protocols and device detection logic. Handles network scanning, protocol listeners (mDNS, SSDP, WS-Discovery, SNMP traps), IP enumeration, and device probing.

**Key Files**: `detect.go`, `probe.go`, `mdns.go`, `ssdp.go`, `wsdiscovery.go`

**Documentation**: [agent/agent/README.md](../agent/agent/README.md)

### Scanner (`agent/scanner/`)
SNMP querying, vendor detection, and printer information extraction. Multi-stage pipeline for efficient scanning with vendor-specific OID profiles.

**Key Files**: `detector.go`, `pipeline.go`, `query.go`, `vendor/*.go`

**Documentation**: [agent/scanner/README.md](../agent/scanner/README.md)

### Logger (`agent/logger/`)
Structured logging with SSE streaming. Provides level-based logging (ERROR/WARN/INFO/DEBUG), rate limiting, ring buffer, and real-time UI updates.

**Key Files**: `logger.go`

**Documentation**: [agent/logger/README.md](../agent/logger/README.md)

### Storage (`agent/storage/`)
SQLite database layer for device persistence and settings storage. Handles device CRUD operations, scan history, and configuration management.

**Key Files**: `sqlite.go`, `device.go`, `agent_config.go`

**Documentation**: [agent/storage/README.md](../agent/storage/README.md)

### Utilities (`agent/util/`)
Shared helper functions for string parsing, numeric coercion, and encryption.

**Key Files**: `helpers.go`, `secret.go`

### Tools (`agent/tools/`)
Development utilities for MIB analysis, test data generation, and debugging.

## Architecture Principles

### Separation of Concerns
- **Agent**: Network discovery and protocol handling
- **Scanner**: SNMP operations and data parsing
- **Logger**: Centralized logging infrastructure
- **Storage**: Data persistence layer

### Data Flow
```
Network Discovery (agent) 
    → IP candidates 
    → Probing (agent) 
    → SNMP Query (scanner) 
    → Parse Results (scanner/vendor) 
    → Store (storage)
```

### Testing Strategy
- Unit tests for parsing logic (`parse_test.go`)
- Integration tests with real SNMP data (`replay_parse_test.go`)
- Vendor module tests (`scanner/*_test.go`)
- Logger behavior tests (`logger_test.go`)

## Key Design Decisions

### Scanner Refactor (Completed Nov 2, 2025)
- Moved from monolithic `snmp.go` to modular vendor system
- Created vendor-specific OID profiles (HP, Canon, Brother, etc.)
- Implemented multi-stage pipeline (liveness → detection → deep scan)
- Separated concerns: agent (discovery) vs scanner (querying)

### Logging Refactor (Completed Nov 2, 2025)
- Replaced polling-based log system with SSE streaming
- Introduced structured logger with level-based filtering
- Added rate limiting to prevent log spam
- Removed callback-based logging in favor of global logger

### Storage Layer
- SQLite for embedded database (no external dependencies)
- Upsert operations for device updates
- Settings stored in database (overrides config.json)
- Scan history tracking for reporting

## Development Workflow

### Build
```powershell
.\build.ps1 agent
```

### Run
```powershell
cd agent
go run . -port 8080
```

### Test
```powershell
cd agent
go test ./... -v
```

### Debug
```powershell
.\dev\launch.ps1
```

## Related Documentation

### Module Documentation
- [Agent Overview](../agent/README.md) - Quick start and architecture summary
- [Agent Module](../agent/agent/README.md) - Discovery and detection
- [Scanner Module](../agent/scanner/README.md) - SNMP and vendor profiles  
- [Logger Module](../agent/logger/README.md) - Logging system
- [Storage Module](../agent/storage/README.md) - Database persistence

### Feature Documentation
- [API Reference](API_REFERENCE.md) - HTTP endpoints
- [Configuration](CONFIGURATION.md) - Config file format
- [Settings TODO](SETTINGS_TODO.md) - Future features
- [Live Discovery](LIVE_DISCOVERY_TODO.md) - Discovery method status
