# PrintMaster E2E Test Data Directory
#
# This directory contains seed data for E2E testing.
# Databases are pre-populated with test devices, agents, and configuration
# to enable predictable, reproducible tests.
#
# Structure:
#   testdata/
#   ├── server/          # Server data directory (mounted to /data in container)
#   │   └── server.db    # Pre-seeded SQLite database
#   ├── agent/           # Agent data directory (mounted to /data in container)  
#   │   ├── agent.db     # Pre-seeded SQLite database
#   │   └── agent_id     # Fixed agent UUID for test stability
#   └── seed/            # SQL scripts to generate seed databases
#
# The .gitkeep files ensure directories exist in git.
# Actual database files are gitignored and regenerated during test setup.

## Regenerating Test Databases

Run the seed script to create fresh test databases:

```bash
# From tests/ directory
./seed-testdata.sh
```

Or manually:

```bash
# Create server seed database
sqlite3 testdata/server/server.db < seed/server_seed.sql

# Create agent seed database  
sqlite3 testdata/agent/agent.db < seed/agent_seed.sql
```

## Test Data Contents

### Server Database
- 1 tenant: "E2E Test Organization"
- 1 registered agent: "e2e-test-agent" (UUID: e2e00000-0000-0000-0000-000000000001)
- 8 realistic test devices with various vendors:
  - **CV25P8** - Epson WF-C5790 Series (color inkjet MFP)
  - **VXF5012345** - Kyocera ECOSYS M3655idn (mono laser MFP, high volume)
  - **PHCBD82R4K** - HP LaserJet Pro M404dn (mono laser)
  - **U64180H8N123456** - Brother MFC-L8900CDW (color laser MFP)
  - **47TT812** - Lexmark MS621dn (mono laser, low toner warning)
  - **C1J012345** - Xerox VersaLink C405DN (color MFP, paper jam error)
  - **X4MF012345** - Epson ST-C8090 Series (large format inkjet)
  - **VXL8123456** - Kyocera ECOSYS P2040dw (mono laser)
- Sample metrics history with realistic page counts and toner levels
- Audit log entries for agent registration and device alerts

### Agent Database
- Agent configured for server connection (http://server:9090)
- 8 test devices matching server data (same serials/models)
- Realistic metrics:
  - Raw metrics (5-minute samples for last 6 hours)
  - Hourly aggregates (last 24 hours)
  - Daily aggregates (last 7 days)
- Scan history showing device discovery events
- Settings for server integration
- Scanner configuration

## Adding New Test Data

1. Modify the appropriate seed SQL file in `seed/`
2. Run `./seed-testdata.sh` to regenerate databases
3. Update this README if the schema changes
